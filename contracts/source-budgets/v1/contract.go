// Package sourcebudgetassets defines the independent D snapshot contract.
// It is not an event connector and never changes A/B immutable identities.
package sourcebudgetassets

import (
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
)

const SchemaVersion = "unirouter.budget-snapshot.v1"
const WriterContract = "unirouter.gateway-financial-journal.v2"

//go:embed *.schema.json manifest.json
var assets embed.FS

func Asset(name string) ([]byte, error) { return assets.ReadFile(name) }

func Verify() error {
	wire, err := Asset("manifest.json")
	if err != nil {
		return err
	}
	var manifest struct {
		SchemaVersion string            `json:"schema_version"`
		SourceSchema  string            `json:"source_schema"`
		Files         map[string]string `json:"files"`
		EventFeed     bool              `json:"event_feed"`
	}
	if err = json.Unmarshal(wire, &manifest); err != nil {
		return err
	}
	if manifest.SchemaVersion != "unirouter.budget-contract-manifest.v1" || manifest.SourceSchema != SchemaVersion || manifest.EventFeed {
		return fmt.Errorf("budget contract manifest identity differs")
	}
	for _, name := range []string{"budget-document.schema.json", "budget-reading.schema.json", "budget-catalog.schema.json"} {
		wire, err := Asset(name)
		if err != nil {
			return err
		}
		if manifest.Files[name] != fmt.Sprintf("sha256:%x", sha256.Sum256(wire)) {
			return fmt.Errorf("budget contract schema digest differs: %s", name)
		}
	}
	return nil
}
