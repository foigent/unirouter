package runtimegeneration

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

func TestMiniMaxM3TierReleaseNeedsNewRuntimeImplementation(t *testing.T) {
	wire, err := os.ReadFile("../../capability-bundles/minimax-m3-stream-tier.json")
	require.NoError(t, err)
	var bundle struct {
		Definitions []json.RawMessage `json:"definitions"`
	}
	require.NoError(t, json.Unmarshal(wire, &bundle))
	require.Len(t, bundle.Definitions, 1)
	d, err := modelprofile.DecodeDefinition(bundle.Definitions[0])
	require.NoError(t, err)
	m := CurrentManifest([]string{"M114"})
	require.NoError(t, m.Require("M114", d.Requirements()))
	m.Implementations = slices.DeleteFunc(slices.Clone(m.Implementations), func(ref modelprofile.ImplementationReference) bool {
		return ref.Kind == "chat" && ref.ID == "protocol.minimax-m3-openai-chat.v11"
	})
	require.Error(t, m.Require("M114", d.Requirements()), "same schema and epoch cannot substitute for missing typed behavior")
}

func TestSupportIsImplementationInclusionNotCatalogOrWholeDigest(t *testing.T) {
	p, ok := modelprofile.Lookup("glm.5.3.api", "1.9.0")
	require.True(t, ok)
	p.Version = "99.0.0"
	d, err := modelprofile.NewDefinition(p, "glm/glm-5.3", "")
	require.NoError(t, err)
	m := CurrentManifest([]string{"M113", "M114"})
	require.NoError(t, m.Require("M113", d.Requirements()))
	require.NoError(t, m.Require("M114", d.Requirements()))
	require.Error(t, m.Require("M115", d.Requirements()))
	small := m
	small.Implementations = d.Requirements()
	a, err := m.Digest()
	require.NoError(t, err)
	b, err := small.Digest()
	require.NoError(t, err)
	require.NotEqual(t, a, b)
	require.NoError(t, small.Require("M113", d.Requirements()))
	small.Implementations = slices.Clone(small.Implementations[1:])
	require.Error(t, small.Require("M113", d.Requirements()))
	wire, err := m.CanonicalBytes()
	require.NoError(t, err)
	verified, err := VerifyManifest(wire, a)
	require.NoError(t, err)
	require.NoError(t, verified.Require("M113", d.Requirements()))
	_, err = VerifyManifest(append(wire, ' '), a)
	require.Error(t, err)
	_, err = VerifyManifest(wire, b)
	require.Error(t, err)
	// Advertisement and request lifetime no longer need a compiled model entry.
	_, ok = modelprofile.Lookup(p.ID, p.Version)
	require.False(t, ok)
	require.GreaterOrEqual(t, modelprofile.MaximumImplementedRequestTimeoutSeconds(), 1800)
}

func TestMiniMaxContinuationRequiresExplicitNewImplementation(t *testing.T) {
	wire, err := os.ReadFile("../../capability-bundles/minimax-tool-history-continuation.json")
	require.NoError(t, err)
	var bundle struct {
		Definitions []json.RawMessage `json:"definitions"`
	}
	require.NoError(t, json.Unmarshal(wire, &bundle))
	checked := 0
	for _, raw := range bundle.Definitions {
		d, err := modelprofile.DecodeDefinition(raw)
		require.NoError(t, err)
		p := d.Profile()
		if p.ID == "minimax.m3.payg" && p.Version == "1.4.0" {
			continue
		}
		m := CurrentManifest([]string{"M114"})
		require.NoError(t, m.Require("M114", d.Requirements()))
		ref := modelprofile.ImplementationReference{Kind: "reasoning", ID: p.ReasoningProfile}
		require.Contains(t, d.Requirements(), ref)
		m.Implementations = slices.DeleteFunc(slices.Clone(m.Implementations), func(v modelprofile.ImplementationReference) bool { return v == ref })
		require.Error(t, m.Require("M114", d.Requirements()), "old Artifact cannot claim support through already-known chat/M27 refs")
		checked++
	}
	require.Equal(t, 2, checked)
}

func TestKimiK3EffortPinRequiresNewProbeImplementation(t *testing.T) {
	ref := modelprofile.ImplementationReference{Kind: "capability_probe", ID: "unirouter.capability-probe.kimi-chat-stream-tools.v1@2.0.0"}
	m := CurrentManifest([]string{"M114"})
	require.NoError(t, m.Require("M114", []modelprofile.ImplementationReference{ref}))
	m.Implementations = slices.DeleteFunc(slices.Clone(m.Implementations), func(v modelprofile.ImplementationReference) bool { return v == ref })
	require.Error(t, m.Require("M114", []modelprofile.ImplementationReference{ref}))
	require.NoError(t, m.Require("M114", []modelprofile.ImplementationReference{{Kind: "capability_probe", ID: "unirouter.capability-probe.kimi-chat-stream-tools.v1@1.0.0"}}))
}

