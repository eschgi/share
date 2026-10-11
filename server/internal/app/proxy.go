package app

import (
	"encoding/json"
	"log"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/config"
	"github.com/eschgi/share/server/internal/homenet"
	"github.com/eschgi/share/server/internal/httpx"
)

// A proxy in front of Share (Cloudflare's tunnel, Caddy, nginx, Traefik…) connects from its
// own address and names the visitor in headers. Share believes those headers only from the
// addresses in config.json's "proxy", and from there it insists on them: a request that came
// through a proxy must never pass for one from the home network.

// trusted reports whether a request's direct peer is one of the configured proxies.
func (a *App) trusted(p netip.Addr) bool {
	for _, prefix := range a.Cfg.Proxies {
		if prefix.Contains(p) {
			return true
		}
	}
	return false
}

// throughProxy serves a request a trusted proxy passed on: as https when the visitor came over
// https, and from the visitor's own address. It never counts as plain http at home, so the
// home proof and home cookies are never handed out through a proxy.
func (a *App) throughProxy(w http.ResponseWriter, r *http.Request, next http.Handler) {
	visitor, https, ok := a.visitor(r)
	if !ok {
		a.hint("headers", peer(r), "a request from the proxy at %s didn't name the visitor; it must send %s", peer(r), a.proxyHeaderNames())
		httpx.WriteError(w, http.StatusBadRequest, "proxy_headers",
			"The proxy in front of Share didn't say who is visiting. The server's log says more.")
		return
	}
	if !https {
		a.refusePlainHTTP(w, r)
		return
	}
	r = auth.WithClientIP(r, visitor)
	if homenet.Host(hostOf(r.Host)) {
		if homenet.Addr(visitor) {
			// https from the home network to an address at home, as on Share's own https port.
			r = auth.WithHome(r)
		} else {
			a.hint("host", peer(r), "the proxy at %s passes requests from the internet on with Host %q, an address at home; it should keep the visitor's Host (nginx: proxy_set_header Host $host)", peer(r), r.Host)
		}
	}
	next.ServeHTTP(w, r)
}

// visitor reads the visitor's address and scheme from the proxy's headers. ok is false when
// they are missing or make no sense.
func (a *App) visitor(r *http.Request) (visitor netip.Addr, https, ok bool) {
	if a.Cfg.Proxy.Headers == config.ProxyCloudflare {
		ips := r.Header.Values("CF-Connecting-IP")
		if len(ips) != 1 {
			return netip.Addr{}, false, false
		}
		ip, err := netip.ParseAddr(strings.TrimSpace(ips[0]))
		scheme := visitorScheme(r)
		if err != nil || scheme == "" {
			return netip.Addr{}, false, false
		}
		return ip.Unmap().WithZone(""), scheme == "https", true
	}
	visitor, ok = auth.ForwardedFor(r.Header.Values("X-Forwarded-For"), a.Cfg.Proxies)
	protos := headerList(r.Header.Values("X-Forwarded-Proto"))
	if !ok || len(protos) == 0 {
		return netip.Addr{}, false, false
	}
	// The last value is the trusted proxy's own; one the visitor sent may stand before it.
	return visitor, strings.EqualFold(protos[len(protos)-1], "https"), true
}

// visitorScheme is how the visitor reached Cloudflare, from its Cf-Visitor header; "" when
// that header is missing or can't be read.
func visitorScheme(r *http.Request) string {
	var v struct {
		Scheme string `json:"scheme"`
	}
	if json.Unmarshal([]byte(r.Header.Get("Cf-Visitor")), &v) != nil {
		return ""
	}
	return v.Scheme
}

// headerList splits comma-separated header lines into their values.
func headerList(lines []string) []string {
	var values []string
	for _, l := range lines {
		for _, v := range strings.Split(l, ",") {
			if v = strings.TrimSpace(v); v != "" {
				values = append(values, v)
			}
		}
	}
	return values
}

// forwardingHeaders are the headers proxies name the visitor in (canonical form; plus every
// X-Forwarded-… header).
var forwardingHeaders = []string{"Cf-Connecting-Ip", "Cf-Visitor", "X-Real-Ip", "Forwarded"}

// forwarded reports whether a request carries a proxy's headers.
func forwarded(r *http.Request) bool {
	for name := range r.Header {
		if strings.HasPrefix(name, "X-Forwarded-") || slices.Contains(forwardingHeaders, name) {
			return true
		}
	}
	return false
}

// untrustedProxy refuses a request that came through a proxy config.json doesn't name, e.g.
// one set up without "proxy": served as plain http at home, its visitors would all count as
// being at home.
func (a *App) untrustedProxy(w http.ResponseWriter, r *http.Request) {
	p := peer(r)
	a.hint("untrusted", p, `requests from %s came through a proxy, but config.json doesn't name one there. For a proxy on this machine, set "proxy": "cloudflare" (cloudflared) or "proxy": "x-forwarded" (Caddy, nginx, Traefik…); for one elsewhere, "proxy": {"headers": …, "trusted_proxies": [%q]}`, p, p.String())
	httpx.WriteError(w, http.StatusForbidden, "proxy_untrusted",
		"This request came through a proxy that Share doesn't trust. The server's log says more.")
}

// proxyHeaderNames says which headers the configured proxy must send, for the log.
func (a *App) proxyHeaderNames() string {
	if a.Cfg.Proxy.Headers == config.ProxyCloudflare {
		return "CF-Connecting-IP and Cf-Visitor"
	}
	return "X-Forwarded-For and X-Forwarded-Proto"
}

// hint logs how to fix the proxy's setup, at most once an hour for each kind and address, so
// that a wrong setup doesn't flood the log.
func (a *App) hint(kind string, p netip.Addr, format string, args ...any) {
	key := kind + " " + p.String()
	now := a.now()
	a.hintsMu.Lock()
	last, seen := a.hints[key]
	due := !seen || now.Sub(last) >= time.Hour
	if due {
		if a.hints == nil {
			a.hints = make(map[string]time.Time)
		}
		a.hints[key] = now
	}
	a.hintsMu.Unlock()
	if due {
		log.Printf("share: "+format, args...)
	}
}
