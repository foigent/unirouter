package sourceevidence

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
)

func TestContractFixturesAndHonestPartialCompatibility(t *testing.T) {
	requestSetBody := readContractFixture(t, "fixtures/request-set.valid.v1.json")
	requestSet, err := DecodeRequestSet(requestSetBody)
	require.NoError(t, err)
	require.Equal(t, RequestSetSchemaVersion, requestSet.SchemaVersion)
	require.Len(t, requestSet.Requests, 1)
	sourceScope, err := DecodeSourceScope(readContractFixture(t, "fixtures/source-scope.valid.v1.json"))
	require.NoError(t, err)
	require.NoError(t, ValidateSourceScopeForRequestSet(sourceScope, requestSet))
	_, err = DecodeSourceScope(readContractFixture(t, "fixtures/runtime-readback.rejected.v1.json"))
	require.Error(t, err, "retired operator assertions must not decode as Source scope")

	partial, err := DecodeCompatibilityBundle(readContractFixture(t, "fixtures/execution-evidence.partial.valid.v2.json"))
	require.NoError(t, err)
	require.Equal(t, "partial", partial.Completeness.State)
	require.False(t, partial.Completeness.SourceProfileComplete)
	require.Contains(t, partial.CaptureGaps, CompatibilityGap{
		Code:   CompatibilityGapRuntimeCloseMarker,
		Stage:  "source",
		Reason: "runtime close marker is unavailable; closed_at is the snapshot cutoff, not a runtime closure claim; expected hashes are limited to persisted facts visible in the frozen repeatable-read snapshot",
	})

	_, err = DecodeCompatibilityBundle(readContractFixture(t, "fixtures/execution-evidence.fake-complete.rejected.v2.json"))
	require.ErrorContains(t, err, "cannot claim complete")
}

func TestContractsRejectUnknownDuplicateAndScopeWidening(t *testing.T) {
	body := readContractFixture(t, "fixtures/request-set.valid.v1.json")
	var document map[string]any
	require.NoError(t, json.Unmarshal(body, &document))
	document["unreviewed_scope"] = true
	withUnknown, err := json.Marshal(document)
	require.NoError(t, err)
	_, err = DecodeRequestSet(withUnknown)
	require.Error(t, err)

	duplicate := bytes.Replace(body, []byte(`"campaign_id": "joint-poc-20260810"`), []byte(`"campaign_id": "joint-poc-20260810", "campaign_id": "other"`), 1)
	_, err = DecodeRequestSet(duplicate)
	require.Error(t, err)

	requestSet := validRequestSet(t)
	requestSet.Limits.MaxRequests++
	require.ErrorContains(t, ValidateRequestSet(requestSet), "limits")

	facts, _ := validGeneration(t, validRequestSet(t))
	facts.SourceFacts.Source.RuntimeObservation = "operator_asserted"
	require.ErrorContains(t, ValidateSourceFacts(facts.SourceFacts), "claim boundary")
	facts, _ = validGeneration(t, validRequestSet(t))
	facts.SourceFacts.Release.Commit = strings.Repeat("A", 40)
	require.ErrorContains(t, ValidateSourceFacts(facts.SourceFacts), "release identity")

	complete := validCompatibilityMember(t)
	complete.Completeness.State = "complete"
	complete.Completeness.SourceProfileComplete = true
	require.ErrorContains(t, ValidateCompatibilityBundle(complete), "cannot claim complete")

	call := CallLogFact{
		SourceRef:          "unirouter:test:call_logs:1",
		ID:                 1,
		Model:              "model-a",
		Provider:           "provider-a",
		PromptTokens:       1,
		CompletionTokens:   1,
		TotalTokens:        2,
		CostMicros:         "1",
		Currency:           "CNY",
		AmountScale:        1_000_000,
		StatusCode:         200,
		LatencyMS:          1,
		PayloadCaptureMode: "metadata_only",
		CreatedAt:          "2026-08-10T12:00:00Z",
	}
	call.FactSHA256, err = semanticFactHash(call)
	require.NoError(t, err)
	require.NoError(t, validateCallLogFact(call))
	call.Model = "tampered-model"
	require.ErrorContains(t, validateCallLogFact(call), "does not match")
}

