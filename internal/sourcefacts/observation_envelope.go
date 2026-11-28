package sourcefacts

import (
	"bytes"
	"errors"
	"sync"

	mediaassets "github.com/uniroute/unirouter/contracts/source-facts/media-charge-v1"
	assets "github.com/uniroute/unirouter/contracts/source-facts/v2"
	"github.com/uniroute/unirouter/internal/eventconnector"
)

const ObservationConnectorVersion = "2.0.0"

func observationDescriptor(profile, schemaID string, schema, manifest []byte) (eventconnector.Descriptor, error) {
	typeName, schemaRef := observationEventBinding(profile)
	schemaDigest := eventconnector.SHA256Reference(schema)
	descriptor, err := eventconnector.NewDescriptor(eventconnector.DescriptorBinding{
		ProtocolVersion: "2.0.2", ProtocolCommit: "ad8fe7a38c9d7335657630baa8670dd07556b98c",
		ReleaseDigest:  "sha256:99f8a541b6d33b0a74d2e4c06aa312c6d6c88c35a4bfb1d4d34b98b4e4e2e8e8",
		ManifestDigest: eventconnector.SHA256Reference(manifest), EnvelopeSpecVersion: eventconnector.EventEnvelopeSpecVersion,
		EnvelopeSchemaDigest: "sha256:6942e85d9b5d8b08ff9828b970c317596394184ef29908cbf2b8fc792187cf26",
		SourceProduct:        "unirouter", ConnectorID: ConnectorID, ConnectorVersion: ObservationConnectorVersion,
		SourceSchema:  eventconnector.SchemaBinding{SchemaID: schemaID, SHA256: schemaDigest},
		EmittedEvents: []eventconnector.EventSchemaBinding{{EventType: typeName, SchemaRef: schemaRef, SchemaVersion: "1", SHA256: schemaDigest}},
	})
	if err != nil {
		return eventconnector.Descriptor{}, err
	}
	return descriptor, nil
}

var observationDescriptors = sync.OnceValues(func() (map[string]eventconnector.Descriptor, error) {
	result := map[string]eventconnector.Descriptor{}
	for _, profile := range v1ObservationProfiles() {
		d, err := observationDescriptor(profile, ObservationVersion, assets.Schema(), assets.Manifest())
		if err != nil {
			return nil, err
		}
		result[profile] = d
	}
	return result, nil
})

var mediaChargeDescriptor = sync.OnceValues(func() (eventconnector.Descriptor, error) {
	return observationDescriptor(MediaChargeProfile, MediaChargeProfile, mediaassets.Schema(), mediaassets.Manifest())
})

func descriptorForObservation(profile string) (eventconnector.Descriptor, error) {
	if profile == MediaChargeProfile {
		return mediaChargeDescriptor()
	}
	registry, err := observationDescriptors()
	return registry[profile], err
}

func observationEventBinding(profile string) (string, string) {
	switch profile {
	case MediaChargeProfile:
		return "execution.media_charge.recorded", "foigent://events/unirouter/media-charge/v1"
	case RuntimeProfile:
		return "execution.runtime.observed", "foigent://events/unirouter/runtime-observation/v1"
	case AttemptProfile:
		return "execution.attempt.observed", "foigent://events/unirouter/attempt-observation/v1"
	case CapabilityProfile:
		return "execution.capability.recorded", "foigent://events/unirouter/capability-snapshot/v1"
	case PolicyProfile:
		return "execution.policy.observed", "foigent://events/unirouter/policy-observation/v1"
	case DestinationProfile:
		return "execution.destination.observed", "foigent://events/unirouter/destination-observation/v1"
	}
	return "", ""
}

// ObservationEvent projects only the fixed source profile; delivery metadata never enters
// the source hash or semantic envelope hash.
func ObservationEvent(d Observation, wire []byte) (eventconnector.ImmutableEvent, error) {
	if e := d.Validate(); e != nil {
		return eventconnector.ImmutableEvent{}, e
	}
	canonical, e := EncodeObservation(d)
	if e != nil {
		return eventconnector.ImmutableEvent{}, e
	}
	if !bytes.Equal(canonical, wire) {
		return eventconnector.ImmutableEvent{}, errors.New("source projection bytes differ")
	}
	descriptor, e := descriptorForObservation(d.Profile)
	if e != nil {
		return eventconnector.ImmutableEvent{}, e
	}
	artifact, ce := eventconnector.NewSourceArtifact(descriptor, d.SourceInstance, d.Revision, wire, eventconnector.StrictJSONOptions{RejectNull: true})
	if ce != nil {
		return eventconnector.ImmutableEvent{}, ce
	}
	payload, e := observationPayload(d, wire)
	if e != nil {
		return eventconnector.ImmutableEvent{}, e
	}
	typeName, schema := observationEventBinding(d.Profile)
	p := func(v string) eventconnector.ProtocolString {
		value, _ := eventconnector.NewProtocolString("source", v)
		return value
	}
	o := func(v string) eventconnector.OpaqueString {
		value, _ := eventconnector.NewOpaqueString("source", v, 1, 128, true)
		return value
	}
	provenance := "direct"
	if d.Coverage != "observed" {
		provenance = "gap"
	}
	draft := eventconnector.EventDraft{EventID: o(d.EventID), EventType: p(typeName), SchemaRef: p(schema), SchemaVersion: p("1"), Scope: eventconnector.EventScope{OrgRef: d.OrgID, ProjectRef: d.ProjectID}, Subject: eventconnector.EventSubject{Type: p(d.SubjectKind), Ref: o(d.SubjectID)}, Actor: map[string]any{"type": "unknown"}, Time: map[string]any{"occurred_at": d.OccurredAt, "observed_at": d.RecordedAt}, Correlation: map[string]any{"request_ref": d.RequestID}, SourceRevision: o(d.Revision), DataPolicy: map[string]any{"payload_capture_mode": "metadata_only", "raw_payload_present": false}, Provenance: map[string]any{"adapter_fidelity": "F2", "field_groups": map[string]any{"source_fact": provenance}}, Payload: payload}
	event, ce := eventconnector.Finalize(descriptor, artifact, draft)
	if ce != nil {
		return eventconnector.ImmutableEvent{}, ce
	}
	return event, nil
}
