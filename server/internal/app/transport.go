package app

import (
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/homenet"
	"github.com/eschgi/share/server/internal/httpx"
	"github.com/eschgi/share/server/internal/upload"
)

// guard lets a request in by how it reached Share: through the proxy config.json names, over
// https, or over plain http from a home network or this machine, whose cookies then can't be
// Secure. Plain http from anywhere else is refused, so that no PIN, password, key or file
// crosses the internet unencrypted. The proxy is checked first, on both ports: what it passes
// on must never count as coming from the home network.
func (a *App) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := peer(r)
		switch {
		case a.trusted(p):
			a.throughProxy(w, r, next)
		case homenet.Addr(p) && forwarded(r):
			// A proxy config.json doesn't name, e.g. one set up without "proxy".
			a.untrustedProxy(w, r)
		case r.TLS != nil:
			// https straight from the home network to an address at home, such as the https
			// port at home: like plain http there, a browser's sign-in works only at home.
			if homenet.Addr(p) && homenet.Host(hostOf(r.Host)) {
				r = auth.WithHome(r)
			}
			next.ServeHTTP(w, r)
		case homenet.Addr(p):
			next.ServeHTTP(w, auth.WithPlainHTTP(r))
		default:
			a.refusePlainHTTP(w, r)
		}
	})
}

// hostOf is the host of a Host header, without the port.
func hostOf(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

func peer(r *http.Request) netip.Addr {
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	return ap.Addr().Unmap().WithZone("")
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

// inContainer reports whether Share runs in a Docker or Podman container, whose own addresses
// mean nothing to phones in the network.
func inContainer() bool {
	for _, f := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(f); err == nil {
			return true
		}
	}
	return false
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
