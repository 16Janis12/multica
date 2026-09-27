package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

// antigravityBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args.
var antigravityBlockedArgs = map[string]blockedArgMode{
	"-p":                             blockedWithValue,
	"--print":                        blockedWithValue,
	"--prompt":                       blockedWithValue,
	"-i":                             blockedStandalone, // interactive mode requires a TTY and cannot run under the daemon
	"--prompt-interactive":           blockedStandalone,
	"-c":                             blockedStandalone, // resume via --conversation, not --continue
	"--continue":                     blockedStandalone,
	"--conversation":                 blockedWithValue, // managed via ExecOptions.ResumeSessionID
	"--model":                        blockedWithValue, // managed via ExecOptions.Model / agent.model
	"--output-format":                blockedWithValue, // stream-json is required for token accounting
	"--print-timeout":                blockedWithValue,
	"--dangerously-skip-permissions": blockedStandalone, // always-on in daemon mode
	"--log-file":                     blockedWithValue,  // daemon needs it for session capture
	"--settings":                     blockedWithValue,  // Claude Code-only flag; agy rejects it
	"--notices":                      blockedStandalone,
	"--help":                         blockedStandalone,
	"-h":                             blockedStandalone,
	"--helpfull":                     blockedStandalone,
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

// isAntigravityACPExecutable reports whether the resolved executable represents
// an ACP JSON-RPC server binary rather than the legacy agy CLI.
func isAntigravityACPExecutable(execPath string) bool {
	base := filepath.Base(execPath)
	return strings.Contains(base, "acp") || strings.HasSuffix(base, ".par")
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

// antigravityBackend implements Backend by spawning either the Google Antigravity
// ACP server (agy_acp_server / agy_acp_server.par) or the legacy agy CLI runner.
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

	if isAntigravityACPExecutable(execPath) {
		return b.executeACP(ctx, prompt, opts, execPath)
	}
	return b.executeCLI(ctx, prompt, opts, execPath)
}

func (b *antigravityBackend) executeACP(ctx context.Context, prompt string, opts ExecOptions, execPath string) (*Session, error) {
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

// ── Legacy CLI runner (agy -p) ──

type antigravityStreamUsage struct {
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	ThinkingTokens   int64 `json:"thinking_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

type antigravityStreamStepUpdate struct {
	ConversationID string                  `json:"conversation_id"`
	StepIndex      *int                    `json:"step_index"`
	State          string                  `json:"state"`
	StepType       string                  `json:"step_type"`
	TextDelta      string                  `json:"text_delta"`
	Usage          *antigravityStreamUsage `json:"usage"`
	ToolName       string                  `json:"tool_name"`
	ToolInfo       *antigravityStreamTool  `json:"tool_info"`
}

type antigravityStreamTool struct {
	Name       string          `json:"name"`
	Parameters map[string]any  `json:"parameters"`
	Output     json.RawMessage `json:"output"`
	Error      *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

type antigravityToolState struct {
	name string
	done bool
}

// Normalize the shared transcript input without duplicating commands or file
// bodies. Preserve collisions and unknown fields, and do not mutate the snapshot.
func antigravityToolInput(parameters map[string]any) map[string]any {
	input := maps.Clone(parameters)
	for _, alias := range []struct {
		from, to   string
		allowEmpty bool
	}{
		{"CommandLine", "command", false},
		{"AbsolutePath", "file_path", false},
		{"TargetFile", "file_path", false},
		{"CodeContent", "content", true},
		{"TargetContent", "old_string", true},
		{"ReplacementContent", "new_string", true},
	} {
		if _, exists := input[alias.to]; exists {
			continue
		}
		if value, ok := parameters[alias.from].(string); ok && (value != "" || alias.allowEmpty) {
			input[alias.to] = value
			delete(input, alias.from)
		}
	}
	return input
}

// Each step has one tool lifecycle, even when agy repeats state snapshots or
// only emits DONE. The existing daemon uploader handles these normal messages;
// tool output must never be appended to the assistant's final answer.
func antigravityToolMessages(step *antigravityStreamStepUpdate, states map[int]antigravityToolState) []Message {
	if step.StepType != "tool" || step.StepIndex == nil || *step.StepIndex < 0 {
		return nil
	}
	active := strings.EqualFold(step.State, "active")
	done := strings.EqualFold(step.State, "done")
	if !active && !done {
		return nil
	}
	index := *step.StepIndex
	state := states[index]
	if state.done {
		return nil
	}
	callID := fmt.Sprintf("agy-step-%d", index)
	var messages []Message
	if state.name == "" {
		name := step.ToolName
		var input map[string]any
		if step.ToolInfo != nil {
			if name == "" {
				name = step.ToolInfo.Name
			}
			input = antigravityToolInput(step.ToolInfo.Parameters)
		}
		if name == "" {
			return nil // A later snapshot may provide the missing metadata.
		}
		state.name = name
		messages = append(messages, Message{Type: MessageToolUse, Tool: name, CallID: callID, Input: input})
	}
	if done {
		var output string
		if info := step.ToolInfo; info != nil {
			if len(info.Output) > 0 && string(info.Output) != "null" {
				if err := json.Unmarshal(info.Output, &output); err != nil {
					output = string(info.Output)
				}
			}
			if info.Error != nil {
				detail := info.Error.Message
				if info.Error.Type != "" {
					if detail != "" {
						detail = info.Error.Type + ": " + detail
					} else {
						detail = info.Error.Type
					}
				}
				// The daemon uploads a bounded prefix of tool output. Put the
				// error first so verbose stdout cannot hide the failure detail.
				if output != "" {
					output = "\n" + output
				}
				output = "Tool error: " + detail + output
			}
		}
		messages = append(messages, Message{Type: MessageToolResult, Tool: state.name, CallID: callID, Output: output})
		state.done = true
	}
	states[index] = state
	return messages
}

type antigravityStreamResult struct {
	ConversationID string                  `json:"conversation_id"`
	Status         string                  `json:"status"`
	Response       string                  `json:"response"`
	Error          string                  `json:"error"`
	Usage          *antigravityStreamUsage `json:"usage"`
}

type antigravityStreamEvent struct {
	Event          string `json:"event"`
	ConversationID string `json:"conversation_id"`
	Init           struct {
		Model string `json:"model"`
	} `json:"init"`
	StepUpdate *antigravityStreamStepUpdate `json:"step_update"`
	Result     *antigravityStreamResult     `json:"result"`
}

const antigravityNetworkIssueError = "There was a network issue connecting to the server, please try again."

func (u antigravityStreamUsage) hasTokens() bool {
	return u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0
}

func (u antigravityStreamUsage) tokenUsage() TokenUsage {
	return TokenUsage{
		InputTokens:      nonNegativeTokens(u.InputTokens),
		OutputTokens:     nonNegativeTokens(u.OutputTokens),
		CacheReadTokens:  nonNegativeTokens(u.CacheReadTokens),
		CacheWriteTokens: nonNegativeTokens(u.CacheWriteTokens),
	}
}

func sumAntigravityStepUsage(steps map[int]TokenUsage) (TokenUsage, bool) {
	var total TokenUsage
	for _, step := range steps {
		total.InputTokens += step.InputTokens
		total.OutputTokens += step.OutputTokens
		total.CacheReadTokens += step.CacheReadTokens
		total.CacheWriteTokens += step.CacheWriteTokens
	}
	return total, len(steps) > 0
}

func antigravityResultStatus(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "", "SUCCESS", "COMPLETED":
		return "completed"
	case "CANCELLED", "CANCELED":
		return "cancelled"
	case "ABORTED":
		return "aborted"
	case "TIMEOUT", "TIMED_OUT":
		return "timeout"
	default:
		return "failed"
	}
}

func antigravityCompletedDespiteTrailingNetworkError(providerError, response string, agentResponseDone bool) bool {
	return strings.EqualFold(strings.TrimSpace(providerError), antigravityNetworkIssueError) &&
		strings.TrimSpace(response) != "" &&
		agentResponseDone
}

var antigravitySelectedModelRe = regexp.MustCompile(
	`Propagating selected model override to backend:\s*label="([^"\r\n]+)"`,
)

