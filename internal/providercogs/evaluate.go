package providercogs

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uniroute/unirouter/internal/eventconnector"
)

const (
	MaxImportBytes  = 4 << 20
	maxPages        = 100
	maxLines        = 10_000
	maxAttempts     = 10_000
	maxIdentitySize = 256
)

var (
	portableIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)
	providerPattern   = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	currencyPattern   = regexp.MustCompile(`^[A-Z]{3}$`)
	hashRefPattern    = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	unsignedPattern   = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
	signedPattern     = regexp.MustCompile(`^(0|[1-9][0-9]*|-[1-9][0-9]*)$`)
	gapCodePattern    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

type normalizedLine struct {
	line   Line
	digest string
	amount int64
}

// Evaluate performs a zero-persistence Financial Truth preview.
func Evaluate(body []byte) (Report, error) {
	request, err := decodeImport(body)
	if err != nil {
		return Report{}, err
	}
	return evaluate(body, request)
}

func decodeImport(body []byte) (ImportRequest, error) {
	if len(body) == 0 || len(body) > MaxImportBytes {
		return ImportRequest{}, contractError(ErrInvalid, "import_size_invalid")
	}
	if strictErr := eventconnector.ValidateStrictJSON(body, eventconnector.StrictJSONOptions{}); strictErr != nil {
		if strictErr.Code == "duplicate_json_key" {
			return ImportRequest{}, contractError(ErrInvalid, "duplicate_json_key")
		}
		return ImportRequest{}, contractError(ErrInvalid, "import_json_invalid")
	}
	var request ImportRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return ImportRequest{}, contractError(ErrInvalid, "import_shape_invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ImportRequest{}, contractError(ErrInvalid, "trailing_json")
	}
	if !hasExactJSONShape(body, request) {
		return ImportRequest{}, contractError(ErrInvalid, "import_shape_invalid")
	}
	return request, nil
}

