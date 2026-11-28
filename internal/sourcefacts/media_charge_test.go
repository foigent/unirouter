package sourcefacts

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/mediajob"
)

func settledMediaChargeFixture() Observation {
	timestamp := "2026-11-27T10:11:12.123456Z"
	id := strings.Repeat("d", 32)
	d := Observation{
		SchemaVersion: MediaChargeProfile, Profile: MediaChargeProfile, Kind: mediaChargeKind,
		SourceInstance: "synthetic-ur", OrgID: "1", ProjectID: "2", RequestID: "media-charge-example",
		SubjectKind: "media_job", SubjectID: "mediajob_" + id, Revision: "1", OccurredAt: timestamp,
		RecordedAt: timestamp, Coverage: "observed", CaptureExpected: []string{},
		MediaCharge: &MediaChargeObservation{
			JobID: "mediajob_" + id, TerminalStatus: mediajob.OutcomeSucceeded, AmountState: "charged",
			ChargeFactID: "mediacharge_" + id, Units: []endpointprotocol.UnitObservation{},
			CustomerCharge: &MediaCustomerCharge{
				AmountMicros: "0", Currency: "CNY", AmountScale: 1_000_000, SettlementRef: "settlement.unit:1",
			},
		},
	}
	d.SourceFactID, d.EventID = d.factID(), d.eventID()
	return d
}

func TestMediaChargeSettlementCanonicalSourceAndEnvelope(t *testing.T) {
	for _, amount := range []string{"0", "125000", "9223372036854775807"} {
		t.Run(amount, func(t *testing.T) {
			d := settledMediaChargeFixture()
			d.MediaCharge.CustomerCharge.AmountMicros = amount
			wire, err := EncodeObservation(d)
			require.NoError(t, err)
			require.Contains(t, string(wire), `"amount_scale":1000000`)
			decoded, err := DecodeObservation(wire)
			require.NoError(t, err)
			require.Equal(t, d, decoded)
			event, err := ObservationEvent(decoded, wire)
			require.NoError(t, err)
			require.Equal(t, eventconnector.HashSourceBytes(wire), event.SourceHash())
			var envelope struct {
				Payload json.RawMessage `json:"payload"`
			}
			require.NoError(t, json.Unmarshal(event.EnvelopeBytes(), &envelope))
			require.Equal(t, json.RawMessage(wire), envelope.Payload)
			_, err = DecodeObservation(envelope.Payload)
			require.NoError(t, err)
			for _, scale := range []string{`"1000000"`, "1e6", "1000000.0"} {
				_, err = DecodeObservation([]byte(strings.Replace(string(wire), `"amount_scale":1000000`, `"amount_scale":`+scale, 1)))
				require.Error(t, err)
			}
		})
	}
	for _, v := range []struct {
		name string
		edit func(*MediaCustomerCharge)
	}{
		{"USD", func(c *MediaCustomerCharge) { c.Currency = "USD" }},
		{"EUR", func(c *MediaCustomerCharge) { c.Currency = "EUR" }},
		{"scale", func(c *MediaCustomerCharge) { c.AmountScale = 10_000 }},
		{"signed", func(c *MediaCustomerCharge) { c.AmountMicros = "-1" }},
		{"leading zero", func(c *MediaCustomerCharge) { c.AmountMicros = "0" + c.AmountMicros }},
		{"overflow", func(c *MediaCustomerCharge) { c.AmountMicros = "9223372036854775808" }},
		{"reference path", func(c *MediaCustomerCharge) { c.SettlementRef = "settlement/ref" }},
		{"reference whitespace", func(c *MediaCustomerCharge) { c.SettlementRef = "settlement ref" }},
		{"reference leading symbol", func(c *MediaCustomerCharge) { c.SettlementRef = "-settlement" }},
	} {
		t.Run(v.name, func(t *testing.T) {
			d := settledMediaChargeFixture()
			v.edit(d.MediaCharge.CustomerCharge)
			_, err := EncodeObservation(d)
			require.Error(t, err)
		})
	}
}

