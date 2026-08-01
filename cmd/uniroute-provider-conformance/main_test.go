package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/uniroute/unirouter/internal/providerpack"
)

func TestRunWritesDeterministicOfflineReport(t *testing.T) {
	reportOne := filepath.Join(t.TempDir(), "report-one.json")
	reportTwo := filepath.Join(t.TempDir(), "report-two.json")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if err := run(context.Background(), []string{"--report", reportOne}, &stdout, &stderr); err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	if err := run(context.Background(), []string{"--report", reportTwo}, &stdout, &stderr); err != nil {
		t.Fatalf("second run failed: %v", err)
	}
	first, err := os.ReadFile(reportOne)
	if err != nil {
		t.Fatalf("read first report: %v", err)
	}
	second, err := os.ReadFile(reportTwo)
	if err != nil {
		t.Fatalf("read second report: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("report bytes differ\nfirst=%s\nsecond=%s", first, second)
	}
	var report providerpack.ConformanceReport
	if err := json.Unmarshal(first, &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report.Summary.Packs != 3 || report.Summary.Cases != 20 ||
		report.Summary.Passed != 20 || report.Summary.Failed != 0 {
		t.Fatalf("unexpected report summary: %+v", report.Summary)
	}
	if stdout.Len() != 0 {
		t.Fatalf("report path mode wrote to stdout: %q", stdout.String())
	}
}

func TestRunRejectsUnexpectedArguments(t *testing.T) {
	if err := run(context.Background(), []string{"unexpected"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("expected positional argument rejection")
	}
}