func evaluate(body []byte, request ImportRequest) (Report, error) {
	if err := validateRequest(request); err != nil {
		return Report{}, err
	}
	payloadHash := hash(body)
	semanticHash, err := canonicalHash(request)
	if err != nil {
		return Report{}, err
	}
	sourceHash, err := canonicalHash(request.SourceSnapshot)
	if err != nil {
		return Report{}, err
	}
	report := Report{
		ReportVersion:          ReportVersion,
		ContractVersion:        ContractVersion,
		ImportID:               request.ImportID,
		ImportPayloadHash:      payloadHash,
		ImportSemanticHash:     semanticHash,
		SourceSnapshotHash:     sourceHash,
		Provider:               request.Provider,
		SourceInstance:         request.SourceInstance,
		Currency:               request.Currency,
		AmountScale:            request.AmountScale,
		Coverage:               cloneCoverage(request.Coverage),
		ReplayStatus:           ReplayAccepted,
		CustomerLedgerMutation: CustomerLedgerMutationNone,
		Pages:                  make([]PageEvidence, 0, len(request.Pages)),
	}

	uniqueLines := make(map[string]normalizedLine)
	var duplicateLines int
	for _, page := range request.Pages {
		pageHash, hashErr := canonicalHash(page)
		if hashErr != nil {
			return Report{}, hashErr
		}
		report.Pages = append(report.Pages, PageEvidence{
			PageID:         page.PageID,
			PageIndex:      page.PageIndex,
			NormalizedHash: pageHash,
			LineCount:      len(page.Lines),
			IsLast:         page.IsLast,
		})
		for _, line := range page.Lines {
			lineHash, lineHashErr := canonicalHash(line)
			if lineHashErr != nil {
				return Report{}, lineHashErr
			}
			amount, parseErr := parseSigned(line.AmountMinor)
			if parseErr != nil {
				return Report{}, contractError(ErrInvalid, "line_amount_invalid")
			}
			if existing, exists := uniqueLines[line.LineID]; exists {
				if existing.digest != lineHash {
					return Report{}, contractError(ErrConflict, "line_identity_conflict")
				}
				duplicateLines++
				continue
			}
			uniqueLines[line.LineID] = normalizedLine{line: line, digest: lineHash, amount: amount}
		}
	}

	attemptsByUpstream := make(map[string][]Attempt)
	attemptsByIdentity := make(map[string]Attempt)
	for _, attempt := range request.SourceSnapshot.Attempts {
		identity := attempt.RequestID + "\x00" + attempt.AttemptID
		if _, duplicate := attemptsByIdentity[identity]; duplicate {
			return Report{}, contractError(ErrConflict, "attempt_identity_conflict")
		}
		attemptsByIdentity[identity] = attempt
		if attempt.UpstreamRequestID != "" {
			attemptsByUpstream[attempt.UpstreamRequestID] = append(
				attemptsByUpstream[attempt.UpstreamRequestID],
				attempt,
			)
		}
	}

	lineIDs := make([]string, 0, len(uniqueLines))
	for lineID := range uniqueLines {
		lineIDs = append(lineIDs, lineID)
	}
	sort.Strings(lineIDs)
	matchedAttempts := make(map[string]struct{})
	var supplierTotal, matchedSupplier, unattributed int64
	for _, lineID := range lineIDs {
		normalized := uniqueLines[lineID]
		if next, addErr := checkedAdd(supplierTotal, normalized.amount); addErr != nil {
			return Report{}, contractError(ErrInvalid, "supplier_total_overflow")
		} else {
			supplierTotal = next
		}
		match := Match{
			LineID:            normalized.line.LineID,
			UpstreamRequestID: normalized.line.UpstreamRequestID,
			ProjectRef:        normalized.line.ProjectRef,
			EvidenceLevel:     normalized.line.EvidenceLevel,
			ProviderCost: &Amount{
				ValueMinor: normalized.line.AmountMinor,
				Currency:   request.Currency,
				Scale:      request.AmountScale,
				Authority:  "supplier_reported",
			},
			CustomerDebtAction: "none",
		}
		if normalized.line.Granularity != GranularityRequest {
			match.Status = StatusAggregateUnattributed
			report.Summary.AggregateUnattributed++
			if next, addErr := checkedAdd(unattributed, normalized.amount); addErr != nil {
				return Report{}, contractError(ErrInvalid, "unattributed_total_overflow")
			} else {
				unattributed = next
			}
			report.Matches = append(report.Matches, match)
			continue
		}
		candidates := attemptsByUpstream[normalized.line.UpstreamRequestID]
		if normalized.line.ProjectRef != "" {
			scoped := make([]Attempt, 0, len(candidates))
			for _, candidate := range candidates {
				if candidate.ProjectRef == normalized.line.ProjectRef {
					scoped = append(scoped, candidate)
				}
			}
			candidates = scoped
		}
		switch len(candidates) {
		case 0:
			match.Status = StatusSupplierOnly
			report.Summary.SupplierOnly++
			if next, addErr := checkedAdd(unattributed, normalized.amount); addErr != nil {
				return Report{}, contractError(ErrInvalid, "unattributed_total_overflow")
			} else {
				unattributed = next
			}
		case 1:
			attempt := candidates[0]
			match.Status = StatusExactRequestMatched
			match.RequestID = attempt.RequestID
			match.AttemptID = attempt.AttemptID
			match.ProjectRef = attempt.ProjectRef
			match.ProviderEstimate = &Amount{
				ValueMinor: attempt.ProviderEstimateMinor,
				Currency:   attempt.ProviderCurrency,
				Scale:      attempt.ProviderAmountScale,
				Authority:  "unirouter_estimated",
			}
			match.CustomerRevenue = &Amount{
				ValueMinor: attempt.CustomerRevenueMicros,
				Currency:   attempt.CustomerCurrency,
				Scale:      attempt.CustomerAmountScale,
				Authority:  "customer_ledger_snapshot",
			}
			estimate, _ := parseSigned(attempt.ProviderEstimateMinor)
			variance, subtractErr := checkedSubtract(normalized.amount, estimate)
			if subtractErr != nil {
				return Report{}, contractError(ErrInvalid, "provider_variance_overflow")
			}
			match.ProviderVariance = strconv.FormatInt(variance, 10)
			matchedAttempts[attempt.RequestID+"\x00"+attempt.AttemptID] = struct{}{}
			report.Summary.ExactRequestMatches++
			if next, addErr := checkedAdd(matchedSupplier, normalized.amount); addErr != nil {
				return Report{}, contractError(ErrInvalid, "matched_total_overflow")
			} else {
				matchedSupplier = next
			}
		default:
			match.Status = StatusAmbiguous
			report.Summary.Ambiguous++
			if next, addErr := checkedAdd(unattributed, normalized.amount); addErr != nil {
				return Report{}, contractError(ErrInvalid, "unattributed_total_overflow")
			} else {
				unattributed = next
			}
		}
		report.Matches = append(report.Matches, match)
	}

	attemptIdentities := make([]string, 0, len(attemptsByIdentity))
	for identity := range attemptsByIdentity {
		attemptIdentities = append(attemptIdentities, identity)
	}
	sort.Strings(attemptIdentities)
	for _, identity := range attemptIdentities {
		if _, matched := matchedAttempts[identity]; matched {
			continue
		}
		attempt := attemptsByIdentity[identity]
		status := StatusSupplierPending
		if request.Coverage.State == CoverageClosed && withinCoverage(
			attempt.OccurredAt,
			request.Coverage.StartAt,
			request.Coverage.EndAt,
		) {
			status = StatusSourceOnly
			report.Summary.SourceOnly++
		} else {
			report.Summary.SupplierEvidencePending++
		}
		report.Matches = append(report.Matches, Match{
			Status:            status,
			EvidenceLevel:     EvidenceExactRequestUsage,
			RequestID:         attempt.RequestID,
			AttemptID:         attempt.AttemptID,
			UpstreamRequestID: attempt.UpstreamRequestID,
			ProjectRef:        attempt.ProjectRef,
			ProviderEstimate: &Amount{
				ValueMinor: attempt.ProviderEstimateMinor,
				Currency:   attempt.ProviderCurrency,
				Scale:      attempt.ProviderAmountScale,
				Authority:  "unirouter_estimated",
			},
			CustomerRevenue: &Amount{
				ValueMinor: attempt.CustomerRevenueMicros,
				Currency:   attempt.CustomerCurrency,
				Scale:      attempt.CustomerAmountScale,
				Authority:  "customer_ledger_snapshot",
			},
			CustomerDebtAction: "none",
		})
	}
	report.Summary.Pages = len(request.Pages)
	report.Summary.UniqueSupplierLines = len(uniqueLines)
	report.Summary.DuplicateNoopLines = duplicateLines
	report.Summary.SourceAttempts = len(request.SourceSnapshot.Attempts)
	report.Summary.SupplierTotalMinor = strconv.FormatInt(supplierTotal, 10)
	report.Summary.MatchedSupplierMinor = strconv.FormatInt(matchedSupplier, 10)
	report.Summary.UnattributedMinor = strconv.FormatInt(unattributed, 10)
	return report, nil
}

