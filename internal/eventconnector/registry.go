package eventconnector

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

const RegistryPinVersion = "unirouter.event-connector-registry-pin.v1"

var requiredRegistryComponents = [...]string{
	"connector_manifest_schema",
	"event_envelope_schema",
	"event_batch_schema",
	"checkpoint_schema",
	"operation_receipt_schema",
	"error_schema",
	"canonical_vector_bundle",
	"conformance_contract",
	"source_projection_contract",
}

type ComponentPin struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type ProtocolPin struct {
	Repository    string                  `json:"repository"`
	Tag           string                  `json:"tag"`
	Commit        string                  `json:"commit"`
	Version       string                  `json:"version"`
	ReleaseDigest string                  `json:"release_digest"`
	Components    map[string]ComponentPin `json:"components"`
}

type RegistryConnector struct {
	LocalAdapterID        string               `json:"local_adapter_id"`
	ImplementationPackage string               `json:"implementation_package"`
	Profile               string               `json:"profile"`
	TransportProfile      string               `json:"transport_profile"`
	ConnectorID           string               `json:"connector_id"`
	ConnectorVersion      string               `json:"connector_version"`
	Manifest              ComponentPin         `json:"manifest"`
	SourceSchema          SchemaBinding        `json:"source_schema"`
	EmittedEvents         []EventSchemaBinding `json:"emitted_events"`
}

type RegistryPin struct {
	RegistryVersion string              `json:"registry_version"`
	Product         string              `json:"product"`
	Protocol        ProtocolPin         `json:"protocol"`
	Connectors      []RegistryConnector `json:"connectors"`
}

