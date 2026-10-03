package app

import (
	"context"
	"net/http"
	"os"
	"testing"

	"github.com/eschgi/share/server/internal/db"
)

func TestEveryFileAndPinGoesIntoAFolder(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	admin := e.admin()
	maria := e.accept(e.invite(admin, "Maria", "member"), "Maria's phone")
	first := e.firstFolder()
	if folders, _ := e.app.DB.FoldersOf(ctx, maria.userID); len(folders) != 1 || folders[0].ID != first.ID {
		t.Fatalf("a new member sees %+v, want the first folder", folders)
	}

	// Signed in or with a PIN, files land in the first folder, in the day's folder.
	id := tus{e, maria.token}.sendFile("IMG_1.jpg", randomBytes(t, 100))
	pin := e.newPin(db.PinDay)
	if pin.FolderID != first.ID {
		t.Fatalf("the PIN sends into %q", pin.FolderID)
	}
	token, _ := e.unlockApp(pin.Code, "")
	pinned := tus{e, token}.sendFile("IMG_2.jpg", randomBytes(t, 100))
	for _, id := range []string{id, pinned} {
		f := e.file(id)
		if f.FolderID != first.ID || f.State != db.StateReady {
			t.Fatalf("file %s: folder %q, state %s", f.Name, f.FolderID, f.State)
		}
		if _, err := os.Stat(e.disk(f)); err != nil {
			t.Fatalf("%s isn't on the drive: %v", f.RelPath, err)
		}
	}

	// A member who was given no folder can't send.
	if err := e.app.DB.SetFolderPerson(ctx, first.ID, maria.userID, false); err != nil {
		t.Fatal(err)
	}
	if r := (tus{e, maria.token}).create("IMG_3.jpg", 10); r.status != http.StatusForbidden || r.errorCode() != "no_folder" {
		t.Fatalf("sending without a folder: %d %s", r.status, r.body)
	}
	// An admin sees every folder without being given any.
	tus{e, admin.token}.sendFile("IMG_4.jpg", randomBytes(t, 10))
}
