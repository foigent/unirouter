package adapters

import (
	"context"
	"net/http"

	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/models"
)

const (
	TypeOpenAICompatible = "openai_compatible"
	TypeAnthropicNative  = "anthropic_native"
	TypeAzureOpenAI      = "azure_openai"
)

type Endpoint string

const (
	EndpointChatCompletions Endpoint = "/v1/chat/completions"
	EndpointEmbeddings      Endpoint = "/v1/embeddings"
)

type FailureKind string

const (
	FailureNone          FailureKind = ""
	FailureClientRequest FailureKind = "client_request"
	FailureAuth          FailureKind = "auth"
	FailureRateLimit     FailureKind = "rate_limit"
	FailureChannel       FailureKind = "channel_failure"
	FailureUnknown       FailureKind = "unknown"
)

type Usage struct {
	PromptTokens         int
	UncachedPromptTokens int
	CacheReadTokens      int
	CacheCreationTokens  int
	CompletionTokens     int
	TotalTokens          int
	Source               string
	Authority            metering.UsageAuthority
	Purpose              metering.UsagePurpose
	Finality             metering.UsageFinality
	ContractVersion      string
	EstimateVersion      string
	CacheUsageSchema     string
	CacheUsageStatus     string
	MeterUsage           []metering.MeterQuantity
}

type ProviderMeta struct {
	Provider       string
	ChannelID      int64
	ChannelName    string
	AdapterType    string
	RequestedModel string
	UpstreamModel  string
	TTFTMillis     int
}

type NormalizedError struct {
	HTTPStatus  int
	Category    string
	Code        string
	Message     string
	Retryable   bool
	FailureKind FailureKind
}

type RequestInput struct {
	Endpoint       Endpoint
	DownstreamBody []byte
	RequestedModel string
	MappedModel    string
	Channel        models.Channel
	APIKey         string
	RequestID      string
}

type ResponseInput struct {
	Endpoint          Endpoint
	StatusCode        int
	Header            http.Header
	Body              []byte
	Channel           models.Channel
	RequestedModel    string
	UpstreamModel     string
	CacheUsageProfile string
	ProtocolProfile   string
	TTFTMillis        int
}

type NormalizedResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	Usage      Usage
	Meta       ProviderMeta
	Error      *NormalizedError
}

type StreamState struct {
	ID                    string
	Created               int64
	ProviderUsageReported bool
	PromptTokens          int
	UncachedPromptTokens  int
	CacheReadTokens       int
	CacheCreationTokens   int
	CompletionTokens      int
	CacheUsageSchema      string
	CacheUsageStatus      string
}

type StreamChunkInput struct {
	Data              []byte
	Channel           models.Channel
	RequestedModel    string
	UpstreamModel     string
	CacheUsageProfile string
	State             *StreamState
}

type NormalizedStreamChunk struct {
	Data      []byte
	Skip      bool
	Done      bool
	Usage     Usage
	HasUsage  bool
	TextDelta string
}

type StreamNormalizer interface {
	NormalizeStreamChunk(ctx context.Context, in StreamChunkInput) (*NormalizedStreamChunk, error)
}

// StreamFrameNormalizer applies provider-specific, content-preserving wire
// normalization to an OpenAI-compatible SSE data frame. It is intentionally
// narrower than StreamNormalizer: the gateway retains ownership of SSE event
// parsing, usage authority, output inspection, and delivery.
type StreamFrameNormalizer interface {
	NormalizeStreamFrame(ctx context.Context, in StreamChunkInput) ([]byte, error)
}

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type TestInput struct {
	Channel models.Channel
	APIKey  string
	Model   string
	Prompt  string
	Client  HTTPDoer
}

type TestResult struct {
	Success    bool
	Status     string
	StatusCode int
	LatencyMs  int64
	Model      string
	// ReportedModel is the exact top-level model identity returned by the
	// provider. It is intentionally separate from Model, which is the identity
	// UniRouter sent upstream. Some providers omit it; callers must represent
	// that as unknown instead of pretending the requested identity was echoed.
	ReportedModel string
	TokensUsed    int
	Body          []byte
	Error         string
	RequestURL    string
}

type Adapter interface {
	Type() string
	BuildRequest(ctx context.Context, in RequestInput) (*http.Request, error)
	ParseResponse(ctx context.Context, in ResponseInput) (*NormalizedResponse, error)
	Test(ctx context.Context, in TestInput) (*TestResult, error)
	ClassifyHTTPStatus(statusCode int, body []byte) *NormalizedError
}

type ConfigValidator interface {
	ValidateConfig(raw []byte) error
}
