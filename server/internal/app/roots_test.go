package app

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/db/dbtest"
	"github.com/eschgi/share/server/internal/e2ee"
)

// folderInfo is a folder as GET /api/folders describes it to who.
func (e *env) folderInfo(who signedIn, id string) map[string]any {
	e.t.Helper()
	for _, f := range e.get("/api/folders", who.token).json(e.t)["folders"].([]any) {
		if f := f.(map[string]any); f["id"] == id {
			return f
		}
	}
	e.t.Fatalf("no folder %s", id)
	return nil
}

// The first recovery key signs every folder as plain, deleted ones too; a new one is signed by
// the one before it and signs everything anew, and the server takes it only complete. Its
// private key goes to the admin who made it; other admins get it from an admin who holds it.
func TestRoots(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	family := e.firstFolder()
	trips := e.newFolder("Trips")
	gone := e.newFolder("Gone")
	if _, err := e.app.Lib.DeleteFolder(context.Background(), gone.ID, admin.userID); err != nil {
		t.Fatal(err)
	}
	ak := e.keyring(admin)
	ak.makePersonKey()

	before := ak.recoveryAnswer()
	if before.PublicKey != nil || len(before.Sign.FolderKeys) != 0 || len(before.Sign.Plain) != 3 {
		t.Fatalf("before the first recovery key: %+v", before)
	}
	body, _, _, _ := ak.newRoot()
	plain := body["plain"].([]map[string]any)
	body["plain"] = plain[:2]
	if r := e.sendJSON("PUT", "/api/recovery", admin.token, body); r.status != http.StatusConflict || r.errorCode() != "key_outdated" {
		t.Errorf("a first recovery key that leaves out a folder: %d %s", r.status, r.body)
	}
	body["plain"] = plain
	body["signature"] = ak.sign(e2ee.RootMessage(ak.unb64(body["public_key"].(string))))
	if r := e.sendJSON("PUT", "/api/recovery", admin.token, body); r.status != http.StatusBadRequest || r.errorCode() != "bad_signature" {
		t.Errorf("a first recovery key with a signature: %d %s", r.status, r.body)
	}
	maria := e.accept(e.invite(admin, "Maria", db.RoleMember), "Maria's phone")
	if r := e.sendJSON("PUT", "/api/recovery", maria.token, body); r.status != http.StatusForbidden {
		t.Errorf("a member makes the recovery key: %d %s", r.status, r.body)
	}
	ak.makeRoot()
	first := ak.rootPub
	a := ak.sync()
	if len(a.Roots) != 1 || a.Roots[0].Signature != nil || ak.root == nil {
		t.Fatalf("the first root: %+v", a.Roots)
	}
	for _, f := range []db.Folder{family, trips} {
		info := e.folderInfo(admin, f.ID)
		if !e2ee.Verify(first, e2ee.PlainMessage(f.ID, 0, f.Name), ak.unb64(info["plain_signature"].(string))) {
			t.Errorf("%s isn't signed as plain", f.Name)
		}
	}
	if f, _ := e.app.DB.FolderByID(context.Background(), gone.ID); !e2ee.Verify(first, e2ee.PlainMessage(gone.ID, 0, gone.Name), f.PlainSignature) {
		t.Error("a deleted folder isn't signed as plain")
	}
	if r := ak.encrypt(family.ID); r.status != http.StatusOK {
		t.Fatalf("encrypting: %d %s", r.status, r.body)
	}
	ak.work(ak.sync())

	// Eva, another admin, gets the root's private key from Stefan's phone.
	eva := e.accept(e.invite(admin, "Eva", db.RoleAdmin), "Eva's phone")
	ek := e.keyring(eva)
	ek.makePersonKey()
	a = ak.sync()
	if len(a.Todo.Roots) != 1 || a.Todo.Roots[0].User != eva.userID || a.Todo.Roots[0].Name != "Eva" || !a.Todo.Roots[0].Active {
		t.Fatalf("Eva waits for the root: %+v", a.Todo.Roots)
	}
	if e := ek.sync(); len(e.Todo.Roots) != 0 || e.RootSealed != nil {
		t.Errorf("Eva's to-do list names someone, or she holds the root: %+v", e)
	}
	// A member can't give it, and nobody can give it to a member.
	sealed, _ := e2ee.Seal(ek.personPub, e2ee.PurposeRoot, e2ee.RootContext, ak.root)
	grant := map[string]any{"roots": []map[string]any{{"user": eva.userID, "sealed": b64u.EncodeToString(sealed)}}}
	if r := e.postJSON(nil, "/api/keys/grants", maria.token, grant, nil); r.status != http.StatusNoContent {
		t.Fatalf("grants: %d %s", r.status, r.body)
	}
	if ek.sync(); ek.root != nil {
		t.Error("Eva got the root from a member")
	}
	mk := e.keyring(maria)
	mk.makePersonKey()
	forMaria, _ := e2ee.Seal(mk.personPub, e2ee.PurposeRoot, e2ee.RootContext, ak.root)
	e.postJSON(nil, "/api/keys/grants", admin.token, map[string]any{"roots": []map[string]any{{"user": maria.userID, "sealed": b64u.EncodeToString(forMaria)}}}, nil)
	if a := mk.sync(); a.RootSealed != nil {
		t.Error("a member holds the root")
	}
	// A check for Eva qualifies for the root alone.
	_, key := e2ee.GenerateKey()
	if r := e.postJSON(nil, "/api/keys/checks", admin.token, map[string]any{"user": eva.userID, "commitment": b64u.EncodeToString(e2ee.Commitment(key))}, nil); r.status != http.StatusCreated {
		t.Errorf("a check for an admin who lacks the root: %d %s", r.status, r.body)
	}
	ak.work(ak.sync())
	if ek.sync(); !bytes.Equal(ek.root, ak.root) {
		t.Fatal("Eva doesn't hold the root")
	}
	if a := ak.sync(); len(a.Todo.Roots) != 0 {
		t.Errorf("still to do: %+v", a.Todo.Roots)
	}

	// A new recovery key: signed by the one before, signing everything anew.
	body, secret, priv, pub := ak.newRoot()
	signature := body["signature"]
	body["signature"] = nil
	if r := e.sendJSON("PUT", "/api/recovery", admin.token, body); r.status != http.StatusBadRequest || r.errorCode() != "bad_signature" {
		t.Errorf("a new recovery key without the old one's signature: %d %s", r.status, r.body)
	}
	body["signature"] = signature
	keys := body["folder_keys"].([]map[string]any)
	body["folder_keys"] = []map[string]any{}
	if r := e.sendJSON("PUT", "/api/recovery", admin.token, body); r.status != http.StatusConflict || r.errorCode() != "key_outdated" {
		t.Errorf("a new recovery key that leaves out a folder's key: %d %s", r.status, r.body)
	}
	wrong := map[string]any{"folder": keys[0]["folder"], "version": keys[0]["version"], "signature": ak.sign(e2ee.FolderKeyMessage(family.ID, 1, ak.folderPubs[family.ID+":1"]))}
	body["folder_keys"] = []map[string]any{wrong}
	if r := e.sendJSON("PUT", "/api/recovery", admin.token, body); r.status != http.StatusBadRequest || r.errorCode() != "bad_signature" {
		t.Errorf("a folder's key signed by the old recovery key: %d %s", r.status, r.body)
	}
	body["folder_keys"] = keys
	if r := e.sendJSON("PUT", "/api/recovery", admin.token, body); r.status != http.StatusNoContent {
		t.Fatalf("a new recovery key: %d %s", r.status, r.body)
	}
	ak.root, ak.rootPub = priv, pub
	a = ak.sync()
	if len(a.Roots) != 2 || !e2ee.Verify(first, e2ee.RootMessage(pub), ak.unb64(*a.Roots[1].Signature)) {
		t.Fatalf("the chain: %+v", a.Roots)
	}
	if len(a.Todo.Recovery) != 1 || len(a.Todo.Roots) != 1 || a.Todo.Roots[0].User != eva.userID {
		t.Errorf("after a new recovery key, the old seals go: %+v", a.Todo)
	}
	if info := e.folderInfo(admin, trips.ID); !e2ee.Verify(pub, e2ee.PlainMessage(trips.ID, 0, trips.Name), ak.unb64(info["plain_signature"].(string))) {
		t.Error("the plain statement isn't signed anew")
	}
	rec := ak.recoveryAnswer()
	if got, err := e2ee.Unlock(e2ee.SecretKey(secret, e2ee.PurposeRecovery), e2ee.RecoveryContext, ak.unb64(*rec.Locked)); err != nil || !bytes.Equal(got, priv) {
		t.Errorf("the new code doesn't open the new key: %v", err)
	}
	if n := dbtest.Count(t, e.app.DB, "SELECT COUNT(*) FROM roots WHERE locked IS NOT NULL"); n != 1 {
		t.Errorf("%d roots keep their private key", n)
	}
	if ek.root = nil; ek.sync().RootSealed != nil {
		t.Error("Eva still holds a root")
	}

	// An admin who becomes a member loses the root.
	ak.work(ak.sync())
	if ek.rootPub = pub; ek.sync().RootSealed == nil {
		t.Fatal("Eva doesn't hold the new root")
	}
	if r := e.sendJSON("PATCH", "/api/users/"+eva.userID, admin.token, map[string]string{"role": db.RoleMember}); r.status != http.StatusNoContent {
		t.Fatalf("making Eva a member: %d %s", r.status, r.body)
	}
	if ek.sync().RootSealed != nil {
		t.Error("a member still holds the root")
	}
}

