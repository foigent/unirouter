package eventconnector

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPhase2BSharedIdentityVectors(t *testing.T) {
	t.Parallel()

	tenant := "7"
	testCases := []struct {
		name       string
		identity   IdentityComponents
		inputHex   string
		idempotent string
	}{
		{
			name: "absent-tenant-settled",
			identity: IdentityComponents{
				ProducerProduct:  "unirouter",
				ProducerInstance: "unirouter-development",
				OrgRef:           "101",
				ProjectRef:       "202",
				EventType:        "model.request.completed",
				EventID:          "req_feed_v1_settled",
			},
			inputHex:   "666f6967656e742e6576656e742d6964656d706f74656e63792e763100000009756e69726f7574657200000015756e69726f757465722d646576656c6f706d656e74000000000000000331303100000003323032000000176d6f64656c2e726571756573742e636f6d706c65746564000000137265715f666565645f76315f736574746c6564",
			idempotent: "sha256:c2822f0d940c01de77e2e06af1a5f6e1fd10fc66269be20fceb0939150c17b2e",
		},
		{
			name: "absent-tenant-cancelled",
			identity: IdentityComponents{
				ProducerProduct:  "unirouter",
				ProducerInstance: "unirouter-development",
				OrgRef:           "101",
				ProjectRef:       "202",
				EventType:        "model.request.completed",
				EventID:          "req_feed_v1_cancelled",
			},
			inputHex:   "666f6967656e742e6576656e742d6964656d706f74656e63792e763100000009756e69726f7574657200000015756e69726f757465722d646576656c6f706d656e74000000000000000331303100000003323032000000176d6f64656c2e726571756573742e636f6d706c65746564000000157265715f666565645f76315f63616e63656c6c6564",
			idempotent: "sha256:d342a560ade558edc557f2cd645a526178958540a012ceabe9ca7047e916796c",
		},
		{
			name: "present-tenant",
			identity: IdentityComponents{
				ProducerProduct:  "unirouter",
				ProducerInstance: "unirouter-development",
				TenantRef:        &tenant,
				OrgRef:           "101",
				ProjectRef:       "202",
				EventType:        "model.request.completed",
				EventID:          "req_feed_v1_settled",
			},
			inputHex:   "666f6967656e742e6576656e742d6964656d706f74656e63792e763100000009756e69726f7574657200000015756e69726f757465722d646576656c6f706d656e7400000001370000000331303100000003323032000000176d6f64656c2e726571756573742e636f6d706c65746564000000137265715f666565645f76315f736574746c6564",
			idempotent: "sha256:3c1dc7d03faee1b736b8e2705c1fbef8147aac06cdc1593bef969da55303672d",
		},
		{
			name: "opaque-non-nfc-event-id",
			identity: IdentityComponents{
				ProducerProduct:  "unirouter",
				ProducerInstance: "unirouter-development",
				OrgRef:           "101",
				ProjectRef:       "202",
				EventType:        "model.request.completed",
				EventID:          "req_e\u0301",
			},
			inputHex:   "666f6967656e742e6576656e742d6964656d706f74656e63792e763100000009756e69726f7574657200000015756e69726f757465722d646576656c6f706d656e74000000000000000331303100000003323032000000176d6f64656c2e726571756573742e636f6d706c65746564000000077265715f65cc81",
			idempotent: "sha256:fd256a506369a0fa05dad3ee68f2e577918b97810778e1802d11950112275447",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			input, err := BuildIdentityInput(testCase.identity)
			if err != nil {
				t.Fatalf("build identity input: %v", err)
			}
			if got := hex.EncodeToString(input); got != testCase.inputHex {
				t.Fatalf("identity input=%s want=%s", got, testCase.inputHex)
			}
			if got := SHA256Reference(input); got != testCase.idempotent {
				t.Fatalf("idempotency=%s want=%s", got, testCase.idempotent)
			}
		})
	}
}

