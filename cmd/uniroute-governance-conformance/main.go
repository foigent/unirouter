package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/uniroute/unirouter/internal/governancesim"
)

func main() {
	reportPath := flag.String("report", "", "write the deterministic JSON report to this path")
	flag.Parse()

	report, err := governancesim.RunConformance()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	body, err := governancesim.MarshalConformanceReport(report)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *reportPath == "" {
		if _, err := os.Stdout.Write(body); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	} else if err := os.WriteFile(*reportPath, body, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if report.Summary.Failed != 0 {
		os.Exit(1)
	}
}
