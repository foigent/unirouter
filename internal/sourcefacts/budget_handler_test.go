package sourcefacts

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/eventconnector"
)

func TestSourceBudgetSnapshotCanonicalBytesPreserveExactMoney(t *testing.T) {
	reading := BudgetReading{SchemaVersion: BudgetSnapshotVersion, SourceInstance: "synthetic-<&>-范围", OrgID: "1", ProjectID: "2", ReadingID: "ur-budget-reading-00000000000000000000000000000001", ReadingCutoff: "2026-09-10T00:00:00.000000Z", ObservedAt: "2026-09-10T00:00:00.001000Z", BudgetRef: "enterprise-budget:example", DefinitionRevision: "2", BindingRevision: "1"}
	body := BudgetMoney{ValueMinor: "999999999999999999999999999999999999999", Currency: "CNY", Scale: "1000000"}
	doc, err := canonicalBudgetDocument(reading, BudgetSummaryProfile, body)
	require.NoError(t, err)
	require.Equal(t, eventconnector.HashSourceBytes(doc.Document), doc.SourceSHA256)
	var decoded struct {
		Body      BudgetMoney `json:"body"`
		ReadingID string      `json:"reading_id"`
	}
	require.NoError(t, json.Unmarshal(doc.Document, &decoded))
	require.Equal(t, body, decoded.Body)
	require.Equal(t, reading.ReadingID, decoded.ReadingID)
	reading.Documents = []BudgetDocument{doc}
	response, err := budgetResponseBytes(reading)
	require.NoError(t, err)
	var roundTrip BudgetReading
	require.NoError(t, json.Unmarshal(response, &roundTrip))
	require.Equal(t, doc.Document, roundTrip.Documents[0].Document)
	require.Equal(t, doc.SourceSHA256, eventconnector.HashSourceBytes(roundTrip.Documents[0].Document))
	replay, err := canonicalBudgetDocument(reading, BudgetSummaryProfile, body)
	require.NoError(t, err)
	require.Equal(t, doc, replay)
	body.ValueMinor = "0"
	next, err := canonicalBudgetDocument(reading, BudgetSummaryProfile, body)
	require.NoError(t, err)
	require.NotEqual(t, doc.SourceSHA256, next.SourceSHA256)
}
