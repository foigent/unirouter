// Package ipallowlist defines the canonical ordinary API-key IP/CIDR
// restriction contract shared by credential provisioning and Gateway auth.
package ipallowlist

import (
	"errors"
	"net/netip"
	"slices"
	"strings"
)

const (
	MaxEntries = 32
	MaxBytes   = 4096
)

var ErrInvalid = errors.New("invalid IP allowlist")

// Normalize returns a stable comma-separated list. Empty means unrestricted.
// Prefixes are masked, mapped IPv6 addresses are rejected as ambiguous, and a
// /0 prefix is rejected because an empty value is the explicit unrestricted
// representation.
func Normalize(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if len(raw) > MaxBytes {
		return "", ErrInvalid
	}
	parts := strings.Split(raw, ",")
	if len(parts) == 0 || len(parts) > MaxEntries {
		return "", ErrInvalid
	}
	values := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		value, err := normalizeEntry(strings.TrimSpace(part))
		if err != nil {
			return "", ErrInvalid
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	slices.Sort(values)
	return strings.Join(values, ","), nil
}

func normalizeEntry(raw string) (string, error) {
	if raw == "" {
		return "", ErrInvalid
	}
	if strings.Contains(raw, "/") {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil || prefix.Addr().Zone() != "" || prefix.Addr().Is4In6() || prefix.Bits() == 0 {
			return "", ErrInvalid
		}
		return prefix.Masked().String(), nil
	}
	address, err := netip.ParseAddr(raw)
	if err != nil || address.Zone() != "" || address.Is4In6() {
		return "", ErrInvalid
	}
	return address.String(), nil
}

// Matches is deliberately tolerant of invalid legacy entries: they never
// match. Newly provisioned grant credentials are normalized before storage.
func Matches(clientIP, raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return true
	}
	client, err := netip.ParseAddr(strings.TrimSpace(clientIP))
	if err != nil || client.Zone() != "" {
		return false
	}
	client = client.Unmap()
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "/") {
			prefix, parseErr := netip.ParsePrefix(part)
			if parseErr == nil && prefix.Addr().Zone() == "" && prefix.Contains(client) {
				return true
			}
			continue
		}
		address, parseErr := netip.ParseAddr(part)
		if parseErr == nil && address.Zone() == "" && address.Unmap() == client {
			return true
		}
	}
	return false
}
