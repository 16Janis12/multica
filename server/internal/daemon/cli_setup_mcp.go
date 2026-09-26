package daemon

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// CLITokenFetcher is a function that requests a CLI credential token from the Multica server.
type CLITokenFetcher func(ctx context.Context, cliName string) (*CLITokenResponse, error)

// CLIExecutor is a function that executes the local CLI login command.
type CLIExecutor func(ctx context.Context, cliName string, tokenResp *CLITokenResponse, customHost string) (string, error)

// CLISetupMCPServer is a local MCP server that exposes a single toolcall: setup_cli.
// When invoked, it contacts the Multica server for credentials, then logs into the requested CLI.
type CLISetupMCPServer struct {
	tokenFetcher CLITokenFetcher
	cliExecutor  CLIExecutor
	path         string
	logger       *slog.Logger
}

type cliSetupMCPSet struct {
	server *http.Server
	ln     net.Listener
	once   sync.Once
}

func (s *cliSetupMCPSet) Close() error {
	if s == nil {
		return nil
	}
	s.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if s.server != nil {
			_ = s.server.Shutdown(ctx)
		}
		if s.ln != nil {
			_ = s.ln.Close()
		}
	})
	return nil
}

// NewCLISetupMCPServer constructs a new CLISetupMCPServer.
func NewCLISetupMCPServer(tokenFetcher CLITokenFetcher, cliExecutor CLIExecutor, path string, logger *slog.Logger) *CLISetupMCPServer {
	if logger == nil {
		logger = slog.Default()
	}
	if cliExecutor == nil {
		cliExecutor = defaultCLIExecutor
	}
	return &CLISetupMCPServer{
		tokenFetcher: tokenFetcher,
		cliExecutor:  cliExecutor,
		path:         path,
		logger:       logger,
	}
}

// startTaskCLISetupMCP starts a local HTTP MCP server for a task, returning its configuration
// to merge into the agent's effective MCP servers.
func startTaskCLISetupMCP(
	lifetimeCtx context.Context,
	taskID, daemonToken string,
	client *Client,
	logger *slog.Logger,
) (json.RawMessage, *cliSetupMCPSet, error) {
	if client == nil {
		return nil, nil, errors.New("daemon client is required")
	}

	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, nil, fmt.Errorf("generate path token: %w", err)
	}
	path := "/" + hex.EncodeToString(tokenBytes)

	tokenFetcher := func(ctx context.Context, cliName string) (*CLITokenResponse, error) {
		return client.FetchCLIToken(ctx, daemonToken, taskID, "", cliName)
	}

	mcpServer := NewCLISetupMCPServer(tokenFetcher, defaultCLIExecutor, path, logger)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, fmt.Errorf("listen for cli setup MCP server: %w", err)
	}

	httpServer := &http.Server{
		Handler:      mcpServer,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	set := &cliSetupMCPSet{server: httpServer, ln: ln}

	go func() {
		if serveErr := httpServer.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			logger.Warn("cli setup MCP server stopped with error", "task_id", taskID, "error", serveErr)
		}
	}()

	go func() {
		<-lifetimeCtx.Done()
		_ = set.Close()
	}()

	mcpConfig, err := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			"multica-cli-setup": map[string]any{
				"type": "http",
				"url":  "http://" + ln.Addr().String() + path,
			},
		},
	})
	if err != nil {
		_ = set.Close()
		return nil, nil, fmt.Errorf("marshal mcpServers config: %w", err)
	}

	return mcpConfig, set, nil
}

