package accessprovisioning

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/accessscope"
	"github.com/uniroute/unirouter/internal/auth"
)

func TestNormalizePageInput(t *testing.T) {
	defaulted, err := NormalizePageInput(PageInput{})
	require.NoError(t, err)
	require.Equal(t, DefaultPageLimit, defaulted.Limit)
	require.Zero(t, defaulted.AfterID)

	bounded, err := NormalizePageInput(PageInput{Limit: MaxPageLimit, AfterID: 42})
	require.NoError(t, err)
	require.Equal(t, PageInput{Limit: MaxPageLimit, AfterID: 42}, bounded)

	for _, input := range []PageInput{
		{Limit: -1}, {Limit: MaxPageLimit + 1}, {AfterID: -1},
	} {
		_, err := NormalizePageInput(input)
		require.ErrorIs(t, err, ErrInvalid)
	}
}

func TestNormalizeGrantLifecycleReasonBoundsDurableEvidenceText(t *testing.T) {
	valid := strings.Repeat("a", MaxGrantLifecycleReasonBytes)
	if got, err := normalizeGrantLifecycleReason("  " + valid + "  "); err != nil || got != valid {
		t.Fatalf("valid reason rejected: len=%d err=%v", len(got), err)
	}
	for _, invalid := range []string{"", "   ", "contains\x00nul", "line\nbreak", "carriage\rreturn", strings.Repeat("b", MaxGrantLifecycleReasonBytes+1)} {
		if _, err := normalizeGrantLifecycleReason(invalid); err == nil {
			t.Fatalf("invalid lifecycle reason accepted: %q", invalid)
		}
	}
}

func TestViewerSubjectsArePolicyRejectedWithStableReason(t *testing.T) {
	tests := []struct {
		name             string
		projectRole      string
		organizationRole string
	}{
		{name: "project viewer", projectRole: auth.ProjectRoleViewer, organizationRole: auth.RoleMember},
		{name: "organization viewer", projectRole: auth.ProjectRoleUser, organizationRole: auth.RoleOrgViewer},
	}
	service := &Service{}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision, err := service.decideTx(context.Background(), nil, Actor{}, ProvisionInput{
				Subject:  Subject{Type: SubjectUser, UserID: 1},
				Envelope: Envelope{ProjectRole: test.projectRole},
			}, test.projectRole, test.organizationRole, nil)
			if err != nil {
				t.Fatal(err)
			}
			if decision.status != RequestRejected || decision.mode != DecisionPolicyRejection {
				t.Fatalf("decision = %+v", decision)
			}
			if len(decision.policyReasons) != 1 || decision.policyReasons[0] != ReasonReadOnlySubjectCannotInvoke {
				t.Fatalf("reasons = %v", decision.policyReasons)
			}
		})
	}
}

func TestNormalizeProvisionInputIsDeterministicAndActorIdempotencyIsExternal(t *testing.T) {
	input := ProvisionInput{
		ClientRequestID: "request-1", OrgID: 7, ProjectID: 9,
		Subject: Subject{Type: SubjectUser, UserID: 11}, RequestKind: RequestKindHumanPersonal,
		Envelope: Envelope{
			ProjectRole: auth.ProjectRoleUser,
			ScopeMode:   accessscope.ModeExplicit,
			Models:      []string{"b", "a", "a"},
			Endpoints:   []string{"/v1/models"},
		},
		Environment: PersonalEnvironment, SubmissionMode: SubmissionSelf,
	}
	normalized, digest, err := normalizeProvisionInput(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized.Envelope.Models) != 2 || normalized.Envelope.Models[0] != "a" || normalized.Envelope.Models[1] != "b" {
		t.Fatalf("models = %v", normalized.Envelope.Models)
	}
	_, secondDigest, err := normalizeProvisionInput(normalized)
	if err != nil || digest != secondDigest {
		t.Fatalf("digest changed: %q %q err=%v", digest, secondDigest, err)
	}
}

func TestNormalizeHumanProvisionRejectsSponsorEvidenceForEverySubmissionMode(t *testing.T) {
	for _, mode := range []string{SubmissionSelf, SubmissionDelegated, SubmissionDirectGrant, SubmissionMigration, SubmissionBootstrap, SubmissionRecovery} {
		t.Run(mode, func(t *testing.T) {
			input := ProvisionInput{
				ClientRequestID: "human-sponsor-" + mode, OrgID: 7, ProjectID: 9,
				Subject: Subject{Type: SubjectUser, UserID: 11}, RequestKind: RequestKindHumanPersonal,
				Envelope:    Envelope{ProjectRole: auth.ProjectRoleUser, ScopeMode: accessscope.ModeProjectDefault},
				Environment: PersonalEnvironment, SubmissionMode: mode, SponsorUserID: 11,
			}
			if _, _, err := normalizeProvisionInput(input); err == nil {
				t.Fatal("human request accepted forged sponsor evidence")
			}
		})
	}
}