func readAntigravitySelectedModel(logPath, streamModel, configuredModel string) string {
	if model := strings.TrimSpace(streamModel); model != "" {
		return model
	}
	if logPath != "" {
		if data, err := os.ReadFile(logPath); err == nil {
			matches := antigravitySelectedModelRe.FindAllSubmatch(data, -1)
			if len(matches) > 0 {
				return strings.TrimSpace(string(matches[len(matches)-1][1]))
			}
		}
	}
	if model := strings.TrimSpace(configuredModel); model != "" {
		return model
	}
	return "unknown"
}

func (b *antigravityBackend) executeCLI(ctx context.Context, prompt string, opts ExecOptions, execPath string) (*Session, error) {
	if opts.Model != "" {
		catalog, _ := ListModels(ctx, "antigravity", b.cfg.commandAt(execPath))
		if err := antigravityModelError(opts.Model, catalog.Models); err != nil {
			return nil, err
		}
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	logFile, err := os.CreateTemp("", "multica-agy-log-*.log")
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create agy log file: %w", err)
	}
	logPath := logFile.Name()
	_ = logFile.Close()

	args := buildAntigravityArgs(prompt, logPath, timeout, opts, b.cfg.Logger)

	cmd := b.cfg.commandAt(execPath).exec(runCtx, args...)
	hideAgentWindow(cmd)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args))
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		_ = os.Remove(logPath)
		return nil, fmt.Errorf("agy stdout pipe: %w", err)
	}
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[agy:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		cancel()
		_ = os.Remove(logPath)
		return nil, fmt.Errorf("start agy: %w", err)
	}

	b.cfg.Logger.Info("agy started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		<-runCtx.Done()
		_ = stdout.Close()
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer os.Remove(logPath)

		startTime := time.Now()
		var output strings.Builder
		var streamModel string
		var streamSessionID string
		var streamResultStatus string
		var streamResultError string
		var streamResponse string
		var streamResultUsage *antigravityStreamUsage
		streamStepUsage := make(map[int]TokenUsage)
		streamTools := make(map[int]antigravityToolState)
		streamLatestAgentResponseStep := -1
		streamLatestAgentResponseDone := false
		finalStatus := "completed"
		var finalError string

		scanner := newAgentStreamScanner(stdout)

		trySend(msgCh, Message{Type: MessageStatus, Status: "running"})

	streamLoop:
		for scanner.Scan() {
			line := scanner.Text()
			var event antigravityStreamEvent
			if err := json.Unmarshal([]byte(line), &event); err == nil && event.Event != "" {
				switch event.Event {
				case "init":
					streamSessionID = event.ConversationID
					streamModel = event.Init.Model
				case "step_update":
					if event.StepUpdate == nil {
						continue
					}
					if event.StepUpdate.ConversationID != "" {
						streamSessionID = event.StepUpdate.ConversationID
					}
					for _, message := range antigravityToolMessages(event.StepUpdate, streamTools) {
						select {
						case msgCh <- message:
						case <-runCtx.Done():
							break streamLoop
						}
					}
					if event.StepUpdate.StepType == "agent_response" && event.StepUpdate.StepIndex != nil && *event.StepUpdate.StepIndex >= streamLatestAgentResponseStep {
						streamLatestAgentResponseStep = *event.StepUpdate.StepIndex
						streamLatestAgentResponseDone = strings.EqualFold(event.StepUpdate.State, "done")
					}
					if event.StepUpdate.StepType == "agent_response" && event.StepUpdate.TextDelta != "" {
						output.WriteString(event.StepUpdate.TextDelta)
						trySend(msgCh, Message{Type: MessageText, Content: event.StepUpdate.TextDelta})
					}
					if strings.EqualFold(event.StepUpdate.State, "done") && event.StepUpdate.StepIndex != nil && event.StepUpdate.Usage != nil && event.StepUpdate.Usage.hasTokens() {
						streamStepUsage[*event.StepUpdate.StepIndex] = event.StepUpdate.Usage.tokenUsage()
					}
				case "result":
					if event.Result == nil {
						continue
					}
					if event.Result.ConversationID != "" {
						streamSessionID = event.Result.ConversationID
					}
					streamResultStatus = event.Result.Status
					streamResultError = event.Result.Error
					streamResponse = event.Result.Response
					if event.Result.Usage != nil && event.Result.Usage.hasTokens() {
						streamResultUsage = event.Result.Usage
					}
				}
				continue
			}
			chunk := line
			if output.Len() > 0 {
				output.WriteByte('\n')
				chunk = "\n" + line
			}
			output.WriteString(line)
			if chunk != "" {
				trySend(msgCh, Message{Type: MessageText, Content: chunk})
			}
		}
		if err := scanner.Err(); err != nil {
			b.cfg.Logger.Warn("agy stdout scanner error", "err", err)
		}

		waitErr := cmd.Wait()
		releaseProcessGroup(cmd)
		duration := time.Since(startTime)

		sessionID := streamSessionID
		if sessionID == "" {
			sessionID = readAntigravityConversationID(logPath)
		}

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("agy timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if status := antigravityResultStatus(streamResultStatus); finalStatus == "completed" && status != "completed" {
			if antigravityCompletedDespiteTrailingNetworkError(streamResultError, streamResponse, streamLatestAgentResponseDone) {
				b.cfg.Logger.Warn("agy reported a trailing network error after a completed response", "err", streamResultError)
			} else {
				finalStatus = status
				finalError = streamResultError
				if finalError == "" {
					finalError = fmt.Sprintf("agy returned status %s", streamResultStatus)
				}
			}
		} else if waitErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("agy exited with error: %v", waitErr)
		} else if finalStatus == "completed" && antigravityPrintTimedOut(logPath) {
			finalStatus = "timeout"
			finalError = fmt.Sprintf(
				"agy --print-timeout elapsed after %s waiting for the agent response; a long-running command likely outlived the print timeout",
				antigravityPrintTimeout(timeout),
			)
		} else if providerErr := antigravityProviderError(logPath); finalStatus == "completed" && providerErr != "" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("agy provider error: %s", providerErr)
		}
		if finalError != "" {
			finalError = withAgentStderr(finalError, "agy", stderrBuf.Tail())
		}

		finalOutput := output.String()
		if streamResponse != "" {
			if finalOutput == "" {
				trySend(msgCh, Message{Type: MessageText, Content: streamResponse})
			}
			finalOutput = streamResponse
		}
		if finalStatus == "completed" && strings.TrimSpace(finalOutput) == "" {
			if recovered := readAntigravityTranscriptOutput(logPath, sessionID); recovered != "" {
				finalOutput = recovered
				trySend(msgCh, Message{Type: MessageText, Content: recovered})
				b.cfg.Logger.Info("agy recovered empty stdout from transcript", "bytes", len(recovered))
			}
		}

		b.cfg.Logger.Info("agy finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		usage, hasStepUsage := sumAntigravityStepUsage(streamStepUsage)
		if !hasStepUsage && streamResultUsage != nil {
			usage = streamResultUsage.tokenUsage()
		}
		usageByModel := map[string]TokenUsage{}
		if usage != (TokenUsage{}) {
			model := readAntigravitySelectedModel(logPath, streamModel, opts.Model)
			usageByModel[model] = usage
		}

		resCh <- Result{
			Status:     finalStatus,
			Output:     finalOutput,
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,
			Usage:      usageByModel,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

var antigravityConversationIDRe = regexp.MustCompile(
	`conversation=([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})`,
)

var antigravityPrintTimeoutRe = regexp.MustCompile(`Print mode: timed out after \d+ polls`)

var antigravityProviderErrorRe = regexp.MustCompile(`agent executor error:\s*(.+)`)

func antigravityPrintTimedOut(logPath string) bool {
	if logPath == "" {
		return false
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return false
	}
	return antigravityPrintTimeoutRe.Match(data)
}

func antigravityProviderError(logPath string) string {
	if logPath == "" {
		return ""
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}
	matches := antigravityProviderErrorRe.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return ""
	}
	return strings.TrimSpace(string(matches[len(matches)-1][1]))
}

func readAntigravityConversationID(logPath string) string {
	if logPath == "" {
		return ""
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}
	matches := antigravityConversationIDRe.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return ""
	}
	return string(matches[len(matches)-1][1])
}

