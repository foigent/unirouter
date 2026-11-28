package endpointprotocol

import (
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/url"
	"time"
)

type MP4Requirements struct {
	DisplayRatioWidth  int
	DisplayRatioHeight int
	Width              int
	Height             int
	MaxWidth           int
	MaxHeight          int
	MaxDuration        time.Duration
	MinDuration        time.Duration
	Duration           time.Duration
	AllowAudio         bool
}

type mp4Box struct {
	kind    string
	payload []byte
}

func mp4Boxes(data []byte) ([]mp4Box, bool) {
	var boxes []mp4Box
	for len(data) > 0 {
		if len(data) < 8 || len(boxes) == 4096 {
			return nil, false
		}
		size, header := uint64(binary.BigEndian.Uint32(data[:4])), uint64(8)
		switch size {
		case 0:
			size = uint64(len(data))
		case 1:
			if len(data) < 16 {
				return nil, false
			}
			size, header = binary.BigEndian.Uint64(data[8:16]), 16
		}
		if size < header || size > uint64(len(data)) {
			return nil, false
		}
		boxes = append(boxes, mp4Box{kind: string(data[4:8]), payload: data[header:size]})
		data = data[size:]
	}
	return boxes, true
}

func mp4Child(boxes []mp4Box, kind string) []byte {
	var payload []byte
	for _, box := range boxes {
		if box.kind != kind {
			continue
		}
		if payload != nil {
			return nil
		}
		payload = box.payload
	}
	return payload
}

func mp4MediaDuration(mdhd []byte) (uint64, uint64) {
	if len(mdhd) < 4 {
		return 0, 0
	}
	switch mdhd[0] {
	case 0:
		if len(mdhd) < 24 {
			return 0, 0
		}
		return uint64(binary.BigEndian.Uint32(mdhd[12:16])), uint64(binary.BigEndian.Uint32(mdhd[16:20]))
	case 1:
		if len(mdhd) < 36 {
			return 0, 0
		}
		return uint64(binary.BigEndian.Uint32(mdhd[20:24])), binary.BigEndian.Uint64(mdhd[24:32])
	default:
		return 0, 0
	}
}

func mp4VideoSamples(md []mp4Box) bool {
	info, ok := mp4Boxes(mp4Child(md, "minf"))
	if !ok || len(info) == 0 {
		return false
	}
	samples, ok := mp4Boxes(mp4Child(info, "stbl"))
	if !ok || len(samples) == 0 {
		return false
	}
	description := mp4Child(samples, "stsd")
	if len(description) < 16 || binary.BigEndian.Uint32(description[4:8]) != 1 {
		return false
	}
	entries, ok := mp4Boxes(description[8:])
	if !ok || len(entries) != 1 {
		return false
	}
	switch entries[0].kind {
	case "avc1", "avc3", "hvc1", "hev1":
	default:
		return false
	}
	if len(entries[0].payload) < 78 {
		return false
	}
	sizes := mp4Child(samples, "stsz")
	if len(sizes) < 12 {
		return false
	}
	count := uint64(binary.BigEndian.Uint32(sizes[8:12]))
	if count == 0 {
		return false
	}
	return binary.BigEndian.Uint32(sizes[4:8]) > 0 || count <= uint64(len(sizes)-12)/4
}

