package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
)

const sensitiveWordsSettingKey = "sensitive_words"

var dedicatedGlobalSettingEndpoints = map[string]string{
	licenseSettingKey:        "/api/v1/license",
	sensitiveWordsSettingKey: "/api/v1/sensitive-words",
}

var redactedGlobalSettingKeys = map[string]struct{}{
	"webhook.feishu":           {},
	"webhook.dingtalk":         {},
	"budget_alert_webhook_url": {},
}

type retiredGlobalSetting struct {
	Replacement string
	Reason      string
}

var retiredGlobalSettings = map[string]retiredGlobalSetting{
	"budget_alert_webhook_url": {
		Replacement: "/api/v1/notification-webhooks",
		Reason:      "project_scoped_notification_webhooks_replace_ambiguous_global_alerts",
	},
	"budget_alert_threshold_cents": {
		Replacement: "/api/v1/projects/{id}",
		Reason:      "projects.budget_cents_and_budget_period_are_authoritative",
	},
	"budget_cents": {
		Replacement: "/api/v1/projects/{id}",
		Reason:      "projects.budget_cents_and_budget_period_are_authoritative",
	},
}

func retiredGlobalSettingContract(key string) (retiredGlobalSetting, bool) {
	setting, ok := retiredGlobalSettings[key]
	return setting, ok
}

type globalSettingQueryRower interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}

type globalSettingAuditSnapshot struct {
	Present     bool   `json:"present"`
	ValueSHA256 string `json:"value_sha256,omitempty"`
	ValueBytes  int    `json:"value_bytes,omitempty"`
}

func requirePlatformGlobalControl(w http.ResponseWriter, r *http.Request) bool {
	if auth.IsPlatformAdmin(actorRole(r.Context())) {
		return true
	}
	writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
	return false
}

func dedicatedGlobalSettingEndpoint(key string) (string, bool) {
	endpoint, ok := dedicatedGlobalSettingEndpoints[key]
	if !ok && strings.HasPrefix(key, "license.") {
		return "/api/v1/license", true
	}
	return endpoint, ok
}

func redactGlobalSettingValue(key string) bool {
	if _, dedicated := dedicatedGlobalSettingEndpoint(key); dedicated {
		return true
	}
	_, redacted := redactedGlobalSettingKeys[key]
	return redacted
}

func readGlobalSetting(ctx context.Context, q globalSettingQueryRower, key string, forUpdate bool) ([]byte, bool, error) {
	query := `SELECT value::text FROM settings WHERE key = $1`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	var raw string
	err := q.QueryRow(ctx, query, key).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) || (err != nil && strings.Contains(err.Error(), "no rows")) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return []byte(raw), true, nil
}

func lockGlobalControlKey(ctx context.Context, tx pgx.Tx, key string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "global_control:"+key)
	return err
}

func globalSettingSnapshot(raw []byte, present bool) globalSettingAuditSnapshot {
	if !present {
		return globalSettingAuditSnapshot{Present: false}
	}
	digest := sha256.Sum256(raw)
	return globalSettingAuditSnapshot{
		Present:     true,
		ValueSHA256: hex.EncodeToString(digest[:]),
		ValueBytes:  len(raw),
	}
}

func globalControlAuditResourceID(raw string) (string, bool) {
	if len(raw) <= 64 {
		return raw, false
	}
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:]), true
}

func globalControlAuditMeta(r *http.Request) auditMeta {
	return auditMeta{
		OrgID: middleware.GetOrgIDFromContext(r.Context()),
		IP:    normalizeGlobalControlAuditIP(r.RemoteAddr), UserAgent: r.UserAgent(),
		RequestID: middleware.GetRequestIDFromContext(r.Context()),
		Source:    auditSourceFromContext(r.Context()),
	}
}

func normalizeGlobalControlAuditIP(remoteAddr string) string {
	host := strings.TrimSpace(remoteAddr)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	if zone := strings.LastIndex(host, "%"); zone >= 0 {
		host = host[:zone]
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	if len(host) <= 45 {
		return host
	}
	return ""
}
