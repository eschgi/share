package auth

import (
	"context"
	"net/http"
	"net/netip"
	"strings"
)

type clientIPKey struct{}

// WithClientIP records the visitor's address that a trusted proxy named for this request.
func WithClientIP(r *http.Request, a netip.Addr) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), clientIPKey{}, a))
}

// ClientIP returns the address a request came from: the visitor's own when a trusted proxy
// passed it on (see WithClientIP), otherwise the direct peer.
func ClientIP(r *http.Request) netip.Addr {
	if a, ok := r.Context().Value(clientIPKey{}).(netip.Addr); ok {
		return a
	}
	if ap, err := netip.ParseAddrPort(r.RemoteAddr); err == nil {
		return ap.Addr().Unmap().WithZone("")
	}
	return netip.Addr{}
}

// ForwardedFor finds the visitor in the lines of an X-Forwarded-For header. Each proxy appends
// the address it got the request from, so the list is read from the right, past the trusted
// proxies: the first address that isn't one is the visitor's. Whatever stands further left may
// be made up by the visitor. ok is false when the rightmost entry, which the trusted proxy
// wrote itself, isn't an address.
func ForwardedFor(lines []string, proxies []netip.Prefix) (visitor netip.Addr, ok bool) {
	var entries []string
	for _, l := range lines {
		for _, e := range strings.Split(l, ",") {
			entries = append(entries, strings.TrimSpace(e))
		}
	}
	var hop netip.Addr // the leftmost trusted proxy so far
	for i := len(entries) - 1; i >= 0; i-- {
		a, err := parseForwarded(entries[i])
		if err != nil {
			if i == len(entries)-1 {
				return netip.Addr{}, false
			}
			return hop, true // nothing trustworthy further left
		}
		if !isTrusted(a, proxies) {
			return a, true
		}
		hop = a
	}
	return hop, hop.IsValid() // every entry is a trusted proxy: the leftmost is as far as it goes
}

// parseForwarded reads one X-Forwarded-For entry: an address, maybe in brackets and with a
// port, as some proxies write it.
func parseForwarded(s string) (netip.Addr, error) {
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().Unmap().WithZone(""), nil
	}
	a, err := netip.ParseAddr(strings.TrimSuffix(strings.TrimPrefix(s, "["), "]"))
	if err != nil {
		return netip.Addr{}, err
	}
	return a.Unmap().WithZone(""), nil
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
