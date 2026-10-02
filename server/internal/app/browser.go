package app

import (
	"mime"
	"net/http"
	"strings"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/httpx"
)

// sameOrigin lets only Share's own pages change state with a browser's cookies. Go's check
// refuses requests that say they come from another site (Sec-Fetch-Site, or else Origin against
// Host) and lets requests through that say nothing, as the app's do. A request carrying a
// session cookie comes from a browser, though, and every browser says where its requests come
// from: so one that says nothing is refused too. And a browser's POST to the API must be JSON,
// even without a body, which no form or other site's page can send without asking first.
func sameOrigin(next http.Handler) http.Handler {
	cop := http.NewCrossOriginProtection()
	refuse := func(w http.ResponseWriter) {
		httpx.WriteError(w, http.StatusForbidden, "cross_origin", "Only Share's own pages can change something here.")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api := strings.HasPrefix(r.URL.Path, "/api/")
		if api {
			// Other pages, even of the same site, can't embed answers such as thumbnails.
			w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		}
		if err := cop.Check(r); err != nil {
			refuse(w)
			return
		}
		if safeMethod(r.Method) || r.Header.Get("Authorization") != "" || !auth.HasSessionCookie(r) {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "" && r.Header.Get("Origin") == "" {
			refuse(w)
			return
		}
		if api && r.Method == http.MethodPost {
			if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
				httpx.WriteError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Send the request body as application/json.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}
