package sourcefacts

import (
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"

	mediaassets "github.com/uniroute/unirouter/contracts/source-facts/media-charge-v1"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/mediajob"
)

const MediaChargeProfile = mediaassets.ID
const mediaChargeKind = "media_charge"

var mediaSettlementReference = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

// These keys are the optional tags accepted for archived observations. New
// writers need only choose one of the metering combinations in the shared schema.
var mediaMeterUnits = map[string][]endpointprotocol.UnitClass{
	"openai.gpt-image.v1":       {endpointprotocol.UnitInputTokens, endpointprotocol.UnitInputTextTokens, endpointprotocol.UnitInputImageTokens, endpointprotocol.UnitOutputImageTokens, "output_images"},
	"alibaba.qwen-image-3.0.v1": {"input_images", "output_images"},
	"alibaba.wan-video.v1":      {"input_video_milliseconds", "output_video_milliseconds", "input_images"},
	"deepseek.ocr.v1":           {"recognized_pages"},
	"deepseek.asr.v1":           {"input_audio_milliseconds"},
	"deepseek.tts.v1":           {"input_billable_characters"},
}

func validMediaUnitCombination(tag string, units map[endpointprotocol.UnitClass]int64) bool {
	matches := func(allowed []endpointprotocol.UnitClass) bool {
		for unit := range units {
			if !slices.Contains(allowed, unit) {
				return false
			}
		}
		return true
	}
	if tag != "" {
		allowed, known := mediaMeterUnits[tag]
		return known && matches(allowed)
	}
	for _, allowed := range mediaMeterUnits {
		if matches(allowed) {
			return true
		}
	}
	return matches([]endpointprotocol.UnitClass{endpointprotocol.UnitInputTokens, endpointprotocol.UnitOutputTokens, "input_audio_milliseconds"}) ||
		matches([]endpointprotocol.UnitClass{endpointprotocol.UnitInputTokens, endpointprotocol.UnitInputTextTokens, endpointprotocol.UnitInputAudioTokens, endpointprotocol.UnitOutputTokens})
}

type MediaCustomerCharge struct {
	AmountMicros  string `json:"amount_micros"`
	Currency      string `json:"currency"`
	AmountScale   int    `json:"amount_scale"`
	SettlementRef string `json:"settlement_ref"`
}

type MediaChargeObservation struct {
	CustomerCharge *MediaCustomerCharge               `json:"customer_charge,omitempty"`
	AmountState    string                             `json:"amount_state"`
	ChargeFactID   string                             `json:"charge_fact_id,omitempty"`
	JobID          string                             `json:"job_id"`
	MeterProfile   string                             `json:"meter_profile,omitempty"`
	TerminalStatus string                             `json:"terminal_status"`
	Units          []endpointprotocol.UnitObservation `json:"units"`
}

func (p MediaChargeObservation) validate() error {
	_, known := mediaMeterUnits[p.MeterProfile]
	if p.MeterProfile != "" && !known || p.Units == nil || len(p.Units) > 5 || !mediajob.ValidJobID(p.JobID) ||
		!oneOf(p.TerminalStatus, mediajob.OutcomeSucceeded, mediajob.OutcomeFailed, mediajob.OutcomeUnknown, mediajob.OutcomeNotStarted) {
		return errors.New("invalid media charge payload")
	}
	if p.AmountState == mediajob.SettlementNotEligible {
		if p.ChargeFactID == "" && len(p.Units) == 0 && p.CustomerCharge == nil {
			return nil
		}
		return errors.New("unexpected media charge fact")
	}
	if !oneOf(p.AmountState, mediajob.ChargeAmountPricingPending, "charged") || p.TerminalStatus != mediajob.OutcomeSucceeded ||
		p.ChargeFactID != "mediacharge_"+strings.TrimPrefix(p.JobID, "mediajob_") {
		return errors.New("invalid pending media charge identity")
	}
	if (p.AmountState == "charged") != (p.CustomerCharge != nil) {
		return errors.New("media charge amount is inconsistent with settlement state")
	}
	if charge := p.CustomerCharge; charge != nil {
		if _, ok := mediaQuantity(charge.AmountMicros); !ok || charge.AmountScale != 1_000_000 ||
			charge.Currency != "CNY" || !mediaSettlementReference.MatchString(charge.SettlementRef) {
			return errors.New("invalid settled media amount")
		}
	}
	seen := map[endpointprotocol.UnitClass]int64{}
	for _, u := range p.Units {
		_, duplicate := seen[u.Class]
		value, ok := mediaQuantity(u.Quantity)
		if !ok || duplicate ||
			u.Authority != "provider_reported" || u.Finality != "final" || u.ChargeState != "provider_observed_not_customer_chargeable" {
			return errors.New("invalid authoritative media unit")
		}
		seen[u.Class] = value
	}
	if !validMediaUnitCombination(p.MeterProfile, seen) {
		return errors.New("invalid media unit combination")
	}
	if input, hasInput := seen[endpointprotocol.UnitInputTokens]; hasInput {
		text, hasText := seen[endpointprotocol.UnitInputTextTokens]
		media, hasMedia := seen[endpointprotocol.UnitInputImageTokens]
		if audio, hasAudio := seen[endpointprotocol.UnitInputAudioTokens]; hasAudio {
			media, hasMedia = audio, true
		}
		if text > input || media > input-text || hasText && hasMedia && media != input-text {
			return errors.New("inconsistent media token partition")
		}
	}
	return nil
}

