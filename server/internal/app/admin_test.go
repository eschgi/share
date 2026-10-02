package app

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/db"
)

func (e *env) get(path, token string) response {
	e.t.Helper()
	return e.do(nil, "GET", path, token, nil, nil)
}

func (e *env) sendJSON(method, path, token string, v any) response {
	e.t.Helper()
	return e.do(nil, method, path, token, bytes.NewReader(mustJSON(e.t, v)), map[string]string{"Content-Type": "application/json"})
}

func wantStatus(t *testing.T, label string, r response, status int, code string) {
	t.Helper()
	if r.status != status || (code != "" && r.errorCode() != code) {
		t.Errorf("%s: %d %s, want %d %s", label, r.status, r.body, status, code)
	}
}

func TestAdminsManagePins(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	member := e.accept(e.invite(admin, "Maria", "member"), "Pixel 8")
	wantStatus(t, "a member listing PINs", e.get("/api/pins", member.token), http.StatusForbidden, "forbidden")

	suggestion := e.get("/api/pins/suggest", admin.token)
	assertShape(t, "suggestion", readFixture(t, "api/pin_suggest.json")["response"], suggestion.json(t))
	if code := suggestion.json(t)["code"].(string); len(code) != 5 {
		t.Errorf("suggested %q", code)
	}

	created := e.postJSON(nil, "/api/pins", admin.token, map[string]string{"kind": "day", "code": "r8d-4w"}, nil)
	wantStatus(t, "a chosen code", created, http.StatusCreated, "")
	assertShape(t, "new PIN", readFixture(t, "api/pin_create.json")["response"], created.json(t))
	day := created.json(t)
	if day["code"] != "R8D4W" || day["link"] != "https://share.example.test/#R8D4W" || day["expires_at"] == nil {
		t.Errorf("new PIN: %v", day)
	}
	wantStatus(t, "the same code again", e.postJSON(nil, "/api/pins", admin.token, map[string]string{"kind": "permanent", "code": "R8D4W"}, nil),
		http.StatusConflict, "pin_taken")
	wantStatus(t, "not a PIN", e.postJSON(nil, "/api/pins", admin.token, map[string]string{"kind": "day", "code": "abc"}, nil),
		http.StatusBadRequest, "pin_format")
	wantStatus(t, "an unknown kind", e.postJSON(nil, "/api/pins", admin.token, map[string]string{"kind": "week"}, nil),
		http.StatusBadRequest, "bad_request")
	perm := e.postJSON(nil, "/api/pins", admin.token, map[string]string{"kind": "permanent"}, nil).json(t)

	// Two phones send with the 24-hour PIN.
	for range 2 {
		token, _ := e.unlockApp("R8D4W", "")
		tus{e, token}.sendFile("IMG.jpg", randomBytes(t, 100))
	}
	list := e.get("/api/pins", admin.token)
	assertShape(t, "PINs", readFixture(t, "api/pins.json")["response"], list.json(t))
	pins := list.json(t)["pins"].([]any)
	if len(pins) != 2 || pins[0].(map[string]any)["id"] != perm["id"] {
		t.Fatalf("PINs: %v", pins)
	}
	if stats := pins[1].(map[string]any); stats["files"] != 2.0 || stats["phones"] != 2.0 {
		t.Errorf("stats: %v", stats)
	}

	renewed := e.postJSON(nil, "/api/pins/"+day["id"].(string)+"/new-code", admin.token, nil, nil)
	wantStatus(t, "new code", renewed, http.StatusOK, "")
	assertShape(t, "renewed", readFixture(t, "api/pin_new_code.json")["response"], renewed.json(t))
	if renewed.json(t)["code"] == "R8D4W" || renewed.json(t)["kind"] != "day" {
		t.Errorf("renewed: %v", renewed.json(t))
	}
	wantStatus(t, "the old code", e.postJSON(nil, "/api/pin/unlock", "", map[string]string{"code": "R8D4W", "client": "app"}, nil),
		http.StatusUnauthorized, "pin_ended")

	end := "/api/pins/" + perm["id"].(string) + "/end"
	wantStatus(t, "ending", e.postJSON(nil, end, admin.token, nil, nil), http.StatusNoContent, "")
	wantStatus(t, "ending again", e.postJSON(nil, end, admin.token, nil, nil), http.StatusNotFound, "not_found")
	if pins := e.get("/api/pins", admin.token).json(t)["pins"].([]any); len(pins) != 1 {
		t.Errorf("after ending: %v", pins)
	}
}

