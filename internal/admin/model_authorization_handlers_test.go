package admin

import (
	"regexp"
	"testing"
)

func TestNewModelAuthorizationRefIsOpaqueAndValid(t *testing.T) {
	first, err := newModelAuthorizationRef()
	if err != nil {
		t.Fatalf("generate first authorization ref: %v", err)
	}
	second, err := newModelAuthorizationRef()
	if err != nil {
		t.Fatalf("generate second authorization ref: %v", err)
	}
	if first == second {
		t.Fatalf("authorization refs must be unique: %q", first)
	}
	if !regexp.MustCompile(`^mca_[0-9a-f]{24}$`).MatchString(first) {
		t.Fatalf("authorization ref has unexpected format: %q", first)
	}
}
