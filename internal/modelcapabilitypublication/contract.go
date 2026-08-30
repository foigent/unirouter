// Package modelcapabilitypublication owns immutable capability-contract
// declarations and their deterministic database projection. Runtime routing
// continues to query the database projection; this package never creates live
// Probe, Approval, Release, pricing or routing authority.
package modelcapabilitypublication

import (
	"errors"
	"time"
)

const (
	DeclarationSchemaV1     = "unirouter.capability-contract-publication.v1"
	BaselineSchemaV1        = "unirouter.capability-contract-baseline.v1"
	CanonicalizationV1      = "unirouter.capability-contract-publication-canonical.v1"
	PlanSchemaV1            = "unirouter.capability-contract-publication-plan.v1"
	InstallEnvelopeSchemaV1 = "unirouter.capability-contract-install-sequence.v1"
	BaselineMigrationM100   = "000100_kimi_k26_function_tools_tool_index_normalizer"

	BindingActionAppend = "append"
	BindingActionNoOp   = "no_op"

	SelectorActionPromotion = "current_promotion"
	SelectorActionNoOp      = "no_op"

	ProjectionActionInsert = "insert"
	ProjectionActionUpdate = "update"
	ProjectionActionNoOp   = "no_op"

	ValidationValid    = "valid"
	ValidationConflict = "conflict"

	ExecutionModePendingApply            = "pending_apply"
	ExecutionModePostCommitRecoveryCheck = "post_commit_recovery_check"

	ArtifactKindSource = "source"
	ArtifactKindImage  = "image"

	LedgerStateExactPublicationPresent = "exact_publication_present"
	LedgerStateMissing                 = "missing"
	LedgerStateUnexpected              = "unexpected"

	LedgerRelationExactPlanPostState = "exact_plan_post_state"
	LedgerRelationAdvancedCompatible = "advanced_compatible"
	LedgerRelationUnexpected         = "unexpected"

	StaticVerificationForPlan         = "static_projection_verified_for_plan"
	StaticVerificationCurrent         = "current_static_projection_verified"
	StaticVerificationMismatch        = "mismatch"
	StaticVerificationPlanUnavailable = "plan_unavailable"

	DynamicReadbackUnreconstructable = "unreconstructable"
)

var (
	ErrInvalidDeclaration = errors.New("capability contract publication declaration is invalid")
	ErrPlanConflict       = errors.New("capability contract publication plan conflicts with target state")
	ErrUnsupportedMode    = errors.New("capability contract publication execution mode is unsupported")
)

// Binding is one immutable database binding row. CreatedAt is deliberately
// absent because it is database-assigned and is the only ignored binding field
// in M100 equivalence.
type Binding struct {
	OperationID             string `json:"operation_id"`
	CapabilityID            string `json:"capability_id"`
	ExecutionProfileID      string `json:"execution_profile_id"`
	ExecutionProfileVersion string `json:"execution_profile_version"`
	ProtocolProfileID       string `json:"protocol_profile_id"`
	ProtocolProfileVersion  string `json:"protocol_profile_version"`
	ProbeContractID         string `json:"probe_contract_id"`
	ProbeContractVersion    string `json:"probe_contract_version"`
	ExpectedProvider        string `json:"expected_provider"`
	ExpectedUpstreamModel   string `json:"expected_upstream_model"`
	PublicPath              string `json:"public_path"`
	IntroducedMigration     string `json:"introduced_migration"`
}

// Selector is one exact current-contract selector. UpdatedAt is deliberately
// absent because it is database-assigned and is the only ignored selector field
// in M100 equivalence.
type Selector struct {
	OperationID             string `json:"operation_id"`
	CapabilityID            string `json:"capability_id"`
	ExecutionProfileID      string `json:"execution_profile_id"`
	ExecutionProfileVersion string `json:"execution_profile_version"`
	ProtocolProfileID       string `json:"protocol_profile_id"`
	ProtocolProfileVersion  string `json:"protocol_profile_version"`
	ProbeContractID         string `json:"probe_contract_id"`
	ProbeContractVersion    string `json:"probe_contract_version"`
}

// SelectorTransition declares one exact absent-to-current or current-to-current
// compare-and-swap. ExpectedPredecessor is nil only for an exactly absent row.
type SelectorTransition struct {
	OperationID             string            `json:"operation_id"`
	CapabilityID            string            `json:"capability_id"`
	ExecutionProfileID      string            `json:"execution_profile_id"`
	ExecutionProfileVersion string            `json:"execution_profile_version"`
	ProtocolProfileID       string            `json:"protocol_profile_id"`
	ProtocolProfileVersion  string            `json:"protocol_profile_version"`
	ExpectedPredecessor     *ContractIdentity `json:"expected_predecessor"`
	Final                   ContractIdentity  `json:"final"`
}

type ContractIdentity struct {
	ProbeContractID      string `json:"probe_contract_id"`
	ProbeContractVersion string `json:"probe_contract_version"`
}

// ProjectionEffect is a declaration-time exact filter. It is expanded into
// exact Target rows by planning and is never itself apply authority.
type ProjectionEffect struct {
	OperationID             string `json:"operation_id"`
	CapabilityID            string `json:"capability_id"`
	ExecutionProfileID      string `json:"execution_profile_id"`
	ExecutionProfileVersion string `json:"execution_profile_version"`
	ProtocolProfileID       string `json:"protocol_profile_id"`
	ProtocolProfileVersion  string `json:"protocol_profile_version"`
	ExpectedProvider        string `json:"expected_provider"`
	ExpectedUpstreamModel   string `json:"expected_upstream_model"`
	ExpectedContractRef     string `json:"expected_contract_ref"`
	FinalContractRef        string `json:"final_contract_ref"`
	AllowInsert             bool   `json:"allow_insert"`
}

