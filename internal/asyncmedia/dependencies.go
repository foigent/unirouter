package asyncmedia

import (
	"errors"

	"github.com/uniroute/unirouter/internal/endpointprotocol"
)

func authoritativeDependencies() ([]ArtifactIdentity, error) {
	schemaSHA, err := endpointprotocol.SchemaDigest()
	if err != nil {
		return nil, err
	}
	goldenSHA, err := endpointprotocol.GoldenReportDigest()
	if err != nil {
		return nil, err
	}
	catalog, err := endpointprotocol.Catalog()
	if err != nil {
		return nil, err
	}
	for _, artifact := range catalog {
		if artifact.Manifest.ProfileID != endpointprotocol.ProfileOpenAIImageGPT {
			continue
		}
		if len(artifact.Manifest.Fixtures) != 1 {
			return nil, errors.New("asyncmedia: image endpoint fixture identity unavailable")
		}
		fixture := artifact.Manifest.Fixtures[0]
		return []ArtifactIdentity{
			{
				Path:   "internal/endpointprotocol/artifacts/endpoint-profile.v1.schema.json",
				SHA256: schemaSHA,
			},
			{
				Path:   "internal/endpointprotocol/artifacts/" + fixture.Path,
				SHA256: fixture.SHA256,
			},
			{Path: imageManifestPath, SHA256: artifact.ManifestSHA256},
			{
				Path:   "internal/endpointprotocol/artifacts/reports/core.v1.json",
				SHA256: goldenSHA,
			},
		}, nil
	}
	return nil, errors.New("asyncmedia: image endpoint profile identity unavailable")
}
