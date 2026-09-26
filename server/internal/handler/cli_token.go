package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type getDaemonCLITokenRequest struct {
	CLI         string `json:"cli"`
	TaskID      string `json:"task_id,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

type DaemonCLITokenResponse struct {
	CLI          string `json:"cli"`
	Token        string `json:"token"`
	InstanceURL  string `json:"instance_url,omitempty"`
	AccountLogin string `json:"account_login,omitempty"`
}

func mintGitHubInstallationAccessToken(ctx context.Context, installationID int64) (string, error) {
	appJWT, err := signGitHubAppJWT(time.Now())
	if err != nil {
		return "", err
	}
	if appJWT == "" {
		return "", errors.New("github App JWT credentials unavailable")
	}

	client := &http.Client{Timeout: 15 * time.Second}
	tokenEndpoint := fmt.Sprintf(
		"%s/app/installations/%d/access_tokens",
		strings.TrimRight(githubAPIBase, "/"),
		installationID,
	)
	tokenReq, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, nil)
	if err != nil {
		return "", err
	}
	setGitHubAPIHeaders(tokenReq, appJWT)
	tokenReq.Header.Set("Content-Type", "application/json")
	tokenResp, err := client.Do(tokenReq)
	if err != nil {
		return "", fmt.Errorf("create installation token: %w", err)
	}
	defer tokenResp.Body.Close()
	if tokenResp.StatusCode != http.StatusCreated {
		_, _ = io.Copy(io.Discard, io.LimitReader(tokenResp.Body, githubAPIResponseLimit))
		return "", fmt.Errorf("create installation token: github status %d", tokenResp.StatusCode)
	}
	var tokenBody struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(io.LimitReader(tokenResp.Body, githubAPIResponseLimit)).Decode(&tokenBody); err != nil {
		return "", fmt.Errorf("decode installation token: %w", err)
	}
	if tokenBody.Token == "" {
		return "", errors.New("github returned an empty installation token")
	}
	return tokenBody.Token, nil
}

// GetDaemonCLIToken returns a credentials token for setting up a CLI (gh, glab, tea, etc.)
// on a daemon or local client.
func (h *Handler) GetDaemonCLIToken(w http.ResponseWriter, r *http.Request) {
	var req getDaemonCLITokenRequest
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&req)
	}

	if taskIDParam := chi.URLParam(r, "id"); taskIDParam != "" {
		req.TaskID = taskIDParam
	}

	if strings.TrimSpace(req.CLI) == "" {
		writeError(w, http.StatusBadRequest, "cli is required")
		return
	}

	var workspaceID string
	if req.TaskID != "" {
		_, wsID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, req.TaskID)
		if !ok {
			return
		}
		workspaceID = wsID
	} else if req.WorkspaceID != "" {
		if !h.requireDaemonWorkspaceAccess(w, r, req.WorkspaceID) {
			return
		}
		workspaceID = req.WorkspaceID
	} else {
		wsID := middleware.DaemonWorkspaceIDFromContext(r.Context())
		if wsID == "" {
			writeError(w, http.StatusBadRequest, "task_id or workspace_id is required")
			return
		}
		workspaceID = wsID
	}

	wsUUID := parseUUID(workspaceID)
	cliNorm := strings.ToLower(strings.TrimSpace(req.CLI))
	cliNorm = strings.TrimSuffix(cliNorm, " cli")

	conns, err := h.Queries.ListVCSConnectionsByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query VCS connections")
		return
	}

	findConnection := func(match func(db.VcsConnection) bool) *db.VcsConnection {
		for i := range conns {
			if match(conns[i]) {
				return &conns[i]
			}
		}
		return nil
	}

	var resp DaemonCLITokenResponse
	resp.CLI = req.CLI

	switch cliNorm {
	case "github", "gh":
		// 1. Check vcs_connection for github
		if conn := findConnection(func(c db.VcsConnection) bool { return c.Provider == "github" }); conn != nil {
			token, err := h.openVCSSecret(conn.AccessTokenEncrypted)
			if err == nil && token != "" {
				resp.Token = token
				resp.InstanceURL = conn.InstanceUrl
				resp.AccountLogin = conn.AccountLogin
			}
		}
		// 2. Check GitHub App installation
		if resp.Token == "" {
			if installations, err := h.Queries.ListGitHubInstallationsByWorkspace(r.Context(), wsUUID); err == nil && len(installations) > 0 {
				token, err := mintGitHubInstallationAccessToken(r.Context(), installations[0].InstallationID)
				if err == nil && token != "" {
					resp.Token = token
					resp.InstanceURL = "https://github.com"
					resp.AccountLogin = installations[0].AccountLogin
				}
			}
		}
		// 3. Fallback to server env var
		if resp.Token == "" {
			if tok := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); tok != "" {
				resp.Token = tok
				resp.InstanceURL = "https://github.com"
			} else if tok := strings.TrimSpace(os.Getenv("GH_TOKEN")); tok != "" {
				resp.Token = tok
				resp.InstanceURL = "https://github.com"
			}
		}

	case "gitlab", "glab":
		// 1. Check vcs_connection for gitlab
		if conn := findConnection(func(c db.VcsConnection) bool { return c.Provider == "gitlab" }); conn != nil {
			token, err := h.openVCSSecret(conn.AccessTokenEncrypted)
			if err == nil && token != "" {
				resp.Token = token
				resp.InstanceURL = conn.InstanceUrl
				resp.AccountLogin = conn.AccountLogin
			}
		}
		// 2. Fallback to server env var
		if resp.Token == "" {
			if tok := strings.TrimSpace(os.Getenv("GITLAB_TOKEN")); tok != "" {
				resp.Token = tok
				resp.InstanceURL = "https://gitlab.com"
			} else if tok := strings.TrimSpace(os.Getenv("GLAB_TOKEN")); tok != "" {
				resp.Token = tok
				resp.InstanceURL = "https://gitlab.com"
			}
		}

	case "gitea", "tea", "forgejo":
		// 1. Check vcs_connection for gitea or forgejo
		if conn := findConnection(func(c db.VcsConnection) bool { return c.Provider == "gitea" || c.Provider == "forgejo" }); conn != nil {
			token, err := h.openVCSSecret(conn.AccessTokenEncrypted)
			if err == nil && token != "" {
				resp.Token = token
				resp.InstanceURL = conn.InstanceUrl
				resp.AccountLogin = conn.AccountLogin
			}
		}
		// 2. Fallback to server env var
		if resp.Token == "" {
			if tok := strings.TrimSpace(os.Getenv("GITEA_TOKEN")); tok != "" {
				resp.Token = tok
			} else if tok := strings.TrimSpace(os.Getenv("TEA_TOKEN")); tok != "" {
				resp.Token = tok
			}
		}

	default:
		// Generic lookup matching provider name
		if conn := findConnection(func(c db.VcsConnection) bool { return strings.EqualFold(c.Provider, cliNorm) }); conn != nil {
			token, err := h.openVCSSecret(conn.AccessTokenEncrypted)
			if err == nil && token != "" {
				resp.Token = token
				resp.InstanceURL = conn.InstanceUrl
				resp.AccountLogin = conn.AccountLogin
			}
		}
	}

	if resp.Token == "" {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no token found for CLI %s in workspace", req.CLI))
		return
	}

	writeJSON(w, http.StatusOK, resp)
}
