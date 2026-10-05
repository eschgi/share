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
	write := func(database string) string {
		path := filepath.Join(dir, "config.json")
		body := `{"public_url": "https://share.example.test", "storage_dir": "` + filepath.Join(dir, "files") + `"` + database + `}`
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	path := write("")
	if _, err := output(t, "init", "--config", path); err != nil {
		t.Logf("init: %v", err) // a temporary folder isn't a drive
	}
	out, _ := output(t, "check", "--config", path)
	if !strings.Contains(out, "Database:      SQLite in "+filepath.Join(dir, "files", ".share", "share.db")) {
		t.Errorf("SQLite:\n%s", out)
	}

	unreachable, _ := json.Marshal("postgres://share:pw@127.0.0.1:1/share")
	out, err := output(t, "check", "--config", write(`, "database": {"postgres": `+string(unreachable)+`}`))
	if err == nil || !strings.Contains(out, "Database:      PostgreSQL at 127.0.0.1:1/share\nProblem: ") || strings.Contains(out, "pw@") {
		t.Errorf("PostgreSQL that doesn't answer: %v\n%s", err, out)
	}

	if url := pgtest.URL(t); url != "" {
		quoted, _ := json.Marshal(url)
		out, _ := output(t, "check", "--config", write(`, "database": {"postgres": `+string(quoted)+`}`))
		if !strings.Contains(out, "Database:      PostgreSQL at 127.0.0.1") || !strings.Contains(out, ", version ") {
			t.Errorf("PostgreSQL:\n%s", out)
		}
	}
}