func TestPhase2BCanonicalNumericTimeAndUnicodeVectors(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		value string
		valid bool
	}{
		{"0", true},
		{"9007199254740991", true},
		{"9007199254740992", true},
		{"9223372036854775807", true},
		{"01", false},
		{"-1", false},
		{"9223372036854775808", false},
		{"1.0", false},
		{"1e3", false},
		{" 1", false},
	} {
		if got := ValidateInt64Decimal(testCase.value) == nil; got != testCase.valid {
			t.Errorf("ValidateInt64Decimal(%q)=%v want=%v", testCase.value, got, testCase.valid)
		}
	}

	for _, testCase := range []struct {
		name       string
		input      string
		normalized string
		errorCode  string
	}{
		{"whole-second", "2026-07-25T04:05:06Z", "2026-07-25T04:05:06.000000Z", ""},
		{"offset-to-utc", "2026-07-25T12:05:06.123456+08:00", "2026-07-25T04:05:06.123456Z", ""},
		{"nonzero-microsecond", "2026-07-25T04:05:06.000001Z", "2026-07-25T04:05:06.000001Z", ""},
		{"sub-microsecond-loss", "2026-07-25T04:05:06.123456789Z", "", "timestamp_precision_loss"},
		{"missing-zone", "2026-07-25T04:05:06", "", "invalid_timestamp"},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			got, err := NormalizeSourceTime(testCase.input)
			if testCase.errorCode != "" {
				if err == nil || err.Code != testCase.errorCode {
					t.Fatalf("error=%v want code=%s", err, testCase.errorCode)
				}
				return
			}
			if err != nil || got != testCase.normalized {
				t.Fatalf("normalized=%q error=%v want=%q", got, err, testCase.normalized)
			}
		})
	}

	if _, err := NewProtocolString("event_type", "model.request.completed"); err != nil {
		t.Fatalf("NFC protocol value rejected: %v", err)
	}
	if _, err := NewProtocolString("event_type", "model.reque\u0301st.completed"); err == nil ||
		err.Code != "protocol_string_not_nfc" {
		t.Fatalf("non-NFC protocol error=%v", err)
	}
	opaque, err := NewOpaqueString("event_id", "req_e\u0301", 1, 128, true)
	if err != nil {
		t.Fatalf("opaque non-NFC value rejected: %v", err)
	}
	if got := hex.EncodeToString([]byte(opaque.String())); got != "7265715f65cc81" {
		t.Fatalf("opaque bytes=%s", got)
	}

	canonical, canonicalErr := CanonicalJSON(map[string]any{
		"\ue000": 2,
		"😀":      1,
	})
	if canonicalErr != nil {
		t.Fatalf("canonicalize UTF-16 ordering vector: %v", canonicalErr)
	}
	if got, want := string(canonical), `{"😀":1,"":2}`; got != want {
		t.Fatalf("canonical UTF-16 ordering=%q want=%q", got, want)
	}
	if _, err := CanonicalJSON(map[string]any{"n": int64(9_007_199_254_740_991)}); err != nil {
		t.Fatalf("max safe integer rejected: %v", err)
	}
	if _, err := CanonicalJSON(map[string]any{"n": int64(9_007_199_254_740_992)}); err == nil ||
		err.Code != "canonicalization_failed" {
		t.Fatalf("above-safe integer error=%v", err)
	}
}

func TestPhase2BStrictJSONVectors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		json []byte
		code string
	}{
		{
			name: "nested-duplicate-key",
			json: []byte(`{"payload":{"terminal":{"status":"settled","status":"cancelled"}}}`),
			code: "duplicate_json_key",
		},
		{
			name: "escaped-duplicate-key",
			json: []byte(`{"status":"settled","\u0073tatus":"cancelled"}`),
			code: "duplicate_json_key",
		},
		{
			name: "unpaired-surrogate",
			json: []byte(`{"id":"\ud800"}`),
			code: "invalid_unicode_escape",
		},
		{
			name: "lossy-null",
			json: []byte(`{"value":null}`),
			code: "invalid_source_json",
		},
		{
			name: "trailing-content",
			json: []byte(`{"value":1} {}`),
			code: "invalid_source_json",
		},
		{
			name: "invalid-utf8",
			json: []byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'},
			code: "invalid_utf8",
		},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateStrictJSON(testCase.json, StrictJSONOptions{RejectNull: true})
			if err == nil || err.Code != testCase.code {
				t.Fatalf("error=%v want code=%s", err, testCase.code)
			}
			if validationErr := err.Validate(); validationErr != nil {
				t.Fatalf("invalid Connector Error: %v", validationErr)
			}
		})
	}
	if err := ValidateStrictJSON([]byte(`{"id":"\ud83d\ude00"}`), StrictJSONOptions{RejectNull: true}); err != nil {
		t.Fatalf("paired surrogate rejected: %v", err)
	}
}

