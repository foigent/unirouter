package modelcapabilitypublication

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/strictjson"
)

const BundleSchemaV2 = "unirouter.capability-bundle.v2"
const MaximumBundleBytes = 16 << 20

// BundleV2 is independent of a backend image and Target. It carries definitions
// and candidate contract deltas only: no executable code, credentials, Probe
// observations, approvals, Slot changes or customer prices.
type BundleV2 struct {
	SchemaVersion        string                `json:"schema_version"`
	MaterializerContract string                `json:"materializer_contract_version"`
	Definitions          []json.RawMessage     `json:"definitions"`
	BindingDelta         []DefinitionBindingV2 `json:"binding_delta"`
	SelectorDelta        []SelectorTransition  `json:"selector_delta"`
}

type DefinitionBindingV2 struct {
	Selector
	ProfileDefinitionDigest string `json:"profile_definition_digest"`
	ExpectedProvider        string `json:"expected_provider"`
	ExpectedUpstreamModel   string `json:"expected_upstream_model"`
	PublicPath              string `json:"public_path"`
}

func (b BundleV2) CanonicalBytes() ([]byte, error) {
	if b.SchemaVersion != BundleSchemaV2 || b.MaterializerContract != MaterializerContractV2 {
		return nil, fmt.Errorf("unsupported capability Bundle schema or materializer")
	}
	if len(b.Definitions)+len(b.BindingDelta)+len(b.SelectorDelta) == 0 || len(b.Definitions) > 1024 || len(b.BindingDelta) > 8192 || len(b.SelectorDelta) > 8192 {
		return nil, fmt.Errorf("capability Bundle delta size is invalid")
	}
	b.Definitions = slices.Clone(b.Definitions)
	b.BindingDelta = slices.Clone(b.BindingDelta)
	b.SelectorDelta = slices.Clone(b.SelectorDelta)
	definitions := map[string]bool{}
	addresses := map[string]bool{}
	for i, wire := range b.Definitions {
		d, err := modelprofile.DecodeDefinition(wire)
		if err != nil {
			return nil, err
		}
		p := d.Profile()
		address := p.ID + "@" + p.Version
		if definitions[d.Digest()] || addresses[address] {
			return nil, fmt.Errorf("duplicate definition address")
		}
		definitions[d.Digest()] = true
		addresses[address] = true
		b.Definitions[i] = d.CanonicalBytes()
	}
	slices.SortFunc(b.Definitions, func(a, b json.RawMessage) int { return bytes.Compare(a, b) })
	seenBindings := map[string]bool{}
	for _, binding := range b.BindingDelta {
		if !modelprofile.IsDefinitionDigest(binding.ProfileDefinitionDigest) {
			return nil, fmt.Errorf("binding requires exact definition digest")
		}
		for _, value := range []string{binding.OperationID, binding.CapabilityID, binding.ExecutionProfileID, binding.ExecutionProfileVersion, binding.ProtocolProfileID, binding.ProtocolProfileVersion, binding.ProbeContractID, binding.ProbeContractVersion, binding.ExpectedProvider, binding.ExpectedUpstreamModel, binding.PublicPath} {
			if err := validateText("binding", value); err != nil {
				return nil, err
			}
		}
		key := dataBindingKey(binding.Selector)
		if seenBindings[key] {
			return nil, fmt.Errorf("duplicate binding delta")
		}
		seenBindings[key] = true
	}
	slices.SortFunc(b.BindingDelta, func(a, b DefinitionBindingV2) int {
		return bytes.Compare([]byte(dataBindingKey(a.Selector)), []byte(dataBindingKey(b.Selector)))
	})
	seenSelectors := map[string]bool{}
	for _, transition := range b.SelectorDelta {
		key := dataSelectorKey(selectorFinalV2(transition))
		if seenSelectors[key] {
			return nil, fmt.Errorf("duplicate selector transition")
		}
		seenSelectors[key] = true
		for _, value := range []string{transition.OperationID, transition.CapabilityID, transition.ExecutionProfileID, transition.ExecutionProfileVersion, transition.ProtocolProfileID, transition.ProtocolProfileVersion, transition.Final.ProbeContractID, transition.Final.ProbeContractVersion} {
			if err := validateText("selector", value); err != nil {
				return nil, err
			}
		}
		if old := transition.ExpectedPredecessor; old != nil {
			if validateText("old Probe", old.ProbeContractID) != nil || validateText("old Probe version", old.ProbeContractVersion) != nil || *old == transition.Final {
				return nil, fmt.Errorf("selector delta is invalid or no-op")
			}
		}
	}
	slices.SortFunc(b.SelectorDelta, func(a, b SelectorTransition) int {
		return bytes.Compare([]byte(dataSelectorKey(selectorFinalV2(a))), []byte(dataSelectorKey(selectorFinalV2(b))))
	})
	wire, err := json.Marshal(b)
	if err != nil {
		return nil, err
	}
	if len(wire) > MaximumBundleBytes {
		return nil, fmt.Errorf("capability Bundle exceeds limit")
	}
	return strictjson.Canonicalize(wire)
}

func DecodeBundleV2(wire []byte) (BundleV2, error) {
	var bundle BundleV2
	if len(wire) == 0 || len(wire) > MaximumBundleBytes {
		return bundle, fmt.Errorf("invalid Bundle size")
	}
	if err := decodeStrict(wire, &bundle); err != nil {
		return bundle, err
	}
	canonical, err := bundle.CanonicalBytes()
	if err != nil {
		return bundle, err
	}
	// Decode is for authoring; the carrier must additionally compare canonical
	// bytes and the independently approved content digest before materializing.
	if err := json.Unmarshal(canonical, &bundle); err != nil {
		return BundleV2{}, err
	}
	return bundle, nil
}

func VerifyBundleV2(wire []byte, digest string) (BundleV2, error) {
	if !modelprofile.IsDefinitionDigest(digest) || sha256Hex(wire) != digest {
		return BundleV2{}, fmt.Errorf("Bundle content digest mismatch")
	}
	b, err := DecodeBundleV2(wire)
	if err != nil {
		return BundleV2{}, err
	}
	canonical, err := b.CanonicalBytes()
	if err != nil {
		return BundleV2{}, err
	}
	if !bytes.Equal(canonical, wire) {
		return BundleV2{}, fmt.Errorf("Bundle bytes must be canonical")
	}
	return b, nil
}

func dataSelectorKey(s Selector) string {
	wire, _ := json.Marshal([]string{s.OperationID, s.CapabilityID, s.ExecutionProfileID, s.ExecutionProfileVersion, s.ProtocolProfileID, s.ProtocolProfileVersion})
	return string(wire)
}
func dataBindingKey(s Selector) string {
	wire, _ := json.Marshal([]string{dataSelectorKey(s), s.ProbeContractID, s.ProbeContractVersion})
	return string(wire)
}
func selectorFinalV2(t SelectorTransition) Selector {
	return Selector{t.OperationID, t.CapabilityID, t.ExecutionProfileID, t.ExecutionProfileVersion, t.ProtocolProfileID, t.ProtocolProfileVersion, t.Final.ProbeContractID, t.Final.ProbeContractVersion}
}
