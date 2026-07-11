package agentcap

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/testutil"
)

type testClock struct {
	nanos atomic.Int64
}

func newTestClock(now time.Time) *testClock {
	clock := &testClock{}
	clock.Set(now)
	return clock
}

func (c *testClock) Now() time.Time {
	return time.Unix(0, c.nanos.Load()).UTC()
}

func (c *testClock) Set(now time.Time) {
	c.nanos.Store(now.UnixNano())
}

type authorityFixture struct {
	mu             sync.RWMutex
	keyID          int64
	orgID          int64
	projectID      int64
	ownerUserID    int64
	keyType        string
	keyStatus      string
	orgStatus      string
	projectStatus  string
	ownerStatus    string
	ownerRole      string
	membershipRole string
	allowedIPs     string
	allowedModels  string
	rpmLimit       int
	rpsLimit       int
	budgetLimit    int
	queryCount     atomic.Int64
}

func defaultAuthorityFixture() *authorityFixture {
	return &authorityFixture{
		keyID:          11,
		orgID:          7,
		projectID:      3,
		ownerUserID:    5,
		keyType:        "personal",
		keyStatus:      "active",
		orgStatus:      "active",
		projectStatus:  "active",
		ownerStatus:    "active",
		ownerRole:      "developer",
		membershipRole: "developer",
		allowedModels:  "gpt-4o,text-embedding-3-small",
		rpmLimit:       120,
		rpsLimit:       20,
		budgetLimit:    10_000,
	}
}

