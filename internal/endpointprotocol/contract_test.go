package endpointprotocol

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestRegistryIsFixedAndDescriptorsAreDefensive(t *testing.T) {
	registry := NewRegistry()
	descriptors := registry.Descriptors()
	if len(descriptors) != 3 {
		t.Fatalf("descriptors=%d want 3", len(descriptors))
	}
	want := []string{
		ProfileOpenAIResponsesText,
		ProfileCohereRerankV2,
		ProfileOpenAIImageGPT,
	}
	for index, descriptor := range descriptors {
		if descriptor.ContractVersion != ContractVersion || descriptor.ProfileID != want[index] {
			t.Fatalf("descriptor[%d]=%+v", index, descriptor)
		}
		if descriptor.CustomerChargeState != ChargeObservedOnly {
			t.Fatalf("profile became charge authority: %+v", descriptor)
		}
	}
	if descriptors[2].RuntimeState != RuntimeDecisionGate {
		t.Fatalf("image runtime bypassed P7-D03: %+v", descriptors[2])
	}
	descriptors[0].SupportedModalities[0] = "mutated"
	if NewRegistry().Descriptors()[0].SupportedModalities[0] != "text" {
		t.Fatal("descriptor slice was not defensively copied")
	}
	if _, err := registry.Get("vendor.dynamic"); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("unknown profile error=%v", err)
	}
}

func TestOpenAIResponsesTextProfileStrictRequestAndFinalUsage(t *testing.T) {
	profile := mustProfile(t, ProfileOpenAIResponsesText)
	request, binding := mustBuild(t, profile, BuildInput{
		BaseURL:        "https://api.openai.com",
		APIKey:         "test-secret",
		RequestID:      "request-123",
		RequestedModel: "company-text",
		MappedModel:    "gpt-5.2",
		Body: []byte(`{
			"model":"company-text",
			"input":"private prompt",
			"instructions":"answer briefly",
			"max_output_tokens":128,
			"temperature":0.5
		}`),
	})
	if request.Method != http.MethodPost || request.URL.String() != "https://api.openai.com/v1/responses" {
		t.Fatalf("request target=%s %s", request.Method, request.URL)
	}
	if request.Header.Get("Authorization") != "Bearer test-secret" ||
		request.Header.Get("X-Client-Request-Id") != "request-123" {
		t.Fatalf("headers=%v", request.Header)
	}
	wire := readJSONObject(t, request)
	if wire["model"] != "gpt-5.2" || wire["store"] != false || wire["stream"] != false {
		t.Fatalf("unsafe responses wire body=%v", wire)
	}

	outcome, err := profile.ParseResponse(context.Background(), binding, http.StatusOK, []byte(`{
		"id":"resp_123",
		"object":"response",
		"status":"completed",
		"model":"gpt-5.2",
		"error":null,
		"incomplete_details":null,
		"output":[{
			"id":"msg_123",
			"type":"message",
			"status":"completed",
			"role":"assistant",
			"content":[{"type":"output_text","text":"safe output","annotations":[]}]
		}],
		"usage":{
			"input_tokens":12,
			"input_tokens_details":{"cached_tokens":4,"cache_write_tokens":2},
			"output_tokens":3,
			"total_tokens":15
		}
	}`))
	if err != nil {
		t.Fatalf("ParseResponse failed: %v", err)
	}
	if outcome.ProviderObject != "resp_123" || outcome.OutputCount != 1 {
		t.Fatalf("outcome=%+v", outcome)
	}
	wantUnits := []UnitObservation{
		observedUnit(UnitInputTokens, "12"),
		observedUnit(UnitOutputTokens, "3"),
		observedUnit(UnitCacheReadTokens, "4"),
		observedUnit(UnitCacheWriteTokens, "2"),
	}
	if !reflect.DeepEqual(outcome.Units, wantUnits) {
		t.Fatalf("units=%+v want %+v", outcome.Units, wantUnits)
	}
}

