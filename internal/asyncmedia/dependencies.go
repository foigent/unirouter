package asyncmedia

// historicalDependencies returns the exact Endpoint Protocol evidence that the
// pre-P7-D03 planning snapshot reviewed. It must not follow the mutable current
// catalog: doing so would rewrite historical evidence whenever a later product
// decision advances the runtime.
func historicalDependencies() ([]ArtifactIdentity, error) {
	return []ArtifactIdentity{
		{
			Path:   "internal/endpointprotocol/artifacts/endpoint-profile.v1.schema.json",
			SHA256: "sha256:e1546a5a7638e400f89b43ea6684e2879a072eb13f0a74a312979aea356752cc",
		},
		{
			Path:   "internal/endpointprotocol/artifacts/fixtures/openai-gpt-image-v1.json",
			SHA256: "sha256:3b802f15539ff4fe0309ca93a779a3639da5910b0f28df3087aa24520fa16755",
		},
		{
			Path:   imageManifestPath,
			SHA256: "sha256:792d54b5341ae13425afd24ffa864c068b705043f2b09f29c6c717963b9febd7",
		},
		{
			Path:   "internal/endpointprotocol/artifacts/reports/core.v1.json",
			SHA256: "sha256:ab3d4ec66077922ce921f1def0ccfa501ba8bf5866190c3581fa5c71a91b5759",
		},
	}, nil
}
