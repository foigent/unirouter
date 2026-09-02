package admin

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelgov"
)

type modelGovernanceOrgScope struct {
	orgID       int64
	constrained bool
}

type modelGovernanceQueryRower interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}

func resolveModelGovernanceCreateOrganization(ctx context.Context, requestedOrgID *int64) (int64, error) {
	if requestedOrgID == nil {
		return resolveActorOrganization(ctx, 0)
	}
	if *requestedOrgID <= 0 {
		return 0, errInvalidOrganizationID
	}
	return resolveActorOrganization(ctx, *requestedOrgID)
}

// resolveModelGovernancePathScope binds production requests to the canonical
// customer context. The platform-only ID fallback is retained solely for
// rolling-upgrade/direct-handler compatibility when CustomerContext is absent;
// it must never let a deployment operator use a globally unique path ID to
// escape the single-customer boundary.
func resolveModelGovernancePathScope(r *http.Request) (modelGovernanceOrgScope, error) {
	requestedOrgID, err := parseOptionalOrganizationID(r.URL.Query().Get("org_id"))
	if err != nil {
		return modelGovernanceOrgScope{}, err
	}
	if customer, present := middleware.GetCustomerContextFromContext(r.Context()); present {
		if !customer.Ready() {
			return modelGovernanceOrgScope{}, errActorOrganizationForbidden
		}
		orgID, resolveErr := resolveActorOrganization(r.Context(), requestedOrgID)
		if resolveErr != nil {
			return modelGovernanceOrgScope{}, resolveErr
		}
		return modelGovernanceOrgScope{orgID: orgID, constrained: true}, nil
	}
	if auth.IsPlatformAdmin(actorOrganizationRole(r.Context())) {
		return modelGovernanceOrgScope{orgID: requestedOrgID, constrained: requestedOrgID > 0}, nil
	}
	orgID, err := resolveActorOrganization(r.Context(), requestedOrgID)
	if err != nil {
		return modelGovernanceOrgScope{}, err
	}
	return modelGovernanceOrgScope{orgID: orgID, constrained: true}, nil
}

func readModelSetTargetOrg(ctx context.Context, q modelGovernanceQueryRower, modelSetID int64, scope modelGovernanceOrgScope, forUpdate bool) (int64, error) {
	query := `SELECT org_id FROM model_sets WHERE id = $1`
	args := []interface{}{modelSetID}
	if scope.constrained {
		query += ` AND org_id = $2`
		args = append(args, scope.orgID)
	}
	if forUpdate {
		query += ` FOR UPDATE`
	}
	var orgID int64
	if err := q.QueryRow(ctx, query, args...).Scan(&orgID); err != nil {
		return 0, err
	}
	return orgID, nil
}

func writeModelGovernanceLookupError(w http.ResponseWriter, err error, resource string) {
	if errors.Is(err, pgx.ErrNoRows) || strings.Contains(strings.ToLower(err.Error()), "no rows") {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": resource + " not found"})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": resource + " query failed"})
}

func modelGovernanceAuditMeta(r *http.Request, orgID int64) auditMeta {
	return auditMeta{
		OrgID:     orgID,
		IP:        normalizeGlobalControlAuditIP(r.RemoteAddr),
		UserAgent: r.UserAgent(),
		RequestID: middleware.GetRequestIDFromContext(r.Context()),
		Source:    auditSourceFromContext(r.Context()),
	}
}

type rowScanner func(dest ...any) error

func scanModelRegistryRow(scan rowScanner) (map[string]interface{}, error) {
	item, _, err := scanModelRegistryRowInternal(scan, false)
	return item, err
}

func scanModelRegistryRowWithAliases(scan rowScanner) (map[string]interface{}, error) {
	item, aliases, err := scanModelRegistryRowInternal(scan, true)
	if err != nil {
		return nil, err
	}
	item["aliases"] = splitCSV(aliases)
	return item, nil
}

func scanModelRegistryRowWithAliasesAndRequestIdentity(scan rowScanner) (map[string]interface{}, string, error) {
	var requestModel string
	item, aliases, err := scanModelRegistryRowInternal(func(dest ...any) error {
		dest = append(dest, &requestModel)
		return scan(dest...)
	}, true)
	if err != nil {
		return nil, "", err
	}
	item["aliases"] = splitCSV(aliases)
	return item, requestModel, nil
}

