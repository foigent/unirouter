package modelcapabilityqualification

import (
	"bytes"
	"fmt"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

// ValidateBundleSource is the offline CI gate for canonical definitions and
// installed bindings. References to retained Target definitions are resolved
// again by the single materializer during exact Plan/apply.
func ValidateBundleSource(wire []byte) error {
	b, err := publication.DecodeBundleV2(wire)
	if err != nil {
		return err
	}
	canonical, err := b.CanonicalBytes()
	if err != nil {
		return err
	}
	if !bytes.Equal(wire, canonical) {
		return fmt.Errorf("CI Bundle source must be canonical bytes")
	}
	definitions := map[string]modelprofile.Definition{}
	for _, wire := range b.Definitions {
		d, err := modelprofile.DecodeDefinition(wire)
		if err != nil {
			return err
		}
		definitions[d.Digest()] = d
	}
	for _, binding := range b.BindingDelta {
		if d, ok := definitions[binding.ProfileDefinitionDigest]; ok {
			if err := validateBundleBinding(binding, d); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateBundleBinding(b publication.DefinitionBindingV2, d modelprofile.Definition) error {
	_, err := BindDefinition(Binding{OperationID: b.OperationID, CapabilityID: b.CapabilityID, ExecutionProfileID: b.ExecutionProfileID, ExecutionProfileVersion: b.ExecutionProfileVersion, ProtocolProfileID: b.ProtocolProfileID, ProtocolProfileVersion: b.ProtocolProfileVersion, ProbeContractID: b.ProbeContractID, ProbeContractVersion: b.ProbeContractVersion, ExpectedProvider: b.ExpectedProvider, ExpectedUpstreamModel: b.ExpectedUpstreamModel, PublicPath: b.PublicPath}, d)
	return err
}
