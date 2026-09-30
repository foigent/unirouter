package sourcefacts

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/middleware"
)

// These rows are synthetic unit inputs. Only the integration tests can establish
// actual SQL authorization, snapshot isolation, or database revocation behavior.
type contentAuthorityTestRow struct {
	values []any
	err    error
}

func (r contentAuthorityTestRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("synthetic column mismatch")
	}
	for i, v := range r.values {
		target := reflect.ValueOf(dest[i]).Elem()
		if v == nil {
			target.Set(reflect.Zero(target.Type()))
		} else {
			target.Set(reflect.ValueOf(v))
		}
	}
	return nil
}

type contentAuthorityTestRows struct {
	pgx.Rows
	rows  []contentAuthorityTestRow
	index int
}

func (r *contentAuthorityTestRows) Next() bool             { r.index++; return r.index < len(r.rows) }
func (r *contentAuthorityTestRows) Scan(dest ...any) error { return r.rows[r.index].Scan(dest...) }
func (r *contentAuthorityTestRows) Err() error             { return nil }
func (r *contentAuthorityTestRows) Close()                 {}

type contentAuthorityTestTx struct {
	pgx.Tx
	access               contentBodyAccess
	manifest             ContentManifest
	token                string
	queries              []string
	keyLookup            int64
	committed, ended     bool
	noManifest, noObject bool
}

func (t *contentAuthorityTestTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	t.queries = append(t.queries, sql)
	t.keyLookup = args[2].(int64)
	v := t.access
	return &contentAuthorityTestRows{index: -1, rows: []contentAuthorityTestRow{{values: []any{v.keyID, v.org, v.project, v.keyStatus, v.ips, v.scope, v.instance, v.global, v.registrationHash, v.prefix, v.keyHash, v.operation, v.profiles, v.hasScope, v.local, v.generation, v.safety, v.epoch, v.high, v.coverage, v.streamInstance, v.coverageTime, v.purposes, v.policyLabel}}}}, nil
}
func (t *contentAuthorityTestTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	t.queries = append(t.queries, sql)
	if strings.Contains(sql, "execution_content_manifests") {
		if t.noManifest || args[5].(int64) >= 1 || args[6].(int64) < 1 || args[4].(string) != t.manifest.RequestID {
			return contentAuthorityTestRow{err: pgx.ErrNoRows}
		}
		wire, err := EncodeContentManifest(t.manifest)
		if err != nil {
			return contentAuthorityTestRow{err: err}
		}
		return contentAuthorityTestRow{values: []any{wire, eventconnector.HashSourceBytes(wire)}}
	}
	if t.noObject {
		return contentAuthorityTestRow{err: pgx.ErrNoRows}
	}
	expires, _ := time.Parse(time.RFC3339Nano, t.manifest.ExpiresAt)
	digest := t.manifest.Representation.Digest
	return contentAuthorityTestRow{values: []any{"synthetic-store", t.token, strings.Repeat("c", 64), "ready", t.manifest.Representation.SavedBytes, &digest, expires}}
}
func (t *contentAuthorityTestTx) Commit(context.Context) error {
	t.committed = true
	t.ended = true
	return nil
}
func (t *contentAuthorityTestTx) Rollback(context.Context) error { t.ended = true; return nil }

type contentAuthorityFixture struct {
	a                    *contentBodyDBAuthority
	request              *http.Request
	selector             ContentBodySelector
	access               contentBodyAccess
	manifest             ContentManifest
	token                string
	transactions         []*contentAuthorityTestTx
	noManifest, noObject bool
}

