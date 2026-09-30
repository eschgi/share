// Package webui serves the website: the upload pages built from web/ into dist/, the PWA
// manifest, and the security headers every page gets.
package webui

import (
	"bytes"
	"embed"
	"encoding/json"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/config"
)

// dist is filled by `npm run build` in web/. It may hold nothing but .gitkeep, so `go build`
// works without npm; the server then shows a note instead of the website.
//
//go:embed all:dist
var dist embed.FS

func init() {
	// Routers rarely have /etc/mime.types, so don't rely on it.
	for ext, typ := range map[string]string{
		".woff2": "font/woff2", ".woff": "font/woff", ".webmanifest": "application/manifest+json",
		".ico": "image/x-icon", ".svg": "image/svg+xml", ".js": "text/javascript; charset=utf-8",
	} {
		mime.AddExtensionType(ext, typ)
	}
}

// CSP allows only this origin. Uploads, previews (blob:) and the service worker all stay on it.
const contentSecurityPolicy = "default-src 'self'; img-src 'self' blob: data:; media-src 'self' blob:; " +
	"worker-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'"

// UI serves the website.
type UI struct {
	files    fs.FS
	built    bool
	manifest []byte
}

// New prepares the website for cfg.
func New(cfg *config.Config) *UI {
	files, _ := fs.Sub(dist, "dist")
	_, err := fs.Stat(files, "index.html")
	u := &UI{files: files, built: err == nil}
	u.manifest, _ = json.Marshal(map[string]any{
		"id":               "/",
		"name":             cfg.Name,
		"short_name":       cfg.Name,
		"start_url":        "/",
		"scope":            "/",
		"display":          "standalone",
		"background_color": "#16120F",
		"theme_color":      "#16120F",
		"icons": []map[string]string{
			{"src": "/icons/icon-192.png", "sizes": "192x192", "type": "image/png"},
			{"src": "/icons/icon-512.png", "sizes": "512x512", "type": "image/png"},
			{"src": "/icons/maskable-512.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable"},
		},
	})
	return u
}

// Built reports whether the website was embedded (npm run build ran before go build).
func (u *UI) Built() bool { return u.built }

func (u *UI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p := r.URL.Path
	switch {
	case p == "/":
		u.page(w, r, "index.html")
	case p == "/join":
		u.page(w, r, "join.html")
	case p == "/manifest.webmanifest":
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, "manifest.webmanifest", time.Time{}, bytes.NewReader(u.manifest))
	case p == "/sw.js":
		// The service worker must always be fresh, and its scope is the whole site.
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Service-Worker-Allowed", "/")
		u.file(w, r, "sw.js")
	case strings.HasPrefix(p, "/assets/"):
		// Vite puts a content hash in every asset name, so these never change.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		u.file(w, r, p[1:])
	case strings.HasPrefix(p, "/icons/") || strings.HasPrefix(p, "/fonts/") || p == "/favicon.ico":
		w.Header().Set("Cache-Control", "public, max-age=86400")
		u.file(w, r, p[1:])
	default:
		u.notFound(w)
	}
}

// page serves an HTML page with the security headers, and gives the browser its random id
// for the wrong-PIN limit.
func (u *UI) page(w http.ResponseWriter, r *http.Request, name string) {
	setPageHeaders(w)
	if !u.built {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(placeholder))
		return
	}
	data, err := fs.ReadFile(u.files, name)
	if err != nil {
		u.notFound(w)
		return
	}
	auth.EnsureClientCookie(w, r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

func (u *UI) file(w http.ResponseWriter, r *http.Request, name string) {
	name = path.Clean(name)
	if !fs.ValidPath(name) || strings.HasSuffix(name, ".html") {
		u.notFound(w)
		return
	}
	data, err := fs.ReadFile(u.files, name)
	if err != nil {
		u.notFound(w)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

func (u *UI) notFound(w http.ResponseWriter) {
	setPageHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte(`<!doctype html><meta charset="utf-8"><title>Not found</title><p>Not found.</p>`))
}

func setPageHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Security-Policy", contentSecurityPolicy)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
	h.Set("Strict-Transport-Security", "max-age=31536000")
	h.Set("Cache-Control", "no-cache")
}

const placeholder = `<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Share</title>
<h1>Share is running</h1>
<p>The website wasn't built into this binary. Run <code>npm ci &amp;&amp; npm run build</code> in <code>web/</code>, then build the server again.</p>
</html>`
