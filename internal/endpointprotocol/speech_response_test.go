package endpointprotocol

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMiniMaxSpeechOptionalMetadata(t *testing.T) {
	profile := miniMaxSpeechProfile{synthesize: true}
	binding := &Binding{profileID: ProfileMiniMaxTTS, profileVersion: "1.0.0",
		mappedModel: MiniMaxTTS, requestedModel: MiniMaxTTS, outputCount: 1}
	wave, err := os.ReadFile("testdata/minimax-speech-synthetic.wav")
	require.NoError(t, err)

	cases := []struct {
		name     string
		metadata map[string]any
		samples  []byte
		want     []UnitObservation
		reject   bool
	}{
		{name: "fields omitted", samples: wave, want: []UnitObservation{}},
		{name: "explicit zero", metadata: map[string]any{"usage_characters": 0, "word_count": 47}, samples: wave,
			want: []UnitObservation{observedUnit(UnitInputBillableChars, "0")}},
		{name: "pcm label with actual wav", metadata: map[string]any{"audio_format": "pcm"}, samples: wave, want: []UnitObservation{}},
		{name: "negative usage", metadata: map[string]any{"usage_characters": -1}, samples: wave, reject: true},
		{name: "flac contradicts wav", metadata: map[string]any{"audio_format": "flac"}, samples: wave, reject: true},
		{name: "rate contradicts wav", metadata: map[string]any{"audio_sample_rate": 16000}, samples: wave, reject: true},
		{name: "raw pcm", metadata: map[string]any{"audio_format": "pcm"}, samples: wave[80:], reject: true},
		{name: "truncated wav", samples: wave[:len(wave)-2], reject: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wire := map[string]any{
				"data":      map[string]any{"audio": hex.EncodeToString(tc.samples), "status": 2},
				"base_resp": map[string]any{"status_code": 0},
			}
			if tc.metadata != nil {
				wire["extra_info"] = tc.metadata
			}
			body, err := json.Marshal(wire)
			require.NoError(t, err)
			out, err := profile.ParseResponse(t.Context(), binding, http.StatusOK, body)
			if tc.reject {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NoError(t, ValidateMediaOutcome(ProfileMiniMaxTTS, MiniMaxTTS, out))
			require.Equal(t, tc.want, out.Units)
			require.Equal(t, wave, out.Media.Bytes)
		})
	}
}

func TestOpenAITranscriptionReportedDurationOrUnknown(t *testing.T) {
	b := &Binding{profileID: ProfileOpenAITranscribe, profileVersion: "1.0.0", mappedModel: OpenAITranscribe, outputCount: 1, inputDurationMS: 2496}
	p := openAITranscribeProfile{}
	for _, tc := range []struct {
		wire string
		want []UnitObservation
	}{
		{`{"text":"2947"}`, []UnitObservation{}},
		{`{"text":"2947","usage":{"type":"duration","seconds":2}}`, []UnitObservation{observedUnit(UnitInputAudioDurationMS, "2000")}},
		{`{"text":"2947","usage":{"type":"duration","seconds":2.496}}`, []UnitObservation{observedUnit(UnitInputAudioDurationMS, "2496")}},
	} {
		out, err := p.ParseResponse(t.Context(), b, http.StatusOK, []byte(tc.wire))
		require.NoError(t, err)
		require.NoError(t, ValidateMediaOutcome(ProfileOpenAITranscribe, OpenAITranscribe, out))
		require.Equal(t, tc.want, out.Units)
		require.Equal(t, "2947", string(out.Media.Bytes))
	}
	for _, usage := range []string{
		`{"type":"duration","seconds":4}`,
		`{"type":"duration","seconds":2,"input_tokens":14}`,
		`{"type":"tokens","input_tokens":14,"output_tokens":45,"total_tokens":59,"input_token_details":{"audio_tokens":15}}`,
	} {
		_, err := p.ParseResponse(t.Context(), b, http.StatusOK, []byte(`{"text":"2947","usage":`+usage+`}`))
		require.Error(t, err)
	}
}