func scanModelRegistryRowInternal(scan rowScanner, withAliases bool) (map[string]interface{}, string, error) {
	var id, orgID int64
	var modelName, displayName, providerFamily, lifecycle, capabilityTags, dataResidency, pricingModelName, migrationRecommendation string
	var executionProfileID, executionProfileVersion string
	var contextLength, outputLimit int
	var approved bool
	var createdAt, updatedAt time.Time
	var aliases string
	dest := []any{&id, &orgID, &modelName, &displayName, &providerFamily, &lifecycle, &contextLength, &outputLimit, &capabilityTags, &dataResidency, &pricingModelName, &approved, &migrationRecommendation, &executionProfileID, &executionProfileVersion, &createdAt, &updatedAt}
	if withAliases {
		dest = append(dest, &aliases)
	}
	if err := scan(dest...); err != nil {
		return nil, "", err
	}
	item := map[string]interface{}{
		"id": id, "org_id": orgID, "model_name": modelName, "display_name": displayName,
		"provider_family": providerFamily, "lifecycle": lifecycle,
		"context_length": contextLength, "output_limit": outputLimit,
		"capability_tags": splitCSV(capabilityTags), "data_residency": dataResidency,
		"pricing_model_name": pricingModelName, "approved_catalog": approved,
		"migration_recommendation": migrationRecommendation,
		"execution_profile_id":     executionProfileID, "execution_profile_version": executionProfileVersion,
		"created_at": createdAt, "updated_at": updatedAt,
	}
	return item, aliases, nil
}

func scanModelAliasRow(scan rowScanner) (map[string]interface{}, error) {
	var id, orgID int64
	var alias, targetModel, displayName, description, status string
	var isPrimary bool
	var createdAt, updatedAt time.Time
	if err := scan(&id, &orgID, &alias, &targetModel, &displayName, &description, &status, &isPrimary, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"id": id, "org_id": orgID, "alias": alias, "target_model": targetModel,
		"display_name": displayName, "description": description, "status": status, "is_primary": isPrimary,
		"created_at": createdAt, "updated_at": updatedAt,
	}, nil
}

func scanModelSetRow(scan rowScanner) (map[string]interface{}, error) {
	var id, orgID, itemCount int64
	var name, slug, description, status string
	var createdAt, updatedAt time.Time
	if err := scan(&id, &orgID, &name, &slug, &description, &status, &itemCount, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"id": id, "org_id": orgID, "name": name, "slug": slug,
		"description": description, "status": status, "item_count": itemCount,
		"created_at": createdAt, "updated_at": updatedAt,
	}, nil
}

func splitCSV(value string) []string {
	normalized := modelgov.NormalizeCSV(value)
	if normalized == "" {
		return []string{}
	}
	return strings.Split(normalized, ",")
}

func validModelSetItems(items []modelSetItemRequest) bool {
	for _, item := range items {
		if !modelgov.IsValidModelSetItemType(item.ItemType) || strings.TrimSpace(item.ItemName) == "" {
			return false
		}
	}
	return true
}

func normalizeModelSetItems(items []modelSetItemRequest) []modelSetItemRequest {
	normalized := make([]modelSetItemRequest, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item.ItemType = strings.TrimSpace(item.ItemType)
		item.ItemName = strings.TrimSpace(item.ItemName)
		key := item.ItemType + ":" + item.ItemName
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, item)
	}
	return normalized
}

func replaceModelSetItems(ctx context.Context, execer auditExecer, modelSetID, orgID int64, items []modelSetItemRequest) error {
	if _, err := execer.Exec(ctx,
		`DELETE FROM model_set_items msi
		 USING model_sets ms
		 WHERE msi.model_set_id = $1 AND ms.id = msi.model_set_id AND ms.org_id = $2`,
		modelSetID, orgID,
	); err != nil {
		return err
	}
	for _, item := range items {
		if _, err := execer.Exec(ctx,
			`INSERT INTO model_set_items (model_set_id, item_type, item_name, created_at)
			 VALUES ($1, $2, $3, NOW())
			 ON CONFLICT DO NOTHING`,
			modelSetID, item.ItemType, item.ItemName,
		); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) projectHasAssignedModelSets(ctx context.Context, projectID, orgID int64) (bool, error) {
	var count int
	err := h.db.QueryRow(ctx,
		`SELECT COUNT(*)
		 FROM project_model_sets pms
		 JOIN projects p ON p.id = pms.project_id
		 JOIN model_sets ms ON ms.id = pms.model_set_id AND ms.org_id = p.org_id
		 WHERE pms.project_id = $1 AND p.org_id = $2`,
		projectID, orgID,
	).Scan(&count)
	return count > 0, err
}
