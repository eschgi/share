package config

import (
	"os"
	"strings"
	"testing"
)

const minimal = `{"public_url": "https://share.example.com", "storage_dir": "/mnt/usb/share"}`

func TestDefaults(t *testing.T) {
	cfg, err := Parse([]byte(minimal))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "Share" || cfg.Listen != "127.0.0.1:8080" || cfg.ClientIPHeader != "CF-Connecting-IP" {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
	if cfg.DataDir != "/mnt/usb/share/.share" {
		t.Errorf("DataDir = %q", cfg.DataDir)
	}
	if cfg.ChunkSize() != 20<<20 || cfg.MaxFileSize() != 0 || cfg.MinFreeSpace() != 2048<<20 {
		t.Errorf("upload limits: chunk %d, max %d, free %d", cfg.ChunkSize(), cfg.MaxFileSize(), cfg.MinFreeSpace())
	}
	if len(cfg.Proxies) != 2 || cfg.Location == nil || cfg.Public.String() != "https://share.example.com" {
		t.Errorf("derived values: proxies %v, location %v, public %v", cfg.Proxies, cfg.Location, cfg.Public)
	}
}

func TestPartialNestedObjectKeepsOtherDefaults(t *testing.T) {
	cfg, err := Parse([]byte(`{"public_url": "https://s.example.com", "storage_dir": "/srv/share",
		"upload": {"chunk_size_mib": 50}, "client_ip_header": ""}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Upload.ChunkSizeMiB != 50 || cfg.Upload.MinFreeSpaceMiB != 2048 || cfg.Upload.IncompleteTTLHours != 168 {
		t.Errorf("upload = %+v", cfg.Upload)
	}
	if cfg.ClientIPHeader != "" {
		t.Errorf("an explicit empty client_ip_header must stay empty, got %q", cfg.ClientIPHeader)
	}
}

func TestTimeZone(t *testing.T) {
	cfg, err := Parse([]byte(`{"public_url": "https://s.example.com", "storage_dir": "/srv/share", "time_zone": "Europe/Rome"}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Location.String() != "Europe/Rome" {
		t.Errorf("Location = %v", cfg.Location)
	}
}

func TestPublicURLIsNormalized(t *testing.T) {
	cfg, err := Parse([]byte(`{"public_url": "https://Share.Example.com/", "storage_dir": "/srv/share"}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicURL != "https://share.example.com" {
		t.Errorf("PublicURL = %q", cfg.PublicURL)
	}
}

func TestErrors(t *testing.T) {
	for _, tc := range []struct {
		name, json, want string
	}{
		{"unknown field", `{"public_url": "https://a.example", "storage_dir": "/s", "storagedir": "/x"}`, "unknown field"},
		{"trailing data", minimal + `{}`, "unexpected data"},
		{"missing public_url", `{"storage_dir": "/s"}`, "public_url: is required"},
		{"http public_url", `{"public_url": "http://share.example.com", "storage_dir": "/s"}`, "must use https"},
		{"public_url with path", `{"public_url": "https://a.example/share", "storage_dir": "/s"}`, "without a path"},
		{"relative storage_dir", `{"public_url": "https://a.example", "storage_dir": "share"}`, "storage_dir: must be an absolute path"},
		{"half of local", `{"public_url": "https://a.example", "storage_dir": "/s", "local": {"listen": ":8443"}}`, "both local.listen and local.url"},
		{"http local url", `{"public_url": "https://a.example", "storage_dir": "/s", "local": {"listen": ":8443", "url": "http://192.168.8.1:8443"}}`, "local.url"},
		{"bad proxy", `{"public_url": "https://a.example", "storage_dir": "/s", "trusted_proxies": ["cloudflare"]}`, "trusted_proxies"},
		{"bad zone", `{"public_url": "https://a.example", "storage_dir": "/s", "time_zone": "Mars/Base"}`, "time_zone"},
		{"bad language", `{"public_url": "https://a.example", "storage_dir": "/s", "languages": ["en", "fr"]}`, `"fr" is not available`},
		{"default not in languages", `{"public_url": "https://a.example", "storage_dir": "/s", "languages": ["de"]}`, "default_language"},
		{"chunk too big", `{"public_url": "https://a.example", "storage_dir": "/s", "upload": {"chunk_size_mib": 100}}`, "chunk_size_mib"},
		{"half tls", `{"public_url": "https://a.example", "storage_dir": "/s", "tls_cert_file": "/c.pem"}`, "tls_key_file"},
		{"package without scheme", `{"public_url": "https://a.example", "storage_dir": "/s", "app": {"android_package": "com.example.share", "link_scheme": ""}}`, "app.link_scheme"},
		{"bad package", `{"public_url": "https://a.example", "storage_dir": "/s", "app": {"android_package": "share", "link_scheme": "share"}}`, "android_package"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.json))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse() error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestAllProblemsAreReportedTogether(t *testing.T) {
	_, err := Parse([]byte(`{"storage_dir": "rel", "trash_days": 0}`))
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"public_url", "storage_dir", "trash_days"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

func TestLocalhostHTTPIsAllowedForDevelopment(t *testing.T) {
	if _, err := Parse([]byte(`{"public_url": "http://localhost:8080", "storage_dir": "/s"}`)); err != nil {
		t.Fatal(err)
	}
}

func TestExampleIsValid(t *testing.T) {
	data, err := os.ReadFile("../../../config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("config.example.json: %v", err)
	}
	if cfg.Local.URLHost() != "192.168.8.1" || cfg.App.LinkScheme != "com.eschgi.share" {
		t.Errorf("local = %+v, app = %+v", cfg.Local, cfg.App)
	}
}