func TestOpenAIResponsesRejectsSemanticDegradationAndNonFinalEvidence(t *testing.T) {
	profile := mustProfile(t, ProfileOpenAIResponsesText)
	tests := []struct {
		name string
		body string
		kind error
	}{
		{
			name: "tools",
			body: `{"model":"company-text","input":"secret","tools":[{"type":"web_search"}]}`,
			kind: ErrUnsupportedSemantics,
		},
		{
			name: "structured input",
			body: `{"model":"company-text","input":[{"role":"user","content":"secret"}]}`,
			kind: ErrUnsupportedSemantics,
		},
		{
			name: "stored state",
			body: `{"model":"company-text","input":"secret","store":true}`,
			kind: ErrUnsupportedSemantics,
		},
		{
			name: "stream",
			body: `{"model":"company-text","input":"secret","stream":true}`,
			kind: ErrUnsupportedSemantics,
		},
		{
			name: "unknown field",
			body: `{"model":"company-text","input":"secret","vendor_magic":true}`,
			kind: ErrInvalidRequest,
		},
		{
			name: "nested duplicate",
			body: `{"model":"company-text","input":"secret","meta":{"a":1,"a":2}}`,
			kind: ErrInvalidJSON,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := profile.BuildRequest(context.Background(), BuildInput{
				BaseURL:        "https://api.openai.com",
				APIKey:         "top-secret",
				RequestID:      "req",
				RequestedModel: "company-text",
				MappedModel:    "gpt-5.2",
				Body:           []byte(test.body),
			})
			if !errors.Is(err, test.kind) {
				t.Fatalf("error=%v want kind %v", err, test.kind)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("safe error leaked request content: %v", err)
			}
		})
	}

	_, binding := mustBuild(t, profile, baseResponsesInput())
	_, err := profile.ParseResponse(context.Background(), binding, http.StatusOK, []byte(`{
		"id":"resp_pending",
		"object":"response",
		"status":"in_progress",
		"model":"gpt-5.2",
		"output":[],
		"usage":null
	}`))
	if !errors.Is(err, ErrNonFinalResponse) {
		t.Fatalf("non-final error=%v", err)
	}
	_, err = profile.ParseResponse(context.Background(), binding, http.StatusOK, []byte(`{
		"id":"resp_tool",
		"object":"response",
		"status":"completed",
		"model":"gpt-5.2",
		"error":null,
		"incomplete_details":null,
		"output":[{"type":"function_call","status":"completed"}],
		"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}
	}`))
	if !errors.Is(err, ErrUnsupportedSemantics) {
		t.Fatalf("tool output error=%v", err)
	}
}

func TestCohereRerankV2ValidatesOrderingIdentityAndSearchUnits(t *testing.T) {
	profile := mustProfile(t, ProfileCohereRerankV2)
	request, binding := mustBuild(t, profile, BuildInput{
		BaseURL:        "https://api.cohere.com",
		APIKey:         "rerank-secret",
		RequestID:      "not-forwarded",
		RequestedModel: "company-rerank",
		MappedModel:    "rerank-v3.5",
		Body: []byte(`{
			"model":"company-rerank",
			"query":"best enterprise router",
			"documents":["document one","document two","document three"],
			"top_n":2,
			"priority":0
		}`),
	})
	if request.URL.String() != "https://api.cohere.com/v2/rerank" ||
		request.Header.Get("X-Client-Request-Id") != "" {
		t.Fatalf("rerank request=%s headers=%v", request.URL, request.Header)
	}
	if readJSONObject(t, request)["model"] != "rerank-v3.5" {
		t.Fatal("rerank model was not mapped")
	}
	outcome, err := profile.ParseResponse(context.Background(), binding, http.StatusOK, []byte(`{
		"id":"rerank-123",
		"results":[
			{"index":2,"relevance_score":0.999999999999999999},
			{"index":0,"relevance_score":0.5}
		],
		"meta":{
			"api_version":{"version":"2","is_deprecated":false,"is_experimental":false},
			"billed_units":{"search_units":1}
		}
	}`))
	if err != nil {
		t.Fatalf("ParseResponse failed: %v", err)
	}
	if outcome.OutputCount != 2 || !reflect.DeepEqual(
		outcome.Units,
		[]UnitObservation{observedUnit(UnitSearchUnits, "1")},
	) {
		t.Fatalf("outcome=%+v", outcome)
	}

	badBodies := []string{
		`{"id":"x","results":[{"index":0,"relevance_score":0.1},{"index":1,"relevance_score":0.2}],"meta":{"api_version":{"version":"2"},"billed_units":{"search_units":1}}}`,
		`{"id":"x","results":[{"index":0,"relevance_score":0.9},{"index":0,"relevance_score":0.8}],"meta":{"api_version":{"version":"2"},"billed_units":{"search_units":1}}}`,
		`{"id":"x","results":[{"index":0,"relevance_score":0.9},{"index":1,"relevance_score":0.8}],"meta":{"api_version":{"version":"2"},"billed_units":{"search_units":1e0}}}`,
	}
	for _, body := range badBodies {
		if _, err := profile.ParseResponse(context.Background(), binding, http.StatusOK, []byte(body)); !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("invalid rerank response accepted: %s error=%v", body, err)
		}
	}
}

