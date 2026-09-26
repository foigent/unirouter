package sourcefacts

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/eventconnector"
)

// All advertised capabilities are explicit implementation inputs. This creates
// no route, policy, credential, storage binding, or capture-health assertion.
type ContentReadHandlerConfig struct {
	Authority                                           ContentBodyAuthority
	Store                                               *contentcapture.FileStore
	SupportedProtocols, ObservationPoints, CaptureModes []string
	Limits                                              ContentLimits
	MaxPageBytes, MaxVerifyBytesPerPage                 int64
	MaxConcurrentRequests                               int
	RequestTimeout, WriteTimeout                        time.Duration
}
type ContentReadHandlers struct{ Capabilities, Manifests http.Handler }
type contentReadHandler struct {
	config    ContentReadHandlerConfig
	authority *contentBodyDBAuthority
	slots     chan struct{}
	verify    func(context.Context, contentcapture.StoredReceipt, contentcapture.StoreLimits) (*os.File, error)
}

func NewContentReadHandlers(c ContentReadHandlerConfig) (ContentReadHandlers, error) {
	a, ok := c.Authority.(*contentBodyDBAuthority)
	if !ok || a == nil || a.source == nil || !a.source.ContentEnabled() || a.begin == nil || len(a.source.options.CursorSecret) == 0 || c.Limits.Validate() != nil || c.Limits.MaxManifestBytes > 65536 || c.Limits.MaxReadBytes < c.Limits.MaxSavedBytes || c.Limits.MaxBufferBytes > c.Limits.MaxReadBytes || c.MaxPageBytes <= 0 || c.MaxPageBytes > MaxPageBytes || c.MaxVerifyBytesPerPage <= 0 || c.MaxVerifyBytesPerPage > contentMaxSafeInteger || c.MaxConcurrentRequests <= 0 || c.MaxConcurrentRequests > 1024 || c.RequestTimeout <= 0 || c.WriteTimeout <= 0 {
		return ContentReadHandlers{}, errors.New("content reads require real authority and finite implementation limits")
	}
	c.SupportedProtocols = slices.Clone(c.SupportedProtocols)
	c.ObservationPoints = slices.Clone(c.ObservationPoints)
	c.CaptureModes = slices.Clone(c.CaptureModes)
	// The schema knows redacted, but this implementation has no redaction writer.
	if slices.Contains(c.CaptureModes, "redacted") || (slices.Contains(c.CaptureModes, "full_content") && c.Store == nil) {
		return ContentReadHandlers{}, errors.New("content capture mode is not implemented or configured")
	}
	probe := ContentCapabilities{SchemaVersion: ContentCapabilitiesVersion, Profile: ContentProfile, SourceInstance: a.source.SourceInstance(), OrgID: "1", ProjectID: "1", Status: "active", CredentialStatus: "feed_active", SupportedProtocols: c.SupportedProtocols, ObservationPoints: c.ObservationPoints, CaptureModes: c.CaptureModes, AllowedPurposes: []string{ContentHumanReview}, ContentScopeRevision: "1", RecoveryEpoch: "1", CoverageFrom: ContentBoundary{Time: "2000-01-01T00:00:00.000000Z", AfterSequence: "0"}, RetentionFloor: ContentBoundary{Time: "2000-01-01T00:00:00.000000Z", AfterSequence: "0"}, Limits: c.Limits, Endpoints: contentReadEndpoints()}
	if err := probe.Validate(); err != nil {
		return ContentReadHandlers{}, err
	}
	h := &contentReadHandler{config: c, authority: a, slots: make(chan struct{}, c.MaxConcurrentRequests)}
	if c.Store != nil {
		h.verify = c.Store.OpenVerified
	}
	return ContentReadHandlers{Capabilities: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.serve(w, r, false) }), Manifests: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.serve(w, r, true) })}, nil
}
func contentReadEndpoints() ContentEndpoints {
	return ContentEndpoints{Capabilities: ContentCapabilitiesPath, Manifests: ContentManifestsPath, Content: ContentEndpointTemplate}
}

type contentPageQuery struct {
	limit  int
	cursor string
}

