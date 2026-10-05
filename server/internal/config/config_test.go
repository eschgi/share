package config

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const minimal = `{"public_url": "https://share.example.com", "storage_dir": "/mnt/usb/share"}`

// bucket is a valid "s3" object.
const bucket = `{"endpoint": "https://s3.eu-central-1.amazonaws.com", "region": "eu-central-1", "bucket": "share",
	"access_key_id": "AKIAIOSFODNN7EXAMPLE", "secret_access_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"}`

// s3With is a config with the bucket, where the JSON object over replaces some of its fields.
func s3With(over string) string {
	var obj map[string]any
	if err := json.Unmarshal([]byte(bucket), &obj); err != nil {
		panic(err)
	}
	if err := json.Unmarshal([]byte(over), &obj); err != nil {
		panic(err)
	}
	b, _ := json.Marshal(obj)
	return `{"public_url": "https://a.example", "data_dir": "/d", "s3": ` + string(b) + `}`
}

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
		{"no storage", `{"public_url": "https://a.example"}`, `storage_dir: is required, or "s3" for a bucket`},
		{"drive and bucket", `{"public_url": "https://a.example", "storage_dir": "/s", "data_dir": "/d", "s3": ` + bucket + `}`, `"s3" can't both be set`},
		{"bucket without data_dir", `{"public_url": "https://a.example", "s3": ` + bucket + `}`, `data_dir: is required with "s3"`},
		{"unknown bucket field", s3With(`{"bucket": "share", "storage_class": "STANDARD"}`), "unknown field"},
		{"no endpoint", s3With(`{"endpoint": ""}`), "s3.endpoint: is required"},
		{"http endpoint on the internet", s3With(`{"endpoint": "http://s3.example.com"}`), "plain http is only for a bucket at home"},
		{"endpoint with a path", s3With(`{"endpoint": "https://s3.example.com/share"}`), "without a path"},
		{"endpoint with the bucket", s3With(`{"endpoint": "https://Share.s3.example.com"}`), "already names the bucket"},
		{"no region", s3With(`{"region": ""}`), "s3.region: is required"},
		{"bad region", s3With(`{"region": "EU Central"}`), "is not a region name"},
		{"no bucket", s3With(`{"bucket": ""}`), "s3.bucket: is required"},
		{"bad bucket", s3With(`{"bucket": "my bucket"}`), "is not a bucket name"},
		{"bucket like an address", s3With(`{"bucket": "192.168.1.20"}`), "is not a bucket name"},
		{"bad prefix", s3With(`{"prefix": "share/../other"}`), "s3.prefix"},
		{"prefix with spaces", s3With(`{"prefix": "my files"}`), "s3.prefix"},
		{"no key id", s3With(`{"access_key_id": ""}`), "s3.access_key_id: is required"},
		{"no secret", s3With(`{"secret_access_key": ""}`), "s3.secret_access_key: is required"},
		{"pieces too small for a bucket", `{"public_url": "https://a.example", "data_dir": "/d", "s3": ` + bucket + `, "upload": {"chunk_size_mib": 4}}`, "at least 5"},
		{"file too big for a bucket", `{"public_url": "https://a.example", "data_dir": "/d", "s3": ` + bucket + `, "upload": {"max_file_size_gib": 6000}}`, "at most 5120"},
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

