package endpointprotocol

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"strings"
)

type imageDeliveryConstraints struct {
	width, height int
	format        string
}

func flareImageDeliveryControls(size, format string) imageDeliveryConstraints {
	constraints := imageDeliveryConstraints{format: format}
	switch size {
	case "1024x1024":
		constraints.width, constraints.height = 1024, 1024
	case "1536x1024":
		constraints.width, constraints.height = 1536, 1024
	case "1024x1536":
		constraints.width, constraints.height = 1024, 1536
	}
	return constraints
}

func SupportsCanonicalFlareImageFormat(format string) bool {
	return oneOfOrEmpty(format, "png", "jpeg")
}

func validateImageDelivery(body []byte, expected imageDeliveryConstraints) error {
	if expected == (imageDeliveryConstraints{}) {
		return nil
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(body))
	// Bound actual decoded memory before decoding compressed provider bytes.
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 4096 || config.Height > 4096 ||
		(format != "png" && format != "jpeg") {
		return protocolError(ErrInvalidResponse, "image_format_invalid")
	}
	if expected.format != "" && expected.format != format {
		return protocolError(ErrInvalidResponse, "image_format_mismatch")
	}
	if expected.width != 0 && (expected.width != config.Width || expected.height != config.Height) {
		return protocolError(ErrInvalidResponse, "image_dimensions_invalid")
	}
	if _, _, err := image.Decode(bytes.NewReader(body)); err != nil {
		return protocolError(ErrInvalidResponse, "image_invalid_"+format)
	}
	return nil
}

func decodeMediaImage(encoded *string, expected imageDeliveryConstraints) (*MediaArtifact, error) {
	if encoded == nil || *encoded == "" {
		return nil, protocolError(ErrInvalidResponse, "image_base64_missing")
	}
	if strings.ContainsAny(*encoded, "\r\n") {
		return nil, protocolError(ErrInvalidResponse, "image_base64_invalid")
	}
	image, err := base64.StdEncoding.Strict().DecodeString(*encoded)
	if err != nil || len(image) == 0 || len(image) > MaxOpenAIImageBinaryBytes {
		return nil, protocolError(ErrInvalidResponse, "image_base64_invalid")
	}
	if err := validateImageDelivery(image, expected); err != nil {
		return nil, err
	}
	mediaType := DetectImageMediaType(image)
	return &MediaArtifact{Bytes: image, MediaType: mediaType}, nil
}

func decodeSquarePNG(encoded *string) (*MediaArtifact, error) {
	return decodeMediaImage(encoded, imageDeliveryConstraints{width: 1024, height: 1024, format: "png"})
}

func DetectImageMediaType(body []byte) string {
	if len(body) < 12 {
		return ""
	}
	switch {
	case bytes.HasPrefix(body, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return "image/png"
	case bytes.HasPrefix(body, []byte{0xff, 0xd8, 0xff}):
		return "image/jpeg"
	case bytes.Equal(body[:4], []byte("RIFF")) && bytes.Equal(body[8:12], []byte("WEBP")):
		return "image/webp"
	}
	switch mediaType := http.DetectContentType(body); mediaType {
	case "image/png", "image/jpeg", "image/webp":
		return mediaType
	default:
		return ""
	}
}
