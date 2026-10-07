package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eschgi/share/server/internal/db/pgtest"
)

func TestCheckNamesTheDatabase(t *testing.T) {
	dir := t.TempDir()
	write := func(url string) string {
		path := filepath.Join(dir, "config.json")
		quoted, _ := json.Marshal(url)
		body := `{"public_url": "https://share.example.test", "storage_dir": "` + filepath.Join(dir, "files") + `", "database": {"postgres": ` + string(quoted) + `}}`
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	out, err := output(t, "check", "--config", write("postgres://share:pw@127.0.0.1:1/share"))
	if err == nil || !strings.Contains(out, "Database:      PostgreSQL at 127.0.0.1:1/share\nProblem: ") || strings.Contains(out, "pw@") {
		t.Errorf("PostgreSQL that doesn't answer: %v\n%s", err, out)
	}

	out, _ = output(t, "check", "--config", write(pgtest.URL(t)))
	if !strings.Contains(out, "Database:      PostgreSQL at 127.0.0.1") || !strings.Contains(out, ", version ") {
		t.Errorf("PostgreSQL:\n%s", out)
	}
}