// Once there is a root, a folder comes with its id and the root's signature: its first key, or
// its plain statement; a folder that sends plain is renamed with a new one.
func TestFolderSignatures(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	e.firstFolder()
	ak := e.keyring(admin)
	ak.makePersonKey()

	// Before any root, a folder needs no signature, and takes none.
	r := e.postJSON(nil, "/api/folders", admin.token, map[string]any{"name": "Before"}, nil)
	if r.status != http.StatusCreated || r.json(t)["plain_signature"] != nil {
		t.Fatalf("a folder before the root: %d %s", r.status, r.body)
	}
	before := r.json(t)["id"].(string)
	ak.makeRoot()

	id := "0199a0c4-8f6e-7d2a-9b1c-3e5f7a9b1c3d"
	if r := e.postJSON(nil, "/api/folders", admin.token, map[string]any{"id": id, "name": "Garden"}, nil); r.status != http.StatusBadRequest || r.errorCode() != "bad_signature" {
		t.Errorf("a folder without a signature: %d %s", r.status, r.body)
	}
	if r := e.postJSON(nil, "/api/folders", admin.token, map[string]any{"id": id, "name": "Garden", "plain_signature": ak.plain(id, 0, "Garden ")}, nil); r.status != http.StatusBadRequest || r.errorCode() != "bad_signature" {
		t.Errorf("a plain statement for another name: %d %s", r.status, r.body)
	}
	if r := e.postJSON(nil, "/api/folders", admin.token, map[string]any{"id": "Garden", "name": "Garden"}, nil); r.status != http.StatusBadRequest {
		t.Errorf("an id that isn't one: %d %s", r.status, r.body)
	}
	r = e.postJSON(nil, "/api/folders", admin.token, map[string]any{"id": id, "name": "  Garden ", "plain_signature": ak.plain(id, 0, "Garden")}, nil)
	if r.status != http.StatusCreated || r.json(t)["id"] != id || r.json(t)["name"] != "Garden" || r.json(t)["plain_signature"] == nil {
		t.Fatalf("a plain folder: %d %s", r.status, r.body)
	}
	if r := e.postJSON(nil, "/api/folders", admin.token, map[string]any{"id": id, "name": "Garden 2", "plain_signature": ak.plain(id, 0, "Garden 2")}, nil); r.status != http.StatusConflict {
		t.Errorf("an id that is taken: %d %s", r.status, r.body)
	}

	// An encrypted one comes with its first key.
	vault := "0199a0c4-8f6e-7d2a-9b1c-3e5f7a9b1c3e"
	r = e.postJSON(nil, "/api/folders", admin.token, map[string]any{"id": vault, "name": "Vault", "key": ak.newFolderKey(vault, 1)}, nil)
	if r.status != http.StatusCreated || r.json(t)["encrypted"] != true || r.json(t)["key_version"] != 1.0 || r.json(t)["plain_signature"] != nil {
		t.Fatalf("an encrypted folder: %d %s", r.status, r.body)
	}
	if r := e.postJSON(nil, "/api/folders", admin.token, map[string]any{"name": "Vault 2", "key": ak.newFolderKey(vault, 1)}, nil); r.status != http.StatusBadRequest {
		t.Errorf("a key without the folder's id: %d %s", r.status, r.body)
	}

	// Renaming a plain folder signs it anew; an encrypted one needs nothing.
	rename := func(folder, name string, sig any) response {
		return e.sendJSON("PATCH", "/api/folders/"+folder, admin.token, map[string]any{"name": name, "plain_signature": sig})
	}
	if r := rename(id, "Allotment", nil); r.status != http.StatusBadRequest || r.errorCode() != "bad_signature" {
		t.Errorf("renaming a plain folder without a signature: %d %s", r.status, r.body)
	}
	if r := rename(id, "Allotment", ak.plain(id, 0, "Garden")); r.errorCode() != "bad_signature" {
		t.Errorf("renaming with the old name's statement: %d %s", r.status, r.body)
	}
	if r := rename(id, "Allotment", ak.plain(id, 0, "Allotment")); r.status != http.StatusOK || r.json(t)["name"] != "Allotment" {
		t.Errorf("renaming a plain folder: %d %s", r.status, r.body)
	}
	if r := rename(before, "Earlier", ak.plain(before, 0, "Earlier")); r.status != http.StatusOK {
		t.Errorf("renaming a folder from before the root: %d %s", r.status, r.body)
	}
	if r := rename(vault, "Safe", nil); r.status != http.StatusOK || r.json(t)["name"] != "Safe" {
		t.Errorf("renaming an encrypted folder: %d %s", r.status, r.body)
	}
}

