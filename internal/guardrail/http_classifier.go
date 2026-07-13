package guardrail

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const ExternalClassifierContractVersion = "unirouter.guardrail-classifier.v1"

type HTTPClassifierOptions struct {
	Name              string
	Version           string
	Endpoint          string
	Client            *http.Client
	AllowInsecureHTTP bool
}

// HTTPClassifier is a platform-wired generic external detector adapter. Its
// endpoint is fixed at process assembly and never accepted from tenant policy
// JSON, preventing guardrail configuration from becoming an SSRF surface.
// Service.detect owns the one-second hard deadline and deliberately performs
// no automatic retry.
type HTTPClassifier struct {
	version  string
	endpoint string
	client   *http.Client
}

func NewHTTPClassifier(options HTTPClassifierOptions) (*HTTPClassifier, error) {
	options.Name = strings.TrimSpace(options.Name)
	options.Version = strings.TrimSpace(options.Version)
	options.Endpoint = strings.TrimSpace(options.Endpoint)
	if options.Name == "" || options.Version == "" || len(options.Name) > 80 || len(options.Version) > 80 {
		return nil, fmt.Errorf("external classifier identity is invalid")
	}
	parsed, err := url.Parse(options.Endpoint)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("external classifier endpoint is invalid")
	}
	if parsed.Scheme != "https" && !(options.AllowInsecureHTTP && parsed.Scheme == "http") {
		return nil, fmt.Errorf("external classifier endpoint must use HTTPS")
	}
	client := options.Client
	if client == nil {
		client = &http.Client{}
	}
	clientCopy := *client
	if clientCopy.Timeout <= 0 || clientCopy.Timeout > time.Second {
		clientCopy.Timeout = time.Second
	}
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &HTTPClassifier{version: options.Version, endpoint: options.Endpoint, client: &clientCopy}, nil
}

func (c *HTTPClassifier) Version() string {
	if c == nil {
		return ""
	}
	return c.version
}

func (c *HTTPClassifier) Detect(ctx Context, text Text, config json.RawMessage) ([]Finding, error) {
	if c == nil || c.client == nil {
		return nil, ErrDetectorMissing
	}
	requestBody, err := json.Marshal(map[string]interface{}{
		"contract_version": ExternalClassifierContractVersion,
		"path":             text.Path, "text": text.Content, "config": json.RawMessage(config),
	})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(contextAdapter{ctx}, http.MethodPost, c.endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "UniRouter-Guardrail/"+ExternalClassifierContractVersion)
	response, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 64*1024 {
		return nil, fmt.Errorf("external classifier response exceeds 64 KiB")
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, fmt.Errorf("external classifier returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		ContractVersion string    `json:"contract_version"`
		Findings        []Finding `json:"findings"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode external classifier response: %w", err)
	}
	if payload.ContractVersion != ExternalClassifierContractVersion || len(payload.Findings) > 256 {
		return nil, fmt.Errorf("external classifier response contract is invalid")
	}
	for _, finding := range payload.Findings {
		if strings.TrimSpace(finding.Category) == "" || len(finding.Category) > 80 ||
			finding.Start < 0 || finding.End <= finding.Start || finding.End > len(text.Content) ||
			len(finding.ConfidenceBucket) > 24 {
			return nil, fmt.Errorf("external classifier finding is invalid")
		}
	}
	// Service.normalizeFindings performs the authoritative sort, UTF-8 boundary
	// validation and overlap reduction. Do not dedupe detector-controlled order
	// here because an unsorted response could otherwise suppress an earlier span.
	return payload.Findings, nil
}

// contextAdapter preserves the standard context contract while Detector keeps
// its public dependency surface narrow.
type contextAdapter struct{ Context }

func (c contextAdapter) Value(any) any { return nil }
