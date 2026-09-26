package sourcefacts

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/middleware"
)

type CurrentRuntimeEntry struct {
	RequestID       string `json:"request_id"`
	Family          string `json:"family"`
	SubjectKind     string `json:"subject_kind"`
	SubjectID       string `json:"subject_id"`
	Operation       string `json:"operation"`
	Freshness       string `json:"freshness"`
	OriginalState   string `json:"original_state"`
	LastObservedAt  string `json:"last_observed_at"`
	ValidUntil      string `json:"valid_until,omitempty"`
	LeaseGeneration string `json:"lease_generation,omitempty"`
	EvidenceRef     string `json:"evidence_ref"`
	CheckpointRef   string `json:"checkpoint_ref,omitempty"`
}

type CurrentRuntimePage struct {
	SchemaVersion  string                `json:"schema_version"`
	SourceInstance string                `json:"source_instance"`
	OrgID          string                `json:"org_id"`
	ProjectID      string                `json:"project_id"`
	Profile        string                `json:"profile"`
	ObservedAt     string                `json:"observed_at"`
	Coverage       string                `json:"coverage"`
	Uncovered      []string              `json:"uncovered"`
	Entries        []CurrentRuntimeEntry `json:"entries"`
	HasMore        bool                  `json:"has_more"`
	NextCursor     string                `json:"next_cursor,omitempty"`
	SourceSHA256   string                `json:"source_sha256,omitempty"`
}

type currentRuntimeCursor struct {
	Version       string `json:"version"`
	Source        string `json:"source"`
	Org           string `json:"org"`
	Project       string `json:"project"`
	Key           string `json:"key"`
	Generation    string `json:"generation"`
	RequestFilter string `json:"request_filter"`
	AfterRequest  string `json:"after_request"`
	AfterFamily   string `json:"after_family"`
	AfterSubject  string `json:"after_subject"`
}

func (s *Service) currentCursorScope(a access, request string) currentRuntimeCursor {
	return currentRuntimeCursor{Version: "unirouter.runtime-current-cursor.v1", Source: s.options.SourceInstance, Org: executioncontext.ID(a.org), Project: executioncontext.ID(a.project), Key: executioncontext.ID(a.keyID), Generation: executioncontext.ID(a.generation), RequestFilter: request}
}
func (s *Service) encodeCurrentCursor(c currentRuntimeCursor) (string, error) {
	body, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(s.options.CursorSecret))
	mac.Write([]byte("unirouter.runtime-current-cursor.v1\x00"))
	mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(append(body, mac.Sum(nil)...)), nil
}
func (s *Service) decodeCurrentCursor(raw string, scope currentRuntimeCursor) (currentRuntimeCursor, error) {
	invalid := errors.New("invalid current runtime cursor")
	if len(raw) > 4096 {
		return scope, invalid
	}
	wire, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(wire) <= sha256.Size {
		return scope, invalid
	}
	body, signature := wire[:len(wire)-sha256.Size], wire[len(wire)-sha256.Size:]
	mac := hmac.New(sha256.New, []byte(s.options.CursorSecret))
	mac.Write([]byte("unirouter.runtime-current-cursor.v1\x00"))
	mac.Write(body)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return scope, invalid
	}
	var c currentRuntimeCursor
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&c) != nil {
		return scope, invalid
	}
	canonical, _ := json.Marshal(c)
	if !bytes.Equal(canonical, body) {
		return scope, invalid
	}
	if c.Version != scope.Version || c.Source != scope.Source || c.Org != scope.Org || c.Project != scope.Project || c.Key != scope.Key || c.Generation != scope.Generation || c.RequestFilter != scope.RequestFilter || !opaque(c.AfterRequest, 64) || !oneOf(c.AfterFamily, "stream", "media", "request") || !opaque(c.AfterSubject, 128) {
		return scope, invalid
	}
	return c, nil
}

