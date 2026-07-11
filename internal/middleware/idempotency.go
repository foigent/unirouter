package middleware

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/errdocs"
)

const DefaultIdempotencyTTL = 24 * time.Hour

const (
	IdempotencyStateInProgress = "in_progress"
	IdempotencyStateCompleted  = "completed"
	IdempotencyStateFailed     = "failed"
)

type IdempotencyClaimInput struct {
	OrgID              int64
	APIKeyID           int64
	KeyHash            string
	RequestFingerprint string
	OperationID        string
	ClientRequestID    string
	CreatedAt          time.Time
	ExpiresAt          time.Time
}

type IdempotencyRecord struct {
	OrgID              int64
	APIKeyID           int64
	KeyHash            string
	RequestFingerprint string
	OperationID        string
	ClientRequestID    string
	State              string
	OriginalHTTPStatus *int
	CreatedAt          time.Time
	UpdatedAt          time.Time
	ExpiresAt          time.Time
}

type IdempotencyClaimResult struct {
	Claimed bool
	Record  IdempotencyRecord
}

type IdempotencyStore interface {
	Claim(context.Context, IdempotencyClaimInput) (IdempotencyClaimResult, error)
	Complete(ctx context.Context, orgID, apiKeyID int64, keyHash, operationID, state string, httpStatus int) error
}

type PostgresIdempotencyStore struct {
	db db.DBQuerier
}

func NewPostgresIdempotencyStore(q db.DBQuerier) *PostgresIdempotencyStore {
	return &PostgresIdempotencyStore{db: q}
}

func (s *PostgresIdempotencyStore) Claim(ctx context.Context, in IdempotencyClaimInput) (IdempotencyClaimResult, error) {
	if s == nil || s.db == nil {
		return IdempotencyClaimResult{}, errors.New("idempotency store is not configured")
	}
	if err := validateIdempotencyClaim(in); err != nil {
		return IdempotencyClaimResult{}, err
	}

	const insertQuery = `
		INSERT INTO request_idempotency (
			org_id, api_key_id, key_hash, request_fingerprint, operation_id,
			client_request_id, state, created_at, updated_at, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, 'in_progress', $7, $7, $8)
		ON CONFLICT (org_id, api_key_id, key_hash) DO NOTHING
		RETURNING org_id, api_key_id, key_hash, request_fingerprint, operation_id,
		          COALESCE(client_request_id, ''), state, original_http_status,
		          created_at, updated_at, expires_at`

	record, err := scanIdempotencyRecord(s.db.QueryRow(ctx, insertQuery,
		in.OrgID, in.APIKeyID, in.KeyHash, in.RequestFingerprint, in.OperationID,
		nullableIdentity(in.ClientRequestID), in.CreatedAt, in.ExpiresAt,
	))
	if err == nil {
		return IdempotencyClaimResult{Claimed: true, Record: record}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return IdempotencyClaimResult{}, fmt.Errorf("insert idempotency claim: %w", err)
	}

	existing, err := s.lookup(ctx, in.OrgID, in.APIKeyID, in.KeyHash)
	if err != nil {
		return IdempotencyClaimResult{}, fmt.Errorf("lookup idempotency claim: %w", err)
	}
	if existing.ExpiresAt.After(in.CreatedAt) {
		return IdempotencyClaimResult{Record: existing}, nil
	}

	const replaceExpiredQuery = `
		UPDATE request_idempotency
		SET request_fingerprint = $4,
		    operation_id = $5,
		    client_request_id = $6,
		    state = 'in_progress',
		    original_http_status = NULL,
		    created_at = $7,
		    updated_at = $7,
		    expires_at = $8
		WHERE org_id = $1 AND api_key_id = $2 AND key_hash = $3
		  AND expires_at <= $7
		RETURNING org_id, api_key_id, key_hash, request_fingerprint, operation_id,
		          COALESCE(client_request_id, ''), state, original_http_status,
		          created_at, updated_at, expires_at`
	record, err = scanIdempotencyRecord(s.db.QueryRow(ctx, replaceExpiredQuery,
		in.OrgID, in.APIKeyID, in.KeyHash, in.RequestFingerprint, in.OperationID,
		nullableIdentity(in.ClientRequestID), in.CreatedAt, in.ExpiresAt,
	))
	if err == nil {
		return IdempotencyClaimResult{Claimed: true, Record: record}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return IdempotencyClaimResult{}, fmt.Errorf("replace expired idempotency claim: %w", err)
	}

	// A concurrent request replaced the expired record first. Read back the
	// winner from a new statement snapshot and return it as the original claim.
	existing, err = s.lookup(ctx, in.OrgID, in.APIKeyID, in.KeyHash)
	if err != nil {
		return IdempotencyClaimResult{}, fmt.Errorf("read winning idempotency claim: %w", err)
	}
	return IdempotencyClaimResult{Record: existing}, nil
}

