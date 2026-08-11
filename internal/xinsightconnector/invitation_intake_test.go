package xinsightconnector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type invitationFixtureIndex struct {
	Valid   []string `json:"valid"`
	Invalid []string `json:"invalid"`
}

func TestFrozenInvitationFixtures(t *testing.T) {
	root := filepath.Join("testdata", "invitations")
	var index invitationFixtureIndex
	decodeFrozenInvitationFixture(t, filepath.Join(root, "index.json"), &index)
	require.NotEmpty(t, index.Valid)
	require.NotEmpty(t, index.Invalid)
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

	for _, relative := range index.Valid {
		t.Run("valid/"+relative, func(t *testing.T) {
			var invitation FrozenInvitationV1
			decodeFrozenInvitationFixture(t, filepath.Join(root, relative), &invitation)
			validated, err := validateFrozenInvitation(invitation, "https://xinsight.example", now)
			require.NoError(t, err)
			require.Equal(t, "xiiv1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", validated.token)
			require.Equal(t, hashBytes([]byte(validated.token)), validated.tokenSHA256)
			require.Equal(t, "xinsight-invitation-"+validated.tokenSHA256[7:], validated.idempotencyKey)
		})
	}

	for _, relative := range index.Invalid {
		t.Run("invalid/"+relative, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(root, relative))
			require.NoError(t, err)
			var invitation FrozenInvitationV1
			decodeErr := decodeStrictFrozenInvitation(raw, &invitation)
			if decodeErr == nil {
				_, decodeErr = validateFrozenInvitation(invitation, "https://xinsight.example", now)
			}
			require.Error(t, decodeErr)
		})
	}
}

func TestFrozenInvitationRejectsExpiryAndCanonicalDrift(t *testing.T) {
	var valid FrozenInvitationV1
	decodeFrozenInvitationFixture(t,
		filepath.Join("testdata", "invitations", "valid", "01-frozen-invitation.json"),
		&valid,
	)
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*FrozenInvitationV1)
	}{
		{name: "expired", mutate: func(value *FrozenInvitationV1) { value.ExpiresAt = now.Format(time.RFC3339) }},
		{name: "ttl exceeds frozen maximum", mutate: func(value *FrozenInvitationV1) {
			value.ExpiresAt = now.Add(30*time.Minute + time.Nanosecond).Format(time.RFC3339Nano)
		}},
		{name: "non-UTC timestamp", mutate: func(value *FrozenInvitationV1) { value.ExpiresAt = "2030-01-01T08:15:00+08:00" }},
		{name: "audience drift", mutate: func(value *FrozenInvitationV1) { value.Audience = "other" }},
		{name: "expected origin drift", mutate: func(value *FrozenInvitationV1) { value.XInsightOrigin = "https://other.example" }},
		{name: "fragment extension", mutate: func(value *FrozenInvitationV1) { value.InstallURL += "&next=https%3A%2F%2Fevil.example" }},
		{name: "query added", mutate: func(value *FrozenInvitationV1) {
			value.InstallURL = "https://unirouter.example/connectors/x-insight/install?next=evil#invitation=xiiv1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&xinsight_origin=https%3A%2F%2Fxinsight.example"
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			candidate := valid
			candidate.RequestedEventScope = append([]string(nil), valid.RequestedEventScope...)
			testCase.mutate(&candidate)
			_, err := validateFrozenInvitation(candidate, "https://xinsight.example", now)
			require.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestInvitationIntakeResumesAfterStagingFailure(t *testing.T) {
	var invitation FrozenInvitationV1
	decodeFrozenInvitationFixture(t,
		filepath.Join("testdata", "invitations", "valid", "01-frozen-invitation.json"),
		&invitation,
	)
	creator := &recordingInvitationCreator{
		installation: Installation{InstallationID: invitation.InstallationID},
		operation:    Operation{OperationRef: "xcop_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}
	stager := &recordingInvitationStager{failFirst: true}
	intake := &InvitationIntake{
		creator: creator, stager: stager,
		expectedXInsightOrigin: "https://xinsight.example",
		runtimeRole:            "stable",
		now: func() time.Time {
			return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
		},
	}
	input := InvitationIntakeInput{
		Invitation: invitation, OrgID: 7, ProjectID: 11, ConsentedByUserID: 13,
	}
	_, err := intake.Accept(context.Background(), input)
	require.ErrorIs(t, err, ErrUnavailable)
	result, err := intake.Accept(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, invitation.InstallationID, result.Installation.InstallationID)
	require.Equal(t, 2, creator.calls)
	require.Equal(t, 2, stager.calls)
	require.Equal(t, creator.consents[0], creator.consents[1])
	require.Equal(t, invitation.InstallationID, stager.installationID)
	require.Equal(t, "xiiv1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", stager.token)
}

func TestInvitationIntakeFailsClosedBeforeDependencies(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		origin string
		role   string
	}{
		{name: "origin missing", role: "stable"},
		{name: "non-stable role", origin: "https://xinsight.example", role: "canary"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			creator := &recordingInvitationCreator{}
			stager := &recordingInvitationStager{}
			intake := &InvitationIntake{
				creator: creator, stager: stager,
				expectedXInsightOrigin: testCase.origin, runtimeRole: testCase.role,
				now: time.Now,
			}
			_, err := intake.Accept(context.Background(), InvitationIntakeInput{})
			require.ErrorIs(t, err, ErrUnavailable)
			require.Zero(t, creator.calls)
			require.Zero(t, stager.calls)
		})
	}
}

type recordingInvitationCreator struct {
	installation Installation
	operation    Operation
	consents     []Consent
	calls        int
}

func (creator *recordingInvitationCreator) CreateInstallation(
	_ context.Context,
	consent Consent,
) (Installation, Operation, error) {
	creator.calls++
	creator.consents = append(creator.consents, consent)
	return creator.installation, creator.operation, nil
}

type recordingInvitationStager struct {
	failFirst      bool
	calls          int
	installationID string
	token          string
}

func (stager *recordingInvitationStager) StageInvitation(
	_ context.Context,
	installationID, token string,
) error {
	stager.calls++
	stager.installationID = installationID
	stager.token = token
	if stager.failFirst && stager.calls == 1 {
		return ErrUnavailable
	}
	return nil
}

func decodeFrozenInvitationFixture(t *testing.T, path string, destination any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, decodeStrictFrozenInvitation(raw, destination))
}

func decodeStrictFrozenInvitation(raw []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("fixture contains trailing JSON")
	}
	return nil
}
