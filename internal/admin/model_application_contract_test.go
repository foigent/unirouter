package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCanonicalModelApplicationViewerProjectionRemovesCredentialIdentities(t *testing.T) {
	bindingID, testedBy := int64(71), int64(81)
	fixture := func() canonicalModelApplicationProjection {
		return canonicalModelApplicationProjection{Applications: []canonicalModelApplication{{
			Credential: canonicalCredentialAxis{
				State: "ready", BoundCount: 2, EligibleCount: 1,
				Bindings: []canonicalCredentialBindingRef{{ID: bindingID}},
			},
			Operations: []canonicalOperationAxis{{LatestProbe: &canonicalOperationProbeView{
				ProbeID: 91, CredentialBindingID: &bindingID, TestedBy: &testedBy,
			}}},
		}}}
	}

	viewer := fixture()
	viewerContext := context.WithValue(context.Background(), middleware.CtxOrganizationRole, auth.RoleOrgViewer)
	redactCanonicalModelApplicationViewerProjection(viewerContext, &viewer)
	if len(viewer.Applications[0].Credential.Bindings) != 0 ||
		viewer.Applications[0].Credential.BoundCount != 2 ||
		viewer.Applications[0].Credential.EligibleCount != 1 {
		t.Fatalf("viewer credential aggregate/redaction mismatch: %+v", viewer.Applications[0].Credential)
	}
	probe := viewer.Applications[0].Operations[0].LatestProbe
	if probe == nil || probe.ProbeID != 91 || probe.CredentialBindingID != nil || probe.TestedBy != nil {
		t.Fatalf("viewer probe identity redaction mismatch: %+v", probe)
	}

	admin := fixture()
	adminContext := context.WithValue(context.Background(), middleware.CtxOrganizationRole, auth.RoleOrgAdmin)
	redactCanonicalModelApplicationViewerProjection(adminContext, &admin)
	if len(admin.Applications[0].Credential.Bindings) != 1 ||
		admin.Applications[0].Operations[0].LatestProbe.CredentialBindingID == nil ||
		admin.Applications[0].Operations[0].LatestProbe.TestedBy == nil {
		t.Fatalf("admin projection was unexpectedly redacted: %+v", admin.Applications[0])
	}
}

func TestCanonicalQualificationOriginGateUsesEffectiveConnectionBeforeWrites(t *testing.T) {
	t.Parallel()
	baseRequest := canonicalModelApplicationCreateRequest{
		Connection: canonicalConnectionInput{
			Name: "Qualification", Provider: "openai", BaseURL: "https://api.example.com/v1",
			AdapterType: adapters.TypeOpenAICompatible, AdapterConfig: json.RawMessage(`{}`),
		},
		PublicModel: canonicalPublicModelInput{ModelName: "qualification-model"},
		Deployment:  canonicalDeploymentInput{UpstreamModel: "qualification-upstream"},
		Credential:  canonicalCredentialInput{APIKey: "qualification-secret"},
		Operation:   canonicalOperationInput{OperationID: string(modeloperation.IDResponses)},
	}
	normalized, err := normalizeCanonicalApplicationCreate(baseRequest)
	if err != nil {
		t.Fatalf("normalize qualification request: %v", err)
	}
	queryCalls := 0
	tx := &testutil.MockTx{QueryRowFn: func(_ context.Context, _ string, _ ...any) pgx.Row {
		queryCalls++
		return &testutil.MockRow{ScanFn: func(dest ...any) error {
			*(dest[0].(*string)) = "https://saved.example.com/provider/v1"
			return nil
		}}
	}}
	if err := validateCanonicalQualificationOrigin(context.Background(), tx, 11, normalized); !errors.Is(err, errCanonicalApplicationQualificationOriginInvalid) {
		t.Fatalf("new pathful Responses origin error=%v want invalid", err)
	}
	if queryCalls != 0 {
		t.Fatalf("new connection origin gate performed %d database reads", queryCalls)
	}

	existing := normalized
	existing.Connection = canonicalConnectionInput{ID: 91}
	if err := validateCanonicalQualificationOrigin(context.Background(), tx, 11, existing); !errors.Is(err, errCanonicalApplicationQualificationOriginConflict) {
		t.Fatalf("existing pathful Responses origin error=%v want conflict", err)
	}
	if queryCalls != 1 {
		t.Fatalf("existing connection origin gate reads=%d want 1", queryCalls)
	}

	existing.Operation.OperationID = string(modeloperation.IDMessages)
	if err := validateCanonicalQualificationOrigin(context.Background(), tx, 11, existing); !errors.Is(err, errCanonicalApplicationQualificationOriginConflict) {
		t.Fatalf("existing pathful Messages origin error=%v want conflict", err)
	}
	if queryCalls != 2 {
		t.Fatalf("Messages connection origin gate reads=%d want 2", queryCalls)
	}

	pathFree := normalized
	pathFree.Connection.BaseURL = " https://API.Example.com:443/ "
	if err := validateCanonicalQualificationOrigin(context.Background(), tx, 11, pathFree); err != nil {
		t.Fatalf("path-free Responses origin rejected: %v", err)
	}
	if queryCalls != 2 {
		t.Fatalf("new path-free connection origin gate performed %d database reads", queryCalls)
	}

	chat := normalized
	chat.Operation.OperationID = string(modeloperation.IDChatCompletions)
	if err := validateCanonicalQualificationOrigin(context.Background(), tx, 11, chat); err != nil {
		t.Fatalf("legacy path-owning Chat base URL rejected: %v", err)
	}
	if queryCalls != 2 {
		t.Fatalf("Chat qualification bypass performed %d database reads", queryCalls)
	}
}