func newTestService(t *testing.T, fixture *authorityFixture, clock *testClock) (*Service, *miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	database := &testutil.MockDB{QueryRowFn: func(_ context.Context, _ string, args ...any) pgx.Row {
		fixture.queryCount.Add(1)
		return &testutil.MockRow{ScanFn: func(dest ...any) error {
			fixture.mu.RLock()
			defer fixture.mu.RUnlock()
			if len(args) != 1 || args[0] != fixture.keyID {
				return fmt.Errorf("unexpected key lookup: %v", args)
			}
			if len(dest) != 18 {
				return fmt.Errorf("unexpected authority scan width: %d", len(dest))
			}
			*dest[0].(*int64) = fixture.keyID
			*dest[1].(*int64) = fixture.orgID
			*dest[2].(*int64) = fixture.projectID
			*dest[3].(*int64) = fixture.ownerUserID
			*dest[4].(*string) = fixture.keyType
			*dest[5].(*string) = "root-key"
			*dest[6].(*string) = "ur-0007-root"
			*dest[7].(*string) = fixture.keyStatus
			*dest[8].(*string) = fixture.allowedIPs
			*dest[9].(*string) = fixture.allowedModels
			*dest[10].(*int) = fixture.rpmLimit
			*dest[11].(*int) = fixture.rpsLimit
			*dest[12].(*int) = fixture.budgetLimit
			*dest[13].(*string) = fixture.orgStatus
			*dest[14].(*string) = fixture.projectStatus
			*dest[15].(*string) = fixture.ownerStatus
			*dest[16].(*string) = fixture.ownerRole
			*dest[17].(*string) = fixture.membershipRole
			return nil
		}}
	}}
	service, err := NewServiceWithOptions(database, rdb, "agent-capability-test-root-secret", ServiceOptions{
		Now:               clock.Now,
		AuthorityCacheTTL: AuthorityCacheTTL,
		AuthorityCacheMax: 128,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return service, mr, rdb
}

func rootKey(fixture *authorityFixture) *models.ApiKey {
	projectID := fixture.projectID
	ownerUserID := fixture.ownerUserID
	budget := fixture.budgetLimit
	return &models.ApiKey{
		ID:            fixture.keyID,
		OrgID:         fixture.orgID,
		ProjectID:     &projectID,
		OwnerUserID:   &ownerUserID,
		KeyType:       fixture.keyType,
		Status:        fixture.keyStatus,
		AllowedModels: fixture.allowedModels,
		RPMLimit:      fixture.rpmLimit,
		QPSLimit:      fixture.rpsLimit,
		BudgetLimit:   &budget,
	}
}

func rootLimits(maxDepth, maxChildren, maxDescendants int) Limits {
	return Limits{
		MaxDepth:         maxDepth,
		MaxChildren:      maxChildren,
		MaxDescendants:   maxDescendants,
		CostBudgetCents:  5_000,
		TokenBudget:      500_000,
		RPMLimit:         100,
		RPSLimit:         10,
		ConcurrencyLimit: 20,
		AllowedModels:    []string{"gpt-4o"},
		AllowedEndpoints: []string{"/v1/chat/completions", "/v1/models"},
	}
}

func exchangeRoot(t *testing.T, service *Service, fixture *authorityFixture, limits Limits) TokenResponse {
	t.Helper()
	response, err := service.Exchange(context.Background(), rootKey(fixture), ExchangeRequest{
		RootAgentID:        "agent:supervisor",
		WorkflowTTLSeconds: 3600,
		Limits:             limits,
	})
	if err != nil {
		t.Fatalf("exchange root capability: %v", err)
	}
	return response
}

func TestExchangeAuthenticateAndTamperResistance(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	fixture := defaultAuthorityFixture()
	service, _, _ := newTestService(t, fixture, newTestClock(base))
	response := exchangeRoot(t, service, fixture, rootLimits(4, 20, 100))

	if !strings.HasPrefix(response.CapabilityToken, TokenPrefix) || response.WorkflowID == "" {
		t.Fatalf("unexpected token response: %#v", response)
	}
	if ttl := response.ExpiresAt.Sub(base); ttl != TokenTTL {
		t.Fatalf("token TTL = %s, want %s", ttl, TokenTTL)
	}
	refreshDelay := response.RefreshAfter.Sub(base)
	if refreshDelay < RefreshBase-RefreshJitter || refreshDelay > RefreshBase+RefreshJitter {
		t.Fatalf("refresh delay outside jitter contract: %s", refreshDelay)
	}

	authentication, err := service.Authenticate(context.Background(), response.CapabilityToken)
	if err != nil {
		t.Fatalf("authenticate capability: %v", err)
	}
	if authentication.Claims.AgentID != "agent:supervisor" || authentication.Claims.Depth != 0 ||
		authentication.APIKey.ID != fixture.keyID || authentication.APIKey.AllowedModels != "gpt-4o" {
		t.Fatalf("unexpected authenticated identity: %#v / %#v", authentication.Claims, authentication.APIKey)
	}

	tampered := response.CapabilityToken[:len(response.CapabilityToken)-1] + "x"
	if _, err := service.Authenticate(context.Background(), tampered); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("tampered token error = %v, want ErrInvalidToken", err)
	}
}

func TestDelegationIsExplicitAndMonotonic(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	fixture := defaultAuthorityFixture()
	service, _, _ := newTestService(t, fixture, newTestClock(base))
	root := exchangeRoot(t, service, fixture, rootLimits(4, 4, 20))
	authenticated, err := service.Authenticate(context.Background(), root.CapabilityToken)
	if err != nil {
		t.Fatal(err)
	}

	childLimits := rootLimits(3, 2, 10)
	childLimits.CostBudgetCents = 1_000
	childLimits.TokenBudget = 100_000
	childLimits.RPMLimit = 50
	childLimits.RPSLimit = 5
	childLimits.ConcurrencyLimit = 5
	child, err := service.Delegate(context.Background(), authenticated.Claims, DelegateRequest{ChildAgentID: "agent:child-1", Limits: childLimits})
	if err != nil {
		t.Fatalf("delegate child: %v", err)
	}
	childAuth, err := service.Authenticate(context.Background(), child.CapabilityToken)
	if err != nil {
		t.Fatalf("authenticate child: %v", err)
	}
	if childAuth.Claims.ParentAgentID != authenticated.Claims.AgentID || childAuth.Claims.Depth != 1 || len(childAuth.Claims.Lineage) != 2 {
		t.Fatalf("unexpected child lineage: %#v", childAuth.Claims)
	}

	widened := childLimits
	widened.RPSLimit = authenticated.Claims.Limits.RPSLimit + 1
	if _, err := service.Delegate(context.Background(), authenticated.Claims, DelegateRequest{ChildAgentID: "agent:too-wide", Limits: widened}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("widened delegation error = %v, want ErrLimitExceeded", err)
	}
	if _, err := service.Delegate(context.Background(), authenticated.Claims, DelegateRequest{ChildAgentID: "agent:child-1", Limits: childLimits}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate child error = %v, want ErrConflict", err)
	}
}

func TestHundredChildAggregateCapIsAtomic(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	fixture := defaultAuthorityFixture()
	service, _, _ := newTestService(t, fixture, newTestClock(base))
	root := exchangeRoot(t, service, fixture, rootLimits(1, 100, 100))
	authenticated, err := service.Authenticate(context.Background(), root.CapabilityToken)
	if err != nil {
		t.Fatal(err)
	}
	childLimits := rootLimits(1, 0, 0)
	childLimits.CostBudgetCents = 100
	childLimits.TokenBudget = 1_000
	childLimits.RPMLimit = 1
	childLimits.RPSLimit = 1
	childLimits.ConcurrencyLimit = 1

	var successes atomic.Int64
	var limited atomic.Int64
	var unexpected atomic.Value
	var wait sync.WaitGroup
	for index := 0; index < 101; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			_, err := service.Delegate(context.Background(), authenticated.Claims, DelegateRequest{
				ChildAgentID: fmt.Sprintf("agent:child-%03d", index),
				Limits:       childLimits,
			})
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrLimitExceeded):
				limited.Add(1)
			default:
				unexpected.Store(err)
			}
		}(index)
	}
	wait.Wait()
	if value := unexpected.Load(); value != nil {
		t.Fatalf("unexpected delegation error: %v", value)
	}
	if successes.Load() != 100 || limited.Load() != 1 {
		t.Fatalf("successes=%d limited=%d, want 100/1", successes.Load(), limited.Load())
	}
}