// Only original active execution leases enter the unfiltered inventory. A
// model's half-open Probe lease cannot satisfy either branch. The C collation
// fixes textual keyset order across supported database locale configurations.
const currentRuntimeSQL = `WITH inventory AS (
 SELECT l.request_id,'stream'::text AS family,'provider_attempt'::text AS subject_kind,l.attempt_id AS subject_id,
        COALESCE(NULLIF(p.route_model_operation_id,''),'not_available') AS operation,
        'lease_valid_execution'::text AS freshness,l.state AS original_state,
        GREATEST(l.updated_at,c.created_at) AS observed_basis,l.lease_expires_at AS valid_until,l.generation,l.owner_id AS owner,
        COALESCE(c.sequence,0) AS checkpoint_sequence
 FROM stream_delivery_leases l
 JOIN freeze_idempotency r ON r.org_id=l.org_id AND r.request_id=l.request_id AND r.delivery_attempt_id=l.attempt_id
 JOIN billing_provider_attempts p ON p.org_id=l.org_id AND p.request_id=l.request_id AND p.attempt_id=l.attempt_id AND p.project_id=r.project_id
 JOIN LATERAL (SELECT mode,outcome,stream_owner_id,stream_lease_generation FROM request_delivery_receipts d
   WHERE d.org_id=l.org_id AND d.request_id=l.request_id AND d.attempt_id=l.attempt_id ORDER BY sequence DESC LIMIT 1) d ON true
 LEFT JOIN LATERAL (SELECT sequence,attempt_id,lease_generation,created_at FROM stream_delivery_checkpoints c
   WHERE c.org_id=l.org_id AND c.request_id=l.request_id ORDER BY sequence DESC LIMIT 1) c ON true
 WHERE l.org_id=$1 AND r.project_id=$2 AND l.state='active' AND l.lease_expires_at>$3 AND l.owner_id<>'' AND l.generation>0
   AND r.status='settlement_pending'
   AND (r.settlement_stream_generation IS NULL OR (r.settlement_stream_generation=l.generation AND r.settlement_stream_owner_id=l.owner_id))
   AND d.mode='stream' AND d.outcome IN ('write_started','partial') AND d.stream_owner_id=l.owner_id AND d.stream_lease_generation=l.generation
   AND (c.sequence IS NULL OR (c.attempt_id=l.attempt_id AND c.lease_generation=l.generation))
 UNION ALL
 SELECT j.request_id,'media'::text,'media_job'::text,j.job_id,j.operation,
        CASE WHEN j.execution_state='running' THEN 'lease_valid_execution' ELSE 'leased_pending' END,
        j.execution_state,j.updated_at,j.lease_expires_at,j.lease_fence,j.lease_owner,0
 FROM async_media_jobs j
 WHERE j.org_id=$1 AND j.project_id=$2 AND j.queue_state='leased' AND j.lease_state='leased'
   AND j.lease_expires_at>$3 AND j.lease_owner<>'' AND j.lease_fence>0 AND j.terminal_at IS NULL
   AND j.provider_outcome='pending' AND j.execution_state IN ('pending','running')
   AND (j.execution_state='pending' OR (j.started_at IS NOT NULL AND j.attempt>0))
), last_known AS (
 SELECT r.request_id,'request'::text AS family,'request'::text AS subject_kind,r.request_id AS subject_id,
        COALESCE(NULLIF(p.route_model_operation_id,''),'not_available') AS operation,'last_known'::text AS freshness,r.status AS original_state,r.updated_at AS observed_basis,
        NULL::timestamptz AS valid_until,NULL::bigint AS generation,''::text AS owner,0 AS checkpoint_sequence
 FROM freeze_idempotency r LEFT JOIN LATERAL (SELECT route_model_operation_id FROM billing_provider_attempts p
   WHERE p.org_id=r.org_id AND p.project_id=r.project_id AND p.request_id=r.request_id ORDER BY route_sequence DESC,attempt_id COLLATE "C" DESC LIMIT 1) p ON true
 WHERE r.org_id=$1 AND r.project_id=$2 AND r.request_id=$4 AND $4<>''
 UNION ALL SELECT j.request_id,'media'::text,'media_job'::text,j.job_id,j.operation,'last_known',j.execution_state,j.updated_at,NULL::timestamptz,NULL::bigint,''::text,0
 FROM async_media_jobs j WHERE j.org_id=$1 AND j.project_id=$2 AND j.request_id=$4 AND $4<>''
), visible AS (
 SELECT * FROM inventory WHERE ($4='' OR request_id=$4)
 UNION ALL SELECT * FROM last_known WHERE NOT EXISTS (SELECT 1 FROM inventory WHERE request_id=$4)
)
SELECT request_id,family,subject_kind,subject_id,operation,freshness,original_state,observed_basis,valid_until,generation,owner,checkpoint_sequence
FROM visible WHERE (request_id COLLATE "C",family COLLATE "C",subject_id COLLATE "C")>($5 COLLATE "C",$6 COLLATE "C",$7 COLLATE "C")
ORDER BY request_id COLLATE "C",family COLLATE "C",subject_id COLLATE "C" LIMIT $8`

func (s *Service) readCurrentRuntimeTx(ctx context.Context, tx pgx.Tx, a access, cursor currentRuntimeCursor, cutoff time.Time, limit int) ([]CurrentRuntimeEntry, error) {
	rows, err := tx.Query(ctx, currentRuntimeSQL, a.org, a.project, cutoff, cursor.RequestFilter, cursor.AfterRequest, cursor.AfterFamily, cursor.AfterSubject, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []CurrentRuntimeEntry{}
	for rows.Next() {
		var item CurrentRuntimeEntry
		var basis time.Time
		var until *time.Time
		var generation *int64
		var owner string
		var checkpoint int64
		if err = rows.Scan(&item.RequestID, &item.Family, &item.SubjectKind, &item.SubjectID, &item.Operation, &item.Freshness, &item.OriginalState, &basis, &until, &generation, &owner, &checkpoint); err != nil {
			return nil, err
		}
		item.LastObservedAt = executioncontext.Time(basis)
		if until != nil {
			item.ValidUntil = executioncontext.Time(*until)
		}
		if generation != nil {
			item.LeaseGeneration = executioncontext.ID(*generation)
		}
		item.EvidenceRef = sourceID("unirouter.runtime-current-observation.v1", s.options.SourceInstance, executioncontext.ID(a.org), executioncontext.ID(a.project), item.Family, item.RequestID, item.SubjectID, item.LeaseGeneration, item.LastObservedAt, item.ValidUntil, fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(owner))))
		if checkpoint > 0 {
			item.CheckpointRef = sourceID("unirouter.runtime-checkpoint.v1", s.options.SourceInstance, executioncontext.ID(a.org), item.RequestID, item.SubjectID, item.LeaseGeneration, executioncontext.ID(checkpoint))
		}
		entries = append(entries, item)
	}
	return entries, rows.Err()
}

