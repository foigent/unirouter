package sourcefacts

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/ipallowlist"
	"github.com/uniroute/unirouter/internal/middleware"
)

// The proof never leaves this package or substitutes for current DB checks.
// It contains no bearer token and is bound to the original authority instance.
type contentBodyProof struct {
	owner                                                            *contentBodyDBAuthority
	keyID, org, project, generation, epoch, safety, coverage         int64
	instance, registrationHash, keyHash, prefix, operation, clientIP string
	coverageTime                                                     time.Time
	purposes                                                         []string
	selectionMu                                                      sync.Mutex
	selection                                                        *contentBodySelection
}

// One fixed selection per HTTP read, not an authorization cache. It binds the
// original object token/path and manifest hash across all later block checks.
type contentBodySelection struct {
	selector   ContentBodySelector
	receipt    contentcapture.StoredReceipt
	sourceHash string
}

type contentBodyDBAuthority struct {
	source   *Service
	resolver middleware.ClientIPResolver
	begin    func(context.Context) (pgx.Tx, error)
}

// NewContentBodyAuthority creates an unregistered read authority. An independent
// pool is required so each method, including every delivery-block recheck, gets
// a fresh short snapshot rather than nesting into an earlier transaction.
// Capture policy does not authorize or revoke access to retained content.
func (s *Service) NewContentBodyAuthority(resolver middleware.ClientIPResolver) (ContentBodyAuthority, error) {
	if !s.ContentEnabled() {
		return nil, errors.New("content source contract is not enabled")
	}
	pool, ok := s.db.(*pgxpool.Pool)
	if !ok || pool == nil {
		return nil, errors.New("content body authority requires an independent connection pool")
	}
	return &contentBodyDBAuthority{source: s, resolver: resolver, begin: func(ctx context.Context) (pgx.Tx, error) {
		return pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	}}, nil
}

type contentBodyAccess struct {
	keyID, org, project                                                                   int64
	keyStatus, ips, scope, instance, global, registrationHash, prefix, keyHash, operation string
	profiles                                                                              []string
	hasScope                                                                              bool
	local                                                                                 string
	generation, safety, epoch, high, coverage                                             int64
	streamInstance                                                                        string
	coverageTime                                                                          *time.Time
	purposes                                                                              []string
}

const contentBodyAccessSQL = `SELECT ak.id,ak.org_id,ak.project_id,ak.status,ak.allowed_ips,ak.source_lifecycle_scope,
c.source_instance,c.status,sc.registration_sha256,ak.key_prefix,ak.key_hash,ak.registration_operation_id,sc.profiles,
cs.project_id IS NOT NULL,COALESCE(cs.status,''),COALESCE(cs.access_generation,0),c.content_safety_version,
COALESCE(st.recovery_epoch,0),COALESCE(st.high_sequence,0),COALESCE(b.coverage_after_sequence,-1),
COALESCE(st.source_instance,''),b.coverage_time,COALESCE(b.allowed_purposes,ARRAY[]::text[])
FROM api_keys ak
JOIN execution_source_credentials sc ON sc.api_key_id=ak.id AND sc.org_id=ak.org_id AND sc.project_id=ak.project_id AND sc.source_lifecycle_scope=ak.source_lifecycle_scope
JOIN execution_source_connections c ON c.org_id=ak.org_id AND c.project_id=ak.project_id
JOIN orgs o ON o.id=ak.org_id
JOIN projects p ON p.id=ak.project_id AND p.org_id=ak.org_id
LEFT JOIN execution_source_content_scopes cs ON cs.org_id=ak.org_id AND cs.project_id=ak.project_id
LEFT JOIN execution_content_streams st ON st.org_id=cs.org_id AND st.project_id=cs.project_id
LEFT JOIN execution_source_content_bindings b ON b.api_key_id=ak.id AND b.org_id=ak.org_id AND b.project_id=ak.project_id AND b.source_lifecycle_scope='content'
WHERE ak.purpose='xinsight.source.read' AND ak.verifier_scheme='receiver_sha256_v1'
AND ak.key_type='service' AND ak.allowed_models='' AND ak.status IN ('feed_active','feed_staged')
AND o.status='active' AND p.status='active' AND ak.key_hash=$1 AND ak.key_prefix=$2
AND ($3::bigint=0 OR ak.id=$3) LIMIT 2`

