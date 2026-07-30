package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/uniroute/unirouter/internal/memberlifecycle"
)

func main() {
	reportPath := flag.String("report", "", "write the deterministic JSON report to this path")
	flag.Parse()

	report, err := memberlifecycle.RunConformance()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	body, err := memberlifecycle.MarshalConformanceReport(report)
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
