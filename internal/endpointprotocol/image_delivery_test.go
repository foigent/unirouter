package endpointprotocol

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"testing"
)

func TestFlareDeliveryChecksExplicitImageControls(t *testing.T) {
	makeImage := func(size int, format string) []byte {
		t.Helper()
		var out bytes.Buffer
		img := image.NewRGBA(image.Rect(0, 0, size, size))
		var err error
		if format == "jpeg" {
			err = jpeg.Encode(&out, img, nil)
		} else {
			err = png.Encode(&out, img)
		}
		if err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}
	square, small, squareJPEG := makeImage(1024, "png"), makeImage(2, "png"), makeImage(1024, "jpeg")
	for _, tc := range []struct {
		name, model, controls, want string
		image                       []byte
		legacy                      bool
	}{
		{"explicit match", OpenAIImageFlare, `,"size":"1024x1024","output_format":"png"`, "", square, false},
		{"explicit dimensions disagree", OpenAIImageFlare, `,"size":"1024x1024","output_format":"png"`, "image_dimensions_invalid", small, false},
		{"explicit MIME disagrees", OpenAIImageFlare, `,"size":"1024x1024","output_format":"png"`, "image_format_mismatch", squareJPEG, false},
		{"truncated PNG", OpenAIImageFlare, `,"size":"1024x1024","output_format":"png"`, "image_invalid_png", square[:len(square)-16], false},
		{"explicit JPEG", OpenAIImageFlare, `,"output_format":"jpeg"`, "", squareJPEG, false},
		{"automatic size keeps old behavior", OpenAIImageFlare, `,"size":"auto"`, "", small, false},
		{"unspecified controls keep old behavior", OpenAIImageFlare, "", "", small, false},
		{"legacy Flare output", OpenAIImageFlare, `,"size":"1024x1024","output_format":"png"`, "", small, true},
		{"legacy Image V1 output", "gpt-image-1", `,"size":"1024x1024","output_format":"png"`, "", small, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := openAIImageProfile{}
			req, binding, err := p.BuildRequest(t.Context(), BuildInput{
				BaseURL: "https://api.openai.com", APIKey: "synthetic-fixture", RequestedModel: "fixture", MappedModel: tc.model,
				Body: []byte(`{"model":"fixture","prompt":"a square"` + tc.controls + `}`), LegacyImageDelivery: tc.legacy,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer req.Body.Close()
			body := fmt.Sprintf(`{"created":1,"data":[{"b64_json":%q}],"usage":{"input_tokens":3,"input_tokens_details":{"text_tokens":3,"image_tokens":0},"output_tokens":2,"total_tokens":5}}`,
				base64.StdEncoding.EncodeToString(tc.image))
			outcome, err := p.ParseResponse(t.Context(), binding, http.StatusOK, []byte(body))
			if tc.want != "" {
				var protocol *ProtocolError
				if !errors.As(err, &protocol) || protocol.Code != tc.want || outcome.ProviderState != "" {
					t.Fatalf("error=%v state=%s want %s", err, outcome.ProviderState, tc.want)
				}
			} else if err != nil || outcome.ProviderState != "completed" {
				t.Fatalf("error=%v state=%s", err, outcome.ProviderState)
			}
		})
	}
}
