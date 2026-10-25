package middleware

import (
	"net/http"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// RequireInstanceAdmin ensures the authenticated user has system_role IN ('instance_owner', 'instance_admin').
func RequireInstanceAdmin(queries *db.Queries) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userIDStr := r.Header.Get("X-User-ID")
			if userIDStr == "" {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			userUUID, err := util.ParseUUID(userIDStr)
			if err != nil {
				http.Error(w, `{"error":"invalid user id"}`, http.StatusUnauthorized)
				return
			}
			user, err := queries.GetUser(r.Context(), userUUID)
			if err != nil {
				http.Error(w, `{"error":"user not found"}`, http.StatusUnauthorized)
				return
			}
			if user.IsSuspended {
				http.Error(w, `{"error":"account is suspended"}`, http.StatusForbidden)
				return
			}
			if user.SystemRole != "instance_owner" && user.SystemRole != "instance_admin" {
				http.Error(w, `{"error":"instance administration privileges required"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
