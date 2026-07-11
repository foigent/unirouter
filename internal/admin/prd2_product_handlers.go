package admin

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/errdocs"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/middleware"
)

type analyticsGroup struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

func (h *Handler) ListErrorDocs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": errdocs.All()})
}

func usageAnalyticsGroup(groupBy string) (analyticsGroup, string, bool) {
	switch strings.TrimSpace(groupBy) {
	case "overall":
		return analyticsGroup{Key: "overall", Label: "总体"}, "'全部请求'", true
	case "", "project":
		return analyticsGroup{Key: "project", Label: "项目"}, "COALESCE(project_id, 0)::text", true
	case "key":
		return analyticsGroup{Key: "key", Label: "访问凭证"}, "COALESCE(api_key_id, 0)::text", true
	case "owner":
		return analyticsGroup{Key: "owner", Label: "Key Owner"}, "COALESCE(owner_user_id, 0)::text", true
	case "key_type":
		return analyticsGroup{Key: "key_type", Label: "凭证类型"}, "COALESCE(NULLIF(key_type, ''), 'unknown')", true
	case "model":
		return analyticsGroup{Key: "model", Label: "模型"}, "COALESCE(NULLIF(model, ''), '(unknown)')", true
	case "alias":
		return analyticsGroup{Key: "alias", Label: "模型别名"}, "COALESCE(NULLIF(requested_model, ''), NULLIF(model, ''), '(unknown)')", true
	case "provider":
		return analyticsGroup{Key: "provider", Label: "供应商"}, "COALESCE(NULLIF(provider, ''), '(unknown)')", true
	case "channel":
		return analyticsGroup{Key: "channel", Label: "上游通道"}, "COALESCE(NULLIF(channel, ''), '(unknown)')", true
	case "status":
		return analyticsGroup{Key: "status", Label: "请求状态"}, "CASE WHEN status_code >= 400 OR status_code = 0 THEN 'error' ELSE 'success' END", true
	case "usage_source":
		return analyticsGroup{Key: "usage_source", Label: "计量来源"}, "COALESCE(NULLIF(usage_source, ''), 'upstream')", true
	default:
		return analyticsGroup{}, "", false
	}
}

func internalStatusLevel(openCritical, openWarning int64) string {
	switch {
	case openCritical > 0:
		return "degraded"
	case openWarning > 0:
		return "warning"
	default:
		return "operational"
	}
}

func usageAnalyticsUpstreamSourceSQL() string {
	return "COALESCE(usage_source, '') IN ('upstream', 'api', '')"
}

func (h *Handler) UsageAnalytics(w http.ResponseWriter, r *http.Request) {
	group, groupExpr, ok := usageAnalyticsGroup(r.URL.Query().Get("group_by"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid group_by"})
		return
	}
	conditions, args, err := h.analyticsConditions(r)
	if err != nil {
		status := http.StatusBadRequest
		if err.Error() == "Forbidden" || strings.Contains(err.Error(), "project_id is required") {
			status = http.StatusForbidden
		}
		writeJSON(w, status, map[string]interface{}{"error": err.Error()})
		return
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	summary, err := h.usageAnalyticsSummary(r, where, args)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Summary failed"})
		return
	}
	groups, err := h.usageAnalyticsGroups(r, groupExpr, where, args)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	if r.URL.Query().Get("format") == "csv" {
		h.writeUsageAnalyticsCSV(w, group, groups)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"group_by": group,
		"summary":  summary,
		"data":     groups,
	})
}

