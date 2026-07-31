package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/uniroute/unirouter/internal/riskcontrol"
)

func main() {
	reportPath := flag.String(
		"report",
		"",
		"write the deterministic risk-control conformance report to this path",
	)
	flag.Parse()
	if *reportPath == "" {
		fmt.Fprintln(os.Stderr, "--report is required")
		os.Exit(2)
	}
	report, err := riskcontrol.RunConformance()
	if err != nil {
		fmt.Fprintf(os.Stderr, "run conformance: %v\n", err)
		os.Exit(1)
	}
	body, err := riskcontrol.MarshalConformanceReport(report)
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal conformance report: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*reportPath, body, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "write conformance report: %v\n", err)
		os.Exit(1)
	}
	if report.Summary.Failed != 0 {
		fmt.Fprintf(
			os.Stderr,
			"%d conformance cases failed\n",
			report.Summary.Failed,
		)
		os.Exit(1)
	}
}
