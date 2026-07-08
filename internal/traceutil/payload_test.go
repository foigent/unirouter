package traceutil

import "testing"

func TestPayloadCaptureModeHelpers(t *testing.T) {
	if got := NormalizePayloadCaptureMode(""); got != PayloadCaptureModeMetadataOnly {
		t.Fatalf("expected empty mode to default to metadata_only, got %q", got)
	}
	if got := NormalizePayloadCaptureMode(" DISABLED "); got != PayloadCaptureModeDisabled {
		t.Fatalf("expected mode normalization, got %q", got)
	}
	if !IsSupportedPayloadCaptureMode(PayloadCaptureModeMetadataOnly) || !IsSupportedPayloadCaptureMode(PayloadCaptureModeDisabled) {
		t.Fatal("expected metadata_only and disabled modes to be supported")
	}
	if IsSupportedPayloadCaptureMode("raw") || IsSupportedPayloadCaptureMode("reference_only") {
		t.Fatal("expected unimplemented payload capture modes to be rejected")
	}
}
