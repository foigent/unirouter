package endpointprotocol

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/url"
)

type mediaFormField struct {
	Name, Value string
}

func buildMediaMultipart(ctx context.Context, origin *url.URL, path string, input BuildInput,
	filename string, content []byte, fields ...mediaFormField,
) (*http.Request, error) {
	var wire bytes.Buffer
	form := multipart.NewWriter(&wire)
	for _, field := range fields {
		if err := form.WriteField(field.Name, field.Value); err != nil {
			return nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
		}
	}
	file, err := form.CreateFormFile("file", filename)
	if err != nil {
		return nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
	}
	if _, err := file.Write(content); err != nil {
		return nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
	}
	if err := form.Close(); err != nil {
		return nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
	}
	req, err := buildPOSTRequest(ctx, origin, path, input.APIKey, input.RequestID, wire.Bytes())
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	return req, nil
}