func TestImmutableFactsSuppressPrivateActorAndAccessLocators(t *testing.T) {
	require.Equal(t, "agent", canonicalSourceActorType("agent"))
	require.Equal(t, "unknown", canonicalSourceActorType("user:42"))
	require.Equal(t, "unknown", canonicalSourceActorType("person@example.com"))

	credentialRef := "cred_source_evidence_correlation"
	correlation, err := logicalCredentialCorrelation(credentialRef)
	require.NoError(t, err)
	require.True(t, hashRefPattern.MatchString(correlation))
	require.NotContains(t, correlation, credentialRef)
	require.Equal(t, correlation, mustLogicalCredentialCorrelation(t, credentialRef))
	_, err = logicalCredentialCorrelation("user:42")
	require.ErrorContains(t, err, "canonical source-owned opaque reference")

	bindingType, bindingCorrelation, err := providerAttemptAccessBinding(
		101, 201, 301,
		sql.NullInt64{Int64: 401, Valid: true},
		sql.NullString{String: credentialRef, Valid: true},
		sql.NullInt64{Int64: 501, Valid: true},
	)
	require.NoError(t, err)
	require.Equal(t, ProviderAttemptAccessBindingLogical, bindingType)
	require.Equal(t, correlation, bindingCorrelation)
	legacyType, legacyCorrelation, err := providerAttemptAccessBinding(101, 201, 301, sql.NullInt64{}, sql.NullString{}, sql.NullInt64{})
	require.NoError(t, err)
	require.Equal(t, ProviderAttemptAccessBindingLegacy, legacyType)
	require.Empty(t, legacyCorrelation)
	_, _, err = providerAttemptAccessBinding(101, 201, 301, sql.NullInt64{Int64: 401, Valid: true}, sql.NullString{}, sql.NullInt64{Int64: 501, Valid: true})
	require.ErrorContains(t, err, "incomplete")
	productionFailureKinds := map[string]string{
		string(adapters.FailureNone):          "",
		string(adapters.FailureClientRequest): "client_request",
		string(adapters.FailureAuth):          "auth",
		string(adapters.FailureRateLimit):     "rate_limit",
		string(adapters.FailureChannel):       "channel_failure",
		string(adapters.FailureUnknown):       "unknown",
		"configuration":                       "configuration",
		"request_build":                       "request_build",
		"response_too_large":                  "response_too_large",
		"payload_capacity":                    "payload_capacity",
		"response_read":                       "response_read",
		"response_parse":                      "response_parse",
		"transport":                           "transport",
		"delivery_failed":                     "delivery_failure",
		"downstream_write_failed":             "delivery_failure",
		"response_encode_failed":              "delivery_failure",
		"process_lost":                        "delivery_failure",
		"settlement_usage_persist_failed":     "delivery_failure",
	}
	for input, expected := range productionFailureKinds {
		require.Equal(t, expected, canonicalProviderFailureKind(input), input)
	}
	require.Equal(t, "unclassified", canonicalProviderFailureKind("raw upstream timeout: person@example.com"))

	attempt := ProviderAttemptFact{
		SourceRef: "unirouter:test:billing_provider_attempts:1:attempt-1", AttemptID: "attempt-1",
		AccessBindingType: ProviderAttemptAccessBindingLogical, LogicalCredentialCorrelationSHA256: correlation,
		RouteSequence: 1, RouteTotal: 1, Endpoint: "/v1/chat/completions", Provider: "provider-a",
		State: "settled", OutcomeCertainty: "known_usage", FailureKind: "transport",
		StartedAt: "2026-08-10T12:00:00Z", UpdatedAt: "2026-08-10T12:00:01Z",
	}
	attempt.FactSHA256, err = semanticFactHash(attempt)
	require.NoError(t, err)
	require.NoError(t, validateProviderAttemptFact(attempt))
	attempt.FailureKind = "raw upstream timeout: person@example.com"
	attempt.FactSHA256 = ""
	attempt.FactSHA256, err = semanticFactHash(attempt)
	require.NoError(t, err)
	require.ErrorContains(t, validateProviderAttemptFact(attempt), "machine classification")

	closedAt := time.Date(2026, 8, 10, 12, 1, 0, 0, time.UTC)
	trace := CompatibilityTraceFact{
		SourceRef: "unirouter:test:request_trace_events:1", SourceEventHash: strings.Repeat("a", 64),
		EventType: "route_selected", Stage: "routing", Severity: "info", CreatedAt: "2026-08-10T12:00:00Z",
		ActorType: "human", ActorRef: "user:42", PayloadCaptureMode: "metadata_only", Metadata: map[string]any{},
	}
	require.ErrorContains(t, validateCompatibilityTrace(0, trace, closedAt), "private actor locator")
	trace.ActorRef = ""
	trace.OnBehalfOf = "api_key:7"
	require.ErrorContains(t, validateCompatibilityTrace(0, trace, closedAt), "private actor locator")
	trace.OnBehalfOf = ""
	trace.ActorType = "person@example.com"
	require.ErrorContains(t, validateCompatibilityTrace(0, trace, closedAt), "not allowlisted")

	call := CompatibilityCallLogFact{
		SourceRef: "unirouter:test:call_logs:1", SourceEventHash: strings.Repeat("b", 64), CreatedAt: "2026-08-10T12:00:00Z",
		ActorType: "human", ActorRef: "user:42", Model: "model-a", Provider: "provider-a", StatusCode: 200,
		PromptTokens: 1, CompletionTokens: 1, CostMicros: "1", Currency: "CNY", AmountScale: 1_000_000,
		PayloadCaptureMode: "metadata_only",
	}
	require.ErrorContains(t, validateCompatibilityCall(0, call, closedAt), "private actor locator")
}

