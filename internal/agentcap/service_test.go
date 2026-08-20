package agentcap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/uniroute/unirouter/internal/accessscope"
	"github.com/uniroute/unirouter/internal/modeloperation"
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
			projectID, ownerUserID := fixture.projectID, fixture.ownerUserID
			budget := int64(fixture.budgetLimit)
			return (testutil.AccessGrantBoundaryFixture{
				KeyID: fixture.keyID, OrgID: fixture.orgID,
				ProjectID: &projectID, OwnerUserID: &ownerUserID,
				KeyType: fixture.keyType, Name: "root-key", Prefix: "ur-0007-root",
				KeyStatus: fixture.keyStatus, AllowedIPs: fixture.allowedIPs,
				AllowedModels: fixture.allowedModels, RPMLimit: fixture.rpmLimit,
				RPSLimit: fixture.rpsLimit, KeyBudgetCents: &budget, AuthEpoch: 1,
				OrgStatus: fixture.orgStatus, ProjectStatus: fixture.projectStatus,
				OwnerStatus: fixture.ownerStatus, OwnerRole: fixture.ownerRole,
				MembershipRole: fixture.membershipRole,
			}).Scan(dest...)
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
		AuthEpoch:     1,
		AllowedModels: fixture.allowedModels,
		RPMLimit:      fixture.rpmLimit,
		QPSLimit:      fixture.rpsLimit,
		BudgetLimit:   &budget,
	}
}

func TestValidateRootLimitsUsesMaterializedGrantDefaultsAndExplicitZero(t *testing.T) {
	authority := authoritySnapshot{APIKey: models.ApiKey{
		ID: 41, OrgID: 7, GrantBound: true, RPMLimit: 120, QPSLimit: 20,
	}}
	if err := validateRootLimits(Limits{RPMLimit: 60, RPSLimit: 10}, authority); err != nil {
		t.Fatalf("nil Grant rates must inherit the canonical defaults: %v", err)
	}
	for _, limits := range []Limits{{RPMLimit: 61, RPSLimit: 10}, {RPMLimit: 60, RPSLimit: 11}} {
		if err := validateRootLimits(limits, authority); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("limits %#v exceeded nil/default Grant rates without rejection: %v", limits, err)
		}
	}

	zero := 0
	authority.APIKey.GrantRPMLimit = &zero
	authority.APIKey.GrantRPSLimit = &zero
	if err := validateRootLimits(Limits{RPMLimit: 1, RPSLimit: 1}, authority); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("explicit zero Grant rates were not fail-closed: %v", err)
	}
}

func TestAgentEndpointsFollowCatalogAndRequireExactRootScope(t *testing.T) {
	limitsFor := func(endpoint string) Limits {
		return Limits{
			MaxDepth: 1, MaxChildren: 1, MaxDescendants: 1,
			CostBudgetCents: 1, TokenBudget: 1,
			RPMLimit: 1, RPSLimit: 1, ConcurrencyLimit: 1,
			AllowedModels: []string{"model"}, AllowedEndpoints: []string{endpoint},
		}
	}
	for _, endpoint := range modeloperation.AgentCapabilityPaths() {
		if err := validateEndpoints([]string{endpoint}); err != nil {
			t.Fatalf("catalog Agent endpoint %q rejected: %v", endpoint, err)
		}
	}
	for _, endpoint := range []string{modeloperation.PathMediaJobs, "/v1/unknown"} {
		if err := validateEndpoints([]string{endpoint}); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("non-Agent endpoint %q error = %v, want ErrInvalidRequest", endpoint, err)
		}
	}

	unbound := authoritySnapshot{APIKey: models.ApiKey{ID: 1}}
	for _, endpoint := range []string{modeloperation.PathChatCompletions, modeloperation.PathEmbeddings, modeloperation.PathModels} {
		if err := validateRootLimits(limitsFor(endpoint), unbound); err != nil {
			t.Fatalf("legacy-unbound root lost %q: %v", endpoint, err)
		}
	}
	for _, endpoint := range []string{modeloperation.PathResponses, modeloperation.PathMessages} {
		if err := validateRootLimits(limitsFor(endpoint), unbound); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("unbound root obtained %q: %v", endpoint, err)
		}

		projectDefault := authoritySnapshot{APIKey: models.ApiKey{
			ID: 1, GrantBound: true, GrantScopeMode: accessscope.ModeProjectDefault,
			AllowedEndpoints: endpoint,
		}}
		if err := validateRootLimits(limitsFor(endpoint), projectDefault); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("project-default root obtained %q: %v", endpoint, err)
		}

		wildcard := authoritySnapshot{APIKey: models.ApiKey{
			ID: 1, GrantBound: true, GrantScopeMode: accessscope.ModeExplicit,
			AllowedEndpoints: "*",
		}}
		if err := validateRootLimits(limitsFor(endpoint), wildcard); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("wildcard root obtained %q: %v", endpoint, err)
		}

		explicit := authoritySnapshot{APIKey: models.ApiKey{
			ID: 1, GrantBound: true, GrantScopeMode: accessscope.ModeExplicit,
			AllowedEndpoints: endpoint,
		}}
		if err := validateRootLimits(limitsFor(endpoint), explicit); err != nil {
			t.Fatalf("exact explicit root could not obtain %q: %v", endpoint, err)
		}
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

