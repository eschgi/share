package main

import (
	"strings"
	"testing"

	"github.com/eschgi/share/server/internal/config"
	"github.com/eschgi/share/server/internal/db"
)

func TestHealthURL(t *testing.T) {
	for _, tc := range []struct{ ports, want string }{
		{``, "http://127.0.0.1:8080/healthz"},
		{`"http": {"listen": "0.0.0.0:9000"}`, "http://127.0.0.1:9000/healthz"},
		{`"http": {"listen": "[::]:9000"}`, "http://[::1]:9000/healthz"},
		{`"http": {"listen": "127.0.0.1:8081"}`, "http://127.0.0.1:8081/healthz"},
		{`"http": {"listen": "192.168.1.20:8080"}`, "http://192.168.1.20:8080/healthz"},
		{`"http": null, "https": {"listen": ":8443"}`, "https://127.0.0.1:8443/healthz"},
		{`"http": {"listen": ":0"}`, "any free port"},
	} {
		settings := tc.ports
		if settings != "" {
			settings = ", " + settings
		}
		cfg, err := config.Parse([]byte(`{"public_url": "https://share.example.com", "storage_dir": "/srv/share"` + settings + `}`))
		if err != nil {
			t.Fatalf("%s: %v", tc.ports, err)
		}
		got, err := healthURL(cfg)
		if err != nil {
			got = err.Error()
		}
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: healthURL = %q, want %q", tc.ports, got, tc.want)
		}
	}
}

func TestProxyLine(t *testing.T) {
	for settings, want := range map[string]string{
		``:                         "Proxy: none",
		`, "proxy": "x-forwarded"`: "Proxy: x-forwarded, from 127.0.0.1/32, ::1/128",
		`, "proxy": {"headers": "cloudflare", "trusted_proxies": ["172.30.0.2"]}`: "Proxy: cloudflare, from 172.30.0.2",
	} {
		cfg, err := config.Parse([]byte(`{"public_url": "https://share.example.com", "storage_dir": "/srv/share"` + settings + `}`))
		if err != nil {
			t.Fatal(err)
		}
		if got := proxyLine(cfg); !strings.HasPrefix(got, want) {
			t.Errorf("%s: %q, want %q", settings, got, want)
		}
	}
}

// The command line has no keys: it refuses what would open folders with encrypted files.
func TestNoEncrypted(t *testing.T) {
	plain := db.Folder{Name: "Family"}
	encrypted := db.Folder{Name: "Taxes", KeyVersion: 1}
	if err := noEncrypted([]db.Folder{plain}, "invite"); err != nil {
		t.Errorf("a plain folder: %v", err)
	}
	if err := noEncrypted([]db.Folder{plain, encrypted}, "invite"); err == nil || !strings.Contains(err.Error(), `"Taxes"`) {
		t.Errorf("an encrypted folder: %v", err)
	}
}