func mustLogicalCredentialCorrelation(t *testing.T, credentialRef string) string {
	t.Helper()
	value, err := logicalCredentialCorrelation(credentialRef)
	require.NoError(t, err)
	return value
}

func TestSourceScopeAndNeutralBindingAreExact(t *testing.T) {
	requestSet := validRequestSet(t)
	sourceScope := SourceScopeForRequestSet(requestSet)
	require.NoError(t, ValidateSourceScopeForRequestSet(sourceScope, requestSet))
	require.NoError(t, ValidateExternalSchemaVersion(
		[]byte(`{"schema_version":"foigent.integration-campaign-manifest.v1","campaign_id":"joint-poc-20260810"}`),
		"foigent.integration-campaign-manifest.v1",
	))
	require.Error(t, ValidateExternalSchemaVersion(
		[]byte(`{"schema_version":"foigent.integration-campaign-result.v1"}`),
		"foigent.integration-campaign-manifest.v1",
	))
	require.Error(t, ValidateExternalSchemaVersion(
		[]byte(`{"schema_version":"foigent.integration-campaign-manifest.v1","schema_version":"foigent.integration-campaign-manifest.v1"}`),
		"foigent.integration-campaign-manifest.v1",
	))

	wrongSource := sourceScope
	wrongSource.SourceInstance = "different-source"
	require.ErrorContains(t, ValidateSourceScopeForRequestSet(wrongSource, requestSet), "does not match")
	wrongAuthority := sourceScope
	wrongAuthority.RuntimeObservation = "operator_asserted"
	require.ErrorContains(t, ValidateSourceScope(wrongAuthority), "claim boundary")

	authorization := validNeutralAuthorization(t, sourceScope)
	authorization.SourceBindingSHA256 = "sha256:" + strings.Repeat("0", 64)
	require.ErrorContains(t, ValidateNeutralAuthorizationBinding(authorization, sourceScope), "does not match")

	generation, _ := validGeneration(t, requestSet)
	generation.SourceScope.SourceInstance = "different-source"
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x42}, ed25519.SeedSize))
	publicKeys := map[string]ed25519.PublicKey{"source-evidence-test-20260810": privateKey.Public().(ed25519.PublicKey)}
	var rejected bytes.Buffer
	_, err := Pack(&rejected, PackageOptions{
		SigningKeyRef: "source-evidence-test-20260810",
		PublicKeys:    publicKeys,
		PrivateKey:    privateKey,
		CreatedAt:     time.Date(2026, 8, 10, 12, 2, 0, 0, time.UTC),
		RequestSet:    requestSet,
		Authorization: validNeutralAuthorization(t, sourceScope),
		Generation:    generation,
	})
	require.ErrorContains(t, err, "Source scope instance does not match request set")
}

