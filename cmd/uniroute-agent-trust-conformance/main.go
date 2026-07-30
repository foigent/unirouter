package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/uniroute/unirouter/internal/agenttrust"
)

func main() {
	reportPath := flag.String("report", "", "write the deterministic JSON report to this path")
	flag.Parse()

	report, err := agenttrust.RunConformance()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	body, err := agenttrust.MarshalConformanceReport(report)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *reportPath == "" {
		_, err = os.Stdout.Write(body)
	} else {
		err = os.WriteFile(*reportPath, body, 0o600)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if report.Summary.Failed != 0 {
		os.Exit(1)
	}
}
