package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

// antigravityBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args.
var antigravityBlockedArgs = map[string]blockedArgMode{
	"--notices":  blockedStandalone,
	"--help":     blockedStandalone,
	"-h":         blockedStandalone,
	"--helpfull": blockedStandalone,
}

// antigravityACPLaunchArgs returns OS-specific launch flags matching the
// official ACP registry distribution for Google Antigravity.
func antigravityACPLaunchArgs() []string {
	if runtime.GOOS == "linux" {
		return []string{"--uid="}
	}
	return nil
}

// antigravityDefaultExecutable is the standard binary name for the Google
// Antigravity ACP server.
func antigravityDefaultExecutable() string {
	return "agy_acp_server"
}

// antigravityResolveExecutable finds the installed ACP server executable or
// falls back to the default command name.
func antigravityResolveExecutable() string {
	for _, candidate := range []string{"agy_acp_server", "agy_acp_server.par", "antigravity-acp", "agy"} {
		if _, err := exec.LookPath(candidate); err == nil {
			return candidate
		}
	}
	return antigravityDefaultExecutable()
}

// ensureAntigravityHarnessEnv checks if ANTIGRAVITY_HARNESS_PATH is set; if not,
// it checks whether localharness_external or localharness exists alongside the
// resolved executable and injects ANTIGRAVITY_HARNESS_PATH so session/new
// succeeds out-of-the-box.
func ensureAntigravityHarnessEnv(env map[string]string, execPath string) map[string]string {
	if env != nil && env["ANTIGRAVITY_HARNESS_PATH"] != "" {
		return env
	}
	if resolved, err := exec.LookPath(execPath); err == nil {
		dir := filepath.Dir(resolved)
		for _, name := range []string{"localharness_external", "localharness"} {
			h := filepath.Join(dir, name)
			if info, err := os.Stat(h); err == nil && !info.IsDir() {
				out := make(map[string]string, len(env)+1)
				for k, v := range env {
					out[k] = v
				}
				out["ANTIGRAVITY_HARNESS_PATH"] = h
				return out
			}
		}
	}
	return env
}

// antigravityToolNameFromTitle extracts a canonical tool name from an ACP
// tool call title. Antigravity ACP emits titles like "Running view_file"
// or "Running run_command".
func antigravityToolNameFromTitle(title, kind string) string {
	t := strings.TrimSpace(title)
	if strings.HasPrefix(t, "Running ") {
		return strings.TrimSpace(strings.TrimPrefix(t, "Running "))
	}
	return hermesToolNameFromTitle(title, kind)
}

// antigravityReaderDrainGrace bounds how long the turn waits for trailing ACP
// notifications after the session/prompt response.
var antigravityReaderDrainGrace = 2 * time.Second

// antigravityBackend implements Backend by spawning the Google Antigravity ACP
// server (`agy_acp_server` / `agy_acp_server.par`) and communicating via the
// standard ACP (Agent Client Protocol) JSON-RPC 2.0 transport over stdin/stdout.
type antigravityBackend struct {
	cfg Config
}