func TestPhase2BImmutableSourceAndDescriptorBinding(t *testing.T) {
	t.Parallel()

	binding := phase2BTestDescriptorBinding()
	descriptor, err := NewDescriptor(binding)
	if err != nil {
		t.Fatalf("create descriptor: %v", err)
	}
	source := []byte("{\"id\":\"e\u0301\"}")
	exactSource := append([]byte(nil), source...)
	expectedHash := HashSourceBytes(exactSource)
	artifact, err := NewSourceArtifact(
		descriptor,
		"source-e\u0301",
		"1",
		source,
		StrictJSONOptions{RejectNull: true},
	)
	if err != nil {
		t.Fatalf("create Source Artifact: %v", err)
	}
	source[2] = 'X'
	returned := artifact.Bytes()
	returned[2] = 'Y'
	if !bytes.Equal(artifact.Bytes(), exactSource) {
		t.Fatal("Source Artifact bytes changed through an external slice")
	}
	if artifact.SourceHash() != expectedHash {
		t.Fatalf("source hash=%s want=%s", artifact.SourceHash(), expectedHash)
	}

	mutated := binding
	mutated.EnvelopeSchemaDigest = "sha256:" + strings.Repeat("9", 64)
	otherDescriptor, otherErr := NewDescriptor(mutated)
	if otherErr != nil {
		t.Fatalf("create changed descriptor: %v", otherErr)
	}
	if descriptor.key == otherDescriptor.key {
		t.Fatal("Envelope Schema digest did not enter descriptor binding")
	}
	mutated = binding
	mutated.EmittedEvents = append([]EventSchemaBinding(nil), binding.EmittedEvents...)
	mutated.EmittedEvents[0].SHA256 = "sha256:" + strings.Repeat("8", 64)
	otherDescriptor, otherErr = NewDescriptor(mutated)
	if otherErr != nil {
		t.Fatalf("create changed Event descriptor: %v", otherErr)
	}
	if descriptor.key == otherDescriptor.key {
		t.Fatal("Event Schema digest did not enter descriptor binding")
	}
}

func TestPhase2BConnectorErrorContract(t *testing.T) {
	t.Parallel()

	valid := NewConnectorError(
		"transport",
		"source_unavailable",
		"fetch",
		"fetch_page",
		true,
		"Source is temporarily unavailable",
		"automatic_retry",
		errors.New("private cause"),
	).WithRetryAfter(3)
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid Connector Error rejected: %v", err)
	}
	wire, err := json.Marshal(valid)
	if err != nil {
		t.Fatalf("marshal Connector Error: %v", err)
	}
	if bytes.Contains(wire, []byte("private cause")) {
		t.Fatalf("private error cause leaked: %s", wire)
	}

	invalid := NewConnectorError(
		"contract",
		"bad_input",
		"validate",
		"validate_input",
		false,
		"Input is invalid",
		"operator_repair",
		nil,
	).WithRetryAfter(1)
	if err := invalid.Validate(); err == nil {
		t.Fatal("non-retryable Connector Error accepted retry_after_seconds")
	}
}