func parseContentPageQuery(r *http.Request, maximum int, page bool) (contentPageQuery, bool) {
	var result contentPageQuery
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return result, false
	}
	if !page {
		return result, len(q) == 0
	}
	if len(q) < 2 || len(q) > 3 {
		return result, false
	}
	for k, v := range q {
		if len(v) != 1 || !oneOf(k, "limit", "cursor", "purpose") {
			return result, false
		}
	}
	if q.Get("purpose") != ContentHumanReview {
		return result, false
	}
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit <= 0 || limit > maximum || strconv.Itoa(limit) != q.Get("limit") {
		return result, false
	}
	result.limit = limit
	if values, exists := q["cursor"]; exists {
		if values[0] == "" || !contentReadCursorPattern.MatchString(values[0]) {
			return result, false
		}
		result.cursor = values[0]
	}
	return result, true
}

func (h *contentReadHandler) serve(w http.ResponseWriter, r *http.Request, page bool) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		contentBodyFailure(w, &ContentHTTPFailure{405, "method_not_allowed", false})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.config.RequestTimeout)
	defer cancel()
	r = r.WithContext(ctx)
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		contentBodyFailure(w, contentBodyUnavailable())
		return
	}
	grant, failure := h.authority.Authenticate(ctx, r)
	if failure != nil {
		contentBodyFailure(w, failure)
		return
	}
	query, ok := parseContentPageQuery(r, h.config.Limits.MaxManifestItems, page)
	if !ok {
		contentBodyFailure(w, &ContentHTTPFailure{400, "invalid_request", false})
		return
	}
	for name := range r.Header {
		if strings.EqualFold(name, "Range") {
			contentBodyFailure(w, &ContentHTTPFailure{400, "invalid_request", false})
			return
		}
	}
	snapshot, entries, failure := h.snapshot(ctx, grant, query, page)
	if failure != nil {
		contentBodyFailure(w, failure)
		return
	}
	var wire []byte
	var err error
	if page {
		h.evaluate(ctx, &snapshot, entries)
		if ctx.Err() != nil {
			contentBodyFailure(w, contentBodyUnavailable())
			return
		}
		wire, err = EncodeContentManifestPage(snapshot.page)
	} else {
		wire, err = EncodeContentCapabilities(snapshot.capabilities)
	}
	if err != nil || (page && int64(len(wire)) > h.config.MaxPageBytes) {
		contentBodyFailure(w, contentBodyUnavailable())
		return
	}
	// File verification and serialization may take time. No response can expose
	// references until the original credential and scope pass a NEW snapshot.
	if failure = h.authority.recheckScope(ctx, r, grant); failure != nil {
		contentBodyFailure(w, failure)
		return
	}
	if ctx.Err() != nil {
		contentBodyFailure(w, contentBodyUnavailable())
		return
	}
	controller := http.NewResponseController(w)
	deadline := time.Now().Add(h.config.WriteTimeout)
	if requestDeadline, ok := ctx.Deadline(); ok && requestDeadline.Before(deadline) {
		deadline = requestDeadline
	}
	if err = controller.SetWriteDeadline(deadline); err != nil {
		contentBodyFailure(w, contentBodyUnavailable())
		return
	}
	defer controller.SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(wire)))
	_, _ = w.Write(wire)
}

type contentReadSnapshot struct {
	page         ContentManifestPage
	capabilities ContentCapabilities
}
type contentPageObject struct {
	receipt contentcapture.StoredReceipt
	state   string
}
type contentManifestIndex struct {
	sequence, size, revision int64
	fact, request, hash      string
}

