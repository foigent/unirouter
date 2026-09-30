//go:build integration

package gateway_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/sourcefacts"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestContentPolicyResponseCacheReadAndCommitIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	var orgID, projectID, keyID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES('cache-policy','active','standard') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects(org_id,name,slug,status,budget_period) VALUES($1,'Cache Policy','cache-policy','active','none') RETURNING id`, orgID).Scan(&projectID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO api_keys(org_id,project_id,name,key_hash,key_prefix,status)
		VALUES($1,$2,'cache-policy',repeat('d',64),'ur-cache-policy','active') RETURNING id`, orgID, projectID).Scan(&keyID))

	limit, quota, retention := int64(1024), int64(8192), int64(3600)
	allPoints := []string{"client_request", "provider_request", "provider_response", "client_response"}
	config := contentcapture.PolicyConfig{Mode: contentcapture.ModeFullContent, SupportedProtocols: []string{string(modeloperation.IDChatCompletions), string(modeloperation.IDEmbeddings)}, ObservationPoints: allPoints,
		RetentionSeconds: &retention, QuotaBytes: &quota, MaxSavedBytes: &limit, AllowedPurposes: []string{"human_review"}, OnLimit: "record_capture_gap"}
	updatePolicy := func(points []string) {
		t.Helper()
		config.ObservationPoints = points
		wire, err := json.Marshal(config)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `UPDATE execution_content_policies SET desired_version=desired_version+1,
			effective_version=effective_version+1,desired_config=$3,effective_config=$3
			WHERE org_id=$1 AND project_id=$2`, orgID, projectID, wire)
		require.NoError(t, err)
	}
	wire, err := json.Marshal(config)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO execution_content_policies(org_id,project_id,desired_version,effective_version,
		desired_config,effective_config,effective_at,application_status,application_reason)
		VALUES($1,$2,1,1,$3,$3,clock_timestamp(),'applied','none')`, orgID, projectID, wire)
	require.NoError(t, err)
	source, err := sourcefacts.New(pool, sourcefacts.Options{Enabled: true, ContentEnabled: true, SourceInstance: "synthetic-cache-policy", CursorSecret: strings.Repeat("s", 32)})
	require.NoError(t, err)
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	binding, err := contentcapture.InitializeFileStore(root, source.SourceInstance())
	require.NoError(t, err)
	files, err := contentcapture.OpenFileStore(root, binding)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, files.Close()) })
	service, err := sourcefacts.NewContentCaptureService(source, files, sourcefacts.ContentCaptureServiceConfig{
		Policy: contentcapture.PolicyStoreOptions{SourceInstance: source.SourceInstance(), DefaultMode: contentcapture.ModeMetadataOnly,
			FullContentEnabled: true, SupportedProtocols: config.SupportedProtocols, ObservationPoints: allPoints,
			Limits: contentcapture.PolicyLimits{MaxSavedBytes: limit, MaxReadBytes: limit, MaxBufferBytes: limit, MaxManifestBytes: 65536, MaxManifestItems: 100}},
		Engine: contentcapture.EngineOptions{Queue: contentcapture.QueueLimits{MaxItems: 16, MaxBytes: 4096, MaxProjectBytes: 4096, MaxChunkBytes: limit},
			MaxActive: 2, MaxProjectActive: 2, PrepareTimeout: time.Second, FinalizeTimeout: time.Second, MaxLifetime: time.Minute},
		LeaseOwner: "synthetic-cache-policy", LeaseTTL: time.Minute, StartTimeout: time.Second, HealthTimeout: time.Second,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, service.Close(closeCtx))
	})

	gateway.VerifyContentPolicyCachePaths(t, service, orgID, projectID, keyID, updatePolicy, func() {
		config.SupportedProtocols = []string{string(modeloperation.IDEmbeddings)}
	})
}