func (h *Handler) analyticsConditions(r *http.Request) ([]string, []interface{}, error) {
	var conditions []string
	var args []interface{}
	add := func(condition string, value interface{}) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
	}

	role := actorRole(r.Context())
	requestedOrgID, err := parseOptionalOrganizationID(r.URL.Query().Get("org_id"))
	if err != nil {
		return nil, nil, fmt.Errorf("Invalid org_id")
	}
	orgID, err := resolveActorOrganization(r.Context(), requestedOrgID)
	if err != nil {
		return nil, nil, fmt.Errorf("Forbidden")
	}
	add("org_id = $%d", orgID)

	currentUserID := middleware.GetUserIDFromContext(r.Context())
	myScope := false
	if value := strings.TrimSpace(r.URL.Query().Get("scope")); value != "" {
		switch value {
		case "my":
			if currentUserID <= 0 {
				return nil, nil, fmt.Errorf("Forbidden")
			}
			add("owner_user_id = $%d", currentUserID)
			myScope = true
		default:
			return nil, nil, fmt.Errorf("Invalid scope")
		}
	}

	projectValue := r.URL.Query().Get("project_id")
	if projectValue != "" {
		projectID, err := strconv.ParseInt(projectValue, 10, 64)
		if err != nil || projectID <= 0 {
			return nil, nil, fmt.Errorf("Invalid project_id")
		}
		if !h.canReadProjectUsage(r.Context(), projectID) {
			return nil, nil, fmt.Errorf("Forbidden")
		}
		add("project_id = $%d", projectID)
	} else if auth.IsProjectScopedRole(role) && !myScope {
		return nil, nil, fmt.Errorf("project_id is required")
	}

	if value := strings.TrimSpace(r.URL.Query().Get("model")); value != "" {
		add("model = $%d", value)
	}
	if value := strings.TrimSpace(r.URL.Query().Get("alias")); value != "" {
		add("requested_model = $%d", value)
	}
	if value := strings.TrimSpace(r.URL.Query().Get("provider")); value != "" {
		add("provider = $%d", value)
	}
	if value := strings.TrimSpace(r.URL.Query().Get("channel")); value != "" {
		add("channel = $%d", value)
	}
	if value := strings.TrimSpace(r.URL.Query().Get("key_id")); value != "" {
		keyID, err := strconv.ParseInt(value, 10, 64)
		if err != nil || keyID <= 0 {
			return nil, nil, fmt.Errorf("Invalid key_id")
		}
		add("api_key_id = $%d", keyID)
	}
	if value := strings.TrimSpace(r.URL.Query().Get("owner_user_id")); value != "" {
		ownerUserID, err := strconv.ParseInt(value, 10, 64)
		if err != nil || ownerUserID <= 0 {
			return nil, nil, fmt.Errorf("Invalid owner_user_id")
		}
		if myScope && ownerUserID != currentUserID {
			return nil, nil, fmt.Errorf("owner_user_id conflicts with scope")
		}
		if auth.IsProjectScopedRole(role) && ownerUserID != currentUserID {
			return nil, nil, fmt.Errorf("Forbidden")
		}
		if !myScope {
			add("owner_user_id = $%d", ownerUserID)
		}
	}
	if value := strings.TrimSpace(r.URL.Query().Get("key_type")); value != "" {
		if value != "unknown" && !validAPIKeyType(value) {
			return nil, nil, fmt.Errorf("Invalid key_type")
		}
		add("key_type = $%d", value)
	}
	if value := strings.TrimSpace(r.URL.Query().Get("usage_source")); value != "" {
		add("usage_source = $%d", metering.NormalizeUsageSource(value))
	}
	if value := strings.TrimSpace(r.URL.Query().Get("status")); value != "" {
		switch value {
		case "success":
			conditions = append(conditions, "status_code > 0 AND status_code < 400")
		case "error":
			conditions = append(conditions, "(status_code >= 400 OR status_code = 0)")
		default:
			statusCode, err := strconv.Atoi(value)
			if err != nil {
				return nil, nil, fmt.Errorf("Invalid status")
			}
			add("status_code = $%d", statusCode)
		}
	}
	if value := strings.TrimSpace(r.URL.Query().Get("start_time")); value != "" {
		if _, err := time.Parse(time.RFC3339, value); err != nil {
			return nil, nil, fmt.Errorf("Invalid start_time")
		}
		add("created_at >= $%d", value)
	}
	if value := strings.TrimSpace(r.URL.Query().Get("end_time")); value != "" {
		if _, err := time.Parse(time.RFC3339, value); err != nil {
			return nil, nil, fmt.Errorf("Invalid end_time")
		}
		add("created_at <= $%d", value)
	}
	return conditions, args, nil
}

