//go:build integration

package sourcefacts

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/middleware"
)

type contentBodyPGFixture struct {
	pool              *pgxpool.Pool
	source            *Service
	authority         ContentBodyAuthority
	store             *contentcapture.FileStore
	org, project, key int64
	request           *http.Request
	selector          ContentBodySelector
	receipt           contentcapture.StoredReceipt
}

func insertContentBodyPGKey(t *testing.T, f *contentBodyPGFixture, digit, scope, status string, coverage int64) (int64, string) {
	t.Helper()
	raw := "ur-xf1-" + strings.Repeat(digit, 64)
	hash, prefix, err := auth.HashReceiverFeedCredential(raw, f.source.options.CursorSecret)
	require.NoError(t, err)
	verifier, err := auth.ReceiverFeedVerifierHex(raw)
	require.NoError(t, err)
	profiles := []string{ContentProfile}
	if scope == "legacy" {
		profiles = []string{"unirouter.execution-context.v1"}
	}
	registration := Registration{SchemaVersion: RegistrationVersion, TokenFormat: auth.ReceiverFeedTokenFormat, VerifierScheme: auth.ReceiverFeedVerifierScheme, KeyPrefix: prefix, VerifierSHA256: verifier, OperationID: strings.Repeat(digit, 32), SourceInstance: f.source.SourceInstance(), OrgID: strconv.FormatInt(f.org, 10), ProjectID: strconv.FormatInt(f.project, 10), Profiles: profiles}
	var digest string
	if scope == "content" {
		digest, err = HashContentRegistration(registration)
	} else {
		digest, err = registration.Hash()
	}
	require.NoError(t, err)
	var id int64
	require.NoError(t, f.pool.QueryRow(t.Context(), `INSERT INTO api_keys(org_id,project_id,key_type,name,key_hash,key_prefix,status,rpm_limit,qps_limit,budget_limit,allowed_ips,allowed_models,policy_labels,purpose,verifier_scheme,registration_operation_id,source_lifecycle_scope)
VALUES($1,$2,'service','Synthetic body authority',$3,$4,$5,60,1,0,'127.0.0.1/32','','','xinsight.source.read',$6,$7,$8) RETURNING id`, f.org, f.project, hash, prefix, status, auth.ReceiverFeedVerifierScheme, registration.OperationID, scope).Scan(&id))
	_, err = f.pool.Exec(t.Context(), `INSERT INTO execution_source_credentials(api_key_id,org_id,project_id,registration_sha256,profiles,source_lifecycle_scope) VALUES($1,$2,$3,$4,$5,$6)`, id, f.org, f.project, digest, profiles, scope)
	require.NoError(t, err)
	if scope == "content" {
		_, err = f.pool.Exec(t.Context(), `INSERT INTO execution_source_content_bindings(api_key_id,org_id,project_id,coverage_after_sequence,coverage_time,allowed_purposes) VALUES($1,$2,$3,$4,clock_timestamp(),ARRAY['human_review']::text[])`, id, f.org, f.project, coverage)
		require.NoError(t, err)
	}
	return id, raw
}

