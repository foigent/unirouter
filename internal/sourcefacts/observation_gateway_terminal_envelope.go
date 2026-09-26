package sourcefacts

import (
	"bytes"
	"encoding/json"
	"errors"
	"sync"

	contract "github.com/uniroute/unirouter/contracts/source-facts/pre-dispatch-v2"
	"github.com/uniroute/unirouter/internal/eventconnector"
)

const gatewayPreDispatchEventType = "execution.gateway_pre_dispatch_terminal.observed"
const gatewayPreDispatchSchemaRef = "foigent://events/unirouter/gateway-pre-dispatch-terminal/v2"

var gatewayPreDispatchDescriptor = sync.OnceValues(func() (eventconnector.Descriptor, error) {
	descriptor, ce := eventconnector.NewDescriptor(eventconnector.DescriptorBinding{
		ProtocolVersion: "2.0.2", ProtocolCommit: "ad8fe7a38c9d7335657630baa8670dd07556b98c",
		ReleaseDigest:        "sha256:99f8a541b6d33b0a74d2e4c06aa312c6d6c88c35a4bfb1d4d34b98b4e4e2e8e8",
		ManifestDigest:       eventconnector.SHA256Reference(contract.Manifest()),
		EnvelopeSpecVersion:  eventconnector.EventEnvelopeSpecVersion,
		EnvelopeSchemaDigest: "sha256:6942e85d9b5d8b08ff9828b970c317596394184ef29908cbf2b8fc792187cf26",
		SourceProduct:        "unirouter", ConnectorID: ConnectorID, ConnectorVersion: ObservationConnectorVersion,
		SourceSchema:  eventconnector.SchemaBinding{SchemaID: contract.SchemaVersion, SHA256: eventconnector.SHA256Reference(contract.Schema())},
		EmittedEvents: []eventconnector.EventSchemaBinding{{EventType: gatewayPreDispatchEventType, SchemaRef: gatewayPreDispatchSchemaRef, SchemaVersion: "2", SHA256: eventconnector.SHA256Reference(contract.Schema())}},
	})
	if ce != nil {
		return eventconnector.Descriptor{}, ce
	}
	return descriptor, nil
})

func GatewayPreDispatchEvent(d GatewayPreDispatchTerminal, wire []byte) (eventconnector.ImmutableEvent, error) {
	canonical, err := EncodeGatewayPreDispatchTerminal(d)
	if err != nil {
		return eventconnector.ImmutableEvent{}, err
	}
	if !bytes.Equal(canonical, wire) {
		return eventconnector.ImmutableEvent{}, errors.New("gateway pre-dispatch source bytes differ")
	}
	descriptor, err := gatewayPreDispatchDescriptor()
	if err != nil {
		return eventconnector.ImmutableEvent{}, err
	}
	artifact, ce := eventconnector.NewSourceArtifact(descriptor, d.SourceInstance, d.Revision, wire, eventconnector.StrictJSONOptions{RejectNull: true})
	if ce != nil {
		return eventconnector.ImmutableEvent{}, ce
	}
	var payload map[string]any
	dec := json.NewDecoder(bytes.NewReader(wire))
	dec.UseNumber()
	if err = dec.Decode(&payload); err != nil {
		return eventconnector.ImmutableEvent{}, err
	}
	p := func(v string) eventconnector.ProtocolString {
		x, _ := eventconnector.NewProtocolString("source", v)
		return x
	}
	o := func(v string) eventconnector.OpaqueString {
		x, _ := eventconnector.NewOpaqueString("source", v, 1, 128, true)
		return x
	}
	draft := eventconnector.EventDraft{EventID: o(d.EventID), EventType: p(gatewayPreDispatchEventType), SchemaRef: p(gatewayPreDispatchSchemaRef), SchemaVersion: p("2"), Scope: eventconnector.EventScope{OrgRef: d.OrgID, ProjectRef: d.ProjectID}, Subject: eventconnector.EventSubject{Type: p("request"), Ref: o(d.RequestID)}, Actor: map[string]any{"type": "unknown"}, Time: map[string]any{"occurred_at": d.OccurredAt, "observed_at": d.RecordedAt}, Correlation: map[string]any{"request_ref": d.RequestID}, SourceRevision: o(d.Revision), DataPolicy: map[string]any{"payload_capture_mode": "metadata_only", "raw_payload_present": false}, Provenance: map[string]any{"adapter_fidelity": "F2", "field_groups": map[string]any{"source_fact": "direct"}}, Payload: payload}
	event, ce := eventconnector.Finalize(descriptor, artifact, draft)
	if ce != nil {
		return eventconnector.ImmutableEvent{}, ce
	}
	return event, nil
}
