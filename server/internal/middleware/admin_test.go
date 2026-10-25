package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type mockUserDB struct {
	user db.User
	err  error
}

func (m *mockUserDB) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag(""), nil
}

func (m *mockUserDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}

func (m *mockUserDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return &mockUserRow{user: m.user, err: m.err}
}

type mockUserRow struct {
	user db.User
	err  error
}

func (r *mockUserRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) >= 18 {
		*(dest[0].(*pgtype.UUID)) = r.user.ID
		*(dest[1].(*string)) = r.user.Name
		*(dest[2].(*string)) = r.user.Email
		*(dest[3].(*pgtype.Text)) = r.user.AvatarUrl
		*(dest[4].(*pgtype.Timestamptz)) = r.user.CreatedAt
		*(dest[5].(*pgtype.Timestamptz)) = r.user.UpdatedAt
		*(dest[6].(*pgtype.Timestamptz)) = r.user.OnboardedAt
		*(dest[7].(*[]byte)) = r.user.OnboardingQuestionnaire
		*(dest[8].(*pgtype.Text)) = r.user.CloudWaitlistEmail
		*(dest[9].(*pgtype.Text)) = r.user.CloudWaitlistReason
		*(dest[10].(*pgtype.Text)) = r.user.StarterContentState
		*(dest[11].(*pgtype.Text)) = r.user.Language
		*(dest[12].(*string)) = r.user.ProfileDescription
		*(dest[13].(*pgtype.Text)) = r.user.Timezone
		*(dest[14].(*string)) = r.user.SystemRole
		*(dest[15].(*bool)) = r.user.IsSuspended
		*(dest[16].(*pgtype.Timestamptz)) = r.user.SuspendedAt
		*(dest[17].(*pgtype.Text)) = r.user.SuspensionReason
	}
	return nil
}

func TestRequireInstanceAdmin(t *testing.T) {
	adminID := "00000000-0000-0000-0000-000000000001"
	adminUUID := util.MustParseUUID(adminID)

	tests := []struct {
		name       string
		userID     string
		user       db.User
		dbErr      error
		wantStatus int
	}{
		{
			name:       "missing user id in header",
			userID:     "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid uuid string",
			userID:     "not-a-uuid",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "user not found",
			userID:     adminID,
			dbErr:      errors.New("user not found"),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:   "standard user rejected",
			userID: adminID,
			user: db.User{
				ID:         adminUUID,
				SystemRole: "standard",
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name:   "suspended instance admin rejected",
			userID: adminID,
			user: db.User{
				ID:          adminUUID,
				SystemRole:  "instance_admin",
				IsSuspended: true,
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name:   "instance admin allowed",
			userID: adminID,
			user: db.User{
				ID:         adminUUID,
				SystemRole: "instance_admin",
			},
			wantStatus: http.StatusOK,
		},
		{
			name:   "instance owner allowed",
			userID: adminID,
			user: db.User{
				ID:         adminUUID,
				SystemRole: "instance_owner",
			},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queries := db.New(&mockUserDB{user: tt.user, err: tt.dbErr})
			mw := RequireInstanceAdmin(queries)

			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/api/admin/vitals", nil)
			if tt.userID != "" {
				req.Header.Set("X-User-ID", tt.userID)
			}
			rec := httptest.NewRecorder()

			mw(nextHandler).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d (body: %s)", tt.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}
