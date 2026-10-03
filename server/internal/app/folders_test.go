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

	"github.com/eschgi/share/server/internal/auth"
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

func TestAdminsCreateRenameAndDeleteFolders(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	admin := e.admin()
	maria := e.accept(e.invite(admin, "Maria", "member"), "Maria's phone")

	r := e.sendJSON("POST", "/api/folders", admin.token, map[string]string{"name": "  Wedding Anna & Marco "})
	wantStatus(t, "create", r, http.StatusCreated, "")
	assertShape(t, "create", readFixture(t, "api/folder_create.json")["response"], r.json(t))
	created := r.json(t)
	if created["name"] != "Wedding Anna & Marco" || created["admins_only"] != true {
		t.Fatalf("created: %v", created)
	}
	id := created["id"].(string)
	if _, err := os.Stat(filepath.Join(e.cfg.StorageDir, "Wedding Anna & Marco")); err != nil {
		t.Fatalf("no directory for the folder: %v", err)
	}
	wantStatus(t, "the same name", e.sendJSON("POST", "/api/folders", admin.token, map[string]string{"name": "wedding anna & marco"}),
		http.StatusConflict, "folder_name_taken")
	wantStatus(t, "a date", e.sendJSON("POST", "/api/folders", admin.token, map[string]string{"name": "2026-09-26"}), http.StatusBadRequest, "bad_request")
	wantStatus(t, "a member", e.sendJSON("POST", "/api/folders", maria.token, map[string]string{"name": "Mine"}), http.StatusForbidden, "forbidden")

	wedding, _ := e.app.DB.FolderByID(ctx, id)
	f := e.put(wedding, "IMG_1.jpg", []byte("photo"))
	pin, err := e.app.Auth.CreatePin(ctx, auth.PinSpec{Kind: db.PinPermanent, FolderID: id}, admin.userID)
	if err != nil {
		t.Fatal(err)
	}
	guest, _ := e.unlockApp(pin.Code, "")
	arriving := (tus{e, guest}).mustCreate("IMG_2.jpg", 100)

	r = e.sendJSON("PATCH", "/api/folders/"+id, admin.token, map[string]string{"name": "Hochzeit Anna & Marco"})
	wantStatus(t, "rename", r, http.StatusOK, "")
	assertShape(t, "rename", readFixture(t, "api/folder_rename.json")["response"], r.json(t))
	if _, err := os.Stat(filepath.Join(e.cfg.StorageDir, "Hochzeit Anna & Marco", "2026-09-27", "IMG_1.jpg")); err != nil {
		t.Fatalf("the directory didn't move: %v", err)
	}
	if r := e.get("/api/files/"+f.ID+"/content", admin.token); string(r.body) != "photo" {
		t.Fatalf("download after the rename: %d %s", r.status, r.body)
	}
	wantStatus(t, "rename someone else's", e.sendJSON("PATCH", "/api/folders/"+id, maria.token, map[string]string{"name": "X"}), http.StatusForbidden, "forbidden")

	r = e.do(nil, "DELETE", "/api/folders/"+id, admin.token, nil, nil)
	wantStatus(t, "delete", r, http.StatusOK, "")
	assertShape(t, "delete", readFixture(t, "api/folder_delete.json")["response"], r.json(t))
	if r.json(t)["changed"] != 1.0 {
		t.Fatalf("delete: %s", r.body)
	}
	if list := e.get("/api/folders", admin.token).json(t)["folders"].([]any); len(list) != 1 {
		t.Fatalf("folders after the delete: %v", list)
	}
	trash := e.get("/api/trash", admin.token)
	assertShape(t, "trash", readFixture(t, "api/trash.json")["response"], trash.json(t))
	folders := trash.json(t)["folders"].([]any)
	if len(folders) != 1 || folders[0].(map[string]any)["deleted"] != true || folders[0].(map[string]any)["name"] != "Hochzeit Anna & Marco" {
		t.Fatalf("the trash's folders: %v", folders)
	}
	if _, r := (tus{e, guest}).head(arriving); r.status == http.StatusOK {
		t.Error("the upload into the deleted folder goes on")
	}
	r = e.postJSON(nil, "/api/pin/unlock", "", map[string]string{"code": pin.Code, "client": "app"}, nil)
	wantStatus(t, "the deleted folder's PIN", r, http.StatusUnauthorized, "pin_ended")
	wantStatus(t, "delete the last folder", e.do(nil, "DELETE", "/api/folders/"+e.firstFolder().ID, admin.token, nil, nil),
		http.StatusConflict, "last_folder")

	// Restoring its file brings the folder back.
	wantStatus(t, "restore", e.sendJSON("POST", "/api/trash/restore", admin.token, map[string][]string{"ids": {f.ID}}), http.StatusOK, "")
	if list := e.get("/api/folders", admin.token).json(t)["folders"].([]any); len(list) != 2 {
		t.Fatalf("folders after the restore: %v", list)
	}
}

