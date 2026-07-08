package traceutil

import (
	"context"
	"net/http"
	"testing"
)

func TestExtractTrustedEvidenceContext_RequiresSecret(t *testing.T) {
	header := http.Header{}
	header.Set(EvidenceContextAuthHeader, "secret")
	header.Set(EvidenceContextTraceIDHeader, "trace-1")

	if got := ExtractTrustedEvidenceContext(header, ""); !got.IsZero() {
		t.Fatalf("expected empty evidence without configured secret, got %#v", got)
	}
	if got := ExtractTrustedEvidenceContext(header, "other-secret"); !got.IsZero() {
		t.Fatalf("expected empty evidence with mismatched secret, got %#v", got)
	}
}

func TestExtractTrustedEvidenceContext_ExplicitHeaders(t *testing.T) {
	header := http.Header{}
	header.Set(EvidenceContextAuthHeader, "shared-secret-123456")
	header.Set(EvidenceContextTraceIDHeader, "trace-abc-123")
	header.Set(EvidenceContextParentSpanHeader, "span-parent-1")
	header.Set(EvidenceContextSessionIDHeader, "session-42")
	header.Set(EvidenceContextActorTypeHeader, "Agent")
	header.Set(EvidenceContextActorRefHeader, "agent:planner")
	header.Set(EvidenceContextOnBehalfOfHeader, "user:42")

	got := ExtractTrustedEvidenceContext(header, "shared-secret-123456")
	if got.TraceID != "trace-abc-123" || got.ParentSpanID != "span-parent-1" || got.SessionID != "session-42" {
		t.Fatalf("unexpected trace/session evidence: %#v", got)
	}
	if got.ActorType != "agent" || got.ActorRef != "agent:planner" || got.OnBehalfOf != "user:42" {
		t.Fatalf("unexpected actor evidence: %#v", got)
	}
}

func TestExtractTrustedEvidenceContext_TraceparentFallbackAndInvalidValues(t *testing.T) {
	header := http.Header{}
	header.Set(EvidenceContextAuthHeader, "shared-secret-123456")
	header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	header.Set(EvidenceContextSessionIDHeader, "bad session")
	header.Set(EvidenceContextActorTypeHeader, "owner")

	got := ExtractTrustedEvidenceContext(header, "shared-secret-123456")
	if got.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" || got.ParentSpanID != "00f067aa0ba902b7" {
		t.Fatalf("expected traceparent evidence, got %#v", got)
	}
	if got.SessionID != "" || got.ActorType != "" {
		t.Fatalf("expected invalid session/actor type to be dropped, got %#v", got)
	}
}

func TestEvidenceContextRoundTrip(t *testing.T) {
	evidence := EvidenceContext{TraceID: "trace-1", SessionID: "session-1"}
	ctx := ContextWithEvidence(nil, evidence)
	if ctx != nil {
		t.Fatal("expected nil context to stay nil")
	}

	ctx = ContextWithEvidence(context.Background(), evidence)
	if got := EvidenceFromContext(ctx); got != evidence {
		t.Fatalf("expected evidence context round trip, got %#v", got)
	}
}
