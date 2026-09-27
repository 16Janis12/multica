package runtimeusage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type claudeCredentialsFile struct {
	ClaudeAiOauth *struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresAt    int64  `json:"expiresAt"` // ms
	} `json:"claudeAiOauth"`
}

type claudeWindowRaw struct {
	Utilization *float64 `json:"utilization"` // 0 to 100 or 0.0 to 1.0
	ResetsAt    *string  `json:"resets_at"`   // ISO-8601 string
}

type claudeUsageResponseRaw struct {
	FiveHour *claudeWindowRaw `json:"five_hour"`
	SevenDay *claudeWindowRaw `json:"seven_day"`
	Limits   []struct {
		Kind     string   `json:"kind"`
		Percent  *float64 `json:"percent"`
		ResetsAt *string  `json:"resets_at"`
		Scope    *struct {
			Model *struct {
				DisplayName string `json:"display_name"`
			} `json:"model"`
		} `json:"scope"`
	} `json:"limits"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// ClaudeProber implements Prober for Claude Code.
type ClaudeProber struct {
	httpClient *http.Client
}

// NewClaudeProber creates a new ClaudeProber.
func NewClaudeProber() *ClaudeProber {
	return &ClaudeProber{
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *ClaudeProber) Provider() string {
	return "claude"
}

func (p *ClaudeProber) findTokenFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	candidates := []string{
		filepath.Join(home, ".claude", ".credentials.json"),
		filepath.Join(home, ".claude.json"),
		filepath.Join(home, ".config", "claude", "config.json"),
	}

	for _, path := range candidates {
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			return path
		}
	}
	return ""
}

func (p *ClaudeProber) refreshToken(ctx context.Context, refreshToken string) (string, error) {
	if refreshToken == "" {
		return "", fmt.Errorf("no refresh token available")
	}

	tokenURL := "https://api.anthropic.com/api/oauth/token"
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", "claude-code")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("claude token refresh failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("claude token refresh status %d: %s", resp.StatusCode, string(body))
	}

	var res struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", fmt.Errorf("decode refresh response: %w", err)
	}

	return res.AccessToken, nil
}

func (p *ClaudeProber) Probe(ctx context.Context, opts ProbeOptions) (*RuntimeUsageSnapshot, error) {
	tokenFile := p.findTokenFile()
	if tokenFile == "" {
		return nil, fmt.Errorf("claude credentials file not found")
	}

	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return nil, fmt.Errorf("read claude credentials file: %w", err)
	}

	var creds claudeCredentialsFile
	if err := json.Unmarshal(data, &creds); err != nil || creds.ClaudeAiOauth == nil {
		return nil, fmt.Errorf("invalid claude credentials format")
	}

	accessToken := creds.ClaudeAiOauth.AccessToken
	if accessToken == "" {
		return nil, fmt.Errorf("empty claude access token")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.anthropic.com/api/oauth/usage", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", "claude-code")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query claude usage: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return &RuntimeUsageSnapshot{
			Provider:      "claude",
			EffectiveTier: CapacityExhausted,
			CheckedAt:     time.Now().UTC(),
			Error:         "Rate limit reached (HTTP 429)",
		}, nil
	}

	if resp.StatusCode == http.StatusUnauthorized && creds.ClaudeAiOauth.RefreshToken != "" {
		newTok, refreshErr := p.refreshToken(ctx, creds.ClaudeAiOauth.RefreshToken)
		if refreshErr == nil && newTok != "" {
			req.Header.Set("Authorization", "Bearer "+newTok)
			resp2, err2 := p.httpClient.Do(req)
			if err2 == nil {
				defer resp2.Body.Close()
				resp = resp2
			}
		}
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("claude usage endpoint returned status %d: %s", resp.StatusCode, string(body))
	}

	var usageResp claudeUsageResponseRaw
	if err := json.NewDecoder(resp.Body).Decode(&usageResp); err != nil {
		return nil, fmt.Errorf("decode claude usage response: %w", err)
	}

	snapshot := &RuntimeUsageSnapshot{
		Provider:     "claude",
		ModelBuckets: make(map[string]WindowMetrics),
		CheckedAt:    time.Now().UTC(),
	}

	normalizeFraction := func(val float64) float64 {
		// Anthropic utilization might be represented as 0.0-1.0 or 0-100
		if val > 1.0 {
			return (100.0 - val) / 100.0
		}
		return 1.0 - val
	}

	if usageResp.FiveHour != nil && usageResp.FiveHour.Utilization != nil {
		var resetsAt *time.Time
		if usageResp.FiveHour.ResetsAt != nil && *usageResp.FiveHour.ResetsAt != "" {
			if parsed, err := time.Parse(time.RFC3339, *usageResp.FiveHour.ResetsAt); err == nil {
				resetsAt = &parsed
			}
		}
		remFrac := normalizeFraction(*usageResp.FiveHour.Utilization)
		m := NewWindowMetrics("claude-session", "5-Hour Session", remFrac, resetsAt)
		snapshot.Session5h = &m
	}

	if usageResp.SevenDay != nil && usageResp.SevenDay.Utilization != nil {
		var resetsAt *time.Time
		if usageResp.SevenDay.ResetsAt != nil && *usageResp.SevenDay.ResetsAt != "" {
			if parsed, err := time.Parse(time.RFC3339, *usageResp.SevenDay.ResetsAt); err == nil {
				resetsAt = &parsed
			}
		}
		remFrac := normalizeFraction(*usageResp.SevenDay.Utilization)
		m := NewWindowMetrics("claude-weekly", "Weekly Limit", remFrac, resetsAt)
		snapshot.Weekly7d = &m
	}

	for _, limit := range usageResp.Limits {
		if limit.Scope != nil && limit.Scope.Model != nil && limit.Percent != nil {
			var resetsAt *time.Time
			if limit.ResetsAt != nil && *limit.ResetsAt != "" {
				if parsed, err := time.Parse(time.RFC3339, *limit.ResetsAt); err == nil {
					resetsAt = &parsed
				}
			}
			remFrac := normalizeFraction(*limit.Percent)
			m := NewWindowMetrics(limit.Scope.Model.DisplayName, limit.Scope.Model.DisplayName, remFrac, resetsAt)
			snapshot.ModelBuckets[limit.Scope.Model.DisplayName] = m
		}
	}

	snapshot.ComputeEffectiveTier()
	return snapshot, nil
}