func (h *contentReadHandler) snapshot(ctx context.Context, grant ContentBodyGrant, q contentPageQuery, page bool) (contentReadSnapshot, []contentPageObject, *ContentHTTPFailure) {
	var result contentReadSnapshot
	if !h.authority.validProof(grant) {
		return result, nil, contentBodyDenied()
	}
	tx, failure := h.authority.beginRead(ctx)
	if failure != nil {
		return result, nil, failure
	}
	defer tx.Rollback(context.Background())
	access, failure := h.authority.checkOriginal(ctx, tx, grant, grant.proof.clientIP)
	if failure != nil {
		return result, nil, failure
	}
	var floor, high, epoch int64
	var floorTime, now time.Time
	var source string
	err := tx.QueryRow(ctx, `SELECT source_instance,retained_after_sequence,retained_after_time,high_sequence,recovery_epoch,clock_timestamp() FROM execution_content_streams WHERE org_id=$1 AND project_id=$2`, access.org, access.project).Scan(&source, &floor, &floorTime, &high, &epoch, &now)
	if err != nil || source != access.instance || floor < 0 || high < floor || epoch != access.epoch || high != access.high {
		return result, nil, contentBodyUnavailable()
	}
	coverage := ContentBoundary{Time: contentReadTime(*access.coverageTime), AfterSequence: strconv.FormatInt(access.coverage, 10)}
	retention := ContentBoundary{Time: contentReadTime(floorTime), AfterSequence: strconv.FormatInt(floor, 10)}
	result.capabilities = ContentCapabilities{SchemaVersion: ContentCapabilitiesVersion, Profile: ContentProfile, SourceInstance: grant.SourceInstance, OrgID: grant.OrgID, ProjectID: grant.ProjectID, Status: "active", CredentialStatus: access.keyStatus, SupportedProtocols: h.config.SupportedProtocols, ObservationPoints: h.config.ObservationPoints, CaptureModes: h.config.CaptureModes, AllowedPurposes: slices.Clone(access.purposes), ContentScopeRevision: grant.ContentScopeRevision, RecoveryEpoch: strconv.FormatInt(epoch, 10), CoverageFrom: coverage, RetentionFloor: retention, Limits: h.config.Limits, Endpoints: contentReadEndpoints()}
	if !page {
		if tx.Commit(ctx) != nil {
			return result, nil, contentBodyUnavailable()
		}
		return result, nil, nil
	}
	scope := ContentCursorScope{SourceInstance: source, OrgID: access.org, ProjectID: access.project, Profile: ContentProfile, AccessGeneration: access.generation, RecoveryEpoch: epoch}
	after := max(access.coverage, floor)
	if q.cursor != "" {
		after, err = DecodeContentCursor([]byte(h.authority.source.options.CursorSecret), q.cursor, scope, floor)
		if err != nil {
			return result, nil, contentCursorFailure(err)
		}
	}
	if after < access.coverage || after > high {
		return result, nil, contentCursorFailure(ErrContentInvalidCursor)
	}
	highCursor, err := EncodeContentCursor([]byte(h.authority.source.options.CursorSecret), scope, high)
	if err != nil {
		return result, nil, contentBodyUnavailable()
	}
	result.page = ContentManifestPage{SchemaVersion: ContentManifestPageVersion, Profile: ContentProfile, SourceInstance: source, OrgID: grant.OrgID, ProjectID: grant.ProjectID, GeneratedAt: contentReadTime(now), ContentScopeRevision: grant.ContentScopeRevision, RecoveryEpoch: strconv.FormatInt(epoch, 10), CoverageFrom: coverage, RetentionFloor: retention, AfterSequence: strconv.FormatInt(after, 10), LastSequence: strconv.FormatInt(after, 10), HighWatermarkSequence: strconv.FormatInt(high, 10), NextCursor: highCursor, HighWatermark: highCursor, Coverage: "unknown", Gaps: []ContentGap{{Reason: "capture_health_unknown", RangeKnown: false}}, Items: []ContentManifestEntry{}}
	knownGap, healthErr := contentKnownHealthGap(ctx, tx, access.org, access.project, epoch)
	if healthErr != nil {
		return result, nil, contentBodyUnavailable()
	}
	if knownGap {
		result.page.Gaps = append(result.page.Gaps, ContentGap{Reason: "manifest_write_unavailable", RangeKnown: false})
	}
	if floor > access.coverage {
		result.page.Gaps = append(result.page.Gaps, ContentGap{Reason: "retention_floor_ahead", RangeKnown: true, AfterSequence: strconv.FormatInt(access.coverage, 10), ThroughSequence: strconv.FormatInt(floor, 10)})
	}
	// Metadata only: a full limit+1 batch can never bring body-sized documents
	// into the driver before cumulative page admission has been decided.
	rows, err := tx.Query(ctx, `SELECT sequence,octet_length(document)::bigint,source_fact_id,revision,request_id,source_hash FROM execution_content_manifests WHERE org_id=$1 AND project_id=$2 AND sequence>$3 AND sequence<=$4 ORDER BY sequence LIMIT $5`, access.org, access.project, after, high, q.limit+1)
	if err != nil {
		return result, nil, contentBodyUnavailable()
	}
	indexes := make([]contentManifestIndex, 0, q.limit+1)
	for rows.Next() {
		if len(indexes) >= q.limit+1 {
			rows.Close()
			return result, nil, contentBodyUnavailable()
		}
		var index contentManifestIndex
		if rows.Scan(&index.sequence, &index.size, &index.fact, &index.revision, &index.request, &index.hash) != nil {
			rows.Close()
			return result, nil, contentBodyUnavailable()
		}
		indexes = append(indexes, index)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, nil, contentBodyUnavailable()
	}
	// Reserve the envelope at its largest sequence width and false boolean size.
	// Each admitted entry separately budgets its worst-case availability encoding.
	budgetPage := result.page
	budgetPage.LastSequence = strconv.FormatInt(high, 10)
	envelope, _ := json.Marshal(budgetPage)
	remaining := h.config.MaxPageBytes - int64(len(envelope))
	objects := make([]contentPageObject, 0, min(len(indexes), q.limit))
	last := after
	if len(indexes) == 0 && after < high {
		return result, nil, contentBodyUnavailable()
	}
	for i, index := range indexes {
		if i >= q.limit {
			break
		}
		if index.sequence <= last || index.sequence > high || index.size < 2 || index.size > h.config.Limits.MaxManifestBytes || index.size > 65536 || !contentFactPattern.MatchString(index.fact) || index.revision <= 0 || !contentText(index.request, 64) || !validHash(index.hash) {
			return result, nil, contentBodyUnavailable()
		}
		cost := contentManifestEntryBudget(index, now)
		if cost > remaining {
			if len(result.page.Items) == 0 {
				return result, nil, contentBodyUnavailable()
			}
			break
		}
		var document []byte
		err = tx.QueryRow(ctx, `SELECT document FROM execution_content_manifests WHERE org_id=$1 AND project_id=$2 AND sequence=$3 AND octet_length(document)=$4`, access.org, access.project, index.sequence, index.size).Scan(&document)
		if err != nil || int64(len(document)) != index.size {
			return result, nil, contentBodyUnavailable()
		}
		m, decodeErr := DecodeContentManifest(document)
		if decodeErr != nil || eventconnector.HashSourceBytes(document) != index.hash || m.SourceInstance != source || m.OrgID != grant.OrgID || m.ProjectID != grant.ProjectID || m.SourceFactID != index.fact || m.Revision != strconv.FormatInt(index.revision, 10) || m.RequestID != index.request {
			return result, nil, contentBodyUnavailable()
		}
		object, failure := contentReadCatalog(ctx, tx, access, m, now)
		if failure != nil {
			return result, nil, failure
		}
		result.page.Items = append(result.page.Items, ContentManifestEntry{Sequence: strconv.FormatInt(index.sequence, 10), Document: m, SourceSHA256: index.hash, Availability: contentAvailability(object.state, now)})
		objects = append(objects, object)
		remaining -= cost
		last = index.sequence
	}
	result.page.LastSequence = strconv.FormatInt(last, 10)
	result.page.HasMore = last < high
	result.page.NextCursor, err = EncodeContentCursor([]byte(h.authority.source.options.CursorSecret), scope, last)
	if err != nil || tx.Commit(ctx) != nil {
		return result, nil, contentBodyUnavailable()
	}
	return result, objects, nil
}