func TestAdminsManagePeople(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()

	created := e.postJSON(nil, "/api/invites", admin.token, map[string]string{"name": "Oma Rosa", "role": "member"}, nil)
	wantStatus(t, "invite", created, http.StatusCreated, "")
	assertShape(t, "invite", readFixture(t, "api/invite_create.json")["response"], created.json(t))
	inv := created.json(t)
	if !strings.HasPrefix(inv["link"].(string), "https://share.example.test/join#shi_") || !strings.HasSuffix(inv["link"].(string), inv["token"].(string)) {
		t.Errorf("link %v", inv["link"])
	}
	wantStatus(t, "an invite without a name", e.postJSON(nil, "/api/invites", admin.token, map[string]string{"name": " ", "role": "member"}, nil),
		http.StatusBadRequest, "bad_request")

	maria := e.accept(e.invite(admin, "Maria", "member"), "Pixel 8")
	withdrawn := e.postJSON(nil, "/api/invites", admin.token, map[string]string{"name": "Peter", "role": "admin"}, nil).json(t)

	people := e.get("/api/users", admin.token)
	assertShape(t, "people", readFixture(t, "api/people.json")["response"], people.json(t))
	body := people.json(t)
	if n := len(body["users"].([]any)); n != 2 {
		t.Fatalf("people: %v", body)
	}
	if first := body["users"].([]any)[0].(map[string]any); first["me"] != true || first["phones"].([]any)[0].(map[string]any)["this"] != true {
		t.Errorf("the admin asking: %v", first)
	}
	if n := len(body["invites"].([]any)); n != 2 {
		t.Errorf("open invites: %v", body["invites"])
	}

	// A second phone for Maria.
	phone := e.postJSON(nil, "/api/users/"+maria.userID+"/invites", admin.token, nil, nil)
	wantStatus(t, "a phone for Maria", phone, http.StatusCreated, "")
	if phone.json(t)["invite"].(map[string]any)["user_id"] != maria.userID {
		t.Errorf("phone invite: %v", phone.json(t))
	}
	tablet := e.accept(phone.json(t)["token"].(string), "Galaxy Tab")
	if tablet.userID != maria.userID {
		t.Fatal("the tablet belongs to someone else")
	}
	var mariaPhones []any
	for _, u := range e.get("/api/users", admin.token).json(t)["users"].([]any) {
		if u.(map[string]any)["id"] == maria.userID {
			mariaPhones = u.(map[string]any)["phones"].([]any)
		}
	}
	if len(mariaPhones) != 2 {
		t.Fatalf("Maria's phones: %v", mariaPhones)
	}

	// Roles, with somebody always left to manage the server.
	role := func(p signedIn, r string) response {
		return e.sendJSON("PATCH", "/api/users/"+p.userID, admin.token, map[string]string{"role": r})
	}
	wantStatus(t, "a member asks", e.get("/api/users", maria.token), http.StatusForbidden, "forbidden")
	wantStatus(t, "the only admin steps down", role(admin, "member"), http.StatusConflict, "last_admin")
	wantStatus(t, "Maria becomes admin", role(maria, "admin"), http.StatusNoContent, "")
	wantStatus(t, "now Maria may", e.get("/api/pins", maria.token), http.StatusOK, "")
	wantStatus(t, "an unknown role", role(maria, "owner"), http.StatusBadRequest, "bad_request")
	wantStatus(t, "Stefan steps down", role(admin, "member"), http.StatusNoContent, "")
	wantStatus(t, "and can't manage any more", e.get("/api/users", admin.token), http.StatusForbidden, "forbidden")
	wantStatus(t, "removing the last admin", e.do(nil, "DELETE", "/api/users/"+maria.userID, maria.token, nil, nil),
		http.StatusConflict, "last_admin")

	// Signing one phone out, withdrawing an invite, removing a person.
	tabletID := ""
	for _, p := range mariaPhones {
		if p.(map[string]any)["name"] == "Galaxy Tab" {
			tabletID = p.(map[string]any)["id"].(string)
		}
	}
	wantStatus(t, "signing the tablet out", e.do(nil, "DELETE", "/api/devices/"+tabletID, maria.token, nil, nil), http.StatusNoContent, "")
	wantStatus(t, "the tablet", e.get("/api/me", tablet.token), http.StatusUnauthorized, "signed_out")
	wantStatus(t, "the phone", e.get("/api/me", maria.token), http.StatusOK, "")
	wantStatus(t, "again", e.do(nil, "DELETE", "/api/devices/"+tabletID, maria.token, nil, nil), http.StatusNotFound, "not_found")

	invID := withdrawn["invite"].(map[string]any)["id"].(string)
	wantStatus(t, "withdrawing", e.do(nil, "DELETE", "/api/invites/"+invID, maria.token, nil, nil), http.StatusNoContent, "")
	wantStatus(t, "the withdrawn invite", e.postJSON(nil, "/api/invites/accept", "", map[string]string{"token": withdrawn["token"].(string)}, nil),
		http.StatusGone, "invite_revoked")

	rosa := e.accept(inv["token"].(string), "Rosa's phone")
	wantStatus(t, "removing Rosa", e.do(nil, "DELETE", "/api/users/"+rosa.userID, maria.token, nil, nil), http.StatusNoContent, "")
	wantStatus(t, "Rosa's phone", e.get("/api/me", rosa.token), http.StatusUnauthorized, "signed_out")
	wantStatus(t, "Rosa again", e.do(nil, "DELETE", "/api/users/"+rosa.userID, maria.token, nil, nil), http.StatusNotFound, "not_found")
}