func (a *contentBodyDBAuthority) readAccess(ctx context.Context, tx pgx.Tx, hash, prefix string, keyID int64) (contentBodyAccess, *ContentHTTPFailure) {
	var result contentBodyAccess
	rows, err := tx.Query(ctx, contentBodyAccessSQL, hash, prefix, keyID)
	if err != nil {
		return result, contentBodyUnavailable()
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count > 1 {
			return contentBodyAccess{}, contentBodyDenied()
		}
		err = rows.Scan(&result.keyID, &result.org, &result.project, &result.keyStatus, &result.ips, &result.scope, &result.instance, &result.global, &result.registrationHash, &result.prefix, &result.keyHash, &result.operation, &result.profiles, &result.hasScope, &result.local, &result.generation, &result.safety, &result.epoch, &result.high, &result.coverage, &result.streamInstance, &result.coverageTime, &result.purposes)
		if err != nil {
			return contentBodyAccess{}, contentBodyUnavailable()
		}
	}
	if rows.Err() != nil {
		return contentBodyAccess{}, contentBodyUnavailable()
	}
	if count != 1 {
		return contentBodyAccess{}, contentBodyDenied()
	}
	return result, nil
}

func (a *contentBodyDBAuthority) beginRead(ctx context.Context) (pgx.Tx, *ContentHTTPFailure) {
	if a == nil || a.source == nil || !a.source.ContentEnabled() || a.begin == nil || ctx.Err() != nil {
		return nil, contentBodyUnavailable()
	}
	tx, err := a.begin(ctx)
	if err != nil {
		return nil, contentBodyUnavailable()
	}
	return tx, nil
}

func (a *contentBodyDBAuthority) Authenticate(ctx context.Context, r *http.Request) (ContentBodyGrant, *ContentHTTPFailure) {
	if a == nil || a.source == nil {
		return ContentBodyGrant{}, contentBodyUnavailable()
	}
	hash, prefix, verifier, failure := a.requestCredential(r)
	if failure != nil {
		return ContentBodyGrant{}, failure
	}
	tx, failure := a.beginRead(ctx)
	if failure != nil {
		return ContentBodyGrant{}, failure
	}
	defer tx.Rollback(context.Background())
	access, failure := a.readAccess(ctx, tx, hash, prefix, 0)
	if failure != nil {
		return ContentBodyGrant{}, failure
	}
	ip := a.resolver.Resolve(r)
	if failure = a.validateAccess(access, ip); failure != nil {
		return ContentBodyGrant{}, failure
	}
	registration := Registration{SchemaVersion: RegistrationVersion, TokenFormat: auth.ReceiverFeedTokenFormat, VerifierScheme: auth.ReceiverFeedVerifierScheme, KeyPrefix: access.prefix, VerifierSHA256: verifier, OperationID: access.operation, SourceInstance: access.instance, OrgID: strconv.FormatInt(access.org, 10), ProjectID: strconv.FormatInt(access.project, 10), Profiles: access.profiles}
	digest, err := HashContentRegistration(registration)
	if err != nil || digest != access.registrationHash {
		return ContentBodyGrant{}, contentBodyDenied()
	}
	proof := &contentBodyProof{owner: a, keyID: access.keyID, org: access.org, project: access.project, generation: access.generation, epoch: access.epoch, safety: access.safety, coverage: access.coverage, instance: access.instance, registrationHash: access.registrationHash, keyHash: hash, prefix: prefix, operation: access.operation, clientIP: ip, coverageTime: *access.coverageTime, purposes: slices.Clone(access.purposes)}
	grant := access.grant(proof)
	if err := tx.Commit(ctx); err != nil {
		return ContentBodyGrant{}, contentBodyUnavailable()
	}
	return grant, nil
}

func (a *contentBodyDBAuthority) Lookup(ctx context.Context, grant ContentBodyGrant, selector ContentBodySelector) (ContentBodyObject, *ContentHTTPFailure) {
	if !a.validProof(grant) {
		return ContentBodyObject{}, contentBodyDenied()
	}
	if !validAuthoritySelector(selector) {
		return ContentBodyObject{}, &ContentHTTPFailure{400, "invalid_request", false}
	}
	tx, failure := a.beginRead(ctx)
	if failure != nil {
		return ContentBodyObject{}, failure
	}
	defer tx.Rollback(context.Background())
	access, failure := a.checkOriginal(ctx, tx, grant, grant.proof.clientIP)
	if failure != nil {
		return ContentBodyObject{}, failure
	}
	object, failure := a.lookupObject(ctx, tx, access, selector)
	if failure != nil {
		return ContentBodyObject{}, failure
	}
	if err := tx.Commit(ctx); err != nil {
		return ContentBodyObject{}, contentBodyUnavailable()
	}
	if failure := grant.proof.bindSelection(selector, object, true); failure != nil {
		return ContentBodyObject{}, failure
	}
	return object, nil
}

