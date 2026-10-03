package app

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/ids"
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

// newFolder makes a folder with its directory, as an admin would.
func (e *env) newFolder(name string) db.Folder {
	e.t.Helper()
	e.clock.Add(time.Second)
	f := db.Folder{ID: ids.New(), Name: name, Dir: name, CreatedBy: "test", CreatedAt: e.clock.Now()}
	if err := e.app.DB.InsertFolder(context.Background(), f); err != nil {
		e.t.Fatal(err)
	}
	return f
}

// put puts a file straight into a folder, as if someone had sent it there.
func (e *env) put(folder db.Folder, name string, data []byte) db.File {
	e.t.Helper()
	ctx := context.Background()
	f := db.File{ID: ids.New(), Name: name, Size: int64(len(data)), Kind: db.KindDocument, FolderID: folder.ID,
		CreatedAt: e.clock.Now(), UpdatedAt: e.clock.Now(), UserID: "someone"}
	if err := e.app.DB.InsertReceiving(ctx, f); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.cfg.StorageDir, ".uploads", f.ID), data, 0o644); err != nil {
		e.t.Fatal(err)
	}
	if err := e.app.Lib.Finalize(ctx, f.ID); err != nil {
		e.t.Fatal(err)
	}
	e.clock.Add(time.Second)
	return e.file(f.ID)
}

