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
	Version        string
}

func (a authoritySnapshot) active() bool {
	if a.APIKey.ID <= 0 || a.APIKey.Status != "active" || a.OrgStatus != "active" {
		return false
	}
	if a.APIKey.ProjectID != nil && a.ProjectStatus != "active" {
		return false
	}
	if a.APIKey.OwnerUserID != nil {
		if a.OwnerStatus != "active" {
			return false
		}
		if a.APIKey.ProjectID != nil && a.MembershipRole == "" {
			return false
		}
	}
	return true
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
	if cached, ok := s.authorities.get(keyID, now); ok {
		return cached, nil
	}

	value, err, _ := s.authorities.group.Do(fmt.Sprintf("%d", keyID), func() (interface{}, error) {
		if cached, ok := s.authorities.get(keyID, s.now()); ok {
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
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInactiveRoot
	}
	return fmt.Errorf("%w: load root authority: %v", ErrUnavailable, err)
}

func queryAuthority(ctx context.Context, q db.DBQuerier, keyID int64) (authoritySnapshot, error) {
	var snapshot authoritySnapshot
	var projectID, ownerUserID int64
	var budgetLimit int
	err := q.QueryRow(ctx, `
		SELECT ak.id, ak.org_id, COALESCE(ak.project_id, 0), COALESCE(ak.owner_user_id, 0),
		       COALESCE(NULLIF(ak.key_type, ''), 'shared'), ak.name, ak.key_prefix,
		       ak.status, COALESCE(ak.allowed_ips, ''), COALESCE(ak.allowed_models, ''),
		       ak.rpm_limit, ak.qps_limit, COALESCE(ak.budget_limit, -1),
		       o.status,
		       COALESCE(p.status, ''), COALESCE(u.status, ''), COALESCE(u.role, ''),
		       COALESCE(pm.role, '')
		FROM api_keys ak
		JOIN orgs o ON o.id = ak.org_id
		LEFT JOIN projects p ON p.id = ak.project_id AND p.org_id = ak.org_id
		LEFT JOIN users u ON u.id = ak.owner_user_id AND u.org_id = ak.org_id
		LEFT JOIN project_members pm ON pm.project_id = ak.project_id AND pm.user_id = ak.owner_user_id
		WHERE ak.id = $1`, keyID).Scan(
		&snapshot.APIKey.ID,
		&snapshot.APIKey.OrgID,
		&projectID,
		&ownerUserID,
		&snapshot.APIKey.KeyType,
		&snapshot.APIKey.Name,
		&snapshot.APIKey.KeyPrefix,
		&snapshot.APIKey.Status,
		&snapshot.APIKey.IPWhitelist,
		&snapshot.APIKey.AllowedModels,
		&snapshot.APIKey.RPMLimit,
		&snapshot.APIKey.QPSLimit,
		&budgetLimit,
		&snapshot.OrgStatus,
		&snapshot.ProjectStatus,
		&snapshot.OwnerStatus,
		&snapshot.OwnerRole,
		&snapshot.MembershipRole,
	)
	if err != nil {
		return authoritySnapshot{}, err
	}
	if projectID > 0 {
		snapshot.APIKey.ProjectID = &projectID
	}
	if ownerUserID > 0 {
		snapshot.APIKey.OwnerUserID = &ownerUserID
	}
	if budgetLimit >= 0 {
		snapshot.APIKey.BudgetLimit = &budgetLimit
	}
	snapshot.Version = authorityVersion(snapshot)
	return snapshot, nil
}

func authorityVersion(snapshot authoritySnapshot) string {
	type versionInput struct {
		KeyID          int64  `json:"key_id"`
		OrgID          int64  `json:"org_id"`
		ProjectID      int64  `json:"project_id"`
		OwnerUserID    int64  `json:"owner_user_id"`
		KeyType        string `json:"key_type"`
		KeyStatus      string `json:"key_status"`
		OrgStatus      string `json:"org_status"`
		ProjectStatus  string `json:"project_status"`
		OwnerStatus    string `json:"owner_status"`
		OwnerRole      string `json:"owner_role"`
		MembershipRole string `json:"membership_role"`
		AllowedIPs     string `json:"allowed_ips"`
		AllowedModels  string `json:"allowed_models"`
		RPMLimit       int    `json:"rpm_limit"`
		RPSLimit       int    `json:"rps_limit"`
		BudgetLimit    int    `json:"budget_limit"`
	}
	input := versionInput{
		KeyID:          snapshot.APIKey.ID,
		OrgID:          snapshot.APIKey.OrgID,
		KeyType:        snapshot.APIKey.KeyType,
		KeyStatus:      snapshot.APIKey.Status,
		OrgStatus:      snapshot.OrgStatus,
		ProjectStatus:  snapshot.ProjectStatus,
		OwnerStatus:    snapshot.OwnerStatus,
		OwnerRole:      snapshot.OwnerRole,
		MembershipRole: snapshot.MembershipRole,
		AllowedIPs:     strings.TrimSpace(snapshot.APIKey.IPWhitelist),
		AllowedModels:  strings.Join(normalizedSet(strings.Split(snapshot.APIKey.AllowedModels, ",")), ","),
		RPMLimit:       effectiveRPM(snapshot.APIKey.RPMLimit),
		RPSLimit:       effectiveRPS(snapshot.APIKey.QPSLimit),
		BudgetLimit:    -1,
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
