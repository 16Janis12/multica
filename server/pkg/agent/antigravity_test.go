package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func quietAntigravityLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func fakeAntigravityACPScript() string {
	return `#!/bin/sh
if [ -n "$ANTIGRAVITY_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$ANTIGRAVITY_ARGS_FILE"
  done
fi
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"mcpCapabilities":{"http":true,"sse":true}}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      if [ -n "$ANTIGRAVITY_RECORD_SESSION_NEW" ]; then
        printf '%s\n' "$line" > "$ANTIGRAVITY_RECORD_SESSION_NEW"
      fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_fake_123","models":{"availableModels":[{"modelId":"gemini-3.8-flash-high","name":"Gemini 3.8 Flash (High)"}],"currentModelId":"gemini-3.8-flash-high"}}}\n' "$id"
      ;;
    *'"method":"session/resume"'*)
      if [ "$ANTIGRAVITY_REJECT_RESUME" = "1" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32002,"message":"Session not found in the current GEMINI_HOME"}}\n' "$id"
      else
        if [ -n "$ANTIGRAVITY_RECORD_SESSION_RESUME" ]; then
          printf '%s\n' "$line" > "$ANTIGRAVITY_RECORD_SESSION_RESUME"
        fi
        printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_fake_123"}}\n' "$id"
      fi
      ;;
    *'"method":"session/set_mode"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/set_model"'*)
      if echo "$line" | grep -q 'bogus-model'; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"Model not available: bogus-model"}}\n' "$id"
      else
        printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      fi
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_fake_123","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Done with task"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func fakeAntigravityACPScriptWithTools() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_tools_456"}}\n' "$id"
      ;;
    *'"method":"session/set_mode"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_tools_456","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Inspecting files..."}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_tools_456","update":{"sessionUpdate":"tool_call","toolCallId":"tc-1","title":"Running view_file","kind":"read","rawInput":{"AbsolutePath":"/test/file.txt"}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_tools_456","update":{"sessionUpdate":"tool_call_update","toolCallId":"tc-1","status":"completed"}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_tools_456","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Final answer after tool."}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func makeFakeAntigravityExecutable(t *testing.T, script string) string {
	t.Helper()
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "agy_acp_server")
	writeTestExecutable(t, binPath, []byte(script))
	return binPath
}

func TestAntigravityToolNameFromTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		title string
		kind  string
		want  string
	}{
		{"Running view_file", "read", "view_file"},
		{"Running run_command", "execute", "run_command"},
		{"Running write_to_file", "write", "write_to_file"},
		{"Running replace_file_content", "edit", "replace_file_content"},
		{"terminal: ls -la", "execute", "terminal"},
		{"read: /etc/hosts", "read", "read_file"},
		{"custom_tool", "other", "custom_tool"},
	}

	for _, tc := range tests {
		got := antigravityToolNameFromTitle(tc.title, tc.kind)
		if got != tc.want {
			t.Errorf("antigravityToolNameFromTitle(%q, %q) = %q, want %q", tc.title, tc.kind, got, tc.want)
		}
	}
}

func TestAntigravityACPLaunchArgs(t *testing.T) {
	t.Parallel()

	args := antigravityACPLaunchArgs()
	if runtime.GOOS == "linux" {
		if !slices.Equal(args, []string{"--uid="}) {
			t.Fatalf("antigravityACPLaunchArgs on linux = %v, want [--uid=]", args)
		}
	} else {
		if len(args) != 0 {
			t.Fatalf("antigravityACPLaunchArgs on %s = %v, want empty", runtime.GOOS, args)
		}
	}
}

func TestAntigravityDefaultExecutable(t *testing.T) {
	t.Parallel()

	if got := antigravityDefaultExecutable(); got != "agy_acp_server" {
		t.Errorf("antigravityDefaultExecutable = %q, want agy_acp_server", got)
	}
}

func TestAntigravityBlockedArgs(t *testing.T) {
	t.Parallel()

	customArgs := []string{"--debug", "--notices", "--help", "-h", "--helpfull"}
	filtered := filterCustomArgs(customArgs, antigravityBlockedArgs, quietAntigravityLogger())

	if slices.Contains(filtered, "--notices") || slices.Contains(filtered, "--help") || slices.Contains(filtered, "-h") || slices.Contains(filtered, "--helpfull") {
		t.Fatalf("filterCustomArgs did not strip blocked args: %v", filtered)
	}
	if !slices.Contains(filtered, "--debug") {
		t.Fatalf("filterCustomArgs unexpectedly stripped --debug: %v", filtered)
	}
}

func TestEnsureAntigravityHarnessEnv(t *testing.T) {
	t.Parallel()

	// Existing env with ANTIGRAVITY_HARNESS_PATH should be preserved
	existing := map[string]string{"ANTIGRAVITY_HARNESS_PATH": "/custom/path"}
	got := ensureAntigravityHarnessEnv(existing, "/usr/bin/agy_acp_server")
	if got["ANTIGRAVITY_HARNESS_PATH"] != "/custom/path" {
		t.Errorf("ensureAntigravityHarnessEnv did not preserve existing env: %v", got)
	}

	// When localharness_external is next to executable, inject ANTIGRAVITY_HARNESS_PATH
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "agy_acp_server")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	harnessPath := filepath.Join(tmpDir, "localharness_external")
	if err := os.WriteFile(harnessPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	empty := map[string]string{}
	injected := ensureAntigravityHarnessEnv(empty, binPath)
	if injected["ANTIGRAVITY_HARNESS_PATH"] != harnessPath {
		t.Errorf("ensureAntigravityHarnessEnv = %v, want %q", injected, harnessPath)
	}
}

func TestAntigravityModelError(t *testing.T) {
	t.Parallel()

	models := []Model{
		{ID: "gemini-3.8-flash-high", Label: "Gemini 3.8 Flash (High)"},
		{ID: "gemini-3.7-flash-high", Label: "Gemini 3.7 Flash (High)"},
	}

	if err := antigravityModelError("gemini-3.8-flash-high", models); err != nil {
		t.Errorf("antigravityModelError valid model returned err: %v", err)
	}
	if err := antigravityModelError("", models); err != nil {
		t.Errorf("antigravityModelError empty model returned err: %v", err)
	}
	if err := antigravityModelError("gemini-9.9", nil); err != nil {
		t.Errorf("antigravityModelError empty models slice returned err: %v", err)
	}
	if err := antigravityModelError("nonexistent", models); err == nil {
		t.Errorf("antigravityModelError invalid model returned nil, want error")
	}
}

func TestAntigravityExecuteSuccess(t *testing.T) {
	t.Parallel()

	binPath := makeFakeAntigravityExecutable(t, fakeAntigravityACPScript())
	backend := &antigravityBackend{
		cfg: Config{
			ExecutablePath: binPath,
			Logger:         quietAntigravityLogger(),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "Hello", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var messages []Message
	for msg := range session.Messages {
		messages = append(messages, msg)
	}

	res := <-session.Result
	if res.Status != "completed" {
		t.Fatalf("status = %q, error = %q, want completed", res.Status, res.Error)
	}
	if res.SessionID != "ses_fake_123" {
		t.Errorf("sessionID = %q, want ses_fake_123", res.SessionID)
	}
	if !strings.Contains(res.Output, "Done with task") {
		t.Errorf("output = %q, want 'Done with task'", res.Output)
	}
	if len(messages) == 0 {
		t.Errorf("expected streamed messages, got none")
	}
}

func TestAntigravityExecuteDeliverableWithTools(t *testing.T) {
	t.Parallel()

	binPath := makeFakeAntigravityExecutable(t, fakeAntigravityACPScriptWithTools())
	backend := &antigravityBackend{
		cfg: Config{
			ExecutablePath: binPath,
			Logger:         quietAntigravityLogger(),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "Run tool", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var toolSeen bool
	for msg := range session.Messages {
		if msg.Type == MessageToolUse && (msg.Tool == "read_file" || msg.Tool == "view_file") {
			toolSeen = true
		}
	}

	if !toolSeen {
		t.Errorf("expected read_file or view_file tool invocation in messages")
	}

	res := <-session.Result
	if res.Status != "completed" {
		t.Fatalf("status = %q, want completed", res.Status)
	}
	// Deliverable tracker isolates post-tool user-facing response
	if !strings.Contains(res.Output, "Final answer after tool.") {
		t.Errorf("output = %q, want post-tool final answer", res.Output)
	}
}

func TestAntigravityExecuteWithMcpServers(t *testing.T) {
	t.Parallel()

	recordFile := filepath.Join(t.TempDir(), "session_new.json")

	binPath := makeFakeAntigravityExecutable(t, fakeAntigravityACPScript())
	backend := &antigravityBackend{
		cfg: Config{
			ExecutablePath: binPath,
			Logger:         quietAntigravityLogger(),
			Env:            map[string]string{"ANTIGRAVITY_RECORD_SESSION_NEW": recordFile},
		},
	}

	mcpConfig := json.RawMessage(`{
		"mcpServers": {
			"test-server": {
				"command": "node",
				"args": ["server.js"]
			}
		}
	}`)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "Test with MCP", ExecOptions{McpConfig: mcpConfig})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	for range session.Messages {
	}
	res := <-session.Result
	if res.Status != "completed" {
		t.Fatalf("status = %q, want completed", res.Status)
	}

	data, err := os.ReadFile(recordFile)
	if err != nil {
		t.Fatalf("read session_new record: %v", err)
	}
	var req struct {
		Params struct {
			McpServers []struct {
				Name    string `json:"name"`
				Command string `json:"command"`
			} `json:"mcpServers"`
		} `json:"params"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		t.Fatalf("unmarshal session_new record: %v", err)
	}
	if len(req.Params.McpServers) != 1 || req.Params.McpServers[0].Name != "test-server" {
		t.Errorf("session/new McpServers = %+v, want test-server", req.Params.McpServers)
	}
}