func TestPackageIsDeterministicContentAddressedAndOfflineVerifiable(t *testing.T) {
	requestSet := validRequestSet(t)
	generation, sourceFactsSHA := validGeneration(t, requestSet)
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x42}, ed25519.SeedSize))
	publicKeys := map[string]ed25519.PublicKey{"source-evidence-test-20260810": privateKey.Public().(ed25519.PublicKey)}
	authorization := validNeutralAuthorization(t, generation.SourceScope)
	options := PackageOptions{
		SigningKeyRef: "source-evidence-test-20260810",
		PublicKeys:    publicKeys,
		PrivateKey:    privateKey,
		CreatedAt:     time.Date(2026, 8, 10, 12, 2, 0, 0, time.UTC),
		RequestSet:    requestSet,
		Authorization: authorization,
		Generation:    generation,
	}

	var first, second bytes.Buffer
	firstManifest, err := Pack(&first, options)
	require.NoError(t, err)
	secondManifest, err := Pack(&second, options)
	require.NoError(t, err)
	require.Equal(t, first.Bytes(), second.Bytes())
	require.Equal(t, firstManifest, secondManifest)
	require.Equal(t, SignaturePurpose, firstManifest.Purpose)
	require.Equal(t, SignatureDomain, firstManifest.Domain)
	require.Equal(t, ProducerContractID, firstManifest.ContractID)
	require.Equal(t, SourceFactsSchemaVersion, firstManifest.ClaimScope)
	require.Equal(t, "complete", firstManifest.ClaimState)
	require.Equal(t, "partial", firstManifest.NestedExecutionEvidenceV2)
	require.Equal(t, sourceFactsSHA, firstManifest.SourceFactsSHA256)
	require.Equal(t, "source-evidence-test-20260810", firstManifest.SigningKeyRef)
	require.Equal(t, signerPublicKeyID(publicKeys[firstManifest.SigningKeyRef]), firstManifest.SignerPublicKeyID)
	require.Equal(t, authorization.AuthorizationManifestSHA256, firstManifest.AuthorizationManifestSHA256)
	require.Equal(t, authorization.AuthorizationStatementSHA256, firstManifest.AuthorizationStatementSHA256)
	require.Equal(t, authorization.AuthorizationSignatureSHA256, firstManifest.AuthorizationSignatureSHA256)
	require.Equal(t, authorization.SourceBindingSHA256, firstManifest.SourceBindingSHA256)

	verification, err := Verify(bytes.NewReader(first.Bytes()), publicKeys)
	require.NoError(t, err)
	require.Equal(t, hashBytes(first.Bytes()), verification.PackageSHA256)
	require.Equal(t, int64(first.Len()), verification.PackageBytes)
	require.Equal(t, requestSet, verification.RequestSet)
	require.Equal(t, generation.SourceScope, verification.SourceScope)
	require.Equal(t, generation.SourceFacts, verification.SourceFacts)
	require.Equal(t, generation.CompatibilityMembers, verification.CompatibilityMembers)

	trustPin, trustSet, err := TrustPin(publicKeys)
	require.NoError(t, err)
	require.Equal(t, SignaturePurpose, trustSet.Purpose)
	require.Equal(t, SignatureDomain, trustSet.Domain)
	require.Equal(t, firstManifest.TrustSetSHA256, trustPin)
	trustSetBody, err := CanonicalJSON(trustSet)
	require.NoError(t, err)
	parsedRing, parsedPin, err := ParseTrustSet(trustSetBody)
	require.NoError(t, err)
	require.Equal(t, publicKeys, parsedRing)
	require.Equal(t, trustPin, parsedPin)
	invalidTrustSet := trustSet
	invalidTrustSet.Keys = append([]TrustSetKey(nil), trustSet.Keys...)
	invalidTrustSet.Keys[0].SignerPublicKeyID = "ed25519:sha256:" + strings.Repeat("0", 64)
	invalidTrustSetBody, err := json.Marshal(invalidTrustSet)
	require.NoError(t, err)
	_, _, err = ParseTrustSet(invalidTrustSetBody)
	require.ErrorContains(t, err, "noncanonical public identity")
	wrongDomainTrustSet := trustSet
	wrongDomainTrustSet.Domain = "unirouter.source-evidence.other-domain.v1"
	wrongDomainTrustSetBody, err := json.Marshal(wrongDomainTrustSet)
	require.NoError(t, err)
	_, _, err = ParseTrustSet(wrongDomainTrustSetBody)
	require.ErrorContains(t, err, "identity is invalid")

	tampered := append([]byte(nil), first.Bytes()...)
	tampered[len(tampered)/2] ^= 0x01
	_, err = Verify(bytes.NewReader(tampered), publicKeys)
	require.Error(t, err)

	_, err = Verify(bytes.NewReader(first.Bytes()[:first.Len()-1]), publicKeys)
	require.Error(t, err, "truncated gzip checksum must fail closed")
	_, err = Verify(bytes.NewReader(append(append([]byte(nil), first.Bytes()...), first.Bytes()...)), publicKeys)
	require.ErrorContains(t, err, "concatenated gzip member")
	_, err = Verify(bytes.NewReader(append(append([]byte(nil), first.Bytes()...), []byte("trailing")...)), publicKeys)
	require.ErrorContains(t, err, "trailing bytes")
	withTarSuffix := packageWithTarSuffix(t, first.Bytes(), []byte("not-part-of-tar"), options.CreatedAt)
	_, err = Verify(bytes.NewReader(withTarSuffix), publicKeys)
	require.ErrorContains(t, err, "bytes after the canonical tar end")
	noncanonicalHeader := append([]byte(nil), first.Bytes()...)
	require.Equal(t, byte(255), noncanonicalHeader[9])
	noncanonicalHeader[9] = 3
	_, err = Verify(bytes.NewReader(noncanonicalHeader), publicKeys)
	require.ErrorContains(t, err, "not the single canonical container representation")

	wrongPrivate := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x24}, ed25519.SeedSize))
	wrongRing := map[string]ed25519.PublicKey{"source-evidence-test-20260810": wrongPrivate.Public().(ed25519.PublicKey)}
	_, err = Verify(bytes.NewReader(first.Bytes()), wrongRing)
	require.Error(t, err)

	wrongProjection := generation
	wrongProjection.CompatibilityMembers = append([]CompatibilityBundle(nil), generation.CompatibilityMembers...)
	wrongProjection.CompatibilityMembers[0].CaptureGaps = append([]CompatibilityGap(nil), wrongProjection.CompatibilityMembers[0].CaptureGaps...)
	wrongProjection.CompatibilityMembers[0].CaptureGaps[0].Reason = "different but otherwise well-formed partial projection"
	options.Generation = wrongProjection
	var rejected bytes.Buffer
	_, err = Pack(&rejected, options)
	require.ErrorContains(t, err, "not the deterministic partial projection")

	invalidManifest := firstManifest
	invalidManifest.Purpose = "upgrade"
	require.Error(t, ValidatePackageManifest(invalidManifest))
	invalidManifest = firstManifest
	invalidManifest.Artifacts = append([]PackageArtifact(nil), firstManifest.Artifacts...)
	invalidManifest.Artifacts[0].MediaType = "application/octet-stream"
	require.Error(t, ValidatePackageManifest(invalidManifest))

	_, _, err = TrustPin(map[string]ed25519.PublicKey{"test-upgrade-20260715-b": publicKeys["source-evidence-test-20260810"]})
	require.ErrorContains(t, err, "invalid key")
	_, _, err = TrustPin(map[string]ed25519.PublicKey{"source-evidence-": publicKeys["source-evidence-test-20260810"]})
	require.ErrorContains(t, err, "invalid key")
	_, _, err = ParseTrustSet([]byte(`{"schema_version":"unirouter.source-evidence.trust-set.v1","purpose":"upgrade","domain":"unirouter.source-evidence.component.v1","keys":[]}`))
	require.Error(t, err)
}

