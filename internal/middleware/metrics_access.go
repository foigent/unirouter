package middleware

import (
	"net"
	"net/http"
)

func MetricsCIDRAllowlist(cidrs []string) func(http.Handler) http.Handler {
	networks := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err == nil {
			networks = append(networks, network)
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(networks) == 0 || remoteAddrAllowed(r.RemoteAddr, networks) {
				next.ServeHTTP(w, r)
				return
			}
			http.NotFound(w, r)
		})
	}
}

func remoteAddrAllowed(remoteAddr string, networks []*net.IPNet) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