func newContentAuthorityFixture(t *testing.T) *contentAuthorityFixture {
	t.Helper()
	secret := strings.Repeat("s", 32)
	raw := "ur-xf1-" + strings.Repeat("a", 64)
	hash, prefix, err := auth.HashReceiverFeedCredential(raw, secret)
	if err != nil {
		t.Fatal(err)
	}
	verifier, _ := auth.ReceiverFeedVerifierHex(raw)
	reg := Registration{SchemaVersion: RegistrationVersion, TokenFormat: auth.ReceiverFeedTokenFormat, VerifierScheme: auth.ReceiverFeedVerifierScheme, KeyPrefix: prefix, VerifierSHA256: verifier, OperationID: strings.Repeat("1", 32), SourceInstance: "synthetic-ur", OrgID: "1", ProjectID: "2", Profiles: []string{ContentProfile}}
	digest, err := HashContentRegistration(reg)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	f := &contentAuthorityFixture{token: strings.Repeat("b", 64), access: contentBodyAccess{keyID: 3, org: 1, project: 2, keyStatus: "feed_active", ips: "127.0.0.1/32", scope: "content", instance: "synthetic-ur", global: "active", registrationHash: digest, prefix: prefix, keyHash: hash, operation: reg.OperationID, profiles: []string{ContentProfile}, hasScope: true, local: "active", generation: 2, safety: 3, epoch: 1, high: 1, coverage: 0, streamInstance: "synthetic-ur", coverageTime: &at, purposes: []string{ContentHumanReview}}}
	f.manifest = syntheticContentManifest([]byte("body"))
	f.manifest.ExpiresAt = at.Add(time.Hour).Format("2006-01-02T15:04:05.000000Z")
	f.selector = ContentBodySelector{RequestID: f.manifest.RequestID, SourceFactID: f.manifest.SourceFactID, Revision: f.manifest.Revision, ContentRef: f.manifest.ContentRef, Purpose: ContentHumanReview}
	f.request = httptest.NewRequest("GET", "http://example.invalid/content", nil)
	f.request.RemoteAddr = "127.0.0.1:1234"
	f.request.Header.Set("Authorization", "Bearer "+raw)
	f.a = &contentBodyDBAuthority{source: &Service{options: Options{Enabled: true, ContentEnabled: true, SourceInstance: "synthetic-ur", CursorSecret: secret}}, resolver: middleware.ClientIPResolver{}}
	f.a.begin = func(context.Context) (pgx.Tx, error) {
		tx := &contentAuthorityTestTx{access: f.access, manifest: f.manifest, token: f.token, noManifest: f.noManifest, noObject: f.noObject}
		f.transactions = append(f.transactions, tx)
		return tx, nil
	}
	return f
}
func requireAuthorityFailure(t *testing.T, failure *ContentHTTPFailure, status int, code string) {
	t.Helper()
	if failure == nil || failure.Status != status || failure.Code != code {
		t.Fatalf("failure=%#v, want %d %s", failure, status, code)
	}
}
func (f *contentAuthorityFixture) grant(t *testing.T) ContentBodyGrant {
	t.Helper()
	g, failure := f.a.Authenticate(t.Context(), f.request)
	if failure != nil {
		t.Fatal(failure)
	}
	return g
}
func (f *contentAuthorityFixture) selected(t *testing.T) ContentBodyGrant {
	t.Helper()
	g := f.grant(t)
	if _, failure := f.a.Lookup(t.Context(), g, f.selector); failure != nil {
		t.Fatal(failure)
	}
	return g
}

func TestContentBodyAuthorityR2AndRegistration(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*contentBodyAccess)
		status int
		code   string
	}{
		{"legacy paused staged", func(v *contentBodyAccess) {
			v.scope = "legacy"
			v.profiles = []string{"unirouter.execution-context.v1"}
			v.global = "paused"
			v.keyStatus = "feed_staged"
		}, 403, "profile_not_authorized"},
		{"paused staged", func(v *contentBodyAccess) { v.local = "paused"; v.keyStatus = "feed_staged" }, 503, "source_paused"},
		{"global paused", func(v *contentBodyAccess) { v.global = "paused" }, 503, "source_paused"},
		{"staged", func(v *contentBodyAccess) { v.keyStatus = "feed_staged" }, 409, "staged_credential_requires_pause"},
		{"wrong ip", func(v *contentBodyAccess) { v.ips = "192.0.2.1/32" }, 401, "unauthorized"},
		{"revoked", func(v *contentBodyAccess) { v.global = "revoked" }, 401, "unauthorized"},
		{"corrupt registration", func(v *contentBodyAccess) { v.registrationHash = "sha256:" + strings.Repeat("0", 64) }, 401, "unauthorized"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newContentAuthorityFixture(t)
			test.change(&f.access)
			_, failure := f.a.Authenticate(t.Context(), f.request)
			requireAuthorityFailure(t, failure, test.status, test.code)
			if len(f.transactions) != 1 || len(f.transactions[0].queries) != 1 || !f.transactions[0].ended {
				t.Fatal("authorization rejection queried content or retained transaction")
			}
		})
	}
}

func TestContentBodyAuthorityProofAndFreshSnapshots(t *testing.T) {
	f := newContentAuthorityFixture(t)
	g := f.grant(t)
	if g.proof == nil || g.proof.owner != f.a || g.proof.keyID != 3 || !f.transactions[0].committed {
		t.Fatal("missing original authority proof")
	}
	for name, alter := range map[string]func(*ContentBodyGrant){"missing": func(g *ContentBodyGrant) { g.proof = nil }, "forged scope": func(g *ContentBodyGrant) { g.ProjectID = "99" }, "foreign": func(g *ContentBodyGrant) {
		g.proof = &contentBodyProof{owner: newContentAuthorityFixture(t).a, keyID: 3}
	}} {
		t.Run(name, func(t *testing.T) {
			bad := g
			alter(&bad)
			count := len(f.transactions)
			_, failure := f.a.Lookup(t.Context(), bad, f.selector)
			requireAuthorityFailure(t, failure, 401, "unauthorized")
			if len(f.transactions) != count {
				t.Fatal("invalid proof reached DB")
			}
		})
	}
	object, failure := f.a.Lookup(t.Context(), g, f.selector)
	if failure != nil {
		t.Fatal(failure)
	}
	if object.Receipt.Binding.Token != f.token || object.Receipt.ObjectName != strings.Repeat("c", 64) {
		t.Fatal("did not preserve original stored mapping")
	}
	if _, failure = f.a.Reauthorize(t.Context(), f.request, g, f.selector); failure != nil {
		t.Fatal(failure)
	}
	if len(f.transactions) != 3 {
		t.Fatal("reused earlier transaction")
	}
	for i, tx := range f.transactions {
		if !tx.committed || !tx.ended {
			t.Fatal("authority returned before closing transaction")
		}
		if i > 0 && tx.keyLookup != g.proof.keyID {
			t.Fatal("selected arbitrary current active credential")
		}
		for _, sql := range tx.queries {
			if strings.Contains(sql, "FOR SHARE") || strings.Contains(sql, "FOR UPDATE") {
				t.Fatal("read authority acquired row locks")
			}
		}
	}
}

