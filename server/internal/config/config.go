// Package config loads and checks config.json.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // routers often ship without zoneinfo; without this every time zone would be UTC

	"github.com/eschgi/share/server/internal/homenet"
)

// SupportedLanguages are the languages the website and the app are translated into.
var SupportedLanguages = []string{"en", "de", "it"}

// Config is config.json. Field names follow the snake_case of the API.
type Config struct {
	Name            string     `json:"name"`
	PublicURL       string     `json:"public_url"`
	HomeURL         string     `json:"home_url"`
	HTTP            *HTTP      `json:"http"`
	HTTPS           *HTTPS     `json:"https"`
	Cloudflare      Cloudflare `json:"cloudflare"`
	StorageDir      string     `json:"storage_dir"`
	DataDir         string     `json:"data_dir"`
	TimeZone        string     `json:"time_zone"`
	Languages       []string   `json:"languages"`
	DefaultLanguage string     `json:"default_language"`
	Upload          Upload     `json:"upload"`
	TrashDays       int        `json:"trash_days"`
	App             App        `json:"app"`

	// Filled in by Load from the fields above.
	Location *time.Location `json:"-"`
	Proxies  []netip.Prefix `json:"-"`
	Public   *url.URL       `json:"-"`
	Home     *url.URL       `json:"-"` // nil without home_url
}

// HTTP is the plain-http port: the website, the API and uploads. Cloudflare's tunnel comes in
// here; browsers and the app may use it directly only from a home network or this machine.
// "http": null switches it off.
type HTTP struct {
	Listen string `json:"listen"`
}

// HTTPS is the encrypted port, with the same website, API and uploads.
type HTTPS struct {
	Listen      string      `json:"listen"`
	Certificate Certificate `json:"certificate"`
}

// Certificate is "self-signed", which Share makes itself and the app pins, or a certificate
// from files: {"cert_file": "…", "key_file": "…"}. Self-signed when left out.
type Certificate struct {
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
}

// SelfSigned reports whether Share makes the certificate itself.
func (c Certificate) SelfSigned() bool { return c.CertFile == "" }

func (c *Certificate) UnmarshalJSON(b []byte) error {
	var name string
	if json.Unmarshal(b, &name) == nil {
		if name != "self-signed" {
			return fmt.Errorf(`https.certificate: %q is not known; use "self-signed" or {"cert_file": …, "key_file": …}`, name)
		}
		*c = Certificate{}
		return nil
	}
	type files Certificate // without this method
	var f files
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return fmt.Errorf(`https.certificate: %w; use "self-signed" or {"cert_file": …, "key_file": …}`, err)
	}
	if f.CertFile == "" || f.KeyFile == "" {
		return errors.New("https.certificate: set both cert_file and key_file")
	}
	*c = Certificate(f)
	return nil
}

// Cloudflare says whether requests come through a Cloudflare Tunnel: true when cloudflared
// runs on this machine, {"trusted_proxies": […]} when it runs elsewhere, false without one.
// Requests from those addresses carry the visitor's address in CF-Connecting-IP.
type Cloudflare struct {
	Enabled        bool
	TrustedProxies []string
}

func (c *Cloudflare) UnmarshalJSON(b []byte) error {
	var on bool
	if json.Unmarshal(b, &on) == nil {
		*c = Cloudflare{Enabled: on}
		if on {
			c.TrustedProxies = slices.Clone(thisMachine)
		}
		return nil
	}
	var v struct {
		TrustedProxies []string `json:"trusted_proxies"`
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return fmt.Errorf(`cloudflare: %w; use true, false or {"trusted_proxies": […]}`, err)
	}
	*c = Cloudflare{Enabled: true, TrustedProxies: v.TrustedProxies}
	return nil
}

// thisMachine is where cloudflared connects from when it runs next to Share.
var thisMachine = []string{"127.0.0.1/32", "::1/128"}

// CloudflareHeader carries the visitor's address in requests from Cloudflare's tunnel.
const CloudflareHeader = "CF-Connecting-IP"

