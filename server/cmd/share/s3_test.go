package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eschgi/share/server/internal/config"
	"github.com/eschgi/share/server/internal/db/pgtest"
	"github.com/eschgi/share/server/internal/s3"
	"github.com/eschgi/share/server/internal/s3/s3test"
)

// output runs a command and returns what it printed.
func output(t *testing.T, args ...string) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	runErr := run(args)
	w.Close()
	os.Stdout = stdout
	out, _ := io.ReadAll(r)
	return string(out), runErr
}

func TestInitAndCheckABucket(t *testing.T) {
	fake := s3test.New(t)
	saved := openBucket
	openBucket = func(c *config.S3) (*s3.Bucket, error) {
		return s3.Open(c, s3.Options{Transport: fake.Client().Transport, MaxRetries: 1})
	}
	t.Cleanup(func() { openBucket = saved })
	dir := t.TempDir()
	setting, _ := json.Marshal(fake.Config("share/"))
	path := filepath.Join(dir, "config.json")
	cfg := fmt.Sprintf(`{"public_url": "https://share.example.com", "data_dir": %q, "s3": %s}`, filepath.Join(dir, "data"), setting)
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	out, _ := output(t, "init", "--config", path)
	if _, err := os.Stat(filepath.Join(dir, "data")); err != nil {
		t.Errorf("no data folder: %v", err)
	}
	if !strings.Contains(out, "Data folder ready") || !strings.Contains(out, "Bucket:        share-test at "+fake.URL+" (region "+s3test.Region+"), keys under share/") || strings.Contains(out, "Problem: the bucket") {
		t.Errorf("init printed:\n%s", out)
	}

	fake.SetCORS(false)
	out, err := output(t, "check", "--config", path)
	if err == nil || !strings.Contains(out, "Problem: the bucket's CORS rules") || !strings.Contains(out, `"CORSRules"`) || !strings.Contains(out, `"https://share.example.com"`) {
		t.Errorf("check without CORS rules: %v\n%s", err, out)
	}
	t.Logf("share check:\n%s", out)
	if strings.Contains(out, s3test.Secret) {
		t.Errorf("check printed the secret:\n%s", out)
	}
}

// With PostgreSQL and a bucket, init has nothing to make on this machine.
func TestInitWithPostgresAndABucket(t *testing.T) {
	url := pgtest.URL(t)
	if url == "" {
		t.Skip("needs PostgreSQL (SHARE_TEST_POSTGRES)")
	}
	fake := s3test.New(t)
	saved := openBucket
	openBucket = func(c *config.S3) (*s3.Bucket, error) {
		return s3.Open(c, s3.Options{Transport: fake.Client().Transport, MaxRetries: 1})
	}
	t.Cleanup(func() { openBucket = saved })
	dir := t.TempDir()
	setting, _ := json.Marshal(fake.Config("share/"))
	database, _ := json.Marshal(url)
	path := filepath.Join(dir, "config.json")
	cfg := fmt.Sprintf(`{"public_url": "https://share.example.com", "s3": %s, "database": {"postgres": %s}}`, setting, database)
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := output(t, "init", "--config", path)
	if err != nil || !strings.Contains(out, "Nothing to keep on this machine") || strings.Contains(out, "Data folder") {
		t.Errorf("init: %v\n%s", err, out)
	}
	out, err = output(t, "check", "--config", path)
	if err != nil || !strings.Contains(out, "Database:      PostgreSQL at ") || !strings.Contains(out, "All good.") {
		t.Errorf("check: %v\n%s", err, out)
	}
}