func TestMediaChargeMeterProfilesPreserveReportedUnits(t *testing.T) {
	unit := func(class endpointprotocol.UnitClass, value string) endpointprotocol.UnitObservation {
		return endpointprotocol.UnitObservation{
			Class: class, Quantity: value, Authority: "provider_reported",
			Finality: "final", ChargeState: endpointprotocol.ChargeObservedOnly,
		}
	}
	for _, v := range []struct {
		profile  string
		reported []endpointprotocol.UnitObservation
	}{
		{"alibaba.qwen-image-3.0.v1", []endpointprotocol.UnitObservation{unit("input_images", "0"), unit("output_images", "1")}},
		{"alibaba.wan-video.v1", []endpointprotocol.UnitObservation{unit("input_video_milliseconds", "1250"), unit("output_video_milliseconds", "2000")}},
		{"deepseek.ocr.v1", []endpointprotocol.UnitObservation{unit("recognized_pages", "2")}},
		{"deepseek.asr.v1", []endpointprotocol.UnitObservation{unit("input_audio_milliseconds", "6100")}},
		{"deepseek.tts.v1", []endpointprotocol.UnitObservation{}},
	} {
		t.Run(v.profile, func(t *testing.T) {
			d := settledMediaChargeFixture()
			d.MediaCharge.AmountState = mediajob.ChargeAmountPricingPending
			d.MediaCharge.CustomerCharge = nil
			d.MediaCharge.MeterProfile = v.profile
			d.MediaCharge.Units = v.reported
			wire, err := EncodeObservation(d)
			require.NoError(t, err)
			got, err := DecodeObservation(wire)
			require.NoError(t, err)
			require.Equal(t, v.reported, got.MediaCharge.Units)
			require.Nil(t, got.MediaCharge.CustomerCharge)
			require.NotContains(t, string(wire), `"customer_charge":`)
			_, err = ObservationEvent(got, wire)
			require.NoError(t, err)
			if len(v.reported) != 0 {
				d.MediaCharge.Units = append(append([]endpointprotocol.UnitObservation{}, v.reported...), v.reported[0])
				_, err = EncodeObservation(d)
				require.Error(t, err)
			}
			d.MediaCharge.Units = []endpointprotocol.UnitObservation{unit(endpointprotocol.UnitInputTokens, "1")}
			_, err = EncodeObservation(d)
			require.Error(t, err)
		})
	}
}

func TestSharedMediaChargeKeepsUnknownAndDisjointUnitsSeparate(t *testing.T) {
	d := settledMediaChargeFixture()
	d.MediaCharge.AmountState = mediajob.ChargeAmountPricingPending
	d.MediaCharge.CustomerCharge = nil
	unit := func(class endpointprotocol.UnitClass, value string) endpointprotocol.UnitObservation {
		return endpointprotocol.UnitObservation{Class: class, Quantity: value,
			Authority: "provider_reported", Finality: "final", ChargeState: endpointprotocol.ChargeObservedOnly}
	}

	d.MediaCharge.Units = []endpointprotocol.UnitObservation{
		unit("input_images", "0"), unit("output_images", "1"),
	}
	wire, err := EncodeObservation(d)
	require.NoError(t, err)
	require.Contains(t, string(wire), `"class":"input_images","finality":"final","quantity":"0"`)
	require.NotContains(t, string(wire), `"meter_profile":`)
	d.MediaCharge.Units = d.MediaCharge.Units[1:]
	wire, err = EncodeObservation(d)
	require.NoError(t, err)
	require.NotContains(t, string(wire), `"input_images"`)

	d.MediaCharge.Units = []endpointprotocol.UnitObservation{
		unit("input_images", "1"), unit("output_video_milliseconds", "4000"),
	}
	_, err = EncodeObservation(d)
	require.NoError(t, err)
	d.MediaCharge.Units = append(d.MediaCharge.Units, unit("input_audio_milliseconds", "6"))
	_, err = EncodeObservation(d)
	require.ErrorContains(t, err, "invalid media unit combination")

	d.MediaCharge.Units = []endpointprotocol.UnitObservation{
		unit(endpointprotocol.UnitInputTokens, "3"),
		unit(endpointprotocol.UnitInputTextTokens, "3"),
		unit(endpointprotocol.UnitInputImageTokens, "1"),
	}
	_, err = EncodeObservation(d)
	require.ErrorContains(t, err, "inconsistent media token partition")
}

