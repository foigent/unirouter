package secretref

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFixtureBackendRotationLifecycleAndMetadataOnlyEvidence(t *testing.T) {
	t.Parallel()

	clock := newTestClock()
	backend := NewFixtureBackend(clock.Now)
	ref := testReference()
	v1Material := []byte("fixture-provider-key-v1")
	v2Material := []byte("fixture-provider-key-v2")

	stageV1, err := backend.Stage(context.Background(), ref, "v1", 0, v1Material)
	if err != nil {
		t.Fatalf("stage v1: %v", err)
	}
	if stageV1.Revision != 1 || stageV1.Generation != 0 {
		t.Fatalf("unexpected stage receipt: %+v", stageV1)
	}

	v1, activateV1, err := backend.Activate(context.Background(), ref, "v1", 1)
	if err != nil {
		t.Fatalf("activate v1: %v", err)
	}
	if v1.Generation != 1 || activateV1.Action != ActionActivate {
		t.Fatalf("unexpected v1 activation: binding=%+v receipt=%+v", v1, activateV1)
	}
	assertMaterial(t, backend, v1, v1Material)

	if _, err := backend.Stage(context.Background(), ref, "v2", 2, v2Material); err != nil {
		t.Fatalf("stage v2: %v", err)
	}
	// Staging does not invalidate the current active binding.
	assertMaterial(t, backend, v1, v1Material)

	v2, _, err := backend.Activate(context.Background(), ref, "v2", 3)
	if err != nil {
		t.Fatalf("activate v2: %v", err)
	}
	if err := backend.WithMaterial(context.Background(), v1, func([]byte) error { return nil }); !errors.Is(err, ErrStaleBinding) {
		t.Fatalf("old worker binding error = %v, want ErrStaleBinding", err)
	}
	assertMaterial(t, backend, v2, v2Material)

	v1Rollback, rollback, err := backend.Rollback(context.Background(), ref, "v1", 4)
	if err != nil {
		t.Fatalf("rollback v1: %v", err)
	}
	if rollback.Action != ActionRollback || rollback.PreviousVersion != "v2" || v1Rollback.Generation != 3 {
		t.Fatalf("unexpected rollback: binding=%+v receipt=%+v", v1Rollback, rollback)
	}
	if err := backend.WithMaterial(context.Background(), v2, func([]byte) error { return nil }); !errors.Is(err, ErrStaleBinding) {
		t.Fatalf("replaced v2 binding error = %v, want ErrStaleBinding", err)
	}

	revoke, err := backend.Revoke(context.Background(), ref, "v2", 5)
	if err != nil {
		t.Fatalf("revoke v2: %v", err)
	}
	if revoke.Action != ActionRevoke || revoke.Generation != 3 || revoke.Revision != 6 {
		t.Fatalf("unexpected revoke receipt: %+v", revoke)
	}

	snapshot, err := backend.Snapshot(context.Background(), ref)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.Active == nil || *snapshot.Active != v1Rollback {
		t.Fatalf("active binding = %+v, want %+v", snapshot.Active, v1Rollback)
	}
	if len(snapshot.Versions) != 2 ||
		snapshot.Versions[0].State != VersionActive ||
		snapshot.Versions[1].State != VersionRevoked {
		t.Fatalf("unexpected versions: %+v", snapshot.Versions)
	}

	audit, err := backend.Audit(context.Background(), ref)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if len(audit) != 6 {
		t.Fatalf("audit count = %d, want 6", len(audit))
	}
	assertMetadataOnly(t, []any{stageV1, activateV1, rollback, revoke, snapshot, audit}, v1Material, v2Material)
}

