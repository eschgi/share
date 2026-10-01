package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/eschgi/share/server/internal/httpx"
)

// AppInfo describes the Android app, for the invite page and for updates in the app.
type AppInfo struct {
	AndroidPackage string   `json:"android_package"`
	LinkScheme     string   `json:"link_scheme"`
	APK            *APKInfo `json:"apk"`            // null when this server offers none
	PlayStoreURL   *string  `json:"play_store_url"` // null until the app is on Google Play
}

// APKInfo is the APK this server hands out at /download/share.apk.
type APKInfo struct {
	VersionCode int    `json:"version_code"`
	VersionName string `json:"version_name"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
}

// APK is the app file from app.apk_file. Its size and hash are worked out once and again
// whenever the file changes; the version comes from <apk_file>.json next to it, which CI
// makes along with the APK (app/README.md).
type APK struct {
	Path string

	mu      sync.Mutex
	modTime time.Time
	info    *APKInfo
}

// Info returns the APK's description, or nil if there is no APK file.
func (a *APK) Info() *APKInfo {
	if a == nil || a.Path == "" {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	st, err := os.Stat(a.Path)
	if err != nil {
		a.info = nil
		return nil
	}
	if a.info != nil && st.ModTime().Equal(a.modTime) {
		return a.info
	}
	f, err := os.Open(a.Path)
	if err != nil {
		log.Printf("api: app: %v", err)
		return nil
	}
	defer f.Close()
	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		log.Printf("api: app: %v", err)
		return nil
	}
	info := &APKInfo{Size: size, SHA256: hex.EncodeToString(h.Sum(nil))}
	if b, err := os.ReadFile(a.Path + ".json"); err == nil {
		var v struct {
			VersionCode int    `json:"version_code"`
			VersionName string `json:"version_name"`
		}
		if err := json.Unmarshal(b, &v); err != nil {
			log.Printf("api: app: %s.json: %v", a.Path, err)
		}
		info.VersionCode, info.VersionName = v.VersionCode, v.VersionName
	}
	a.info, a.modTime = info, st.ModTime()
	return info
}

func (a *API) appInfo(w http.ResponseWriter, r *http.Request) {
	info := AppInfo{AndroidPackage: a.Cfg.App.AndroidPackage, LinkScheme: a.Cfg.App.LinkScheme, APK: a.APK.Info()}
	if a.Cfg.App.PlayStoreURL != "" {
		info.PlayStoreURL = &a.Cfg.App.PlayStoreURL
	}
	httpx.WriteJSON(w, http.StatusOK, info)
}

func (a *API) downloadAPK(w http.ResponseWriter, r *http.Request) {
	if a.APK.Info() == nil {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(a.APK.Path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		internal(w, "app download", err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/vnd.android.package-archive")
	h.Set("Content-Disposition", `attachment; filename="share.apk"`)
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(newDeadlineWriter(w), r, "", st.ModTime(), f)
}