func (a *contentBodyDBAuthority) Reauthorize(ctx context.Context, r *http.Request, grant ContentBodyGrant, selector ContentBodySelector) (ContentBodyGrant, *ContentHTTPFailure) {
	if !a.validProof(grant) {
		return ContentBodyGrant{}, contentBodyDenied()
	}
	if !validAuthoritySelector(selector) {
		return ContentBodyGrant{}, &ContentHTTPFailure{400, "invalid_request", false}
	}
	hash, prefix, verifier, failure := a.requestCredential(r)
	if failure != nil {
		return ContentBodyGrant{}, failure
	}
	if hash != grant.proof.keyHash || prefix != grant.proof.prefix {
		return ContentBodyGrant{}, contentBodyDenied()
	}
	ip := a.resolver.Resolve(r)
	if ip != grant.proof.clientIP {
		return ContentBodyGrant{}, contentBodyDenied()
	}
	tx, failure := a.beginRead(ctx)
	if failure != nil {
		return ContentBodyGrant{}, failure
	}
	defer tx.Rollback(context.Background())
	access, failure := a.checkOriginal(ctx, tx, grant, ip)
	if failure != nil {
		return ContentBodyGrant{}, failure
	}
	registration := Registration{SchemaVersion: RegistrationVersion, TokenFormat: auth.ReceiverFeedTokenFormat, VerifierScheme: auth.ReceiverFeedVerifierScheme, KeyPrefix: access.prefix, VerifierSHA256: verifier, OperationID: access.operation, SourceInstance: access.instance, OrgID: strconv.FormatInt(access.org, 10), ProjectID: strconv.FormatInt(access.project, 10), Profiles: access.profiles}
	digest, err := HashContentRegistration(registration)
	if err != nil || digest != grant.proof.registrationHash {
		return ContentBodyGrant{}, contentBodyDenied()
	}
	// Re-read exact membership, purpose, lifetime and the original storage
	// binding on every block. There is no cached availability or policy bypass.
	object, failure := a.lookupObject(ctx, tx, access, selector)
	if failure != nil {
		return ContentBodyGrant{}, failure
	}
	if failure := grant.proof.bindSelection(selector, object, false); failure != nil {
		return ContentBodyGrant{}, failure
	}
	if err := tx.Commit(ctx); err != nil {
		return ContentBodyGrant{}, contentBodyUnavailable()
	}
	return access.grant(grant.proof), nil
}

func (a *contentBodyDBAuthority) requestCredential(r *http.Request) (string, string, string, *ContentHTTPFailure) {
	if r == nil {
		return "", "", "", contentBodyDenied()
	}
	var values []string
	for name, current := range r.Header {
		if strings.EqualFold(name, "Authorization") {
			values = append(values, current...)
		}
	}
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return "", "", "", contentBodyDenied()
	}
	raw := strings.TrimPrefix(values[0], "Bearer ")
	hash, prefix, err := auth.HashReceiverFeedCredential(raw, a.source.options.CursorSecret)
	if err != nil {
		return "", "", "", contentBodyDenied()
	}
	verifier, err := auth.ReceiverFeedVerifierHex(raw)
	if err != nil {
		return "", "", "", contentBodyDenied()
	}
	return hash, prefix, verifier, nil
}

// recheckScope is for discovery/page responses, which have no single body
// selection. It never creates or changes the proof's block-read selection.
func (a *contentBodyDBAuthority) recheckScope(ctx context.Context, r *http.Request, grant ContentBodyGrant) *ContentHTTPFailure {
	if !a.validProof(grant) {
		return contentBodyDenied()
	}
	hash, prefix, _, failure := a.requestCredential(r)
	if failure != nil {
		return failure
	}
	ip := a.resolver.Resolve(r)
	if hash != grant.proof.keyHash || prefix != grant.proof.prefix || ip != grant.proof.clientIP {
		return contentBodyDenied()
	}
	tx, failure := a.beginRead(ctx)
	if failure != nil {
		return failure
	}
	defer tx.Rollback(context.Background())
	if _, failure = a.checkOriginal(ctx, tx, grant, ip); failure != nil {
		return failure
	}
	if err := tx.Commit(ctx); err != nil {
		return contentBodyUnavailable()
	}
	return nil
}

