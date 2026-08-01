// Package asyncmedia freezes the repository-local, pre-gate contract for a
// future asynchronous media job plane. It contains no queue, lease, object
// store, provider, billing, cancellation, retention, or runtime integration.
package asyncmedia

import "errors"

const (
	ContractVersion          = "unirouter.async-media-job-plan.v1"
	FixtureSchemaVersion     = "unirouter.async-media-job-fixture.v1"
	ConformanceReportVersion = "unirouter.async-media-conformance-report.v1"

	RuntimeStateDecisionGated = "decision_gated_p7_d03"
	DecisionPending           = "unresolved_p7_d03"
	RuntimeMutationNone       = "none"

	OperationTextToImage  = "text_to_image"
	OperationImageToVideo = "image_to_video"

	CoverageDecisionGatedImage = "covered_by_decision_gated_profile"
	CoverageUnavailable        = "no_endpoint_profile_available"

	ChargeUnresolved = "unresolved_not_customer_chargeable"
)

var (
	ErrInvalid  = errors.New("asyncmedia: invalid contract")
	ErrConflict = errors.New("asyncmedia: identity or semantic conflict")
)

type ContractError struct {
	Code string `json:"code"`
	kind error
}

func (e *ContractError) Error() string {
	if e == nil {
		return "asyncmedia: unknown error"
	}
	return "asyncmedia: " + e.Code
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

type ArtifactIdentity struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type TenantScope struct {
	OrgRef     string `json:"org_ref"`
	ProjectRef string `json:"project_ref"`
}

type MediaObjectRef struct {
	ObjectRef     string      `json:"object_ref"`
	TenantScope   TenantScope `json:"tenant_scope"`
	ContentSHA256 string      `json:"content_sha256"`
	MediaType     string      `json:"media_type"`
	SizeBytes     string      `json:"size_bytes"`
	Immutable     bool        `json:"immutable"`
}

type OutputExpectation struct {
	Modality string `json:"modality"`
	Count    string `json:"count"`
}

type DecisionGates struct {
	MediaProvider string `json:"media_provider"`
	ObjectStore   string `json:"object_store"`
	Retention     string `json:"retention"`
	ChargeTrigger string `json:"charge_trigger"`
}

type LifecycleBoundary struct {
	AdmissionState    string `json:"admission_state"`
	QueueState        string `json:"queue_state"`
	LeaseState        string `json:"lease_state"`
	ExecutionState    string `json:"execution_state"`
	OutcomeState      string `json:"outcome_state"`
	CancellationState string `json:"cancellation_state"`
	SettlementState   string `json:"settlement_state"`
}

type EndpointEvidence struct {
	CoverageStatus          string `json:"coverage_status"`
	ReferenceProfileID      string `json:"reference_profile_id"`
	ReferenceManifestPath   string `json:"reference_manifest_path"`
	ReferenceManifestSHA256 string `json:"reference_manifest_sha256"`
}

type GapDeclaration struct {
	GapID      string `json:"gap_id"`
	State      string `json:"state"`
	Provenance string `json:"provenance"`
}

type PrivacyBoundary struct {
	MetadataOnly           bool `json:"metadata_only"`
	RawPromptIncluded      bool `json:"raw_prompt_included"`
	RawMediaBytesIncluded  bool `json:"raw_media_bytes_included"`
	StorageLocatorIncluded bool `json:"storage_locator_included"`
	SignedURLIncluded      bool `json:"signed_url_included"`
	CredentialIncluded     bool `json:"credential_included"`
}

type RuntimeBoundary struct {
	RuntimeRegistered     bool   `json:"runtime_registered"`
	QueueCreated          bool   `json:"queue_created"`
	LeaseCreated          bool   `json:"lease_created"`
	ObjectPersisted       bool   `json:"object_persisted"`
	ProviderNetworkCall   bool   `json:"provider_network_call"`
	CredentialsAccepted   bool   `json:"credentials_accepted"`
	CallbackAccepted      bool   `json:"callback_accepted"`
	CustomerChargeCreated bool   `json:"customer_charge_created"`
	RuntimeMutation       string `json:"runtime_mutation"`
	ExternalDecisionGate  string `json:"external_decision_gate"`
}

type CompatibilityBoundary struct {
	RequestPathUnchanged       bool `json:"request_path_unchanged"`
	RoutingUnchanged           bool `json:"routing_unchanged"`
	BillingUnchanged           bool `json:"billing_unchanged"`
	CompletedFeedWireUnchanged bool `json:"completed_feed_wire_unchanged"`
	OutboxIdentityUnchanged    bool `json:"outbox_identity_unchanged"`
	CursorBehaviorUnchanged    bool `json:"cursor_behavior_unchanged"`
}

type JobPlan struct {
	ContractVersion  string                `json:"contract_version"`
	PlanID           string                `json:"plan_id"`
	RuntimeState     string                `json:"runtime_state"`
	TenantScope      TenantScope           `json:"tenant_scope"`
	OperationClass   string                `json:"operation_class"`
	IntentSHA256     string                `json:"intent_sha256"`
	IdempotencyKey   string                `json:"idempotency_key"`
	InputObjects     []MediaObjectRef      `json:"input_objects"`
	Output           OutputExpectation     `json:"output"`
	DecisionGates    DecisionGates         `json:"decision_gates"`
	Lifecycle        LifecycleBoundary     `json:"lifecycle"`
	EndpointEvidence EndpointEvidence      `json:"endpoint_evidence"`
	ChargeState      string                `json:"charge_state"`
	Gaps             []GapDeclaration      `json:"gaps"`
	ThreatControls   []string              `json:"threat_controls"`
	Privacy          PrivacyBoundary       `json:"privacy"`
	RuntimeBoundary  RuntimeBoundary       `json:"runtime_boundary"`
	Compatibility    CompatibilityBoundary `json:"compatibility"`
}

type FixtureExpected struct {
	OperationClass   string   `json:"operation_class"`
	InputObjectCount int      `json:"input_object_count"`
	OutputModality   string   `json:"output_modality"`
	CoverageStatus   string   `json:"coverage_status"`
	GapIDs           []string `json:"gap_ids"`
	RuntimeMutation  string   `json:"runtime_mutation"`
}

type Fixture struct {
	SchemaVersion string             `json:"schema_version"`
	FixtureID     string             `json:"fixture_id"`
	Dependencies  []ArtifactIdentity `json:"dependencies"`
	Plan          JobPlan            `json:"plan"`
	Expected      FixtureExpected    `json:"expected"`
}

type ValidatedFixture struct {
	Fixture     Fixture
	SemanticSHA string
}

type ConformanceCase struct {
	CaseID      string   `json:"case_id"`
	CaseClasses []string `json:"case_classes"`
	Passed      bool     `json:"passed"`
	Result      string   `json:"result"`
}

type ConformanceReport struct {
	SchemaVersion   string             `json:"schema_version"`
	ContractVersion string             `json:"contract_version"`
	Schema          ArtifactIdentity   `json:"schema"`
	Fixtures        []ArtifactIdentity `json:"fixtures"`
	Dependencies    []ArtifactIdentity `json:"dependencies"`
	Cases           []ConformanceCase  `json:"cases"`
	Summary         struct {
		Fixtures int `json:"fixtures"`
		Cases    int `json:"cases"`
		Passed   int `json:"passed"`
		Failed   int `json:"failed"`
	} `json:"summary"`
}