func validateRequest(request ImportRequest) error {
	if request.ContractVersion != ContractVersion {
		return contractError(ErrInvalid, "contract_version_unsupported")
	}
	if !validPortableID(request.ImportID, 160) ||
		!providerPattern.MatchString(request.Provider) ||
		!hashRefPattern.MatchString(request.SourceInstance) ||
		!currencyPattern.MatchString(request.Currency) {
		return contractError(ErrInvalid, "import_identity_invalid")
	}
	scale, err := parseUnsigned(request.AmountScale)
	if err != nil || scale == 0 || scale > 1_000_000_000 || !isPowerOfTen(scale) {
		return contractError(ErrInvalid, "amount_scale_invalid")
	}
	if err := validateCoverage(request.Coverage); err != nil {
		return err
	}
	if len(request.Pages) == 0 || len(request.Pages) > maxPages {
		return contractError(ErrInvalid, "page_count_invalid")
	}
	expectedCursor := ""
	totalLines := 0
	seenPages := make(map[string]struct{}, len(request.Pages))
	for index, page := range request.Pages {
		if !validPortableID(page.PageID, 160) || page.PageIndex != strconv.Itoa(index) {
			return contractError(ErrInvalid, "page_identity_invalid")
		}
		if _, duplicate := seenPages[page.PageID]; duplicate {
			return contractError(ErrConflict, "page_identity_conflict")
		}
		seenPages[page.PageID] = struct{}{}
		if page.CursorIn != expectedCursor ||
			!validOpaque(page.CursorIn, 1024) ||
			!validOpaque(page.CursorOut, 1024) {
			return contractError(ErrInvalid, "page_cursor_chain_invalid")
		}
		isFinalIndex := index == len(request.Pages)-1
		if page.IsLast != isFinalIndex {
			return contractError(ErrInvalid, "page_finality_invalid")
		}
		if page.IsLast && page.CursorOut != "" {
			return contractError(ErrInvalid, "final_page_cursor_invalid")
		}
		if !page.IsLast && page.CursorOut == "" {
			return contractError(ErrInvalid, "nonfinal_page_cursor_invalid")
		}
		expectedCursor = page.CursorOut
		if page.Lines == nil {
			return contractError(ErrInvalid, "page_lines_missing")
		}
		totalLines += len(page.Lines)
		if totalLines > maxLines {
			return contractError(ErrInvalid, "line_count_invalid")
		}
		for _, line := range page.Lines {
			if err := validateLine(line); err != nil {
				return err
			}
		}
	}
	if request.Coverage.State == CoverageProvisional &&
		!hasGap(request.Coverage.Gaps, GapProviderDataDelayed) {
		return contractError(ErrInvalid, "provisional_coverage_gap_missing")
	}
	if err := validateSnapshot(request.SourceSnapshot, request); err != nil {
		return err
	}
	return nil
}

