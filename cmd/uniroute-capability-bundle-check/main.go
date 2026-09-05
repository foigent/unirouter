// An offline source validator. It has no database, publication or Provider I/O.
package main

import (
	"fmt"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	qualification "github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"io"
	"os"
)

func main() {
	wire, err := io.ReadAll(io.LimitReader(os.Stdin, publication.MaximumBundleBytes+1))
	if err == nil {
		err = qualification.ValidateBundleSource(wire)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Canonical Bundle source and installed bindings validated; no Target or qualification claims")
}