func TestRefreshIsFiveMinuteAndServerSingleflightSafe(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	fixture := defaultAuthorityFixture()
	clock := newTestClock(base.Add(-3 * time.Minute))
	service, _, _ := newTestService(t, fixture, clock)
	root := exchangeRoot(t, service, fixture, rootLimits(2, 10, 20))
	clock.Set(base)
	authenticated, err := service.Authenticate(context.Background(), root.CapabilityToken)
	if err != nil {
		t.Fatal(err)
	}

	const callers = 20
	tokens := make(chan string, callers)
	errorsCh := make(chan error, callers)
	var wait sync.WaitGroup
	for index := 0; index < callers; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			response, err := service.Refresh(context.Background(), authenticated.Claims)
			if err != nil {
				errorsCh <- err
				return
			}
			tokens <- response.CapabilityToken
		}()
	}
	wait.Wait()
	close(tokens)
	close(errorsCh)
	for err := range errorsCh {
		t.Fatalf("concurrent refresh: %v", err)
	}
	var first string
	count := 0
	for token := range tokens {
		count++
		if first == "" {
			first = token
		} else if token != first {
			t.Fatal("concurrent refresh returned different capability tokens")
		}
	}
	if count != callers {
		t.Fatalf("refresh responses=%d, want %d", count, callers)
	}
	if _, err := service.Authenticate(context.Background(), root.CapabilityToken); !errors.Is(err, ErrSuperseded) {
		t.Fatalf("old token error = %v, want ErrSuperseded", err)
	}
	newAuthentication, err := service.Authenticate(context.Background(), first)
	if err != nil {
		t.Fatalf("authenticate refreshed token: %v", err)
	}
	if ttl := newAuthentication.Claims.ExpiresAt.Time.Sub(newAuthentication.Claims.IssuedAt.Time); ttl != TokenTTL {
		t.Fatalf("refreshed TTL = %s, want %s", ttl, TokenTTL)
	}
}

func TestRefreshBeforeGuardIsRejected(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	fixture := defaultAuthorityFixture()
	service, _, _ := newTestService(t, fixture, newTestClock(base))
	root := exchangeRoot(t, service, fixture, rootLimits(2, 10, 20))
	authenticated, err := service.Authenticate(context.Background(), root.CapabilityToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Refresh(context.Background(), authenticated.Claims); !errors.Is(err, ErrRefreshTooEarly) {
		t.Fatalf("early refresh error = %v, want ErrRefreshTooEarly", err)
	}
}

func TestRevocationConvergesAcrossServicesAndRedisFailsClosed(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	fixture := defaultAuthorityFixture()
	clock := newTestClock(base)
	service, mr, rdb := newTestService(t, fixture, clock)
	root := exchangeRoot(t, service, fixture, rootLimits(2, 10, 20))

	second, err := NewServiceWithOptions(service.db, rdb, "agent-capability-test-root-secret", ServiceOptions{Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Authenticate(context.Background(), root.CapabilityToken); err != nil {
		t.Fatalf("second service authenticate before revoke: %v", err)
	}
	if _, err := service.Revoke(context.Background(), rootKey(fixture), root.WorkflowID); err != nil {
		t.Fatalf("revoke workflow: %v", err)
	}
	if _, err := second.Authenticate(context.Background(), root.CapabilityToken); !errors.Is(err, ErrRevoked) {
		t.Fatalf("second service after revoke = %v, want ErrRevoked", err)
	}

	root2 := exchangeRoot(t, service, fixture, rootLimits(2, 10, 20))
	mr.Close()
	if _, err := service.Authenticate(context.Background(), root2.CapabilityToken); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Redis outage error = %v, want ErrUnavailable", err)
	}
}

func TestRootAuthorityChangeIsRejectedWithinCacheSLO(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	fixture := defaultAuthorityFixture()
	clock := newTestClock(base)
	service, _, _ := newTestService(t, fixture, clock)
	root := exchangeRoot(t, service, fixture, rootLimits(2, 10, 20))
	if _, err := service.Authenticate(context.Background(), root.CapabilityToken); err != nil {
		t.Fatal(err)
	}

	fixture.mu.Lock()
	fixture.keyStatus = "disabled"
	fixture.mu.Unlock()
	clock.Set(base.Add(AuthorityCacheTTL + time.Second))
	if _, err := service.Authenticate(context.Background(), root.CapabilityToken); !errors.Is(err, ErrInactiveRoot) {
		t.Fatalf("authority change error = %v, want ErrInactiveRoot", err)
	}
	if fixture.queryCount.Load() < 2 {
		t.Fatalf("authority was not refreshed after cache SLO; queries=%d", fixture.queryCount.Load())
	}
}
