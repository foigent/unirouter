package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestContentReclaimCLIRejectsMissingOfflineProofBeforeIO(t *testing.T) {
	for _, args := range [][]string{nil, {"check"}, {"apply", "--root", "/private/synthetic", "--token-file", "sensitive-value"}, {"apply", "--token", "secret-value"}, {"unknown"}} {
		var output bytes.Buffer
		err := runContentReclaimCLI(t.Context(), args, &output)
		if err == nil || output.Len() != 0 || strings.Contains(err.Error(), "sensitive-value") || strings.Contains(err.Error(), "secret-value") {
			t.Fatal("invalid inputs must reject without echo or output")
		}
	}
}
