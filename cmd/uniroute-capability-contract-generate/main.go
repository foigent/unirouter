package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
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
	return runWithOutput(arguments, os.Stdout)
}

func runWithOutput(arguments []string, output io.Writer) error {
	flags := flag.NewFlagSet("uniroute-capability-contract-generate", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	publicationID := flags.String("publication", "", "exact compiled publication identity")
	migrationsDir := flags.String("migrations-dir", "", "repository migration artifact directory")
	review := flags.Bool("review", false, "print a source-only review summary without writing migration files")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*publicationID) == "" {
		return fmt.Errorf("--publication is required and positional arguments are unsupported")
	}
	if *review == (strings.TrimSpace(*migrationsDir) != "") {
		return fmt.Errorf("specify exactly one of --review or --migrations-dir")
	}
	declaration, predecessor, err := publication.CompiledPublication(strings.TrimSpace(*publicationID))
	if err != nil {
		return err
	}
	if *review {
		return writePublicationReview(output, declaration)
	}
	if err := publication.WriteGeneratedMigrationFiles(strings.TrimSpace(*migrationsDir), predecessor, declaration); err != nil {
		return err
	}
	return publication.VerifyGeneratedMigrationFiles(strings.TrimSpace(*migrationsDir), predecessor, declaration)
}

func writePublicationReview(output io.Writer, declaration publication.Declaration) error {
	digest, err := publication.DeclarationDigest(declaration)
	if err != nil {
		return err
	}
	var summary bytes.Buffer
	fmt.Fprintf(&summary, "Publication %s\nDeclaration SHA-256 %s\nStatic predecessor %s\nSchema predecessor %s\n",
		declaration.PublicationID, digest, declaration.ExpectedStaticPredecessor, declaration.ExpectedSchemaPredecessor)
	fmt.Fprintln(&summary, "Source declaration only. Target state requires a separate plan.")
	fmt.Fprintf(&summary, "\nNew bindings (%d)\n", len(declaration.BindingAppends))
	for _, binding := range declaration.BindingAppends {
		fmt.Fprintf(&summary, "- %s %s, %s/%s, Profile %s@%s, protocol %s@%s, probe %s@%s\n",
			binding.OperationID, binding.CapabilityID, binding.ExpectedProvider, binding.ExpectedUpstreamModel,
			binding.ExecutionProfileID, binding.ExecutionProfileVersion, binding.ProtocolProfileID, binding.ProtocolProfileVersion,
			binding.ProbeContractID, binding.ProbeContractVersion)
	}
	fmt.Fprintf(&summary, "\nCurrent selectors (%d)\n", len(declaration.SelectorTransitions))
	for _, transition := range declaration.SelectorTransitions {
		previous := "(absent)"
		if from := transition.ExpectedPredecessor; from != nil {
			previous = from.ProbeContractID + "@" + from.ProbeContractVersion
		}
		fmt.Fprintf(&summary, "- %s %s, %s@%s / %s@%s: %s -> %s@%s\n",
			transition.OperationID, transition.CapabilityID, transition.ExecutionProfileID, transition.ExecutionProfileVersion,
			transition.ProtocolProfileID, transition.ProtocolProfileVersion, previous,
			transition.Final.ProbeContractID, transition.Final.ProbeContractVersion)
	}
	fmt.Fprintf(&summary, "\nGuarded downgrade\n- Verify the exact predecessor and restore any applied projection effects (%d declared).\n",
		len(declaration.DeclaredProjectionEffects))
	fmt.Fprintln(&summary, "- Retained execution, probe or approval evidence can block downgrade.")
	for _, profile := range declaration.ProfileAttachmentGuards {
		fmt.Fprintf(&summary, "- Retained attachment to %s@%s blocks downgrade.\n", profile.ExecutionProfileID, profile.ExecutionProfileVersion)
	}
	_, err = output.Write(summary.Bytes())
	return err
}