func TestCurrentDeepSeekFlashEffortPinRequiresV3Implementation(t *testing.T) {
	ref := modelprofile.ImplementationReference{Kind: "capability_probe", ID: "unirouter.capability-probe.deepseek-chat-stream-tools.v1@3.0.0"}
	m := CurrentManifest(schemaauthority.SupportedSchemaHeads())
	require.NoError(t, m.Require(schemaauthority.LatestSupportedMigration, []modelprofile.ImplementationReference{ref}))
	m.Implementations = slices.DeleteFunc(slices.Clone(m.Implementations), func(v modelprofile.ImplementationReference) bool { return v == ref })
	require.Error(t, m.Require(schemaauthority.LatestSupportedMigration, []modelprofile.ImplementationReference{ref}), "the historical runtime cannot claim the new data Probe")
	for _, version := range []string{"1.0.0", "2.0.0"} {
		require.NoError(t, m.Require(schemaauthority.LatestSupportedMigration, []modelprofile.ImplementationReference{{Kind: "capability_probe", ID: "unirouter.capability-probe.deepseek-chat-stream-tools.v1@" + version}}))
	}
}

func TestQwenStreamBundleRequiresChatAndProbeImplementations(t *testing.T) {
	wire, err := os.ReadFile("../../capability-bundles/qwen-flash-streamed-tools.json")
	require.NoError(t, err)
	var bundle struct {
		Definitions []json.RawMessage `json:"definitions"`
	}
	require.NoError(t, json.Unmarshal(wire, &bundle))
	require.Len(t, bundle.Definitions, 1)
	d, err := modelprofile.DecodeDefinition(bundle.Definitions[0])
	require.NoError(t, err)
	probe := modelprofile.ImplementationReference{Kind: "capability_probe", ID: "unirouter.capability-probe.qwen-chat-stream-tools.v1@1.0.0"}
	requirements := append(d.Requirements(), probe)
	m := CurrentManifest([]string{"M114"})
	require.NoError(t, m.Require("M114", requirements))
	for _, missing := range []modelprofile.ImplementationReference{{Kind: "chat", ID: "protocol.qwen-openai-chat.flash.v7"}, probe} {
		old := m
		old.Implementations = slices.DeleteFunc(slices.Clone(m.Implementations), func(ref modelprofile.ImplementationReference) bool { return ref == missing })
		require.Error(t, old.Require("M114", requirements), "old runtime cannot activate an unsupported Qwen Release")
	}
}

func TestQwenProviderCompletionRequiresNewRuntimeImplementations(t *testing.T) {
	requirements := []modelprofile.ImplementationReference{{Kind: "chat", ID: "protocol.qwen-openai-chat.flash.v8"}, {Kind: "capability_probe", ID: "unirouter.capability-probe.qwen-chat-stream-tools.v2@1.0.0"}}
	m := CurrentManifest([]string{"M114"})
	require.NoError(t, m.Require("M114", requirements))
	for _, missing := range requirements {
		old := m
		old.Implementations = slices.DeleteFunc(slices.Clone(m.Implementations), func(ref modelprofile.ImplementationReference) bool { return ref == missing })
		require.Error(t, old.Require("M114", requirements), "v7 chat or v1 probe support cannot substitute for the new output contract")
	}
}

func TestContentLeaseRequiresTypedLifecycleAndSchema(t *testing.T) {
	m := CurrentManifest(schemaauthority.SupportedSchemaHeads())
	require.NoError(t, requireLeaseManifest(m, schemaauthority.ContentCaptureExpandHead))
	old := m
	old.Implementations = slices.DeleteFunc(slices.Clone(m.Implementations), func(r modelprofile.ImplementationReference) bool { return r.Kind == "source_content" })
	require.ErrorIs(t, requireLeaseManifest(old, schemaauthority.ContentCaptureExpandHead), ErrIncompatible)
	require.NoError(t, requireLeaseManifest(old, schemaauthority.BudgetSnapshotExpandHead), "pre-C manifests remain valid at their original head")
	wrong := m
	wrong.SchemaHeads = slices.DeleteFunc(slices.Clone(m.SchemaHeads), func(head string) bool { return head == schemaauthority.ContentCaptureExpandHead })
	require.ErrorIs(t, requireLeaseManifest(wrong, schemaauthority.ContentCaptureExpandHead), ErrIncompatible)
	wrong = m
	wrong.Implementations = slices.Clone(m.Implementations)
	for i, ref := range wrong.Implementations {
		if ref.Kind == "source_content" {
			wrong.Implementations[i].ID = "unirouter.source-content-lifecycle.v0"
		}
	}
	require.ErrorIs(t, requireLeaseManifest(wrong, schemaauthority.ContentCaptureExpandHead), ErrIncompatible)
}

func TestPreDispatchLeaseRequiresV2Implementation(t *testing.T) {
	m := CurrentManifest(schemaauthority.SupportedSchemaHeads())
	require.NoError(t, requireLeaseManifest(m, schemaauthority.PreDispatchExpandHead))
	old := m
	old.Implementations = slices.DeleteFunc(slices.Clone(m.Implementations), func(ref modelprofile.ImplementationReference) bool {
		return ref.Kind == "source_observations" && ref.ID == "unirouter.source-observation.v2"
	})
	require.ErrorIs(t, requireLeaseManifest(old, schemaauthority.PreDispatchExpandHead), ErrIncompatible)
	require.NoError(t, requireLeaseManifest(old, schemaauthority.ContentReclaimExpandHead), "M120 generation retains its original contract")
	old = m
	old.SchemaHeads = slices.DeleteFunc(slices.Clone(m.SchemaHeads), func(head string) bool { return head == schemaauthority.PreDispatchExpandHead })
	require.ErrorIs(t, requireLeaseManifest(old, schemaauthority.PreDispatchExpandHead), ErrIncompatible)
}