func TestNormalizeProvisionInputEnforcesLegacyInt4AndModelCSVBoundaries(t *testing.T) {
	max := int(MaxOperationalLimit)
	tooLarge := int(MaxOperationalLimit + 1)
	base := ProvisionInput{
		ClientRequestID: "boundary", OrgID: 7, ProjectID: 9,
		Subject: Subject{Type: SubjectUser, UserID: 11}, RequestKind: RequestKindHumanPersonal,
		Envelope: Envelope{
			ProjectRole: auth.ProjectRoleUser, ScopeMode: accessscope.ModeExplicit,
			Models: []string{"model-a"}, Endpoints: []string{"/v1/models"},
		},
		Environment: PersonalEnvironment, SubmissionMode: SubmissionSelf,
	}
	for _, field := range []string{"rpm", "rps", "concurrency"} {
		t.Run(field+" max", func(t *testing.T) {
			input := base
			switch field {
			case "rpm":
				input.Envelope.RPMLimit = &max
			case "rps":
				input.Envelope.RPSLimit = &max
			default:
				input.Envelope.ConcurrencyLimit = &max
			}
			if _, _, err := normalizeProvisionInput(input); err != nil {
				t.Fatalf("max int4 rejected: %v", err)
			}
		})
		t.Run(field+" overflow", func(t *testing.T) {
			input := base
			switch field {
			case "rpm":
				input.Envelope.RPMLimit = &tooLarge
			case "rps":
				input.Envelope.RPSLimit = &tooLarge
			default:
				input.Envelope.ConcurrencyLimit = &tooLarge
			}
			if _, _, err := normalizeProvisionInput(input); err == nil {
				t.Fatal("int4 overflow accepted")
			}
		})
	}
	comma := base
	comma.Envelope.Models = []string{"model,a"}
	if _, _, err := normalizeProvisionInput(comma); err == nil {
		t.Fatal("comma-bearing model scope accepted")
	}
}

func TestGrantEnvelopeWideningAllowsOnlyEquivalentOrNarrowerRevisions(t *testing.T) {
	value := func(input int) *int { return &input }
	defaultEnvelope := func() Envelope {
		return Envelope{ProjectRole: auth.ProjectRoleUser, ScopeMode: accessscope.ModeProjectDefault}
	}
	explicitEnvelope := func(models, endpoints []string) Envelope {
		return Envelope{
			ProjectRole: auth.ProjectRoleUser, ScopeMode: accessscope.ModeExplicit,
			Models: models, Endpoints: endpoints,
		}
	}
	tests := []struct {
		name      string
		requested Envelope
		baseline  *Grant
		want      grantEnvelopeWidening
	}{
		{name: "initial project defaults", requested: defaultEnvelope()},
		{name: "explicit baseline to project default", requested: defaultEnvelope(), baseline: &Grant{Envelope: explicitEnvelope([]string{"model-a"}, []string{"/v1/chat/completions"})}, want: grantEnvelopeWidening{scope: true}},
		{name: "explicit baseline adds model", requested: explicitEnvelope([]string{"model-a", "model-b"}, []string{"/v1/chat/completions"}), baseline: &Grant{Envelope: explicitEnvelope([]string{"model-a"}, []string{"/v1/chat/completions"})}, want: grantEnvelopeWidening{scope: true}},
		{name: "explicit baseline adds endpoint", requested: explicitEnvelope([]string{"model-a"}, []string{"/v1/chat/completions", "/v1/embeddings"}), baseline: &Grant{Envelope: explicitEnvelope([]string{"model-a"}, []string{"/v1/chat/completions"})}, want: grantEnvelopeWidening{scope: true}},
		{name: "explicit baseline narrowed", requested: explicitEnvelope([]string{"model-a"}, []string{"/v1/chat/completions"}), baseline: &Grant{Envelope: explicitEnvelope([]string{"model-a", "model-b"}, []string{"/v1/chat/completions", "/v1/embeddings"})}},
		{name: "project default to explicit is narrower", requested: explicitEnvelope([]string{"model-a"}, []string{"/v1/chat/completions"}), baseline: &Grant{Envelope: defaultEnvelope()}},
		{name: "role expands", requested: func() Envelope { e := defaultEnvelope(); e.ProjectRole = auth.RoleProjectAdmin; return e }(), baseline: &Grant{Envelope: defaultEnvelope()}, want: grantEnvelopeWidening{role: true}},
		{name: "rpm above default", requested: func() Envelope {
			e := defaultEnvelope()
			e.RPMLimit = value(accessscope.ProjectDefaultRPMLimit + 1)
			return e
		}(), want: grantEnvelopeWidening{rateOrConcurrency: true}},
		{name: "rps above default", requested: func() Envelope {
			e := defaultEnvelope()
			e.RPSLimit = value(accessscope.ProjectDefaultRPSLimit + 1)
			return e
		}(), want: grantEnvelopeWidening{rateOrConcurrency: true}},
		{name: "baseline rate expansion", requested: func() Envelope { e := defaultEnvelope(); e.RPMLimit = value(31); return e }(), baseline: &Grant{Envelope: func() Envelope { e := defaultEnvelope(); e.RPMLimit = value(30); return e }()}, want: grantEnvelopeWidening{rateOrConcurrency: true}},
		{name: "baseline concurrency expansion", requested: func() Envelope { e := defaultEnvelope(); e.ConcurrencyLimit = value(3); return e }(), baseline: &Grant{Envelope: func() Envelope { e := defaultEnvelope(); e.ConcurrencyLimit = value(2); return e }()}, want: grantEnvelopeWidening{rateOrConcurrency: true}},
		{name: "baseline concurrency removal", requested: defaultEnvelope(), baseline: &Grant{Envelope: func() Envelope { e := defaultEnvelope(); e.ConcurrencyLimit = value(2); return e }()}, want: grantEnvelopeWidening{rateOrConcurrency: true}},
		{name: "baseline concurrency narrowing", requested: func() Envelope { e := defaultEnvelope(); e.ConcurrencyLimit = value(1); return e }(), baseline: &Grant{Envelope: func() Envelope { e := defaultEnvelope(); e.ConcurrencyLimit = value(2); return e }()}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := compareGrantEnvelope(test.requested, test.baseline); got != test.want {
				t.Fatalf("compareGrantEnvelope() = %+v, want %+v", got, test.want)
			}
		})
	}
}
