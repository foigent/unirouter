package sourcefacts

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/middleware"
)

type BudgetDocument struct {
	Profile      string          `json:"profile"`
	Document     json.RawMessage `json:"document"`
	SourceSHA256 string          `json:"source_sha256"`
}
type BudgetReading struct {
	SchemaVersion      string           `json:"schema_version"`
	SourceInstance     string           `json:"source_instance"`
	OrgID              string           `json:"org_id"`
	ProjectID          string           `json:"project_id"`
	ReadingID          string           `json:"reading_id"`
	ReadingCutoff      string           `json:"reading_cutoff"`
	ObservedAt         string           `json:"observed_at"`
	BudgetRef          string           `json:"budget_ref"`
	DefinitionRevision string           `json:"definition_revision"`
	BindingRevision    string           `json:"binding_revision,omitempty"`
	AccessGeneration   string           `json:"access_generation"`
	CatalogRevision    string           `json:"catalog_revision"`
	Documents          []BudgetDocument `json:"documents"`
}

func canonicalBudgetDocument(base BudgetReading, profile string, body any) (BudgetDocument, error) {
	wire, err := json.Marshal(map[string]any{"schema_version": BudgetSnapshotVersion, "source_instance": base.SourceInstance, "org_id": base.OrgID, "project_id": base.ProjectID,
		"reading_id": base.ReadingID, "reading_cutoff": base.ReadingCutoff, "observed_at": base.ObservedAt, "budget_ref": base.BudgetRef, "definition_revision": base.DefinitionRevision,
		"binding_revision": base.BindingRevision, "profile": profile, "body": body})
	if err != nil {
		return BudgetDocument{}, err
	}
	var value any
	if err = json.Unmarshal(wire, &value); err != nil {
		return BudgetDocument{}, err
	}
	canonical, canonicalErr := eventconnector.CanonicalJSON(value)
	if canonicalErr != nil || len(canonical) > 65536 {
		return BudgetDocument{}, errBudgetSourceData
	}
	return BudgetDocument{profile, canonical, eventconnector.HashSourceBytes(canonical)}, nil
}

func (s *Service) budgetCursorScope(a budgetAccess, kind string) budgetCursor {
	return budgetCursor{Version: BudgetSnapshotVersion, Kind: kind, Instance: a.instance, Org: a.org, Project: a.project, Key: a.keyID, AccessGeneration: a.dGeneration, CatalogRevision: a.catalogRevision}
}