// person is someone in GET /api/users.
func person(t *testing.T, people map[string]any, id string) map[string]any {
	t.Helper()
	for _, u := range people["users"].([]any) {
		if u := u.(map[string]any); u["id"] == id {
			return u
		}
	}
	t.Fatalf("%s isn't in %v", id, people)
	return nil
}

func TestAdminsChooseWhoSeesAFolder(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	maria := e.accept(e.invite(admin, "Maria", "member"), "Maria's phone")
	family := e.firstFolder()
	wedding := e.newFolder("Wedding")
	e.put(wedding, "IMG_1.jpg", []byte("photo"))

	people := e.get("/api/users", admin.token)
	assertShape(t, "people", readFixture(t, "api/people.json")["response"], people.json(t))
	if got := person(t, people.json(t), admin.userID)["folders"].([]any); len(got) != 2 {
		t.Errorf("the admin sees %v, want every folder", got)
	}
	if got := person(t, people.json(t), maria.userID)["folders"].([]any); len(got) != 1 || got[0] != family.ID {
		t.Errorf("Maria sees %v", got)
	}

	before := e.get("/api/library", maria.token).json(t)["version"]
	path := "/api/folders/" + wedding.ID + "/people/" + maria.userID
	wantStatus(t, "give", e.do(nil, "PUT", path, admin.token, nil, nil), http.StatusNoContent, "")
	wantStatus(t, "give again", e.do(nil, "PUT", path, admin.token, nil, nil), http.StatusNoContent, "")
	lib := e.get("/api/library", maria.token).json(t)
	if lib["version"] == before || len(e.get("/api/files?folder="+wedding.ID, maria.token).json(t)["files"].([]any)) != 1 {
		t.Fatalf("after giving Maria the folder: %v", lib)
	}
	if got := person(t, e.get("/api/users", admin.token).json(t), maria.userID)["folders"].([]any); len(got) != 2 {
		t.Errorf("Maria sees %v", got)
	}
	wantStatus(t, "take away", e.do(nil, "DELETE", path, admin.token, nil, nil), http.StatusNoContent, "")
	wantStatus(t, "Maria after", e.get("/api/files?folder="+wedding.ID, maria.token), http.StatusNotFound, "not_found")

	wantStatus(t, "a member switching", e.do(nil, "PUT", path, maria.token, nil, nil), http.StatusForbidden, "forbidden")
	wantStatus(t, "no such person", e.do(nil, "PUT", "/api/folders/"+wedding.ID+"/people/"+ids.New(), admin.token, nil, nil), http.StatusNotFound, "not_found")
	wantStatus(t, "no such folder", e.do(nil, "PUT", "/api/folders/"+ids.New()+"/people/"+maria.userID, admin.token, nil, nil), http.StatusNotFound, "not_found")
}

