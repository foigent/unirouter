package agentcap

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/singleflight"

	"github.com/uniroute/unirouter/internal/accessgrantboundary"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/models"
)

type authoritySnapshot struct {
	APIKey         models.ApiKey
	OrgStatus      string
	ProjectStatus  string
	OwnerStatus    string
	OwnerRole      string
	MembershipRole string
	Boundary       accessgrantboundary.Snapshot
	Version        string
}

func (a authoritySnapshot) active() bool {
	return a.APIKey.ID > 0
}

type authorityCacheEntry struct {
	key       int64
	snapshot  authoritySnapshot
	expiresAt time.Time
}

type authorityCache struct {
	mu         sync.Mutex
	entries    map[int64]*list.Element
	lru        *list.List
	maxEntries int
	ttl        time.Duration
	group      singleflight.Group
}

func newAuthorityCache(maxEntries int, ttl time.Duration) *authorityCache {
	return &authorityCache{
		entries:    make(map[int64]*list.Element),
		lru:        list.New(),
		maxEntries: maxEntries,
		ttl:        ttl,
	}
}

func (c *authorityCache) get(key int64, now time.Time) (authoritySnapshot, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.entries[key]
	if !ok {
		return authoritySnapshot{}, false
	}
	entry := element.Value.(*authorityCacheEntry)
	if !now.Before(entry.expiresAt) {
		c.lru.Remove(element)
		delete(c.entries, key)
		return authoritySnapshot{}, false
	}
	c.lru.MoveToFront(element)
	return entry.snapshot, true
}

func (c *authorityCache) put(key int64, snapshot authoritySnapshot, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.entries[key]; ok {
		entry := element.Value.(*authorityCacheEntry)
		entry.snapshot = snapshot
		entry.expiresAt = now.Add(c.ttl)
		c.lru.MoveToFront(element)
		return
	}
	entry := &authorityCacheEntry{key: key, snapshot: snapshot, expiresAt: now.Add(c.ttl)}
	c.entries[key] = c.lru.PushFront(entry)
	for c.lru.Len() > c.maxEntries {
		oldest := c.lru.Back()
		if oldest == nil {
			break
		}
		c.lru.Remove(oldest)
		delete(c.entries, oldest.Value.(*authorityCacheEntry).key)
	}
}

func (s *Service) loadAuthority(ctx context.Context, keyID int64) (authoritySnapshot, error) {
	now := s.now()
	if cached, ok := s.authorities.get(keyID, now); ok && !cached.APIKey.GrantBound {
		return cached, nil
	}

	value, err, _ := s.authorities.group.Do(fmt.Sprintf("%d", keyID), func() (interface{}, error) {
		if cached, ok := s.authorities.get(keyID, s.now()); ok && !cached.APIKey.GrantBound {
			return cached, nil
		}
		snapshot, err := queryAuthority(ctx, s.db, keyID)
		if err != nil {
			return authoritySnapshot{}, mapAuthorityError(err)
		}
		s.authorities.put(keyID, snapshot, s.now())
		return snapshot, nil
	})
	if err != nil {
		return authoritySnapshot{}, err
	}
	return value.(authoritySnapshot), nil
}

func mapAuthorityError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, accessgrantboundary.ErrInactive) {
		return ErrInactiveRoot
	}
	return fmt.Errorf("%w: load root authority: %v", ErrUnavailable, err)
}

func queryAuthority(ctx context.Context, q db.DBQuerier, keyID int64) (authoritySnapshot, error) {
	boundary, err := accessgrantboundary.Resolve(ctx, q, keyID)
	if err != nil {
		return authoritySnapshot{}, err
	}
	snapshot := authoritySnapshot{
		APIKey: boundary.APIKey, OrgStatus: boundary.OrgStatus,
		ProjectStatus: boundary.ProjectStatus, OwnerStatus: boundary.OwnerStatus,
		OwnerRole: boundary.OwnerRole, MembershipRole: boundary.MembershipRole,
		Boundary: boundary,
	}
	snapshot.Version = authorityVersion(snapshot)
	return snapshot, nil
}

