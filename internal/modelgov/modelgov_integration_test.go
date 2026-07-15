//go:build integration

package modelgov

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestFilterCallableModels_MatchesCheckAccessIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := testutil.NewTestOrg(t, pool)

	var projectID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id, name, slug, status)
		 VALUES ($1, 'Pilot Project', 'pilot', 'active')
		 RETURNING id`, orgID,
	).Scan(&projectID))

	_, err := pool.Exec(ctx,
		`INSERT INTO model_registry
		 (org_id, model_name, display_name, lifecycle, approved_catalog)
		 VALUES
		 ($1, 'pilot-direct', 'Pilot Direct', 'ga', FALSE),
		 ($1, 'pilot-alias-name-target', 'Alias Name Target', 'ga', FALSE),
		 ($1, 'pilot-target-item', 'Target Item', 'ga', FALSE),
		 ($1, 'pilot-retired', 'Retired', 'retired', TRUE),
		 ($1, 'pilot-denied', 'Denied', 'ga', TRUE)`,
		orgID,
	)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO model_aliases (org_id, alias, target_model, status)
		 VALUES
		 ($1, 'pilot-alias-by-name', 'pilot-alias-name-target', 'active'),
		 ($1, 'pilot-alias-by-target', 'pilot-target-item', 'active')`,
		orgID,
	)
	require.NoError(t, err)

	var modelSetID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO model_sets (org_id, name, slug, status)
		 VALUES ($1, 'Pilot Models', 'pilot-models', 'active')
		 RETURNING id`, orgID,
	).Scan(&modelSetID))
	_, err = pool.Exec(ctx,
		`INSERT INTO model_set_items (model_set_id, item_type, item_name)
		 VALUES
		 ($1, 'model', 'pilot-direct'),
		 ($1, 'alias', 'pilot-alias-by-name'),
		 ($1, 'model', 'pilot-target-item')`,
		modelSetID,
	)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO project_model_sets (project_id, model_set_id) VALUES ($1, $2)`,
		projectID, modelSetID,
	)
	require.NoError(t, err)

	candidates := []string{
		"pilot-direct",
		"pilot-alias-by-name",
		"pilot-alias-name-target",
		"pilot-alias-by-target",
		"pilot-target-item",
		"pilot-retired",
		"pilot-denied",
		"pilot-unregistered",
	}
	for _, tc := range []struct {
		name      string
		projectID int64
	}{
		{name: "without project still excludes retired", projectID: 0},
		{name: "project model set intersection", projectID: projectID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expected := callableModelsByCheckAccess(t, ctx, pool, orgID, tc.projectID, candidates)
			actual, err := FilterCallableModels(ctx, pool, orgID, tc.projectID, candidates)
			require.NoError(t, err)
			require.Equal(t, expected, actual)
		})
	}
}

func callableModelsByCheckAccess(t *testing.T, ctx context.Context, q db.DBQuerier, orgID, projectID int64, candidates []string) []string {
	t.Helper()
	allowed := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		result, err := CheckAccess(ctx, q, orgID, projectID, candidate)
		if errors.Is(err, ErrModelRetired) || errors.Is(err, ErrModelDenied) {
			continue
		}
		require.NoError(t, err)
		if result.Allowed {
			allowed = append(allowed, candidate)
		}
	}
	return allowed
}