func TestContentBodyAuthorityStopsInvalidatedOriginalGrant(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*contentAuthorityFixture)
		status int
		code   string
	}{
		{"parent pause resume", func(f *contentAuthorityFixture) { f.access.safety += 2 }, 409, "content_scope_changed"},
		{"content pause resume", func(f *contentAuthorityFixture) { f.access.generation++ }, 409, "content_scope_changed"},
		{"epoch", func(f *contentAuthorityFixture) { f.access.epoch++ }, 409, "source_epoch_changed"},
		{"original revoked", func(f *contentAuthorityFixture) { f.access.keyStatus = "feed_revoked" }, 401, "unauthorized"},
		{"rotated key", func(f *contentAuthorityFixture) { f.access.keyID++ }, 401, "unauthorized"},
		{"registration changed", func(f *contentAuthorityFixture) { f.access.registrationHash = "sha256:" + strings.Repeat("f", 64) }, 401, "unauthorized"},
		{"coverage changed", func(f *contentAuthorityFixture) { f.access.coverage = 1 }, 409, "content_scope_changed"},
		{"ip changed", func(f *contentAuthorityFixture) { f.request.RemoteAddr = "192.0.2.1:23" }, 401, "unauthorized"},
		{"object token changed", func(f *contentAuthorityFixture) { f.token = strings.Repeat("d", 64) }, 503, "representation_mismatch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newContentAuthorityFixture(t)
			g := f.selected(t)
			test.change(f)
			_, failure := f.a.Reauthorize(t.Context(), f.request, g, f.selector)
			requireAuthorityFailure(t, failure, test.status, test.code)
		})
	}
}

func TestContentBodyAuthorityScopeBeforeAvailability(t *testing.T) {
	f := newContentAuthorityFixture(t)
	f.access.coverage = 1
	g := f.grant(t)
	_, failure := f.a.Lookup(t.Context(), g, f.selector)
	requireAuthorityFailure(t, failure, 404, "not_found")
	if got := len(f.transactions[1].queries); got != 2 {
		t.Fatalf("coverage denial queried object availability: %d queries", got)
	}
	f = newContentAuthorityFixture(t)
	g = f.grant(t)
	wrong := f.selector
	wrong.ContentRef = "uc1_" + strings.Repeat("e", 64)
	_, failure = f.a.Lookup(t.Context(), g, wrong)
	requireAuthorityFailure(t, failure, 404, "not_found")
	if len(f.transactions[1].queries) != 2 {
		t.Fatal("wrong reference queried availability")
	}
	f = newContentAuthorityFixture(t)
	g = f.grant(t)
	f.noObject = true
	_, failure = f.a.Lookup(t.Context(), g, f.selector)
	requireAuthorityFailure(t, failure, 503, "content_missing")
	f = newContentAuthorityFixture(t)
	g = f.selected(t)
	wrong = f.selector
	wrong.Purpose = "training"
	_, failure = f.a.Reauthorize(t.Context(), f.request, g, wrong)
	requireAuthorityFailure(t, failure, 400, "invalid_request")
	// Restoring a lower stream high watermark must not hide a changed epoch
	// behind a generic failure; exact coverage remains enforced by Lookup SQL.
	f = newContentAuthorityFixture(t)
	f.access.coverage = 1
	g = f.grant(t)
	f.access.high = 0
	f.access.epoch++
	_, failure = f.a.Reauthorize(t.Context(), f.request, g, f.selector)
	requireAuthorityFailure(t, failure, 409, "source_epoch_changed")
}

func TestContentBodyAuthorityStrictHeaderAndConstructor(t *testing.T) {
	f := newContentAuthorityFixture(t)
	f.request.Header["authorization"] = []string{f.request.Header.Get("Authorization")}
	_, failure := f.a.Authenticate(t.Context(), f.request)
	requireAuthorityFailure(t, failure, 401, "unauthorized")
	if len(f.transactions) != 0 {
		t.Fatal("malformed header reached DB")
	}
	if _, err := f.a.source.NewContentBodyAuthority(middleware.ClientIPResolver{}); err == nil {
		t.Fatal("accepted no independent pool")
	}
	f.a.source.db = &contentAuthorityTestTx{}
	if _, err := f.a.source.NewContentBodyAuthority(middleware.ClientIPResolver{}); err == nil {
		t.Fatal("accepted an outer transaction as a fresh-read source")
	}
	disabled := &Service{}
	if _, err := disabled.NewContentBodyAuthority(middleware.ClientIPResolver{}); err == nil {
		t.Fatal("accepted disabled contract")
	}
}
