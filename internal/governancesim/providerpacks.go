package governancesim

import (
	"sort"
	"strings"

	"github.com/uniroute/unirouter/internal/providerpack"
)

// CurrentProviderPackSnapshots returns normalized, credential-free identities
// from the authoritative embedded Provider Pack catalog.
func CurrentProviderPackSnapshots() ([]ProviderPackSnapshot, error) {
	packs, err := providerpack.Catalog()
	if err != nil {
		return nil, err
	}
	result := make([]ProviderPackSnapshot, 0, len(packs))
	for _, pack := range packs {
		result = append(result, providerPackSnapshot(pack))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].PackID < result[j].PackID })
	return result, nil
}

func providerPackSnapshot(pack providerpack.Pack) ProviderPackSnapshot {
	endpoints := append([]string(nil), pack.Manifest.Endpoints...)
	modalities := append([]string(nil), pack.Manifest.Modalities...)
	authProfiles := make([]string, 0, len(pack.Manifest.AuthProfiles))
	fixtures := make([]FixtureSnapshot, 0, len(pack.Manifest.Fixtures))
	for _, profile := range pack.Manifest.AuthProfiles {
		authProfiles = append(authProfiles, profile.Type+":"+profile.WireHeader)
	}
	for _, fixture := range pack.Manifest.Fixtures {
		fixtures = append(fixtures, FixtureSnapshot{
			FixtureID: fixture.FixtureID,
			SHA256:    fixture.SHA256,
		})
	}
	sort.Strings(endpoints)
	sort.Strings(modalities)
	sort.Strings(authProfiles)
	sort.Slice(fixtures, func(i, j int) bool { return fixtures[i].FixtureID < fixtures[j].FixtureID })
	return ProviderPackSnapshot{
		SchemaVersion:   pack.Manifest.SchemaVersion,
		SchemaSHA256:    pack.SchemaSHA256,
		PackID:          pack.Manifest.PackID,
		PackVersion:     pack.Manifest.PackVersion,
		ManifestSHA256:  pack.ManifestSHA256,
		Provider:        pack.Manifest.Provider,
		AdapterType:     pack.Manifest.AdapterType,
		NetworkVerified: pack.Manifest.NetworkVerified,
		Endpoints:       endpoints,
		Modalities:      modalities,
		AuthProfiles:    authProfiles,
		UsageSignature: "authority=" + pack.Manifest.UsageProfile.AuthorityWhenComplete +
			";missing=" + pack.Manifest.UsageProfile.MissingBehavior +
			";cache=" + strings.Join(pack.Manifest.UsageProfile.CacheClasses, ","),
		ErrorSignature: "classifier=" + pack.Manifest.ErrorProfile.Classifier +
			";unknown=" + pack.Manifest.ErrorProfile.UnknownBehavior,
		Fixtures: fixtures,
	}
}
