package endpointprotocol

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/jpeg"
)

// InlineJPEGBounds is the decoded-image budget. Provider envelopes are checked
// by their own protocols before this validator receives a base64 payload.
type InlineJPEGBounds struct {
	MaximumBytes       int64
	MinimumWidth       int
	MinimumHeight      int
	MinimumPixels      int64
	MaximumPixels      int64
	MaximumAspectRatio int64
}

func CheckBoundedInlineJPEG(payload string, bounds InlineJPEGBounds) (int64, error) {
	if int64(base64.StdEncoding.DecodedLen(len(payload))) > bounds.MaximumBytes+2 {
		return 0, fmt.Errorf("image decoded bytes must not exceed %d", bounds.MaximumBytes)
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(payload)
	if err != nil {
		return 0, fmt.Errorf("image_url contains invalid base64 data")
	}
	if int64(len(decoded)) > bounds.MaximumBytes {
		return 0, fmt.Errorf("image decoded bytes must not exceed %d", bounds.MaximumBytes)
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(decoded))
	if err != nil {
		return 0, fmt.Errorf("image_url payload must be a valid JPEG image")
	}
	if config.Width < bounds.MinimumWidth || config.Height < bounds.MinimumHeight {
		return 0, fmt.Errorf("image dimensions must be at least %dx%d", bounds.MinimumWidth, bounds.MinimumHeight)
	}
	pixels := int64(config.Width) * int64(config.Height)
	if pixels < bounds.MinimumPixels || pixels > bounds.MaximumPixels {
		return 0, fmt.Errorf("image pixels must be between %d and %d", bounds.MinimumPixels, bounds.MaximumPixels)
	}
	if int64(config.Width) > int64(config.Height)*bounds.MaximumAspectRatio ||
		int64(config.Height) > int64(config.Width)*bounds.MaximumAspectRatio {
		return 0, fmt.Errorf("image aspect ratio must not exceed %d:1", bounds.MaximumAspectRatio)
	}
	decodedImage, err := jpeg.Decode(bytes.NewReader(decoded))
	if err != nil {
		return 0, fmt.Errorf("image_url payload must be a complete JPEG image")
	}
	if rect := decodedImage.Bounds(); rect.Dx() != config.Width || rect.Dy() != config.Height {
		return 0, fmt.Errorf("image JPEG dimensions are inconsistent")
	}
	return pixels, nil
}
