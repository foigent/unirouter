package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/uniroute/unirouter/internal/asyncmedia"
)

func main() {
	reportPath := flag.String(
		"report",
		"",
		"write the deterministic pre-gate media conformance report to this path",
	)
	flag.Parse()
	if *reportPath == "" {
		fmt.Fprintln(os.Stderr, "--report is required")
		os.Exit(2)
	}
	report, err := asyncmedia.RunConformance()
	if err != nil {
		fmt.Fprintf(os.Stderr, "run async-media conformance: %v\n", err)
		os.Exit(1)
	}
	body, err := asyncmedia.MarshalConformanceReport(report)
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal async-media conformance: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*reportPath, body, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "write async-media conformance: %v\n", err)
		os.Exit(1)
	}
	if report.Summary.Failed != 0 {
		fmt.Fprintf(os.Stderr, "%d async-media conformance cases failed\n", report.Summary.Failed)
		os.Exit(1)
	}
}
