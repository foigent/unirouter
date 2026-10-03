package xinsightconnector

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	FrozenInvitationSchemaVersion = "xinsight.connector-installation-invitation.v1"
	FrozenInvitationAudience      = "unirouter"
	frozenInvitationInstallPath   = "/connectors/x-insight/install"
	maximumFrozenInvitationTTL    = 30 * time.Minute
)

var (
	frozenInvitationIDPattern    = regexp.MustCompile(`^xii_[A-Za-z0-9_-]{22}$`)
	frozenInvitationTokenPattern = regexp.MustCompile(`^xiiv1_[A-Za-z0-9_-]{43}$`)
)

// FrozenInvitationV1 is a consumer-only representation of the X-Insight-owned
// frozen invitation. UniRouter validates it but does not publish or extend it.
type FrozenInvitationV1 struct {
	SchemaVersion       string   `json:"schema_version"`
	InvitationID        string   `json:"invitation_id"`
	InstallationID      string   `json:"installation_id"`
	DeploymentID        string   `json:"deployment_id"`
	Audience            string   `json:"audience"`
	XInsightOrigin      string   `json:"xinsight_origin"`
	RequestedEventScope []string `json:"requested_event_scope"`
	ExpiresAt           string   `json:"expires_at"`
	InstallURL          string   `json:"install_url"`
}

type InvitationIntakeInput struct {
	ReceiverMode      string
	Invitation        FrozenInvitationV1
	OrgID             int64
	ProjectID         int64
	ConsentedByUserID int64
}

type InvitationIntakeResult struct {
	Installation Installation
	Operation    Operation
}

type invitationInstallationCreator interface {
	CreateInstallation(context.Context, Consent) (Installation, Operation, error)
}

type invitationSecretStager interface {
	StageInvitation(context.Context, string, string) error
}

// InvitationIntake validates one frozen invitation and composes the existing
// idempotent installation and secret-staging primitives. If staging fails
// after installation commit, replay resumes at the same installation and
// stages the same token without creating another operation.
type InvitationIntake struct {
	creator                invitationInstallationCreator
	stager                 invitationSecretStager
	expectedXInsightOrigin string
	runtimeRole            string
	now                    func() time.Time
}

func NewInvitationIntake(
	manager *Manager,
	journal *PostgresPhaseJournal,
	expectedXInsightOrigin, runtimeRole string,
) (*InvitationIntake, error) {
	if manager == nil || journal == nil {
		return nil, fmt.Errorf("%w: invitation intake dependencies are required", ErrInvalid)
	}
	if expectedXInsightOrigin != "" && !validFrozenHTTPSOrigin(expectedXInsightOrigin) {
		return nil, fmt.Errorf("%w: invitation intake X-Insight origin", ErrInvalid)
	}
	return &InvitationIntake{
		creator: manager, stager: journal,
		expectedXInsightOrigin: expectedXInsightOrigin,
		runtimeRole:            runtimeRole,
		now:                    time.Now,
	}, nil
}

func (intake *InvitationIntake) Accept(
	ctx context.Context,
	input InvitationIntakeInput,
) (InvitationIntakeResult, error) {
	if intake == nil || intake.creator == nil || intake.stager == nil || intake.now == nil {
		return InvitationIntakeResult{}, ErrUnavailable
	}
	// Invitation acceptance may run before the worker is enabled, but it never
	// runs on a non-stable role or without a locally pinned counterpart origin.
	if intake.runtimeRole != "stable" || intake.expectedXInsightOrigin == "" {
		return InvitationIntakeResult{}, ErrUnavailable
	}
	now := intake.now().UTC()
	if now.IsZero() {
		return InvitationIntakeResult{}, ErrUnavailable
	}
	validated, err := validateFrozenInvitation(
		input.Invitation, intake.expectedXInsightOrigin, now,
	)
	if err != nil {
		return InvitationIntakeResult{}, err
	}
	installation, operation, err := intake.creator.CreateInstallation(ctx, Consent{
		ReceiverMode:      input.ReceiverMode,
		InstallationID:    input.Invitation.InstallationID,
		XDeploymentID:     input.Invitation.DeploymentID,
		InvitationSHA256:  validated.tokenSHA256,
		OrgID:             input.OrgID,
		ProjectID:         input.ProjectID,
		EventScope:        EventScopeModelCompleted,
		IdempotencyKey:    validated.idempotencyKey,
		ConsentedByUserID: input.ConsentedByUserID,
	})
	if err != nil {
		return InvitationIntakeResult{}, err
	}
	if err := intake.stager.StageInvitation(
		ctx, installation.InstallationID, validated.token,
	); err != nil {
		return InvitationIntakeResult{}, err
	}
	return InvitationIntakeResult{Installation: installation, Operation: operation}, nil
}

type validatedFrozenInvitation struct {
	token          string
	tokenSHA256    string
	idempotencyKey string
}