func TestAdminsSetNewPasswords(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	e.setPassword(admin.token, "stefan", "correct horse")
	maria := e.accept(e.invite(admin, "Maria", "member"), "Pixel 8")
	reset := func(by signedIn, userID string, v any) response {
		return e.postJSON(nil, "/api/users/"+userID+"/password", by.token, v, nil)
	}
	login := func(user, pass string) response {
		return e.postJSON(nil, "/api/auth/login", "", map[string]string{"username": user, "password": pass, "device_name": "Laptop"}, nil)
	}

	wantStatus(t, "a member", reset(maria, admin.userID, map[string]string{}), http.StatusForbidden, "forbidden")
	wantStatus(t, "for oneself", reset(admin, admin.userID, map[string]string{}), http.StatusForbidden, "forbidden")
	wantStatus(t, "nobody", reset(admin, "u7ld5x2k7mbqz4bwdbyj6qsqxa", map[string]string{"username": "nobody"}), http.StatusNotFound, "not_found")
	wantStatus(t, "Maria has no username yet", reset(admin, maria.userID, map[string]string{}), http.StatusBadRequest, "bad_request")
	wantStatus(t, "a username with a space", reset(admin, maria.userID, map[string]string{"username": "maria rossi"}), http.StatusBadRequest, "bad_request")
	wantStatus(t, "Stefan's username", reset(admin, maria.userID, map[string]string{"username": "Stefan"}), http.StatusConflict, "username_taken")

	r := reset(admin, maria.userID, map[string]string{"username": " maria.rossi "})
	wantStatus(t, "a first password", r, http.StatusOK, "")
	assertShape(t, "new password", readFixture(t, "api/user_password.json")["response"], r.json(t))
	if cc := r.header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control %q", cc)
	}
	first := r.json(t)
	if first["username"] != "maria.rossi" || len(first["password"].(string)) != 19 {
		t.Fatalf("new password: %v", first)
	}
	wantStatus(t, "Maria signs in with it", login("maria.rossi", first["password"].(string)), http.StatusOK, "")
	if me := e.get("/api/me", maria.token).json(t)["user"].(map[string]any); me["username"] != "maria.rossi" || me["has_password"] != true {
		t.Errorf("Maria: %v", me)
	}

	// Maria forgot it, tried too often, and is paused; a new one works at once, and her
	// phones stay signed in.
	for range 10 {
		login("maria.rossi", "guess")
	}
	wantStatus(t, "paused", login("maria.rossi", first["password"].(string)), http.StatusTooManyRequests, "login_locked")
	again := reset(admin, maria.userID, map[string]string{})
	wantStatus(t, "another one", again, http.StatusOK, "")
	second := again.json(t)
	if second["username"] != "maria.rossi" || second["password"] == first["password"] {
		t.Fatalf("another one: %v", second)
	}
	wantStatus(t, "the old one", login("maria.rossi", first["password"].(string)), http.StatusUnauthorized, "login_wrong")
	wantStatus(t, "the new one", login("maria.rossi", second["password"].(string)), http.StatusOK, "")
	wantStatus(t, "her phone", e.get("/api/me", maria.token), http.StatusOK, "")
	wantStatus(t, "Stefan's password", login("stefan", "correct horse"), http.StatusOK, "")

	renamed := reset(admin, maria.userID, map[string]string{"username": "mrossi"})
	wantStatus(t, "with another username", renamed, http.StatusOK, "")
	wantStatus(t, "the old username", login("maria.rossi", renamed.json(t)["password"].(string)), http.StatusUnauthorized, "login_wrong")
	wantStatus(t, "the new username", login("mrossi", renamed.json(t)["password"].(string)), http.StatusOK, "")
}

