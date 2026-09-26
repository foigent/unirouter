package sourcefacts

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/uniroute/unirouter/internal/eventconnector"
)

// Exercise the actual Go codecs against the accepted C0 inputs. This reuses the
// original vectors without regenerating identities, hashes, or similar fixtures.
// Other C0 schemas (including XI) retain their original offline validator; this
// test claims only the implemented sourcefacts codecs listed below.
func TestContentContractOriginalC0Vectors(t *testing.T) {
	type vector struct {
		ID       string          `json:"id"`
		Schema   string          `json:"schema"`
		Document json.RawMessage `json:"document"`
	}
	var input struct {
		Valid   []vector `json:"valid"`
		Invalid []vector `json:"invalid"`
	}
	wire, err := os.ReadFile(filepath.Join("..", "..", "contracts", "source-content", "v1", "vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(wire, &input); err != nil {
		t.Fatal(err)
	}
	decoders := map[string]func([]byte) error{
		"Manifest":      func(b []byte) error { _, err := DecodeContentManifest(b); return err },
		"ReadMetadata":  func(b []byte) error { _, err := DecodeContentReadMetadata(b); return err },
		"ContentAccess": func(b []byte) error { var a ContentAccess; return decodeContentCanonical(b, &a) },
		"Capabilities":  func(b []byte) error { _, err := DecodeContentCapabilities(b); return err },
		"Availability":  func(b []byte) error { _, err := DecodeContentAvailability(b); return err },
		"ManifestEntry": func(b []byte) error { _, err := DecodeContentManifestEntry(b); return err },
		"ManifestPage":  func(b []byte) error { _, err := DecodeContentManifestPage(b); return err },
	}
	counts := [2]int{}
	for i, group := range [][]vector{input.Valid, input.Invalid} {
		for _, v := range group {
			decode, covered := decoders[v.Schema]
			if !covered {
				continue
			}
			counts[i]++
			t.Run(v.ID, func(t *testing.T) {
				// RawMessage has original pretty-print whitespace. Canonicalize the
				// JSON value with the product helper before testing its wire codec.
				var value any
				parser := json.NewDecoder(bytes.NewReader(v.Document))
				parser.UseNumber()
				if err := parser.Decode(&value); err != nil {
					t.Fatal(err)
				}
				value, err = contentCanonicalIntegers(value)
				if err != nil {
					t.Fatal(err)
				}
				canonical, canonicalErr := eventconnector.CanonicalJSON(value)
				if canonicalErr != nil {
					// The original null_capture negative cannot be represented by
					// the null-free canonicalizer. Pass its actual JSON to the
					// codec's strict input check instead of changing that vector.
					if i != 1 || v.ID != "null_capture" {
						t.Fatal(canonicalErr)
					}
					canonical = v.Document
				}
				err := decode(canonical)
				if i == 0 && err != nil {
					t.Fatalf("accepted %s vector rejected: %v", v.Schema, err)
				}
				if i == 1 && err == nil {
					t.Fatalf("invalid %s vector accepted", v.Schema)
				}
			})
		}
	}
	if counts != [2]int{25, 19} {
		t.Fatalf("original C0 vector coverage changed: valid=%d invalid=%d", counts[0], counts[1])
	}
}
