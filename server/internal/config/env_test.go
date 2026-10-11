package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bucketEnv is a server with its files in a bucket, from variables alone.
var bucketEnv = []string{
	"SHARE_PUBLIC_URL=https://share.example.com",
	"SHARE_DATABASE_POSTGRES=postgres://share:pw@db.example.com/share?sslmode=require",
	"SHARE_S3_ENDPOINT=https://s3.eu-central-1.amazonaws.com",
	"SHARE_S3_REGION=eu-central-1",
	"SHARE_S3_BUCKET=share",
	"SHARE_S3_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE",
	"SHARE_S3_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	"HOME=/home/share", // not Share's: left alone
}

// writeConfig writes a config.json for load and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEnvAlone(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "config.json")
	cfg, err := load(missing, bucketEnv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicURL != "https://share.example.com" || cfg.S3 == nil || cfg.S3.Bucket != "share" || cfg.S3.SecretAccessKey != "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" {
		t.Errorf("config from the environment: %+v, %v", cfg, cfg.S3)
	}
	if cfg.HTTP == nil || cfg.HTTP.Listen != ":8080" || cfg.TrashDays != 30 || cfg.DataDir != "" {
		t.Errorf("defaults: http %v, trash %d, data_dir %q", cfg.HTTP, cfg.TrashDays, cfg.DataDir)
	}

	_, err = load(missing, []string{"SHARE_PUBLIC_URL=https://share.example.com"})
	if err == nil || !strings.HasPrefix(err.Error(), "the environment: config:") || !strings.Contains(err.Error(), "database.postgres: is required") {
		t.Errorf("an incomplete environment: %v", err)
	}
	_, err = load(missing, []string{"HOME=/home/share", "PORT=3000"})
	if err == nil || !strings.Contains(err.Error(), "no such file") || !strings.Contains(err.Error(), "SHARE_ environment variables") {
		t.Errorf("neither file nor variables: %v", err)
	}
}

func TestEnvWinsOverFile(t *testing.T) {
	path := writeConfig(t, `{"name": "Home", "public_url": "https://old.example.com", "storage_dir": "/srv/share",
		"database": {"postgres": "postgres://share@localhost/share"}, "upload": {"chunk_size_mib": 10}, "trash_days": 7}`)
	cfg, err := load(path, []string{"SHARE_PUBLIC_URL=https://new.example.com", "SHARE_TRASH_DAYS=60"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicURL != "https://new.example.com" || cfg.TrashDays != 60 {
		t.Errorf("the variables: public %q, trash %d", cfg.PublicURL, cfg.TrashDays)
	}
	if cfg.Name != "Home" || cfg.StorageDir != "/srv/share" || cfg.Upload.ChunkSizeMiB != 10 || cfg.Upload.IncompleteTTLHours != 168 {
		t.Errorf("the file and the defaults: %+v", cfg)
	}

	_, err = load(path, []string{"SHARE_TRASH_DAYS=1000"})
	if err == nil || !strings.HasPrefix(err.Error(), path+" and the environment: config:") || !strings.Contains(err.Error(), "trash_days: must be 1 to 365") {
		t.Errorf("a bad value from a variable: %v", err)
	}
}

func TestEnvValues(t *testing.T) {
	env := append([]string{
		"SHARE_LANGUAGES=de, it",
		"SHARE_DEFAULT_LANGUAGE=it",
		"SHARE_NAME=1234", // a name, not a number
		"SHARE_S3_PATH_STYLE=true",
		"SHARE_S3_PREFIX=family",
		"SHARE_UPLOAD_MAX_FILE_SIZE_GIB=8",
		"SHARE_PROXY=x-forwarded",
		"SHARE_HTTPS_LISTEN=:8443",
		"SHARE_HTTPS_CERTIFICATE=self-signed",
		"SHARE_DATA_DIR=/data",
	}, bucketEnv...)
	cfg, err := load(filepath.Join(t.TempDir(), "none.json"), env)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.Languages, ",") != "de,it" || cfg.DefaultLanguage != "it" || cfg.Name != "1234" {
		t.Errorf("languages %v, default %q, name %q", cfg.Languages, cfg.DefaultLanguage, cfg.Name)
	}
	if !cfg.S3.PathStyle || cfg.S3.Prefix != "family/" || cfg.Upload.MaxFileSizeGiB != 8 {
		t.Errorf("bucket %+v, max %d", cfg.S3, cfg.Upload.MaxFileSizeGiB)
	}
	if cfg.Proxy == nil || cfg.Proxy.Headers != ProxyXForwarded || len(cfg.Proxies) != 2 || !cfg.SelfSigned() {
		t.Errorf("proxy %+v, https %+v", cfg.Proxy, cfg.HTTPS)
	}

	// JSON, as config.json has it.
	cfg, err = load(filepath.Join(t.TempDir(), "none.json"), append([]string{
		`SHARE_PROXY={"headers": "cloudflare", "trusted_proxies": ["192.168.1.30"]}`,
		`SHARE_LANGUAGES=["en"]`,
	}, bucketEnv...))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Proxy.Headers != ProxyCloudflare || cfg.Proxies[0].String() != "192.168.1.30/32" || len(cfg.Languages) != 1 {
		t.Errorf("proxy %+v, languages %v", cfg.Proxy, cfg.Languages)
	}
}

func TestEnvMistakes(t *testing.T) {
	_, err := load(filepath.Join(t.TempDir(), "none.json"), append([]string{
		"SHARE_S3_BUCKT=share",
		"SHARE_TRASH_DAYS=thirty",
		"SHARE_S3_PATH_STYLE=yes please",
		"PORT=http",
		"SHARE_COMMIT=abc1234", // scripts/version.sh's
		"SHARE_TEST_POSTGRES=postgres://share@localhost/share",
	}, bucketEnv...))
	if err == nil {
		t.Fatal("no error")
	}
	for _, want := range []string{
		"SHARE_S3_BUCKT: is not a setting",
		"SHARE_TRASH_DAYS: must be a whole number",
		"SHARE_S3_PATH_STYLE: must be true or false",
		"PORT: must be a port number",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q:\n%v", want, err)
		}
	}
	if strings.Contains(err.Error(), "SHARE_COMMIT") || strings.Contains(err.Error(), "SHARE_TEST_POSTGRES") {
		t.Errorf("variables that aren't settings were reported:\n%v", err)
	}
}