func TestFixtureBackendGoldenMetadataEvidence(t *testing.T) {
	t.Parallel()

	backend := NewFixtureBackend(newTestClock().Now)
	ref := testReference()
	if _, err := backend.Stage(context.Background(), ref, "v1", 0, []byte("fixture-provider-key-v1")); err != nil {
		t.Fatalf("stage v1: %v", err)
	}
	if _, _, err := backend.Activate(context.Background(), ref, "v1", 1); err != nil {
		t.Fatalf("activate v1: %v", err)
	}
	if _, err := backend.Stage(context.Background(), ref, "v2", 2, []byte("fixture-provider-key-v2")); err != nil {
		t.Fatalf("stage v2: %v", err)
	}
	if _, _, err := backend.Activate(context.Background(), ref, "v2", 3); err != nil {
		t.Fatalf("activate v2: %v", err)
	}
	if _, _, err := backend.Rollback(context.Background(), ref, "v1", 4); err != nil {
		t.Fatalf("rollback v1: %v", err)
	}
	if _, err := backend.Revoke(context.Background(), ref, "v2", 5); err != nil {
		t.Fatalf("revoke v2: %v", err)
	}
	snapshot, err := backend.Snapshot(context.Background(), ref)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	audit, err := backend.Audit(context.Background(), ref)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}

	actual, err := json.MarshalIndent(struct {
		Snapshot Snapshot       `json:"snapshot"`
		Audit    []AuditReceipt `json:"audit"`
	}{Snapshot: snapshot, Audit: audit}, "", "  ")
	if err != nil {
		t.Fatalf("marshal evidence: %v", err)
	}
	actual = append(actual, '\n')
	expected, err := os.ReadFile("testdata/rotation-metadata.golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(actual, expected) {
		t.Fatalf("golden mismatch\nactual:\n%s\nexpected:\n%s", actual, expected)
	}
}

func TestFixtureBackendImmutableVersionAndCAS(t *testing.T) {
	t.Parallel()

	backend := NewFixtureBackend(newTestClock().Now)
	ref := testReference()
	original := []byte("fixture-original")
	if _, err := backend.Stage(context.Background(), ref, "v1", 0, original); err != nil {
		t.Fatalf("stage: %v", err)
	}
	original[0] = 'X'

	if _, err := backend.Stage(context.Background(), ref, "v1", 1, []byte("replacement")); !errors.Is(err, ErrVersionExists) {
		t.Fatalf("duplicate stage error = %v, want ErrVersionExists", err)
	}
	if _, _, err := backend.Activate(context.Background(), ref, "v1", 0); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale activate error = %v, want ErrRevisionConflict", err)
	}
	binding, _, err := backend.Activate(context.Background(), ref, "v1", 1)
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	assertMaterial(t, backend, binding, []byte("fixture-original"))

	if _, err := backend.Revoke(context.Background(), ref, "v1", 2); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("active revoke error = %v, want ErrInvalidTransition", err)
	}
	if _, _, err := backend.Rollback(context.Background(), ref, "v1", 2); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("active rollback error = %v, want ErrInvalidTransition", err)
	}
}

func TestFixtureBackendOutageFailsClosedWithoutMutation(t *testing.T) {
	t.Parallel()

	backend := NewFixtureBackend(newTestClock().Now)
	ref := testReference()
	if _, err := backend.Stage(context.Background(), ref, "v1", 0, []byte("fixture-key")); err != nil {
		t.Fatalf("stage: %v", err)
	}
	binding, _, err := backend.Activate(context.Background(), ref, "v1", 1)
	if err != nil {
		t.Fatalf("activate: %v", err)
	}

	backend.SetAvailable(false)
	assertUnavailable := func(name string, err error) {
		t.Helper()
		if !errors.Is(err, ErrBackendUnavailable) {
			t.Fatalf("%s error = %v, want ErrBackendUnavailable", name, err)
		}
	}
	_, err = backend.Pin(context.Background(), ref)
	assertUnavailable("pin", err)
	assertUnavailable("use", backend.WithMaterial(context.Background(), binding, func([]byte) error { return nil }))
	_, err = backend.Stage(context.Background(), ref, "v2", 2, []byte("fixture-key-2"))
	assertUnavailable("stage", err)
	_, _, err = backend.Activate(context.Background(), ref, "v1", 2)
	assertUnavailable("activate", err)
	_, err = backend.Revoke(context.Background(), ref, "v1", 2)
	assertUnavailable("revoke", err)
	_, err = backend.Snapshot(context.Background(), ref)
	assertUnavailable("snapshot", err)
	_, err = backend.Audit(context.Background(), ref)
	assertUnavailable("audit", err)

	backend.SetAvailable(true)
	snapshot, err := backend.Snapshot(context.Background(), ref)
	if err != nil {
		t.Fatalf("snapshot after outage: %v", err)
	}
	if snapshot.Revision != 2 || snapshot.Generation != 1 || snapshot.Active == nil || *snapshot.Active != binding {
		t.Fatalf("outage changed lifecycle state: %+v", snapshot)
	}
}

