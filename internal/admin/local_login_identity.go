package admin

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	localLoginIdentityUniqueIndex = "users_local_login_id_unique"
	legacyOrgUsernameConstraint   = "users_org_username_unique"
)

// writeLocalLoginIdentityConflict handles only the two known username
// uniqueness contracts. Other unique, foreign-key, trigger, and connectivity
// failures must retain their real server-error semantics instead of being
// mislabeled as an identity collision.
func writeLocalLoginIdentityConflict(w http.ResponseWriter, err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return false
	}
	if pgErr.ConstraintName != localLoginIdentityUniqueIndex && pgErr.ConstraintName != legacyOrgUsernameConstraint {
		return false
	}
	writeJSON(w, http.StatusConflict, map[string]interface{}{
		"error": "Local login ID is unavailable",
		"code":  "local_login_id_conflict",
	})
	return true
}