func TestInvitesGiveTheirFolders(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	family := e.firstFolder()
	wedding := e.newFolder("Wedding")
	kindergarten := e.newFolder("Kindergarten")

	invite := func(body map[string]any) response {
		return e.sendJSON("POST", "/api/invites", admin.token, body)
	}
	r := invite(map[string]any{"name": "Oma Rosa", "role": "member", "folders": []string{kindergarten.ID, wedding.ID}})
	wantStatus(t, "invite", r, http.StatusCreated, "")
	assertShape(t, "invite", readFixture(t, "api/invite_create.json")["response"], r.json(t))
	in := r.json(t)["invite"].(map[string]any)
	if got := in["folders"].([]any); len(got) != 2 {
		t.Fatalf("the invite gives %v", got)
	}

	// The folder's switch for the invite.
	path := "/api/folders/" + family.ID + "/invites/" + in["id"].(string)
	wantStatus(t, "give the invite a folder", e.do(nil, "PUT", path, admin.token, nil, nil), http.StatusNoContent, "")
	wantStatus(t, "and take another", e.do(nil, "DELETE", "/api/folders/"+wedding.ID+"/invites/"+in["id"].(string), admin.token, nil, nil), http.StatusNoContent, "")
	rosa := e.accept(r.json(t)["token"].(string), "Rosa's tablet")
	got, _ := e.app.DB.FoldersOf(context.Background(), rosa.userID)
	if len(got) != 2 || got[0].ID != family.ID || got[1].ID != kindergarten.ID {
		t.Fatalf("Oma Rosa sees %+v", got)
	}
	wantStatus(t, "a used invite", e.do(nil, "PUT", path, admin.token, nil, nil), http.StatusNotFound, "not_found")

	// None at all, the oldest when left out, and none for an admin.
	for _, tc := range []struct {
		body map[string]any
		want int
	}{
		{map[string]any{"name": "Peter", "role": "member", "folders": []string{}}, 0},
		{map[string]any{"name": "Anna", "role": "member"}, 1},
		{map[string]any{"name": "Marco", "role": "admin", "folders": []string{wedding.ID}}, 3},
	} {
		r := invite(tc.body)
		if got := r.json(t)["invite"].(map[string]any)["folders"].([]any); len(got) != tc.want {
			t.Errorf("%v gives %v", tc.body, got)
		}
	}
	wantStatus(t, "an unknown folder", invite(map[string]any{"name": "X", "role": "member", "folders": []string{ids.New()}}),
		http.StatusNotFound, "not_found")
}

func TestDemotedAdminKeepsSeeingEverything(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	peter := e.accept(e.invite(admin, "Peter", "admin"), "Peter's phone")
	e.newFolder("Wedding")
	wantStatus(t, "demote", e.sendJSON("PATCH", "/api/users/"+peter.userID, admin.token, map[string]string{"role": "member"}), http.StatusNoContent, "")
	if list := e.get("/api/folders", peter.token).json(t)["folders"].([]any); len(list) != 2 {
		t.Fatalf("Peter, a member now, sees %v", list)
	}
}

func TestAdminsMoveFiles(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	maria := e.accept(e.invite(admin, "Maria", "member"), "Maria's phone")
	taxes := e.newFolder("Taxes 2026")
	id := tus{e, maria.token}.sendFile("Steuer.png", pngBytes(t, 64, 48))
	e.clock.Add(3 * time.Minute)
	if _, err := e.app.Thumbs.MakePending(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := e.file(id)

	r := e.sendJSON("POST", "/api/files/move", admin.token, map[string]any{"ids": []string{id}, "folder": taxes.ID})
	wantStatus(t, "move", r, http.StatusOK, "")
	assertShape(t, "move", readFixture(t, "api/files_move.json")["response"], r.json(t))
	after := e.file(id)
	if after.FolderID != taxes.ID || !after.UpdatedAt.Equal(before.UpdatedAt) || after.Thumb != before.Thumb {
		t.Fatalf("after the move: %+v", after)
	}
	if _, err := os.Stat(e.disk(after)); err != nil {
		t.Fatalf("not in Taxes 2026 on the drive: %v", err)
	}
	// Maria doesn't see Taxes 2026; the admin still downloads the file and its thumbnail.
	wantStatus(t, "Maria after the move", e.get("/api/files/"+id, maria.token), http.StatusNotFound, "not_found")
	wantStatus(t, "the thumbnail", e.get("/api/files/"+id+"/thumb", admin.token), http.StatusOK, "")

	wantStatus(t, "a member moving", e.sendJSON("POST", "/api/files/move", maria.token, map[string]any{"ids": []string{id}, "folder": taxes.ID}),
		http.StatusForbidden, "forbidden")
	wantStatus(t, "no such folder", e.sendJSON("POST", "/api/files/move", admin.token, map[string]any{"ids": []string{id}, "folder": ids.New()}),
		http.StatusNotFound, "not_found")
	wantStatus(t, "no ids", e.sendJSON("POST", "/api/files/move", admin.token, map[string]any{"ids": []string{}, "folder": taxes.ID}),
		http.StatusBadRequest, "bad_request")
}