func TestAntigravityExecuteSetModel(t *testing.T) {
	t.Parallel()

	binPath := makeFakeAntigravityExecutable(t, fakeAntigravityACPScript())
	backend := &antigravityBackend{
		cfg: Config{
			ExecutablePath: binPath,
			Logger:         quietAntigravityLogger(),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Setting a valid model succeeds
	session, err := backend.Execute(ctx, "Test model", ExecOptions{Model: "gemini-3.8-flash-high"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	for range session.Messages {
	}
	res := <-session.Result
	if res.Status != "completed" {
		t.Fatalf("status = %q, want completed", res.Status)
	}

	// Setting an invalid model fails cleanly
	sessionBad, err := backend.Execute(ctx, "Test bogus model", ExecOptions{Model: "bogus-model"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	for range sessionBad.Messages {
	}
	resBad := <-sessionBad.Result
	if resBad.Status != "failed" || !strings.Contains(resBad.Error, "bogus-model") {
		t.Fatalf("status = %q, error = %q, want failure on bogus-model", resBad.Status, resBad.Error)
	}
}

func TestAntigravityExecuteResumeSuccess(t *testing.T) {
	t.Parallel()

	recordFile := filepath.Join(t.TempDir(), "session_resume.json")

	binPath := makeFakeAntigravityExecutable(t, fakeAntigravityACPScript())
	backend := &antigravityBackend{
		cfg: Config{
			ExecutablePath: binPath,
			Logger:         quietAntigravityLogger(),
			Env:            map[string]string{"ANTIGRAVITY_RECORD_SESSION_RESUME": recordFile},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "Resume task", ExecOptions{ResumeSessionID: "ses_orig_999"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	for range session.Messages {
	}
	res := <-session.Result
	if res.Status != "completed" {
		t.Fatalf("status = %q, want completed", res.Status)
	}

	data, err := os.ReadFile(recordFile)
	if err != nil {
		t.Fatalf("read session_resume record: %v", err)
	}
	var req struct {
		Params struct {
			SessionID string `json:"sessionId"`
		} `json:"params"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		t.Fatalf("unmarshal session_resume record: %v", err)
	}
	if req.Params.SessionID != "ses_orig_999" {
		t.Errorf("sessionId in session/resume = %q, want ses_orig_999", req.Params.SessionID)
	}
}

func TestAntigravityExecuteResumeRejected(t *testing.T) {
	t.Parallel()

	binPath := makeFakeAntigravityExecutable(t, fakeAntigravityACPScript())
	backend := &antigravityBackend{
		cfg: Config{
			ExecutablePath: binPath,
			Logger:         quietAntigravityLogger(),
			Env:            map[string]string{"ANTIGRAVITY_REJECT_RESUME": "1"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "Resume task", ExecOptions{ResumeSessionID: "stale_session_id"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	for range session.Messages {
	}
	res := <-session.Result
	if !res.ResumeRejected {
		t.Errorf("res.ResumeRejected = false, want true when session is rejected")
	}
	if res.Status != "failed" {
		t.Errorf("status = %q, want failed", res.Status)
	}
}
