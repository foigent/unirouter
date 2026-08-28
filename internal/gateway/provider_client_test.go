package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/models"
)

func TestProviderClientDoesNotFollowRedirects(t *testing.T) {
	for _, status := range []int{200, 301, 302, 303, 307, 308} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			client := (&Relay{}).providerClient(time.Second).(*http.Client)
			calls := 0
			client.Transport = executionRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if calls > 1 {
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: req}, nil
				}
				return &http.Response{
					StatusCode: status,
					Header:     http.Header{"Location": []string{"/redirected"}},
					Body:       io.NopCloser(strings.NewReader("redirect response")),
					Request:    req,
				}, nil
			})
			adapter := adapters.NewOpenAICompatible()
			request, err := adapter.BuildRequest(context.Background(), adapters.RequestInput{
				Channel:        models.Channel{BaseURL: "https://provider.example"},
				Endpoint:       adapters.EndpointChatCompletions,
				RequestedModel: "test-model",
				DownstreamBody: []byte(`{"model":"test-model","messages":[{"role":"user","content":"hello"}],"max_tokens":1,"stream":false}`),
			})
			if err != nil {
				t.Fatal(err)
			}
			if request.GetBody == nil {
				t.Fatal("fixture must exercise the adapter's replayable request body")
			}
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if calls != 1 {
				t.Fatalf("one provider attempt dispatched %d HTTP requests; want exactly 1", calls)
			}
			if response.StatusCode != status {
				t.Fatalf("status = %d; want original %d", response.StatusCode, status)
			}
			body, err := io.ReadAll(response.Body)
			if err != nil || string(body) != "redirect response" {
				t.Fatalf("original redirect body unavailable: %q, %v", body, err)
			}
		})
	}
}
