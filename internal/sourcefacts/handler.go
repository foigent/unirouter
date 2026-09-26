package sourcefacts

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/eventconnector/pullv1"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/ipallowlist"
	"github.com/uniroute/unirouter/internal/middleware"
)

type access struct {
	keyID, org, project, generation  int64
	keyStatus, status, instance, ips string
	profiles, credentialProfiles     []string
}

var errSourceAccessDenied = errors.New("source credential denied")

func (s *Service) authenticateTx(ctx context.Context, tx pgx.Tx, r *http.Request, resolver middleware.ClientIPResolver) (access, error) {
	var a access
	values := []string{}
	for name, current := range r.Header {
		if strings.EqualFold(name, "Authorization") {
			values = append(values, current...)
		}
	}
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return a, errSourceAccessDenied
	}
	raw := strings.TrimPrefix(values[0], "Bearer ")
	hash, prefix, e := auth.HashReceiverFeedCredential(raw, s.options.CursorSecret)
	if e != nil {
		return a, errSourceAccessDenied
	}
	e = tx.QueryRow(ctx, `SELECT ak.id,ak.org_id,ak.project_id,ak.status,ak.allowed_ips,c.source_instance,c.status,c.scope_generation,c.profiles,sc.profiles
	 FROM api_keys ak JOIN execution_source_credentials sc ON sc.api_key_id=ak.id AND sc.org_id=ak.org_id AND sc.project_id=ak.project_id
	 JOIN execution_source_connections c ON c.org_id=sc.org_id AND c.project_id=sc.project_id
	 JOIN orgs o ON o.id=ak.org_id JOIN projects p ON p.id=ak.project_id AND p.org_id=ak.org_id
	 WHERE ak.purpose=$1 AND ak.verifier_scheme=$2 AND ak.key_hash=$3 AND ak.key_prefix=$4
	 AND COALESCE(to_jsonb(ak)->>'source_lifecycle_scope','legacy')='legacy'
	 AND ak.key_type='service' AND ak.allowed_models='' AND ak.status IN ('feed_active','feed_staged')
	 AND o.status='active' AND p.status='active' FOR SHARE OF c`, Purpose, auth.ReceiverFeedVerifierScheme, hash, prefix).Scan(&a.keyID, &a.org, &a.project, &a.keyStatus, &a.ips, &a.instance, &a.status, &a.generation, &a.profiles, &a.credentialProfiles)
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return a, errSourceAccessDenied
		}
		return a, e
	}
	if a.instance != s.options.SourceInstance || a.status == "revoked" || strings.TrimSpace(a.ips) == "" || !ipallowlist.Matches(resolver.Resolve(r), a.ips) {
		return a, errSourceAccessDenied
	}
	return a, nil
}

func writeError(w http.ResponseWriter, status int, code string, retryable bool) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if retryable {
		w.Header().Set("Retry-After", "1")
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": eventconnector.NewConnectorError("source", code, "fetch", "read_source", retryable, "Source facts request could not be completed", "operator_repair", nil)})
}

// Handler authenticates and reads within one repeatable-read snapshot; no
// reader-dependent redaction changes the immutable document or event hash.
func (s *Service) Handler(resolver middleware.ClientIPResolver) http.Handler {
	return s.eventHandler(resolver, false)
}

func (s *Service) ObservationHandler(resolver middleware.ClientIPResolver) http.Handler {
	return s.eventHandler(resolver, true)
}