// ClientIPHeader is the header with the visitor's address from trusted proxies, or "" without.
func (c *Config) ClientIPHeader() string {
	if c.Cloudflare.Enabled {
		return CloudflareHeader
	}
	return ""
}

// CertificateHost is the name or address Share's own certificate is made for.
func (c *Config) CertificateHost() string {
	if c.Home != nil && c.Home.Scheme == "https" {
		return c.Home.Hostname()
	}
	return "localhost"
}

// SelfSigned reports whether there is an https port with Share's own certificate, which the
// app pins on the home address.
func (c *Config) SelfSigned() bool { return c.HTTPS != nil && c.HTTPS.Certificate.SelfSigned() }

// Upload holds the limits for tus uploads.
type Upload struct {
	ChunkSizeMiB       int   `json:"chunk_size_mib"`
	MaxFileSizeGiB     int64 `json:"max_file_size_gib"`
	MinFreeSpaceMiB    int64 `json:"min_free_space_mib"`
	IncompleteTTLHours int   `json:"incomplete_ttl_hours"`
}

// App describes the Android app offered on the invite page.
type App struct {
	APKFile        string `json:"apk_file"`
	PlayStoreURL   string `json:"play_store_url"`
	AndroidPackage string `json:"android_package"`
	LinkScheme     string `json:"link_scheme"`
}

// Default returns the configuration used for every field config.json leaves out.
func Default() Config {
	return Config{
		Name: "Share",
		// On every interface, so phones at home reach it; plain http from outside a home
		// network is refused there anyway.
		HTTP:            &HTTP{Listen: ":8080"},
		Cloudflare:      Cloudflare{Enabled: true, TrustedProxies: slices.Clone(thisMachine)},
		Languages:       slices.Clone(SupportedLanguages),
		DefaultLanguage: "en",
		Upload: Upload{
			// Well below Cloudflare's 100 MB request limit, and small enough that a slow
			// uplink still finishes a chunk inside Cloudflare's 125 s response timeout.
			ChunkSizeMiB:       20,
			MinFreeSpaceMiB:    2048,
			IncompleteTTLHours: 168,
		},
		TrashDays: 30,
		// The app from this repository; a fork with its own app changes both.
		App: App{AndroidPackage: "com.eschgi.share", LinkScheme: "com.eschgi.share"},
	}
}

// ChunkSize is the tus chunk size in bytes that clients are told to use.
func (c *Config) ChunkSize() int64 { return int64(c.Upload.ChunkSizeMiB) << 20 }

// MaxFileSize is the largest accepted upload in bytes; 0 means no limit.
func (c *Config) MaxFileSize() int64 { return c.Upload.MaxFileSizeGiB << 30 }

// MinFreeSpace is the space uploads must leave free on the storage drive, in bytes.
func (c *Config) MinFreeSpace() int64 { return c.Upload.MinFreeSpaceMiB << 20 }

// IncompleteTTL is how long an unfinished upload may sit idle before it is dropped.
func (c *Config) IncompleteTTL() time.Duration {
	return time.Duration(c.Upload.IncompleteTTLHours) * time.Hour
}