func TestS3(t *testing.T) {
	cfg, err := Parse([]byte(`{"public_url": "https://share.example.com", "home_url": "http://192.168.8.52:8080", "data_dir": "/srv/share-data",
		"s3": {"endpoint": "HTTPS://S3.EU-Central-1.Amazonaws.com:443/", "region": "eu-central-1", "bucket": "Family-Files",
		"prefix": "/share", "access_key_id": "AKIAIOSFODNN7EXAMPLE", "secret_access_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.S3.Endpoint != "https://s3.eu-central-1.amazonaws.com" || cfg.S3.Prefix != "share/" || cfg.StorageDir != "" || cfg.DataDir != "/srv/share-data" {
		t.Errorf("endpoint %q, prefix %q, storage %q, data %q", cfg.S3.Endpoint, cfg.S3.Prefix, cfg.StorageDir, cfg.DataDir)
	}
	if cfg.StorageKey() != "s3:Family-Files/share/" || cfg.MaxFileSize() != 5<<40 {
		t.Errorf("key %q, largest file %d", cfg.StorageKey(), cfg.MaxFileSize())
	}
	if got := cfg.Origins(); !slices.Equal(got, []string{"https://share.example.com", "http://192.168.8.52:8080"}) {
		t.Errorf("origins %v", got)
	}
	for _, leak := range []string{fmt.Sprint(*cfg.S3), fmt.Sprintf("%+v", *cfg.S3), fmt.Sprintf("%#v", *cfg.S3), fmt.Sprintf("%+v", *cfg)} {
		if strings.Contains(leak, "wJalrXUtnFEMI") {
			t.Errorf("the secret shows: %s", leak)
		}
	}

	cfg, err = Parse([]byte(`{"public_url": "https://a.example", "data_dir": "/d", "upload": {"max_file_size_gib": 10}, "s3": ` + bucket + `}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxFileSize() != 10<<30 || cfg.S3.Prefix != "" || cfg.StorageKey() != "s3:share/" {
		t.Errorf("largest file %d, prefix %q, key %q", cfg.MaxFileSize(), cfg.S3.Prefix, cfg.StorageKey())
	}
	if disk, _ := Parse([]byte(minimal)); disk.StorageKey() != "disk" {
		t.Errorf("disk key %q", disk.StorageKey())
	}

	// Plain http only for a bucket at home, such as MinIO in the same network.
	for endpoint, want := range map[string]string{
		"http://192.168.1.20:9000":  "http://192.168.1.20:9000",
		"http://[fd12::52]:9000":    "http://[fd12::52]:9000",
		"http://minio.local:80":     "http://minio.local",
		"https://[2001:db8::1]:443": "https://[2001:db8::1]",
	} {
		cfg, err := Parse([]byte(s3With(`{"endpoint": "` + endpoint + `", "path_style": true}`)))
		if err != nil {
			t.Errorf("%s: %v", endpoint, err)
		} else if cfg.S3.Endpoint != want || !cfg.S3.PathStyle {
			t.Errorf("%s: endpoint %q, path style %v", endpoint, cfg.S3.Endpoint, cfg.S3.PathStyle)
		}
	}
}

func TestPostgres(t *testing.T) {
	const secret = "s3cr3t-pass"
	parse := func(url string) (*Config, error) {
		quoted, _ := json.Marshal(url)
		return Parse([]byte(`{"public_url": "https://a.example", "storage_dir": "/s", "database": {"postgres": ` + string(quoted) + `}}`))
	}
	for _, ok := range []string{
		"postgres://share:" + secret + "@ep-x.eu-central-1.aws.neon.tech/share?sslmode=require&channel_binding=require",
		"postgresql://share:" + secret + "@db.example.com:5432/share?sslmode=verify-full",
		"postgres://share@127.0.0.1:5433/share",                // on this machine, no TLS needed
		"postgres://share@nas.home.arpa/share?sslmode=disable", // at home
	} {
		if _, err := parse(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, tc := range []struct{ url, want string }{
		{"", "database.postgres: is required"},
		{"mysql://share:" + secret + "@db.example.com/share", "must be an address like"},
		{"postgres://share:" + secret + "@db.example.com", "names no database"},
		{"postgres://share:" + secret + "@db.example.com/share", "needs sslmode=require"},
		{"postgres://share:" + secret + "@db.example.com/share?sslmode=prefer", "needs sslmode=require"},
	} {
		_, err := parse(tc.url)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: %v, want %q", tc.url, err, tc.want)
		}
		if err != nil && strings.Contains(err.Error(), secret) {
			t.Errorf("%q: the error shows the password: %v", tc.url, err)
		}
	}
	cfg, err := parse("postgres://share:" + secret + "@ep-x.eu-central-1.aws.neon.tech/share?sslmode=require")
	if err != nil {
		t.Fatal(err)
	}
	for _, shown := range []string{cfg.Database.String(), fmt.Sprintf("%v %+v %#v", *cfg.Database, *cfg.Database, *cfg.Database)} {
		if strings.Contains(shown, secret) || !strings.Contains(shown, "ep-x.eu-central-1.aws.neon.tech/share") {
			t.Errorf("printed as %q", shown)
		}
	}
}

// With PostgreSQL and a bucket nothing needs to be kept on this machine.
func TestDataDirIsOptionalWithPostgresAndABucket(t *testing.T) {
	s3 := `"s3": {"endpoint": "https://s3.eu-central-1.amazonaws.com", "region": "eu-central-1", "bucket": "family-share", "access_key_id": "AKID", "secret_access_key": "SECRET"}`
	database := `"database": {"postgres": "postgres://share:pw@db.example.com/share?sslmode=require"}`
	cfg, err := Parse([]byte(`{"public_url": "https://a.example", ` + s3 + `, ` + database + `}`))
	if err != nil || cfg.DataDir != "" {
		t.Fatalf("a bucket and PostgreSQL: %q, %v", cfg.DataDir, err)
	}
	for _, tc := range []struct{ json, want string }{
		{`{"public_url": "https://a.example", ` + s3 + `}`, `unless "database" names PostgreSQL`},
		{`{"public_url": "https://a.example", ` + s3 + `, ` + database + `, "https": {"listen": ":8443"}}`, "own certificate"},
	} {
		if _, err := Parse([]byte(tc.json)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", tc.json, err, tc.want)
		}
	}
}
