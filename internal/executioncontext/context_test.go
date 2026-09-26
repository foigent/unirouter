package executioncontext

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func boundedSnapshot(fill string) Snapshot {
	id := "9223372036854775807"
	value := Snapshot{SchemaVersion: Version, Core: Core{
		RequestID: strings.Repeat(fill, 64), OrgID: id, ProjectID: id, APIKeyID: id,
		CredentialID: id, WorkloadApplicationID: id, CredentialRef: strings.Repeat(fill, 128),
		KeyType: "personal", AuthKind: strings.Repeat(fill, 64), AuthEpoch: id,
		Operation: strings.Repeat(fill, 64), Protocol: strings.Repeat(fill, 64),
		CapturedAt: "2026-09-09T00:00:00.000000Z",
	}, Identity: Identity{
		OwnerUserID: id, GrantID: id, GrantRevision: id, GrantSubjectUserID: id,
		GrantSubjectWorkloadID: id, SponsorUserID: id, ActualHuman: "unknown",
		GrantSubjectType: strings.Repeat(fill, 128), WorkflowID: strings.Repeat(fill, 128),
		RootAgentID: strings.Repeat(fill, 128), AgentID: strings.Repeat(fill, 128),
		ParentAgentID: strings.Repeat(fill, 128), AuthorityVersion: strings.Repeat(fill, 128),
		AuthorityRead: strings.Repeat(fill, 64), Lineage: make([]string, 32),
	}}
	for i := range value.Identity.Lineage {
		value.Identity.Lineage[i] = strings.Repeat(fill, 128)
	}
	return value
}

func TestSnapshotEncodingBoundAndInvalidIdentity(t *testing.T) {
	value := boundedSnapshot("a")
	require.NoError(t, value.Validate())
	wire, err := json.Marshal(value)
	require.NoError(t, err)
	require.LessOrEqual(t, len(wire), 32<<10)
	require.Contains(t, string(wire), `"api_key_id":"9223372036854775807"`)
	// JSON HTML escaping amplifies these individually legal fields. The whole
	// persisted snapshot must still reject the oversized combination.
	require.ErrorContains(t, boundedSnapshot("<").Validate(), "exceeds bound")
	for _, mutate := range []func(*Snapshot){
		func(s *Snapshot) { s.Identity.OwnerUserID = "9223372036854775808" },
		func(s *Snapshot) { s.Core.AuthEpoch = "01" },
		func(s *Snapshot) { s.Identity.AgentID = "agent\nsecond" },
		func(s *Snapshot) { s.Identity.AgentID = string([]byte{0xff}) },
		func(s *Snapshot) { s.Identity.Lineage = append(s.Identity.Lineage, "too-deep") },
		func(s *Snapshot) { s.Identity.ActualHuman = "owner" },
	} {
		candidate := boundedSnapshot("a")
		mutate(&candidate)
		require.Error(t, candidate.Validate())
	}
}

func TestSnapshotLineageCannotBeMutatedThroughContext(t *testing.T) {
	value := boundedSnapshot("a")
	ctx := WithSnapshot(context.Background(), value)
	value.Identity.Lineage[0] = "changed-after-auth"
	first, ok := FromContext(ctx)
	require.True(t, ok)
	require.Equal(t, strings.Repeat("a", 128), first.Identity.Lineage[0])
	first.Identity.Lineage[0] = "changed-after-read"
	second, ok := FromContext(ctx)
	require.True(t, ok)
	require.Equal(t, strings.Repeat("a", 128), second.Identity.Lineage[0])
}
