package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/uniroute/unirouter/internal/providercogs"
)

func main() {
	inputPath := flag.String("input", "", "read one offline provider COGS JSON import")
	reportPath := flag.String("report", "", "write the evaluated JSON report")
	flag.Parse()
	if *inputPath == "" || *reportPath == "" {
		fmt.Fprintln(os.Stderr, "usage: uniroute-provider-cogs-preview --input <input.json> --report <report.json>")
		os.Exit(2)
	}
	body, err := os.ReadFile(*inputPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read input failed")
		os.Exit(1)
	}
	report, err := providercogs.Evaluate(body)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	output, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "encode report failed")
		os.Exit(1)
	}
	output = append(output, '\n')
	if err := os.WriteFile(*reportPath, output, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "write report failed")
		os.Exit(1)
	}
}