func TestCanonicalPublicModelNameIsImmutableFromCreation(t *testing.T) {
	t.Parallel()
	if err := validateCanonicalPublicModelRename("stable-model", "stable-model"); err != nil {
		t.Fatalf("unchanged identity rejected: %v", err)
	}
	if err := validateCanonicalPublicModelRename("stable-model", " stable-model "); err != nil {
		t.Fatalf("trim-equivalent identity rejected: %v", err)
	}
	if err := validateCanonicalPublicModelRename("stable-model", "renamed-model"); !errors.Is(err, errCanonicalApplicationConflict) {
		t.Fatalf("rename error=%v want conflict", err)
	}
	if err := validateCanonicalPublicModelRename("stable-model", ""); !errors.Is(err, errCanonicalApplicationInvalid) {
		t.Fatalf("invalid identity error=%v want invalid", err)
	}
}

func TestCanonicalModelApplicationCreateRejectsDuplicateMembersBeforeSideEffects(t *testing.T) {
	t.Parallel()
	beginCalls := 0
	database := &testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) {
		beginCalls++
		return &testutil.MockTx{}, nil
	}}
	handler := NewHandlerWithAPIKeyHashSecret(
		database, "duplicate-member-jwt", "duplicate-member-hmac",
		[]byte("0123456789abcdef01234567"), nil,
	)
	baseSuffix := `,
  "public_model":{"model_name":"duplicate-model"},
  "deployment":{"upstream_model":"duplicate-upstream"},
  "credential":{"api_key":"duplicate-secret"},
  "operation":{"operation_id":"openai.chat_completions.v1"}
}`
	for name, body := range map[string]string{
		"top-level": `{
  "connection":{"name":"a"},
  "connection":{"name":"b"}` + baseSuffix,
		"nested credential": `{
  "connection":{"name":"a","provider":"openai","base_url":"https://duplicate.example/v1","adapter_type":"openai_compatible","adapter_config":{}}` + `,
  "public_model":{"model_name":"duplicate-model"},
  "deployment":{"upstream_model":"duplicate-upstream"},
  "credential":{"api_key":"duplicate-secret-a","api_key":"duplicate-secret-b"},
  "operation":{"operation_id":"openai.chat_completions.v1"}
}`,
		"adapter config": `{
  "connection":{"name":"a","provider":"openai","base_url":"https://duplicate.example/v1","adapter_type":"openai_compatible","adapter_config":{"n":1,"n":2}}` + baseSuffix,
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/model-deployments", strings.NewReader(body))
			request.Header.Set(middleware.IdempotencyKeyHeader, "duplicate-member-key")
			ctx := context.WithValue(request.Context(), middleware.CtxOrgID, int64(1))
			ctx = context.WithValue(ctx, middleware.CtxUserID, int64(1))
			response := httptest.NewRecorder()
			middleware.OperationIdentityWithSecret(handler.apiKeyHashSecret)(
				http.HandlerFunc(handler.CreateCanonicalModelApplication),
			).ServeHTTP(response, request.WithContext(ctx))
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "canonical_application_invalid") {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	if beginCalls != 0 {
		t.Fatalf("duplicate members reached database transaction %d times", beginCalls)
	}
}