func validateCoverage(coverage Coverage) error {
	start, err := parseCanonicalTime(coverage.StartAt)
	if err != nil {
		return contractError(ErrInvalid, "coverage_time_invalid")
	}
	end, err := parseCanonicalTime(coverage.EndAt)
	if err != nil || !start.Before(end) {
		return contractError(ErrInvalid, "coverage_time_invalid")
	}
	if coverage.State != CoverageProvisional && coverage.State != CoverageClosed {
		return contractError(ErrInvalid, "coverage_state_invalid")
	}
	if coverage.Gaps == nil {
		return contractError(ErrInvalid, "coverage_gaps_missing")
	}
	seen := make(map[string]struct{}, len(coverage.Gaps))
	for _, gap := range coverage.Gaps {
		if !gapCodePattern.MatchString(gap.Code) ||
			!oneOf(
				gap.Code,
				GapProviderDataDelayed,
				GapRequestAttributionUnavailable,
				GapDiscountCreditUnallocated,
				GapTaxFXUnallocated,
			) ||
			gap.Provenance != "declared" ||
			gap.Detail == "" ||
			len(gap.Detail) > 240 ||
			!utf8.ValidString(gap.Detail) {
			return contractError(ErrInvalid, "coverage_gap_invalid")
		}
		if _, duplicate := seen[gap.Code]; duplicate {
			return contractError(ErrConflict, "coverage_gap_duplicate")
		}
		seen[gap.Code] = struct{}{}
	}
	return nil
}

