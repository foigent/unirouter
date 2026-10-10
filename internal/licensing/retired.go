package licensing

import "time"

// StatusNotRequired is a compatibility response, never proof of a valid license.
const StatusNotRequired = "not_required"

func NotRequiredStatus() Status {
	return Status{Status: StatusNotRequired, CheckedAt: time.Now().UTC().Format(time.RFC3339)}
}

// NotRequiredCapabilities preserves /me's deprecated shape. These flags only
// indicate the absence of commercial gates; role and resource checks still apply.
func NotRequiredCapabilities() CapabilityStatus {
	features := make(map[string]bool, len(concreteFeatures))
	for _, feature := range concreteFeatures {
		features[feature] = true
	}
	return CapabilityStatus{Status: StatusNotRequired, Features: features}
}
