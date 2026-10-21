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
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

var antigravityQuotaEndpoints = []string{
	"https://daily-cloudcode-pa.googleapis.com",
	"https://cloudcode-pa.googleapis.com",
}

type antigravityCredentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
	AccessToken  string `json:"access_token"`
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
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *AntigravityProber) Provider() string {
	return "antigravity"
}

// getAntigravityUserAgents formats the desktop client user agents required by CloudCode PA.
// The upstream backend filters the "Gemini Models" group based on the client User-Agent;
// using the desktop format ensures both Gemini and Claude/GPT quota buckets are returned.
func getAntigravityUserAgents() []string {
	sys := strings.ToLower(runtime.GOOS)
	arch := strings.ToLower(runtime.GOARCH)
	if arch == "amd64" {
		arch = "x86_64"
	}
	return []string{
		fmt.Sprintf("antigravity/2.23.0 (%s; %s)", sys, arch),
		fmt.Sprintf("antigravity/hub/1.0.0 (aidev_client; os_type=%s; arch=%s; cl=963137146)", sys, arch),
	}
}

func (p *AntigravityProber) findTokenFile() string {
	if envPath := os.Getenv("ANTIGRAVITY_TOKEN_PATH"); envPath != "" {
		if fi, err := os.Stat(envPath); err == nil && !fi.IsDir() {
			return envPath
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	var candidates []string
	specific := []string{
		filepath.Join(home, ".gemini", "antigravity-acp", "acp_token.json"),
		filepath.Join(home, ".gemini", "antigravity", "acp_token.json"),
		filepath.Join(home, ".gemini", "acp_token.json"),
		"/root/.gemini/antigravity-acp/acp_token.json",
		filepath.Join(home, ".config", "antigravity", "acp_token.json"),
		filepath.Join(home, ".antigravity", "acp_token.json"),
	}

	for _, cand := range specific {
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			candidates = append(candidates, cand)
		}
	}

	// Recursive walk across ~/.t3/userdata/providers/antigravity
	t3Roots := []string{
		filepath.Join(home, ".t3", "userdata", "providers", "antigravity"),
	}
	if home != "/root" {
		t3Roots = append(t3Roots, "/root/.t3/userdata/providers/antigravity")
	}

	for _, root := range t3Roots {
		if fi, err := os.Stat(root); err == nil && fi.IsDir() {
			_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				if !info.IsDir() && info.Name() == "acp_token.json" {
					candidates = append(candidates, path)
				}
				return nil
			})
		}
	}

	if len(candidates) > 0 {
		// Sort by modification time descending so the freshest active token is preferred
		sort.Slice(candidates, func(i, j int) bool {
			fiI, errI := os.Stat(candidates[i])
			fiJ, errJ := os.Stat(candidates[j])
			if errI != nil || errJ != nil {
				return false
			}
			return fiI.ModTime().After(fiJ.ModTime())
		})
		return candidates[0]
	}

	return ""
}

// getTokenFromKeyring attempts to retrieve token from Linux Secret Service if acp_token.json is missing.
func (p *AntigravityProber) getTokenFromKeyring(ctx context.Context) *antigravityCredentials {
	secretTool, err := exec.LookPath("secret-tool")
	if err != nil || secretTool == "" {
		return nil
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, secretTool, "lookup", "service", "gemini", "username", "antigravity")
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return nil
	}

	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil
	}

	var data struct {
		Token struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		} `json:"token"`
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil
	}

	if data.Token.AccessToken == "" && data.Token.RefreshToken == "" {
		return nil
	}

	return &antigravityCredentials{
		ClientID:     "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com",
		AccessToken:  data.Token.AccessToken,
		RefreshToken: data.Token.RefreshToken,
	}
}