func TestCohereRerankRejectsDocumentObjects(t *testing.T) {
	profile := mustProfile(t, ProfileCohereRerankV2)
	_, _, err := profile.BuildRequest(context.Background(), BuildInput{
		BaseURL:        "https://api.cohere.com",
		APIKey:         "secret",
		RequestedModel: "company-rerank",
		MappedModel:    "rerank-v3.5",
		Body: []byte(`{
			"model":"company-rerank",
			"query":"q",
			"documents":[{"text":"document"}]
		}`),
	})
	if !errors.Is(err, ErrUnsupportedSemantics) {
		t.Fatalf("document object error=%v", err)
	}
}

func TestOpenAIImageProfileIsDecisionGatedAndValidatesInlineOutput(t *testing.T) {
	profile := mustProfile(t, ProfileOpenAIImageGPT)
	request, binding := mustBuild(t, profile, BuildInput{
		BaseURL:        "https://api.openai.com",
		APIKey:         "image-secret",
		RequestID:      "image-request",
		RequestedModel: "company-image",
		MappedModel:    "gpt-image-1",
		Body: []byte(`{
			"model":"company-image",
			"prompt":"private image prompt",
			"size":"1024x1024",
			"output_format":"png"
		}`),
	})
	wire := readJSONObject(t, request)
	if wire["model"] != "gpt-image-1" || wire["n"] != float64(1) ||
		wire["stream"] != false {
		t.Fatalf("unsafe image wire body=%v", wire)
	}
	outcome, err := profile.ParseResponse(context.Background(), binding, http.StatusOK, []byte(`{
		"created":123,
		"data":[{"b64_json":"aW1hZ2U="}],
		"usage":{
			"input_tokens":10,
			"input_tokens_details":{"text_tokens":7,"image_tokens":3},
			"output_tokens":20,
			"total_tokens":30
		}
	}`))
	if err != nil {
		t.Fatalf("ParseResponse failed: %v", err)
	}
	if outcome.OutputCount != 1 || outcome.ProviderObject != "" ||
		len(outcome.Units) != 4 {
		t.Fatalf("outcome=%+v", outcome)
	}

	badBodies := []string{
		`{"created":1,"data":[{"url":"https://provider.example/image"}],"usage":{"input_tokens":1,"input_tokens_details":{"text_tokens":1,"image_tokens":0},"output_tokens":1,"total_tokens":2}}`,
		`{"created":1,"data":[{"b64_json":"not base64"}],"usage":{"input_tokens":1,"input_tokens_details":{"text_tokens":1,"image_tokens":0},"output_tokens":1,"total_tokens":2}}`,
		`{"created":1,"data":[{"b64_json":"aQ=="}],"usage":{"input_tokens":2,"input_tokens_details":{"text_tokens":1,"image_tokens":0},"output_tokens":1,"total_tokens":3}}`,
	}
	for _, body := range badBodies {
		if _, err := profile.ParseResponse(context.Background(), binding, http.StatusOK, []byte(body)); err == nil {
			t.Fatalf("invalid image response accepted: %s", body)
		}
	}
}