func setupContentBodyPG(t *testing.T, coverage int64) *contentBodyPGFixture {
	t.Helper()
	pool, s, org, project := setupContentJournal(t)
	f := &contentBodyPGFixture{pool: pool, source: s, org: org, project: project}
	_, err := pool.Exec(t.Context(), `INSERT INTO execution_source_connections(org_id,project_id,source_instance,status,profiles) VALUES($1,$2,$3,'active',ARRAY['unirouter.execution-context.v1']::text[])`, org, project, s.SourceInstance())
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `INSERT INTO execution_source_content_scopes(org_id,project_id,status,state_version,access_generation) VALUES($1,$2,'active',1,2)`, org, project)
	require.NoError(t, err)
	var raw string
	f.key, raw = insertContentBodyPGKey(t, f, "a", "content", "feed_active", coverage)
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	binding, err := contentcapture.InitializeFileStore(root, "synthetic-store")
	require.NoError(t, err)
	f.store, err = contentcapture.OpenFileStore(root, binding)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.store.Close() })
	body := []byte{0, 255, 'b', 'o', 'd', 'y'}
	f.receipt, err = f.store.Save(t.Context(), bytes.NewReader(body), contentcapture.StoreLimits{MaxSavedBytes: 1024, MaxReadBytes: 1024, MaxBufferBytes: 2})
	require.NoError(t, err)
	m := syntheticContentManifest(body)
	m.OrgID = strconv.FormatInt(org, 10)
	m.ProjectID = strconv.FormatInt(project, 10)
	m.SourceInstance = s.SourceInstance()
	m.SourceFactID = m.FactID()
	m.EventID = m.EventIDForRevision()
	expires := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	m.ExpiresAt = expires.Format("2006-01-02T15:04:05.000000Z")
	_, err = pool.Exec(t.Context(), `INSERT INTO execution_content_objects(content_ref,org_id,project_id,source_fact_id,revision,storage_identity,storage_binding_token,policy_version,recovery_epoch,relative_path,state,reserved_bytes,saved_bytes,digest,expires_at,lease_owner,lease_expires_at,policy_config,policy_effective_at)
VALUES($1,$2,$3,$4,1,$5,$6,$13,1,$7,'ready',$8,$8,$9,$10,'synthetic-test',clock_timestamp()+interval '1 hour',$11,$12)`, m.ContentRef, org, project, m.SourceFactID, f.receipt.Binding.Identity, f.receipt.Binding.Token, f.receipt.ObjectName, f.receipt.SavedBytes, f.receipt.SHA256, expires, contentFixturePolicy(t, m), m.Capture.PolicyEffectiveAt, m.Capture.PolicyVersion)
	require.NoError(t, err)
	_, err = s.AppendContentManifest(t.Context(), m, 1)
	require.NoError(t, err)
	f.selector = ContentBodySelector{RequestID: m.RequestID, SourceFactID: m.SourceFactID, Revision: m.Revision, ContentRef: m.ContentRef, Purpose: ContentHumanReview}
	f.request = httptest.NewRequest("GET", "http://example.invalid/content", nil)
	f.request.RemoteAddr = "127.0.0.1:1234"
	f.request.Header.Set("Authorization", "Bearer "+raw)
	f.authority, err = s.NewContentBodyAuthority(middleware.ClientIPResolver{})
	require.NoError(t, err)
	return f
}

func (f *contentBodyPGFixture) selected(t *testing.T) ContentBodyGrant {
	t.Helper()
	g, failure := f.authority.Authenticate(t.Context(), f.request)
	require.Nil(t, failure)
	object, failure := f.authority.Lookup(t.Context(), g, f.selector)
	require.Nil(t, failure)
	require.Equal(t, f.receipt, object.Receipt)
	return g
}

func TestContentBodyAuthorityPGOriginalGrantStopsAcrossSafetyAndEpoch(t *testing.T) {
	f := setupContentBodyPG(t, 0)
	g := f.selected(t)
	_, failure := f.authority.Reauthorize(t.Context(), f.request, g, f.selector)
	require.Nil(t, failure)
	// Both transitions happen between block checks. The safety version must
	// reject the old read even though the current parent is active again.
	_, err := f.pool.Exec(t.Context(), `UPDATE execution_source_connections SET status='paused' WHERE org_id=$1 AND project_id=$2`, f.org, f.project)
	require.NoError(t, err)
	_, err = f.pool.Exec(t.Context(), `UPDATE execution_source_connections SET status='active' WHERE org_id=$1 AND project_id=$2`, f.org, f.project)
	require.NoError(t, err)
	_, failure = f.authority.Reauthorize(t.Context(), f.request, g, f.selector)
	requireAuthorityFailure(t, failure, 409, "content_scope_changed")
	g = f.selected(t)
	_, err = f.pool.Exec(t.Context(), `UPDATE execution_source_content_scopes SET status='paused',access_generation=access_generation+1 WHERE org_id=$1 AND project_id=$2`, f.org, f.project)
	require.NoError(t, err)
	_, err = f.pool.Exec(t.Context(), `UPDATE execution_source_content_scopes SET status='active' WHERE org_id=$1 AND project_id=$2`, f.org, f.project)
	require.NoError(t, err)
	_, failure = f.authority.Reauthorize(t.Context(), f.request, g, f.selector)
	requireAuthorityFailure(t, failure, 409, "content_scope_changed")
	g = f.selected(t)
	_, err = f.pool.Exec(t.Context(), `UPDATE execution_content_streams SET recovery_epoch=recovery_epoch+1 WHERE org_id=$1 AND project_id=$2`, f.org, f.project)
	require.NoError(t, err)
	_, failure = f.authority.Reauthorize(t.Context(), f.request, g, f.selector)
	requireAuthorityFailure(t, failure, 409, "source_epoch_changed")
	// A newly authorized read still accesses the retained ready representation.
	// The object's allocation epoch remains 1; it is not a second read epoch.
	g = f.selected(t)
	require.EqualValues(t, 2, g.RecoveryEpoch)
	_, failure = f.authority.Reauthorize(t.Context(), f.request, g, f.selector)
	require.Nil(t, failure)
}

