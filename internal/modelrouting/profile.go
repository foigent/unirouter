package modelrouting

import (
	"fmt"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func resolvePlanProfile(id, version string, d modelprofile.Definition) (modelprofile.Profile, error) {
	if d.Digest() != "" {
		p := d.Profile()
		if p.ID != id || p.Version != version {
			return modelprofile.Profile{}, fmt.Errorf("%w: exact data Profile address", ErrManifestStale)
		}
		return p, nil
	}
	p, ok := modelprofile.Lookup(id, version)
	if !ok {
		return modelprofile.Profile{}, fmt.Errorf("%w: unknown legacy Profile %s@%s", ErrManifestStale, id, version)
	}
	return p, nil
}

// ExecutionProfile never consults a mutable current Profile or falls back from
// a named data definition. All downstream request shaping shares this value.
func (p Plan) ExecutionProfile() (modelprofile.Profile, error) {
	return resolvePlanProfile(p.ExecutionProfileID, p.ExecutionProfileVersion, p.Definition)
}