func authorityVersion(snapshot authoritySnapshot) string {
	type versionInput struct {
		KeyID                   int64  `json:"key_id"`
		OrgID                   int64  `json:"org_id"`
		ProjectID               int64  `json:"project_id"`
		OwnerUserID             int64  `json:"owner_user_id"`
		KeyType                 string `json:"key_type"`
		KeyStatus               string `json:"key_status"`
		AuthEpoch               int64  `json:"auth_epoch"`
		Purpose                 string `json:"purpose"`
		OrgStatus               string `json:"org_status"`
		ProjectStatus           string `json:"project_status"`
		OwnerStatus             string `json:"owner_status"`
		OwnerRole               string `json:"owner_role"`
		MembershipRole          string `json:"membership_role"`
		AllowedIPs              string `json:"allowed_ips"`
		AllowedModels           string `json:"allowed_models"`
		RPMLimit                int    `json:"rpm_limit"`
		RPSLimit                int    `json:"rps_limit"`
		BudgetLimit             int    `json:"budget_limit"`
		AllowedEndpoints        string `json:"allowed_endpoints"`
		AccessGrantID           int64  `json:"access_grant_id"`
		GrantRevision           int64  `json:"grant_revision"`
		GrantScopeMode          string `json:"grant_scope_mode"`
		GrantSubjectType        string `json:"grant_subject_type"`
		GrantSubjectUserID      int64  `json:"grant_subject_user_id"`
		GrantSubjectWorkloadID  int64  `json:"grant_subject_workload_id"`
		GrantSponsorUserID      int64  `json:"grant_sponsor_user_id"`
		SponsorOrgID            int64  `json:"sponsor_org_id"`
		SponsorStatus           string `json:"sponsor_status"`
		SponsorOrganizationRole string `json:"sponsor_organization_role"`
		SponsorMembershipRole   string `json:"sponsor_membership_role"`
		GrantProjectRole        string `json:"grant_project_role"`
		GrantEnvironment        string `json:"grant_environment"`
		GrantProductionUse      bool   `json:"grant_production_use"`
		GrantRPMLimit           int    `json:"grant_rpm_limit"`
		GrantRPSLimit           int    `json:"grant_rps_limit"`
		GrantConcurrencyLimit   int    `json:"grant_concurrency_limit"`
		WorkloadApplicationID   int64  `json:"workload_application_id"`
		WorkloadStatus          string `json:"workload_status"`
		WorkloadKind            string `json:"workload_kind"`
		WorkloadIdentity        string `json:"workload_identity"`
		WorkloadEnvironment     string `json:"workload_environment"`
		WorkloadProductionUse   bool   `json:"workload_production_use"`
	}
	input := versionInput{
		KeyID:                   snapshot.APIKey.ID,
		OrgID:                   snapshot.APIKey.OrgID,
		KeyType:                 snapshot.APIKey.KeyType,
		KeyStatus:               snapshot.APIKey.Status,
		AuthEpoch:               snapshot.APIKey.AuthEpoch,
		Purpose:                 snapshot.APIKey.Purpose,
		OrgStatus:               snapshot.OrgStatus,
		ProjectStatus:           snapshot.ProjectStatus,
		OwnerStatus:             snapshot.OwnerStatus,
		OwnerRole:               snapshot.OwnerRole,
		MembershipRole:          snapshot.MembershipRole,
		AllowedIPs:              strings.TrimSpace(snapshot.APIKey.IPWhitelist),
		AllowedModels:           strings.Join(normalizedSet(strings.Split(snapshot.APIKey.AllowedModels, ",")), ","),
		AllowedEndpoints:        strings.Join(normalizedSet(strings.Split(snapshot.APIKey.AllowedEndpoints, ",")), ","),
		RPMLimit:                effectiveRPM(snapshot.APIKey.RPMLimit),
		RPSLimit:                effectiveRPS(snapshot.APIKey.QPSLimit),
		BudgetLimit:             -1,
		GrantRevision:           snapshot.Boundary.GrantRevision,
		GrantScopeMode:          snapshot.APIKey.GrantScopeMode,
		GrantSubjectType:        snapshot.Boundary.GrantSubjectType,
		GrantSubjectUserID:      snapshot.Boundary.GrantSubjectUserID,
		GrantSubjectWorkloadID:  snapshot.Boundary.GrantSubjectWorkloadID,
		GrantSponsorUserID:      snapshot.Boundary.GrantSponsorUserID,
		SponsorOrgID:            snapshot.Boundary.SponsorOrgID,
		SponsorStatus:           snapshot.Boundary.SponsorStatus,
		SponsorOrganizationRole: snapshot.Boundary.SponsorOrganizationRole,
		SponsorMembershipRole:   snapshot.Boundary.SponsorMembershipRole,
		GrantProjectRole:        snapshot.Boundary.GrantProjectRole,
		GrantEnvironment:        snapshot.Boundary.GrantEnvironment,
		GrantProductionUse:      snapshot.Boundary.GrantProductionUse,
		WorkloadStatus:          snapshot.Boundary.WorkloadStatus,
		WorkloadKind:            snapshot.Boundary.WorkloadKind,
		WorkloadIdentity:        snapshot.Boundary.WorkloadIdentity,
		WorkloadEnvironment:     snapshot.Boundary.WorkloadEnvironment,
		WorkloadProductionUse:   snapshot.Boundary.WorkloadProductionUse,
	}
	if snapshot.APIKey.ProjectID != nil {
		input.ProjectID = *snapshot.APIKey.ProjectID
	}
	if snapshot.APIKey.OwnerUserID != nil {
		input.OwnerUserID = *snapshot.APIKey.OwnerUserID
	}
	if snapshot.APIKey.BudgetLimit != nil {
		input.BudgetLimit = *snapshot.APIKey.BudgetLimit
	}
	if snapshot.APIKey.AccessGrantID != nil {
		input.AccessGrantID = *snapshot.APIKey.AccessGrantID
	}
	if snapshot.APIKey.WorkloadApplicationID != nil {
		input.WorkloadApplicationID = *snapshot.APIKey.WorkloadApplicationID
	}
	if snapshot.APIKey.GrantRPMLimit != nil {
		input.GrantRPMLimit = *snapshot.APIKey.GrantRPMLimit
	}
	if snapshot.APIKey.GrantRPSLimit != nil {
		input.GrantRPSLimit = *snapshot.APIKey.GrantRPSLimit
	}
	if snapshot.APIKey.GrantConcurrencyLimit != nil {
		input.GrantConcurrencyLimit = *snapshot.APIKey.GrantConcurrencyLimit
	}
	encoded, _ := json.Marshal(input)
	digest := sha256.Sum256(encoded)
	return "authz-sha256:" + hex.EncodeToString(digest[:])
}

func effectiveRPM(value int) int {
	if value <= 0 {
		return 60
	}
	return value
}

func effectiveRPS(value int) int {
	if value <= 0 {
		return 10
	}
	return value
}
