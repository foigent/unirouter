package modelgov

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/db"
)

const (
	LifecycleStaging    = "staging"
	LifecycleGA         = "ga"
	LifecycleDeprecated = "deprecated"
	LifecycleRetired    = "retired"
)

const (
	AliasStatusActive   = "active"
	AliasStatusDisabled = "disabled"
)

const (
	ModelSetStatusActive   = "active"
	ModelSetStatusDisabled = "disabled"
)

const (
	ModelSetItemModel = "model"
	ModelSetItemAlias = "alias"
)

var (
	ErrModelRequired = errors.New("model is required")
	ErrModelRetired  = errors.New("model is retired")
	ErrModelDenied   = errors.New("model is not allowed for project")
)

// AccessResult describes the model governance decision for a gateway request.
type AccessResult struct {
	RequestedModel string
	ResolvedModel  string
	Alias          string
	Lifecycle      string
	Allowed        bool
	Reason         string
}

func IsValidLifecycle(lifecycle string) bool {
	switch strings.TrimSpace(lifecycle) {
	case LifecycleStaging, LifecycleGA, LifecycleDeprecated, LifecycleRetired:
		return true
	default:
		return false
	}
}

func IsCatalogLifecycle(lifecycle string) bool {
	switch strings.TrimSpace(lifecycle) {
	case LifecycleGA, LifecycleDeprecated:
		return true
	default:
		return false
	}
}

func IsCallableLifecycle(lifecycle string) bool {
	return strings.TrimSpace(lifecycle) != LifecycleRetired
}

func IsValidAliasStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case AliasStatusActive, AliasStatusDisabled:
		return true
	default:
		return false
	}
}

func IsValidModelSetStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case ModelSetStatusActive, ModelSetStatusDisabled:
		return true
	default:
		return false
	}
}

func IsValidModelSetItemType(itemType string) bool {
	switch strings.TrimSpace(itemType) {
	case ModelSetItemModel, ModelSetItemAlias:
		return true
	default:
		return false
	}
}

func NormalizeCSV(value string) string {
	value = strings.ReplaceAll(value, "，", ",")
	parts := strings.Split(value, ",")
	seen := make(map[string]bool)
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || seen[part] {
			continue
		}
		seen[part] = true
		result = append(result, part)
	}
	return strings.Join(result, ",")
}

func NormalizeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == ' ':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// CheckAccess resolves an optional enterprise alias and enforces lifecycle and project model set rules.
// If no model governance rows or no project model sets are configured, it preserves PRD1 behavior.
func CheckAccess(ctx context.Context, q db.DBQuerier, orgID, projectID int64, requestedModel string) (AccessResult, error) {
	requestedModel = strings.TrimSpace(requestedModel)
	result := AccessResult{
		RequestedModel: requestedModel,
		ResolvedModel:  requestedModel,
		Allowed:        true,
		Reason:         "allowed",
	}
	if requestedModel == "" {
		result.Allowed = false
		result.Reason = "model_required"
		return result, ErrModelRequired
	}
	if orgID <= 0 {
		return result, fmt.Errorf("org_id is required")
	}

	targetModel, alias, err := resolveAlias(ctx, q, orgID, requestedModel)
	if err != nil {
		return result, err
	}
	if alias != "" {
		result.Alias = alias
		result.ResolvedModel = targetModel
	}

	lifecycle, err := modelLifecycle(ctx, q, orgID, result.ResolvedModel)
	if err != nil {
		return result, err
	}
	result.Lifecycle = lifecycle
	if lifecycle == LifecycleRetired {
		result.Allowed = false
		result.Reason = "model_retired"
		return result, ErrModelRetired
	}

	if projectID > 0 {
		hasSets, err := projectHasModelSets(ctx, q, projectID)
		if err != nil {
			return result, err
		}
		if hasSets {
			allowed, err := projectAllowsModel(ctx, q, projectID, requestedModel, result.ResolvedModel)
			if err != nil {
				return result, err
			}
			if !allowed {
				result.Allowed = false
				result.Reason = "project_model_not_allowed"
				return result, ErrModelDenied
			}
		}
	}

	return result, nil
}

func resolveAlias(ctx context.Context, q db.DBQuerier, orgID int64, requestedModel string) (targetModel, alias string, err error) {
	err = q.QueryRow(ctx,
		`SELECT target_model
		 FROM model_aliases
		 WHERE org_id = $1 AND alias = $2 AND status = 'active'`,
		orgID, requestedModel,
	).Scan(&targetModel)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("resolve model alias: %w", err)
	}
	return targetModel, requestedModel, nil
}

func modelLifecycle(ctx context.Context, q db.DBQuerier, orgID int64, modelName string) (string, error) {
	var lifecycle string
	err := q.QueryRow(ctx,
		`SELECT lifecycle
		 FROM model_registry
		 WHERE org_id = $1 AND model_name = $2`,
		orgID, modelName,
	).Scan(&lifecycle)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("query model registry: %w", err)
	}
	return lifecycle, nil
}

func projectHasModelSets(ctx context.Context, q db.DBQuerier, projectID int64) (bool, error) {
	var count int
	err := q.QueryRow(ctx,
		`SELECT COUNT(*)
		 FROM project_model_sets
		 WHERE project_id = $1`,
		projectID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("query project model sets: %w", err)
	}
	return count > 0, nil
}

func projectAllowsModel(ctx context.Context, q db.DBQuerier, projectID int64, requestedModel, resolvedModel string) (bool, error) {
	var count int
	err := q.QueryRow(ctx,
		`SELECT COUNT(*)
		 FROM project_model_sets pms
		 JOIN model_sets ms ON ms.id = pms.model_set_id AND ms.status = 'active'
		 JOIN model_set_items msi ON msi.model_set_id = ms.id
		 WHERE pms.project_id = $1
		   AND (
		     (msi.item_type = 'model' AND (msi.item_name = $2 OR msi.item_name = $3))
		     OR (msi.item_type = 'alias' AND msi.item_name = $4)
		   )`,
		projectID, requestedModel, resolvedModel, requestedModel,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("query project model set items: %w", err)
	}
	return count > 0, nil
}
