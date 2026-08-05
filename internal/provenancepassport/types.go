// Package provenancepassport builds a read-only provenance and Capture-Gap
// passport from the exact completed-execution Source bytes and the existing
// Foigent Reference Connector projection.
package provenancepassport

import (
	"errors"
	"sync"
)

const (
	ContractVersion          = "unirouter.provenance-passport.v1"
	ReportVersion            = "unirouter.provenance-passport-report.v1"
	FixtureSchemaVersion     = "unirouter.provenance-passport-fixture.v1"
	ConformanceReportVersion = "unirouter.provenance-passport-conformance-report.v1"

	SourceProfile        = "unirouter.completedfeed.reference-v1"
	SourceRevision       = "1"
	OpenInferenceProfile = "otel.openinference.metadata-only"

	ProductProtocolVersion = "2.0.2"
	ProductProtocolCommit  = "ad8fe7a38c9d7335657630baa8670dd07556b98c"
	ProductProtocolDigest  = "sha256:99f8a541b6d33b0a74d2e4c06aa312c6d6c88c35a4bfb1d4d34b98b4e4e2e8e8"

	ReferenceMappingDigest  = "sha256:5425ab384bebcd9d4a7e81882bfa1bb1700df7cc397863b91f69e2855df12928"
	ReferenceCompatDigest   = "sha256:03fb035134b173165e6a03f348d40a58fe1d28d82c24bacb1dec553c8b769c89"
	ReferenceContractDigest = "sha256:f3ae834265170ff22088943283fd23dcb562bc2308571ccc722414c2c896dc23"
	ReferenceVectorDigest   = "sha256:50fc8e8be08c37381611d6423e29780894741fb108fc4569df7c895452e5d4db"
	ReferenceGoReportDigest = "sha256:7c1d2c3041bcd5e06a1e5c509992ee9c44ef9d8da4db896630ce0e6c10a2faa7"
	ReferenceTSReportDigest = "sha256:8f1129e134959af412e352d32b3919575527506290ecce1ea7b28b011a1352fc"

	LiteLLMConnectorID      = "foigent.litellm.spend-logs"
	LiteLLMConnectorVersion = "1.0.0"
	LiteLLMManifestDigest   = "sha256:d4386de89b24271aef32014cd33a11b02865c8eab5a4d594d742a051dd65ae28"
	LiteLLMMappingDigest    = "sha256:828a3d840ca880626ab5b1a3bc8836102265f209051f274d9da94e3decfbd08c"
	LiteLLMCompatDigest     = "sha256:3b0a2c5e3bfa89dc060299fa0b4351edcd33d92b2702cf95c99b1f26b51dd4ec"
	LiteLLMContractDigest   = "sha256:9f6859446dce72c80762a49fc9b62926f1b1c6350af79bc6b50e8eec9e29fde2"
	LiteLLMVectorDigest     = "sha256:44681fdec12476284e3c88f8aab710e04bcd35456007e829ae5199421df995d4"
	LiteLLMGoReportDigest   = "sha256:89777dfb10fd414512eea5bcc8a920f50e8d1e10ccede7d98c2b4e679fff3659"
	LiteLLMTSReportDigest   = "sha256:b56872ce923b43508628351dd7867f198f13f94592df6bc1e9413871f6200bd6"

	OpenInferenceGoldenDigest = "sha256:25cead642e4e9a59f47e514580b5a69e889d8f78022117f2c32dd4e8fd6efe56"

	ReplayAccepted      = "accepted"
	ReplayDuplicateNoop = "duplicate_noop"
	RuntimeMutationNone = "none"
	MaxInputBytes       = 3 << 20
)

var (
	ErrInvalid  = errors.New("provenancepassport: invalid contract")
	ErrConflict = errors.New("provenancepassport: identity or semantic conflict")
)

type ContractError struct {
	Code string `json:"code"`
	kind error
}

func (e *ContractError) Error() string {
	if e == nil {
		return "provenancepassport: unknown error"
	}
	return "provenancepassport: " + e.Code
}

func (e *ContractError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.kind
}

func contractError(kind error, code string) error {
	return &ContractError{Code: code, kind: kind}
}

type Request struct {
	ContractVersion       string `json:"contract_version"`
	PassportID            string `json:"passport_id"`
	SourceProfile         string `json:"source_profile"`
	SourceInstance        string `json:"source_instance"`
	SourceRevision        string `json:"source_revision"`
	SourceSnapshotBase64  string `json:"source_snapshot_base64"`
	OpenInferenceEvidence string `json:"openinference_evidence_sha256"`
}

type FieldEvidence struct {
	Field          string `json:"field"`
	Classification string `json:"classification"`
	EvidencePath   string `json:"evidence_path"`
	Reason         string `json:"reason,omitempty"`
}

type FieldGroups struct {
	Direct   []FieldEvidence `json:"direct"`
	Derived  []FieldEvidence `json:"derived"`
	Declared []FieldEvidence `json:"declared"`
	Missing  []FieldEvidence `json:"missing"`
}

type CaptureGapEvidence struct {
	GapID              string            `json:"gap_id"`
	AffectedClaim      string            `json:"affected_claim"`
	GapType            string            `json:"gap_type"`
	ReasonCode         string            `json:"reason_code"`
	Severity           string            `json:"severity"`
	ObservedAt         string            `json:"observed_at"`
	PayloadCaptureMode string            `json:"payload_capture_mode"`
	FieldProvenance    map[string]string `json:"field_provenance"`
}