func (h *Handler) usageAnalyticsSummary(r *http.Request, where string, args []interface{}) (map[string]interface{}, error) {
	query := fmt.Sprintf(
		`SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN status_code > 0 AND status_code < 400 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status_code >= 400 OR status_code = 0 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(total_tokens), 0),
			COALESCE(SUM(cost_cents), 0),
			COALESCE(AVG(NULLIF(latency_ms, 0)), 0)::float8,
			COALESCE(SUM(CASE WHEN usage_source IN ('estimated', 'adapter_estimated', 'counter_v2') THEN total_tokens ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN %s THEN total_tokens ELSE 0 END), 0)
		 FROM call_logs%s`,
		usageAnalyticsUpstreamSourceSQL(),
		where,
	)
	var totalRequests, successRequests, failedRequests, totalTokens, costCents, estimatedTokens, upstreamTokens int64
	var avgLatency float64
	if err := h.db.QueryRow(r.Context(), query, args...).Scan(&totalRequests, &successRequests, &failedRequests, &totalTokens, &costCents, &avgLatency, &estimatedTokens, &upstreamTokens); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"total_requests":     totalRequests,
		"success_requests":   successRequests,
		"failed_requests":    failedRequests,
		"total_tokens":       totalTokens,
		"cost_cents":         costCents,
		"avg_latency_ms":     avgLatency,
		"estimated_tokens":   estimatedTokens,
		"upstream_tokens":    upstreamTokens,
		"estimated_ratio":    metering.EstimatedTokenRatio(int(estimatedTokens), int(upstreamTokens)),
		"csv_export_scope":   "filtered_query",
		"prompt_retention":   "not_stored_by_default",
		"response_retention": "not_stored_by_default",
	}, nil
}