func TestFixtureBackendConsumerErrorIsSanitized(t *testing.T) {
	t.Parallel()

	backend := NewFixtureBackend(newTestClock().Now)
	ref := testReference()
	material := []byte("fixture-never-in-error")
	if _, err := backend.Stage(context.Background(), ref, "v1", 0, material); err != nil {
		t.Fatalf("stage: %v", err)
	}
	binding, _, err := backend.Activate(context.Background(), ref, "v1", 1)
	if err != nil {
		t.Fatalf("activate: %v", err)
	}

	err = backend.WithMaterial(context.Background(), binding, func(value []byte) error {
		return fmt.Errorf("unsafe callback error %s", value)
	})
	if err != ErrConsumerRejected {
		t.Fatalf("consumer error = %v, want exact ErrConsumerRejected", err)
	}
	if strings.Contains(err.Error(), string(material)) {
		t.Fatal("returned error contains secret material")
	}

	err = backend.WithMaterial(context.Background(), binding, func(value []byte) error {
		panic(fmt.Sprintf("unsafe callback panic %s", value))
	})
	if err != ErrConsumerRejected {
		t.Fatalf("consumer panic error = %v, want exact ErrConsumerRejected", err)
	}
}

func TestFixtureBackendActivationCannotRaceMaterialInjection(t *testing.T) {
	t.Parallel()

	backend := NewFixtureBackend(newTestClock().Now)
	ref := testReference()
	if _, err := backend.Stage(context.Background(), ref, "v1", 0, []byte("fixture-v1")); err != nil {
		t.Fatalf("stage v1: %v", err)
	}
	v1, _, err := backend.Activate(context.Background(), ref, "v1", 1)
	if err != nil {
		t.Fatalf("activate v1: %v", err)
	}
	if _, err := backend.Stage(context.Background(), ref, "v2", 2, []byte("fixture-v2")); err != nil {
		t.Fatalf("stage v2: %v", err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	useDone := make(chan error, 1)
	go func() {
		useDone <- backend.WithMaterial(context.Background(), v1, func([]byte) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered

	activateDone := make(chan error, 1)
	go func() {
		_, _, activateErr := backend.Activate(context.Background(), ref, "v2", 3)
		activateDone <- activateErr
	}()

	select {
	case err := <-activateDone:
		t.Fatalf("activation completed before material callback: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-useDone; err != nil {
		t.Fatalf("use: %v", err)
	}
	if err := <-activateDone; err != nil {
		t.Fatalf("activate v2: %v", err)
	}
	if err := backend.WithMaterial(context.Background(), v1, func([]byte) error { return nil }); !errors.Is(err, ErrStaleBinding) {
		t.Fatalf("post-activation v1 error = %v, want ErrStaleBinding", err)
	}
}

func TestSecretReferenceValidationAndCancellation(t *testing.T) {
	t.Parallel()

	valid := Reference{Backend: "fixture", Scope: "org-42/providers", Name: "openai-primary"}
	canonical, err := valid.Canonical()
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	if canonical != "secretref://fixture/org-42/providers/openai-primary" {
		t.Fatalf("canonical = %q", canonical)
	}

	invalid := []Reference{
		{},
		{Backend: "Fixture", Scope: "org", Name: "key"},
		{Backend: "fixture-", Scope: "org", Name: "key"},
		{Backend: "fixture..local", Scope: "org", Name: "key"},
		{Backend: "fixture", Scope: "../org", Name: "key"},
		{Backend: "fixture", Scope: "org//providers", Name: "key"},
		{Backend: "fixture", Scope: "org", Name: "path/key"},
		{Backend: "fixture", Scope: "组织", Name: "key"},
		{Backend: "fixture", Scope: "org\nscope", Name: "key"},
	}
	for _, ref := range invalid {
		if err := ref.Validate(); !errors.Is(err, ErrInvalidReference) {
			t.Errorf("Validate(%+v) = %v, want ErrInvalidReference", ref, err)
		}
	}

	backend := NewFixtureBackend(newTestClock().Now)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := backend.Pin(ctx, valid); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled pin error = %v, want context.Canceled", err)
	}
	if _, err := backend.Stage(context.Background(), valid, "bad/version", 0, []byte("x")); !errors.Is(err, ErrInvalidVersion) {
		t.Fatalf("invalid version error = %v, want ErrInvalidVersion", err)
	}
	if _, err := backend.Stage(context.Background(), valid, "v1", 0, nil); !errors.Is(err, ErrInvalidMaterial) {
		t.Fatalf("empty material error = %v, want ErrInvalidMaterial", err)
	}
	if _, err := backend.Stage(context.Background(), valid, "v1", 0, make([]byte, MaxMaterialBytes+1)); !errors.Is(err, ErrInvalidMaterial) {
		t.Fatalf("oversized material error = %v, want ErrInvalidMaterial", err)
	}
}

func TestRegistryIsFixedAndRejectsUnknownOrDuplicateBackends(t *testing.T) {
	t.Parallel()

	fixture := NewFixtureBackend(newTestClock().Now)
	registry, err := NewRegistry(fixture)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	got, err := registry.BackendFor(testReference())
	if err != nil {
		t.Fatalf("backend for fixture: %v", err)
	}
	if got != fixture {
		t.Fatal("registry returned a different backend")
	}
	if _, err := registry.BackendFor(Reference{Backend: "vault", Scope: "org", Name: "key"}); !errors.Is(err, ErrBackendNotFound) {
		t.Fatalf("unknown backend error = %v, want ErrBackendNotFound", err)
	}
	if _, err := NewRegistry(fixture, NewFixtureBackend(newTestClock().Now)); !errors.Is(err, ErrBackendConflict) {
		t.Fatalf("duplicate backend error = %v, want ErrBackendConflict", err)
	}
}

func TestFixtureBackendConcurrentCASHasSingleWinner(t *testing.T) {
	t.Parallel()

	backend := NewFixtureBackend(newTestClock().Now)
	ref := testReference()
	if _, err := backend.Stage(context.Background(), ref, "v1", 0, []byte("fixture-v1")); err != nil {
		t.Fatalf("stage v1: %v", err)
	}
	if _, err := backend.Stage(context.Background(), ref, "v2", 1, []byte("fixture-v2")); err != nil {
		t.Fatalf("stage v2: %v", err)
	}

	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, version := range []string{"v1", "v2"} {
		version := version
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := backend.Activate(context.Background(), ref, version, 2)
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	var successes, conflicts int
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrRevisionConflict):
			conflicts++
		default:
			t.Fatalf("unexpected activation error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d, want 1/1", successes, conflicts)
	}
}

func assertMaterial(t *testing.T, backend Backend, binding Binding, want []byte) {
	t.Helper()
	if err := backend.WithMaterial(context.Background(), binding, func(got []byte) error {
		if string(got) != string(want) {
			t.Fatalf("material = %q, want %q", got, want)
		}
		got[0] ^= 0xff
		return nil
	}); err != nil {
		t.Fatalf("with material: %v", err)
	}
	// Consumer mutation only touched the temporary callback copy.
	if err := backend.WithMaterial(context.Background(), binding, func(got []byte) error {
		if string(got) != string(want) {
			t.Fatalf("material after callback mutation = %q, want %q", got, want)
		}
		return nil
	}); err != nil {
		t.Fatalf("with material after mutation: %v", err)
	}
}

func assertMetadataOnly(t *testing.T, values []any, forbidden ...[]byte) {
	t.Helper()
	encoded, err := json.Marshal(values)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	for _, value := range forbidden {
		if strings.Contains(string(encoded), string(value)) {
			t.Fatalf("metadata output contains secret material: %s", encoded)
		}
	}
	if strings.Contains(string(encoded), "material") ||
		strings.Contains(string(encoded), "ciphertext") ||
		strings.Contains(string(encoded), "digest") {
		t.Fatalf("metadata output exposes prohibited secret field: %s", encoded)
	}
}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock {
	return &testClock{now: time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	value := c.now
	c.now = c.now.Add(time.Second)
	return value
}

func testReference() Reference {
	return Reference{Backend: "fixture", Scope: "org-42/providers", Name: "openai-primary"}
}