func TestNormalizeCanonicalApplicationCreateStrictlyCanonicalizesAdapterConfig(t *testing.T) {
	t.Parallel()
	if canonicalModelApplicationMaxRequestBytes <= canonicalModelApplicationMaxAdapterConfigBytes {
		t.Fatalf("request limit %d must leave envelope space around the %d-byte adapter_config contract",
			canonicalModelApplicationMaxRequestBytes, canonicalModelApplicationMaxAdapterConfigBytes)
	}
	base := canonicalModelApplicationCreateRequest{
		Connection: canonicalConnectionInput{
			Name: "Precise config", Provider: "openai", BaseURL: "https://precise.example/v1",
			AdapterType: adapters.TypeOpenAICompatible,
		},
		PublicModel: canonicalPublicModelInput{ModelName: "precise-model"},
		Deployment:  canonicalDeploymentInput{UpstreamModel: "precise-upstream"},
		Credential:  canonicalCredentialInput{APIKey: "precise-secret"},
		Operation:   canonicalOperationInput{OperationID: string(modeloperation.IDChatCompletions)},
	}
	left := base
	left.Connection.AdapterConfig = json.RawMessage(`{"z":900719925474099312345678901234567890,"nested":{"b":2,"a":1}}`)
	right := base
	right.Connection.AdapterConfig = json.RawMessage(` { "nested" : { "a" : 1, "b" : 2 }, "z" : 900719925474099312345678901234567890 } `)
	normalizedLeft, err := normalizeCanonicalApplicationCreate(left)
	if err != nil {
		t.Fatalf("normalize left: %v", err)
	}
	normalizedRight, err := normalizeCanonicalApplicationCreate(right)
	if err != nil {
		t.Fatalf("normalize right: %v", err)
	}
	if string(normalizedLeft.Connection.AdapterConfig) != string(normalizedRight.Connection.AdapterConfig) {
		t.Fatalf("canonical configs differ: %s != %s", normalizedLeft.Connection.AdapterConfig, normalizedRight.Connection.AdapterConfig)
	}
	if !strings.Contains(string(normalizedLeft.Connection.AdapterConfig), "900719925474099312345678901234567890") {
		t.Fatalf("large integer lost precision: %s", normalizedLeft.Connection.AdapterConfig)
	}
	leftFingerprint, err := canonicalModelApplicationFingerprint(normalizedLeft, "canonical-config-secret")
	if err != nil {
		t.Fatalf("fingerprint left: %v", err)
	}
	rightFingerprint, err := canonicalModelApplicationFingerprint(normalizedRight, "canonical-config-secret")
	if err != nil {
		t.Fatalf("fingerprint right: %v", err)
	}
	if leftFingerprint != rightFingerprint {
		t.Fatalf("object ordering changed fingerprint: %s != %s", leftFingerprint, rightFingerprint)
	}

	for _, raw := range []string{
		`{"a":1,"a":2}`,
		`{"nested":{"a":1,"a":2}}`,
		`[]`,
		`null`,
		`42`,
		`"config"`,
	} {
		candidate := base
		candidate.Connection.AdapterConfig = json.RawMessage(raw)
		if _, err := normalizeCanonicalApplicationCreate(candidate); !errors.Is(err, errCanonicalApplicationInvalid) {
			t.Fatalf("adapter_config %s error=%v want invalid", raw, err)
		}
	}
	oversized := base
	oversized.Connection.AdapterConfig = json.RawMessage(`{"padding":"` + strings.Repeat("a", canonicalModelApplicationMaxAdapterConfigBytes) + `"}`)
	if _, err := normalizeCanonicalApplicationCreate(oversized); !errors.Is(err, errCanonicalApplicationInvalid) {
		t.Fatalf("oversized adapter_config error=%v want invalid", err)
	}
}