// PORT is the http port when nothing else names one.
func TestPort(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "none.json")
	for _, tc := range []struct {
		file string
		env  []string
		want string // the http port; "" when it is switched off
	}{
		{"", []string{"PORT=3000"}, ":3000"},
		{"", []string{"PORT=3000", "SHARE_HTTP_LISTEN=127.0.0.1:9000"}, "127.0.0.1:9000"},
		{`{"http": {"listen": ":8081"}}`, []string{"PORT=3000"}, ":8081"},
		{`{"http": {}}`, []string{"PORT=3000"}, ":3000"},
		{`{"http": null, "https": {"listen": ":8443"}, "data_dir": "/data"}`, []string{"PORT=3000"}, ""},
	} {
		path := missing
		if tc.file != "" {
			path = writeConfig(t, tc.file)
		}
		cfg, err := load(path, append(tc.env, bucketEnv...))
		if err != nil {
			t.Errorf("%s %v: %v", tc.file, tc.env, err)
			continue
		}
		got := ""
		if cfg.HTTP != nil {
			got = cfg.HTTP.Listen
		}
		if got != tc.want {
			t.Errorf("%s %v: http on %q, want %q", tc.file, tc.env, got, tc.want)
		}
	}
}

// Each setting of config.json has its variable.
func TestEnvSettings(t *testing.T) {
	names := map[string]bool{}
	for _, s := range envSettings {
		names[s.name] = true
	}
	for _, want := range []string{"SHARE_PUBLIC_URL", "SHARE_HTTP_LISTEN", "SHARE_HTTPS_CERTIFICATE", "SHARE_PROXY",
		"SHARE_DATABASE_POSTGRES", "SHARE_S3_SECRET_ACCESS_KEY", "SHARE_UPLOAD_CHUNK_SIZE_MIB", "SHARE_APP_PLAY_STORE_URL"} {
		if !names[want] {
			t.Errorf("no %s", want)
		}
	}
	for _, unwanted := range []string{"SHARE_LOCATION", "SHARE_PROXY_HEADERS", "SHARE_HTTPS_CERTIFICATE_CERT_FILE"} {
		if names[unwanted] {
			t.Errorf("%s shouldn't be a setting", unwanted)
		}
	}
}