func contentManifestEntryBudget(index contentManifestIndex, now time.Time) int64 {
	// RawMessage is an empty-object stand-in: document bytes are admitted before
	// fetching. Canonical output never HTML-escapes immutable document strings.
	entry := struct {
		Sequence     string              `json:"sequence"`
		Document     json.RawMessage     `json:"document"`
		SourceSHA256 string              `json:"source_sha256"`
		Availability ContentAvailability `json:"availability"`
	}{strconv.FormatInt(index.sequence, 10), json.RawMessage(`{}`), index.hash, contentAvailability("source_unavailable", now)}
	wire, _ := json.Marshal(entry)
	return index.size + int64(len(wire)) - 2 + 2 // comma plus retryable=false can add one byte
}
func contentReadCatalog(ctx context.Context, tx pgx.Tx, a contentBodyAccess, m ContentManifest, now time.Time) (contentPageObject, *ContentHTTPFailure) {
	if m.Capture.Completeness == "pending" {
		return contentPageObject{state: "pending"}, nil
	}
	if m.Capture.Completeness == "not_captured" {
		return contentPageObject{state: "not_captured"}, nil
	}
	expires, _ := time.Parse(time.RFC3339Nano, m.ExpiresAt)
	if !now.Before(expires) {
		return contentPageObject{state: "expired"}, nil
	}
	var o contentPageObject
	var state string
	var digest *string
	var storedExpires time.Time
	revision, _ := strconv.ParseInt(m.Revision, 10, 64)
	err := tx.QueryRow(ctx, `SELECT storage_identity,storage_binding_token,relative_path,state,saved_bytes,digest,expires_at FROM execution_content_objects WHERE org_id=$1 AND project_id=$2 AND source_fact_id=$3 AND revision=$4 AND content_ref=$5`, a.org, a.project, m.SourceFactID, revision, m.ContentRef).Scan(&o.receipt.Binding.Identity, &o.receipt.Binding.Token, &o.receipt.ObjectName, &state, &o.receipt.SavedBytes, &digest, &storedExpires)
	if errors.Is(err, pgx.ErrNoRows) {
		return contentPageObject{state: "missing"}, nil
	}
	if err != nil {
		return o, contentBodyUnavailable()
	}
	if state == "reserved" {
		return contentPageObject{state: "pending"}, nil
	}
	if state == "orphan" {
		return contentPageObject{state: "missing"}, nil
	}
	if state != "ready" || !contentText(o.receipt.Binding.Identity, 128) || !validHash("sha256:"+o.receipt.Binding.Token) || !validHash("sha256:"+o.receipt.ObjectName) || digest == nil || *digest != m.Representation.Digest || o.receipt.SavedBytes != m.Representation.SavedBytes || !storedExpires.Equal(expires) {
		return contentPageObject{state: "corrupt"}, nil
	}
	o.receipt.SHA256 = *digest
	o.state = "verify"
	return o, nil
}
func (h *contentReadHandler) evaluate(ctx context.Context, snapshot *contentReadSnapshot, objects []contentPageObject) {
	remaining := h.config.MaxVerifyBytesPerPage
	for i, object := range objects {
		if ctx.Err() != nil {
			return
		}
		if object.state != "verify" {
			continue
		}
		m := snapshot.page.Items[i].Document
		now := time.Now()
		expires, _ := time.Parse(time.RFC3339Nano, m.ExpiresAt)
		state := "source_unavailable"
		if !now.Before(expires) {
			state = "expired"
		} else if h.verify != nil && object.receipt.SavedBytes <= h.config.Limits.MaxReadBytes && object.receipt.SavedBytes <= remaining {
			remaining -= object.receipt.SavedBytes
			f, err := h.verify(ctx, object.receipt, contentcapture.StoreLimits{MaxSavedBytes: h.config.Limits.MaxReadBytes, MaxReadBytes: h.config.Limits.MaxReadBytes, MaxBufferBytes: h.config.Limits.MaxBufferBytes})
			if err == nil {
				if f == nil {
					state = "source_unavailable"
				} else {
					state = "available"
					if f.Close() != nil {
						state = "source_unavailable"
					}
				}
			} else if errors.Is(err, os.ErrNotExist) {
				state = "missing"
			} else if errors.Is(err, contentcapture.ErrStoreIntegrity) || errors.Is(err, contentcapture.ErrStoreBinding) || errors.Is(err, contentcapture.ErrStorePath) {
				state = "corrupt"
			}
		}
		now = time.Now()
		if state == "available" && !now.Before(expires) {
			state = "expired"
		}
		snapshot.page.Items[i].Availability = contentAvailability(state, now)
	}
}
func contentAvailability(state string, now time.Time) ContentAvailability {
	return ContentAvailability{State: state, EvaluatedAt: contentReadTime(now), Retryable: state == "pending" || state == "source_unavailable"}
}
func contentReadTime(t time.Time) string {
	return t.UTC().Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z")
}
func contentCursorFailure(err error) *ContentHTTPFailure {
	switch {
	case errors.Is(err, ErrContentScopeChanged):
		return &ContentHTTPFailure{409, "content_scope_changed", false}
	case errors.Is(err, ErrContentEpochChanged):
		return &ContentHTTPFailure{409, "source_epoch_changed", false}
	case errors.Is(err, ErrContentCursorExpired):
		return &ContentHTTPFailure{410, "cursor_expired", false}
	default:
		return &ContentHTTPFailure{400, "invalid_cursor", false}
	}
}