func TestVerifierRejectsPortableContainerNegativeMatrix(t *testing.T) {
	requestSet := validRequestSet(t)
	generation, _ := validGeneration(t, requestSet)
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x42}, ed25519.SeedSize))
	publicKeys := map[string]ed25519.PublicKey{"source-evidence-test-20260810": privateKey.Public().(ed25519.PublicKey)}
	createdAt := time.Date(2026, 8, 10, 12, 2, 0, 0, time.UTC)
	options := PackageOptions{
		SigningKeyRef: "source-evidence-test-20260810",
		PublicKeys:    publicKeys,
		PrivateKey:    privateKey,
		CreatedAt:     createdAt,
		RequestSet:    requestSet,
		Authorization: validNeutralAuthorization(t, generation.SourceScope),
		Generation:    generation,
	}
	var valid bytes.Buffer
	_, err := Pack(&valid, options)
	require.NoError(t, err)
	base := readPackageEntriesForTest(t, valid.Bytes())
	memberIndex := -1
	for index := range base {
		if strings.HasPrefix(base[index].path, "payload/compatibility/") {
			memberIndex = index
			break
		}
	}
	require.NotEqual(t, -1, memberIndex)

	tests := []struct {
		name       string
		mutate     func([]packageContent) []packageContent
		errorMatch string
	}{
		{
			name: "wrong purpose",
			mutate: func(entries []packageContent) []packageContent {
				var manifest PackageManifest
				require.NoError(t, json.Unmarshal(entries[0].body, &manifest))
				manifest.Purpose = "upgrade"
				entries[0].body, err = json.Marshal(manifest)
				require.NoError(t, err)
				return entries
			},
			errorMatch: "signing identity",
		},
		{
			name: "wrong operational key",
			mutate: func(entries []packageContent) []packageContent {
				var signature PackageSignature
				require.NoError(t, json.Unmarshal(entries[1].body, &signature))
				signature.SigningKeyRef = "source-evidence-other"
				entries[1].body, err = json.Marshal(signature)
				require.NoError(t, err)
				return entries
			},
			errorMatch: "does not match manifest",
		},
		{
			name: "manifest tamper",
			mutate: func(entries []packageContent) []packageContent {
				var manifest PackageManifest
				require.NoError(t, json.Unmarshal(entries[0].body, &manifest))
				manifest.SourceFactsSHA256 = "sha256:" + strings.Repeat("0", 64)
				entries[0].body, err = json.Marshal(manifest)
				require.NoError(t, err)
				return entries
			},
			errorMatch: "signature verification failed",
		},
		{
			name: "member tamper",
			mutate: func(entries []packageContent) []packageContent {
				entries[memberIndex].body = append(entries[memberIndex].body, ' ')
				return entries
			},
			errorMatch: "does not match signed identity",
		},
		{
			name: "missing member",
			mutate: func(entries []packageContent) []packageContent {
				return append(entries[:memberIndex], entries[memberIndex+1:]...)
			},
			errorMatch: "artifact count",
		},
		{
			name: "extra member",
			mutate: func(entries []packageContent) []packageContent {
				return append(entries, packageContent{path: "payload/compatibility/999-extra.json", body: []byte(`{}`)})
			},
			errorMatch: "artifact count",
		},
		{
			name: "duplicate member",
			mutate: func(entries []packageContent) []packageContent {
				return append(entries, packageContent{path: entries[memberIndex].path, body: bytes.Clone(entries[memberIndex].body)})
			},
			errorMatch: "duplicate entries",
		},
		{
			name: "path unsafe member",
			mutate: func(entries []packageContent) []packageContent {
				entries[memberIndex].path = "payload/../escape.json"
				return entries
			},
			errorMatch: "unsafe entry",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entries := clonePackageEntries(base)
			entries = test.mutate(entries)
			var invalid bytes.Buffer
			require.NoError(t, writeCanonicalContainer(&invalid, createdAt, entries))
			_, verifyErr := Verify(bytes.NewReader(invalid.Bytes()), publicKeys)
			require.ErrorContains(t, verifyErr, test.errorMatch)
		})
	}
}