func validateFrozenInvitation(
	invitation FrozenInvitationV1,
	expectedXInsightOrigin string,
	now time.Time,
) (validatedFrozenInvitation, error) {
	if invitation.SchemaVersion != FrozenInvitationSchemaVersion ||
		!frozenInvitationIDPattern.MatchString(invitation.InvitationID) ||
		!validExactExternalIdentity(invitation.InstallationID) ||
		!validExactDeploymentIdentity(invitation.DeploymentID) ||
		invitation.Audience != FrozenInvitationAudience ||
		len(invitation.RequestedEventScope) != 1 ||
		invitation.RequestedEventScope[0] != EventScopeModelCompleted ||
		!validFrozenHTTPSOrigin(invitation.XInsightOrigin) ||
		invitation.XInsightOrigin != expectedXInsightOrigin {
		return validatedFrozenInvitation{}, ErrInvalid
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, invitation.ExpiresAt)
	if err != nil || !strings.HasSuffix(invitation.ExpiresAt, "Z") ||
		expiresAt.UTC().Format(time.RFC3339Nano) != invitation.ExpiresAt ||
		!expiresAt.After(now) || expiresAt.After(now.Add(maximumFrozenInvitationTTL)) {
		return validatedFrozenInvitation{}, ErrInvalid
	}
	token, origin, err := parseFrozenInstallURL(invitation.InstallURL)
	if err != nil || origin != invitation.XInsightOrigin {
		return validatedFrozenInvitation{}, ErrInvalid
	}
	tokenSHA256 := hashBytes([]byte(token))
	return validatedFrozenInvitation{
		token:          token,
		tokenSHA256:    tokenSHA256,
		idempotencyKey: "xinsight-invitation-" + strings.TrimPrefix(tokenSHA256, "sha256:"),
	}, nil
}

func parseFrozenInstallURL(raw string) (string, string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.Opaque != "" || parsed.Path != frozenInvitationInstallPath || parsed.RawPath != "" ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment == "" ||
		strings.Contains(parsed.Host, "@") {
		return "", "", ErrInvalid
	}
	installOrigin := "https://" + parsed.Host
	if !validFrozenHTTPSOrigin(installOrigin) {
		return "", "", ErrInvalid
	}
	fragment, err := url.ParseQuery(parsed.Fragment)
	if err != nil || len(fragment) != 2 || len(fragment["invitation"]) != 1 ||
		len(fragment["xinsight_origin"]) != 1 {
		return "", "", ErrInvalid
	}
	token := fragment.Get("invitation")
	xInsightOrigin := fragment.Get("xinsight_origin")
	if !frozenInvitationTokenPattern.MatchString(token) ||
		!validFrozenHTTPSOrigin(xInsightOrigin) ||
		raw != installOrigin+frozenInvitationInstallPath+"#"+fragment.Encode() {
		return "", "", ErrInvalid
	}
	return token, xInsightOrigin, nil
}

func validFrozenHTTPSOrigin(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.User == nil &&
		parsed.Host != "" && parsed.Opaque == "" && parsed.Path == "" &&
		parsed.RawPath == "" && parsed.RawQuery == "" && !parsed.ForceQuery &&
		parsed.Fragment == "" && parsed.String() == raw &&
		!strings.Contains(parsed.Host, "@")
}

// AcceptTransaction is the normal setup's atomic composition of the existing
// frozen invitation validator, installation creator and encrypted journal.
func (intake *InvitationIntake) AcceptTransaction(ctx context.Context, tx pgx.Tx, input InvitationIntakeInput) (InvitationIntakeResult, error) {
	manager, ok := intake.creator.(*Manager)
	journal, journalOK := intake.stager.(*PostgresPhaseJournal)
	if !ok || !journalOK || intake.runtimeRole != "stable" || intake.expectedXInsightOrigin == "" {
		return InvitationIntakeResult{}, ErrUnavailable
	}
	v, err := validateFrozenInvitation(input.Invitation, intake.expectedXInsightOrigin, intake.now().UTC())
	if err != nil {
		return InvitationIntakeResult{}, err
	}
	i, op, err := manager.CreateInstallationTx(ctx, tx, Consent{ReceiverMode: input.ReceiverMode, InstallationID: input.Invitation.InstallationID, XDeploymentID: input.Invitation.DeploymentID, InvitationSHA256: v.tokenSHA256, OrgID: input.OrgID, ProjectID: input.ProjectID, EventScope: EventScopeModelCompleted, IdempotencyKey: v.idempotencyKey, ConsentedByUserID: input.ConsentedByUserID})
	if err != nil {
		return InvitationIntakeResult{}, err
	}
	if err = journal.StageInvitationTx(ctx, tx, i.InstallationID, v.token); err != nil {
		return InvitationIntakeResult{}, err
	}
	return InvitationIntakeResult{Installation: i, Operation: op}, nil
}

func ValidateSetupInvitation(inv FrozenInvitationV1, origin string, now time.Time) (string, error) {
	v, err := validateFrozenInvitation(inv, origin, now)
	return v.token, err
}