func mediaQuantity(raw string) (int64, bool) {
	value, err := strconv.ParseInt(raw, 10, 64)
	return value, err == nil && value >= 0 && strconv.FormatInt(value, 10) == raw
}

func (d Observation) validateMediaChargeObservation() error {
	if d.SchemaVersion != MediaChargeProfile || d.Kind != mediaChargeKind || d.SubjectKind != "media_job" ||
		d.MediaCharge == nil || d.SubjectID != d.MediaCharge.JobID || d.Coverage != "observed" ||
		d.CaptureExpected == nil || len(d.CaptureExpected) != 0 || d.ContextRef != "" || d.RouteEvidenceHash != "" ||
		d.Runtime != nil || d.Attempt != nil || d.Capability != nil || d.Policy != nil || d.Destination != nil || d.Measurement != nil ||
		!positiveID(d.OrgID) || !positiveID(d.ProjectID) || !positiveID(d.Revision) ||
		!opaque(d.SourceInstance, 128) || !opaque(d.RequestID, 64) || !observationTime(d.OccurredAt) || !observationTime(d.RecordedAt) ||
		d.OccurredAt > d.RecordedAt || d.SourceFactID != d.factID() || d.EventID != d.eventID() {
		return errors.New("invalid media charge observation")
	}
	if d.Revision == "1" {
		if d.PreviousRevision != "" || d.PreviousHash != "" || d.CorrectionReason != "" {
			return errors.New("initial media charge has predecessor fields")
		}
	} else {
		prev, _ := strconv.ParseInt(d.PreviousRevision, 10, 64)
		rev, _ := strconv.ParseInt(d.Revision, 10, 64)
		if !positiveID(d.PreviousRevision) || prev != rev-1 || !validHash(d.PreviousHash) || !opaque(d.CorrectionReason, 128) {
			return errors.New("invalid prior media charge revision")
		}
	}
	return d.MediaCharge.validate()
}

func (s *Service) mediaChargeObservation(job mediajob.Job) (Observation, error) {
	if !job.Terminal() || job.TerminalAt == nil || job.TerminalAt.IsZero() || !endpointprotocol.IsMediaJobProfileID(job.ProfileID) {
		return Observation{}, errors.New("media charge source requires a supported terminal job")
	}
	p := MediaChargeObservation{AmountState: mediajob.SettlementNotEligible, JobID: job.JobID, TerminalStatus: job.Lifecycle.Outcome, Units: []endpointprotocol.UnitObservation{}}
	recorded := *job.TerminalAt
	if fact := job.ChargeFact; fact != nil {
		if job.Lifecycle.Settlement != mediajob.SettlementChargeFactRecorded || fact.AmountMicros != nil ||
			fact.Trigger != "provider_terminal_success_and_output_persisted" || fact.RecordedAt.Before(recorded) {
			return Observation{}, errors.New("unsupported media charge authority")
		}
		p.AmountState, p.ChargeFactID = fact.AmountState, fact.FactID
		p.Units = append(p.Units, fact.Units...)
		recorded = fact.RecordedAt
	} else if job.Lifecycle.Settlement != mediajob.SettlementNotEligible {
		return Observation{}, errors.New("terminal media charge fact is missing")
	}
	d := s.observation(job.Scope.OrgID, job.Scope.ProjectID, job.RequestID, MediaChargeProfile, "media_job", job.JobID, mediaChargeKind, *job.TerminalAt)
	d.SchemaVersion = MediaChargeProfile
	d.RecordedAt = executioncontext.Time(recorded)
	d.CaptureExpected = []string{}
	d.MediaCharge = &p
	d.SourceFactID, d.EventID = d.factID(), d.eventID()
	return d, d.Validate()
}