func (s *Service) BudgetHandler(resolver middleware.ClientIPResolver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeError(w, 405, "method_not_allowed", false)
			return
		}
		if !s.BudgetsEnabled() {
			writeError(w, 503, "source_disabled", true)
			return
		}
		current := strings.HasSuffix(r.URL.Path, "/budgets/current")
		if !current && !strings.HasSuffix(r.URL.Path, "/budgets") {
			writeError(w, 404, "not_found", false)
			return
		}
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			writeError(w, 400, "invalid_query", false)
			return
		}
		for key, values := range query {
			if len(values) != 1 || (!current && !oneOf(key, "cursor", "limit")) || (current && !oneOf(key, "budget_ref", "alerts_cursor", "limit")) {
				writeError(w, 400, "invalid_query", false)
				return
			}
		}
		limit := 50
		if query.Has("limit") {
			limit, err = strconv.Atoi(query.Get("limit"))
			if err != nil || limit < 1 || limit > 50 {
				writeError(w, 400, "invalid_limit", false)
				return
			}
		}
		if current && !opaque(query.Get("budget_ref"), 256) {
			writeError(w, 400, "invalid_budget_ref", false)
			return
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
		original, err := s.authenticateTx(ctx, tx, r, resolver)
		if err != nil {
			if errors.Is(err, errSourceAccessDenied) {
				writeError(w, 401, "source_credential_denied", false)
			} else {
				writeError(w, 503, "source_unavailable", true)
			}
			return
		}
		allowed := []string{}
		for _, p := range BudgetProfiles() {
			if slices.Contains(original.profiles, p) && slices.Contains(original.credentialProfiles, p) {
				allowed = append(allowed, p)
			}
		}
		if len(allowed) == 0 {
			writeError(w, 403, "profile_not_authorized", false)
			return
		}
		if original.status != "active" {
			writeError(w, 503, "source_paused", true)
			return
		}
		if original.keyStatus != "feed_active" {
			writeError(w, 409, "staged_credential_requires_pause", false)
			return
		}
		a := budgetAccess{access: original}
		if err = tx.QueryRow(ctx, `SELECT d_access_generation,catalog_revision FROM execution_source_budget_scopes WHERE source_instance=$1 AND org_id=$2 AND project_id=$3 FOR SHARE`, a.instance, a.org, a.project).Scan(&a.dGeneration, &a.catalogRevision); err != nil {
			writeError(w, 503, "source_unavailable", true)
			return
		}
		var cutoff time.Time
		if err = tx.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&cutoff); err != nil {
			writeError(w, 503, "source_unavailable", true)
			return
		}
		var output any
		if !current {
			after := ""
			if query.Get("cursor") != "" {
				c, e := s.decodeBudgetCursor(query.Get("cursor"), "catalog", a)
				if e != nil {
					writeError(w, 400, "invalid_cursor", false)
					return
				}
				after = c.AfterRef
			}
			entries, e := readBudgetCatalogTx(ctx, tx, a, after, limit)
			if e != nil {
				writeError(w, 503, "source_unavailable", true)
				return
			}
			hasMore := len(entries) > limit
			next := ""
			if hasMore {
				entries = entries[:limit]
				c := s.budgetCursorScope(a, "catalog")
				c.AfterRef = entries[len(entries)-1].BudgetRef
				next, e = s.encodeBudgetCursor(c)
				if e != nil {
					writeError(w, 503, "source_unavailable", true)
					return
				}
			}
			var hasBinding bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM execution_source_budget_bindings WHERE org_id=$1 AND project_id=$2 AND status='active')`, a.org, a.project).Scan(&hasBinding); err != nil {
				writeError(w, 503, "source_unavailable", true)
				return
			}
			bindingState := "scope_not_configured"
			if hasBinding {
				bindingState = "explicit_project_binding"
			}
			output = map[string]any{"schema_version": "unirouter.budget-catalog.v1", "source_instance": a.instance, "org_id": executioncontext.ID(a.org), "project_id": executioncontext.ID(a.project), "reading_cutoff": executioncontext.Time(cutoff), "catalog_revision": executioncontext.ID(a.catalogRevision), "access_generation": executioncontext.ID(a.dGeneration), "profiles": allowed, "enterprise_scope": bindingState, "entries": entries, "has_more": hasMore, "next_cursor": next}
		} else {
			record, e := readBudgetRecordTx(ctx, tx, a, query.Get("budget_ref"))
			if e != nil {
				if errors.Is(e, pgx.ErrNoRows) {
					writeError(w, 404, "budget_not_found", false)
				} else {
					writeError(w, 503, "source_unavailable", true)
				}
				return
			}
			after := int64(0)
			if query.Get("alerts_cursor") != "" {
				if !slices.Contains(allowed, BudgetAlertProfile) {
					writeError(w, 403, "profile_not_authorized", false)
					return
				}
				c, e := s.decodeBudgetCursor(query.Get("alerts_cursor"), "alerts", a)
				if e != nil || c.BudgetRef != record.entry.BudgetRef || executioncontext.ID(c.DefinitionRevision) != record.entry.DefinitionRevision || c.BindingRevision != record.bindingRevision {
					writeError(w, 400, "invalid_cursor", false)
					return
				}
				after = c.AfterAlertID
			}
			bodies := map[string]any{}
			if slices.Contains(allowed, BudgetDefinitionProfile) {
				bodies[BudgetDefinitionProfile] = record.definition
			}
			if slices.Contains(allowed, BudgetSummaryProfile) {
				var summary BudgetPeriodSummary
				if record.entry.Origin == "gateway_project" {
					summary, e = s.gatewayBudgetSummaryTx(ctx, tx, a, record.definition, cutoff)
				} else {
					summary, e = enterpriseBudgetSummaryTx(ctx, tx, a, record, cutoff)
				}
				if e != nil {
					writeError(w, 503, "source_unavailable", true)
					return
				}
				bodies[BudgetSummaryProfile] = summary
			}
			if slices.Contains(allowed, BudgetAlertProfile) {
				alerts, last, e := readBudgetAlertsTx(ctx, tx, a, record, after, limit, cutoff)
				if e != nil {
					writeError(w, 503, "source_unavailable", true)
					return
				}
				if alerts.HasMore {
					c := s.budgetCursorScope(a, "alerts")
					c.BudgetRef = record.entry.BudgetRef
					c.DefinitionRevision, _ = strconv.ParseInt(record.entry.DefinitionRevision, 10, 64)
					c.BindingRevision = record.bindingRevision
					c.AfterAlertID = last
					alerts.NextCursor, e = s.encodeBudgetCursor(c)
					if e != nil {
						writeError(w, 503, "source_unavailable", true)
						return
					}
				}
				bodies[BudgetAlertProfile] = alerts
			}
			var id [16]byte
			if _, err = rand.Read(id[:]); err != nil {
				writeError(w, 503, "source_unavailable", true)
				return
			}
			var observed time.Time
			if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&observed); err != nil {
				writeError(w, 503, "source_unavailable", true)
				return
			}
			reading := BudgetReading{SchemaVersion: BudgetSnapshotVersion, SourceInstance: a.instance, OrgID: executioncontext.ID(a.org), ProjectID: executioncontext.ID(a.project), ReadingID: "ur-budget-reading-" + hex.EncodeToString(id[:]), ReadingCutoff: executioncontext.Time(cutoff), ObservedAt: executioncontext.Time(observed), BudgetRef: record.entry.BudgetRef, DefinitionRevision: record.entry.DefinitionRevision, BindingRevision: record.entry.BindingRevision, AccessGeneration: executioncontext.ID(a.dGeneration), CatalogRevision: executioncontext.ID(a.catalogRevision), Documents: []BudgetDocument{}}
			for _, profile := range allowed {
				doc, e := canonicalBudgetDocument(reading, profile, bodies[profile])
				if e != nil {
					writeError(w, 503, "source_unavailable", true)
					return
				}
				reading.Documents = append(reading.Documents, doc)
			}
			output = reading
		}
		wire, err := budgetResponseBytes(output)
		if err != nil || len(wire) > 8<<20 {
			writeError(w, 503, "source_unavailable", true)
			return
		}
		if err = tx.Commit(ctx); err != nil {
			writeError(w, 503, "source_unavailable", true)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(wire)
	})
}

// Preserve the already canonical raw document bytes, including HTML characters.
func budgetResponseBytes(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