// The person's note goes to their phones and browsers, and goes when they start over.
func TestNote(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	maria := e.accept(e.invite(admin, "Maria", db.RoleMember), "Maria's phone")
	mk := e.keyring(maria)
	note := map[string]string{"note": b64u.EncodeToString(randomBytes(t, 100))}
	if r := e.sendJSON("PUT", "/api/keys/note", maria.token, note); r.status != http.StatusConflict || r.errorCode() != "no_key" {
		t.Errorf("a note without a key: %d %s", r.status, r.body)
	}
	mk.makePersonKey()
	if r := e.sendJSON("PUT", "/api/keys/note", maria.token, map[string]string{"note": b64u.EncodeToString(randomBytes(t, e2ee.LockOverhead))}); r.status != http.StatusBadRequest {
		t.Errorf("an empty note: %d %s", r.status, r.body)
	}
	if r := e.sendJSON("PUT", "/api/keys/note", maria.token, note); r.status != http.StatusNoContent {
		t.Fatalf("note: %d %s", r.status, r.body)
	}
	if a := mk.sync(); a.Person.Note == nil || *a.Person.Note != note["note"] {
		t.Errorf("the note: %+v", a.Person)
	}
	priv, pub := e2ee.GenerateKey()
	sealed, _ := e2ee.Seal(mk.devicePub, e2ee.PurposePerson, e2ee.PersonContext(maria.userID), priv)
	if r := e.sendJSON("PUT", "/api/keys/person", maria.token, map[string]any{"public_key": b64u.EncodeToString(pub), "sealed": b64u.EncodeToString(sealed), "start_over": true}); r.status != http.StatusNoContent {
		t.Fatalf("start over: %d %s", r.status, r.body)
	}
	if a := mk.sync(); a.Person.Note != nil {
		t.Error("the note stays with a new person key")
	}
}