func (a *contentBodyDBAuthority) validateAccess(v contentBodyAccess, ip string) *ContentHTTPFailure {
	if v.keyID <= 0 || v.org <= 0 || v.project <= 0 || v.instance != a.source.options.SourceInstance || v.global == "revoked" || strings.TrimSpace(v.ips) == "" || !ipallowlist.Matches(ip, v.ips) {
		return contentBodyDenied()
	}
	if failure := DecideContentReadR2(v.state()); failure != nil {
		return failure
	}
	if v.global != "active" || v.local == "revoked" {
		return contentBodyDenied()
	}
	if v.local != "active" || v.generation <= 0 || v.safety <= 0 || v.epoch <= 0 || v.streamInstance != v.instance || v.coverage < 0 || v.coverageTime == nil || v.high < 0 {
		return contentBodyUnavailable()
	}
	if len(v.purposes) != 1 || v.purposes[0] != ContentHumanReview {
		return &ContentHTTPFailure{404, "not_found", false}
	}
	if !validHash(v.registrationHash) {
		return contentBodyDenied()
	}
	return nil
}

func (a *contentBodyDBAuthority) validProof(g ContentBodyGrant) bool {
	p := g.proof
	return a != nil && p != nil && p.owner == a && p.keyID > 0 && validContentBodyGrant(g) && g.SourceInstance == p.instance && g.OrgID == strconv.FormatInt(p.org, 10) && g.ProjectID == strconv.FormatInt(p.project, 10) && g.ContentScopeRevision == strconv.FormatInt(p.generation, 10) && g.RecoveryEpoch == p.epoch
}

func (a *contentBodyDBAuthority) checkOriginal(ctx context.Context, tx pgx.Tx, g ContentBodyGrant, ip string) (contentBodyAccess, *ContentHTTPFailure) {
	p := g.proof
	v, failure := a.readAccess(ctx, tx, p.keyHash, p.prefix, p.keyID)
	if failure != nil {
		return v, failure
	}
	if failure = a.validateAccess(v, ip); failure != nil {
		return v, failure
	}
	if v.org != p.org || v.project != p.project || v.instance != p.instance {
		return v, &ContentHTTPFailure{404, "not_found", false}
	}
	if v.keyID != p.keyID || v.registrationHash != p.registrationHash || v.keyHash != p.keyHash || v.prefix != p.prefix || v.operation != p.operation {
		return v, contentBodyDenied()
	}
	if v.generation != p.generation || v.safety != p.safety || v.coverage != p.coverage || !v.coverageTime.Equal(p.coverageTime) || !slices.Equal(v.purposes, p.purposes) {
		return v, &ContentHTTPFailure{409, "content_scope_changed", false}
	}
	if v.epoch != p.epoch {
		return v, &ContentHTTPFailure{409, "source_epoch_changed", false}
	}
	return v, nil
}

func (v contentBodyAccess) state() ContentReadState {
	return ContentReadState{HasProfile: v.scope == "content" && v.hasScope && len(v.profiles) == 1 && v.profiles[0] == ContentProfile, GlobalPaused: v.global == "paused", ContentPaused: v.local == "paused", CredentialStatus: v.keyStatus}
}
func (v contentBodyAccess) grant(proof *contentBodyProof) ContentBodyGrant {
	return ContentBodyGrant{SourceInstance: v.instance, OrgID: strconv.FormatInt(v.org, 10), ProjectID: strconv.FormatInt(v.project, 10), ContentScopeRevision: strconv.FormatInt(v.generation, 10), RecoveryEpoch: v.epoch, State: v.state(), proof: proof}
}