type SourceEvidence struct {
	Profile             string `json:"profile"`
	SchemaID            string `json:"schema_id"`
	SchemaSHA256        string `json:"schema_sha256"`
	SourceInstance      string `json:"source_instance"`
	SourceRevision      string `json:"source_revision"`
	RequestRef          string `json:"request_ref"`
	TerminalStatus      string `json:"terminal_status"`
	SourceBytes         int    `json:"source_bytes"`
	SourceHash          string `json:"source_hash"`
	EnvelopeHash        string `json:"envelope_hash"`
	IdempotencyKey      string `json:"idempotency_key"`
	ExactBytesPreserved bool   `json:"exact_bytes_preserved"`
	AdapterFidelity     string `json:"adapter_fidelity"`
	RawPayloadPresent   bool   `json:"raw_payload_present"`
	PayloadCaptureMode  string `json:"payload_capture_mode"`
	TimelineComplete    bool   `json:"timeline_complete"`
}

type FeedCompatibility struct {
	Endpoint                string `json:"endpoint"`
	PageSchema              string `json:"page_schema"`
	ConnectorID             string `json:"connector_id"`
	ConnectorVersion        string `json:"connector_version"`
	EventSchemaRef          string `json:"event_schema_ref"`
	EventSchemaVersion      string `json:"event_schema_version"`
	ExistingDTOUnchanged    bool   `json:"existing_dto_unchanged"`
	SourceBytesUnchanged    bool   `json:"source_bytes_unchanged"`
	EventIdentityUnchanged  bool   `json:"event_identity_unchanged"`
	FeedWireUnchanged       bool   `json:"feed_wire_unchanged"`
	PageBehaviorUnchanged   bool   `json:"page_behavior_unchanged"`
	CursorBehaviorUnchanged bool   `json:"cursor_behavior_unchanged"`
	CredentialUnchanged     bool   `json:"credential_unchanged"`
}

type ProtocolEvidence struct {
	Repository    string `json:"repository"`
	Version       string `json:"version"`
	Commit        string `json:"commit"`
	ReleaseDigest string `json:"release_digest"`
}

type ConnectorAttestation struct {
	ConnectorID               string   `json:"connector_id"`
	ConnectorVersion          string   `json:"connector_version"`
	SourceProduct             string   `json:"source_product"`
	SourceSchema              string   `json:"source_schema"`
	EventSchema               string   `json:"event_schema"`
	ManifestSHA256            string   `json:"manifest_sha256"`
	SourceMappingSHA256       string   `json:"source_mapping_sha256"`
	CompatibilitySHA256       string   `json:"compatibility_sha256"`
	ConformanceContractSHA256 string   `json:"conformance_contract_sha256"`
	VectorBundleSHA256        string   `json:"vector_bundle_sha256"`
	IndependentReports        []string `json:"independent_reports"`
	ConformanceCases          int      `json:"conformance_cases"`
	ConformanceClasses        int      `json:"conformance_classes"`
	Status                    string   `json:"status"`
	RuntimeRegistered         bool     `json:"runtime_registered"`
	ProductRuntimeOwned       bool     `json:"product_runtime_owned"`
	CredentialOwned           bool     `json:"credential_owned"`
	CheckpointOwned           bool     `json:"checkpoint_owned"`
	CoreVendorBranchRequired  bool     `json:"core_vendor_branch_required"`
}

type ProjectionEvidence struct {
	Profile             string `json:"profile"`
	ContractVersion     string `json:"contract_version"`
	GoldenFixtureSHA256 string `json:"golden_fixture_sha256"`
	CreatesSourceTruth  bool   `json:"creates_source_truth"`
	IsConnectorRuntime  bool   `json:"is_connector_runtime"`
	RawContentIncluded  bool   `json:"raw_content_included"`
}

type RuntimeBoundary struct {
	ProductRegistryConnectorIDs []string `json:"product_registry_connector_ids"`
	SecondConnectorRegistered   bool     `json:"second_connector_registered"`
	SecondConnectorRuntimeOwned bool     `json:"second_connector_runtime_owned"`
	SynchronousDependency       bool     `json:"synchronous_dependency"`
	RuntimeMutation             string   `json:"runtime_mutation"`
}

type Report struct {
	ReportVersion      string               `json:"report_version"`
	ContractVersion    string               `json:"contract_version"`
	PassportID         string               `json:"passport_id"`
	InputPayloadHash   string               `json:"input_payload_hash"`
	InputSemanticHash  string               `json:"input_semantic_hash"`
	Source             SourceEvidence       `json:"source"`
	FieldGroups        FieldGroups          `json:"field_groups"`
	CaptureGaps        []CaptureGapEvidence `json:"capture_gaps"`
	Compatibility      FeedCompatibility    `json:"compatibility"`
	Protocol           ProtocolEvidence     `json:"protocol"`
	ReferenceConnector ConnectorAttestation `json:"reference_connector"`
	SecondConnector    ConnectorAttestation `json:"second_connector"`
	OpenInference      ProjectionEvidence   `json:"openinference"`
	ReplayStatus       string               `json:"replay_status"`
	RuntimeBoundary    RuntimeBoundary      `json:"runtime_boundary"`
}

type storedPassport struct {
	semanticHash string
	report       Report
}

type MemoryStore struct {
	mu        sync.Mutex
	passports map[string]storedPassport
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{passports: make(map[string]storedPassport)}
}

type Service struct {
	store *MemoryStore
}

func NewService(store *MemoryStore) *Service {
	return &Service{store: store}
}