func TestContentBodyAuthorityPGLegacyStagedAndRotation(t *testing.T) {
	f := setupContentBodyPG(t, 0)
	original := f.selected(t)
	_, legacy := insertContentBodyPGKey(t, f, "b", "legacy", "feed_active", 0)
	r := f.request.Clone(context.Background())
	r.Header.Set("Authorization", "Bearer "+legacy)
	_, failure := f.authority.Authenticate(t.Context(), r)
	requireAuthorityFailure(t, failure, 403, "profile_not_authorized")
	stagedID, staged := insertContentBodyPGKey(t, f, "c", "content", "feed_staged", 0)
	r.Header.Set("Authorization", "Bearer "+staged)
	_, failure = f.authority.Authenticate(t.Context(), r)
	requireAuthorityFailure(t, failure, 409, "staged_credential_requires_pause")
	_, err := f.pool.Exec(t.Context(), `UPDATE execution_source_connections SET status='paused' WHERE org_id=$1 AND project_id=$2`, f.org, f.project)
	require.NoError(t, err)
	_, failure = f.authority.Authenticate(t.Context(), r)
	requireAuthorityFailure(t, failure, 503, "source_paused")
	_, err = f.pool.Exec(t.Context(), `UPDATE execution_source_connections SET status='active' WHERE org_id=$1 AND project_id=$2`, f.org, f.project)
	require.NoError(t, err)
	_, err = f.pool.Exec(t.Context(), `UPDATE api_keys SET status='feed_revoked' WHERE id=$1`, f.key)
	require.NoError(t, err)
	_, err = f.pool.Exec(t.Context(), `UPDATE api_keys SET status='feed_active' WHERE id=$1`, stagedID)
	require.NoError(t, err)
	_, err = f.pool.Exec(t.Context(), `UPDATE execution_source_content_scopes SET access_generation=access_generation+1 WHERE org_id=$1 AND project_id=$2`, f.org, f.project)
	require.NoError(t, err)
	_, failure = f.authority.Reauthorize(t.Context(), f.request, original, f.selector)
	requireAuthorityFailure(t, failure, 401, "unauthorized")
	f.request = r
	f.key = stagedID
	f.selected(t)
}

func TestContentBodyAuthorityPGCoverageAndOriginalStorageToken(t *testing.T) {
	t.Run("coverage", func(t *testing.T) {
		f := setupContentBodyPG(t, 1)
		g, failure := f.authority.Authenticate(t.Context(), f.request)
		require.Nil(t, failure)
		_, failure = f.authority.Lookup(t.Context(), g, f.selector)
		requireAuthorityFailure(t, failure, 404, "not_found")
	})
	t.Run("storage binding", func(t *testing.T) {
		f := setupContentBodyPG(t, 0)
		g := f.selected(t)
		otherRoot, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		otherBinding, err := contentcapture.InitializeFileStore(otherRoot, f.receipt.Binding.Identity)
		require.NoError(t, err)
		otherStore, err := contentcapture.OpenFileStore(otherRoot, otherBinding)
		require.NoError(t, err)
		defer otherStore.Close()
		_, err = otherStore.OpenVerified(t.Context(), f.receipt, contentcapture.StoreLimits{MaxSavedBytes: 1024, MaxReadBytes: 1024, MaxBufferBytes: 2})
		require.True(t, errors.Is(err, contentcapture.ErrStoreBinding))
		_, err = f.pool.Exec(t.Context(), `UPDATE execution_content_objects SET storage_binding_token=$1 WHERE content_ref=$2`, otherBinding.Token, f.selector.ContentRef)
		require.NoError(t, err)
		_, failure := f.authority.Reauthorize(t.Context(), f.request, g, f.selector)
		requireAuthorityFailure(t, failure, 503, "representation_mismatch")
	})
}
