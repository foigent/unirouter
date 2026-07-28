package completedfeed

import (
	"bytes"
	"errors"
	"fmt"
)

type ReferenceMode string

const (
	ReferenceModeLegacy        ReferenceMode = "legacy"
	ReferenceModeShadow        ReferenceMode = "shadow"
	ReferenceModeNeutralCanary ReferenceMode = "neutral_canary"
)

const (
	ReferenceProtocolVersionV1 = "1.0.0"
	ReferenceConnectorIDV1     = "foigent.unirouter.completed-execution"
)

const (
	ReferenceProjectionMatch             = "match"
	ReferenceProjectionLegacyError       = "legacy_error"
	ReferenceProjectionNeutralError      = "neutral_error"
	ReferenceProjectionCanonicalMismatch = "canonical_mismatch"
	ReferenceProjectionEnvelopeMismatch  = "envelope_mismatch"
	ReferenceProjectionIdentityMismatch  = "identity_mismatch"
)

// ReferenceRuntimeBinding identifies the exact protocol, Connector and Schema
// authority that produced a neutral Reference result. It is runtime
// configuration only and never enters a Feed item, Cursor or retained row.
type ReferenceRuntimeBinding struct {
	ProtocolVersion      string
	ProtocolCommit       string
	ReleaseDigest        string
	ManifestDigest       string
	EnvelopeSpecVersion  string
	EnvelopeSchemaDigest string
	SourceProduct        string
	ConnectorID          string
	ConnectorVersion     string
	SourceSchemaID       string
	SourceSchemaDigest   string
	EventType            string
	EventSchemaRef       string
	EventSchemaVersion   string
	EventSchemaDigest    string
}

var expectedReferenceRuntimeBindingV1 = ReferenceRuntimeBinding{
	ProtocolVersion:      ReferenceProtocolVersionV1,
	ProtocolCommit:       "c09bc93cc7750e2333752655d6b4e69802e6769d",
	ReleaseDigest:        "sha256:aaeb5b37b94a1fd603048d77b2180fa88be1ddbd6eeea6a1369edf0cea9c5e2b",
	ManifestDigest:       "sha256:a3bc97bd5d5c7aab6452de9518be0b00f62776877deb431a91c175da5287de79",
	EnvelopeSpecVersion:  "foigent.event-envelope.v1",
	EnvelopeSchemaDigest: "sha256:6942e85d9b5d8b08ff9828b970c317596394184ef29908cbf2b8fc792187cf26",
	SourceProduct:        "unirouter",
	ConnectorID:          ReferenceConnectorIDV1,
	ConnectorVersion:     "1.0.0",
	SourceSchemaID:       SnapshotSchemaVersion,
	SourceSchemaDigest:   "sha256:4ae2897d4f8470c38624dfe21c54d72eaf6d995d2c9578c4420c94d244424fb2",
	EventType:            "model.request.completed",
	EventSchemaRef:       "foigent://events/model/request-completed/v1",
	EventSchemaVersion:   "1",
	EventSchemaDigest:    "sha256:ea0ea7354f3d9f435bb86a34eb28284e4297e5a2d1a3d22a23058c7dcc514dc6",
}

func ExpectedReferenceRuntimeBindingV1() ReferenceRuntimeBinding {
	return expectedReferenceRuntimeBindingV1
}

// ReferenceProjector maps an immutable copy of exact retained Source bytes.
// The caller keeps the retained bytes authoritative and unchanged.
type ReferenceProjector func([]byte, string) (ReferenceEnvelopeV1Result, error)

type ReferenceProjectionObserver func(ReferenceMode, string) error

type ServiceOption func(*Service) error

// WithReferenceProjection enables the Phase 2C Reference comparison seam.
// Legacy mode must not initialize a neutral projector. Shadow and canary modes
// require the exact pinned neutral Adapter binding.
func WithReferenceProjection(
	mode string,
	projector ReferenceProjector,
	bindings ...ReferenceRuntimeBinding,
) ServiceOption {
	return func(service *Service) error {
		referenceMode, err := parseReferenceMode(mode)
		if err != nil {
			return err
		}
		if referenceMode == ReferenceModeLegacy {
			if projector != nil || len(bindings) != 0 {
				return errors.New("completed-execution legacy Reference mode does not accept a neutral projector or binding")
			}
			service.referenceMode = ReferenceModeLegacy
			service.referenceProjector = nil
			service.referenceBinding = expectedReferenceRuntimeBindingV1
			return nil
		}
		if projector == nil {
			return errors.New("completed-execution Reference runtime projector is required")
		}
		if len(bindings) != 1 {
			return errors.New("completed-execution Reference runtime requires exactly one binding")
		}
		if bindings[0] != expectedReferenceRuntimeBindingV1 {
			return errors.New("completed-execution Reference runtime binding does not match the pinned contract")
		}
		service.referenceMode = referenceMode
		service.referenceProjector = projector
		service.referenceBinding = bindings[0]
		return nil
	}
}

func parseReferenceMode(value string) (ReferenceMode, error) {
	mode := ReferenceMode(value)
	switch mode {
	case ReferenceModeLegacy, ReferenceModeShadow, ReferenceModeNeutralCanary:
		return mode, nil
	default:
		return "", fmt.Errorf("completed-execution reference mode %q is invalid", value)
	}
}

func (service *Service) ReferenceMode() ReferenceMode {
	if service == nil || service.referenceMode == "" {
		return ReferenceModeLegacy
	}
	return service.referenceMode
}

func (service *Service) compareReferenceProjection(snapshotBytes []byte) string {
	legacy, err := BuildReferenceEnvelopeV1(snapshotBytes, service.sourceInstance)
	if err != nil {
		return ReferenceProjectionLegacyError
	}
	if service == nil || service.referenceProjector == nil {
		return ReferenceProjectionNeutralError
	}
	neutral, err := callReferenceProjector(
		service.referenceProjector,
		append([]byte(nil), snapshotBytes...),
		service.sourceInstance,
	)
	if err != nil {
		return ReferenceProjectionNeutralError
	}
	if !bytes.Equal(legacy.CanonicalBytes, neutral.CanonicalBytes) {
		return ReferenceProjectionCanonicalMismatch
	}
	if !bytes.Equal(legacy.EnvelopeBytes, neutral.EnvelopeBytes) ||
		legacy.EnvelopeHash != neutral.EnvelopeHash {
		return ReferenceProjectionEnvelopeMismatch
	}
	if service.referenceBinding != expectedReferenceRuntimeBindingV1 ||
		legacy.SourceHash != neutral.SourceHash ||
		legacy.IdempotencyKey != neutral.IdempotencyKey ||
		legacy.SourceRequestID != neutral.SourceRequestID {
		return ReferenceProjectionIdentityMismatch
	}
	return ReferenceProjectionMatch
}

func callReferenceProjector(
	projector ReferenceProjector,
	snapshotBytes []byte,
	sourceInstance string,
) (result ReferenceEnvelopeV1Result, err error) {
	defer func() {
		if recover() != nil {
			result = ReferenceEnvelopeV1Result{}
			err = errors.New("neutral Reference projector panicked")
		}
	}()
	return projector(snapshotBytes, sourceInstance)
}

func (service *Service) observeReferenceProjection(mode ReferenceMode, result string) {
	if service == nil || service.referenceObserve == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	_ = service.referenceObserve(mode, result)
}

func observeReferenceProjection(mode ReferenceMode, result string) error {
	feedReferenceProjectionComparisons.WithLabelValues(string(mode), result).Inc()
	return nil
}
