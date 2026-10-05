package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/config"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/ids"
	"github.com/eschgi/share/server/internal/s3"
	"github.com/eschgi/share/server/internal/s3/s3test"
	"github.com/eschgi/share/server/internal/storage"
	"github.com/eschgi/share/server/internal/upload"
)

func newS3Env(t *testing.T) *env { return newS3EnvWith(t, "") }

// newS3EnvWith is newEnvWith with the files in a bucket: the test's own, or with
// SHARE_TEST_S3 a real one, on a fresh prefix.
func newS3EnvWith(t *testing.T, settings string) *env {
	t.Helper()
	var setting []byte
	var fake *s3test.Server
	prefix := "share-test-" + randomHex() + "/"
	if live := os.Getenv("SHARE_TEST_S3"); live != "" {
		var c map[string]any
		if err := json.Unmarshal([]byte(live), &c); err != nil {
			t.Fatalf("SHARE_TEST_S3: %v", err)
		}
		base, _ := c["prefix"].(string)
		if base = strings.Trim(base, "/"); base != "" {
			base += "/"
		}
		c["prefix"] = base + prefix
		setting, _ = json.Marshal(c)
	} else {
		fake = s3test.New(t)
		fake.SetMinPartSize(s3TestPart)
		setting, _ = json.Marshal(fake.Config(prefix))
	}
	return startS3Env(t, t.TempDir(), setting, fake, settings)
}

// s3TestPart is the parts' size in the tests: small, so a file of a few parts stays small.
const s3TestPart = 64 << 10

func startS3Env(t *testing.T, dataDir string, setting []byte, fake *s3test.Server, settings string) *env {
	t.Helper()
	if settings != "" {
		settings = ", " + settings
	}
	cfg, err := config.Parse([]byte(fmt.Sprintf(
		`{"public_url": "https://share.example.test", "data_dir": %q, "s3": %s, "time_zone": "Europe/Rome", "http": {"listen": "127.0.0.1:0"}%s}`,
		dataDir, setting, settings)))
	if err != nil {
		t.Fatal(err)
	}
	opts := s3.Options{}
	if fake != nil {
		opts = s3.Options{Transport: fake.Client().Transport, MaxRetries: 1}
	}
	bucket, err := s3.Open(cfg.S3, opts)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, cfg: cfg, clock: &clock{t: time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)}, fake: fake}
	if fake != nil {
		fake.SetClock(e.clock.Now)
	}
	upCfg := upload.DefaultConfig()
	checkStorage := func(context.Context) storage.Report {
		if r := e.report.Load(); r != nil {
			return *r
		}
		return storage.Report{}
	}
	a, err := New(context.Background(), cfg, Options{Now: e.clock.Now, Upload: &upCfg, CheckStorage: checkStorage, S3: bucket})
	if err != nil {
		t.Fatal(err)
	}
	e.app = a
	e.srv = httptest.NewTLSServer(a.Handler)
	t.Cleanup(func() {
		e.srv.Close()
		a.Close()
	})
	return e
}

func randomHex() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// needFake skips a test that needs to look into the bucket or to make it misbehave.
func (e *env) needFake() *s3test.Server {
	e.t.Helper()
	if e.fake == nil {
		e.t.Skip("needs the test's own bucket")
	}
	return e.fake
}

func TestS3ModeStarts(t *testing.T) {
	e := newS3Env(t)
	if e.app.Tus != nil || e.app.CRCs != nil || e.app.S3 == nil || e.app.Lib.S3() == nil {
		t.Fatalf("tus %v, CRCs %v, bucket %v", e.app.Tus, e.app.CRCs, e.app.S3)
	}
	if r := e.do(nil, "GET", "/api/info", "", nil, nil); r.status != http.StatusOK {
		t.Errorf("info: %d %s", r.status, r.body)
	}
	if r := e.do(nil, "POST", "/tus/", "", nil, map[string]string{"Tus-Resumable": "1.0.0"}); r.status != http.StatusNotFound || r.errorCode() != "not_found" {
		t.Errorf("tus in S3 mode: %d %s", r.status, r.body)
	}
	if v, _ := e.app.DB.Meta(context.Background(), "storage"); v != e.cfg.StorageKey() || !strings.HasPrefix(v, "s3:") {
		t.Errorf("storage noted as %q", v)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.app.Serve(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve didn't stop")
	}
}

func TestS3ModeKeepsItsBucket(t *testing.T) {
	fake := s3test.New(t)
	dir := t.TempDir()
	setting, _ := json.Marshal(fake.Config("share/"))
	e := startS3Env(t, dir, setting, fake, "")
	f := db.File{ID: ids.New(), Name: "a.jpg", Size: 1, CreatedAt: e.clock.Now(), UpdatedAt: e.clock.Now(), FolderID: e.firstFolder().ID}
	if err := e.app.DB.InsertReceiving(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	e.srv.Close()
	e.app.Close()

	other, _ := json.Marshal(fake.Config("other/"))
	cfg, err := config.Parse([]byte(fmt.Sprintf(`{"public_url": "https://share.example.test", "data_dir": %q, "s3": %s, "http": {"listen": "127.0.0.1:0"}}`, dir, other)))
	if err != nil {
		t.Fatal(err)
	}
	bucket, _ := s3.Open(cfg.S3, s3.Options{Transport: fake.Client().Transport, MaxRetries: 1})
	if _, err := New(context.Background(), cfg, Options{S3: bucket}); err == nil || !strings.Contains(err.Error(), "new data_dir") {
		t.Errorf("another prefix for the same database: %v", err)
	}
	disk, err := config.Parse([]byte(fmt.Sprintf(`{"public_url": "https://share.example.test", "storage_dir": %q, "data_dir": %q, "http": {"listen": "127.0.0.1:0"}}`, filepath.Join(dir, "files"), dir)))
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Init(storage.Layout{StorageDir: disk.StorageDir, DataDir: disk.DataDir}); err != nil {
		t.Fatal(err)
	}
	if _, err := New(context.Background(), disk, Options{}); err == nil || !strings.Contains(err.Error(), "new data_dir") {
		t.Errorf("a drive for the same database: %v", err)
	}
}
