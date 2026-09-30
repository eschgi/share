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
	_ "time/tzdata" // OpenWrt ships without zoneinfo; without this every time zone would be UTC
)

// SupportedLanguages are the languages the website and the app are translated into.
var SupportedLanguages = []string{"en", "de", "it"}

// Config is config.json. Field names follow the snake_case of the API.
type Config struct {
	Name            string   `json:"name"`
	PublicURL       string   `json:"public_url"`
	Listen          string   `json:"listen"`
	TrustedProxies  []string `json:"trusted_proxies"`
	ClientIPHeader  string   `json:"client_ip_header"`
	TLSCertFile     string   `json:"tls_cert_file"`
	TLSKeyFile      string   `json:"tls_key_file"`
	Local           Local    `json:"local"`
	StorageDir      string   `json:"storage_dir"`
	DataDir         string   `json:"data_dir"`
	TimeZone        string   `json:"time_zone"`
	Languages       []string `json:"languages"`
	DefaultLanguage string   `json:"default_language"`
	Upload          Upload   `json:"upload"`
	TrashDays       int      `json:"trash_days"`
	App             App      `json:"app"`

	// Filled in by Load from the fields above.
	Location *time.Location `json:"-"`
	Proxies  []netip.Prefix `json:"-"`
	Public   *url.URL       `json:"-"`
}

// Local is the optional second address the Android app prefers when it can reach it.
type Local struct {
	Listen string `json:"listen"`
	URL    string `json:"url"`
}

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
		Name:            "Share",
		Listen:          "127.0.0.1:8080",
		TrustedProxies:  []string{"127.0.0.1/32", "::1/128"},
		ClientIPHeader:  "CF-Connecting-IP",
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
	} else if u, err := parseOrigin(c.PublicURL, true); err != nil {
		bad("public_url", "%v", err)
	} else {
		c.Public = u
		c.PublicURL = u.String()
	}

	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		bad("listen", "must be host:port, e.g. 127.0.0.1:8080")
	}

	c.Proxies = nil
	for _, p := range c.TrustedProxies {
		prefix, err := netip.ParsePrefix(p)
		if err != nil {
			addr, err2 := netip.ParseAddr(p)
			if err2 != nil {
				bad("trusted_proxies", "%q is neither an address nor a CIDR range", p)
				continue
			}
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		c.Proxies = append(c.Proxies, prefix.Masked())
	}

	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		bad("tls_cert_file", "set both tls_cert_file and tls_key_file, or neither")
	}

	if (c.Local.Listen == "") != (c.Local.URL == "") {
		bad("local", "set both local.listen and local.url, or neither")
	} else if c.Local.URL != "" {
		if _, _, err := net.SplitHostPort(c.Local.Listen); err != nil {
			bad("local.listen", "must be host:port, e.g. :8443")
		}
		if u, err := parseOrigin(c.Local.URL, false); err != nil {
			bad("local.url", "%v", err)
		} else if u.Scheme != "https" {
			bad("local.url", "must start with https://")
		} else {
			c.Local.URL = u.String()
		}
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
// fine). Plain http is allowed only for localhost, which is handy during development.
func parseOrigin(raw string, allowLocalHTTP bool) (*url.URL, error) {
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
		host := u.Hostname()
		if !allowLocalHTTP || (host != "localhost" && host != "127.0.0.1" && host != "::1") {
			return nil, fmt.Errorf("%q must use https://", raw)
		}
	default:
		return nil, fmt.Errorf("%q must use https://", raw)
	}
	return &url.URL{Scheme: u.Scheme, Host: strings.ToLower(u.Host)}, nil
}