func DecodeRegistryPin(payload []byte) (RegistryPin, *ConnectorError) {
	if strictErr := ValidateStrictJSON(payload, StrictJSONOptions{RejectNull: true}); strictErr != nil {
		return RegistryPin{}, strictErr
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var registry RegistryPin
	if err := decoder.Decode(&registry); err != nil {
		return RegistryPin{}, contractError(
			"invalid_registry_pin",
			"configure",
			"decode_registry_pin",
			"Connector registry pin cannot be decoded",
			err,
		)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return RegistryPin{}, contractError(
			"invalid_registry_pin",
			"configure",
			"decode_registry_pin",
			"Connector registry pin contains trailing content",
			err,
		)
	}
	if err := registry.Validate(); err != nil {
		return RegistryPin{}, err
	}
	return registry, nil
}

func (registry RegistryPin) Validate() *ConnectorError {
	if registry.RegistryVersion != RegistryPinVersion {
		return registryError("registry_version is unsupported")
	}
	product, protocolErr := NewProtocolString("registry.product", registry.Product)
	if protocolErr != nil {
		return protocolErr
	}
	if !productPattern.MatchString(product.String()) || len(product.String()) > 64 {
		return registryError("registry product is invalid")
	}
	if registry.Protocol.Repository != "https://github.com/foigent/event-connector-spec" {
		return registryError("protocol repository is not the Binding authority")
	}
	if registry.Protocol.Tag == "" || registry.Protocol.Tag != "v"+registry.Protocol.Version {
		return registryError("protocol tag and version are inconsistent")
	}
	if !commitPattern.MatchString(registry.Protocol.Commit) ||
		!semverPattern.MatchString(registry.Protocol.Version) ||
		!sha256ReferencePattern.MatchString(registry.Protocol.ReleaseDigest) {
		return registryError("protocol identity is invalid")
	}
	for _, name := range requiredRegistryComponents {
		component, ok := registry.Protocol.Components[name]
		if !ok {
			return registryError("required protocol component is missing: " + name)
		}
		if componentErr := validateComponentPin(name, component); componentErr != nil {
			return componentErr
		}
	}
	if len(registry.Connectors) == 0 {
		return registryError("registry must contain at least one Connector")
	}
	localIDs := make(map[string]struct{}, len(registry.Connectors))
	portableIDs := make(map[string]struct{}, len(registry.Connectors))
	for _, connector := range registry.Connectors {
		if !utf8.ValidString(connector.LocalAdapterID) ||
			!connectorIDPattern.MatchString(connector.LocalAdapterID) ||
			len(connector.LocalAdapterID) > 200 {
			return registryError("local_adapter_id is invalid")
		}
		if !strings.HasPrefix(connector.ImplementationPackage, "github.com/uniroute/unirouter/internal/") ||
			len(connector.ImplementationPackage) > 240 {
			return registryError("implementation_package is outside the UniRouter private package boundary")
		}
		for name, value := range map[string]string{
			"profile":           connector.Profile,
			"transport_profile": connector.TransportProfile,
		} {
			protocolValue, err := NewProtocolString(name, value)
			if err != nil {
				return err
			}
			if !connectorIDPattern.MatchString(protocolValue.String()) || len(protocolValue.String()) > 200 {
				return registryError(name + " is invalid")
			}
		}
		if _, duplicate := localIDs[connector.LocalAdapterID]; duplicate {
			return registryError("local_adapter_id is duplicated")
		}
		localIDs[connector.LocalAdapterID] = struct{}{}
		portableID := connector.ConnectorID + "@" + connector.ConnectorVersion
		if _, duplicate := portableIDs[portableID]; duplicate {
			return registryError("portable Connector identity is duplicated")
		}
		portableIDs[portableID] = struct{}{}
		if componentErr := validateComponentPin("manifest", connector.Manifest); componentErr != nil {
			return componentErr
		}
		descriptor, descriptorErr := NewDescriptor(DescriptorBinding{
			ProtocolVersion:      registry.Protocol.Version,
			ProtocolCommit:       registry.Protocol.Commit,
			ReleaseDigest:        registry.Protocol.ReleaseDigest,
			ManifestDigest:       connector.Manifest.SHA256,
			EnvelopeSpecVersion:  EventEnvelopeSpecVersion,
			EnvelopeSchemaDigest: registry.Protocol.Components["event_envelope_schema"].SHA256,
			SourceProduct:        registry.Product,
			ConnectorID:          connector.ConnectorID,
			ConnectorVersion:     connector.ConnectorVersion,
			SourceSchema:         connector.SourceSchema,
			EmittedEvents:        connector.EmittedEvents,
		})
		if descriptorErr != nil || descriptor.key == "" {
			if descriptorErr != nil {
				return descriptorErr
			}
			return registryError("Connector descriptor is invalid")
		}
	}
	return nil
}

func (registry RegistryPin) Descriptor(localAdapterID string) (Descriptor, *ConnectorError) {
	if err := registry.Validate(); err != nil {
		return Descriptor{}, err
	}
	for _, connector := range registry.Connectors {
		if connector.LocalAdapterID != localAdapterID {
			continue
		}
		return NewDescriptor(DescriptorBinding{
			ProtocolVersion:      registry.Protocol.Version,
			ProtocolCommit:       registry.Protocol.Commit,
			ReleaseDigest:        registry.Protocol.ReleaseDigest,
			ManifestDigest:       connector.Manifest.SHA256,
			EnvelopeSpecVersion:  EventEnvelopeSpecVersion,
			EnvelopeSchemaDigest: registry.Protocol.Components["event_envelope_schema"].SHA256,
			SourceProduct:        registry.Product,
			ConnectorID:          connector.ConnectorID,
			ConnectorVersion:     connector.ConnectorVersion,
			SourceSchema:         connector.SourceSchema,
			EmittedEvents:        connector.EmittedEvents,
		})
	}
	return Descriptor{}, NewConnectorError(
		"configuration",
		"connector_not_registered",
		"configure",
		"resolve_connector",
		false,
		"Connector is not present in the static product registry",
		"operator_repair",
		nil,
	)
}

func (registry RegistryPin) ConnectorIDs() []string {
	ids := make([]string, 0, len(registry.Connectors))
	for _, connector := range registry.Connectors {
		ids = append(ids, connector.LocalAdapterID)
	}
	sort.Strings(ids)
	return ids
}

func validateComponentPin(name string, component ComponentPin) *ConnectorError {
	if component.Path == "" ||
		len(component.Path) > 240 ||
		strings.HasPrefix(component.Path, "/") ||
		strings.Contains(component.Path, "..") ||
		!utf8.ValidString(component.Path) {
		return registryError(name + " path is invalid")
	}
	if !sha256ReferencePattern.MatchString(component.SHA256) {
		return registryError(name + " digest is invalid")
	}
	return nil
}

func registryError(message string) *ConnectorError {
	return NewConnectorError(
		"configuration",
		"invalid_registry_pin",
		"configure",
		"validate_registry_pin",
		false,
		message,
		"operator_repair",
		fmt.Errorf("%s", message),
	)
}
