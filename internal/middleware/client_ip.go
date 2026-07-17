package middleware

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

const maxForwardedForHops = 32

// ClientIPResolver resolves a request source without trusting forwarding
// headers from arbitrary callers. X-Forwarded-For and X-Real-IP are consulted
// only when the direct network peer is in TrustedProxyCIDRs. Forwarded chains
// are then walked from right to left so client-supplied entries cannot override
// the first untrusted hop appended by a trusted proxy.
type ClientIPResolver struct {
	trustedProxyCIDRs []netip.Prefix
}

func NewClientIPResolver(trustedProxyCIDRs []string) (ClientIPResolver, error) {
	resolver := ClientIPResolver{
		trustedProxyCIDRs: make([]netip.Prefix, 0, len(trustedProxyCIDRs)),
	}
	for _, raw := range trustedProxyCIDRs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return ClientIPResolver{}, fmt.Errorf("invalid trusted proxy CIDR %q: %w", raw, err)
		}
		prefix, err = canonicalPrefix(prefix)
		if err != nil {
			return ClientIPResolver{}, fmt.Errorf("invalid trusted proxy CIDR %q: %w", raw, err)
		}
		if prefix.Bits() == 0 {
			return ClientIPResolver{}, fmt.Errorf("invalid trusted proxy CIDR %q: trusting every address is forbidden", raw)
		}
		resolver.trustedProxyCIDRs = append(resolver.trustedProxyCIDRs, prefix.Masked())
	}
	return resolver, nil
}

// Resolve returns the canonical client IP, or a stable unknown marker when the
// server-owned RemoteAddr is not a valid IP address. An empty trusted-proxy
// list is fail-safe: forwarding headers are ignored and RemoteAddr is used.
func (r ClientIPResolver) Resolve(request *http.Request) string {
	if request == nil {
		return "__unknown__"
	}
	peer, ok := parseRemoteIP(request.RemoteAddr)
	if !ok {
		return "__unknown__"
	}
	if !r.isTrustedProxy(peer) {
		return peer.String()
	}

	if values, present := request.Header[http.CanonicalHeaderKey("X-Forwarded-For")]; present {
		chain := strings.Split(strings.Join(values, ","), ",")
		if len(chain) == 0 || len(chain) > maxForwardedForHops {
			return peer.String()
		}
		current := peer
		for index := len(chain) - 1; index >= 0; index-- {
			candidate, valid := parseForwardedIP(chain[index])
			if !valid {
				return peer.String()
			}
			current = candidate
			if !r.isTrustedProxy(current) {
				return current.String()
			}
		}
		return current.String()
	}

	if values, present := request.Header[http.CanonicalHeaderKey("X-Real-Ip")]; present {
		if len(values) != 1 {
			return peer.String()
		}
		candidate, valid := parseForwardedIP(values[0])
		if !valid {
			return peer.String()
		}
		return candidate.String()
	}
	return peer.String()
}

func (r ClientIPResolver) isTrustedProxy(address netip.Addr) bool {
	address = address.Unmap()
	for _, prefix := range r.trustedProxyCIDRs {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func canonicalPrefix(prefix netip.Prefix) (netip.Prefix, error) {
	address := prefix.Addr()
	if address.Zone() != "" {
		return netip.Prefix{}, fmt.Errorf("zones are not allowed")
	}
	if !address.Is4In6() {
		return prefix, nil
	}
	bits := prefix.Bits() - 96
	if bits < 0 {
		return netip.Prefix{}, fmt.Errorf("IPv4-mapped prefix is broader than IPv4")
	}
	return netip.PrefixFrom(address.Unmap(), bits), nil
}

func parseRemoteIP(remoteAddress string) (netip.Addr, bool) {
	host := strings.TrimSpace(remoteAddress)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return address.WithZone("").Unmap(), true
}

func parseForwardedIP(raw string) (netip.Addr, bool) {
	address, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil || address.Zone() != "" {
		return netip.Addr{}, false
	}
	return address.Unmap(), true
}

// getClientIP preserves the package-local helper used by older callers and
// tests, but now has safe direct-peer semantics. Production proxy-aware paths
// receive an explicitly configured ClientIPResolver.
func getClientIP(request *http.Request) string {
	return (ClientIPResolver{}).Resolve(request)
}
