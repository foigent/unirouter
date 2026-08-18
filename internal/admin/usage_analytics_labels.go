package admin

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

func usageAnalyticsFallbackLabel(groupKey, key string) string {
	if key == "0" && (groupKey == "project" || groupKey == "key" || groupKey == "owner") {
		return "未归属"
	}
	switch groupKey {
	case "project":
		return "项目 #" + key
	case "key":
		return "访问凭证 #" + key
	case "owner":
		return "用户 #" + key
	default:
		return key
	}
}

func usageAnalyticsEntityLabel(groupKey string, id int64, name, detail string) string {
	if id <= 0 {
		return usageAnalyticsFallbackLabel(groupKey, "0")
	}
	name = strings.TrimSpace(name)
	detail = strings.TrimSpace(detail)
	switch groupKey {
	case "project":
		if detail == "default" || name == "Default Project" {
			name = "默认项目"
		}
		if name == "" {
			return fmt.Sprintf("项目 #%d", id)
		}
		return fmt.Sprintf("%s（项目 #%d）", name, id)
	case "key":
		if name == "" {
			name = fmt.Sprintf("访问凭证 #%d", id)
			if detail != "" {
				name += " · " + detail + "***"
			}
			return name
		}
		if detail != "" {
			name += " · " + detail + "***"
		}
		return fmt.Sprintf("%s（凭证 #%d）", name, id)
	case "owner":
		if name == "" {
			return fmt.Sprintf("用户 #%d", id)
		}
		return fmt.Sprintf("%s（用户 #%d）", name, id)
	default:
		return name
	}
}

// hydrateUsageAnalyticsLabels resolves the at-most 50 grouped identifiers in
// one tenant-scoped query. The metric aggregation remains authoritative; this
// method only adds a human-readable projection without N+1 lookups.
func (h *Handler) hydrateUsageAnalyticsLabels(ctx context.Context, groupKey string, orgID int64, groups []map[string]interface{}) error {
	if groupKey != "project" && groupKey != "key" && groupKey != "owner" {
		return nil
	}

	ids := make([]int64, 0, len(groups))
	for _, group := range groups {
		key := fmt.Sprint(group["key"])
		group["label"] = usageAnalyticsFallbackLabel(groupKey, key)
		id, err := strconv.ParseInt(key, 10, 64)
		if err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}

	var query string
	switch groupKey {
	case "project":
		query = `SELECT id, COALESCE(name, ''), COALESCE(slug, '') FROM projects WHERE org_id=$1 AND id=ANY($2::bigint[])`
	case "key":
		query = `SELECT id, COALESCE(name, ''), COALESCE(key_prefix, '') FROM api_keys WHERE org_id=$1 AND id=ANY($2::bigint[])`
	case "owner":
		query = `SELECT id, COALESCE(username, ''), '' FROM users WHERE org_id=$1 AND id=ANY($2::bigint[])`
	}
	rows, err := h.db.Query(ctx, query, orgID, ids)
	if err != nil {
		return err
	}
	defer rows.Close()

	labels := make(map[string]string, len(ids))
	for rows.Next() {
		var id int64
		var name, detail string
		if err := rows.Scan(&id, &name, &detail); err != nil {
			return err
		}
		labels[strconv.FormatInt(id, 10)] = usageAnalyticsEntityLabel(groupKey, id, name, detail)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, group := range groups {
		if label, exists := labels[fmt.Sprint(group["key"])]; exists {
			group["label"] = label
		}
	}
	return nil
}
