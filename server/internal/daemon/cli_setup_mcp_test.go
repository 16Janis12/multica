package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func callCLIMCP(t *testing.T, server *CLISetupMCPServer, path, body string) map[string]any {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	var decoded map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
	return decoded
}

func TestCLISetupMCP_ToolsList(t *testing.T) {
	server := NewCLISetupMCPServer(nil, nil, "/test", nil)
	resp := callCLIMCP(t, server, "/test", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %v", resp)
	}
	tools, ok := result["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %v", result["tools"])
	}
	tool0, ok := tools[0].(map[string]any)
	if !ok || tool0["name"] != "setup_cli" {
		t.Fatalf("expected tool setup_cli, got %v", tools[0])
	}
}

func TestCLISetupMCP_InitializeAndPing(t *testing.T) {
	server := NewCLISetupMCPServer(nil, nil, "/test", nil)

	// initialize
	initResp := callCLIMCP(t, server, "/test", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	initResult, ok := initResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result in initialize: %v", initResp)
	}
	serverInfo := initResult["serverInfo"].(map[string]any)
	if serverInfo["name"] != "multica-cli-setup" {
		t.Fatalf("serverInfo name = %v", serverInfo["name"])
	}

	// ping
	pingResp := callCLIMCP(t, server, "/test", `{"jsonrpc":"2.0","id":2,"method":"ping"}`)
	if pingResp["error"] != nil {
		t.Fatalf("expected ping success, got error: %v", pingResp["error"])
	}
}

func TestCLISetupMCP_ToolsCall_Success(t *testing.T) {
	var fetchedCLI string
	mockFetcher := func(ctx context.Context, cliName string) (*CLITokenResponse, error) {
		fetchedCLI = cliName
		return &CLITokenResponse{
			CLI:          cliName,
			Token:        "secret-token-xyz",
			InstanceURL:  "https://github.com",
			AccountLogin: "octocat",
		}, nil
	}

	var executedCLI, executedToken, executedHost string
	mockExecutor := func(ctx context.Context, cliName string, tokenResp *CLITokenResponse, customHost string) (string, error) {
		executedCLI = cliName
		executedToken = tokenResp.Token
		executedHost = customHost
		return "Authenticated octocat on github.com", nil
	}

	server := NewCLISetupMCPServer(mockFetcher, mockExecutor, "/test", nil)
	resp := callCLIMCP(t, server, "/test", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"setup_cli","arguments":{"cli":"github","hostname":"github.com"}}}`)

	if resp["error"] != nil {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object: %v", resp)
	}
	if result["isError"] == true {
		t.Fatalf("expected isError to not be true: %v", result)
	}
	content := result["content"].([]any)
	firstContent := content[0].(map[string]any)
	text := firstContent["text"].(string)
	if !strings.Contains(text, "Authenticated octocat") {
		t.Fatalf("unexpected text output: %s", text)
	}

	if fetchedCLI != "github" {
		t.Fatalf("expected fetchedCLI=github, got %s", fetchedCLI)
	}
	if executedCLI != "github" || executedToken != "secret-token-xyz" || executedHost != "github.com" {
		t.Fatalf("unexpected executor args: %s, %s, %s", executedCLI, executedToken, executedHost)
	}
}

func TestCLISetupMCP_ToolsCall_TokenFetchFailure(t *testing.T) {
	mockFetcher := func(ctx context.Context, cliName string) (*CLITokenResponse, error) {
		return nil, errors.New("no connection configured in workspace")
	}

	server := NewCLISetupMCPServer(mockFetcher, nil, "/test", nil)
	resp := callCLIMCP(t, server, "/test", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"setup_cli","arguments":{"cli":"gitlab"}}}`)

	// A failed token fetch must surface as a TOOL error (isError: true), not a protocol error
	if resp["error"] != nil {
		t.Fatalf("should not be a JSON-RPC protocol error: %v", resp["error"])
	}
	result := resp["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("expected isError=true, got %v", result)
	}
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "failed to get token from Multica server") {
		t.Fatalf("expected error text, got %s", text)
	}
}

func TestCLISetupMCP_ToolsCall_ExecutorFailure(t *testing.T) {
	mockFetcher := func(ctx context.Context, cliName string) (*CLITokenResponse, error) {
		return &CLITokenResponse{CLI: cliName, Token: "tok"}, nil
	}
	mockExecutor := func(ctx context.Context, cliName string, tokenResp *CLITokenResponse, customHost string) (string, error) {
		return "", errors.New("glab: command not found")
	}

	server := NewCLISetupMCPServer(mockFetcher, mockExecutor, "/test", nil)
	resp := callCLIMCP(t, server, "/test", `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"setup_cli","arguments":{"cli":"gitlab"}}}`)

	result := resp["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("expected isError=true, got %v", result)
	}
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "CLI setup failed: glab: command not found") {
		t.Fatalf("expected executor error text, got %s", text)
	}
}