func (h *Handler) usageAnalyticsGroups(r *http.Request, groupExpr, where string, args []interface{}) ([]map[string]interface{}, error) {
	query := fmt.Sprintf(
		`SELECT
			%s AS group_key,
			COUNT(*),
			COALESCE(SUM(CASE WHEN status_code > 0 AND status_code < 400 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status_code >= 400 OR status_code = 0 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(total_tokens), 0),
			COALESCE(SUM(cost_cents), 0),
			COALESCE(AVG(NULLIF(latency_ms, 0)), 0)::float8,
			COALESCE(SUM(CASE WHEN usage_source IN ('estimated', 'adapter_estimated', 'counter_v2') THEN total_tokens ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN %s THEN total_tokens ELSE 0 END), 0)
		 FROM call_logs%s
		 GROUP BY 1
		 ORDER BY COUNT(*) DESC
		 LIMIT 50`,
		groupExpr, usageAnalyticsUpstreamSourceSQL(), where,
	)
	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := make([]map[string]interface{}, 0)
	for rows.Next() {
		var key string
		var totalRequests, successRequests, failedRequests, totalTokens, costCents, estimatedTokens, upstreamTokens int64
		var avgLatency float64
		if err := rows.Scan(&key, &totalRequests, &successRequests, &failedRequests, &totalTokens, &costCents, &avgLatency, &estimatedTokens, &upstreamTokens); err != nil {
			return nil, err
		}
		groups = append(groups, map[string]interface{}{
			"key":              key,
			"total_requests":   totalRequests,
			"success_requests": successRequests,
			"failed_requests":  failedRequests,
			"total_tokens":     totalTokens,
			"cost_cents":       costCents,
			"avg_latency_ms":   avgLatency,
			"estimated_tokens": estimatedTokens,
			"upstream_tokens":  upstreamTokens,
			"estimated_ratio":  metering.EstimatedTokenRatio(int(estimatedTokens), int(upstreamTokens)),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return groups, nil
}

func (h *Handler) writeUsageAnalyticsCSV(w http.ResponseWriter, group analyticsGroup, groups []map[string]interface{}) {
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=uniroute-usage-analytics.csv")
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{neutralizeSpreadsheetFormula(group.Label), "total_requests", "success_requests", "failed_requests", "total_tokens", "cost_cents", "avg_latency_ms", "estimated_ratio"})
	for _, row := range groups {
		_ = writer.Write([]string{
			neutralizeSpreadsheetFormula(fmt.Sprint(row["key"])),
			fmt.Sprint(row["total_requests"]),
			fmt.Sprint(row["success_requests"]),
			fmt.Sprint(row["failed_requests"]),
			fmt.Sprint(row["total_tokens"]),
			fmt.Sprint(row["cost_cents"]),
			fmt.Sprintf("%.2f", row["avg_latency_ms"]),
			fmt.Sprintf("%.4f", row["estimated_ratio"]),
		})
	}
	writer.Flush()
}

func (h *Handler) InternalStatus(w http.ResponseWriter, r *http.Request) {
	conditions, args, err := h.internalStatusConditions(r)
	if err != nil {
		status := http.StatusBadRequest
		if err.Error() == "Forbidden" || strings.Contains(err.Error(), "project_id is required") {
			status = http.StatusForbidden
		}
		writeJSON(w, status, map[string]interface{}{"error": err.Error()})
		return
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	var openCritical, openWarning int64
	summaryQuery := fmt.Sprintf(
		`SELECT
			COALESCE(SUM(CASE WHEN status = 'open' AND severity = 'critical' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'open' AND severity = 'warning' THEN 1 ELSE 0 END), 0)
		 FROM notification_events%s`,
		where,
	)
	if err := h.db.QueryRow(r.Context(), summaryQuery, args...).Scan(&openCritical, &openWarning); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Summary failed"})
		return
	}

	query := fmt.Sprintf(
		`SELECT id, event_type, severity, title, message, resource, resource_id, status, delivery_state, created_at, updated_at
		 FROM notification_events%s
		 ORDER BY CASE WHEN status = 'open' THEN 0 ELSE 1 END, created_at DESC
		 LIMIT 20`,
		where,
	)
	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	incidents := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id int64
		var eventType, severity, title, message, resource, resourceID, status, deliveryState string
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &eventType, &severity, &title, &message, &resource, &resourceID, &status, &deliveryState, &createdAt, &updatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		incidents = append(incidents, map[string]interface{}{
			"id":             id,
			"event_type":     eventType,
			"severity":       severity,
			"title":          title,
			"message":        message,
			"resource":       resource,
			"resource_id":    resourceID,
			"status":         status,
			"delivery_state": deliveryState,
			"created_at":     createdAt,
			"updated_at":     updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":              internalStatusLevel(openCritical, openWarning),
		"open_critical":       openCritical,
		"open_warning":        openWarning,
		"incidents":           incidents,
		"maintenance_windows": []interface{}{},
	})
}

func (h *Handler) internalStatusConditions(r *http.Request) ([]string, []interface{}, error) {
	var conditions []string
	var args []interface{}
	add := func(condition string, value interface{}) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
	}

	role := actorRole(r.Context())
	requestedOrgID, err := parseOptionalOrganizationID(r.URL.Query().Get("org_id"))
	if err != nil {
		return nil, nil, fmt.Errorf("Invalid org_id")
	}
	orgID, err := resolveActorOrganization(r.Context(), requestedOrgID)
	if err != nil {
		return nil, nil, fmt.Errorf("Forbidden")
	}
	add("org_id = $%d", orgID)
	if value := r.URL.Query().Get("project_id"); value != "" {
		projectID, err := strconv.ParseInt(value, 10, 64)
		if err != nil || projectID <= 0 {
			return nil, nil, fmt.Errorf("Invalid project_id")
		}
		if !h.canReadProjectUsage(r.Context(), projectID) {
			return nil, nil, fmt.Errorf("Forbidden")
		}
		add("(project_id = $%d OR project_id IS NULL)", projectID)
	} else if auth.IsProjectScopedRole(role) {
		return nil, nil, fmt.Errorf("project_id is required")
	}
	return conditions, args, nil
}
