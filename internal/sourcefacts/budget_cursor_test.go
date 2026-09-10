package sourcefacts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceBudgetCursorDomainsAndAccessRevisions(t *testing.T) {
	s := &Service{options: Options{SourceInstance: "synthetic", CursorSecret: strings.Repeat("x", 32)}}
	scope := budgetAccess{access: access{instance: "synthetic", org: 2, project: 3, keyID: 4, generation: 5}, dGeneration: 6, catalogRevision: 7}
	c := budgetCursor{Version: BudgetSnapshotVersion, Kind: "catalog", Instance: "synthetic", Org: 2, Project: 3, Key: 4, AccessGeneration: 6, CatalogRevision: 7, AfterRef: "enterprise.synthetic"}
	token, err := s.encodeBudgetCursor(c)
	require.NoError(t, err)
	read, err := s.decodeBudgetCursor(token, "catalog", scope)
	require.NoError(t, err)
	require.Equal(t, c, read)
	_, err = s.decodeBudgetCursor(token, "alerts", scope)
	require.Error(t, err)
	for _, mutate := range []func(*budgetAccess){
		func(v *budgetAccess) { v.org++ }, func(v *budgetAccess) { v.project++ },
		func(v *budgetAccess) { v.keyID++ }, func(v *budgetAccess) { v.instance = "other" },
		func(v *budgetAccess) { v.dGeneration++ }, func(v *budgetAccess) { v.catalogRevision++ },
	} {
		changed := scope
		mutate(&changed)
		_, err = s.decodeBudgetCursor(token, "catalog", changed)
		require.Error(t, err)
	}
	// D has its own generation. This does not bypass global authentication:
	// handlers must reject revoked keys and paused connections before decoding.
	changed := scope
	changed.generation++
	_, err = s.decodeBudgetCursor(token, "catalog", changed)
	require.NoError(t, err)
	_, err = s.decodeBudgetCursor(token+"x", "catalog", scope)
	require.Error(t, err)
	_, err = s.decodeBudgetCursor(strings.Repeat("x", 4097), "catalog", scope)
	require.Error(t, err)
	c.Kind, c.AfterRef, c.BudgetRef, c.DefinitionRevision, c.BindingRevision, c.AfterAlertID = "alerts", "", "gateway", 2, 0, 19
	alert, err := s.encodeBudgetCursor(c)
	require.NoError(t, err)
	read, err = s.decodeBudgetCursor(alert, "alerts", scope)
	require.NoError(t, err)
	require.Equal(t, int64(2), read.DefinitionRevision)
	require.Equal(t, int64(19), read.AfterAlertID)
	_, err = s.decodeBudgetCursor(alert, "catalog", scope)
	require.Error(t, err)
}