func TestSharedMediaChargePreservesAudioUsage(t *testing.T) {
	unit := func(class endpointprotocol.UnitClass, quantity string) endpointprotocol.UnitObservation {
		return endpointprotocol.UnitObservation{Class: class, Quantity: quantity,
			Authority: "provider_reported", Finality: "final", ChargeState: endpointprotocol.ChargeObservedOnly}
	}
	qwenInput := unit(endpointprotocol.UnitInputTokens, "78")
	qwenDuration := unit("input_audio_milliseconds", "6000")
	input := unit(endpointprotocol.UnitInputTokens, "14")
	text := unit(endpointprotocol.UnitInputTextTokens, "0")
	audio := unit(endpointprotocol.UnitInputAudioTokens, "14")
	output := unit(endpointprotocol.UnitOutputTokens, "45")
	for _, tc := range []struct {
		name    string
		tag     string
		units   []endpointprotocol.UnitObservation
		invalid bool
	}{
		{name: "qwen asr", units: []endpointprotocol.UnitObservation{qwenDuration, qwenInput, unit(endpointprotocol.UnitOutputTokens, "16")}},
		{name: "qwen output zero", units: []endpointprotocol.UnitObservation{qwenInput, unit(endpointprotocol.UnitOutputTokens, "0")}},
		{name: "qwen output absent", units: []endpointprotocol.UnitObservation{qwenInput}},
		{name: "transcription", units: []endpointprotocol.UnitObservation{input, text, audio, output}},
		{name: "transcription details absent", units: []endpointprotocol.UnitObservation{input, output}},
		{name: "transcription duration", units: []endpointprotocol.UnitObservation{qwenDuration}},
		{name: "aggregate total", units: []endpointprotocol.UnitObservation{qwenInput, unit("total_tokens", "94")}, invalid: true},
		{name: "mixed image output", units: []endpointprotocol.UnitObservation{qwenInput, output, unit(endpointprotocol.UnitOutputImageTokens, "6")}, invalid: true},
		{name: "mixed detail and duration", units: []endpointprotocol.UnitObservation{input, audio, qwenDuration}, invalid: true},
		{name: "legacy tag", tag: "deepseek.asr.v1", units: []endpointprotocol.UnitObservation{qwenInput, qwenDuration}, invalid: true},
		{name: "audio part exceeds input", units: []endpointprotocol.UnitObservation{input, unit(endpointprotocol.UnitInputAudioTokens, "15")}, invalid: true},
		{name: "parts do not balance", units: []endpointprotocol.UnitObservation{input, text, unit(endpointprotocol.UnitInputAudioTokens, "13")}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := settledMediaChargeFixture()
			d.MediaCharge.AmountState = mediajob.ChargeAmountPricingPending
			d.MediaCharge.CustomerCharge = nil
			d.MediaCharge.MeterProfile = tc.tag
			d.MediaCharge.Units = tc.units
			wire, err := EncodeObservation(d)
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			got, err := DecodeObservation(wire)
			require.NoError(t, err)
			require.Equal(t, tc.units, got.MediaCharge.Units)
			_, err = ObservationEvent(got, wire)
			require.NoError(t, err)
		})
	}
}