func (s *Service) RuntimeCurrentHandler(resolver middleware.ClientIPResolver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeError(w, 405, "method_not_allowed", false)
			return
		}
		if !s.ObservationsEnabled() {
			writeError(w, 503, "source_disabled", true)
			return
		}
		query := r.URL.Query()
		for key, values := range query {
			if len(values) != 1 || !oneOf(key, "request_id", "cursor", "limit") {
				writeError(w, 400, "invalid_query", false)
				return
			}
		}
		request := query.Get("request_id")
		if request != "" && !opaque(request, 64) {
			writeError(w, 400, "invalid_request_id", false)
			return
		}
		limit := 50
		if query.Get("limit") != "" {
			var err error
			limit, err = strconv.Atoi(query.Get("limit"))
			if err != nil || limit < 1 || limit > 50 {
				writeError(w, 400, "invalid_limit", false)
				return
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		select {
		case s.readSlots <- struct{}{}:
			defer func() { <-s.readSlots }()
		default:
			writeError(w, 503, "source_busy", true)
			return
		}
		tx, err := s.db.Begin(ctx)
		if err != nil {
			writeError(w, 503, "source_unavailable", true)
			return
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`); err != nil {
			writeError(w, 503, "source_unavailable", true)
			return
		}
		a, err := s.authenticateTx(ctx, tx, r, resolver)
		if err != nil {
			if errors.Is(err, errSourceAccessDenied) {
				writeError(w, 401, "source_credential_denied", false)
			} else {
				writeError(w, 503, "source_unavailable", true)
			}
			return
		}
		if !slices.Contains(a.profiles, RuntimeProfile) || !slices.Contains(a.credentialProfiles, RuntimeProfile) {
			writeError(w, 403, "profile_not_authorized", false)
			return
		}
		if a.status != "active" {
			writeError(w, 503, "source_paused", true)
			return
		}
		if a.keyStatus != "feed_active" {
			writeError(w, 409, "staged_credential_requires_pause", false)
			return
		}
		cursor := s.currentCursorScope(a, request)
		if query.Get("cursor") != "" {
			cursor, err = s.decodeCurrentCursor(query.Get("cursor"), cursor)
			if err != nil {
				writeError(w, 400, "invalid_cursor", false)
				return
			}
		}
		var cutoff time.Time
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&cutoff); err != nil {
			writeError(w, 503, "source_unavailable", true)
			return
		}
		entries, err := s.readCurrentRuntimeTx(ctx, tx, a, cursor, cutoff, limit)
		if err != nil {
			writeError(w, 503, "source_unavailable", true)
			return
		}
		page := CurrentRuntimePage{SchemaVersion: "unirouter.runtime-current.v1", SourceInstance: s.options.SourceInstance, OrgID: executioncontext.ID(a.org), ProjectID: executioncontext.ID(a.project), Profile: RuntimeProfile, ObservedAt: executioncontext.Time(cutoff), Coverage: "lease_backed_inventory", Uncovered: []string{"synchronous_requests_without_execution_lease", "progress_between_lease_observations"}, Entries: entries, HasMore: len(entries) > limit}
		if page.HasMore {
			page.Entries = entries[:limit]
			last := page.Entries[len(page.Entries)-1]
			cursor.AfterRequest, cursor.AfterFamily, cursor.AfterSubject = last.RequestID, last.Family, last.SubjectID
			page.NextCursor, err = s.encodeCurrentCursor(cursor)
			if err != nil {
				writeError(w, 503, "page_contract_failure", false)
				return
			}
		}
		if request != "" {
			page.Coverage = "requested_execution_snapshot"
		}
		source, err := json.Marshal(page)
		if err != nil {
			writeError(w, 503, "page_contract_failure", false)
			return
		}
		var value any
		if err = json.Unmarshal(source, &value); err != nil {
			writeError(w, 503, "page_contract_failure", false)
			return
		}
		canonical, ce := eventconnector.CanonicalJSON(value)
		if ce != nil {
			writeError(w, 503, "page_contract_failure", false)
			return
		}
		page.SourceSHA256 = eventconnector.HashSourceBytes(canonical)
		wire, err := json.Marshal(page)
		if err != nil || len(wire) >= MaxPageBytes {
			writeError(w, 503, "page_size_failure", false)
			return
		}
		if err = tx.Commit(ctx); err != nil {
			writeError(w, 503, "source_unavailable", true)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(wire)
	})
}
