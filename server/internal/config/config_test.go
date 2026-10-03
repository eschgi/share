package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const minimal = `{"public_url": "https://share.example.com", "storage_dir": "/mnt/usb/share"}`

func TestDefaults(t *testing.T) {
	cfg, err := Parse([]byte(minimal))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "Share" || cfg.HTTP == nil || cfg.HTTP.Listen != ":8080" || cfg.HTTPS != nil || cfg.Proxy != nil {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
	if cfg.Home != nil || cfg.SelfSigned() {
		t.Errorf("home %v, self-signed %v", cfg.Home, cfg.SelfSigned())
	}
	if cfg.DataDir != "/mnt/usb/share/.share" {
		t.Errorf("DataDir = %q", cfg.DataDir)
	}
	if cfg.ChunkSize() != 20<<20 || cfg.MaxFileSize() != 0 || cfg.MinFreeSpace() != 2048<<20 {
		t.Errorf("upload limits: chunk %d, max %d, free %d", cfg.ChunkSize(), cfg.MaxFileSize(), cfg.MinFreeSpace())
	}
	if len(cfg.Proxies) != 0 || cfg.Location == nil || cfg.Public.String() != "https://share.example.com" {
		t.Errorf("derived values: proxies %v, location %v, public %v", cfg.Proxies, cfg.Location, cfg.Public)
	}
}

func TestPartialNestedObjectKeepsOtherDefaults(t *testing.T) {
	cfg, err := Parse([]byte(`{"public_url": "https://s.example.com", "storage_dir": "/srv/share",
		"upload": {"chunk_size_mib": 50}, "http": {}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Upload.ChunkSizeMiB != 50 || cfg.Upload.MinFreeSpaceMiB != 2048 || cfg.Upload.IncompleteTTLHours != 168 {
		t.Errorf("upload = %+v", cfg.Upload)
	}
	if cfg.HTTP.Listen != ":8080" {
		t.Errorf("an empty http block keeps the default port, got %q", cfg.HTTP.Listen)
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
		{"http public address", `{"public_url": "http://8.8.8.8:8080", "storage_dir": "/s"}`, "must use https"},
		{"public_url with path", `{"public_url": "https://a.example/share", "storage_dir": "/s"}`, "without a path"},
		{"relative storage_dir", `{"public_url": "https://a.example", "storage_dir": "share"}`, "storage_dir: must be an absolute path"},
		{"no port", `{"public_url": "https://a.example", "storage_dir": "/s", "http": null}`, `switch on "http", "https" or both`},
		{"bad http listen", `{"public_url": "https://a.example", "storage_dir": "/s", "http": {"listen": "8080"}}`, "http.listen"},
		{"same port twice", `{"public_url": "https://a.example", "storage_dir": "/s", "https": {"listen": "0.0.0.0:8080"}}`, "same port"},
		{"http home_url without http", `{"public_url": "https://a.example", "storage_dir": "/s", "http": null, "https": {}, "home_url": "http://192.168.8.1:8080"}`, "http port is switched off"},
		{"https home_url without https", `{"public_url": "https://a.example", "storage_dir": "/s", "home_url": "https://192.168.8.1:8443"}`, `switch on "https"`},
		{"http home_url on the internet", `{"public_url": "https://a.example", "storage_dir": "/s", "home_url": "http://share.example.com:8080"}`, "home_url"},
		{"unknown certificate", `{"public_url": "https://a.example", "storage_dir": "/s", "https": {"certificate": "letsencrypt"}}`, "https.certificate"},
		{"half a certificate", `{"public_url": "https://a.example", "storage_dir": "/s", "https": {"certificate": {"cert_file": "/c.pem"}}}`, "both cert_file and key_file"},
		{"unknown proxy", `{"public_url": "https://a.example", "storage_dir": "/s", "proxy": "nginx"}`, `proxy: "nginx" is not known`},
		{"proxy true", `{"public_url": "https://a.example", "storage_dir": "/s", "proxy": true}`, `"x-forwarded"`},
		{"proxy list", `{"public_url": "https://a.example", "storage_dir": "/s", "proxy": ["cloudflare"]}`, `"x-forwarded"`},
		{"proxy without headers", `{"public_url": "https://a.example", "storage_dir": "/s", "proxy": {"trusted_proxies": ["192.168.1.30"]}}`, "proxy.headers"},
		{"unknown proxy field", `{"public_url": "https://a.example", "storage_dir": "/s", "proxy": {"headers": "x-forwarded", "trusted_proxies": ["192.168.1.30"], "header": "X-Real-IP"}}`, "unknown field"},
		{"no proxies", `{"public_url": "https://a.example", "storage_dir": "/s", "proxy": {"headers": "cloudflare", "trusted_proxies": []}}`, "the address the proxy connects from"},
		{"bad proxy", `{"public_url": "https://a.example", "storage_dir": "/s", "proxy": {"headers": "x-forwarded", "trusted_proxies": ["caddy"]}}`, "neither an address nor a CIDR range"},
		{"public proxy", `{"public_url": "https://a.example", "storage_dir": "/s", "proxy": {"headers": "x-forwarded", "trusted_proxies": ["203.0.113.5"]}}`, "isn't on a home network"},
		{"everyone a proxy", `{"public_url": "https://a.example", "storage_dir": "/s", "proxy": {"headers": "x-forwarded", "trusted_proxies": ["0.0.0.0/0"]}}`, "isn't on a home network"},
		{"half a home range", `{"public_url": "https://a.example", "storage_dir": "/s", "proxy": {"headers": "x-forwarded", "trusted_proxies": ["172.0.0.0/8"]}}`, "isn't on a home network"},
		{"old listen", `{"public_url": "https://a.example", "storage_dir": "/s", "listen": "127.0.0.1:8080"}`, `listen: moved into "http"`},
		{"old local", `{"public_url": "https://a.example", "storage_dir": "/s", "local": {"listen": ":8443", "url": "https://192.168.8.1:8443"}}`, `"home_url"`},
		{"old tls", `{"public_url": "https://a.example", "storage_dir": "/s", "tls_cert_file": "/c.pem", "tls_key_file": "/k.pem"}`, "cert_file"},
		{"old cloudflare", `{"public_url": "https://a.example", "storage_dir": "/s", "cloudflare": true}`, `"proxy": "cloudflare"`},
		{"old proxies", `{"public_url": "https://a.example", "storage_dir": "/s", "trusted_proxies": ["10.0.0.2"]}`, `"proxy": {"headers"`},
		{"old header", `{"public_url": "https://a.example", "storage_dir": "/s", "client_ip_header": "X-Real-IP"}`, `"proxy" says which headers`},
		{"bad zone", `{"public_url": "https://a.example", "storage_dir": "/s", "time_zone": "Mars/Base"}`, "time_zone"},
		{"bad language", `{"public_url": "https://a.example", "storage_dir": "/s", "languages": ["en", "fr"]}`, `"fr" is not available`},
		{"default not in languages", `{"public_url": "https://a.example", "storage_dir": "/s", "languages": ["de"]}`, "default_language"},
		{"chunk too big", `{"public_url": "https://a.example", "storage_dir": "/s", "upload": {"chunk_size_mib": 100}}`, "chunk_size_mib"},
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

func TestPlainHTTPAtHome(t *testing.T) {
	for _, public := range []string{"http://localhost:8080", "http://192.168.8.52:8080", "http://share.local:8080"} {
		if _, err := Parse([]byte(`{"public_url": "` + public + `", "storage_dir": "/s"}`)); err != nil {
			t.Errorf("%s: %v", public, err)
		}
	}
	cfg, err := Parse([]byte(`{"public_url": "https://share.example.com", "storage_dir": "/s", "home_url": "http://192.168.8.52:8080/"}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HomeURL != "http://192.168.8.52:8080" || cfg.Home.Scheme != "http" || cfg.SelfSigned() {
		t.Errorf("home %q %v, self-signed %v", cfg.HomeURL, cfg.Home, cfg.SelfSigned())
	}
}

func TestHTTPSPort(t *testing.T) {
	cfg, err := Parse([]byte(`{"public_url": "https://share.example.com", "storage_dir": "/s",
		"https": {}, "home_url": "https://192.168.8.52:8443"}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPS.Listen != ":8443" || !cfg.SelfSigned() || cfg.CertificateHost() != "192.168.8.52" {
		t.Errorf("https %+v, self-signed %v, host %q", cfg.HTTPS, cfg.SelfSigned(), cfg.CertificateHost())
	}
	cfg, err = Parse([]byte(`{"public_url": "https://share.example.com", "storage_dir": "/s", "http": null,
		"https": {"listen": ":443", "certificate": {"cert_file": "/etc/share/cert.pem", "key_file": "/etc/share/key.pem"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP != nil || cfg.SelfSigned() || cfg.HTTPS.Certificate.KeyFile != "/etc/share/key.pem" {
		t.Errorf("http %+v, https %+v", cfg.HTTP, cfg.HTTPS)
	}
	if _, err := Parse([]byte(`{"public_url": "https://share.example.com", "storage_dir": "/s", "https": {"certificate": "self-signed"}}`)); err != nil {
		t.Error(err)
	}
}

func TestProxy(t *testing.T) {
	thisMachine := []string{"127.0.0.1/32", "::1/128"}
	for _, tc := range []struct {
		json, headers string
		proxies       []string
	}{
		{`"cloudflare"`, "cloudflare", thisMachine},
		{`"x-forwarded"`, "x-forwarded", thisMachine},
		{`{"headers": "x-forwarded", "trusted_proxies": ["172.30.0.2", "192.168.1.0/24", "fd00::1:2/112", "::1"]}`, "x-forwarded",
			[]string{"172.30.0.2/32", "192.168.1.0/24", "fd00::1:0/112", "::1/128"}},
		{`{"headers": "cloudflare", "trusted_proxies": ["10.0.0.0/8"]}`, "cloudflare", []string{"10.0.0.0/8"}},
		{`null`, "", nil},
	} {
		cfg, err := Parse([]byte(`{"public_url": "https://a.example", "storage_dir": "/s", "proxy": ` + tc.json + `}`))
		if err != nil {
			t.Fatalf("%s: %v", tc.json, err)
		}
		headers := ""
		if cfg.Proxy != nil {
			headers = cfg.Proxy.Headers
		}
		var proxies []string
		for _, p := range cfg.Proxies {
			proxies = append(proxies, p.String())
		}
		if headers != tc.headers || !slices.Equal(proxies, tc.proxies) {
			t.Errorf("%s: headers %q, proxies %v", tc.json, headers, proxies)
		}
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
	if cfg.StorageDir != "/srv/share" || cfg.App.LinkScheme != "com.eschgi.share" {
		t.Errorf("storage = %q, app = %+v", cfg.StorageDir, cfg.App)
	}
}

// The setups in deploy/ come with examples of config.json, which must work as they are.
func TestDeployExamplesAreValid(t *testing.T) {
	var found int
	err := filepath.WalkDir("../../../deploy", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.Name() != "config.example.json" {
			return err
		}
		found++
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if _, err := Parse(data); err != nil {
			t.Errorf("%s: %v", path, err)
		}
		return nil
	})
	if err != nil || found == 0 {
		t.Fatalf("looking for examples in deploy/: %d found, %v", found, err)
	}
}