func TestContractArtifactsAreStrictJSONAndIdentityBound(t *testing.T) {
	for _, name := range []string{
		"unirouter.integration-control.n1-pin.v1.schema.json",
		"unirouter.release-state-identity.v1.schema.json",
		"unirouter.source-evidence-scope-receipt.v1.schema.json",
		"unirouter.source-evidence.request-set.v1.schema.json",
		"unirouter.source-evidence.source-scope.v1.schema.json",
		"unirouter.source-evidence.runtime-readback.v1.schema.json",
		"unirouter.poc-source-facts.v1.schema.json",
		"unirouter.source-evidence.component-manifest.v1.schema.json",
		"unirouter.source-evidence.component-package.v1.schema.json",
		"unirouter.source-evidence.component-verification-receipt.v1.schema.json",
		"unirouter.source-evidence.component-verifier-release.v1.schema.json",
		"unirouter.source-evidence.detached-signature.v1.schema.json",
		"unirouter.source-evidence.trust-set.v1.schema.json",
		"unirouter.execution-evidence-v2.compatibility-pin.json",
	} {
		body := readContractFixture(t, name)
		var value any
		require.NoErrorf(t, json.Unmarshal(body, &value), "parse %s", name)
	}

	var factsSchema struct {
		Defs map[string]struct {
			Pattern string `json:"pattern"`
		} `json:"$defs"`
	}
	require.NoError(t, json.Unmarshal(readContractFixture(t, "unirouter.poc-source-facts.v1.schema.json"), &factsSchema))
	gitPattern, err := regexp.Compile(factsSchema.Defs["git_object"].Pattern)
	require.NoError(t, err)
	require.True(t, gitPattern.MatchString(strings.Repeat("a", 40)))
	require.False(t, gitPattern.MatchString(strings.Repeat("A", 40)), "JSON Schema must reject noncanonical uppercase Git identity")
	var providerAttemptSchema struct {
		Defs map[string]struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	require.NoError(t, json.Unmarshal(readContractFixture(t, "unirouter.poc-source-facts.v1.schema.json"), &providerAttemptSchema))
	expectedFailureKinds := make([]string, 0, len(exportedProviderFailureKinds)-1)
	for kind := range exportedProviderFailureKinds {
		if kind != "" {
			expectedFailureKinds = append(expectedFailureKinds, kind)
		}
	}
	sort.Strings(expectedFailureKinds)
	require.Equal(t, expectedFailureKinds, providerAttemptSchema.Defs["provider_attempt"].Properties["failure_kind"].Enum)

	var trustSetSchema struct {
		Properties map[string]struct {
			Const string `json:"const"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(readContractFixture(t, "unirouter.source-evidence.trust-set.v1.schema.json"), &trustSetSchema))
	require.Equal(t, SignaturePurpose, trustSetSchema.Properties["purpose"].Const)
	require.Equal(t, SignatureDomain, trustSetSchema.Properties["domain"].Const)

	var verificationReceiptSchema struct {
		Properties map[string]struct {
			Properties map[string]struct {
				Pattern string `json:"pattern"`
			} `json:"properties"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(readContractFixture(t, "unirouter.source-evidence.component-verification-receipt.v1.schema.json"), &verificationReceiptSchema))
	require.Equal(t, `^source-evidence-[A-Za-z0-9._-]{1,48}$`, verificationReceiptSchema.Properties["source_evidence_signing"].Properties["signing_key_ref"].Pattern)

	var retiredSchema struct {
		Deprecated bool           `json:"deprecated"`
		Not        map[string]any `json:"not"`
	}
	require.NoError(t, json.Unmarshal(readContractFixture(t, "unirouter.source-evidence.runtime-readback.v1.schema.json"), &retiredSchema))
	require.True(t, retiredSchema.Deprecated)
	require.NotNil(t, retiredSchema.Not, "not:{} must reject every legacy runtime-readback instance")

	var pin struct {
		BundleSchemaVersion  string `json:"bundle_schema_version"`
		BundleSchemaSHA256   string `json:"bundle_schema_sha256"`
		ProfileID            string `json:"profile_id"`
		ProfileManifestSHA   string `json:"profile_manifest_sha256"`
		ProjectionState      string `json:"required_projection_state"`
		RequiredCaptureGap   string `json:"required_capture_gap"`
		CompleteClaimAllowed bool   `json:"complete_claim_permitted"`
	}
	require.NoError(t, json.Unmarshal(readContractFixture(t, "unirouter.execution-evidence-v2.compatibility-pin.json"), &pin))
	require.Equal(t, CompatibilityBundleSchema, pin.BundleSchemaVersion)
	require.Equal(t, CompatibilityBundleSchemaSHA256, pin.BundleSchemaSHA256)
	require.Equal(t, CompatibilityProfileID, pin.ProfileID)
	require.Equal(t, CompatibilityProfileManifestSHA256, pin.ProfileManifestSHA)
	require.Equal(t, "partial", pin.ProjectionState)
	require.Equal(t, CompatibilityGapRuntimeCloseMarker, pin.RequiredCaptureGap)
	require.False(t, pin.CompleteClaimAllowed)

	protocolPin, err := os.ReadFile(filepath.Join(repositoryRoot(t), "contracts", "foigent", "event-connector-protocol.pin.json"))
	require.NoError(t, err)
	protocol, err := ParseProtocolPin(protocolPin)
	require.NoError(t, err)
	require.Equal(t, "2.0.2", protocol.ProtocolVersion)
	require.Len(t, protocol.Components, 19)
}

func validRequestSet(t *testing.T) RequestSet {
	t.Helper()
	requestSet, err := DecodeRequestSet(readContractFixture(t, "fixtures/request-set.valid.v1.json"))
	require.NoError(t, err)
	return requestSet
}

func validGeneration(t *testing.T, requestSet RequestSet) (Generation, string) {
	t.Helper()
	requestSetSHA, err := SemanticSHA256(requestSet)
	require.NoError(t, err)
	sourceScope := SourceScopeForRequestSet(requestSet)
	sourceBindingSHA, err := SemanticSHA256(sourceScope)
	require.NoError(t, err)
	facts := SourceFacts{
		SchemaVersion:       SourceFactsSchemaVersion,
		GeneratedAt:         "2026-08-10T12:01:00Z",
		RequestSetSHA256:    requestSetSHA,
		SourceBindingSHA256: sourceBindingSHA,
		CampaignID:          requestSet.CampaignID,
		Source: SourceIdentity{
			Product:            "unirouter",
			Instance:           requestSet.SourceInstance,
			IdentityAuthority:  SourceIdentityAuthority,
			RuntimeObservation: SourceRuntimeObservation,
			FeedSnapshotSchema: "unirouter.completed-execution.v1",
		},
		Release: ReleaseFacts{
			ReleaseSequence: 8,
			Version:         "1.1.8-source-evidence",
			Commit:          strings.Repeat("1", 40),
			PackageSHA256:   strings.Repeat("3", 64),
			TrustedKeyID:    "test-upgrade-key",
			RecordedSchema:  "000059_enterprise_access_provisioning",
			CurrentSchema:   "000059_enterprise_access_provisioning",
			RecordedAt:      "2026-08-10T11:59:00Z",
		},
		Protocol: ProtocolFacts{
			PinVersion:      "unirouter.protocol-pin.v1",
			ProtocolVersion: "foigent.event-connector.v1",
			ReleaseDigest:   "sha256:" + strings.Repeat("4", 64),
			PinSHA256:       "sha256:" + strings.Repeat("5", 64),
			Components: []ProtocolComponent{
				{Name: "event_schema", SHA256: "sha256:" + strings.Repeat("6", 64)},
			},
		},
		Snapshot: SnapshotFacts{
			Isolation:             "repeatable read",
			ReadOnly:              true,
			TransactionObservedAt: "2026-08-10T12:00:59Z",
			TransactionIDAssigned: false,
			SelectionMode:         "bounded_tenant_request_set",
		},
		Requests: []RequestFacts{{
			ScenarioID:       requestSet.Requests[0].ScenarioID,
			RequestID:        requestSet.Requests[0].RequestID,
			OrgID:            requestSet.Requests[0].OrgID,
			ProjectID:        requestSet.Requests[0].ProjectID,
			TraceEvents:      []TraceFact{},
			ProviderAttempts: []ProviderAttemptFact{},
			CaptureGaps: []SourceCaptureGap{
				sourceCaptureGapContract["call_log_unavailable"],
				sourceCaptureGapContract["request_trace_unavailable"],
				sourceCaptureGapContract["provider_attempt_unavailable"],
				sourceCaptureGapContract["completed_feed_item_unavailable"],
			},
		}},
		FeedRange: FeedRangeFacts{
			Selection:          "bounded_request_set",
			SelectedItems:      0,
			CursorClaim:        "not_part_of_source_facts",
			HighWatermarkClaim: "not_part_of_source_facts",
		},
		Counts: SourceFactCounts{
			RequestedRequests: 1,
			CaptureGaps:       4,
			TotalFacts:        4,
		},
		Completeness: SourceCompleteness{
			ClaimScope:                    SourceFactsSchemaVersion,
			State:                         "complete",
			BoundedRequestSetComplete:     true,
			ExecutionEvidenceV2Projection: "partial",
			RuntimeCloseMarkerAvailable:   false,
		},
		Safety: SourceSafety{DatabaseMutation: "none"},
	}
	require.NoError(t, ValidateSourceFacts(facts))
	member, err := projectCompatibilityMember(facts, facts.Requests[0])
	require.NoError(t, err)
	factsSHA, err := SemanticSHA256(facts)
	require.NoError(t, err)
	return Generation{SourceScope: sourceScope, SourceFacts: facts, CompatibilityMembers: []CompatibilityBundle{member}}, factsSHA
}

func validNeutralAuthorization(t *testing.T, sourceScope SourceScope) NeutralAuthorizationBinding {
	t.Helper()
	sourceBindingSHA, err := SemanticSHA256(sourceScope)
	require.NoError(t, err)
	return NeutralAuthorizationBinding{
		AuthorizationManifestSHA256:  "sha256:" + strings.Repeat("7", 64),
		AuthorizationStatementSHA256: "sha256:" + strings.Repeat("8", 64),
		AuthorizationSignatureSHA256: "sha256:" + strings.Repeat("9", 64),
		SourceBindingSHA256:          sourceBindingSHA,
	}
}

func packageWithTarSuffix(t *testing.T, packageBody, suffix []byte, createdAt time.Time) []byte {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(packageBody))
	require.NoError(t, err)
	tarBody, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	var result bytes.Buffer
	writer := gzip.NewWriter(&result)
	writer.Name = ""
	writer.Comment = ""
	writer.ModTime = createdAt.UTC()
	writer.OS = 255
	_, err = writer.Write(append(tarBody, suffix...))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return result.Bytes()
}

func readPackageEntriesForTest(t *testing.T, packageBody []byte) []packageContent {
	t.Helper()
	gzipReader, err := gzip.NewReader(bytes.NewReader(packageBody))
	require.NoError(t, err)
	tarReader := tar.NewReader(gzipReader)
	entries := []packageContent{}
	for {
		header, nextErr := tarReader.Next()
		if nextErr == io.EOF {
			break
		}
		require.NoError(t, nextErr)
		body, readErr := io.ReadAll(tarReader)
		require.NoError(t, readErr)
		entries = append(entries, packageContent{path: header.Name, body: body})
	}
	require.NoError(t, gzipReader.Close())
	return entries
}

func clonePackageEntries(entries []packageContent) []packageContent {
	cloned := make([]packageContent, len(entries))
	for index, entry := range entries {
		cloned[index] = packageContent{path: entry.path, mediaType: entry.mediaType, body: bytes.Clone(entry.body)}
	}
	return cloned
}

func validCompatibilityMember(t *testing.T) CompatibilityBundle {
	t.Helper()
	member, err := DecodeCompatibilityBundle(readContractFixture(t, "fixtures/execution-evidence.partial.valid.v2.json"))
	require.NoError(t, err)
	return member
}

func readContractFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join(repositoryRoot(t), "contracts", "source-evidence", "v1", filepath.FromSlash(name))
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	return body
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
