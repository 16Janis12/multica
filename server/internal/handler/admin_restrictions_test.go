package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type mockAdminDB struct {
	user         db.User
	userErr      error
	settingValue []byte
	settingErr   error
	totalUsers   int64
	hasInvite    bool
}

func (m *mockAdminDB) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag(""), nil
}

func (m *mockAdminDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}

func (m *mockAdminDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "FROM system_settings") {
		if m.settingErr != nil {
			return &mockAdminRow{err: m.settingErr}
		}
		return &mockAdminRow{setting: db.SystemSetting{Value: m.settingValue}}
	}
	if strings.Contains(sql, "FROM \"user\"") && strings.Contains(sql, "WHERE id = $1") {
		return &mockAdminRow{user: m.user, err: m.userErr}
	}
	if strings.Contains(sql, "SELECT count(*) FROM \"user\"") {
		return &mockAdminRow{count: m.totalUsers}
	}
	if strings.Contains(sql, "FROM instance_invitation") {
		return &mockAdminRow{boolVal: m.hasInvite}
	}
	return &mockAdminRow{}
}

type mockAdminRow struct {
	user    db.User
	setting db.SystemSetting
	count   int64
	boolVal bool
	err     error
}

func (r *mockAdminRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) == 1 {
		switch ptr := dest[0].(type) {
		case *int64:
			*ptr = r.count
			return nil
		case *bool:
			*ptr = r.boolVal
			return nil
		}
	}
	if len(dest) == 5 { // system_settings
		*(dest[0].(*string)) = r.setting.Key
		*(dest[1].(*[]byte)) = r.setting.Value
		return nil
	}
	if len(dest) >= 18 { // user
		*(dest[0].(*pgtype.UUID)) = r.user.ID
		*(dest[1].(*string)) = r.user.Name
		*(dest[2].(*string)) = r.user.Email
		*(dest[14].(*string)) = r.user.SystemRole
		*(dest[15].(*bool)) = r.user.IsSuspended
		return nil
	}
	return nil
}

func TestWorkspaceCreationRestrictedToAdminsOnly(t *testing.T) {
	stdUserID := "00000000-0000-0000-0000-000000000002"
	stdUserUUID := util.MustParseUUID(stdUserID)

	adminUserID := "00000000-0000-0000-0000-000000000001"
	adminUserUUID := util.MustParseUUID(adminUserID)

	adminsOnlySetting, _ := json.Marshal(map[string]string{"policy": "admins_only"})

	t.Run("standard user cannot create workspace when policy is admins_only", func(t *testing.T) {
		h := &Handler{
			Queries: db.New(&mockAdminDB{
				settingValue: adminsOnlySetting,
				user: db.User{
					ID:         stdUserUUID,
					SystemRole: "standard",
				},
			}),
		}

		body := `{"name":"Forbidden WS","slug":"forbidden-ws"}`
		req := httptest.NewRequest(http.MethodPost, "/api/workspaces", strings.NewReader(body))
		req.Header.Set("X-User-ID", stdUserID)
		rec := httptest.NewRecorder()

		h.CreateWorkspace(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d (body: %s)", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "only instance administrators") {
			t.Fatalf("expected admin restriction message, got %s", rec.Body.String())
		}
	})

	t.Run("instance admin is permitted past the creation policy gate", func(t *testing.T) {
		h := &Handler{
			Queries: db.New(&mockAdminDB{
				settingValue: adminsOnlySetting,
				user: db.User{
					ID:         adminUserUUID,
					SystemRole: "instance_admin",
				},
			}),
		}

		// Provide invalid body so it passes policy gate and fails later on body parsing/slug validation
		body := `{"name":"","slug":""}`
		req := httptest.NewRequest(http.MethodPost, "/api/workspaces", strings.NewReader(body))
		req.Header.Set("X-User-ID", adminUserID)
		rec := httptest.NewRecorder()

		h.CreateWorkspace(rec, req)

		// It should NOT be 403 Forbidden! It should reach body validation (400)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected admin to pass policy gate to validation (400), got %d (body: %s)", rec.Code, rec.Body.String())
		}
	})
}

func TestStrictInviteOnlySignupPolicy(t *testing.T) {
	inviteOnlySetting, _ := json.Marshal(map[string]any{"policy": "invite_only", "allowed_domains": []string{}})

	t.Run("registration without invite rejected in invite-only mode", func(t *testing.T) {
		h := &Handler{
			cfg: Config{AllowSignup: false},
			Queries: db.New(&mockAdminDB{
				settingValue: inviteOnlySetting,
				totalUsers:   5,
				hasInvite:    false,
			}),
		}

		err := h.checkSignupAllowed(context.Background(), "uninvited@other.com", true)
		if err == nil {
			t.Fatal("expected uninvited user to be rejected in invite-only mode")
		}
	})

	t.Run("registration with instance invitation accepted in invite-only mode", func(t *testing.T) {
		h := &Handler{
			cfg: Config{AllowSignup: false},
			Queries: db.New(&mockAdminDB{
				settingValue: inviteOnlySetting,
				totalUsers:   5,
				hasInvite:    true,
			}),
		}

		err := h.checkSignupAllowed(context.Background(), "invited@other.com", true)
		if err != nil {
			t.Fatalf("expected user with valid instance invite to be accepted, got %v", err)
		}
	})

	t.Run("first user bootstrap is always accepted even in invite-only mode", func(t *testing.T) {
		h := &Handler{
			cfg: Config{AllowSignup: false},
			Queries: db.New(&mockAdminDB{
				settingValue: inviteOnlySetting,
				totalUsers:   0, // 0 users in instance
				hasInvite:    false,
			}),
		}

		err := h.checkSignupAllowed(context.Background(), "first_owner@acme.corp", true)
		if err != nil {
			t.Fatalf("expected first user to bootstrap successfully, got %v", err)
		}
	})
}