func TestCanonicalJSONEqualPreservesNumberPrecisionAndObjectSemantics(t *testing.T) {
	t.Parallel()
	if !canonicalJSONEqual(
		[]byte(`{"z":900719925474099312345678901234567890,"nested":{"b":2,"a":1}}`),
		[]byte(` { "nested": { "a": 1, "b": 2 }, "z": 900719925474099312345678901234567890 } `),
	) {
		t.Fatal("semantically equal objects with precise integers were rejected")
	}
	if canonicalJSONEqual(
		[]byte(`{"value":9007199254740992}`),
		[]byte(`{"value":9007199254740993}`),
	) {
		t.Fatal("distinct integers above float64 precision were treated as equal")
	}
	if canonicalJSONEqual([]byte(`{"a":1,"a":1}`), []byte(`{"a":1}`)) {
		t.Fatal("duplicate-member JSON was treated as an equal canonical object")
	}
}

func TestCanonicalModelApplicationFingerprintIncludesSecretWithoutExposingIt(t *testing.T) {
	t.Parallel()
	request := canonicalModelApplicationCreateRequest{
		Connection: canonicalConnectionInput{
			Name: "Fingerprint", Provider: "openai", BaseURL: "https://fingerprint.example/v1",
			AdapterType: adapters.TypeOpenAICompatible, AdapterConfig: json.RawMessage(`{}`),
		},
		PublicModel: canonicalPublicModelInput{ModelName: "fingerprint-model"},
		Deployment:  canonicalDeploymentInput{UpstreamModel: "fingerprint-upstream"},
		Credential:  canonicalCredentialInput{APIKey: "provider-secret-a"},
		Operation:   canonicalOperationInput{OperationID: string(modeloperation.IDChatCompletions)},
	}
	normalized, err := normalizeCanonicalApplicationCreate(request)
	if err != nil {
		t.Fatal(err)
	}
	first, err := canonicalModelApplicationFingerprint(normalized, "fingerprint-hmac-secret")
	if err != nil {
		t.Fatal(err)
	}
	explicitDefaults := normalized
	explicitDefaults.PublicModel.ProviderFamily = "openai"
	explicitDefaults.PublicModel.ContextLength = canonicalApplicationIntPointer(0)
	explicitDefaults.PublicModel.OutputLimit = canonicalApplicationIntPointer(0)
	explicitDefaults.PublicModel.ApprovedCatalog = canonicalApplicationBoolPointer(false)
	explicitDefaults.Deployment.Priority = canonicalApplicationIntPointer(100)
	explicitDefaults.Deployment.RoutingWeight = canonicalApplicationIntPointer(100)
	explicitDefaults.Deployment.MaxContext = canonicalApplicationIntPointer(0)
	explicitDefaults.Deployment.DefaultMaxOutput = canonicalApplicationIntPointer(0)
	explicitDefaults.Deployment.DegradationThreshold = canonicalApplicationIntPointer(10)
	explicitFingerprint, err := canonicalModelApplicationFingerprint(explicitDefaults, "fingerprint-hmac-secret")
	if err != nil {
		t.Fatal(err)
	}
	if first != explicitFingerprint {
		t.Fatalf("fixed create defaults changed fingerprint: %s != %s", first, explicitFingerprint)
	}
	normalized.Credential.APIKey = "provider-secret-b"
	second, err := canonicalModelApplicationFingerprint(normalized, "fingerprint-hmac-secret")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("different provider secrets produced the same request fingerprint")
	}
	if strings.Contains(first, "provider-secret") || !strings.HasPrefix(first, "hmac-sha256:") || len(first) != 76 {
		t.Fatalf("unsafe or malformed fingerprint %q", first)
	}
}

