//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/contentcapture"
)

func contentPolicyIntegrationStore(t *testing.T, f sourceControlFixture, mode string) {
	t.Helper()
	s, err := contentcapture.NewPolicyStore(f.pool, contentcapture.PolicyStoreOptions{SourceInstance: "synthetic-source-control", DefaultMode: mode, FullContentEnabled: true, SupportedProtocols: []string{"openai.chat"}, ObservationPoints: []string{"client_request"}, Limits: contentcapture.PolicyLimits{MaxSavedBytes: 256, MaxReadBytes: 512, MaxBufferBytes: 32, MaxManifestBytes: 4096, MaxManifestItems: 10}})
	require.NoError(t, err)
	f.handler.SetContentPolicyController(s, func(context.Context) (contentcapture.PolicyApplication, error) {
		return contentcapture.PolicyApplication{StorageStatus: "not_configured", ApplyNow: false}, nil
	})
}
func contentPolicyIntegrationHTTP(t *testing.T, f sourceControlFixture, method string, input any, status int) contentcapture.PolicyView {
	t.Helper()
	r := f.request(t, "policy", input)
	r.Method = method
	r.SetPathValue("id", stringInt64(f.projectID))
	w := httptest.NewRecorder()
	f.handler.projectPayloadPolicy(w, r, method == "PUT")
	require.Equal(t, status, w.Code, w.Body.String())
	var view contentcapture.PolicyView
	if status == 200 || status == 202 {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &view))
		require.NoError(t, view.Validate())
	}
	return view
}
func contentPolicyIntegrationFull() contentcapture.PolicyConfig {
	retention, quota, maximum := int64(60), int64(1024), int64(64)
	return contentcapture.PolicyConfig{Mode: contentcapture.ModeFullContent, SupportedProtocols: []string{"openai.chat"}, ObservationPoints: []string{"client_request"}, AllowedPurposes: []string{"human_review"}, RetentionSeconds: &retention, QuotaBytes: &quota, MaxSavedBytes: &maximum, OnLimit: "record_capture_gap"}
}

func TestContentPolicyHTTPDefaultPendingCASAndDeploymentStopIntegration(t *testing.T) {
	f := newSourceControlFixture(t)
	contentPolicyIntegrationStore(t, f, contentcapture.ModeMetadataOnly)
	initial := contentPolicyIntegrationHTTP(t, f, "GET", nil, 200)
	var count int
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_content_policies WHERE org_id=$1 AND project_id=$2`, f.orgID, f.projectID).Scan(&count))
	require.Zero(t, count, "GET must not materialize a default")
	put := contentcapture.PolicyPut{SchemaVersion: contentcapture.PolicyPutSchema, ExpectedPolicyVersion: initial.Desired.PolicyVersion, Desired: contentPolicyIntegrationFull()}
	pending := contentPolicyIntegrationHTTP(t, f, "PUT", put, 202)
	require.Equal(t, "2", pending.Desired.PolicyVersion)
	require.Equal(t, "1", pending.Effective.PolicyVersion)
	require.Equal(t, contentcapture.ModeMetadataOnly, pending.Effective.Config.Mode)
	contentPolicyIntegrationHTTP(t, f, "PUT", put, 409)
	disable := contentcapture.PolicyConfig{Mode: contentcapture.ModeDisabled, SupportedProtocols: []string{"openai.chat"}, ObservationPoints: []string{"client_request"}, AllowedPurposes: []string{}}
	put.ExpectedPolicyVersion = pending.Desired.PolicyVersion
	put.Desired = disable
	disabled := contentPolicyIntegrationHTTP(t, f, "PUT", put, 200)
	require.Equal(t, "3", disabled.Effective.PolicyVersion)
	contentPolicyIntegrationStore(t, f, contentcapture.ModeDisabled)
	contentPolicyIntegrationHTTP(t, f, "GET", nil, 200)
	put.ExpectedPolicyVersion = "3"
	put.Desired = contentPolicyIntegrationFull()
	contentPolicyIntegrationHTTP(t, f, "PUT", put, 400)
	var desired, effective int64
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT desired_version,effective_version FROM execution_content_policies WHERE org_id=$1 AND project_id=$2`, f.orgID, f.projectID).Scan(&desired, &effective))
	require.EqualValues(t, 3, desired)
	require.EqualValues(t, 3, effective)
	var auditCount int
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM audit_logs WHERE action='content.policy.update' AND project_id=$1`, f.projectID).Scan(&auditCount))
	require.Equal(t, 2, auditCount)
}

func TestContentPolicyHTTPAuditFailureRollsBackIntegration(t *testing.T) {
	f := newSourceControlFixture(t)
	contentPolicyIntegrationStore(t, f, contentcapture.ModeMetadataOnly)
	name, function, trigger := contentIntegrationFailureSQL(t, "audit_logs")
	trigger = strings.Replace(trigger, "NEW.resource='xinsight_source_content'", "NEW.action='content.policy.update'", 1)
	quoted := pgx.Identifier{name}.Sanitize()
	_, err := f.pool.Exec(t.Context(), function)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := f.pool.Exec(ctx, `DROP TRIGGER IF EXISTS `+quoted+` ON audit_logs`)
		require.NoError(t, err)
		_, err = f.pool.Exec(ctx, `DROP FUNCTION IF EXISTS `+quoted+`()`)
		require.NoError(t, err)
	})
	_, err = f.pool.Exec(t.Context(), trigger)
	require.NoError(t, err)
	put := contentcapture.PolicyPut{SchemaVersion: contentcapture.PolicyPutSchema, ExpectedPolicyVersion: "1", Desired: contentPolicyIntegrationFull()}
	contentPolicyIntegrationHTTP(t, f, "PUT", put, 503)
	var count int
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_content_policies WHERE org_id=$1 AND project_id=$2`, f.orgID, f.projectID).Scan(&count))
	require.Zero(t, count)
	_, err = f.pool.Exec(t.Context(), `DROP TRIGGER `+quoted+` ON audit_logs`)
	require.NoError(t, err)
	contentPolicyIntegrationHTTP(t, f, "PUT", put, 202)
}