// ServeHTTP implements the HTTP transport for the MCP server.
func (s *CLISetupMCPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.path != "" && r.URL.Path != s.path {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var req struct {
		JSONRPC string `json:"jsonrpc"`
		ID      any    `json:"id"`
		Method  string `json:"method"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid json-rpc", http.StatusBadRequest)
		return
	}

	// Notifications have no id and need no reply body
	if req.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	resp := s.handleMessage(r.Context(), body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp)
}

// ServeStdio implements the stdio transport for the MCP server.
func (s *CLISetupMCPServer) ServeStdio(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	const maxScanTokenSize = 4 * 1024 * 1024
	buf := make([]byte, maxScanTokenSize)
	scanner.Buffer(buf, maxScanTokenSize)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var req struct {
			ID any `json:"id"`
		}
		_ = json.Unmarshal(line, &req)

		resp := s.handleMessage(ctx, line)
		if req.ID != nil && len(resp) > 0 {
			if _, err := out.Write(append(resp, '\n')); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

func (s *CLISetupMCPServer) handleMessage(ctx context.Context, body []byte) []byte {
	var base struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      any             `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(body, &base); err != nil {
		return s.jsonRPCError(nil, -32700, "Parse error")
	}

	switch base.Method {
	case "initialize":
		return s.jsonRPCSuccess(base.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "multica-cli-setup",
				"version": "1.0.0",
			},
		})

	case "notifications/initialized":
		return nil

	case "ping":
		return s.jsonRPCSuccess(base.ID, map[string]any{})

	case "tools/list":
		return s.jsonRPCSuccess(base.ID, map[string]any{
			"tools": []any{
				map[string]any{
					"name":        "setup_cli",
					"description": "Set up and authenticate a CLI tool (GitHub CLI 'gh', GitLab CLI 'glab', Gitea CLI 'tea', etc.) using credentials obtained from the Multica server.",
					"inputSchema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"cli": map[string]any{
								"type":        "string",
								"description": "The CLI to setup and authenticate (e.g. 'github' / 'gh', 'gitlab' / 'glab', 'gitea' / 'tea', 'forgejo').",
							},
							"hostname": map[string]any{
								"type":        "string",
								"description": "Optional hostname or instance URL for enterprise/self-hosted instances (e.g. 'gitlab.mycompany.com').",
							},
						},
						"required": []string{"cli"},
					},
				},
			},
		})

	case "tools/call":
		var call struct {
			Name      string `json:"name"`
			Arguments struct {
				CLI      string `json:"cli"`
				Hostname string `json:"hostname,omitempty"`
			} `json:"arguments"`
		}
		if err := json.Unmarshal(base.Params, &call); err != nil {
			return s.jsonRPCError(base.ID, -32602, "Invalid params")
		}

		if call.Name != "setup_cli" {
			return s.jsonRPCError(base.ID, -32601, fmt.Sprintf("Unknown tool: %s", call.Name))
		}

		cliArg := strings.TrimSpace(call.Arguments.CLI)
		if cliArg == "" {
			return s.jsonRPCToolError(base.ID, "parameter 'cli' is required (e.g. 'github', 'gitlab', 'gitea')")
		}

		if s.tokenFetcher == nil {
			return s.jsonRPCToolError(base.ID, "token fetcher is not configured on this MCP server")
		}

		tokenResp, err := s.tokenFetcher(ctx, cliArg)
		if err != nil {
			return s.jsonRPCToolError(base.ID, fmt.Sprintf("failed to get token from Multica server for CLI %q: %v", cliArg, err))
		}
		if tokenResp == nil || tokenResp.Token == "" {
			return s.jsonRPCToolError(base.ID, fmt.Sprintf("no token returned by Multica server for CLI %q", cliArg))
		}

		executor := s.cliExecutor
		if executor == nil {
			executor = defaultCLIExecutor
		}

		output, err := executor(ctx, cliArg, tokenResp, call.Arguments.Hostname)
		if err != nil {
			return s.jsonRPCToolError(base.ID, fmt.Sprintf("CLI setup failed: %v", err))
		}

		return s.jsonRPCToolSuccess(base.ID, output)

	default:
		return s.jsonRPCError(base.ID, -32601, fmt.Sprintf("Method not found: %s", base.Method))
	}
}

func (s *CLISetupMCPServer) jsonRPCSuccess(id any, result any) []byte {
	resp, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
	return resp
}

func (s *CLISetupMCPServer) jsonRPCError(id any, code int, message string) []byte {
	resp, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
	return resp
}

func (s *CLISetupMCPServer) jsonRPCToolError(id any, message string) []byte {
	return s.jsonRPCSuccess(id, map[string]any{
		"isError": true,
		"content": []map[string]string{
			{
				"type": "text",
				"text": message,
			},
		},
	})
}

func (s *CLISetupMCPServer) jsonRPCToolSuccess(id any, message string) []byte {
	return s.jsonRPCSuccess(id, map[string]any{
		"content": []map[string]string{
			{
				"type": "text",
				"text": message,
			},
		},
	})
}

