package admin

import (
	"context"
	"strconv"

	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/middleware"
)

func (h *Handler) emitAPIKeyDisabledEvent(ctx context.Context, orgID, projectID, keyID int64, name, keyPrefix string) {
	if h.eventService == nil || orgID <= 0 || keyID <= 0 {
		return
	}
	_, _ = h.eventService.Emit(ctx, billing.EventInput{
		OrgID:      orgID,
		ProjectID:  projectID,
		EventType:  billing.EventAPIKeyDisabled,
		Severity:   billing.NotificationSeverityWarning,
		Title:      "API key disabled",
		Message:    "API key " + name + " was disabled",
		Resource:   "api_key",
		ResourceID: strconv.FormatInt(keyID, 10),
		DedupeKey:  "api_key.disabled:" + strconv.FormatInt(keyID, 10),
		Payload: map[string]interface{}{
			"org_id":     orgID,
			"project_id": nullableInt64(projectID),
			"key_id":     keyID,
			"name":       name,
			"key_prefix": keyPrefix,
		},
	})
}

func (h *Handler) emitModelLifecycleEvent(ctx context.Context, orgID, modelID int64, modelName, lifecycle, recommendation string) {
	if h.eventService == nil || orgID <= 0 || modelID <= 0 {
		return
	}
	eventType := ""
	severity := billing.NotificationSeverityInfo
	title := ""
	switch lifecycle {
	case "deprecated":
		eventType = billing.EventModelDeprecated
		severity = billing.NotificationSeverityWarning
		title = "Model deprecated"
	case "retired":
		eventType = billing.EventModelRetired
		severity = billing.NotificationSeverityCritical
		title = "Model retired"
	default:
		return
	}
	_, _ = h.eventService.Emit(ctx, billing.EventInput{
		OrgID:      orgID,
		EventType:  eventType,
		Severity:   severity,
		Title:      title,
		Message:    "Model " + modelName + " lifecycle changed to " + lifecycle,
		Resource:   "model_registry",
		ResourceID: strconv.FormatInt(modelID, 10),
		DedupeKey:  "model.lifecycle:" + strconv.FormatInt(modelID, 10) + ":" + lifecycle,
		Payload: map[string]interface{}{
			"org_id":                   orgID,
			"model_id":                 modelID,
			"model_name":               modelName,
			"lifecycle":                lifecycle,
			"migration_recommendation": recommendation,
		},
	})
}

func (h *Handler) emitLicenseWarningEvent(ctx context.Context, status licensing.Status) {
	if h.eventService == nil || status.License == nil || !status.Warning {
		return
	}
	_, _ = h.eventService.Emit(ctx, billing.EventInput{
		OrgID:      middleware.GetOrgIDFromContext(ctx),
		EventType:  billing.EventLicenseWarning,
		Severity:   billing.NotificationSeverityWarning,
		Title:      "License expiry warning",
		Message:    "License for " + status.License.Customer + " is expiring",
		Resource:   "license",
		ResourceID: licenseResourceID(status),
		DedupeKey:  "license.warning:" + licenseResourceID(status) + ":" + status.License.ExpiresAt,
		Payload: map[string]interface{}{
			"status":            status.Status,
			"customer":          status.License.Customer,
			"deployment_id":     status.License.DeploymentID,
			"expires_at":        status.License.ExpiresAt,
			"days_until_expiry": status.DaysUntilExpiry,
		},
	})
}