func (b *antigravityBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = antigravityResolveExecutable()
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("antigravity executable not found at %q: %w", execPath, err)
	}

	// Translate the agent's mcp_config (Claude-style object of objects)
	// into the array shape ACP session/new and session/resume expect.
	mcpServers, err := buildACPMcpServers(opts.McpConfig, b.cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("antigravity: invalid mcp_config: %w", err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	antigravityArgs := append(antigravityACPLaunchArgs(), filterCustomArgs(opts.CustomArgs, antigravityBlockedArgs, b.cfg.Logger)...)
	cmd := b.cfg.commandAt(execPath).exec(runCtx, antigravityArgs...)
	hideAgentWindow(cmd)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(antigravityArgs))
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	childEnv := ensureAntigravityHarnessEnv(b.cfg.Env, execPath)
	cmd.Env = buildEnv(childEnv)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("antigravity stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("antigravity stdin pipe: %w", err)
	}

	providerErr := newACPProviderErrorSniffer("antigravity")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("antigravity stderr pipe: %w", err)
	}

	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		cancel()
		return nil, fmt.Errorf("start antigravity: %w", err)
	}

	stderrSink := io.MultiWriter(newLogWriter(b.cfg.Logger, "[antigravity:stderr] "), providerErr)
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(stderrSink, stderr)
	}()

	b.cfg.Logger.Info("antigravity acp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	var deliverable acpDeliverableTracker
	var streamingCurrentTurn atomic.Bool
	promptDone := make(chan hermesPromptResult, 1)
	activity := make(chan struct{}, 1)

	c := &hermesClient{
		cfg:                        b.cfg,
		stdin:                      stdin,
		pending:                    make(map[int]*pendingRPC),
		pendingTools:               make(map[string]*pendingToolCall),
		toolStartCarriesFinalInput: true,
		acceptNotification: func(string) bool {
			return streamingCurrentTurn.Load()
		},
		onActivity: func() {
			select {
			case activity <- struct{}{}:
			default:
			}
		},
		onMessage: func(msg Message) {
			if !streamingCurrentTurn.Load() {
				return
			}
			if msg.Type == MessageToolUse {
				msg.Tool = antigravityToolNameFromTitle(msg.Tool, "")
			}
			deliverable.observe(msg)
			trySend(msgCh, msg)
		},
		onPromptDone: func(result hermesPromptResult) {
			if !streamingCurrentTurn.Load() {
				return
			}
			select {
			case promptDone <- result:
			default:
			}
		},
	}

	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		scanner := newAgentStreamScanner(stdout)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			c.handleLine(line)
		}
		c.closeAllPending(fmt.Errorf("antigravity process exited"))
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer func() {
			stdin.Close()
			_ = cmd.Wait()
			releaseProcessGroup(cmd)
		}()

		startTime := time.Now()
		finalStatus := "completed"
		var finalError string
		var sessionID string
		var resumeRejected bool
		effectiveModel := strings.TrimSpace(opts.Model)

		// 1. Initialize handshake
		initResult, err := c.request(runCtx, "initialize", map[string]any{
			"protocolVersion": 1,
			"clientInfo": map[string]any{
				"name":    "multica-agent-sdk",
				"version": "0.2.0",
			},
			"clientCapabilities": map[string]any{},
		})
		if err != nil {
			finalStatus = "failed"
			finalError = fmt.Sprintf("antigravity initialize failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(initResult), "antigravity", b.cfg)

		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}

		// 2. Create or resume session
		if opts.ResumeSessionID != "" {
			result, err := c.request(runCtx, "session/resume", map[string]any{
				"cwd":        cwd,
				"sessionId":  opts.ResumeSessionID,
				"mcpServers": mcpServers,
			})
			if err != nil {
				finalStatus, finalError, resumeRejected = classifyACPResumeFailure(
					runCtx, "antigravity", "session/resume", err, timeout, b.cfg.Logger)
				resCh <- Result{
					Status:         finalStatus,
					Error:          finalError,
					DurationMs:     time.Since(startTime).Milliseconds(),
					ResumeRejected: resumeRejected,
				}
				return
			}
			var changed bool
			sessionID, changed = resolveResumedSessionID(opts.ResumeSessionID, result)
			if changed {
				b.cfg.Logger.Warn("agent returned a different session id on resume — original was likely lost; continuing with the new id",
					"backend", "antigravity",
					"requested", opts.ResumeSessionID,
					"actual", sessionID,
				)
			}
			if effectiveModel == "" {
				effectiveModel = extractACPCurrentModelID(result)
			}
		} else {
			result, err := c.request(runCtx, "session/new", map[string]any{
				"cwd":        cwd,
				"mcpServers": mcpServers,
			})
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("antigravity session/new failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionID = extractACPSessionID(result)
			if sessionID == "" {
				finalStatus = "failed"
				finalError = "antigravity session/new returned no session ID"
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			if effectiveModel == "" {
				effectiveModel = extractACPCurrentModelID(result)
			}
		}

		c.sessionID = sessionID
		b.cfg.Logger.Info("antigravity session created", "session_id", sessionID)

		// 3. Set mode to "yolo" to auto-approve tool permission requests
		if _, err := c.request(runCtx, "session/set_mode", map[string]any{
			"sessionId": sessionID,
			"modeId":    "yolo",
		}); err != nil {
			b.cfg.Logger.Debug("antigravity set_mode yolo failed or not supported", "error", err)
		}

		// 4. Set model if specified
		if opts.Model != "" {
			if _, err := c.request(runCtx, "session/set_model", map[string]any{
				"sessionId": sessionID,
				"modelId":   opts.Model,
			}); err != nil {
				b.cfg.Logger.Warn("antigravity set_session_model failed", "error", err, "requested_model", opts.Model)
				finalStatus = "failed"
				finalError = fmt.Sprintf("antigravity could not switch to model %q: %v", opts.Model, err)
				if setupFailureWithholdsSessionID(opts) {
					sessionID = ""
				} else if isACPSessionNotFound(err) {
					b.cfg.Logger.Warn("resumed session not found at set_model time; clearing session id so the daemon retries fresh",
						"backend", "antigravity",
						"session_id", sessionID,
					)
					sessionID = ""
					resumeRejected = true
				}
				resCh <- Result{
					Status:         finalStatus,
					Error:          finalError,
					DurationMs:     time.Since(startTime).Milliseconds(),
					SessionID:      sessionID,
					ResumeRejected: resumeRejected,
				}
				return
			}
			effectiveModel = opts.Model
			b.cfg.Logger.Info("antigravity session model set", "model", opts.Model)
		}

		// 5. Send prompt
		userText := prompt
		if opts.SystemPrompt != "" {
			userText = opts.SystemPrompt + "\n\n---\n\n" + prompt
		}
		promptBlocks := []map[string]any{
			{"type": "text", "text": userText},
		}

		streamingCurrentTurn.Store(true)
		_, err = c.request(runCtx, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"prompt":    promptBlocks,
		})
		if err != nil {
			if runCtx.Err() == context.DeadlineExceeded {
				finalStatus = "timeout"
				finalError = fmt.Sprintf("antigravity timed out after %s", timeout)
			} else if runCtx.Err() == context.Canceled {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			} else {
				finalStatus = "failed"
				finalError = fmt.Sprintf("antigravity session/prompt failed: %v", err)
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					b.cfg.Logger.Warn("resumed session not found at prompt time; clearing session id so the daemon retries fresh",
						"backend", "antigravity",
						"session_id", sessionID,
					)
					sessionID = ""
					resumeRejected = true
				}
			}
		} else {
			select {
			case pr := <-promptDone:
				if pr.stopReason == "cancelled" {
					finalStatus = "aborted"
					finalError = "antigravity cancelled the prompt"
				}
				c.mergeUsage(pr.usage)
			default:
			}
			waitForACPNotificationQuiescence(runCtx, activity, readerDone, acpNotificationQuietTime, antigravityReaderDrainGrace)
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("antigravity finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		stdin.Close()
		cancel()

		<-readerDone
		<-stderrDone

		finalOutput, providerErrorOutput := deliverable.result()
		finalStatus, finalError = promoteACPResultOnProviderError(finalStatus, finalError, providerErrorOutput, providerErr)

		u := c.accumulatedUsage()
		var usageMap map[string]TokenUsage
		if acpUsagePresent(u) {
			model := effectiveModel
			if model == "" {
				model = "unknown"
			}
			usageMap = map[string]TokenUsage{model: u}
		}

		resCh <- Result{
			Status:         finalStatus,
			Output:         finalOutput,
			Error:          finalError,
			DurationMs:     duration.Milliseconds(),
			SessionID:      sessionID,
			Usage:          usageMap,
			ResumeRejected: resumeRejected,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// antigravityModelError returns an actionable error when `model` is non-empty
// and definitively absent from `available`.
func antigravityModelError(model string, available []Model) error {
	if model == "" || len(available) == 0 {
		return nil
	}
	ids := make([]string, 0, len(available))
	for _, m := range available {
		if m.ID == model {
			return nil
		}
		ids = append(ids, m.ID)
	}
	return fmt.Errorf(
		"antigravity model %q is not available; pick one of: %s",
		model, strings.Join(ids, ", "),
	)
}