func TestMembersSeeOnlyTheirFolders(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	maria := e.accept(e.invite(admin, "Maria", "member"), "Maria's phone")
	family := e.firstFolder()
	taxes := e.newFolder("Taxes 2026")
	photo := e.put(family, "IMG_1.jpg", []byte("photo"))
	tax := e.put(taxes, "Steuer.pdf", []byte("tax"))

	// Maria sees Family only, and nothing tells her about Taxes 2026.
	list := e.get("/api/folders", maria.token).json(t)["folders"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["name"] != "Share" {
		t.Fatalf("Maria's folders: %v", list)
	}
	files := e.get("/api/files", maria.token).json(t)["files"].([]any)
	if len(files) != 1 || files[0].(map[string]any)["id"] != photo.ID {
		t.Fatalf("Maria's files: %v", files)
	}
	if days := e.get("/api/library", maria.token).json(t)["days"].([]any); len(days) != 1 || days[0].(map[string]any)["count"] != 1.0 {
		t.Fatalf("Maria's days: %v", days)
	}
	if ids := e.get("/api/files/ids", maria.token).json(t)["ids"].([]any); len(ids) != 1 {
		t.Fatalf("Maria's ids: %v", ids)
	}
	for _, path := range []string{
		"/api/library?folder=" + taxes.ID, "/api/files?folder=" + taxes.ID, "/api/files/ids?folder=" + taxes.ID,
		"/api/files/" + tax.ID, "/api/files/" + tax.ID + "/content", "/api/files/" + tax.ID + "/thumb",
	} {
		wantStatus(t, "Maria: "+path, e.get(path, maria.token), http.StatusNotFound, "not_found")
	}
	wantStatus(t, "Maria's ZIP of Taxes", e.askZip(maria.token, tax.ID), http.StatusNotFound, "not_found")
	if z := e.askZip(maria.token, tax.ID, photo.ID).json(t); z["count"] != 1.0 {
		t.Fatalf("Maria's ZIP of both: %v", z)
	}

	// Admins see every folder, one at a time or together.
	if list := e.get("/api/folders", admin.token).json(t)["folders"].([]any); len(list) != 2 {
		t.Fatalf("the admin's folders: %v", list)
	}
	if files := e.get("/api/files?folder="+taxes.ID, admin.token).json(t)["files"].([]any); len(files) != 1 ||
		files[0].(map[string]any)["folder"] != taxes.ID {
		t.Fatalf("Taxes 2026 for the admin: %v", files)
	}
	if files := e.get("/api/files", admin.token).json(t)["files"].([]any); len(files) != 2 {
		t.Fatalf("all files for the admin: %v", files)
	}

	// Given the folder, Maria sees it too.
	if err := e.app.DB.SetFolderPerson(context.Background(), taxes.ID, maria.userID, true); err != nil {
		t.Fatal(err)
	}
	if r := e.get("/api/files/"+tax.ID+"/content", maria.token); r.status != http.StatusOK || string(r.body) != "tax" {
		t.Fatalf("Maria's download from Taxes 2026: %d %s", r.status, r.body)
	}
}

func TestFolderListCountsAndCover(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	e.accept(e.invite(admin, "Maria", "member"), "Maria's phone")
	e.invite(admin, "Oma Rosa", "member") // still open: Oma Rosa will see the first folder
	family := e.firstFolder()
	taxes := e.newFolder("Taxes 2026")
	pin, _ := e.unlockApp(e.newPin(db.PinDay).Code, "")
	tus{e, pin}.sendFile("old.png", pngBytes(t, 64, 48))
	e.clock.Add(time.Minute)
	newer := tus{e, admin.token}.sendFile("new.png", pngBytes(t, 64, 48))
	tus{e, admin.token}.sendFile("notes.txt", []byte("no picture"))
	e.put(taxes, "Steuer.pdf", []byte("tax"))
	e.clock.Add(3 * time.Minute) // the server makes the thumbnails a little later
	if _, err := e.app.Thumbs.MakePending(context.Background()); err != nil {
		t.Fatal(err)
	}

	r := e.get("/api/folders", admin.token)
	assertShape(t, "folders", readFixture(t, "api/folders.json")["response"], r.json(t))
	list := r.json(t)["folders"].([]any)
	f, tx := list[0].(map[string]any), list[1].(map[string]any)
	if f["id"] != family.ID || f["files"] != 3.0 || f["senders"] != 2.0 || f["people"] != 3.0 || f["admins_only"] != false {
		t.Errorf("first folder: %v", f)
	}
	if cover, _ := f["cover"].(map[string]any); cover == nil || cover["id"] != newer {
		t.Errorf("cover: %v, want the newest picture", f["cover"])
	}
	if tx["files"] != 1.0 || tx["bytes"] != 3.0 || tx["people"] != 1.0 || tx["admins_only"] != true || tx["cover"] != nil {
		t.Errorf("Taxes 2026: %v", tx)
	}
}

func TestZipsAcrossFolders(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	family := e.firstFolder()
	taxes := e.newFolder("Taxes 2026")
	a := e.put(family, "IMG_1.jpg", []byte("photo"))
	b := e.put(taxes, "IMG_1.jpg", []byte("tax"))

	both := e.askZip(admin.token, a.ID, b.ID).json(t)
	var paths []string
	for _, f := range both["files"].([]any) {
		paths = append(paths, f.(map[string]any)["path"].(string))
	}
	slices.Sort(paths)
	if strings.Join(paths, ",") != "Share/2026-09-27/IMG_1.jpg,Taxes 2026/2026-09-27/IMG_1.jpg" || both["name"] != "Share 2026-09-27.zip" {
		t.Fatalf("a ZIP across folders: %v %v", both["name"], paths)
	}
	one := e.askZip(admin.token, b.ID).json(t)
	if p := one["files"].([]any)[0].(map[string]any)["path"]; p != "2026-09-27/IMG_1.jpg" || one["name"] != "Taxes 2026 2026-09-27.zip" {
		t.Fatalf("a ZIP from one folder: %v %v", one["name"], p)
	}
	body := e.get("/api/downloads/"+both["id"].(string), admin.token).body
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if slices.Sort(names); strings.Join(names, ",") != strings.Join(paths, ",") {
		t.Fatalf("the ZIP holds %v", names)
	}
}

func TestDownloadsLeaveOutWhatIsNoLongerVisible(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	maria := e.accept(e.invite(admin, "Maria", "member"), "Maria's phone")
	family := e.firstFolder()
	f := e.put(family, "IMG_1.jpg", []byte("photo"))
	z := e.askZip(maria.token, f.ID).json(t)
	if err := e.app.DB.SetFolderPerson(context.Background(), family.ID, maria.userID, false); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, "the ZIP after Maria lost the folder", e.get("/api/downloads/"+z["id"].(string), maria.token), http.StatusNotFound, "not_found")
	wantStatus(t, "an admin fetching Maria's ZIP", e.get("/api/downloads/"+z["id"].(string), admin.token), http.StatusNotFound, "not_found")
}
