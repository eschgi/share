package db

import (
	"context"
	"errors"
	"testing"

	"github.com/eschgi/share/server/internal/ids"
)

// Names that differ only in case are the same name, in every letter: Ö and ö, Σ, σ and ς.
func TestNamesIgnoreCaseInEveryLetter(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	for _, name := range []string{"Ötzi", "Σίσυφος"} {
		if err := d.InsertUser(ctx, User{ID: ids.New(), Name: name, Username: name, Role: RoleMember, CreatedAt: t0, CreatedBy: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	for typed, want := range map[string]string{"Ötzi": "Ötzi", "ötzi": "Ötzi", "ÖTZI": "Ötzi", "ΣΊΣΥΦΟΣ": "Σίσυφος", "σίσυφοσ": "Σίσυφος"} {
		if u, err := d.UserByUsername(ctx, typed); err != nil || u.Name != want {
			t.Errorf("UserByUsername(%q) = %+v, %v; want %s", typed, u, err, want)
		}
	}
	if err := d.InsertUser(ctx, User{ID: ids.New(), Name: "Other", Username: "öTZI", Role: RoleMember, CreatedAt: t0, CreatedBy: "test"}); !errors.Is(err, ErrConflict) {
		t.Errorf("öTZI next to Ötzi: %v, want ErrConflict", err)
	}

	doctors := testFolder(t, d, "Ärzte")
	if taken, err := d.DirTaken(ctx, "ärzte"); err != nil || !taken {
		t.Errorf("DirTaken(ärzte) next to Ärzte = %v, %v", taken, err)
	}
	f := File{ID: ids.New(), Name: "Über.pdf", Size: 3, CreatedAt: t0, UpdatedAt: t0, FolderID: doctors}
	if err := d.InsertReceiving(ctx, f); err != nil {
		t.Fatal(err)
	}
	if ok, err := d.MarkFinalizing(ctx, f.ID, "2026-09-27/Über.pdf", "2026-09-27", t0); err != nil || !ok {
		t.Fatalf("MarkFinalizing = %v, %v", ok, err)
	}
	if err := d.MarkReady(ctx, f.ID, "application/pdf", KindDocument, t0); err != nil {
		t.Fatal(err)
	}
	if taken, err := d.RelPathTaken(ctx, doctors, "2026-09-27/ÜBER.PDF"); err != nil || !taken {
		t.Errorf("RelPathTaken(ÜBER.PDF) next to Über.pdf = %v, %v", taken, err)
	}
	for _, q := range []string{"über", "ÜBER", "BER.P"} {
		files, err := d.LibraryFiles(ctx, LibraryFilter{Folders: []string{doctors}, Query: q}, nil, 10)
		if err != nil || len(files) != 1 {
			t.Errorf("searching %q: %d files, %v", q, len(files), err)
		}
	}
}