var antigravityAppDataDirRe = regexp.MustCompile(`CLI app data directory:\s*(.+)`)

type antigravityTranscriptRecord struct {
	Type    string          `json:"type"`
	Source  string          `json:"source"`
	Status  string          `json:"status"`
	Content json.RawMessage `json:"content"`
}

func readAntigravityTranscriptOutput(logPath, conversationID string) string {
	if logPath == "" || conversationID == "" {
		return ""
	}
	appDataDir := readAntigravityAppDataDir(logPath)
	if appDataDir == "" {
		return ""
	}
	transcriptPath := filepath.Join(
		appDataDir, "brain", conversationID, ".system_generated", "logs", "transcript.jsonl",
	)
	f, err := os.Open(transcriptPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	var parts []string
	scanner := newAgentStreamScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec antigravityTranscriptRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.Type == "USER_INPUT" {
			parts = parts[:0]
			continue
		}
		if rec.Type != "PLANNER_RESPONSE" || rec.Source != "MODEL" || rec.Status != "DONE" {
			continue
		}
		var text string
		if err := json.Unmarshal(rec.Content, &text); err != nil {
			continue
		}
		if strings.TrimSpace(text) != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func readAntigravityAppDataDir(logPath string) string {
	if logPath == "" {
		return ""
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}
	m := antigravityAppDataDirRe.FindSubmatch(data)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(string(m[1]))
}

func buildAntigravityArgs(prompt, logPath string, timeout time.Duration, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"-p", prompt,
		"--dangerously-skip-permissions",
		"--output-format", "stream-json",
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	args = append(args, "--print-timeout", antigravityFormatTimeout(antigravityPrintTimeout(timeout)))
	args = append(args, "--log-file", logPath)
	if opts.ResumeSessionID != "" {
		args = append(args, "--conversation", opts.ResumeSessionID)
	}
	if opts.Cwd != "" {
		args = append(args, "--add-dir", filepath.Clean(opts.Cwd))
	}
	args = append(args, filterCustomArgs(opts.ExtraArgs, antigravityBlockedArgs, logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, antigravityBlockedArgs, logger)...)
	return args
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

const antigravityNoCapPrintTimeout = 24 * time.Hour

func antigravityPrintTimeout(timeout time.Duration) time.Duration {
	if timeout > 0 {
		return timeout
	}
	return antigravityNoCapPrintTimeout
}

func antigravityFormatTimeout(d time.Duration) string {
	if d < time.Second {
		d = time.Second
	}
	return d.String()
}