// defaultCLIExecutor handles authenticating gh, glab, and tea CLIs.
func defaultCLIExecutor(ctx context.Context, cliName string, tokenResp *CLITokenResponse, customHost string) (string, error) {
	norm := strings.ToLower(strings.TrimSpace(cliName))
	norm = strings.TrimSuffix(norm, " cli")

	switch norm {
	case "github", "gh":
		ghPath, err := exec.LookPath("gh")
		if err != nil {
			return "", fmt.Errorf("gh (GitHub CLI) is not installed or not found on PATH: %w", err)
		}

		host := "github.com"
		if customHost != "" {
			host = sanitizeHost(customHost)
		} else if tokenResp.InstanceURL != "" {
			host = sanitizeHost(tokenResp.InstanceURL)
		}

		cmd := exec.CommandContext(ctx, ghPath, "auth", "login", "--hostname", host, "--with-token")
		cmd.Stdin = strings.NewReader(tokenResp.Token)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("gh auth login failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
		}

		res := fmt.Sprintf("Successfully authenticated GitHub CLI (gh) on %s", host)
		if tokenResp.AccountLogin != "" {
			res += fmt.Sprintf(" as %s", tokenResp.AccountLogin)
		}
		if trimmedOut := strings.TrimSpace(string(out)); trimmedOut != "" {
			res += fmt.Sprintf("\n%s", trimmedOut)
		}
		return res, nil

	case "gitlab", "glab":
		glabPath, err := exec.LookPath("glab")
		if err != nil {
			return "", fmt.Errorf("glab (GitLab CLI) is not installed or not found on PATH: %w", err)
		}

		host := "gitlab.com"
		if customHost != "" {
			host = sanitizeHost(customHost)
		} else if tokenResp.InstanceURL != "" {
			host = sanitizeHost(tokenResp.InstanceURL)
		}

		cmd := exec.CommandContext(ctx, glabPath, "auth", "login", "--hostname", host, "--stdin")
		cmd.Stdin = strings.NewReader(tokenResp.Token)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("glab auth login failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
		}

		res := fmt.Sprintf("Successfully authenticated GitLab CLI (glab) on %s", host)
		if tokenResp.AccountLogin != "" {
			res += fmt.Sprintf(" as %s", tokenResp.AccountLogin)
		}
		if trimmedOut := strings.TrimSpace(string(out)); trimmedOut != "" {
			res += fmt.Sprintf("\n%s", trimmedOut)
		}
		return res, nil

	case "gitea", "tea", "forgejo":
		teaPath, err := exec.LookPath("tea")
		if err != nil {
			return "", fmt.Errorf("tea (Gitea CLI) is not installed or not found on PATH: %w", err)
		}

		rawURL := "https://gitea.com"
		if customHost != "" {
			if !strings.HasPrefix(customHost, "http://") && !strings.HasPrefix(customHost, "https://") {
				rawURL = "https://" + customHost
			} else {
				rawURL = customHost
			}
		} else if tokenResp.InstanceURL != "" {
			rawURL = tokenResp.InstanceURL
		}

		loginName := "multica"
		if tokenResp.AccountLogin != "" {
			loginName = tokenResp.AccountLogin
		}

		// Delete existing login with same name if present so add succeeds
		delCmd := exec.CommandContext(ctx, teaPath, "login", "delete", loginName)
		_ = delCmd.Run()

		// Prefer passing token via GITEA_SERVER_TOKEN environment variable rather than command line args
		addCmd := exec.CommandContext(ctx, teaPath, "login", "add", "--name", loginName, "--url", rawURL)
		addCmd.Env = append(addCmd.Environ(), "GITEA_SERVER_TOKEN="+tokenResp.Token)
		out, err := addCmd.CombinedOutput()
		if err != nil {
			// Fallback: try with --token in case the installed tea version does not support GITEA_SERVER_TOKEN
			fallbackCmd := exec.CommandContext(ctx, teaPath, "login", "add", "--name", loginName, "--url", rawURL, "--token", tokenResp.Token)
			fbOut, fbErr := fallbackCmd.CombinedOutput()
			if fbErr != nil {
				return "", fmt.Errorf("tea login add failed: %w (output: %s)", fbErr, strings.TrimSpace(string(fbOut)))
			}
			out = fbOut
		}

		// Set this newly added login as the active default profile
		defCmd := exec.CommandContext(ctx, teaPath, "login", "default", loginName)
		_ = defCmd.Run()

		res := fmt.Sprintf("Successfully configured Gitea CLI (tea) login %q for %s", loginName, rawURL)
		if trimmedOut := strings.TrimSpace(string(out)); trimmedOut != "" {
			res += fmt.Sprintf("\n%s", trimmedOut)
		}
		return res, nil

	default:
		return "", fmt.Errorf("unsupported CLI %q: supported CLIs are GitHub CLI ('gh'/'github'), GitLab CLI ('glab'/'gitlab'), and Gitea CLI ('tea'/'gitea'/'forgejo')", cliName)
	}
}

func sanitizeHost(input string) string {
	input = strings.TrimSpace(input)
	if !strings.Contains(input, "://") {
		input = "https://" + input
	}
	if parsed, err := url.Parse(input); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return strings.TrimRight(strings.TrimPrefix(strings.TrimPrefix(input, "http://"), "https://"), "/")
}
