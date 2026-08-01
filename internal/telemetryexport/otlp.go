package telemetryexport

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const maxOTLPPayloadBytes = 1024 * 1024

var errOTLPPayloadTooLarge = errors.New("OTLP payload exceeds one MiB")

type resourceConfig struct {
	ServiceName      string
	ServiceNamespace string
	ServiceVersion   string
	DeploymentEnv    string
}

type otlpExportRequest struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

type otlpResourceSpans struct {
	Resource   otlpResource    `json:"resource"`
	ScopeSpans []otlpScopeSpan `json:"scopeSpans"`
}

type otlpResource struct {
	Attributes []keyValue `json:"attributes"`
}

type otlpScopeSpan struct {
	Scope otlpScope  `json:"scope"`
	Spans []otlpSpan `json:"spans"`
}

type otlpScope struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type otlpSpan struct {
	TraceID           string     `json:"traceId"`
	SpanID            string     `json:"spanId"`
	ParentSpanID      string     `json:"parentSpanId,omitempty"`
	Name              string     `json:"name"`
	Kind              int        `json:"kind"`
	StartTimeUnixNano string     `json:"startTimeUnixNano"`
	EndTimeUnixNano   string     `json:"endTimeUnixNano"`
	Attributes        []keyValue `json:"attributes"`
	Status            otlpStatus `json:"status"`
}

type otlpStatus struct {
	Code int `json:"code"`
}

func marshalBatch(records []spanRecord, resource resourceConfig) ([]byte, error) {
	serviceName := strings.TrimSpace(resource.ServiceName)
	serviceNamespace := strings.TrimSpace(resource.ServiceNamespace)
	if !tokenValuePattern.MatchString(serviceName) || !tokenValuePattern.MatchString(serviceNamespace) {
		return nil, fmt.Errorf("OTLP resource service identity is not portable")
	}
	resourceAttributes := map[string]string{
		"service.name":      serviceName,
		"service.namespace": serviceNamespace,
	}
	if value := strings.TrimSpace(resource.ServiceVersion); tokenValuePattern.MatchString(value) {
		resourceAttributes["service.version"] = value
	}
	if value := strings.TrimSpace(resource.DeploymentEnv); tokenValuePattern.MatchString(value) {
		resourceAttributes["deployment.environment.name"] = value
	}
	keys := make([]string, 0, len(resourceAttributes))
	for key, value := range resourceAttributes {
		if value != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	attributes := make([]keyValue, 0, len(keys))
	for _, key := range keys {
		attributes = append(attributes, keyValue{
			Key:   key,
			Value: anyValue{StringValue: resourceAttributes[key]},
		})
	}

	spans := make([]otlpSpan, 0, len(records))
	for _, record := range records {
		spans = append(spans, otlpSpan{
			TraceID:           record.TraceID,
			SpanID:            record.SpanID,
			ParentSpanID:      record.ParentSpanID,
			Name:              record.Name,
			Kind:              1, // SPAN_KIND_INTERNAL
			StartTimeUnixNano: record.StartTimeUnixNano,
			EndTimeUnixNano:   record.EndTimeUnixNano,
			Attributes:        record.Attributes,
			Status:            otlpStatus{Code: record.StatusCode},
		})
	}
	request := otlpExportRequest{
		ResourceSpans: []otlpResourceSpans{{
			Resource: otlpResource{Attributes: attributes},
			ScopeSpans: []otlpScopeSpan{{
				Scope: otlpScope{
					Name:    "github.com/uniroute/unirouter/internal/telemetryexport",
					Version: SchemaVersion,
				},
				Spans: spans,
			}},
		}},
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if len(encoded) > maxOTLPPayloadBytes {
		return nil, errOTLPPayloadTooLarge
	}
	return encoded, nil
}
