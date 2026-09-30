package auth

import (
	"net/http"
	"net/netip"
	"strings"
)

// ClientIP returns the address a request came from. Behind cloudflared every request comes
// from the tunnel, so the real address is taken from header — but only when the direct peer
// is a trusted proxy; anyone else could send that header too.
func ClientIP(r *http.Request, proxies []netip.Prefix, header string) netip.Addr {
	peer := netip.Addr{}
	if ap, err := netip.ParseAddrPort(r.RemoteAddr); err == nil {
		peer = ap.Addr().Unmap()
	}
	if header == "" || !isTrusted(peer, proxies) {
		return peer
	}
	v := strings.TrimSpace(r.Header.Get(header))
	if i := strings.IndexByte(v, ','); i >= 0 { // X-Forwarded-For style lists: the first is the client
		v = strings.TrimSpace(v[:i])
	}
	if a, err := netip.ParseAddr(v); err == nil {
		return a.Unmap()
	}
	return peer
}

func isTrusted(a netip.Addr, proxies []netip.Prefix) bool {
	if !a.IsValid() {
		return false
	}
	for _, p := range proxies {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// IPKey groups addresses for rate limits. An IPv6 user usually controls a whole /64, so
// that is what counts as one address.
func IPKey(a netip.Addr) string {
	if a.Is6() {
		if p, err := a.Prefix(64); err == nil {
			return p.String()
		}
	}
	return a.String()
}