func TestNewAgentOperationsRecheckLiveRootAcrossCapabilityLifecycle(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	fixture := defaultAuthorityFixture()
	clock := newTestClock(base)
	service, _, _ := newTestService(t, fixture, clock)

	for _, endpoint := range []string{modeloperation.PathResponses, modeloperation.PathMessages} {
		limits := rootLimits(2, 2, 4)
		limits.AllowedEndpoints = []string{endpoint}
		if _, err := service.Exchange(context.Background(), rootKey(fixture), ExchangeRequest{
			RootAgentID: "agent:unbound-root", WorkflowTTLSeconds: 3600, Limits: limits,
		}); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("unbound Exchange obtained %q: %v", endpoint, err)
		}
	}

	root := exchangeRoot(t, service, fixture, rootLimits(2, 2, 4))
	authenticated, err := service.Authenticate(context.Background(), root.CapabilityToken)
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{modeloperation.PathResponses, modeloperation.PathMessages} {
		forged := *authenticated.Claims
		forged.Limits = authenticated.Claims.Limits
		forged.Limits.AllowedEndpoints = []string{endpoint}
		if _, err := service.Delegate(context.Background(), &forged, DelegateRequest{
			ChildAgentID: "agent:child-" + strings.TrimPrefix(endpoint, "/v1/"),
			Limits:       forged.Limits,
		}); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("unbound Delegate obtained %q: %v", endpoint, err)
		}
	}

	clock.Set(base.Add(3 * time.Minute))
	refreshed := *authenticated.Claims
	refreshed.Limits = authenticated.Claims.Limits
	refreshed.Limits.AllowedEndpoints = []string{modeloperation.PathResponses}
	if _, err := service.Refresh(context.Background(), &refreshed); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("unbound Refresh obtained Responses: %v", err)
	}
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

	tamperIndex := len(TokenPrefix) + (len(response.CapabilityToken)-len(TokenPrefix))/2
	replacement := byte('A')
	if response.CapabilityToken[tamperIndex] == replacement {
		replacement = 'B'
	}
	tamperedBytes := []byte(response.CapabilityToken)
	tamperedBytes[tamperIndex] = replacement
	tampered := string(tamperedBytes)
	if _, err := service.Authenticate(context.Background(), tampered); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("tampered token error = %v, want ErrInvalidToken", err)
	}
}

