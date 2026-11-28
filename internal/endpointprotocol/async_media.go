package endpointprotocol

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AsyncMediaProfile models one paid submission followed by read-only queries.
// The caller must durably bind the accepted task before querying it.
type AsyncMediaProfile interface {
	Profile
	BuildStatusRequest(context.Context, BuildInput, string) (*http.Request, error)
	ParseStatusResponse(expectedModel, taskID string, status int, body []byte) (Outcome, error)
	AcceptsResultURL(*url.URL) bool
	VideoRequirements() MP4Requirements
}

const (
	MediaTaskPending                    = "pending"
	MediaTaskCompleted                  = "completed"
	MediaTaskFailed                     = "failed"
	MediaTaskUnavailable                = "unavailable"
	MediaTaskMaxStatusReads             = 40
	MediaTaskPollInterval               = 15 * time.Second
	MediaTaskReadWindow                 = 24 * time.Hour
	UnitInputVideoDurationMS  UnitClass = "input_video_milliseconds"
	UnitOutputVideoDurationMS UnitClass = "output_video_milliseconds"
)

func ValidMediaTaskID(id string) bool {
	if len(id) < 1 || len(id) > 128 || id == "." || id == ".." {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func validMediaAssetURL(u *url.URL) bool {
	return u != nil && u.Scheme == "https" && u.Hostname() != "" && u.Host == u.Hostname() &&
		u.User == nil && u.Fragment == "" && u.Path != "" && u.Path != "/" && u.Opaque == ""
}

func (outcome Outcome) MediaTaskReference() string {
	if outcome.ProviderState == MediaTaskPending && ValidMediaTaskID(outcome.ProviderObject) {
		return outcome.ProviderObject
	}
	return ""
}

// Milliseconds retains the provider's reported decimal duration exactly.
func mediaMilliseconds(seconds string) (string, error) {
	whole, fraction, _ := strings.Cut(seconds, ".")
	if whole == "" || len(fraction) > 3 {
		return "", errors.New("unsupported media duration")
	}
	for _, c := range whole + fraction {
		if c < '0' || c > '9' {
			return "", errors.New("invalid media duration")
		}
	}
	value := strings.TrimLeft(whole+fraction+strings.Repeat("0", 3-len(fraction)), "0")
	if value == "" {
		return "0", nil
	}
	return value, nil
}

func decodeMediaVideoPrompt(input BuildInput, limit int) (string, error) {
	var downstream struct {
		Model     string `json:"model"`
		Prompt    string `json:"prompt"`
		Operation string `json:"operation"`
		N         int    `json:"n,omitempty"`
		Stream    bool   `json:"stream,omitempty"`
	}
	if err := decodeStrictRequest(input.Body, &downstream); err != nil {
		return "", err
	}
	if downstream.Model != input.RequestedModel || downstream.Operation != "text_to_video" ||
		(downstream.N != 0 && downstream.N != 1) || downstream.Stream ||
		strings.TrimSpace(downstream.Prompt) == "" || len(downstream.Prompt) > limit {
		return "", protocolError(ErrInvalidRequest, "video_request_invalid")
	}
	return downstream.Prompt, nil
}

func buildMediaStatusGET(ctx context.Context, input BuildInput, path, taskID string) (*http.Request, error) {
	if !ValidMediaTaskID(taskID) || strings.TrimSpace(input.APIKey) == "" {
		return nil, protocolError(ErrInvalidConfiguration, "media_status_binding_invalid")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(input.BaseURL, "/")+path+taskID, nil)
	if err != nil {
		return nil, protocolError(ErrInvalidConfiguration, "media_status_binding_invalid")
	}
	req.Header.Set("Authorization", "Bearer "+input.APIKey)
	return req, nil
}

// NextMediaTaskStatusRead keeps the normal 15-second observations without
// spending the entire read budget while the original task remains recoverable.
func NextMediaTaskStatusRead(now, readUntil time.Time) time.Time {
	age := MediaTaskReadWindow - readUntil.Sub(now)
	var interval time.Duration
	switch {
	case age < 5*time.Minute:
		interval = MediaTaskPollInterval
	case age < 15*time.Minute:
		interval = time.Minute
	case age < time.Hour:
		interval = 15 * time.Minute
	default:
		interval = 4 * time.Hour
	}
	next := now.Add(interval)
	last := readUntil.Add(-2 * MediaTaskPollInterval)
	if next.After(last) {
		if now.Before(last) {
			return last
		}
		return readUntil
	}
	return next
}