func (s *PostgresIdempotencyStore) lookup(ctx context.Context, orgID, apiKeyID int64, keyHash string) (IdempotencyRecord, error) {
	const query = `
		SELECT org_id, api_key_id, key_hash, request_fingerprint, operation_id,
		       COALESCE(client_request_id, ''), state, original_http_status,
		       created_at, updated_at, expires_at
		FROM request_idempotency
		WHERE org_id = $1 AND api_key_id = $2 AND key_hash = $3`
	return scanIdempotencyRecord(s.db.QueryRow(ctx, query, orgID, apiKeyID, keyHash))
}

func (s *PostgresIdempotencyStore) Complete(ctx context.Context, orgID, apiKeyID int64, keyHash, operationID, state string, httpStatus int) error {
	if s == nil || s.db == nil {
		return errors.New("idempotency store is not configured")
	}
	if state != IdempotencyStateCompleted && state != IdempotencyStateFailed {
		return fmt.Errorf("invalid terminal idempotency state %q", state)
	}
	if orgID <= 0 || apiKeyID <= 0 || len(keyHash) != 64 || strings.TrimSpace(operationID) == "" {
		return errors.New("invalid idempotency completion identity")
	}
	if httpStatus < 100 || httpStatus > 599 {
		return fmt.Errorf("invalid idempotency completion HTTP status %d", httpStatus)
	}

	tag, err := s.db.Exec(ctx, `
		UPDATE request_idempotency
		SET state = $5, original_http_status = $6, updated_at = NOW()
		WHERE org_id = $1 AND api_key_id = $2 AND key_hash = $3
		  AND operation_id = $4 AND state = 'in_progress'`,
		orgID, apiKeyID, keyHash, operationID, state, httpStatus,
	)
	if err != nil {
		return fmt.Errorf("complete idempotency claim: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errors.New("idempotency claim ownership lost before completion")
	}
	return nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanIdempotencyRecord(row rowScanner) (IdempotencyRecord, error) {
	var record IdempotencyRecord
	err := row.Scan(
		&record.OrgID, &record.APIKeyID, &record.KeyHash, &record.RequestFingerprint,
		&record.OperationID, &record.ClientRequestID, &record.State,
		&record.OriginalHTTPStatus, &record.CreatedAt, &record.UpdatedAt, &record.ExpiresAt,
	)
	return record, err
}

func validateIdempotencyClaim(in IdempotencyClaimInput) error {
	if in.OrgID <= 0 || in.APIKeyID <= 0 {
		return errors.New("idempotency claim requires tenant and API-key identity")
	}
	if len(in.KeyHash) != 64 || len(in.RequestFingerprint) != 64 {
		return errors.New("idempotency claim requires keyed 64-character digests")
	}
	if strings.TrimSpace(in.OperationID) == "" || len(in.OperationID) > 64 {
		return errors.New("idempotency claim requires a bounded operation identity")
	}
	if len(in.ClientRequestID) > 128 {
		return errors.New("client request identity exceeds 128 characters")
	}
	if in.CreatedAt.IsZero() || !in.ExpiresAt.After(in.CreatedAt) {
		return errors.New("idempotency claim requires a future expiry")
	}
	return nil
}

func nullableIdentity(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func IdempotencyGuard(store IdempotencyStore, fingerprintSecret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := GetRequestIdentityFromContext(r.Context())
		if !identity.IdempotencyKeyPresent {
			next.ServeHTTP(w, r)
			return
		}
		if store == nil || strings.TrimSpace(fingerprintSecret) == "" {
			writeRequestIdentityError(w, http.StatusInternalServerError, "server_error.internal_error", "Idempotency is not configured")
			return
		}

		orgID := GetOrgIDFromContext(r.Context())
		apiKeyID := GetKeyIDFromContext(r.Context())
		if orgID <= 0 || apiKeyID <= 0 {
			writeRequestIdentityError(w, http.StatusInternalServerError, "server_error.internal_error", "Authenticated idempotency scope is unavailable")
			return
		}
		fingerprint, err := fingerprintRequest(r, fingerprintSecret)
		if err != nil {
			writeRequestIdentityError(w, http.StatusBadRequest, "validation_error.invalid_request", "Failed to fingerprint request body")
			return
		}

		now := time.Now().UTC()
		claim, err := store.Claim(r.Context(), IdempotencyClaimInput{
			OrgID:              orgID,
			APIKeyID:           apiKeyID,
			KeyHash:            identity.IdempotencyKeyHash,
			RequestFingerprint: fingerprint,
			OperationID:        identity.OperationID,
			ClientRequestID:    identity.ClientRequestID,
			CreatedAt:          now,
			ExpiresAt:          now.Add(DefaultIdempotencyTTL),
		})
		if err != nil {
			writeRequestIdentityError(w, http.StatusInternalServerError, "server_error.internal_error", "Failed to reserve idempotent request")
			return
		}
		if !claim.Claimed {
			writeIdempotencyConflict(w, fingerprint, claim.Record)
			return
		}

		capture := &responseStatusCapture{}
		wrapped := wrapStatusResponseWriter(w, capture)
		panicked := true
		defer func() {
			status := capture.Status()
			state := IdempotencyStateCompleted
			if panicked || status < 200 || status >= 300 {
				state = IdempotencyStateFailed
				if panicked && status < 500 {
					status = http.StatusInternalServerError
				}
			}
			finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Second)
			defer cancel()
			if err := store.Complete(finalizeCtx, orgID, apiKeyID, identity.IdempotencyKeyHash, identity.OperationID, state, status); err != nil {
				log.Printf("[idempotency] operation=%s finalization failed: %v", identity.OperationID, err)
			}
		}()

		next.ServeHTTP(wrapped, r)
		panicked = false
	})
}

