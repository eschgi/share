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
	"regexp"
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
	// Small systems, containers and Windows rarely have /etc/mime.types, so don't rely on it.
	for ext, typ := range map[string]string{
		".woff2": "font/woff2", ".woff": "font/woff", ".webmanifest": "application/manifest+json",
		".ico": "image/x-icon", ".svg": "image/svg+xml", ".js": "text/javascript; charset=utf-8",
	} {
		mime.AddExtensionType(ext, typ)
	}
}

// contentSecurityPolicy allows only this origin, and a bucket's for what pages send and fetch
// there. Uploads, previews (blob:) and the service worker otherwise all stay on this origin.
func contentSecurityPolicy(bucket string) string {
	if bucket != "" {
		bucket = " " + bucket
	}
	return "default-src 'self'; img-src 'self' blob: data:" + bucket + "; media-src 'self' blob:" + bucket + "; " +
		"worker-src 'self'; connect-src 'self'" + bucket + "; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'"
}

// UI serves the website.
type UI struct {
	files    fs.FS
	built    bool
	manifest []byte
	csp      string
}

// New prepares the website for cfg. bucket is the origin of the bucket's links, where pages
// send and fetch the files; "" when they are on a drive.
func New(cfg *config.Config, bucket string) *UI {
	files, _ := fs.Sub(dist, "dist")
	_, err := fs.Stat(files, "index.html")
	u := &UI{files: files, built: err == nil, csp: contentSecurityPolicy(bucket)}
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
		// Installed on Android, the website is in the share sheet. The service worker takes
		// the files (web/src/sw.ts); they never come here.
		"share_target": map[string]any{
			"action":  ShareTarget,
			"method":  "POST",
			"enctype": "multipart/form-data",
			"params":  map[string]any{"files": []map[string]any{{"name": "files", "accept": []string{"*/*"}}}},
		},
	})
	return u
}

// ShareTarget is where the share sheet posts files (contract/web_routes.json). The service
// worker answers it; when there is none, such as right after installing, the server sends the
// browser to the Send page, which offers to pick the files instead, without reading them.
const (
	ShareTarget       = "/share-target"
	ShareTargetFailed = "/send?share=failed"
)

// Built reports whether the website was embedded (npm run build ran before go build).
func (u *UI) Built() bool { return u.built }

func (u *UI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == ShareTarget {
		u.setPageHeaders(w)
		http.Redirect(w, r, ShareTargetFailed, http.StatusSeeOther)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p := r.URL.Path
	switch {
	case p == "/" || isScreen(p):
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
	case p == "/theme-boot.js":
		// Every page loads it before it is drawn; its name has no hash, so it is always checked.
		w.Header().Set("Cache-Control", "no-cache")
		u.file(w, r, "theme-boot.js")
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

// screens are the website's paths besides "/", which its single page routes itself
// (contract/web_routes.json). All but sign-in may have up to three more segments, such as
// /settings/people/u7ld…; anything else stays 404.
var screens = []string{"/sign-in", "/library", "/send", "/settings"}

var screenSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func isScreen(p string) bool {
	for _, s := range screens {
		if p == s {
			return true
		}
		rest, ok := strings.CutPrefix(p, s+"/")
		if !ok || s == "/sign-in" {
			continue
		}
		parts := strings.Split(rest, "/")
		if len(parts) > 3 {
			return false
		}
		for _, part := range parts {
			if !screenSegment.MatchString(part) {
				return false
			}
		}
		return true
	}
	return false
}

// page serves an HTML page with the security headers, and gives the browser its random id
// for the wrong-PIN limit.
func (u *UI) page(w http.ResponseWriter, r *http.Request, name string) {
	u.setPageHeaders(w)
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
	u.setPageHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte(`<!doctype html><meta charset="utf-8"><title>Not found</title><p>Not found.</p>`))
}

func (u *UI) setPageHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Security-Policy", u.csp)
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
