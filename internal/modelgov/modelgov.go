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

// EnsureActivatedCatalogModel makes the exact public model identity used by a
// stable runtime assignment available in the customer catalog. It records only
// configured identity facts; capabilities remain empty until separately
// verified. A retired model is never resurrected by routing activation.
func EnsureActivatedCatalogModel(ctx context.Context, tx pgx.Tx, orgID int64, modelName, providerFamily string) error {
	modelName = strings.TrimSpace(modelName)
	providerFamily = strings.TrimSpace(providerFamily)
	if orgID <= 0 || modelName == "" {
		return fmt.Errorf("activated catalog model requires org_id and model_name")
	}

	tag, err := tx.Exec(ctx, `
		INSERT INTO model_registry (
			org_id, model_name, display_name, provider_family, lifecycle,
			context_length, output_limit, capability_tags, data_residency,
			pricing_model_name, approved_catalog, migration_recommendation)
		VALUES ($1,$2,$2,$3,'ga',0,0,'','',$2,TRUE,'')
		ON CONFLICT (org_id, model_name) DO UPDATE
		SET display_name = CASE
				WHEN model_registry.display_name = '' THEN EXCLUDED.display_name
				ELSE model_registry.display_name
			END,
			provider_family = CASE
				WHEN model_registry.provider_family = '' THEN EXCLUDED.provider_family
				ELSE model_registry.provider_family
			END,
			lifecycle = CASE
				WHEN model_registry.lifecycle = 'staging' THEN 'ga'
				ELSE model_registry.lifecycle
			END,
			pricing_model_name = CASE
				WHEN model_registry.pricing_model_name = '' THEN EXCLUDED.pricing_model_name
				ELSE model_registry.pricing_model_name
			END,
			approved_catalog = TRUE,
			updated_at = NOW()
		WHERE model_registry.lifecycle <> 'retired'`, orgID, modelName, providerFamily)
	if err != nil {
		return fmt.Errorf("ensure activated model catalog entry: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrModelRetired
	}
	return nil
}

// ReconcileActivatedCatalogModels upgrades older deployments whose stable
// runtime assignments predate automatic catalog registration. It is
// idempotent and preserves an explicit retired lifecycle.
func ReconcileActivatedCatalogModels(ctx context.Context, tx pgx.Tx, orgID int64) error {
	if orgID <= 0 {
		return fmt.Errorf("activated catalog reconciliation requires org_id")
	}

	rows, err := tx.Query(ctx, `
		SELECT s.model_name,
		       COALESCE((
		         SELECT CASE
		                  WHEN COUNT(DISTINCT c.provider) = 0 THEN ''
		                  WHEN COUNT(DISTINCT c.provider) = 1 THEN MIN(c.provider)
		                  ELSE 'mixed'
		                END
		           FROM jsonb_array_elements(r.candidate->'overrides') AS override
		           JOIN channels c ON c.id = (override->>'channel_id')::bigint
		          WHERE override->>'admission' = 'allow'
		       ), '') AS provider_family
		  FROM runtime_change_slots s
		  JOIN runtime_change_releases r ON r.id = s.stable_release_id
		 WHERE r.stage = 'active'
		 ORDER BY s.model_name`)
	if err != nil {
		return fmt.Errorf("list activated models for catalog reconciliation: %w", err)
	}
	type activatedModel struct {
		name           string
		providerFamily string
	}
	models := make([]activatedModel, 0)
	for rows.Next() {
		var item activatedModel
		if err := rows.Scan(&item.name, &item.providerFamily); err != nil {
			rows.Close()
			return fmt.Errorf("scan activated model for catalog reconciliation: %w", err)
		}
		models = append(models, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read activated models for catalog reconciliation: %w", err)
	}
	rows.Close()

	for _, item := range models {
		if err := EnsureActivatedCatalogModel(ctx, tx, orgID, item.name, item.providerFamily); err != nil {
			if errors.Is(err, ErrModelRetired) {
				continue
			}
			return err
		}
	}
	return nil
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
		hasSets, err := projectHasModelSets(ctx, q, orgID, projectID)
		if err != nil {
			return result, err
		}
		if hasSets {
			allowed, err := projectAllowsModel(ctx, q, orgID, projectID, requestedModel, result.ResolvedModel)
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

// FilterCallableModels applies the same alias, lifecycle, and project model-set
// rules as CheckAccess to a model list in one database query. It deliberately
// does not apply approved_catalog: that flag controls catalog presentation,
// while gateway call admission is governed by lifecycle and project model sets.
func FilterCallableModels(ctx context.Context, q db.DBQuerier, orgID, projectID int64, requestedModels []string) ([]string, error) {
	if orgID <= 0 {
		return nil, fmt.Errorf("org_id is required")
	}

	normalized := make([]string, 0, len(requestedModels))
	seen := make(map[string]struct{}, len(requestedModels))
	for _, model := range requestedModels {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, duplicate := seen[model]; duplicate {
			continue
		}
		seen[model] = struct{}{}
		normalized = append(normalized, model)
	}
	if len(normalized) == 0 {
		return []string{}, nil
	}

	rows, err := q.Query(ctx,
		`WITH candidates AS (
		   SELECT requested_model, ordinality
		   FROM unnest($3::text[]) WITH ORDINALITY AS candidate(requested_model, ordinality)
		 ),
		 resolved AS (
		   SELECT c.requested_model,
		          c.ordinality,
		          COALESCE(ma.target_model, c.requested_model) AS resolved_model
		   FROM candidates c
		   LEFT JOIN model_aliases ma
		     ON ma.org_id = $1::bigint
		    AND ma.alias = c.requested_model
		    AND ma.status = 'active'
		 ),
		 project_scope AS (
		   SELECT EXISTS (
		     SELECT 1
		     FROM project_model_sets pms
		     JOIN projects p ON p.id = pms.project_id
		     JOIN model_sets ms ON ms.id = pms.model_set_id AND ms.org_id = p.org_id
		     WHERE $2::bigint > 0
		       AND pms.project_id = $2::bigint
		       AND p.org_id = $1::bigint
		   ) AS has_sets
		 )
		 SELECT r.requested_model
		 FROM resolved r
		 CROSS JOIN project_scope ps
		 LEFT JOIN model_registry mr
		   ON mr.org_id = $1::bigint
		  AND mr.model_name = r.resolved_model
		 WHERE COALESCE(mr.lifecycle, '') <> 'retired'
		   AND (
		     $2::bigint <= 0
		     OR NOT ps.has_sets
		     OR EXISTS (
		       SELECT 1
		       FROM project_model_sets pms
		       JOIN projects p ON p.id = pms.project_id
		       JOIN model_sets ms
		         ON ms.id = pms.model_set_id
		        AND ms.status = 'active'
		        AND ms.org_id = p.org_id
		       JOIN model_set_items msi ON msi.model_set_id = ms.id
		       WHERE pms.project_id = $2::bigint
		         AND p.org_id = $1::bigint
		         AND (
		           (msi.item_type = 'model' AND (msi.item_name = r.requested_model OR msi.item_name = r.resolved_model))
		           OR (msi.item_type = 'alias' AND msi.item_name = r.requested_model)
		         )
		     )
		   )
		 ORDER BY r.ordinality`,
		orgID, projectID, normalized,
	)
	if err != nil {
		return nil, fmt.Errorf("filter callable models: %w", err)
	}
	defer rows.Close()

	allowed := make([]string, 0, len(normalized))
	for rows.Next() {
		var model string
		if err := rows.Scan(&model); err != nil {
			return nil, fmt.Errorf("scan callable model: %w", err)
		}
		allowed = append(allowed, model)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate callable models: %w", err)
	}
	return allowed, nil
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

func projectHasModelSets(ctx context.Context, q db.DBQuerier, orgID, projectID int64) (bool, error) {
	var count int
	err := q.QueryRow(ctx,
		`SELECT COUNT(*)
		 FROM project_model_sets pms
		 JOIN projects p ON p.id = pms.project_id
		 JOIN model_sets ms ON ms.id = pms.model_set_id AND ms.org_id = p.org_id
		 WHERE pms.project_id = $1 AND p.org_id = $2`,
		projectID, orgID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("query project model sets: %w", err)
	}
	return count > 0, nil
}

func projectAllowsModel(ctx context.Context, q db.DBQuerier, orgID, projectID int64, requestedModel, resolvedModel string) (bool, error) {
	var count int
	err := q.QueryRow(ctx,
		`SELECT COUNT(*)
		 FROM project_model_sets pms
		 JOIN projects p ON p.id = pms.project_id
		 JOIN model_sets ms ON ms.id = pms.model_set_id AND ms.status = 'active' AND ms.org_id = p.org_id
		 JOIN model_set_items msi ON msi.model_set_id = ms.id
		 WHERE pms.project_id = $1 AND p.org_id = $2
		   AND (
		     (msi.item_type = 'model' AND (msi.item_name = $3 OR msi.item_name = $4))
		     OR (msi.item_type = 'alias' AND msi.item_name = $5)
		   )`,
		projectID, orgID, requestedModel, resolvedModel, requestedModel,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("query project model set items: %w", err)
	}
	return count > 0, nil
}