func TestAgentCapabilityV1RejectsAsyncMediaEndpointAcrossLifecycle(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	fixture := defaultAuthorityFixture()
	service, _, _ := newTestService(t, fixture, newTestClock(base))

	mediaLimits := rootLimits(4, 20, 100)
	mediaLimits.AllowedEndpoints = []string{"/v1/media/jobs"}
	if _, err := service.Exchange(context.Background(), rootKey(fixture), ExchangeRequest{
		RootAgentID:        "agent:media-root",
		WorkflowTTLSeconds: 3600,
		Limits:             mediaLimits,
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("media exchange error = %v, want ErrInvalidRequest", err)
	}

	root := exchangeRoot(t, service, fixture, rootLimits(4, 20, 100))
	authenticated, err := service.Authenticate(context.Background(), root.CapabilityToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Delegate(context.Background(), authenticated.Claims, DelegateRequest{
		ChildAgentID: "agent:media-child",
		Limits:       mediaLimits,
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("media delegation error = %v, want ErrInvalidRequest", err)
	}

	invalidRefresh := *authenticated.Claims
	invalidRefresh.Limits = authenticated.Claims.Limits
	invalidRefresh.Limits.AllowedEndpoints = []string{"/v1/media/jobs"}
	if _, err := service.Refresh(context.Background(), &invalidRefresh); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("media refresh error = %v, want ErrInvalidToken", err)
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

func TestSupersededParentCannotCommitChildAfterDelegationPrecheck(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	fixture := defaultAuthorityFixture()
	service, _, rdb := newTestService(t, fixture, newTestClock(base))
	root := exchangeRoot(t, service, fixture, rootLimits(2, 10, 20))
	parent, err := service.Authenticate(context.Background(), root.CapabilityToken)
	if err != nil {
		t.Fatal(err)
	}

	// Model the exact vulnerable interleaving: Delegate completed its initial
	// state and authority checks, Refresh then committed supersession, and only
	// afterward did Delegate attempt the child/topology commit.
	authority, err := service.loadAuthority(context.Background(), parent.Claims.RootKeyID)
	if err != nil {
		t.Fatal(err)
	}
	childLimits := rootLimits(1, 0, 0)
	childLimits.CostBudgetCents = 1_000
	childLimits.TokenBudget = 100_000
	childLimits.RPMLimit = 50
	childLimits.RPSLimit = 5
	childLimits.ConcurrencyLimit = 5
	lineage := append(slices.Clone(parent.Claims.Lineage), "agent:raced-child")
	childClaims, _, err := service.newClaims(
		authority,
		parent.Claims.WorkflowID,
		parent.Claims.WorkflowGeneration,
		time.Unix(parent.Claims.WorkflowExpiresAt, 0).UTC(),
		parent.Claims.RootAgentID,
		parent.Claims.AgentID,
		lineage,
		parent.Claims.Depth+1,
		childLimits,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.reserveRefreshGrant(context.Background(), parent.Claims); err != nil {
		t.Fatal(err)
	}

	if err := service.registerChild(context.Background(), parent.Claims, childClaims); !errors.Is(err, ErrSuperseded) {
		t.Fatalf("child commit after parent supersession error = %v, want ErrSuperseded", err)
	}
	if exists, err := rdb.Exists(context.Background(), agentKey(parent.Claims.WorkflowID, childClaims.AgentID)).Result(); err != nil || exists != 0 {
		t.Fatalf("superseded parent left child state: exists=%d err=%v", exists, err)
	}
	workflowCount, err := rdb.HGet(context.Background(), workflowKey(parent.Claims.WorkflowID), "descendant_count").Int64()
	if err != nil || workflowCount != 0 {
		t.Fatalf("superseded parent changed workflow count: count=%d err=%v", workflowCount, err)
	}
	parentCount, err := rdb.HGet(context.Background(), agentKey(parent.Claims.WorkflowID, parent.Claims.AgentID), "children_count").Int64()
	if err != nil || parentCount != 0 {
		t.Fatalf("superseded parent changed parent count: count=%d err=%v", parentCount, err)
	}
}

func TestChildCommittedBeforeParentRefreshRemainsValid(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	fixture := defaultAuthorityFixture()
	clock := newTestClock(base.Add(-3 * time.Minute))
	service, _, _ := newTestService(t, fixture, clock)
	root := exchangeRoot(t, service, fixture, rootLimits(2, 10, 20))
	clock.Set(base)
	parent, err := service.Authenticate(context.Background(), root.CapabilityToken)
	if err != nil {
		t.Fatal(err)
	}
	childLimits := rootLimits(1, 0, 0)
	childLimits.CostBudgetCents = 1_000
	childLimits.TokenBudget = 100_000
	childLimits.RPMLimit = 50
	childLimits.RPSLimit = 5
	childLimits.ConcurrencyLimit = 5
	child, err := service.Delegate(context.Background(), parent.Claims, DelegateRequest{
		ChildAgentID: "agent:committed-before-refresh",
		Limits:       childLimits,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Refresh(context.Background(), parent.Claims); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(context.Background(), child.CapabilityToken); err != nil {
		t.Fatalf("already committed child was retroactively revoked: %v", err)
	}
}

func TestLegacySupersededMarkerRemainsFailClosedDuringBoundedReadBridge(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	fixture := defaultAuthorityFixture()
	service, _, rdb := newTestService(t, fixture, newTestClock(base))
	root := exchangeRoot(t, service, fixture, rootLimits(2, 10, 20))
	authenticated, err := service.Authenticate(context.Background(), root.CapabilityToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := rdb.Set(context.Background(), legacySupersededKey(authenticated.Claims.ID), "1", TokenTTL).Err(); err != nil {
		t.Fatal(err)
	}
	if exists, err := rdb.Exists(context.Background(), supersededKey(authenticated.Claims.WorkflowID, authenticated.Claims.ID)).Result(); err != nil || exists != 0 {
		t.Fatalf("fixture unexpectedly wrote new marker: exists=%d err=%v", exists, err)
	}
	if _, err := service.Authenticate(context.Background(), root.CapabilityToken); !errors.Is(err, ErrSuperseded) {
		t.Fatalf("legacy superseded token error = %v, want ErrSuperseded", err)
	}
}

func TestLegacyRefreshGrantReplaysDuringBoundedReadBridge(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	fixture := defaultAuthorityFixture()
	clock := newTestClock(base.Add(-3 * time.Minute))
	service, _, rdb := newTestService(t, fixture, clock)
	root := exchangeRoot(t, service, fixture, rootLimits(2, 10, 20))
	clock.Set(base)
	authenticated, err := service.Authenticate(context.Background(), root.CapabilityToken)
	if err != nil {
		t.Fatal(err)
	}
	grant := refreshGrant{JTI: "capjti_legacy_bridge_fixture", IssuedAt: base.Unix()}
	encoded, err := json.Marshal(grant)
	if err != nil {
		t.Fatal(err)
	}
	pipe := rdb.TxPipeline()
	pipe.Set(context.Background(), legacyRefreshKey(authenticated.Claims.ID), string(encoded), TokenTTL)
	pipe.Set(context.Background(), legacySupersededKey(authenticated.Claims.ID), "1", TokenTTL)
	if _, err := pipe.Exec(context.Background()); err != nil {
		t.Fatal(err)
	}

	refreshed, err := service.Refresh(context.Background(), authenticated.Claims)
	if err != nil {
		t.Fatalf("replay legacy refresh grant: %v", err)
	}
	refreshedAuth, err := service.Authenticate(context.Background(), refreshed.CapabilityToken)
	if err != nil {
		t.Fatalf("authenticate legacy-bridge refreshed token: %v", err)
	}
	if refreshedAuth.Claims.ID != grant.JTI {
		t.Fatalf("refreshed JTI = %q, want legacy grant %q", refreshedAuth.Claims.ID, grant.JTI)
	}
}

func TestRefreshAndChildCommitKeysShareWorkflowClusterSlot(t *testing.T) {
	workflowID := "capwf_cluster_slot_fixture"
	wantTag := "{" + workflowID + "}"
	keys := []string{
		workflowKey(workflowID),
		agentKey(workflowID, "agent:parent"),
		refreshKey(workflowID, "capjti_parent"),
		supersededKey(workflowID, "capjti_parent"),
	}
	for _, key := range keys {
		if !strings.Contains(key, wantTag) {
			t.Fatalf("key %q does not share workflow hash tag %q", key, wantTag)
		}
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

func TestInvocationBudgetsAggregateAcrossAncestorsAndSettleActualUsage(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	fixture := defaultAuthorityFixture()
	service, _, rdb := newTestService(t, fixture, newTestClock(base))
	limits := rootLimits(1, 2, 2)
	limits.CostBudgetCents = 100
	limits.TokenBudget = 1_000
	limits.RPMLimit = 100
	limits.RPSLimit = 20
	limits.ConcurrencyLimit = 10
	root := exchangeRoot(t, service, fixture, limits)
	rootAuth, err := service.Authenticate(context.Background(), root.CapabilityToken)
	if err != nil {
		t.Fatal(err)
	}
	childLimits := limits
	childLimits.MaxChildren = 0
	childLimits.MaxDescendants = 0
	childLimits.CostBudgetCents = 80
	childLimits.TokenBudget = 800
	first, err := service.Delegate(context.Background(), rootAuth.Claims, DelegateRequest{ChildAgentID: "agent:first", Limits: childLimits})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Delegate(context.Background(), rootAuth.Claims, DelegateRequest{ChildAgentID: "agent:second", Limits: childLimits})
	if err != nil {
		t.Fatal(err)
	}
	firstAuth, err := service.Authenticate(context.Background(), first.CapabilityToken)
	if err != nil {
		t.Fatal(err)
	}
	secondAuth, err := service.Authenticate(context.Background(), second.CapabilityToken)
	if err != nil {
		t.Fatal(err)
	}

	reservation, err := service.ReserveInvocation(context.Background(), firstAuth.Claims, "req_first", 600, 600_000, time.Minute)
	if err != nil {
		t.Fatalf("reserve first child: %v", err)
	}
	if err := service.FinalizeInvocation(context.Background(), reservation, 500, 500_000); err != nil {
		t.Fatalf("finalize first child: %v", err)
	}
	rootState, err := rdb.HMGet(context.Background(), agentKey(root.WorkflowID, rootAuth.Claims.AgentID),
		"cost_reserved_micros", "cost_used_micros", "token_reserved", "token_used").Result()
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(rootState) != "[0 500000 0 500]" {
		t.Fatalf("unexpected root usage after settlement: %v", rootState)
	}

	if _, err := service.ReserveInvocation(context.Background(), secondAuth.Claims, "req_cost_denied", 400, 600_000, time.Minute); !errors.Is(err, ErrCostBudgetExceeded) {
		t.Fatalf("aggregate cost denial = %v, want ErrCostBudgetExceeded", err)
	}
	if _, err := service.ReserveInvocation(context.Background(), secondAuth.Claims, "req_token_denied", 600, 400_000, time.Minute); !errors.Is(err, ErrTokenBudgetExceeded) {
		t.Fatalf("aggregate token denial = %v, want ErrTokenBudgetExceeded", err)
	}
	secondReservation, err := service.ReserveInvocation(context.Background(), secondAuth.Claims, "req_second", 400, 400_000, time.Minute)
	if err != nil {
		t.Fatalf("reserve remaining ancestor budget: %v", err)
	}
	if err := service.CancelInvocation(context.Background(), secondReservation); err != nil {
		t.Fatalf("cancel second child: %v", err)
	}
	rootState, err = rdb.HMGet(context.Background(), agentKey(root.WorkflowID, rootAuth.Claims.AgentID),
		"cost_reserved_micros", "cost_used_micros", "token_reserved", "token_used").Result()
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(rootState) != "[0 500000 0 500]" {
		t.Fatalf("cancel must release reservations without refunding usage: %v", rootState)
	}
}

func TestP6F11InvocationExtensionCoversEveryAncestorAndIsIdempotent(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	fixture := defaultAuthorityFixture()
	service, _, rdb := newTestService(t, fixture, newTestClock(base))
	limits := rootLimits(1, 1, 1)
	limits.CostBudgetCents = 100
	limits.TokenBudget = 100
	limits.RPMLimit = 100
	limits.RPSLimit = 20
	limits.ConcurrencyLimit = 10
	root := exchangeRoot(t, service, fixture, limits)
	rootAuth, err := service.Authenticate(context.Background(), root.CapabilityToken)
	if err != nil {
		t.Fatal(err)
	}
	childLimits := limits
	childLimits.MaxChildren = 0
	childLimits.MaxDescendants = 0
	child, err := service.Delegate(context.Background(), rootAuth.Claims, DelegateRequest{
		ChildAgentID: "agent:p6f11-child", Limits: childLimits,
	})
	if err != nil {
		t.Fatal(err)
	}
	childAuth, err := service.Authenticate(context.Background(), child.CapabilityToken)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := service.ReserveInvocation(context.Background(), childAuth.Claims, "req_p6f11_extension", 40, 400_000, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ExtendInvocation(context.Background(), reservation, 1, 30, 300_000); err != nil {
		t.Fatalf("first ancestor extension: %v", err)
	}
	if err := service.ExtendInvocation(context.Background(), reservation, 1, 30, 300_000); err != nil {
		t.Fatalf("idempotent ancestor extension replay: %v", err)
	}
	if reservation.ReservedTokens != 70 || reservation.ReservedCostMicros != 700_000 || reservation.ExtensionSequence != 1 {
		t.Fatalf("extension was not applied exactly once: %#v", reservation)
	}
	for _, agentID := range reservation.Lineage {
		state, readErr := rdb.HMGet(context.Background(), agentKey(root.WorkflowID, agentID),
			"cost_reserved_micros", "token_reserved").Result()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if fmt.Sprint(state) != "[700000 70]" {
			t.Fatalf("ancestor %s did not reserve the extension: %v", agentID, state)
		}
	}
	if err := service.ExtendInvocation(context.Background(), reservation, 2, 40, 400_000); !errors.Is(err, ErrCostBudgetExceeded) {
		t.Fatalf("extension beyond ancestor budget = %v, want ErrCostBudgetExceeded", err)
	}
	if reservation.ExtensionSequence != 1 || reservation.ReservedTokens != 70 || reservation.ReservedCostMicros != 700_000 {
		t.Fatalf("denied extension changed local authority: %#v", reservation)
	}
	if err := service.FinalizeInvocation(context.Background(), reservation, 50, 500_000); err != nil {
		t.Fatal(err)
	}
	for _, agentID := range reservation.Lineage {
		state, readErr := rdb.HMGet(context.Background(), agentKey(root.WorkflowID, agentID),
			"cost_reserved_micros", "cost_used_micros", "token_reserved", "token_used").Result()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if fmt.Sprint(state) != "[0 500000 0 50]" {
			t.Fatalf("ancestor %s retained the terminal extension: %v", agentID, state)
		}
	}
}

func TestInvocationRateAttemptsAreNotRefundedAndConcurrencyIsReleased(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	fixture := defaultAuthorityFixture()
	service, mr, _ := newTestService(t, fixture, newTestClock(base))
	mr.SetTime(base)
	limits := rootLimits(0, 0, 0)
	limits.RPMLimit = 100
	limits.RPSLimit = 1
	limits.ConcurrencyLimit = 1
	root := exchangeRoot(t, service, fixture, limits)
	authenticated, err := service.Authenticate(context.Background(), root.CapabilityToken)
	if err != nil {
		t.Fatal(err)
	}

	first, err := service.ReserveInvocation(context.Background(), authenticated.Claims, "req_rate_one", 10, 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := service.ReserveInvocation(context.Background(), authenticated.Claims, "req_rate_one", 10, 10, time.Minute)
	if err != nil || duplicate.OperationID != first.OperationID {
		t.Fatalf("idempotent reservation = %#v / %v", duplicate, err)
	}
	if err := service.CancelInvocation(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReserveInvocation(context.Background(), authenticated.Claims, "req_rate_two", 10, 10, time.Minute); !errors.Is(err, ErrRPSLimitExceeded) {
		t.Fatalf("cancelled attempt must still count in RPS: %v", err)
	}
	mr.SetTime(base.Add(1100 * time.Millisecond))
	second, err := service.ReserveInvocation(context.Background(), authenticated.Claims, "req_rate_two", 10, 10, time.Minute)
	if err != nil {
		t.Fatalf("reservation after RPS window: %v", err)
	}
	mr.SetTime(base.Add(3300 * time.Millisecond))
	if _, err := service.ReserveInvocation(context.Background(), authenticated.Claims, "req_concurrency_three", 10, 10, time.Minute); !errors.Is(err, ErrConcurrencyExceeded) {
		t.Fatalf("shared concurrency denial = %v, want ErrConcurrencyExceeded", err)
	}
	if err := service.CancelInvocation(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	mr.SetTime(base.Add(4400 * time.Millisecond))
	if _, err := service.ReserveInvocation(context.Background(), authenticated.Claims, "req_concurrency_three", 10, 10, time.Minute); err != nil {
		t.Fatalf("released concurrency must admit next request: %v", err)
	}
}

func TestInvocationRPMAggregatesAfterRPSWindow(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	fixture := defaultAuthorityFixture()
	service, mr, _ := newTestService(t, fixture, newTestClock(base))
	mr.SetTime(base)
	limits := rootLimits(0, 0, 0)
	limits.RPMLimit = 1
	limits.RPSLimit = 20
	limits.ConcurrencyLimit = 10
	root := exchangeRoot(t, service, fixture, limits)
	authenticated, err := service.Authenticate(context.Background(), root.CapabilityToken)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.ReserveInvocation(context.Background(), authenticated.Claims, "req_rpm_one", 10, 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.CancelInvocation(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	mr.SetTime(base.Add(2 * time.Second))
	if _, err := service.ReserveInvocation(context.Background(), authenticated.Claims, "req_rpm_two", 10, 10, time.Minute); !errors.Is(err, ErrRPMLimitExceeded) {
		t.Fatalf("RPM denial after RPS window = %v, want ErrRPMLimitExceeded", err)
	}
	mr.SetTime(base.Add(61 * time.Second))
	if _, err := service.ReserveInvocation(context.Background(), authenticated.Claims, "req_rpm_two", 10, 10, time.Minute); err != nil {
		t.Fatalf("reservation after RPM window: %v", err)
	}
}

func TestInvocationConcurrencyIsReplicaInvariant(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	fixture := defaultAuthorityFixture()
	clock := newTestClock(base)
	firstService, _, rdb := newTestService(t, fixture, clock)
	secondService, err := NewServiceWithOptions(firstService.db, rdb, "agent-capability-test-root-secret", ServiceOptions{Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	limits := rootLimits(0, 0, 0)
	limits.RPMLimit = 100
	limits.RPSLimit = 20
	limits.ConcurrencyLimit = 3
	root := exchangeRoot(t, firstService, fixture, limits)
	authenticated, err := firstService.Authenticate(context.Background(), root.CapabilityToken)
	if err != nil {
		t.Fatal(err)
	}

	var admitted atomic.Int64
	var rejected atomic.Int64
	var unexpected atomic.Value
	var wait sync.WaitGroup
	for index := 0; index < 20; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			service := firstService
			if index%2 == 1 {
				service = secondService
			}
			_, reserveErr := service.ReserveInvocation(context.Background(), authenticated.Claims, fmt.Sprintf("req_replica_%02d", index), 10, 10, time.Minute)
			switch {
			case reserveErr == nil:
				admitted.Add(1)
			case errors.Is(reserveErr, ErrConcurrencyExceeded):
				rejected.Add(1)
			default:
				unexpected.Store(reserveErr)
			}
		}(index)
	}
	wait.Wait()
	if value := unexpected.Load(); value != nil {
		t.Fatalf("unexpected replica reservation error: %v", value)
	}
	if admitted.Load() != 3 || rejected.Load() != 17 {
		t.Fatalf("admitted=%d rejected=%d, want 3/17", admitted.Load(), rejected.Load())
	}
}

func TestInvocationConcurrencyLeaseUsesRedisClock(t *testing.T) {
	redisNow := time.Now().UTC().Truncate(time.Second)
	fixture := defaultAuthorityFixture()
	serviceClock := newTestClock(redisNow.Add(2 * time.Hour))
	service, mr, rdb := newTestService(t, fixture, serviceClock)
	mr.SetTime(redisNow)
	limits := rootLimits(0, 0, 0)
	root := exchangeRoot(t, service, fixture, limits)
	authenticated, err := service.Authenticate(context.Background(), root.CapabilityToken)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := service.ReserveInvocation(context.Background(), authenticated.Claims, "req_redis_clock", 10, 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	score, err := rdb.ZScore(context.Background(), invocationConcurrencyKey(root.WorkflowID, authenticated.Claims.AgentID), invocationOperationDigest(reservation.OperationID)).Result()
	if err != nil {
		t.Fatal(err)
	}
	want := float64(redisNow.Add(time.Minute + InvocationFinalizationGrace).UnixMilli())
	if score != want {
		t.Fatalf("concurrency lease score=%.0f want Redis TIME based %.0f", score, want)
	}
}

func TestInvocationTerminalStateIsBoundToTheReservedLineage(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	fixture := defaultAuthorityFixture()
	service, _, _ := newTestService(t, fixture, newTestClock(base))
	root := exchangeRoot(t, service, fixture, rootLimits(1, 1, 1))
	rootAuth, err := service.Authenticate(context.Background(), root.CapabilityToken)
	if err != nil {
		t.Fatal(err)
	}
	childLimits := rootLimits(1, 0, 0)
	child, err := service.Delegate(context.Background(), rootAuth.Claims, DelegateRequest{ChildAgentID: "agent:lineage-child", Limits: childLimits})
	if err != nil {
		t.Fatal(err)
	}
	childAuth, err := service.Authenticate(context.Background(), child.CapabilityToken)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := service.ReserveInvocation(context.Background(), childAuth.Claims, "req_lineage_bound", 10, 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	tampered := *reservation
	tampered.Lineage = []string{reservation.AgentID}
	if err := service.FinalizeInvocation(context.Background(), &tampered, 5, 5); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("tampered lineage finalization = %v, want ErrUnavailable", err)
	}
	if err := service.CancelInvocation(context.Background(), reservation); err != nil {
		t.Fatalf("original lineage cancellation: %v", err)
	}
}

func TestInvocationActualOverrunExhaustsFutureAdmissionsAndRedisFailsClosed(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	fixture := defaultAuthorityFixture()
	service, mr, _ := newTestService(t, fixture, newTestClock(base))
	limits := rootLimits(0, 0, 0)
	limits.CostBudgetCents = 100
	limits.TokenBudget = 1_000
	limits.RPMLimit = 100
	limits.RPSLimit = 20
	limits.ConcurrencyLimit = 10
	root := exchangeRoot(t, service, fixture, limits)
	authenticated, err := service.Authenticate(context.Background(), root.CapabilityToken)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := service.ReserveInvocation(context.Background(), authenticated.Claims, "req_overrun", 100, 800_000, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.FinalizeInvocation(context.Background(), reservation, 100, 1_200_000); err != nil {
		t.Fatalf("finalize actual overrun: %v", err)
	}
	if _, err := service.ReserveInvocation(context.Background(), authenticated.Claims, "req_after_overrun", 1, 10_000, time.Minute); !errors.Is(err, ErrCostBudgetExceeded) {
		t.Fatalf("future request after actual overrun = %v, want ErrCostBudgetExceeded", err)
	}

	secondRoot := exchangeRoot(t, service, fixture, limits)
	secondAuth, err := service.Authenticate(context.Background(), secondRoot.CapabilityToken)
	if err != nil {
		t.Fatal(err)
	}
	mr.Close()
	if _, err := service.ReserveInvocation(context.Background(), secondAuth.Claims, "req_redis_down", 1, 1, time.Minute); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Redis outage admission = %v, want ErrUnavailable", err)
	}
}
