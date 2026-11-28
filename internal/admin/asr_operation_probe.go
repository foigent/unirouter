package admin

import (
	"crypto/rand"
	"embed"
	"encoding/binary"
	"strings"
	"unicode"

	"github.com/uniroute/unirouter/internal/endpointprotocol"
)

// These are four clipped digit samples from a local synthetic voice. Random
// assembly retains the answer in memory, outside the submitted WAV metadata.
//
//go:embed probeassets/digit-*.pcm
var asrDigitSamples embed.FS

const asrProbeDigits = "2479"
const asrProbeDigitCount = 5

func makeASRProbe(model string) ([]byte, string, error) {
	wave, answer, err := makeASRProbeWAV()
	if err != nil {
		return nil, "", err
	}
	body, err := endpointprotocol.TranscriptionRequestBody(model, wave)
	return body, answer, err
}

func makeASRProbeWAV() ([]byte, string, error) {
	var choices [asrProbeDigitCount]byte
	if _, err := rand.Read(choices[:]); err != nil {
		return nil, "", err
	}
	answer := make([]byte, len(choices))
	wave := make([]byte, 44, 44+len(choices)*8640)
	for i, choice := range choices {
		digit := asrProbeDigits[int(choice)%len(asrProbeDigits)]
		pcm, err := asrDigitSamples.ReadFile("probeassets/digit-" + string(digit) + ".pcm")
		if err != nil {
			return nil, "", err
		}
		wave = append(wave, pcm...)
		answer[i] = digit
	}
	copy(wave[0:4], "RIFF")
	binary.LittleEndian.PutUint32(wave[4:8], uint32(len(wave)-8))
	copy(wave[8:16], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wave[16:20], 16)
	binary.LittleEndian.PutUint16(wave[20:22], 1)
	binary.LittleEndian.PutUint16(wave[22:24], 1)
	binary.LittleEndian.PutUint32(wave[24:28], 8000)
	binary.LittleEndian.PutUint32(wave[28:32], 16000)
	binary.LittleEndian.PutUint16(wave[32:34], 2)
	binary.LittleEndian.PutUint16(wave[34:36], 16)
	copy(wave[36:40], "data")
	binary.LittleEndian.PutUint32(wave[40:44], uint32(len(wave)-44))
	return wave, string(answer), nil
}

func verifiesASRProbe(result *endpointprotocol.MediaArtifact, code string) bool {
	if result == nil || result.MediaType != "text/plain; charset=utf-8" || code == "" {
		return false
	}
	return matchesSpokenDigits(string(result.Bytes), code)
}

func matchesSpokenDigits(text, code string) bool {
	parts := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r)
	})
	var digits strings.Builder
	for _, token := range parts {
		switch token {
		case "two":
			token = "2"
		case "four":
			token = "4"
		case "seven":
			token = "7"
		case "nine":
			token = "9"
		}
		digits.WriteString(token)
	}
	return digits.String() == code
}