func (s *Service) eventHandler(resolver middleware.ClientIPResolver, observations bool) http.Handler {
	profiles, validProfile, connectorVersion := Profiles(), ValidProfile, ConnectorVersion
	encodeCursor, decodeCursor := s.encodeCursor, s.decodeCursor
	if observations {
		profiles, validProfile, connectorVersion = ObservationProfiles(), validObservationProfile, ObservationConnectorVersion
		encodeCursor, decodeCursor = s.encodeObservationCursor, s.decodeObservationCursor
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeError(w, 405, "method_not_allowed", false)
			return
		}
		capabilities := strings.HasSuffix(r.URL.Path, "/capabilities")
		if !s.Enabled() || observations && !s.ObservationsEnabled() && !(capabilities && s.BudgetsEnabled()) {
			writeError(w, 503, "source_disabled", true)
			return
		}
		query := r.URL.Query()
		limit := pullv1.MaxBatchItems
		for key, values := range query {
			if len(values) != 1 || (key != "profile" && key != "cursor" && key != "limit") || capabilities {
				writeError(w, 400, "invalid_query", false)
				return
			}
		}
		profile := query.Get("profile")
		if !capabilities && !validProfile(profile) {
			writeError(w, 400, "unsupported_profile", false)
			return
		}
		if value := query.Get("limit"); value != "" {
			var e error
			limit, e = strconv.Atoi(value)
			if e != nil || limit < 1 || limit > 50 {
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
		tx, e := s.db.Begin(ctx)
		if e != nil {
			writeError(w, 503, "source_unavailable", true)
			return
		}
		defer tx.Rollback(ctx)
		if _, e = tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`); e != nil {
			writeError(w, 503, "source_unavailable", true)
			return
		}
		a, e := s.authenticateTx(ctx, tx, r, resolver)
		if e != nil {
			if errors.Is(e, errSourceAccessDenied) {
				writeError(w, 401, "source_credential_denied", false)
			} else {
				writeError(w, 503, "source_unavailable", true)
			}
			return
		}
		requestProfiles := profiles
		if observations && capabilities && s.BudgetsEnabled() {
			requestProfiles = slices.Clone(profiles)
			if !s.ObservationsEnabled() {
				requestProfiles = nil
			}
			requestProfiles = append(requestProfiles, BudgetProfiles()...)
			slices.Sort(requestProfiles)
		}
		allowed := []string{}
		for _, p := range requestProfiles {
			if slices.Contains(a.profiles, p) && slices.Contains(a.credentialProfiles, p) {
				allowed = append(allowed, p)
			}
		}
		if capabilities {
			if a.keyStatus == "feed_staged" && a.status != "paused" {
				writeError(w, 409, "staged_credential_requires_pause", false)
				return
			}
			if e = tx.Commit(ctx); e != nil {
				writeError(w, 503, "source_unavailable", true)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			capabilitySchema := "unirouter.source-capabilities.v1"
			if observations {
				capabilitySchema = "unirouter.source-capabilities.v2"
			}
			result := map[string]any{"schema_version": capabilitySchema, "source_instance": s.options.SourceInstance, "profiles": allowed, "profile_version": connectorVersion, "status": a.status, "credential_status": a.keyStatus, "scope_generation": strconv.FormatInt(a.generation, 10), "retention_seconds": int64(RetentionWindow / time.Second), "bootstrap": "forward_only", "timeline_complete": false, "field_authorization": "fixed_profiles"}
			if observations {
				transports := map[string][]string{}
				for _, p := range allowed {
					transports[p] = []string{"incremental_events"}
					if validBudgetProfile(p) {
						transports[p] = []string{"budget_catalog", "current_budget_snapshot"}
					}
					if p == RuntimeProfile {
						transports[p] = append(transports[p], "current_snapshot")
					}
				}
				result["profile_transports"] = transports
				result["event_coverage"] = "forward_only"
				result["current_runtime_coverage"] = "lease_backed_inventory"
			}
			json.NewEncoder(w).Encode(result)
			return
		}
		if !slices.Contains(allowed, profile) {
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
		var high, floor, epoch int64
		e = tx.QueryRow(ctx, `SELECT high_sequence,retained_after_sequence,source_epoch FROM execution_source_streams WHERE source_instance=$1 AND org_id=$2 AND project_id=$3 AND profile=$4`, s.options.SourceInstance, a.org, a.project, profile).Scan(&high, &floor, &epoch)
		if errors.Is(e, pgx.ErrNoRows) {
			epoch = 1
		} else if e != nil {
			writeError(w, 503, "source_unavailable", true)
			return
		}
		scope := cursorScope{a.org, a.project, a.generation, epoch, profile}
		bootstrap := query.Get("cursor") == ""
		after := high
		if !bootstrap {
			after, e = decodeCursor(query.Get("cursor"), scope)
			if e != nil {
				writeError(w, 400, "invalid_cursor", false)
				return
			}
			if after < floor {
				writeError(w, 410, "cursor_expired", false)
				return
			}
			if after > high {
				writeError(w, 409, "cursor_regressed", false)
				return
			}
		}
		batch := pullv1.EventBatch{BatchVersion: pullv1.EventBatchVersion, ConnectorID: ConnectorID, ConnectorVersion: connectorVersion, ProducerInstance: s.options.SourceInstance, GeneratedAt: executioncontext.Time(time.Now()), Events: []eventconnector.ImmutableEvent{}}
		batch.HighWatermark, _ = encodeCursor(scope, high)
		next := after
		if !bootstrap {
			rows, err := tx.Query(ctx, `SELECT sequence,document,source_hash FROM execution_source_facts WHERE source_instance=$1 AND org_id=$2 AND project_id=$3 AND profile=$4 AND sequence>$5 ORDER BY sequence LIMIT $6`, s.options.SourceInstance, a.org, a.project, profile, after, limit+1)
			if err != nil {
				writeError(w, 503, "source_unavailable", true)
				return
			}
			for rows.Next() {
				var position int64
				var wire []byte
				var hash string
				if err = rows.Scan(&position, &wire, &hash); err != nil {
					break
				}
				if len(batch.Events) == limit {
					batch.HasMore = true
					break
				}
				if eventconnector.HashSourceBytes(wire) != hash {
					err = errors.New("source integrity failure")
					break
				}
				event, ee := s.boundEvent(wire, a, profile, observations)
				if ee != nil {
					err = ee
					break
				}

				batch.Events = append(batch.Events, event)
				next = position
			}
			if err == nil {
				err = rows.Err()
			}
			rows.Close()
			if err != nil {
				writeError(w, 503, "source_integrity_failure", false)
				return
			}
			// This acknowledges only the previous durable consumer checkpoint.
			_, e = tx.Exec(ctx, `INSERT INTO execution_source_acknowledgements(org_id,project_id,profile,scope_generation,acknowledged_sequence) VALUES($1,$2,$3,$4,$5)
			 ON CONFLICT(org_id,project_id,profile) DO UPDATE SET scope_generation=EXCLUDED.scope_generation,acknowledged_sequence=CASE WHEN execution_source_acknowledgements.scope_generation=EXCLUDED.scope_generation THEN GREATEST(execution_source_acknowledgements.acknowledged_sequence,EXCLUDED.acknowledged_sequence) ELSE EXCLUDED.acknowledged_sequence END,last_authenticated_pull_at=clock_timestamp()`, a.org, a.project, profile, a.generation, after)
			if e != nil {
				writeError(w, 503, "checkpoint_unavailable", true)
				return
			}
		}
		batch.NextCursor, _ = encodeCursor(scope, next)
		if e := batch.Validate(); e != nil {
			writeError(w, 503, "page_contract_failure", false)
			return
		}
		wire, e := json.Marshal(batch)
		if e != nil || len(wire) >= MaxPageBytes {
			writeError(w, 503, "page_size_failure", false)
			return
		}
		if e = tx.Commit(ctx); e != nil {
			writeError(w, 503, "source_unavailable", true)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(wire)
	})
}

func (s *Service) boundEvent(wire []byte, a access, profile string, observations bool) (eventconnector.ImmutableEvent, error) {
	if observations {
		if profile == GatewayPreDispatchProfile {
			doc, err := DecodeGatewayPreDispatchTerminal(wire)
			if err != nil {
				return eventconnector.ImmutableEvent{}, err
			}
			if doc.Profile != profile || doc.OrgID != strconv.FormatInt(a.org, 10) || doc.ProjectID != strconv.FormatInt(a.project, 10) || doc.SourceInstance != a.instance {
				return eventconnector.ImmutableEvent{}, errors.New("source row binding failure")
			}
			return GatewayPreDispatchEvent(doc, wire)
		}
		doc, err := DecodeObservation(wire)
		if err != nil {
			return eventconnector.ImmutableEvent{}, err
		}
		if doc.Profile != profile || doc.OrgID != strconv.FormatInt(a.org, 10) || doc.ProjectID != strconv.FormatInt(a.project, 10) || doc.SourceInstance != a.instance {
			return eventconnector.ImmutableEvent{}, errors.New("source row binding failure")
		}
		return ObservationEvent(doc, wire)
	}
	doc, err := Decode(wire)
	if err != nil {
		return eventconnector.ImmutableEvent{}, err
	}
	if doc.Profile != profile || doc.OrgID != strconv.FormatInt(a.org, 10) || doc.ProjectID != strconv.FormatInt(a.project, 10) || doc.SourceInstance != a.instance {
		return eventconnector.ImmutableEvent{}, errors.New("source row binding failure")
	}
	return s.event(doc, wire)
}
