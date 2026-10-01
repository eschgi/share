package app

import (
	"encoding/json"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/config"
	"github.com/eschgi/share/server/internal/homenet"
	"github.com/eschgi/share/server/internal/httpx"
	"github.com/eschgi/share/server/internal/upload"
)

// guard lets a request in by how it reached Share: over https, through Cloudflare's tunnel
// (https from the visitor to Cloudflare), or over plain http from a home network or this
// machine, whose cookies then can't be Secure. Plain http from anywhere else is refused, so
// that no PIN, password, key or file crosses the internet unencrypted.
func (a *App) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.TLS != nil:
			next.ServeHTTP(w, r)
		case a.fromTunnel(r):
			if visitorScheme(r) == "http" {
				a.refusePlainHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		case homenet.Addr(peer(r)):
			next.ServeHTTP(w, auth.WithPlainHTTP(r))
		default:
			a.refusePlainHTTP(w, r)
		}
	})
}

// fromTunnel reports whether cloudflared passed the request on: it comes from a trusted proxy
// and names the visitor.
func (a *App) fromTunnel(r *http.Request) bool {
	if !a.Cfg.Cloudflare.Enabled || r.Header.Get(config.CloudflareHeader) == "" {
		return false
	}
	p := peer(r)
	for _, prefix := range a.Cfg.Proxies {
		if prefix.Contains(p) {
			return true
		}
	}
	return false
}

// visitorScheme is how the visitor reached Cloudflare, from its Cf-Visitor header.
func visitorScheme(r *http.Request) string {
	var v struct {
		Scheme string `json:"scheme"`
	}
	json.Unmarshal([]byte(r.Header.Get("Cf-Visitor")), &v)
	return v.Scheme
}

func peer(r *http.Request) netip.Addr {
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	return ap.Addr().Unmap()
}

// refusePlainHTTP answers plain http from outside a home network. A page goes on to the public
// address when that is https; the API and uploads get an error.
func (a *App) refusePlainHTTP(w http.ResponseWriter, r *http.Request) {
	page := (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
		!strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, upload.BasePath)
	if page && a.Cfg.Public.Scheme == "https" {
		http.Redirect(w, r, a.Cfg.PublicURL+r.URL.RequestURI(), http.StatusFound)
		return
	}
	httpx.WriteError(w, http.StatusForbidden, "https_required",
		"Plain http only works on a home network. Open "+a.Cfg.PublicURL+" instead.")
}

// homeURLs are the addresses at home of an http port that listens on every interface: what
// to open on a phone in the same network. For the log.
func homeURLs(listen string) []string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil || (host != "" && host != "0.0.0.0" && host != "::") {
		return nil
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var urls []string
	for _, ad := range addrs {
		ipnet, ok := ad.(*net.IPNet)
		if !ok {
			continue
		}
		if ip, ok := netip.AddrFromSlice(ipnet.IP); ok && ip.Unmap().Is4() && ip.Unmap().IsPrivate() {
			urls = append(urls, "http://"+net.JoinHostPort(ip.Unmap().String(), port))
		}
	}
	return urls
}