func fingerprintRequest(r *http.Request, secret string) (string, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return "", err
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))

	canonicalBody := body
	if json.Valid(body) {
		var value any
		if err := json.Unmarshal(body, &value); err == nil {
			if encoded, marshalErr := json.Marshal(value); marshalErr == nil {
				canonicalBody = encoded
			}
		}
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("unirouter.request-fingerprint.v1\x00"))
	_, _ = mac.Write([]byte(strings.ToUpper(r.Method)))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(r.URL.RequestURI()))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(canonicalBody)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func writeIdempotencyConflict(w http.ResponseWriter, fingerprint string, record IdempotencyRecord) {
	code := "idempotency_error.request_in_progress"
	message := "An identical request is already in progress"
	if record.RequestFingerprint != fingerprint {
		code = "idempotency_error.key_reused_with_different_request"
		message = "The Idempotency-Key was already used with a different request"
	} else {
		switch record.State {
		case IdempotencyStateCompleted:
			code = "idempotency_error.already_completed"
			message = "The request already completed; its model response was not stored for replay"
		case IdempotencyStateFailed:
			code = "idempotency_error.original_failed"
			message = "The original request failed and will not be executed again with this Idempotency-Key"
		default:
			w.Header().Set("Retry-After", "1")
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set(OriginalOperationHeader, record.OperationID)
	w.WriteHeader(http.StatusConflict)
	receipt := map[string]any{
		"original_request_id": record.OperationID,
		"status":              record.State,
		"created_at":          record.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if record.OriginalHTTPStatus != nil {
		receipt["original_http_status"] = *record.OriginalHTTPStatus
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"type":    "idempotency_error",
			"code":    code,
			"message": message,
			"doc_url": errdocs.URLForCode(code),
			"param":   nil,
		},
		"idempotency": receipt,
	})
}

type responseStatusCapture struct {
	status int
}

func (c *responseStatusCapture) Status() int {
	if c.status == 0 {
		return http.StatusOK
	}
	return c.status
}

type statusResponseWriter struct {
	http.ResponseWriter
	capture *responseStatusCapture
}

func (w *statusResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *statusResponseWriter) WriteHeader(status int) {
	if w.capture.status != 0 {
		return
	}
	w.capture.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusResponseWriter) Write(body []byte) (int, error) {
	if w.capture.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

type flushStatusResponseWriter struct {
	*statusResponseWriter
	flusher http.Flusher
}

func (w *flushStatusResponseWriter) Flush() {
	if w.capture.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	w.flusher.Flush()
}

func wrapStatusResponseWriter(w http.ResponseWriter, capture *responseStatusCapture) http.ResponseWriter {
	base := &statusResponseWriter{ResponseWriter: w, capture: capture}
	if flusher, ok := w.(http.Flusher); ok {
		return &flushStatusResponseWriter{statusResponseWriter: base, flusher: flusher}
	}
	return base
}