// Load reads, completes and checks the configuration at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse decodes config.json content on top of Default and checks it. Unknown fields are
// errors, so a typo never silently falls back to a default.
func Parse(data []byte) (*Config, error) {
	if moved := movedSettings(data); len(moved) > 0 {
		return nil, errors.New("config:\n  " + strings.Join(moved, "\n  "))
	}
	cfg := Default()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("config: unexpected data after the JSON object")
	}
	if err := cfg.complete(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// movedSettings explains the settings of earlier versions instead of calling them unknown.
func movedSettings(data []byte) []string {
	var top map[string]json.RawMessage
	if json.Unmarshal(data, &top) != nil {
		return nil // the decoder reports what is wrong with it
	}
	hints := []struct{ key, hint string }{
		{"listen", `moved into "http": {"listen": "…"}`},
		{"tls_cert_file", `moved: "https": {"listen": ":443", "certificate": {"cert_file": "…", "key_file": "…"}}`},
		{"tls_key_file", `moved: "https": {"listen": ":443", "certificate": {"cert_file": "…", "key_file": "…"}}`},
		{"local", `replaced: "https": {"listen": "…"} for the port, and "home_url" for the address the app uses at home`},
		{"trusted_proxies", `moved: "cloudflare": {"trusted_proxies": […]}`},
		{"client_ip_header", `is gone: behind Cloudflare the visitor's address is in CF-Connecting-IP`},
	}
	var moved []string
	for _, h := range hints {
		if _, ok := top[h.key]; ok {
			moved = append(moved, h.key+": "+h.hint)
		}
	}
	return moved
}

var (
	packagePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*(\.[a-zA-Z][a-zA-Z0-9_]*)+$`)
	schemePattern  = regexp.MustCompile(`^[a-z][a-z0-9+.-]*$`)
)

// complete fills in derived values and returns every problem at once, so fixing
// config.json doesn't take one restart per mistake.
func (c *Config) complete() error {
	var problems []string
	bad := func(field, format string, args ...any) {
		problems = append(problems, field+": "+fmt.Sprintf(format, args...))
	}

	if n := len([]rune(c.Name)); n < 1 || n > 40 {
		bad("name", "must be 1 to 40 characters")
	}

	if c.PublicURL == "" {
		bad("public_url", "is required, e.g. https://share.example.com")
	} else if u, err := parseOrigin(c.PublicURL); err != nil {
		bad("public_url", "%v", err)
	} else {
		c.Public = u
		c.PublicURL = u.String()
	}

	if c.HTTP == nil && c.HTTPS == nil {
		bad("http", `switch on "http", "https" or both; without a port nobody can reach Share`)
	}
	if c.HTTP != nil {
		if c.HTTP.Listen == "" {
			c.HTTP.Listen = ":8080"
		}
		if _, _, err := net.SplitHostPort(c.HTTP.Listen); err != nil {
			bad("http.listen", "must be host:port or :port, e.g. :8080")
		}
	}
	if c.HTTPS != nil {
		if c.HTTPS.Listen == "" {
			c.HTTPS.Listen = ":8443"
		}
		if _, _, err := net.SplitHostPort(c.HTTPS.Listen); err != nil {
			bad("https.listen", "must be host:port or :port, e.g. :8443")
		} else if c.HTTP != nil && samePort(c.HTTP.Listen, c.HTTPS.Listen) {
			bad("https.listen", "is the same port as http.listen")
		}
	}

	if c.HomeURL != "" {
		if u, err := parseOrigin(c.HomeURL); err != nil {
			bad("home_url", "%v", err)
		} else if u.Scheme == "http" && c.HTTP == nil {
			bad("home_url", "is an http address, but the http port is switched off")
		} else if u.Scheme == "https" && c.HTTPS == nil {
			bad("home_url", `is an https address: switch on "https" for it`)
		} else {
			c.Home = u
			c.HomeURL = u.String()
		}
	}

	c.Proxies = nil
	if c.Cloudflare.Enabled && len(c.Cloudflare.TrustedProxies) == 0 {
		bad("cloudflare.trusted_proxies", "needs the address cloudflared connects from, e.g. 192.168.8.20")
	}
	for _, p := range c.Cloudflare.TrustedProxies {
		prefix, err := netip.ParsePrefix(p)
		if err != nil {
			addr, err2 := netip.ParseAddr(p)
			if err2 != nil {
				bad("cloudflare.trusted_proxies", "%q is neither an address nor a CIDR range", p)
				continue
			}
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		c.Proxies = append(c.Proxies, prefix.Masked())
	}

	if c.StorageDir == "" {
		bad("storage_dir", "is required")
	} else if !filepath.IsAbs(c.StorageDir) {
		bad("storage_dir", "must be an absolute path")
	} else {
		c.StorageDir = filepath.Clean(c.StorageDir)
	}
	if c.DataDir == "" && c.StorageDir != "" {
		c.DataDir = filepath.Join(c.StorageDir, ".share")
	} else if c.DataDir != "" && !filepath.IsAbs(c.DataDir) {
		bad("data_dir", "must be an absolute path")
	} else {
		c.DataDir = filepath.Clean(c.DataDir)
	}

	if c.TimeZone == "" {
		c.Location = time.Local
	} else if loc, err := time.LoadLocation(c.TimeZone); err != nil {
		bad("time_zone", "%q is not a known time zone, e.g. Europe/Rome", c.TimeZone)
	} else {
		c.Location = loc
	}

	if len(c.Languages) == 0 {
		bad("languages", "needs at least one language")
	}
	for _, l := range c.Languages {
		if !slices.Contains(SupportedLanguages, l) {
			bad("languages", "%q is not available; choose from %s", l, strings.Join(SupportedLanguages, ", "))
		}
	}
	if !slices.Contains(c.Languages, c.DefaultLanguage) {
		bad("default_language", "must be one of languages")
	}

	u := c.Upload
	if u.ChunkSizeMiB < 1 || u.ChunkSizeMiB > 90 {
		bad("upload.chunk_size_mib", "must be 1 to 90 (Cloudflare rejects requests over 100 MB)")
	}
	if u.MaxFileSizeGiB < 0 {
		bad("upload.max_file_size_gib", "must be 0 (no limit) or more")
	}
	if u.MinFreeSpaceMiB < 0 {
		bad("upload.min_free_space_mib", "must be 0 or more")
	}
	if u.IncompleteTTLHours < 1 || u.IncompleteTTLHours > 720 {
		bad("upload.incomplete_ttl_hours", "must be 1 to 720")
	}
	if c.TrashDays < 1 || c.TrashDays > 365 {
		bad("trash_days", "must be 1 to 365")
	}

	a := c.App
	if a.PlayStoreURL != "" && !strings.HasPrefix(a.PlayStoreURL, "https://") {
		bad("app.play_store_url", "must start with https://")
	}
	if (a.AndroidPackage == "") != (a.LinkScheme == "") {
		bad("app", "set both app.android_package and app.link_scheme, or neither")
	}
	if a.AndroidPackage != "" && !packagePattern.MatchString(a.AndroidPackage) {
		bad("app.android_package", "%q is not a valid package name", a.AndroidPackage)
	}
	if a.LinkScheme != "" && !schemePattern.MatchString(a.LinkScheme) {
		bad("app.link_scheme", "%q is not a valid URL scheme", a.LinkScheme)
	}
	if a.APKFile != "" && !filepath.IsAbs(a.APKFile) {
		bad("app.apk_file", "must be an absolute path")
	}

	if len(problems) > 0 {
		return errors.New("config:\n  " + strings.Join(problems, "\n  "))
	}
	return nil
}

// parseOrigin accepts an origin such as https://share.example.com (a trailing slash is
// fine). Plain http only for an address on a home network or this machine (homenet.Host).
func parseOrigin(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("%q is not a URL like https://share.example.com", raw)
	}
	if u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, fmt.Errorf("%q must be just scheme and host, without a path", raw)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !homenet.Host(u.Hostname()) {
			return nil, fmt.Errorf("%q must use https://: plain http is only for addresses at home, such as http://192.168.1.20:8080 or http://share.local:8080", raw)
		}
	default:
		return nil, fmt.Errorf("%q must use https://", raw)
	}
	return &url.URL{Scheme: u.Scheme, Host: strings.ToLower(u.Host)}, nil
}

// samePort reports whether two listen addresses use the same port. Port 0, any free one,
// never clashes.
func samePort(a, b string) bool {
	_, pa, errA := net.SplitHostPort(a)
	_, pb, errB := net.SplitHostPort(b)
	return errA == nil && errB == nil && pa == pb && pa != "0"
}
