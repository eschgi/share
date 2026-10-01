// Package homenet says which addresses are on a home network or this machine: the only places
// where Share speaks plain http. Anywhere else, passwords, keys and files would cross the
// internet unencrypted.
package homenet

import (
	"net/netip"
	"strings"
)

// Addr reports whether a is this machine or on a private network: 127.0.0.0/8 and ::1,
// 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, fc00::/7, and link-local addresses.
func Addr(a netip.Addr) bool {
	a = a.Unmap()
	return a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast()
}

// homeSuffixes are names that only a home network resolves: mDNS (.local), RFC 8375
// (.home.arpa) and the name ICANN keeps for private use (.internal).
var homeSuffixes = []string{".local", ".home.arpa", ".internal"}

// Host reports whether the host of a URL is such an address, localhost, or one of those
// names. A name that public DNS could answer doesn't count: it could lead to the internet.
func Host(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if a, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		return Addr(a)
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	for _, s := range homeSuffixes {
		if strings.HasSuffix(host, s) && len(host) > len(s) {
			return true
		}
	}
	return false
}