func TestDeleteRestoreAndPurge(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	member := e.accept(e.invite(admin, "Maria", "member"), "Pixel 8")
	token, _ := e.unlockApp(e.newPin(db.PinPermanent).Code, "")
	up := tus{e, token}
	photo := jpegBytes(t, 64, 48, 1)
	a := up.sendFile("IMG_1.jpg", photo)
	if r := e.putThumb(token, a, "", "image/jpeg", jpegBytes(t, 32, 24, 1)); r.status != http.StatusNoContent {
		t.Fatalf("thumbnail: %d %s", r.status, r.body)
	}
	b := up.sendFile("Menu.pdf", randomBytes(t, 500))
	c := up.sendFile("clip.mp4", randomBytes(t, 700))
	del := func(p signedIn, fileIDs ...string) response {
		return e.postJSON(nil, "/api/files/delete", p.token, map[string][]string{"ids": fileIDs}, nil)
	}

	wantStatus(t, "a member deletes", del(member, a), http.StatusForbidden, "forbidden")
	wantStatus(t, "no ids", del(admin), http.StatusBadRequest, "bad_request")
	wantStatus(t, "not an id", del(admin, "../etc"), http.StatusBadRequest, "bad_request")
	deleted := del(admin, a, b)
	wantStatus(t, "deleting two", deleted, http.StatusOK, "")
	assertShape(t, "deleted", readFixture(t, "api/files_delete.json")["response"], deleted.json(t))
	if deleted.json(t)["changed"] != 2.0 {
		t.Errorf("deleted: %v", deleted.json(t))
	}
	if ids := e.get("/api/files/ids", member.token).json(t)["ids"].([]any); len(ids) != 1 || ids[0] != c {
		t.Errorf("library after deleting: %v", ids)
	}
	wantStatus(t, "a deleted file", e.get("/api/files/"+a+"/content", admin.token), http.StatusNotFound, "not_found")
	wantStatus(t, "its thumbnail, for a member", e.get("/api/files/"+a+"/thumb", member.token), http.StatusNotFound, "not_found")
	wantStatus(t, "its thumbnail, for an admin", e.get("/api/files/"+a+"/thumb", admin.token), http.StatusOK, "")

	trash := e.get("/api/trash", admin.token)
	assertShape(t, "trash", readFixture(t, "api/trash.json")["response"], trash.json(t))
	files := trash.json(t)["files"].([]any)
	if len(files) != 2 || files[0].(map[string]any)["deleted_by"] != "Admin" {
		t.Fatalf("trash: %v", files)
	}
	wantStatus(t, "a member's trash", e.get("/api/trash", member.token), http.StatusForbidden, "forbidden")

	restored := e.postJSON(nil, "/api/trash/restore", admin.token, map[string][]string{"ids": {a, c}}, nil)
	assertShape(t, "restored", readFixture(t, "api/trash_restore.json")["response"], restored.json(t))
	if restored.json(t)["changed"] != 1.0 {
		t.Errorf("restored: %v", restored.json(t))
	}
	if got := e.get("/api/files/"+a+"/content", member.token); got.status != http.StatusOK || !bytes.Equal(got.body, photo) {
		t.Errorf("restored file: %d, %d bytes", got.status, len(got.body))
	}

	purged := e.postJSON(nil, "/api/trash/purge", admin.token, map[string][]string{"ids": {b}}, nil)
	assertShape(t, "purged", readFixture(t, "api/trash_purge.json")["response"], purged.json(t))
	if purged.json(t)["changed"] != 1.0 || len(e.get("/api/trash", admin.token).json(t)["files"].([]any)) != 0 {
		t.Errorf("purged: %v", purged.json(t))
	}

	// After thirty days in the trash, a file goes by itself.
	del(admin, c)
	e.clock.Add(31 * 24 * time.Hour)
	if n, err := e.app.Lib.PurgeOld(context.Background(), e.cfg.TrashDays); err != nil || n != 1 {
		t.Fatalf("purge: %d, %v", n, err)
	}
	if _, err := e.app.DB.FileByID(context.Background(), c); err == nil {
		t.Error("the old file is still there")
	}
}

func TestStorageInfo(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	member := e.accept(e.invite(admin, "Maria", "member"), "Pixel 8")
	token, _ := e.unlockApp(e.newPin(db.PinPermanent).Code, "")
	up := tus{e, token}
	up.sendFile("a.jpg", randomBytes(t, 300))
	gone := up.sendFile("b.jpg", randomBytes(t, 200))
	e.postJSON(nil, "/api/files/delete", admin.token, map[string][]string{"ids": {gone}}, nil)

	wantStatus(t, "a member asks", e.get("/api/admin/storage", member.token), http.StatusForbidden, "forbidden")
	info := e.get("/api/admin/storage", admin.token)
	assertShape(t, "storage", readFixture(t, "api/storage.json")["response"], info.json(t))
	v := info.json(t)
	if v["files"] != 1.0 || v["bytes"] != 300.0 || v["trash_files"] != 1.0 || v["trash_bytes"] != 200.0 || v["storage_dir"] != e.cfg.StorageDir {
		t.Errorf("storage: %v", v)
	}
}
