package traceutil

import "strings"

const (
	PayloadCaptureModeMetadataOnly = "metadata_only"
	PayloadCaptureModeDisabled     = "disabled"
)

func NormalizePayloadCaptureMode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return PayloadCaptureModeMetadataOnly
	}
	return value
}

func IsSupportedPayloadCaptureMode(value string) bool {
	switch NormalizePayloadCaptureMode(value) {
	case PayloadCaptureModeMetadataOnly, PayloadCaptureModeDisabled:
		return true
	default:
		return false
	}
}

func SupportedPayloadCaptureModes() string {
	return PayloadCaptureModeMetadataOnly + ", " + PayloadCaptureModeDisabled
}
