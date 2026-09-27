package runtimeusage

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

type codexRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type codexWindowRaw struct {
	UsedPercent        float64 `json:"usedPercent"`
	WindowDurationMins int     `json:"windowDurationMins"`
	ResetsAt           *int64  `json:"resetsAt"` // Epoch seconds
}

type codexRateLimitsPayload struct {
	RateLimits *struct {
		LimitID   *string         `json:"limitId"`
		PlanType  *string         `json:"planType"`
		Primary   *codexWindowRaw `json:"primary"`
		Secondary *codexWindowRaw `json:"secondary"`
	} `json:"rateLimits"`
	RateLimitResetCredits *struct {
		AvailableCount int64 `json:"availableCount"`
	} `json:"rateLimitResetCredits"`
}

type codexRPCResponse struct {
	JSONRPC string                  `json:"jsonrpc"`
	ID      int                     `json:"id"`
	Result  *codexRateLimitsPayload `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// CodexProber implements Prober for OpenAI Codex.
type CodexProber struct {
	executablePath string
}

// NewCodexProber creates a CodexProber.
func NewCodexProber(executablePath string) *CodexProber {
	if executablePath == "" {
		executablePath = "codex"
	}
	return &CodexProber{
		executablePath: executablePath,
	}
}

func (p *CodexProber) Provider() string {
	return "codex"
}

func (p *CodexProber) Probe(ctx context.Context, opts ProbeOptions) (*RuntimeUsageSnapshot, error) {
	execPath, err := exec.LookPath(p.executablePath)
	if err != nil {
		return nil, fmt.Errorf("codex executable not found: %w", err)
	}

	cmd := exec.CommandContext(ctx, execPath, "app-server", "--listen", "stdio://")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open stdout pipe: %w", err)
	}
	defer stdin.Close()
	defer stdout.Close()

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start codex app-server: %w", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	req := codexRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "account/rateLimits/read",
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := stdin.Write(append(reqData, '\n')); err != nil {
		return nil, fmt.Errorf("write rpc request: %w", err)
	}

	reader := bufio.NewReader(stdout)
	lineChan := make(chan []byte, 1)
	errChan := make(chan error, 1)

	go func() {
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				if err != io.EOF {
					errChan <- err
				}
				return
			}
			trimmed := strings.TrimSpace(string(line))
			if strings.HasPrefix(trimmed, "{") {
				lineChan <- line
				return
			}
		}
	}()

	var respBytes []byte
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-errChan:
		return nil, fmt.Errorf("read codex response: %w", err)
	case respBytes = <-lineChan:
	}

	var rpcResp codexRPCResponse
	if err := json.Unmarshal(respBytes, &rpcResp); err != nil {
		return nil, fmt.Errorf("decode codex response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("codex rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	if rpcResp.Result == nil || rpcResp.Result.RateLimits == nil {
		return nil, fmt.Errorf("empty rate limits payload from codex")
	}

	rl := rpcResp.Result.RateLimits
	snapshot := &RuntimeUsageSnapshot{
		Provider:  "codex",
		CheckedAt: time.Now().UTC(),
	}

	if rpcResp.Result.RateLimitResetCredits != nil {
		snapshot.ResetCredits = &rpcResp.Result.RateLimitResetCredits.AvailableCount
	}

	if rl.Primary != nil {
		var resetsAt *time.Time
		if rl.Primary.ResetsAt != nil {
			t := time.Unix(*rl.Primary.ResetsAt, 0).UTC()
			resetsAt = &t
		}
		remFrac := (100.0 - rl.Primary.UsedPercent) / 100.0
		m := NewWindowMetrics("codex-session", "5-Hour Session", remFrac, resetsAt)
		snapshot.Session5h = &m
	}

	if rl.Secondary != nil {
		var resetsAt *time.Time
		if rl.Secondary.ResetsAt != nil {
			t := time.Unix(*rl.Secondary.ResetsAt, 0).UTC()
			resetsAt = &t
		}
		remFrac := (100.0 - rl.Secondary.UsedPercent) / 100.0
		m := NewWindowMetrics("codex-weekly", "Weekly Limit", remFrac, resetsAt)
		snapshot.Weekly7d = &m
	}

	snapshot.ComputeEffectiveTier()
	return snapshot, nil
}
