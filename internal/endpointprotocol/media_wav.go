package endpointprotocol

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"strings"
)

const MaxASRWAVBytes = 45 << 10

type PCMInfo struct {
	DurationMS int64
	SampleRate uint32
	Channels   uint16
	HasSignal  bool
}

// ValidatePCM16WAV requires a complete bounded PCM container and a whole
// number of samples. Truncated bodies and streaming size sentinels cannot
// be treated as completed synthesis.
func ValidatePCM16WAV(body []byte) (PCMInfo, bool) {
	if len(body) < 44 || !bytes.Equal(body[:4], []byte("RIFF")) || !bytes.Equal(body[8:12], []byte("WAVE")) ||
		uint64(binary.LittleEndian.Uint32(body[4:8]))+8 != uint64(len(body)) {
		return PCMInfo{}, false
	}
	var info PCMInfo
	var blockAlign uint16
	var audioBytes int64
	for chunks := body[12:]; len(chunks) > 0; {
		if len(chunks) < 8 {
			return PCMInfo{}, false
		}
		size := uint64(binary.LittleEndian.Uint32(chunks[4:8]))
		next := 8 + size + size%2
		if next > uint64(len(chunks)) {
			return PCMInfo{}, false
		}
		chunk := chunks[8 : 8+size]
		switch string(chunks[:4]) {
		case "fmt ":
			if info.SampleRate != 0 || len(chunk) < 16 ||
				binary.LittleEndian.Uint16(chunk[:2]) != 1 || binary.LittleEndian.Uint16(chunk[14:16]) != 16 {
				return PCMInfo{}, false
			}
			info.Channels = binary.LittleEndian.Uint16(chunk[2:4])
			info.SampleRate = binary.LittleEndian.Uint32(chunk[4:8])
			blockAlign = binary.LittleEndian.Uint16(chunk[12:14])
			if (info.Channels != 1 && info.Channels != 2) || blockAlign != 2*info.Channels ||
				info.SampleRate < 8000 || info.SampleRate > 48000 ||
				binary.LittleEndian.Uint32(chunk[8:12]) != info.SampleRate*uint32(blockAlign) {
				return PCMInfo{}, false
			}
		case "data":
			if blockAlign == 0 || audioBytes != 0 || size == 0 || size%uint64(blockAlign) != 0 {
				return PCMInfo{}, false
			}
			audioBytes = int64(size)
			for i := 0; i < len(chunk); i += 2 {
				if binary.LittleEndian.Uint16(chunk[i:i+2]) != 0 {
					info.HasSignal = true
					break
				}
			}
		}
		chunks = chunks[next:]
	}
	if audioBytes == 0 || info.SampleRate == 0 {
		return PCMInfo{}, false
	}
	samples := audioBytes / int64(blockAlign)
	info.DurationMS = (samples*1000 + int64(info.SampleRate) - 1) / int64(info.SampleRate)
	return info, info.DurationMS > 0
}

func DecodeASRInputWAV(input string) ([]byte, PCMInfo, error) {
	const prefix = "data:audio/wav;base64,"
	encoded, ok := strings.CutPrefix(input, prefix)
	if !ok || len(encoded) > base64.StdEncoding.EncodedLen(MaxASRWAVBytes) || strings.ContainsAny(encoded, "\r\n") {
		return nil, PCMInfo{}, protocolError(ErrInvalidRequest, "asr_wav_input_invalid")
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(raw) > MaxASRWAVBytes {
		return nil, PCMInfo{}, protocolError(ErrInvalidRequest, "asr_wav_input_invalid")
	}
	info, ok := ValidatePCM16WAV(raw)
	if !ok || info.Channels != 1 {
		return nil, PCMInfo{}, protocolError(ErrInvalidRequest, "asr_wav_input_invalid")
	}
	switch info.SampleRate {
	case 8000, 16000, 22050, 24000, 32000, 44100, 48000:
		return raw, info, nil
	default:
		return nil, PCMInfo{}, protocolError(ErrInvalidRequest, "asr_wav_input_invalid")
	}
}
