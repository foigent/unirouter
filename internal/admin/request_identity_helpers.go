package admin

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/middleware"
)

// ensureOperationIdentityForHandler is a direct-handler test and internal-call
// fallback. Production routes receive an identity from the outer middleware;
// this helper never replaces an existing server operation with a client value.
func ensureOperationIdentityForHandler(w http.ResponseWriter, r *http.Request) (*http.Request, string, bool) {
	operationID := middleware.GetRequestIDFromContext(r.Context())
	if operationID == "" {
		var err error
		operationID, err = middleware.NewOperationID()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to generate request identity"})
			return r, "", false
		}
		r = r.WithContext(context.WithValue(r.Context(), middleware.CtxRequestID, operationID))
	}
	w.Header().Set(middleware.OperationIDHeader, operationID)
	return r, operationID, true
}

func isPostgresUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