func TestOpenAIImageRejectsRuntimeAndOutputSemantics(t *testing.T) {
	profile := mustProfile(t, ProfileOpenAIImageGPT)
	for _, body := range []string{
		`{"model":"company-image","prompt":"secret","n":2}`,
		`{"model":"company-image","prompt":"secret","stream":true}`,
		`{"model":"company-image","prompt":"secret","response_format":"url"}`,
	} {
		_, _, err := profile.BuildRequest(context.Background(), BuildInput{
			BaseURL:        "https://api.openai.com",
			APIKey:         "secret-key",
			RequestID:      "request",
			RequestedModel: "company-image",
			MappedModel:    "gpt-image-1",
			Body:           []byte(body),
		})
		if !errors.Is(err, ErrUnsupportedSemantics) {
			t.Fatalf("unsupported image request error=%v", err)
		}
		if strings.Contains(err.Error(), "secret") {
			t.Fatalf("safe error leaked content: %v", err)
		}
	}
}

func TestStatusClassificationMatchesExistingFailoverClasses(t *testing.T) {
	profile := mustProfile(t, ProfileOpenAIResponsesText)
	tests := []struct {
		status    int
		kind      string
		retryable bool
	}{
		{http.StatusBadRequest, "client_request", false},
		{http.StatusUnauthorized, "auth", false},
		{http.StatusRequestTimeout, "channel_failure", true},
		{http.StatusTooManyRequests, "rate_limit", true},
		{http.StatusBadGateway, "channel_failure", true},
		{599, "unknown", false},
	}
	for _, test := range tests {
		kind, retryable := profile.ClassifyHTTPStatus(test.status)
		if kind != test.kind || retryable != test.retryable {
			t.Fatalf("status %d => %s/%t want %s/%t", test.status, kind, retryable, test.kind, test.retryable)
		}
	}
}

func TestConfigurationAndBindingFailClosed(t *testing.T) {
	profile := mustProfile(t, ProfileOpenAIResponsesText)
	input := baseResponsesInput()
	input.BaseURL = "http://api.openai.com/path?secret=x"
	if _, _, err := profile.BuildRequest(context.Background(), input); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("unsafe base URL error=%v", err)
	}
	input = baseResponsesInput()
	input.RequestID = strings.Repeat("x", maxOpenAIRequestIDLen+1)
	if _, _, err := profile.BuildRequest(context.Background(), input); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("oversized request id error=%v", err)
	}
	if _, err := profile.ParseResponse(context.Background(), nil, http.StatusOK, []byte(`{}`)); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("nil binding error=%v", err)
	}
}

func mustProfile(t *testing.T, profileID string) Profile {
	t.Helper()
	profile, err := NewRegistry().Get(profileID)
	if err != nil {
		t.Fatalf("Get(%s): %v", profileID, err)
	}
	return profile
}

func mustBuild(t *testing.T, profile Profile, input BuildInput) (*http.Request, *Binding) {
	t.Helper()
	request, binding, err := profile.BuildRequest(context.Background(), input)
	if err != nil {
		t.Fatalf("BuildRequest failed: %v", err)
	}
	return request, binding
}

func baseResponsesInput() BuildInput {
	return BuildInput{
		BaseURL:        "https://api.openai.com",
		APIKey:         "test-secret",
		RequestID:      "request",
		RequestedModel: "company-text",
		MappedModel:    "gpt-5.2",
		Body:           []byte(`{"model":"company-text","input":"private prompt"}`),
	}
}

func readJSONObject(t *testing.T, request *http.Request) map[string]any {
	t.Helper()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	var object map[string]any
	if err := json.Unmarshal(body, &object); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	return object
}