func TestCLISetupMCP_ToolsCall_MissingParam(t *testing.T) {
	server := NewCLISetupMCPServer(nil, nil, "/test", nil)
	resp := callCLIMCP(t, server, "/test", `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"setup_cli","arguments":{}}}`)

	result := resp["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("expected isError=true for missing cli argument, got %v", result)
	}
}

func TestCLISetupMCP_PathValidation(t *testing.T) {
	server := NewCLISetupMCPServer(nil, nil, "/secret-token", nil)
	req, _ := http.NewRequest(http.MethodPost, "/wrong-path", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for wrong path, got %d", rec.Code)
	}
}

func TestCLISetupMCP_Stdio(t *testing.T) {
	mockFetcher := func(ctx context.Context, cliName string) (*CLITokenResponse, error) {
		return &CLITokenResponse{CLI: cliName, Token: "tok-abc"}, nil
	}
	mockExecutor := func(ctx context.Context, cliName string, tokenResp *CLITokenResponse, customHost string) (string, error) {
		return "Authenticated successfully", nil
	}

	server := NewCLISetupMCPServer(mockFetcher, mockExecutor, "", nil)

	in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":100,"method":"tools/list"}` + "\n")
	var out bytes.Buffer

	if err := server.ServeStdio(context.Background(), in, &out); err != nil {
		t.Fatalf("ServeStdio failed: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("decode stdio output: %v", err)
	}
	if resp["id"] != float64(100) {
		t.Fatalf("expected id=100, got %v", resp["id"])
	}
}

func TestDefaultCLIExecutor_UnsupportedCLI(t *testing.T) {
	_, err := defaultCLIExecutor(context.Background(), "unknown_cli", &CLITokenResponse{Token: "x"}, "")
	if err == nil {
		t.Fatal("expected error for unknown CLI, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported CLI") {
		t.Fatalf("expected unsupported CLI error, got %v", err)
	}
}

func TestStartTaskCLISetupMCP_EndToEnd(t *testing.T) {
	// Mock server that answers the cli-token API call
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/daemon/tasks/task-999/cli-token" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			CLI string `json:"cli"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"cli":           req.CLI,
			"token":         "mock-api-token",
			"instance_url":  "https://github.com",
			"account_login": "mock-user",
		})
	}))
	defer mockAPI.Close()

	client := NewClient(mockAPI.URL)
	lifetimeCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	configJSON, set, err := startTaskCLISetupMCP(lifetimeCtx, "task-999", "daemon-tok", client, slog.Default())
	if err != nil {
		t.Fatalf("startTaskCLISetupMCP: %v", err)
	}
	defer set.Close()

	var cfg struct {
		MCPServers map[string]struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	serverCfg, ok := cfg.MCPServers["multica-cli-setup"]
	if !ok || serverCfg.Type != "http" || serverCfg.URL == "" {
		t.Fatalf("multica-cli-setup MCP configuration missing or invalid: %+v", cfg)
	}

	// Make HTTP call to the running MCP server
	httpClient := &http.Client{Timeout: 5 * time.Second}
	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	resp, err := httpClient.Post(serverCfg.URL, "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("call running MCP server: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	var rpcResp map[string]any
	if err := json.Unmarshal(bodyBytes, &rpcResp); err != nil {
		t.Fatalf("decode rpc response: %v", err)
	}
	result, ok := rpcResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object: %v", rpcResp)
	}
	tools, ok := result["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %v", result["tools"])
	}
}
func TestSanitizeHost(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"github.com", "github.com"},
		{"https://github.com", "github.com"},
		{"https://github.com/", "github.com"},
		{"gitlab.example.com/", "gitlab.example.com"},
		{"https://gitlab.example.com/some/path", "gitlab.example.com"},
		{"https://gitlab.example.com:8443/api/v4", "gitlab.example.com:8443"},
		{"gitlab.example.com:8443", "gitlab.example.com:8443"},
	}

	for _, tt := range tests {
		got := sanitizeHost(tt.input)
		if got != tt.expected {
			t.Errorf("sanitizeHost(%q) = %q, expected %q", tt.input, got, tt.expected)
		}
	}
}

func TestCLISetupMCPSet_Close_Idempotent(t *testing.T) {
	set := &cliSetupMCPSet{}
	if err := set.Close(); err != nil {
		t.Fatalf("first close failed: %v", err)
	}
	if err := set.Close(); err != nil {
		t.Fatalf("second close failed: %v", err)
	}
}