func (a *contentBodyDBAuthority) lookupObject(ctx context.Context, tx pgx.Tx, access contentBodyAccess, selector ContentBodySelector) (ContentBodyObject, *ContentHTTPFailure) {
	var object ContentBodyObject
	if selector.Purpose != ContentHumanReview || !slices.Contains(access.purposes, selector.Purpose) {
		return object, &ContentHTTPFailure{404, "not_found", false}
	}
	revision, err := strconv.ParseInt(selector.Revision, 10, 64)
	if err != nil {
		return object, &ContentHTTPFailure{400, "invalid_request", false}
	}
	var wire []byte
	var hash string
	// Scope/request/coverage predicates precede any object/availability query.
	err = tx.QueryRow(ctx, `SELECT document,source_hash FROM execution_content_manifests
WHERE org_id=$1 AND project_id=$2 AND source_fact_id=$3 AND revision=$4 AND request_id=$5
AND sequence>$6 AND sequence<=$7`, access.org, access.project, selector.SourceFactID, revision, selector.RequestID, access.coverage, access.high).Scan(&wire, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return object, &ContentHTTPFailure{404, "not_found", false}
	}
	if err != nil {
		return object, contentBodyUnavailable()
	}
	manifest, err := DecodeContentManifest(wire)
	if err != nil || hash != eventconnector.HashSourceBytes(wire) {
		return object, &ContentHTTPFailure{503, "representation_mismatch", false}
	}
	if manifest.SourceInstance != access.instance || manifest.OrgID != strconv.FormatInt(access.org, 10) || manifest.ProjectID != strconv.FormatInt(access.project, 10) || manifest.RequestID != selector.RequestID || manifest.SourceFactID != selector.SourceFactID || manifest.Revision != selector.Revision {
		return object, &ContentHTTPFailure{503, "representation_mismatch", false}
	}
	if manifest.ContentRef != selector.ContentRef {
		return object, &ContentHTTPFailure{404, "not_found", false}
	}
	metadata, err := manifest.ReadMetadata()
	if err != nil || metadata.SourceSHA256 != hash {
		return object, &ContentHTTPFailure{503, "representation_mismatch", false}
	}
	expires, err := time.Parse(time.RFC3339Nano, metadata.ExpiresAt)
	if err != nil {
		return object, &ContentHTTPFailure{503, "representation_mismatch", false}
	}
	if !time.Now().Before(expires) {
		return object, &ContentHTTPFailure{410, "content_expired", false}
	}
	var identity, token, name, state string
	var saved int64
	var digest *string
	var objectExpires time.Time
	err = tx.QueryRow(ctx, `SELECT storage_identity,storage_binding_token,relative_path,state,saved_bytes,digest,expires_at
FROM execution_content_objects WHERE org_id=$1 AND project_id=$2 AND source_fact_id=$3 AND revision=$4 AND content_ref=$5`, access.org, access.project, selector.SourceFactID, revision, selector.ContentRef).Scan(&identity, &token, &name, &state, &saved, &digest, &objectExpires)
	if errors.Is(err, pgx.ErrNoRows) {
		return object, &ContentHTTPFailure{503, "content_missing", false}
	}
	if err != nil {
		return object, contentBodyUnavailable()
	}
	if state == "reserved" {
		return object, &ContentHTTPFailure{503, "content_pending", true}
	}
	if state == "orphan" {
		return object, &ContentHTTPFailure{503, "content_missing", false}
	}
	if state != "ready" || !contentText(identity, 128) || !validHash("sha256:"+token) || !validHash("sha256:"+name) || digest == nil || saved != metadata.Representation.SavedBytes || *digest != metadata.Representation.Digest || !objectExpires.Equal(expires) {
		return object, &ContentHTTPFailure{503, "representation_mismatch", false}
	}
	// Preserve the token on the original persistent reservation. Never fill it
	// from the currently open FileStore, even if the identity names happen to match.
	object.Receipt = contentcapture.StoredReceipt{Binding: contentcapture.StoreBinding{Identity: identity, Token: token}, ObjectName: name, SavedBytes: saved, SHA256: *digest}
	object.Metadata = metadata
	return object, nil
}

func validAuthoritySelector(s ContentBodySelector) bool {
	return contentText(s.RequestID, 64) && contentFactPattern.MatchString(s.SourceFactID) && positiveID(s.Revision) && contentRefPattern.MatchString(s.ContentRef) && s.Purpose == ContentHumanReview
}
func contentBodyDenied() *ContentHTTPFailure { return &ContentHTTPFailure{401, "unauthorized", false} }

func (p *contentBodyProof) bindSelection(selector ContentBodySelector, object ContentBodyObject, initial bool) *ContentHTTPFailure {
	p.selectionMu.Lock()
	defer p.selectionMu.Unlock()
	if p.selection == nil {
		if !initial {
			return contentBodyDenied()
		}
		p.selection = &contentBodySelection{selector: selector, receipt: object.Receipt, sourceHash: object.Metadata.SourceSHA256}
		return nil
	}
	if p.selection.selector != selector {
		return &ContentHTTPFailure{404, "not_found", false}
	}
	if p.selection.receipt != object.Receipt || p.selection.sourceHash != object.Metadata.SourceSHA256 {
		return &ContentHTTPFailure{503, "representation_mismatch", false}
	}
	return nil
}
