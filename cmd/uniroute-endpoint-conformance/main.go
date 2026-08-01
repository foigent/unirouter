package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/uniroute/unirouter/internal/endpointprotocol"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("uniroute-endpoint-conformance", flag.ContinueOnError)
	flags.SetOutput(stderr)
	reportPath := flags.String("report", "", "write the deterministic JSON report to this path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	report, err := endpointprotocol.RunOffline(ctx, endpointprotocol.NewRegistry())
	if err != nil {
		return fmt.Errorf("run endpoint conformance: %w", err)
	}
	reportBytes, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode endpoint conformance report: %w", err)
	}
	reportBytes = append(reportBytes, '\n')
	if *reportPath != "" {
		if err := os.WriteFile(*reportPath, reportBytes, 0o644); err != nil {
			return fmt.Errorf("write endpoint conformance report: %w", err)
		}
	} else if _, err := stdout.Write(reportBytes); err != nil {
		return fmt.Errorf("write endpoint conformance report: %w", err)
	}
	if report.Summary.Failed != 0 {
		return fmt.Errorf("endpoint conformance failed: %d/%d cases", report.Summary.Failed, report.Summary.Cases)
	}
	fmt.Fprintf(stderr, "endpoint conformance passed: profiles=%d cases=%d\n", report.Summary.Profiles, report.Summary.Passed)
	return nil
}
