package orgdirectory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// SnapshotIdentity returns a deterministic product-local identity for one
// directory projection snapshot. It is deliberately independent from the
// shared Event Envelope hash domain.
func SnapshotIdentity(snapshot ProjectionSnapshot) (string, error) {
	if err := ValidateSnapshot(snapshot); err != nil {
		return "", err
	}

	canonical := snapshot
	canonical.SourceRef = strings.TrimSpace(canonical.SourceRef)
	canonical.SourceRevision = strings.TrimSpace(canonical.SourceRevision)
	canonical.PreviousRevision = strings.TrimSpace(canonical.PreviousRevision)
	canonical.Units = append([]OrganizationUnitProjection{}, snapshot.Units...)
	canonical.People = append([]PersonProjection{}, snapshot.People...)
	canonical.Accounts = append([]AccountBindingProjection{}, snapshot.Accounts...)
	canonical.Gaps = append([]Gap{}, snapshot.Gaps...)

	sort.Slice(canonical.Units, func(i, j int) bool {
		return canonical.Units[i].UnitRef < canonical.Units[j].UnitRef
	})
	sort.Slice(canonical.People, func(i, j int) bool {
		return canonical.People[i].PersonRef < canonical.People[j].PersonRef
	})
	sort.Slice(canonical.Accounts, func(i, j int) bool {
		return canonical.Accounts[i].AccountRef < canonical.Accounts[j].AccountRef
	})
	sort.Slice(canonical.Gaps, func(i, j int) bool {
		if canonical.Gaps[i].Code == canonical.Gaps[j].Code {
			if canonical.Gaps[i].Provenance == canonical.Gaps[j].Provenance {
				return canonical.Gaps[i].Detail < canonical.Gaps[j].Detail
			}
			return canonical.Gaps[i].Provenance < canonical.Gaps[j].Provenance
		}
		return canonical.Gaps[i].Code < canonical.Gaps[j].Code
	})

	wire, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(wire)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
