package traceutil

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
)

const (
	EvidenceContextAuthHeader       = "X-UniRouter-Evidence-Auth"
	EvidenceContextTraceIDHeader    = "X-UniRouter-Trace-Id"
	EvidenceContextParentSpanHeader = "X-UniRouter-Parent-Span-Id"
	EvidenceContextSessionIDHeader  = "X-UniRouter-Session-Id"
	EvidenceContextWorkflowIDHeader = "X-UniRouter-Workflow-Id"
	EvidenceContextAgentIDHeader    = "X-UniRouter-Agent-Id"
	EvidenceContextActorTypeHeader  = "X-UniRouter-Actor-Type"
	EvidenceContextActorRefHeader   = "X-UniRouter-Actor-Ref"
	EvidenceContextOnBehalfOfHeader = "X-UniRouter-On-Behalf-Of"
)

type EvidenceContext struct {
	TraceID      string
	ParentSpanID string
	SessionID    string
	WorkflowID   string
	AgentID      string
	ActorType    string
	ActorRef     string
	OnBehalfOf   string
}

type evidenceContextKey struct{}

func ContextWithEvidence(ctx context.Context, evidence EvidenceContext) context.Context {
	if ctx == nil || evidence.IsZero() {
		return ctx
	}
	return context.WithValue(ctx, evidenceContextKey{}, evidence)
}

func EvidenceFromContext(ctx context.Context) EvidenceContext {
	if ctx == nil {
		return EvidenceContext{}
	}
	evidence, _ := ctx.Value(evidenceContextKey{}).(EvidenceContext)
	return evidence
}

func (e EvidenceContext) IsZero() bool {
	return e.TraceID == "" && e.ParentSpanID == "" && e.SessionID == "" &&
		e.WorkflowID == "" && e.AgentID == "" &&
		e.ActorType == "" && e.ActorRef == "" && e.OnBehalfOf == ""
}

func ExtractTrustedEvidenceContext(header http.Header, trustSecret string) EvidenceContext {
	trustSecret = strings.TrimSpace(trustSecret)
	if trustSecret == "" {
		return EvidenceContext{}
	}
	provided := strings.TrimSpace(header.Get(EvidenceContextAuthHeader))
	if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(trustSecret)) != 1 {
		return EvidenceContext{}
	}

	evidence := EvidenceContext{
		TraceID:      sanitizeEvidenceField(header.Get(EvidenceContextTraceIDHeader), 64),
		ParentSpanID: sanitizeEvidenceField(header.Get(EvidenceContextParentSpanHeader), 64),
		SessionID:    sanitizeEvidenceField(header.Get(EvidenceContextSessionIDHeader), 128),
		WorkflowID:   sanitizeEvidenceField(header.Get(EvidenceContextWorkflowIDHeader), 128),
		AgentID:      sanitizeEvidenceField(header.Get(EvidenceContextAgentIDHeader), 128),
		ActorRef:     sanitizeEvidenceField(header.Get(EvidenceContextActorRefHeader), 128),
		OnBehalfOf:   sanitizeEvidenceField(header.Get(EvidenceContextOnBehalfOfHeader), 128),
	}
	if traceID, parentSpanID := traceparentEvidence(header.Get("Traceparent")); evidence.TraceID == "" && traceID != "" {
		evidence.TraceID = traceID
		evidence.ParentSpanID = parentSpanID
	}
	if actorType := normalizeActorType(header.Get(EvidenceContextActorTypeHeader)); actorType != "" {
		evidence.ActorType = actorType
	}
	return evidence
}

func sanitizeEvidenceField(value string, maxLen int) string {
	value = strings.Trim(strings.TrimSpace(value), `"`)
	if value == "" || len(value) > maxLen {
		return ""
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 33 || value[i] > 126 {
			return ""
		}
	}
	return value
}

func normalizeActorType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "human", "service", "agent", "api_key", "system", "unknown":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func traceparentEvidence(value string) (string, string) {
	parts := strings.Split(strings.TrimSpace(value), "-")
	if len(parts) != 4 {
		return "", ""
	}
	traceID := strings.ToLower(parts[1])
	parentSpanID := strings.ToLower(parts[2])
	if !isLowerHex(traceID, 32) || !isLowerHex(parentSpanID, 16) {
		return "", ""
	}
	return traceID, parentSpanID
}

func isLowerHex(value string, expectedLen int) bool {
	if len(value) != expectedLen {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
