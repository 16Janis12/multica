package runtimeusage

import (
	"bytes"
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

var antigravityQuotaEndpoints = []string{
	"https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary",
	"https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:retrieveUserQuotaSummary",
}

type antigravityCredentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
	TokenURI     string `json:"token_uri"`
	ProjectID    string `json:"project_id"`
}

type antigravityTokenRefreshResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

type antigravityBucketRaw struct {
	BucketId          string   `json:"bucketId"`
	DisplayName       string   `json:"displayName"`
	Window            string   `json:"window"`
	ResetTime         string   `json:"resetTime"`
	Description       string   `json:"description"`
	RemainingFraction *float64 `json:"remainingFraction"`
}

type antigravityGroupRaw struct {
	DisplayName string                 `json:"displayName"`
	Description string                 `json:"description"`
	Buckets     []antigravityBucketRaw `json:"buckets"`
}

type antigravityQuotaResponseRaw struct {
	Groups      []antigravityGroupRaw `json:"groups"`
	Description string                `json:"description"`
}

// AntigravityProber implements Prober for Google Antigravity.
type AntigravityProber struct {
	httpClient *http.Client
}

// NewAntigravityProber creates an AntigravityProber.
func NewAntigravityProber() *AntigravityProber {
	return &AntigravityProber{
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *AntigravityProber) Provider() string {
	return "antigravity"
}

func (p *AntigravityProber) findTokenFile() string {
	if envPath := os.Getenv("ANTIGRAVITY_TOKEN_PATH"); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	patterns := []string{
		filepath.Join(home, ".gemini", "antigravity-acp", "acp_token.json"),
		filepath.Join(home, ".gemini", "*", "acp_token.json"),
		filepath.Join(home, ".gemini", "acp_token.json"),
		"/root/.gemini/antigravity-acp/acp_token.json",
		filepath.Join(home, ".t3", "userdata", "providers", "antigravity", "*", "antigravity-acp", "acp_token.json"),
		filepath.Join(home, ".config", "antigravity", "acp_token.json"),
		filepath.Join(home, ".antigravity", "acp_token.json"),
	}

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err == nil && len(matches) > 0 {
			return matches[0]
		}
	}
	return ""
}

func (p *AntigravityProber) refreshAccessToken(ctx context.Context, creds antigravityCredentials) (string, error) {
	tokenURL := creds.TokenURI
	if tokenURL == "" {
		tokenURL = "https://oauth2.googleapis.com/token"
	}

	form := url.Values{}
	form.Set("client_id", creds.ClientID)
	form.Set("client_secret", creds.ClientSecret)
	form.Set("refresh_token", creds.RefreshToken)
	form.Set("grant_type", "refresh_token")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("oauth token refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("oauth token refresh returned status %d: %s", resp.StatusCode, string(body))
	}

	var refreshResp antigravityTokenRefreshResponse
	if err := json.NewDecoder(resp.Body).Decode(&refreshResp); err != nil {
		return "", fmt.Errorf("decode token refresh response: %w", err)
	}

	return refreshResp.AccessToken, nil
}

func (p *AntigravityProber) fetchQuotaSummary(ctx context.Context, accessToken, projectID string) (*antigravityQuotaResponseRaw, error) {
	reqBody := map[string]string{}
	if projectID != "" {
		reqBody["project"] = projectID
	}
	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, endpoint := range antigravityQuotaEndpoints {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonBytes))
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("User-Agent", "antigravity")

		resp, err := p.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			var raw antigravityQuotaResponseRaw
			if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
				return nil, fmt.Errorf("decode quota summary response: %w", err)
			}
			return &raw, nil
		}

		body, _ := io.ReadAll(resp.Body)
		lastErr = fmt.Errorf("quota endpoint %s status %d: %s", endpoint, resp.StatusCode, string(body))
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode < 500 {
			break
		}
	}

	return nil, lastErr
}

func (p *AntigravityProber) Probe(ctx context.Context, opts ProbeOptions) (*RuntimeUsageSnapshot, error) {
	tokenFile := p.findTokenFile()
	if tokenFile == "" {
		return nil, fmt.Errorf("antigravity credentials not found")
	}

	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return nil, fmt.Errorf("read token file %s: %w", tokenFile, err)
	}

	var creds antigravityCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parse token file: %w", err)
	}

	accessToken, err := p.refreshAccessToken(ctx, creds)
	if err != nil {
		return nil, fmt.Errorf("refresh token: %w", err)
	}

	raw, err := p.fetchQuotaSummary(ctx, accessToken, creds.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("fetch quota summary: %w", err)
	}

	snapshot := &RuntimeUsageSnapshot{
		Provider:     "antigravity",
		ModelBuckets: make(map[string]WindowMetrics),
		CheckedAt:    time.Now().UTC(),
	}

	// Model selection heuristic: Gemini models vs Claude/GPT models in Antigravity
	isThirdParty := strings.Contains(strings.ToLower(opts.Model), "claude") ||
		strings.Contains(strings.ToLower(opts.Model), "gpt") ||
		strings.Contains(strings.ToLower(opts.Model), "3p")

	for _, group := range raw.Groups {
		groupLower := strings.ToLower(group.DisplayName)
		groupMatches := (!isThirdParty && strings.Contains(groupLower, "gemini")) ||
			(isThirdParty && (strings.Contains(groupLower, "claude") || strings.Contains(groupLower, "gpt")))

		for _, b := range group.Buckets {
			remFrac := 1.0
			if b.RemainingFraction != nil {
				remFrac = *b.RemainingFraction
			}

			var resetsAt *time.Time
			if b.ResetTime != "" {
				if parsed, err := time.Parse(time.RFC3339, b.ResetTime); err == nil {
					resetsAt = &parsed
				}
			}

			metrics := NewWindowMetrics(b.BucketId, b.DisplayName, remFrac, resetsAt)
			snapshot.ModelBuckets[b.BucketId] = metrics

			if groupMatches {
				if b.Window == "5h" {
					m := metrics
					snapshot.Session5h = &m
				} else if b.Window == "weekly" {
					m := metrics
					snapshot.Weekly7d = &m
				}
			}
		}
	}

	// Fallback to first available buckets if group matching did not set session/weekly
	if snapshot.Session5h == nil || snapshot.Weekly7d == nil {
		for _, b := range snapshot.ModelBuckets {
			if snapshot.Session5h == nil && strings.Contains(b.ID, "5h") {
				m := b
				snapshot.Session5h = &m
			}
			if snapshot.Weekly7d == nil && strings.Contains(b.ID, "weekly") {
				m := b
				snapshot.Weekly7d = &m
			}
		}
	}

	snapshot.ComputeEffectiveTier()
	return snapshot, nil
}