func validateLine(line Line) error {
	if !validPortableID(line.LineID, maxIdentitySize) ||
		!unsignedPattern.MatchString(line.Revision) ||
		line.Revision == "0" ||
		!oneOf(line.LineKind, LineCharge, LineCredit, LineTax, LineAdjustment) ||
		!oneOf(line.Granularity, GranularityRequest, GranularityProjectBucket, GranularityAccountBucket, GranularityInvoiceLine) ||
		!oneOf(line.Finality, FinalityProvisional, FinalityFinal, FinalityInvoice) {
		return contractError(ErrInvalid, "line_identity_invalid")
	}
	if _, err := parseCanonicalTime(line.ObservedAt); err != nil {
		return contractError(ErrInvalid, "line_observed_at_invalid")
	}
	amount, err := parseSigned(line.AmountMinor)
	if err != nil ||
		((line.LineKind == LineCharge || line.LineKind == LineTax) && amount < 0) ||
		(line.LineKind == LineCredit && amount > 0) {
		return contractError(ErrInvalid, "line_amount_invalid")
	}
	switch line.Granularity {
	case GranularityRequest:
		if line.UpstreamRequestID == "" ||
			!validOpaque(line.UpstreamRequestID, maxIdentitySize) ||
			line.PeriodStart != "" ||
			line.PeriodEnd != "" ||
			line.EvidenceLevel != EvidenceExactRequestUsage {
			return contractError(ErrInvalid, "request_line_semantics_invalid")
		}
	default:
		start, startErr := parseCanonicalTime(line.PeriodStart)
		end, endErr := parseCanonicalTime(line.PeriodEnd)
		if startErr != nil || endErr != nil || !start.Before(end) ||
			line.UpstreamRequestID != "" ||
			(line.EvidenceLevel != EvidenceAggregateMatched &&
				line.EvidenceLevel != EvidenceInvoiceReconciled) {
			return contractError(ErrInvalid, "aggregate_line_semantics_invalid")
		}
	}
	if line.Finality == FinalityInvoice && line.EvidenceLevel != EvidenceInvoiceReconciled {
		return contractError(ErrInvalid, "invoice_evidence_level_invalid")
	}
	if line.ProjectRef != "" && !hashRefPattern.MatchString(line.ProjectRef) {
		return contractError(ErrInvalid, "line_project_ref_invalid")
	}
	if line.LineItem == "" || len(line.LineItem) > 160 || !utf8.ValidString(line.LineItem) {
		return contractError(ErrInvalid, "line_item_invalid")
	}
	return nil
}

func validateSnapshot(snapshot SourceSnapshot, request ImportRequest) error {
	if snapshot.SchemaVersion != SourceSnapshotVersion ||
		!validPortableID(snapshot.SnapshotID, 160) ||
		snapshot.Attempts == nil ||
		len(snapshot.Attempts) > maxAttempts {
		return contractError(ErrInvalid, "source_snapshot_identity_invalid")
	}
	if _, err := parseCanonicalTime(snapshot.CreatedAt); err != nil {
		return contractError(ErrInvalid, "source_snapshot_time_invalid")
	}
	for _, attempt := range snapshot.Attempts {
		if !hashRefPattern.MatchString(attempt.OrgRef) ||
			(attempt.ProjectRef != "" && !hashRefPattern.MatchString(attempt.ProjectRef)) ||
			!validPortableID(attempt.RequestID, maxIdentitySize) ||
			!validPortableID(attempt.AttemptID, maxIdentitySize) ||
			attempt.Provider != request.Provider ||
			attempt.SourceInstance != request.SourceInstance ||
			(attempt.UpstreamRequestID != "" && !validOpaque(attempt.UpstreamRequestID, maxIdentitySize)) {
			return contractError(ErrInvalid, "source_attempt_identity_invalid")
		}
		if _, err := parseCanonicalTime(attempt.OccurredAt); err != nil {
			return contractError(ErrInvalid, "source_attempt_time_invalid")
		}
		if _, err := parseSigned(attempt.ProviderEstimateMinor); err != nil ||
			attempt.ProviderCurrency != request.Currency ||
			attempt.ProviderAmountScale != request.AmountScale {
			return contractError(ErrInvalid, "provider_estimate_invalid")
		}
		if _, err := parseUnsigned(attempt.CustomerRevenueMicros); err != nil ||
			attempt.CustomerCurrency != "CNY" ||
			attempt.CustomerAmountScale != "1000000" {
			return contractError(ErrInvalid, "customer_revenue_snapshot_invalid")
		}
	}
	return nil
}

func parseCanonicalTime(value string) (time.Time, error) {
	if len(value) != len("2006-01-02T15:04:05.000000Z") {
		return time.Time{}, errors.New("time length")
	}
	parsed, err := time.Parse("2006-01-02T15:04:05.000000Z", value)
	if err != nil || parsed.UTC().Format("2006-01-02T15:04:05.000000Z") != value {
		return time.Time{}, errors.New("time format")
	}
	return parsed, nil
}