// loadCredentials loads credentials from standard token file paths or the system keyring.
func (p *AntigravityProber) loadCredentials(ctx context.Context) (*antigravityCredentials, error) {
	tokenFile := p.findTokenFile()
	if tokenFile != "" {
		data, err := os.ReadFile(tokenFile)
		if err == nil {
			var creds antigravityCredentials
			if err := json.Unmarshal(data, &creds); err == nil {
				return &creds, nil
			}
		}
	}

	if creds := p.getTokenFromKeyring(ctx); creds != nil {
		return creds, nil
	}

	return nil, fmt.Errorf("antigravity credentials not found across token files or keyring")
}

func (p *AntigravityProber) refreshAccessToken(ctx context.Context, creds antigravityCredentials) (string, error) {
	if creds.RefreshToken == "" {
		if creds.AccessToken != "" {
			return creds.AccessToken, nil
		}
		return "", fmt.Errorf("no refresh_token or access_token found in credentials")
	}

	tokenURL := creds.TokenURI
	if tokenURL == "" {
		tokenURL = "https://oauth2.googleapis.com/token"
	}

	form := url.Values{}
	if creds.ClientID != "" {
		form.Set("client_id", creds.ClientID)
	}
	if creds.ClientSecret != "" {
		form.Set("client_secret", creds.ClientSecret)
	}
	form.Set("refresh_token", creds.RefreshToken)
	form.Set("grant_type", "refresh_token")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		if creds.AccessToken != "" {
			return creds.AccessToken, nil
		}
		return "", fmt.Errorf("oauth token refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if creds.AccessToken != "" {
			return creds.AccessToken, nil
		}
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

	userAgents := getAntigravityUserAgents()
	var lastErr error
	var fallback *antigravityQuotaResponseRaw

	for _, ua := range userAgents {
		for _, endpoint := range antigravityQuotaEndpoints {
			reqURL := endpoint
			if !strings.Contains(reqURL, "retrieveUserQuotaSummary") {
				reqURL = strings.TrimRight(endpoint, "/") + "/v1internal:retrieveUserQuotaSummary"
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(jsonBytes))
			if err != nil {
				lastErr = err
				continue
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+accessToken)
			req.Header.Set("User-Agent", ua)

			resp, err := p.httpClient.Do(req)
			if err != nil {
				lastErr = err
				continue
			}

			if resp.StatusCode == http.StatusOK {
				var raw antigravityQuotaResponseRaw
				if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
					resp.Body.Close()
					lastErr = fmt.Errorf("decode quota summary response: %w", err)
					continue
				}
				resp.Body.Close()

				if len(raw.Groups) > 0 {
					hasGemini := false
					for _, g := range raw.Groups {
						if strings.Contains(strings.ToLower(g.DisplayName), "gemini") {
							hasGemini = true
							break
						}
					}
					if hasGemini {
						return &raw, nil
					}
					if fallback == nil {
						fallback = &raw
					}
				}
				continue
			}

			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("quota endpoint %s status %d: %s", reqURL, resp.StatusCode, string(body))
		}
	}

	if fallback != nil {
		return fallback, nil
	}

	return nil, lastErr
}

func (p *AntigravityProber) Probe(ctx context.Context, opts ProbeOptions) (*RuntimeUsageSnapshot, error) {
	creds, err := p.loadCredentials(ctx)
	if err != nil {
		return nil, err
	}

	accessToken, err := p.refreshAccessToken(ctx, *creds)
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
				if b.Window == "5h" || strings.Contains(b.BucketId, "5h") {
					m := metrics
					snapshot.Session5h = &m
				} else if b.Window == "weekly" || strings.Contains(b.BucketId, "weekly") || strings.Contains(b.BucketId, "7d") {
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
			if snapshot.Weekly7d == nil && (strings.Contains(b.ID, "weekly") || strings.Contains(b.ID, "7d")) {
				m := b
				snapshot.Weekly7d = &m
			}
		}
	}

	snapshot.ComputeEffectiveTier()
	return snapshot, nil
}
