package admin

import (
	"errors"
	"io"
	"net/http"

	"github.com/uniroute/unirouter/internal/providercogs"
)

// ProviderCOGSOfflineContract exposes only the local import contract and its
// safety boundary. It does not discover provider accounts or credentials.
func (h *Handler) ProviderCOGSOfflineContract(w http.ResponseWriter, _ *http.Request) {
	setNoStoreHeaders(w)
	schema, fixtures, goldenReport, err := providercogs.ArtifactDigests()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": map[string]string{
				"code":    "provider_cogs_contract_unavailable",
				"message": "The offline provider cost contract is unavailable.",
			},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"contract_version":        providercogs.ContractVersion,
		"source_snapshot_version": providercogs.SourceSnapshotVersion,
		"report_version":          providercogs.ReportVersion,
		"max_import_bytes":        providercogs.MaxImportBytes,
		"evidence_levels": []string{
			providercogs.EvidenceExactRequestUsage,
			providercogs.EvidenceAggregateMatched,
			providercogs.EvidenceInvoiceReconciled,
		},
		"coverage_states": []string{
			providercogs.CoverageProvisional,
			providercogs.CoverageClosed,
		},
		"capture_gap_codes": []string{
			providercogs.GapProviderDataDelayed,
			providercogs.GapRequestAttributionUnavailable,
			providercogs.GapDiscountCreditUnallocated,
			providercogs.GapTaxFXUnallocated,
		},
		"artifacts": map[string]interface{}{
			"schema":        schema,
			"fixtures":      fixtures,
			"golden_report": goldenReport,
		},
		"authority_boundary": map[string]interface{}{
			"mode":                       "offline_preview",
			"persistent":                 false,
			"provider_network_access":    false,
			"credentials_accepted":       false,
			"customer_ledger_mutation":   providercogs.CustomerLedgerMutationNone,
			"aggregate_auto_attribution": false,
		},
	})
}

// PreviewProviderCOGSOffline evaluates supplied synthetic or manually exported
// evidence in memory. The handler has no database, provider-network or customer
// ledger write path.
func (h *Handler) PreviewProviderCOGSOffline(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	r.Body = http.MaxBytesReader(w, r.Body, providercogs.MaxImportBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]interface{}{
			"error": map[string]string{
				"code":    "import_size_invalid",
				"message": "Offline provider cost evidence exceeds the bounded preview size.",
			},
		})
		return
	}
	report, err := providercogs.Evaluate(body)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, providercogs.ErrConflict) {
			status = http.StatusConflict
		} else if errors.Is(err, providercogs.ErrUnsafe) {
			status = http.StatusUnprocessableEntity
		}
		code := "provider_cogs_evidence_invalid"
		var contractErr *providercogs.ContractError
		if errors.As(err, &contractErr) {
			code = contractErr.Code
		}
		writeJSON(w, status, map[string]interface{}{
			"error": map[string]string{
				"code":    code,
				"message": "Offline provider cost evidence did not satisfy the strict contract.",
			},
		})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func setNoStoreHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}
