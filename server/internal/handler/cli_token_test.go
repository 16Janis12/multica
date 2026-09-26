package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestGetDaemonCLIToken_Validation(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	t.Run("missing cli", func(t *testing.T) {
		req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/cli-token", map[string]any{
			"workspace_id": testWorkspaceID,
		}, testWorkspaceID, "test-daemon")
		w := httptest.NewRecorder()
		testHandler.GetDaemonCLIToken(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for missing cli, got %d", w.Code)
		}
	})

	t.Run("missing workspace and task without daemon context", func(t *testing.T) {
		req := newRequest(http.MethodPost, "/api/daemon/cli-token", map[string]any{
			"cli": "github",
		})
		w := httptest.NewRecorder()
		testHandler.GetDaemonCLIToken(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 when workspace_id is absent and context empty, got %d", w.Code)
		}
	})
}

func TestGetDaemonCLIToken_VCSConnection(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	box := withVCSBox(t)
	t.Cleanup(func() { cleanupVCS(context.Background(), "") })

	// Seed connections for github, gitlab, gitea
	ghToken := "ghp_mock_token_123456"
	glToken := "glpat_mock_token_654321"
	giteaToken := "tea_mock_token_789012"

	_, err := testHandler.Queries.UpsertVCSConnection(ctx, db.UpsertVCSConnectionParams{
		WorkspaceID:            parseUUID(testWorkspaceID),
		Provider:               "github",
		InstanceUrl:            "https://github.com",
		AccountLogin:           "gh-bot",
		AccessTokenEncrypted:   sealTestSecret(t, box, ghToken),
		WebhookSecretEncrypted: "",
		ConnectedByID:          pgtype.UUID{},
	})
	if err != nil {
		t.Fatalf("failed to seed github vcs connection: %v", err)
	}

	_, err = testHandler.Queries.UpsertVCSConnection(ctx, db.UpsertVCSConnectionParams{
		WorkspaceID:            parseUUID(testWorkspaceID),
		Provider:               "gitlab",
		InstanceUrl:            "https://gitlab.example.com",
		AccountLogin:           "gitlab-bot",
		AccessTokenEncrypted:   sealTestSecret(t, box, glToken),
		WebhookSecretEncrypted: "",
		ConnectedByID:          pgtype.UUID{},
	})
	if err != nil {
		t.Fatalf("failed to seed gitlab vcs connection: %v", err)
	}

	_, err = testHandler.Queries.UpsertVCSConnection(ctx, db.UpsertVCSConnectionParams{
		WorkspaceID:            parseUUID(testWorkspaceID),
		Provider:               "gitea",
		InstanceUrl:            "https://gitea.example.com",
		AccountLogin:           "gitea-bot",
		AccessTokenEncrypted:   sealTestSecret(t, box, giteaToken),
		WebhookSecretEncrypted: "",
		ConnectedByID:          pgtype.UUID{},
	})
	if err != nil {
		t.Fatalf("failed to seed gitea vcs connection: %v", err)
	}

	// 1. Fetch GitHub token
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/cli-token", map[string]any{
		"cli":          "github",
		"workspace_id": testWorkspaceID,
	}, testWorkspaceID, "test-daemon")
	w := httptest.NewRecorder()
	testHandler.GetDaemonCLIToken(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp DaemonCLITokenResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Token != ghToken {
		t.Fatalf("expected token %q, got %q", ghToken, resp.Token)
	}
	if resp.AccountLogin != "gh-bot" {
		t.Fatalf("expected account_login gh-bot, got %q", resp.AccountLogin)
	}

	// 2. Fetch using alias 'gh'
	reqAlias := newDaemonTokenRequest(http.MethodPost, "/api/daemon/cli-token", map[string]any{
		"cli":          "gh",
		"workspace_id": testWorkspaceID,
	}, testWorkspaceID, "test-daemon")
	wAlias := httptest.NewRecorder()
	testHandler.GetDaemonCLIToken(wAlias, reqAlias)
	if wAlias.Code != http.StatusOK {
		t.Fatalf("expected 200 for 'gh', got %d: %s", wAlias.Code, wAlias.Body.String())
	}

	// 3. Fetch GitLab token using 'glab'
	reqGitLab := newDaemonTokenRequest(http.MethodPost, "/api/daemon/cli-token", map[string]any{
		"cli":          "glab",
		"workspace_id": testWorkspaceID,
	}, testWorkspaceID, "test-daemon")
	wGitLab := httptest.NewRecorder()
	testHandler.GetDaemonCLIToken(wGitLab, reqGitLab)
	if wGitLab.Code != http.StatusOK {
		t.Fatalf("expected 200 for 'glab', got %d: %s", wGitLab.Code, wGitLab.Body.String())
	}
	var glResp DaemonCLITokenResponse
	if err := json.Unmarshal(wGitLab.Body.Bytes(), &glResp); err != nil {
		t.Fatalf("decode gitlab resp: %v", err)
	}
	if glResp.Token != glToken || glResp.InstanceURL != "https://gitlab.example.com" {
		t.Fatalf("unexpected gitlab response: %+v", glResp)
	}

	// 4. Fetch Gitea token using 'tea'
	reqGitea := newDaemonTokenRequest(http.MethodPost, "/api/daemon/cli-token", map[string]any{
		"cli":          "tea",
		"workspace_id": testWorkspaceID,
	}, testWorkspaceID, "test-daemon")
	wGitea := httptest.NewRecorder()
	testHandler.GetDaemonCLIToken(wGitea, reqGitea)
	if wGitea.Code != http.StatusOK {
		t.Fatalf("expected 200 for 'tea', got %d: %s", wGitea.Code, wGitea.Body.String())
	}
	var teaResp DaemonCLITokenResponse
	if err := json.Unmarshal(wGitea.Body.Bytes(), &teaResp); err != nil {
		t.Fatalf("decode gitea resp: %v", err)
	}
	if teaResp.Token != giteaToken || teaResp.InstanceURL != "https://gitea.example.com" {
		t.Fatalf("unexpected gitea response: %+v", teaResp)
	}

	// 5. Unknown CLI / no connection
	reqUnknown := newDaemonTokenRequest(http.MethodPost, "/api/daemon/cli-token", map[string]any{
		"cli":          "unknown-vcs",
		"workspace_id": testWorkspaceID,
	}, testWorkspaceID, "test-daemon")
	wUnknown := httptest.NewRecorder()
	testHandler.GetDaemonCLIToken(wUnknown, reqUnknown)
	if wUnknown.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown CLI, got %d", wUnknown.Code)
	}
}

func TestGetDaemonCLIToken_EnvFallback(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	t.Cleanup(func() { cleanupVCS(context.Background(), "") })

	t.Setenv("GITHUB_TOKEN", "ghp_env_fallback_token")
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/cli-token", map[string]any{
		"cli":          "github",
		"workspace_id": testWorkspaceID,
	}, testWorkspaceID, "test-daemon")
	w := httptest.NewRecorder()
	testHandler.GetDaemonCLIToken(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 via env fallback, got %d: %s", w.Code, w.Body.String())
	}
	var resp DaemonCLITokenResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Token != "ghp_env_fallback_token" {
		t.Fatalf("expected token %q, got %q", "ghp_env_fallback_token", resp.Token)
	}
}

func sealTestSecret(t *testing.T, box *secretbox.Box, plain string) string {
	t.Helper()
	sealed, err := box.Seal([]byte(plain))
	if err != nil {
		t.Fatalf("seal secret: %v", err)
	}
	return base64.StdEncoding.EncodeToString(sealed)
}
