package admin

import (
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/xinsightconnector"
	"time"
)

const setupConsentSchema = "unirouter.xinsight.setup-consent.v1"

type xInsightSetupIssue struct {
	Code           string `json:"code"`
	RequiresChoice bool   `json:"requires_choice"`
}
type xInsightSetupPeerView struct {
	SchemaVersion   string `json:"schema_version"`
	OrgID           string `json:"org_id"`
	AnchorProjectID string `json:"anchor_project_id"`
	SourceInstance  string `json:"source_instance"`
	XIOrigin        string `json:"xi_origin"`
	XIDeploymentID  string `json:"xi_deployment_id"`
	XISetupURL      string `json:"xi_setup_url"`
	TrustReady      bool   `json:"trust_ready"`
}
type xInsightSetupCustody struct {
	Mode              string `json:"mode"`
	RetentionSeconds  int64  `json:"retention_seconds"`
	ProjectQuotaBytes int64  `json:"project_quota_bytes"`
	MaxItemBytes      int64  `json:"max_item_bytes"`
}
type xInsightSetupXIConsent struct {
	SchemaVersion           string               `json:"schema_version"`
	InstallationID          string               `json:"installation_id"`
	DeploymentID            string               `json:"deployment_id"`
	XIOrigin                string               `json:"xi_origin"`
	UROrigin                string               `json:"ur_origin"`
	ConsentRevision         int64                `json:"consent_revision"`
	ConsentSHA256           string               `json:"consent_sha256"`
	ExpiresAt               time.Time            `json:"expires_at"`
	Custody                 xInsightSetupCustody `json:"custody"`
	ExpectedCustodyRevision int64                `json:"expected_custody_revision"`
}
type xInsightSetupResolved struct {
	Invitation xinsightconnector.FrozenInvitationV1 `json:"invitation"`
	Consent    xInsightSetupXIConsent               `json:"consent"`
}
type xInsightSetupPreview struct {
	Invitation            xinsightconnector.FrozenInvitationV1 `json:"invitation"`
	SchemaVersion         string                               `json:"schema_version"`
	PreviewSHA256         string                               `json:"preview_sha256"`
	InstallationID        string                               `json:"installation_id"`
	XIConsent             xInsightSetupXIConsent               `json:"xi_consent"`
	AvailableProfiles     []string                             `json:"available_profiles"`
	DefaultProfiles       []string                             `json:"default_profiles"`
	ExistingProfiles      []string                             `json:"existing_profiles"`
	ContentAvailable      bool                                 `json:"content_available"`
	ReceiverMode          string                               `json:"receiver_mode"`
	ExistingBindingSHA256 *string                              `json:"existing_binding_sha256"`
	URPolicy              contentcapture.ScopedManagementView  `json:"ur_policy"`
	AllowedCoverage       []string                             `json:"allowed_coverage"`
	AllowedPolicyScopes   []string                             `json:"allowed_policy_scopes"`
	AllowedPolicyAccess   []xInsightSetupPolicyAccess          `json:"allowed_policy_access"`
	Issues                []xInsightSetupIssue                 `json:"issues"`
}
type xInsightSetupPolicyAccess struct {
	TargetKind string `json:"target_kind"`
	Coverage   string `json:"coverage"`
	Read       bool   `json:"read"`
	Write      bool   `json:"write"`
	UserID     string `json:"user_id,omitempty"`
}
type xInsightSetupPolicyPut struct {
	Scope string                             `json:"scope"`
	Put   contentcapture.ScopedManagementPut `json:"put"`
}
type xInsightSetupConsentPut struct {
	SchemaVersion           string                               `json:"schema_version"`
	RequestID               string                               `json:"request_id"`
	ProjectID               string                               `json:"project_id"`
	Invitation              xinsightconnector.FrozenInvitationV1 `json:"invitation"`
	ExpectedPreviewSHA256   string                               `json:"expected_preview_sha256"`
	ExpectedXIConsentSHA256 string                               `json:"expected_xi_consent_sha256"`
	ReceiverMode            string                               `json:"receiver_mode"`
	ExistingBindingSHA256   *string                              `json:"existing_binding_sha256"`
	Coverage                string                               `json:"coverage"`
	ExcludedProjectIDs      []string                             `json:"excluded_project_ids"`
	Profiles                []string                             `json:"profiles"`
	IncludeContent          bool                                 `json:"include_content"`
	PolicyAccess            []xInsightSetupPolicyAccess          `json:"policy_access"`
	URPolicy                *xInsightSetupPolicyPut              `json:"ur_policy"`
}

// Saved setup is deliberately non-secret: the original invitation remains only
// in the purpose-encrypted journal, and the request identity binds its digest.
type xInsightSavedSetup struct {
	SchemaVersion        string                      `json:"schema_version"`
	RequestID            string                      `json:"request_id"`
	ActorID              int64                       `json:"actor_id"`
	ActorEpoch           int64                       `json:"actor_epoch"`
	OrgID                int64                       `json:"org_id"`
	ProjectID            int64                       `json:"project_id"`
	ControlFence         int64                       `json:"control_fence"`
	InvitationSHA256     string                      `json:"invitation_sha256"`
	RequestSHA256        string                      `json:"request_sha256"`
	XIConsent            xInsightSetupXIConsent      `json:"xi_consent"`
	Coverage             string                      `json:"coverage"`
	ExcludedProjectIDs   []string                    `json:"excluded_project_ids"`
	Profiles             []string                    `json:"profiles"`
	IncludeContent       bool                        `json:"include_content"`
	PolicyAccess         []xInsightSetupPolicyAccess `json:"policy_access"`
	URPolicy             *xInsightSetupPolicyPut     `json:"ur_policy"`
	PolicyVector         string                      `json:"policy_vector"`
	ExistingSourceStates []bindExistingCandidate     `json:"existing_source_states"`
}
type xInsightSetupXIStatus struct {
	SchemaVersion   string `json:"schema_version"`
	InstallationID  string `json:"installation_id"`
	DeploymentID    string `json:"deployment_id"`
	ConsentRevision int64  `json:"consent_revision"`
	State           string `json:"state"`
	Custody         struct {
		DesiredRevision   int64                 `json:"desired_revision"`
		EffectiveRevision int64                 `json:"effective_revision"`
		Loaded            bool                  `json:"loaded"`
		Settings          *xInsightSetupCustody `json:"settings"`
	} `json:"custody"`
	Receiving struct {
		LastSuccessAt         *time.Time `json:"last_success_at"`
		ProfilesReceived      []string   `json:"profiles_received"`
		ContentReceivedAt     *time.Time `json:"content_received_at"`
		ContentReadVerifiedAt *time.Time `json:"content_read_verified_at"`
	} `json:"receiving"`
	Issues []xInsightSetupIssue `json:"issues"`
}
type xInsightSetupStatus struct {
	SchemaVersion      string `json:"schema_version"`
	InstallationID     string `json:"installation_id"`
	ConsentRevision    int64  `json:"consent_revision"`
	State              string `json:"state"`
	ConfigurationReady bool   `json:"configuration_ready"`
	PurposeStatus      struct {
		PolicyManage string `json:"policy_manage"`
		SourceEnroll string `json:"source_enroll"`
	} `json:"purpose_status"`
	URPolicyStatus struct {
		State         string `json:"state"`
		PolicyRef     string `json:"policy_ref"`
		PolicyVersion string `json:"policy_version"`
		VersionVector string `json:"version_vector"`
	} `json:"ur_policy_status"`
	XI     *xInsightSetupXIStatus `json:"xi"`
	Issues []xInsightSetupIssue   `json:"issues"`
}
