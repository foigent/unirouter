package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/uniroute/unirouter/internal/endpointprotocol"
)

func TestRunWritesPassingReportToStdout(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(context.Background(), nil, &stdout, &stderr); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	var report endpointprotocol.ConformanceReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}
	if report.Summary.Profiles != 3 || report.Summary.Cases != 25 ||
		report.Summary.Passed != 25 || report.Summary.Failed != 0 {
		t.Fatalf("summary=%+v", report.Summary)
	}
	if stderr.String() != "endpoint conformance passed: profiles=3 cases=25\n" {
		t.Fatalf("stderr=%q", stderr.String())
	}
}