type Baseline struct {
	SchemaVersion    string     `json:"schema_version"`
	ThroughMigration string     `json:"through_migration"`
	Bindings         []Binding  `json:"bindings"`
	Selectors        []Selector `json:"selectors"`
}

type Declaration struct {
	SchemaVersion             string               `json:"schema_version"`
	PublicationID             string               `json:"publication_id"`
	ExpectedStaticPredecessor string               `json:"expected_static_predecessor"`
	ExpectedSchemaPredecessor string               `json:"expected_schema_predecessor"`
	BindingAppends            []Binding            `json:"binding_appends"`
	SelectorTransitions       []SelectorTransition `json:"selector_transitions"`
	DeclaredProjectionEffects []ProjectionEffect   `json:"declared_projection_effects"`
}

type ArtifactIdentity struct {
	Kind                      string `json:"kind"`
	Commit                    string `json:"commit"`
	Tree                      string `json:"tree"`
	Version                   string `json:"version"`
	ImageDigest               string `json:"image_digest"`
	PublisherExecutableDigest string `json:"publisher_executable_digest"`
}

// MigrationFileIdentity binds one exact artifact migration to the bytes that
// the publication owner is allowed to execute.
type MigrationFileIdentity struct {
	Version  string `json:"version"`
	UpSHA256 string `json:"up_sha256"`
}

// PlanAuthority is the current compiled, artifact-bound authority against
// which an untrusted local Plan must be checked before apply or recovery.
type PlanAuthority struct {
	Artifact                ArtifactIdentity
	ArtifactMigrationLedger []string
	ArtifactMigrationFiles  []MigrationFileIdentity
	PublicationDownSHA256   string
	PublicationVerifySHA256 string
	Predecessor             Baseline
	Declaration             Declaration
}

type TargetIdentity struct {
	Environment      string `json:"environment"`
	DatabaseName     string `json:"database_name"`
	SystemIdentifier string `json:"system_identifier"`
}

type BindingPlanItem struct {
	Action   string   `json:"action"`
	Expected *Binding `json:"expected"`
	Final    Binding  `json:"final"`
}

type SelectorPlanItem struct {
	Action   string    `json:"action"`
	Expected *Selector `json:"expected"`
	Final    Selector  `json:"final"`
}

// DeploymentProjection is the complete authoritative row subset mutated by a
// publication. Runtime-created fields outside this subset are not inferred.
type DeploymentProjection struct {
	DeploymentID    int64      `json:"deployment_id"`
	OperationID     string     `json:"operation_id"`
	CapabilityID    string     `json:"capability_id"`
	SupportState    string     `json:"support_state"`
	EvidenceState   string     `json:"evidence_state"`
	EvidenceRef     string     `json:"evidence_ref"`
	VerifiedAt      *time.Time `json:"verified_at"`
	Revision        int64      `json:"revision"`
	SupportRevision int64      `json:"support_revision"`
}

type ProjectionPlanItem struct {
	Action               string                `json:"action"`
	Expected             *DeploymentProjection `json:"expected"`
	Final                DeploymentProjection  `json:"final"`
	ExpectedAffectedRows int64                 `json:"expected_affected_rows"`
}

type Conflict struct {
	Code   string `json:"code"`
	Target string `json:"target"`
}

type Plan struct {
	SchemaVersion           string                  `json:"schema_version"`
	CanonicalizationVersion string                  `json:"canonicalization_version"`
	PublicationID           string                  `json:"publication_id"`
	DeclarationDigest       string                  `json:"declaration_digest"`
	Artifact                ArtifactIdentity        `json:"artifact"`
	Target                  TargetIdentity          `json:"target"`
	ArtifactMigrationLedger []string                `json:"artifact_migration_ledger"`
	ArtifactMigrationFiles  []MigrationFileIdentity `json:"artifact_migration_files"`
	PublicationDownSHA256   string                  `json:"publication_down_sha256"`
	PublicationVerifySHA256 string                  `json:"publication_verify_sha256"`
	ExpectedLedger          []string                `json:"expected_ledger"`
	PendingMigrations       []string                `json:"pending_migrations"`
	ApplyMigrations         []string                `json:"apply_migrations"`
	ProposedLedger          []string                `json:"proposed_ledger"`
	BindingItems            []BindingPlanItem       `json:"binding_items"`
	SelectorItems           []SelectorPlanItem      `json:"selector_items"`
	ProjectionItems         []ProjectionPlanItem    `json:"projection_items"`
	Validation              string                  `json:"validation"`
	Conflicts               []Conflict              `json:"conflicts"`
	PlanDigest              string                  `json:"plan_digest"`
}

// RecoveryCheckResult is an honest read-only observation. Dynamic readback is
// never reconstructed after the original serialized apply window.
type RecoveryCheckResult struct {
	LedgerState        string `json:"ledger_state"`
	LedgerRelation     string `json:"ledger_relation"`
	StaticVerification string `json:"static_verification"`
	DynamicReadback    string `json:"dynamic_readback"`
}

type InstallSequenceItem struct {
	PublicationID string `json:"publication_id"`
	PlanDigest    string `json:"plan_digest"`
}

type InstallEnvelope struct {
	SchemaVersion   string                `json:"schema_version"`
	Artifact        ArtifactIdentity      `json:"artifact"`
	Target          TargetIdentity        `json:"target"`
	StartLedger     []string              `json:"start_ledger"`
	ExpectedLedger  []string              `json:"expected_ledger"`
	InstallSequence []InstallSequenceItem `json:"install_sequence"`
	EnvelopeDigest  string                `json:"envelope_digest"`
}