// ValidateMP4 checks bounded container metadata and a video sample table. It
// does not decode the stream. Files without a video track cannot be delivered
// as video, even when ftyp and mdat are present.
func ValidateMP4(body []byte, expected MP4Requirements) bool {
	top, ok := mp4Boxes(body)
	if !ok || len(mp4Child(top, "ftyp")) < 8 {
		return false
	}
	var dataFound bool
	for _, box := range top {
		if box.kind == "mdat" && len(box.payload) > 0 {
			dataFound = true
		}
	}
	if !dataFound {
		return false
	}
	movie, ok := mp4Boxes(mp4Child(top, "moov"))
	if !ok {
		return false
	}
	videos := 0
	for _, box := range movie {
		if box.kind != "trak" {
			continue
		}
		track, ok := mp4Boxes(box.payload)
		if !ok {
			return false
		}
		media, ok := mp4Boxes(mp4Child(track, "mdia"))
		if !ok {
			return false
		}
		handler := mp4Child(media, "hdlr")
		if len(handler) < 12 {
			return false
		}
		switch string(handler[8:12]) {
		case "soun":
			if !expected.AllowAudio {
				return false
			}
		case "vide":
			header := mp4Child(track, "tkhd")
			if len(header) < 84 || !mp4VideoSamples(media) {
				return false
			}
			width := binary.BigEndian.Uint32(header[len(header)-8:]) >> 16
			height := binary.BigEndian.Uint32(header[len(header)-4:]) >> 16
			timescale, duration := mp4MediaDuration(mp4Child(media, "mdhd"))
			if width == 0 || height == 0 || timescale == 0 || duration == 0 ||
				(expected.Width > 0 && uint32(expected.Width) != width) || (expected.Height > 0 && uint32(expected.Height) != height) {
				return false
			}
			if (expected.MaxWidth > 0 && width > uint32(expected.MaxWidth)) ||
				(expected.MaxHeight > 0 && height > uint32(expected.MaxHeight)) ||
				(expected.MinDuration > 0 && duration < timescale*uint64(expected.MinDuration/time.Second)) ||
				(expected.MaxDuration > 0 && duration > timescale*uint64(expected.MaxDuration/time.Second)) {
				return false
			}
			if expected.DisplayRatioWidth > 0 && expected.DisplayRatioHeight > 0 {
				// tkhd stores display dimensions in 16.16 fixed point.
				// The approved rasters may round a 16:9 width by a pixel or two.
				diff := int64(width)*int64(expected.DisplayRatioHeight) - int64(height)*int64(expected.DisplayRatioWidth)
				tolerance := int64(2 * expected.DisplayRatioHeight)
				if diff < -tolerance || diff > tolerance {
					return false
				}
			}
			if expected.Duration > 0 {
				// Exact integer nanosecond comparison avoids float rounding or
				// overflow from an untrusted media header. The configured
				// formats here have whole-second bounds.
				if expected.Duration%time.Second != 0 || duration != timescale*uint64(expected.Duration/time.Second) {
					return false
				}
			}
			videos++
		}
	}
	return videos == 1
}

type mediaAssetOrigin interface {
	AcceptsResultURL(*url.URL) bool
}

type RemoteMediaProfile interface {
	Profile
	mediaAssetOrigin
	ValidateRemoteMedia([]byte) (*MediaArtifact, error)
}

// DownloadMP4 verifies the playable video track before returning any bytes.
func DownloadMP4(ctx context.Context, client *http.Client, profile AsyncMediaProfile, rawURL string) (*MediaArtifact, error) {
	body, err := downloadMediaAsset(ctx, client, profile, rawURL)
	if err != nil {
		return nil, err
	}
	if !ValidateMP4(body, profile.VideoRequirements()) {
		return nil, protocolError(ErrInvalidResponse, "media_asset_invalid")
	}
	return &MediaArtifact{Bytes: body, MediaType: "video/mp4"}, nil
}

func DownloadRemoteMedia(ctx context.Context, client *http.Client, profile RemoteMediaProfile, rawURL string) (*MediaArtifact, error) {
	body, err := downloadMediaAsset(ctx, client, profile, rawURL)
	if err != nil {
		return nil, err
	}
	return profile.ValidateRemoteMedia(body)
}

// downloadMediaAsset issues a separate credential-free GET to the reviewed
// artifact origin. Upstream authentication is only used for the provider API.
func downloadMediaAsset(ctx context.Context, client *http.Client, origin mediaAssetOrigin, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || !origin.AcceptsResultURL(u) {
		return nil, protocolError(ErrInvalidResponse, "media_asset_origin_invalid")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, protocolError(ErrInvalidResponse, "media_asset_request_invalid")
	}
	noRedirect := *client
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := noRedirect.Do(req)
	if err != nil {
		return nil, protocolError(ErrUpstreamResponse, "media_asset_unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, protocolError(ErrUpstreamResponse, "media_asset_unavailable")
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, MaxOpenAIImageBinaryBytes+1))
	if err != nil {
		return nil, protocolError(ErrUpstreamResponse, "media_asset_unavailable")
	}
	if len(body) > MaxOpenAIImageBinaryBytes {
		return nil, protocolError(ErrInvalidResponse, "media_asset_invalid")
	}
	return body, nil
}