func TestCanonicalModelApplicationCreateRetriesOnlyOneFreshSerializableTransaction(t *testing.T) {
	t.Parallel()
	request := canonicalModelApplicationCreateRequest{
		Connection: canonicalConnectionInput{
			Name: "Retry", Provider: "openai", BaseURL: "https://retry.example/v1",
			AdapterType: adapters.TypeOpenAICompatible, AdapterConfig: json.RawMessage(`{}`),
		},
		PublicModel: canonicalPublicModelInput{ModelName: "retry-model"},
		Deployment:  canonicalDeploymentInput{UpstreamModel: "retry-upstream"},
		Credential:  canonicalCredentialInput{APIKey: "retry-secret"},
		Operation:   canonicalOperationInput{OperationID: string(modeloperation.IDChatCompletions)},
	}
	for _, test := range []struct {
		name       string
		code       string
		wantBegins int
	}{
		{name: "serialization failure", code: "40001", wantBegins: 2},
		{name: "deadlock", code: "40P01", wantBegins: 2},
		{name: "non retryable database error", code: "23505", wantBegins: 1},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			beginCalls := 0
			database := &testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) {
				beginCalls++
				return nil, &pgconn.PgError{Code: test.code, Message: "injected transaction begin failure"}
			}}
			handler := NewHandlerWithAPIKeyHashSecret(
				database, "retry-jwt", "retry-hmac", []byte("0123456789abcdef01234567"), nil,
			)
			raw := httptest.NewRequest(http.MethodPost, "/api/v1/model-deployments", nil)
			raw.Header.Set(middleware.IdempotencyKeyHeader, "retry-key")
			ctx := context.WithValue(raw.Context(), middleware.CtxOrgID, int64(17))
			ctx = context.WithValue(ctx, middleware.CtxUserID, int64(23))
			var identified *http.Request
			middleware.OperationIdentityWithSecret(handler.apiKeyHashSecret)(http.HandlerFunc(
				func(_ http.ResponseWriter, request *http.Request) { identified = request },
			)).ServeHTTP(httptest.NewRecorder(), raw.WithContext(ctx))
			if identified == nil {
				t.Fatal("operation identity middleware did not forward request")
			}
			if _, err := handler.createCanonicalModelApplication(
				identified.Context(), 17, request, requestAuditMeta(identified, 17, 0),
			); err == nil {
				t.Fatal("injected transaction failure unexpectedly succeeded")
			}
			if beginCalls != test.wantBegins {
				t.Fatalf("Begin calls=%d want %d", beginCalls, test.wantBegins)
			}
		})
	}
	if isCanonicalCreateTransactionRetryable(errCanonicalApplicationIdempotencyConflict) {
		t.Fatal("typed idempotency conflict must never be transaction-retried")
	}
}

func TestResolveCanonicalOperationProfileUsesOnlyServerRegisteredTemplates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		operation   modeloperation.ID
		adapter     string
		wantID      string
		wantVersion string
		wantError   bool
	}{
		{name: "openai chat", operation: modeloperation.IDChatCompletions, adapter: adapters.TypeOpenAICompatible, wantID: "openai.chat_completions", wantVersion: "1.0.0"},
		{name: "azure chat", operation: modeloperation.IDChatCompletions, adapter: adapters.TypeAzureOpenAI, wantID: "openai.chat_completions", wantVersion: "1.0.0"},
		{name: "openai embeddings", operation: modeloperation.IDEmbeddings, adapter: adapters.TypeOpenAICompatible, wantID: "openai.embeddings", wantVersion: "1.0.0"},
		{name: "responses exact registry profile", operation: modeloperation.IDResponses, adapter: adapters.TypeOpenAICompatible, wantID: endpointprotocol.ProfileOpenAIResponsesText, wantVersion: "1.0.0"},
		{name: "messages exact registry profile", operation: modeloperation.IDMessages, adapter: adapters.TypeAnthropicNative, wantID: endpointprotocol.ProfileAnthropicMessagesText, wantVersion: "1.0.0"},
		{name: "anthropic adapter cannot claim openai chat", operation: modeloperation.IDChatCompletions, adapter: adapters.TypeAnthropicNative, wantError: true},
		{name: "openai adapter cannot claim anthropic messages", operation: modeloperation.IDMessages, adapter: adapters.TypeOpenAICompatible, wantError: true},
		{name: "azure responses template not registered", operation: modeloperation.IDResponses, adapter: adapters.TypeAzureOpenAI, wantError: true},
		{name: "models list is not a provider deployment", operation: modeloperation.IDModelsList, adapter: adapters.TypeOpenAICompatible, wantError: true},
		{name: "media jobs need their own provider template", operation: modeloperation.IDMediaJobs, adapter: adapters.TypeOpenAICompatible, wantError: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result, err := resolveCanonicalOperationProfile(canonicalOperationInput{OperationID: string(test.operation)}, test.adapter)
			if test.wantError {
				if err == nil {
					t.Fatalf("expected unsupported template to fail, got %+v", result)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolve template: %v", err)
			}
			if result.ProtocolProfileID != test.wantID || result.ProtocolProfileVersion != test.wantVersion {
				t.Fatalf("profile=%s/%s want %s/%s", result.ProtocolProfileID, result.ProtocolProfileVersion, test.wantID, test.wantVersion)
			}
		})
	}
}
