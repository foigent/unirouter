package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	flags := flag.NewFlagSet("uniroute-capability-contract-generate", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	publicationID := flags.String("publication", "", "exact compiled publication identity")
	migrationsDir := flags.String("migrations-dir", "", "repository migration artifact directory")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*publicationID) == "" || strings.TrimSpace(*migrationsDir) == "" {
		return fmt.Errorf("--publication and --migrations-dir are required and positional arguments are unsupported")
	}
	declaration, predecessor, err := publication.CompiledPublication(strings.TrimSpace(*publicationID))
	if err != nil {
		return err
	}
	if err := publication.WriteGeneratedMigrationFiles(strings.TrimSpace(*migrationsDir), predecessor, declaration); err != nil {
		return err
	}
	return publication.VerifyGeneratedMigrationFiles(strings.TrimSpace(*migrationsDir), predecessor, declaration)
}