func TestPhase2BStaticRegistryPin(t *testing.T) {
	t.Parallel()

	payload, err := os.ReadFile(filepath.Join("..", "..", "contracts", "foigent", "connector-registry.pin.v1.json"))
	if err != nil {
		t.Fatalf("read registry pin: %v", err)
	}
	registry, connectorErr := DecodeRegistryPin(payload)
	if connectorErr != nil {
		t.Fatalf("decode registry pin: %v", connectorErr)
	}
	if registry.Protocol.Tag != "v1.0.0" ||
		registry.Protocol.Version != "1.0.0" ||
		registry.Protocol.Commit != "c09bc93cc7750e2333752655d6b4e69802e6769d" ||
		registry.Protocol.ReleaseDigest != "sha256:aaeb5b37b94a1fd603048d77b2180fa88be1ddbd6eeea6a1369edf0cea9c5e2b" {
		t.Fatalf("unexpected protocol identity: %#v", registry.Protocol)
	}
	expectedComponents := map[string]string{
		"connector_manifest_schema":  "sha256:a6f43b37477002ab836a4d9ea7c4e462675cd4313f0637c0733cd98675fc0adf",
		"event_envelope_schema":      "sha256:6942e85d9b5d8b08ff9828b970c317596394184ef29908cbf2b8fc792187cf26",
		"event_batch_schema":         "sha256:8e2133922bcb77cf032309290593665df1bf56cfa47e433dd573d0e42f36eef9",
		"checkpoint_schema":          "sha256:8be2fd290d6db8d288a9b10ac6d9aa0b55a66ff21ea0fb55ac0af2d1b66bf800",
		"operation_receipt_schema":   "sha256:3c3b99e009e208456039d3bfe26d84af7e3f1af23589e7fa62a1e1a61e0fb3c6",
		"error_schema":               "sha256:f449ba6314f1f730be5a0b188dc6d2cc2704e8f5d84a4eebecac576221e4ca56",
		"canonical_vector_bundle":    "sha256:50fc8e8be08c37381611d6423e29780894741fb108fc4569df7c895452e5d4db",
		"conformance_contract":       "sha256:f3ae834265170ff22088943283fd23dcb562bc2308571ccc722414c2c896dc23",
		"source_projection_contract": "sha256:5425ab384bebcd9d4a7e81882bfa1bb1700df7cc397863b91f69e2855df12928",
	}
	if len(registry.Protocol.Components) != len(expectedComponents) {
		t.Fatalf("component count=%d want=%d", len(registry.Protocol.Components), len(expectedComponents))
	}
	for name, expected := range expectedComponents {
		if got := registry.Protocol.Components[name].SHA256; got != expected {
			t.Errorf("%s digest=%s want=%s", name, got, expected)
		}
	}
	if got, want := registry.ConnectorIDs(), []string{"unirouter.completedfeed.reference-v1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("connector IDs=%v want=%v", got, want)
	}
	descriptor, descriptorErr := registry.Descriptor("unirouter.completedfeed.reference-v1")
	if descriptorErr != nil {
		t.Fatalf("resolve descriptor: %v", descriptorErr)
	}
	if descriptor.ConnectorID() != "foigent.unirouter.completed-execution" ||
		descriptor.ConnectorVersion() != "1.0.0" ||
		descriptor.SourceProduct() != "unirouter" {
		t.Fatalf("unexpected descriptor binding: %#v", descriptor.Binding())
	}

	sourceSchema, err := os.ReadFile(filepath.Join("..", "..", "contracts", "completed-execution", "v1", "unirouter.completed-execution.v1.schema.json"))
	if err != nil {
		t.Fatalf("read Source Schema: %v", err)
	}
	if got, want := HashSourceBytes(sourceSchema), descriptor.Binding().SourceSchema.SHA256; got != want {
		t.Fatalf("Source Schema digest=%s want=%s", got, want)
	}
}

func phase2BTestDescriptorBinding() DescriptorBinding {
	return DescriptorBinding{
		ProtocolVersion:      "1.0.0",
		ProtocolCommit:       strings.Repeat("1", 40),
		ReleaseDigest:        "sha256:" + strings.Repeat("2", 64),
		ManifestDigest:       "sha256:" + strings.Repeat("3", 64),
		EnvelopeSpecVersion:  EventEnvelopeSpecVersion,
		EnvelopeSchemaDigest: "sha256:" + strings.Repeat("4", 64),
		SourceProduct:        "unirouter",
		ConnectorID:          "foigent.unirouter.test",
		ConnectorVersion:     "1.0.0",
		SourceSchema: SchemaBinding{
			SchemaID: "unirouter.test.v1",
			SHA256:   "sha256:" + strings.Repeat("5", 64),
		},
		EmittedEvents: []EventSchemaBinding{
			{
				EventType:     "test.event",
				SchemaRef:     "foigent://events/test/event/v1",
				SchemaVersion: "1",
				SHA256:        "sha256:" + strings.Repeat("6", 64),
			},
		},
	}
}