func withinCoverage(value, start, end string) bool {
	instant, instantErr := parseCanonicalTime(value)
	from, fromErr := parseCanonicalTime(start)
	to, toErr := parseCanonicalTime(end)
	return instantErr == nil && fromErr == nil && toErr == nil &&
		!instant.Before(from) && instant.Before(to)
}

func parseUnsigned(value string) (int64, error) {
	if !unsignedPattern.MatchString(value) {
		return 0, errors.New("not uint")
	}
	return strconv.ParseInt(value, 10, 64)
}

func parseSigned(value string) (int64, error) {
	if !signedPattern.MatchString(value) {
		return 0, errors.New("not int")
	}
	return strconv.ParseInt(value, 10, 64)
}

func checkedAdd(left, right int64) (int64, error) {
	if (right > 0 && left > math.MaxInt64-right) ||
		(right < 0 && left < math.MinInt64-right) {
		return 0, errors.New("overflow")
	}
	return left + right, nil
}

func checkedSubtract(left, right int64) (int64, error) {
	if right == math.MinInt64 {
		if left >= 0 {
			return 0, errors.New("overflow")
		}
		return left - right, nil
	}
	return checkedAdd(left, -right)
}

func canonicalHash(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", contractError(ErrInvalid, "canonical_encoding_failed")
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", contractError(ErrInvalid, "canonical_encoding_failed")
	}
	canonical, connectorErr := eventconnector.CanonicalJSON(decoded)
	if connectorErr != nil {
		return "", contractError(ErrInvalid, "canonical_encoding_failed")
	}
	return hash(canonical), nil
}

func hasExactJSONShape(body []byte, typed any) bool {
	var source any
	if err := json.Unmarshal(body, &source); err != nil {
		return false
	}
	typedBody, err := json.Marshal(typed)
	if err != nil {
		return false
	}
	var normalized any
	if err := json.Unmarshal(typedBody, &normalized); err != nil {
		return false
	}
	sourceCanonical, sourceErr := eventconnector.CanonicalJSON(source)
	normalizedCanonical, normalizedErr := eventconnector.CanonicalJSON(normalized)
	return sourceErr == nil && normalizedErr == nil &&
		bytes.Equal(sourceCanonical, normalizedCanonical)
}

func hash(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func validPortableID(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && portableIDPattern.MatchString(value)
}

func validOpaque(value string, maximum int) bool {
	if len(value) > maximum || !utf8.ValidString(value) {
		return false
	}
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return false
		}
	}
	return true
}

func oneOf(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}

func isPowerOfTen(value int64) bool {
	for value > 1 && value%10 == 0 {
		value /= 10
	}
	return value == 1
}

func hasGap(gaps []Gap, code string) bool {
	for _, gap := range gaps {
		if gap.Code == code {
			return true
		}
	}
	return false
}

func cloneCoverage(coverage Coverage) Coverage {
	coverage.Gaps = append([]Gap(nil), coverage.Gaps...)
	return coverage
}

func cloneReport(report Report) Report {
	report.Coverage = cloneCoverage(report.Coverage)
	report.Pages = append([]PageEvidence(nil), report.Pages...)
	report.Matches = append([]Match(nil), report.Matches...)
	for index := range report.Matches {
		if report.Matches[index].ProviderCost != nil {
			value := *report.Matches[index].ProviderCost
			report.Matches[index].ProviderCost = &value
		}
		if report.Matches[index].ProviderEstimate != nil {
			value := *report.Matches[index].ProviderEstimate
			report.Matches[index].ProviderEstimate = &value
		}
		if report.Matches[index].CustomerRevenue != nil {
			value := *report.Matches[index].CustomerRevenue
			report.Matches[index].CustomerRevenue = &value
		}
	}
	return report
}

func safeErrorCode(err error) string {
	var contractErr *ContractError
	if errors.As(err, &contractErr) {
		return contractErr.Code
	}
	return fmt.Sprintf("unknown_%T", err)
}

func sortedGapCodes(gaps []Gap) []string {
	result := make([]string, 0, len(gaps))
	for _, gap := range gaps {
		result = append(result, gap.Code)
	}
	sort.Strings(result)
	return result
}

func normalizeProvider(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
