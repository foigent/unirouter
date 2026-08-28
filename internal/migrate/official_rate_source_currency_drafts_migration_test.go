package migrate_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOfficialRateSourceCurrencyDraftMigrationContracts(t *testing.T) {
	upBody, err := os.ReadFile("../../migrations/000084_official_rate_source_currency_drafts.up.sql")
	require.NoError(t, err)
	up := string(upBody)
	require.Contains(t, up, "CHECK (currency IN ('CNY', 'USD'))")
	require.Contains(t, up, "review-only")
	require.NotContains(t, strings.ToLower(up), "wallets")
	require.NotContains(t, strings.ToLower(up), "exchange_rate")

	downBody, err := os.ReadFile("../../migrations/000084_official_rate_source_currency_drafts.down.sql")
	require.NoError(t, err)
	down := string(downBody)
	require.Contains(t, down, "version > '000084_official_rate_source_currency_drafts'")
	require.Contains(t, down, "non-CNY source evidence exists")
	require.Less(t, strings.Index(down, "non-CNY source evidence exists"), strings.Index(down, "CHECK (currency = 'CNY')"))
	require.Less(t, strings.Index(down, "CHECK (currency = 'CNY')"), strings.Index(down, "DELETE FROM schema_migrations"))
}
