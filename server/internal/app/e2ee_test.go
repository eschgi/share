package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/db/dbtest"
	"github.com/eschgi/share/server/internal/e2ee"
	"github.com/eschgi/share/server/internal/upload"
)

var b64u = base64.RawURLEncoding

// keyring is a phone's keys, as the app keeps them: its own, its person's once opened, and the
// folder keys it opened, by "<folder>:<version>".
type keyring struct {
	e          *env
	who        signedIn
	device     []byte
	devicePub  []byte
	person     []byte
	personPub  []byte
	folders    map[string][]byte
	folderPubs map[string][]byte
}

func deviceIDOf(s signedIn) string { return s.body["device"].(map[string]any)["id"].(string) }

// keyring sends a new device key for a signed-in phone.
func (e *env) keyring(s signedIn) *keyring {
	e.t.Helper()
	priv, pub := e2ee.GenerateKey()
	k := &keyring{e: e, who: s, device: priv, devicePub: pub, folders: map[string][]byte{}, folderPubs: map[string][]byte{}}
	if r := e.sendJSON("PUT", "/api/keys/device", s.token, map[string]string{"public_key": b64u.EncodeToString(pub)}); r.status != http.StatusNoContent {
		e.t.Fatalf("device key: %d %s", r.status, r.body)
	}
	return k
}

// makePersonKey makes the person's key on this phone.
func (k *keyring) makePersonKey() {
	k.e.t.Helper()
	k.person, k.personPub = e2ee.GenerateKey()
	sealed, err := e2ee.Seal(k.devicePub, e2ee.PurposePerson, e2ee.PersonContext(k.who.userID), k.person)
	if err != nil {
		k.e.t.Fatal(err)
	}
	r := k.e.sendJSON("PUT", "/api/keys/person", k.who.token, map[string]string{"public_key": b64u.EncodeToString(k.personPub), "sealed": b64u.EncodeToString(sealed)})
	if r.status != http.StatusNoContent {
		k.e.t.Fatalf("person key: %d %s", r.status, r.body)
	}
}

type keysAnswer struct {
	DeviceKey *string `json:"device_key"`
	Person    struct {
		PublicKey    *string `json:"public_key"`
		Sealed       *string `json:"sealed"`
		PasswordLock *string `json:"password_lock"`
		HeldBy       int     `json:"held_by"`
	} `json:"person"`
	Folders []struct {
		Folder    string  `json:"folder"`
		Version   int     `json:"version"`
		PublicKey string  `json:"public_key"`
		Sealed    *string `json:"sealed"`
	} `json:"folders"`
	RecoveryKey *string `json:"recovery_key"`
	Todo        struct {
		Devices []struct {
			ID        string `json:"id"`
			PublicKey string `json:"public_key"`
		} `json:"devices"`
		People []struct {
			Folder    string `json:"folder"`
			Version   int    `json:"version"`
			User      string `json:"user"`
			PublicKey string `json:"public_key"`
		} `json:"people"`
		Recovery []struct {
			Folder  string `json:"folder"`
			Version int    `json:"version"`
		} `json:"recovery"`
		Rekey []string `json:"rekey"`
		Pins  []struct {
			Pin           string `json:"pin"`
			Folder        string `json:"folder"`
			Version       int    `json:"version"`
			SecretVersion int    `json:"secret_version"`
			SecretSealed  string `json:"secret_sealed"`
		} `json:"pins"`
	} `json:"todo"`
}

func (k *keyring) unb64(s string) []byte {
	b, err := b64u.DecodeString(s)
	if err != nil {
		k.e.t.Fatal(err)
	}
	return b
}

// sync asks for the keys and opens what is sealed for this phone, as every start does.
func (k *keyring) sync() keysAnswer {
	k.e.t.Helper()
	r := k.e.get("/api/keys", k.who.token)
	if r.status != http.StatusOK {
		k.e.t.Fatalf("keys: %d %s", r.status, r.body)
	}
	var a keysAnswer
	if err := json.Unmarshal(r.body, &a); err != nil {
		k.e.t.Fatal(err)
	}
	if k.person == nil && a.Person.Sealed != nil {
		p, err := e2ee.Open(k.device, e2ee.PurposePerson, e2ee.PersonContext(k.who.userID), k.unb64(*a.Person.Sealed))
		if err != nil {
			k.e.t.Fatalf("opening the person key: %v", err)
		}
		k.person, k.personPub = p, k.unb64(*a.Person.PublicKey)
	}
	for _, f := range a.Folders {
		id := fmt.Sprintf("%s:%d", f.Folder, f.Version)
		k.folderPubs[id] = k.unb64(f.PublicKey)
		if f.Sealed == nil || k.person == nil {
			continue
		}
		priv, err := e2ee.Open(k.person, e2ee.PurposeFolder, e2ee.FolderContext(f.Folder, f.Version), k.unb64(*f.Sealed))
		if err != nil {
			k.e.t.Fatalf("opening %s: %v", id, err)
		}
		if pub, _ := e2ee.PublicKey(priv); !bytes.Equal(pub, k.folderPubs[id]) {
			k.e.t.Fatalf("%s: not the folder's key", id)
		}
		k.folders[id] = priv
	}
	return a
}

// work does what the to-do list asks, with the keys this phone holds, and sends it all.
func (k *keyring) work(a keysAnswer) {
	k.e.t.Helper()
	req := map[string][]map[string]any{"devices": {}, "people": {}, "recovery": {}, "pins": {}}
	seal := func(pub []byte, purpose string, aad, key []byte) string {
		s, err := e2ee.Seal(pub, purpose, aad, key)
		if err != nil {
			k.e.t.Fatal(err)
		}
		return b64u.EncodeToString(s)
	}
	for _, d := range a.Todo.Devices {
		req["devices"] = append(req["devices"], map[string]any{"device": d.ID, "sealed": seal(k.unb64(d.PublicKey), e2ee.PurposePerson, e2ee.PersonContext(k.who.userID), k.person)})
	}
	for _, n := range a.Todo.People {
		priv := k.folders[fmt.Sprintf("%s:%d", n.Folder, n.Version)]
		req["people"] = append(req["people"], map[string]any{"folder": n.Folder, "version": n.Version, "user": n.User,
			"sealed": seal(k.unb64(n.PublicKey), e2ee.PurposeFolder, e2ee.FolderContext(n.Folder, n.Version), priv)})
	}
	for _, v := range a.Todo.Recovery {
		priv := k.folders[fmt.Sprintf("%s:%d", v.Folder, v.Version)]
		req["recovery"] = append(req["recovery"], map[string]any{"folder": v.Folder, "version": v.Version,
			"sealed": seal(k.unb64(*a.RecoveryKey), e2ee.PurposeFolder, e2ee.FolderContext(v.Folder, v.Version), priv)})
	}
	for _, p := range a.Todo.Pins {
		holder := k.folders[fmt.Sprintf("%s:%d", p.Folder, p.SecretVersion)]
		secret, err := e2ee.Open(holder, e2ee.PurposePin, e2ee.FolderContext(p.Folder, p.SecretVersion), k.unb64(p.SecretSealed))
		if err != nil {
			k.e.t.Fatalf("opening a PIN's secret: %v", err)
		}
		locked := e2ee.Lock(e2ee.SecretKey(secret, e2ee.PurposePin), e2ee.FolderContext(p.Folder, p.Version), k.folders[fmt.Sprintf("%s:%d", p.Folder, p.Version)])
		req["pins"] = append(req["pins"], map[string]any{"pin": p.Pin, "version": p.Version, "locked": b64u.EncodeToString(locked)})
	}
	if r := k.e.postJSON(nil, "/api/keys/grants", k.who.token, req, nil); r.status != http.StatusNoContent {
		k.e.t.Fatalf("grants: %d %s", r.status, r.body)
	}
}

// newFolderKey makes a version of a folder's key on this phone: sealed for its person and
// for the recovery key.
func (k *keyring) newFolderKey(folderID string, version int, recoveryPub []byte) map[string]any {
	k.e.t.Helper()
	priv, pub := e2ee.GenerateKey()
	aad := e2ee.FolderContext(folderID, version)
	sealed, err := e2ee.Seal(k.personPub, e2ee.PurposeFolder, aad, priv)
	if err != nil {
		k.e.t.Fatal(err)
	}
	rs, err := e2ee.Seal(recoveryPub, e2ee.PurposeFolder, aad, priv)
	if err != nil {
		k.e.t.Fatal(err)
	}
	id := fmt.Sprintf("%s:%d", folderID, version)
	k.folders[id], k.folderPubs[id] = priv, pub
	return map[string]any{"public_key": b64u.EncodeToString(pub), "sealed": b64u.EncodeToString(sealed), "recovery_sealed": b64u.EncodeToString(rs)}
}

// recoveryKey makes the server's recovery key, as the first admin who encrypts does.
func (e *env) recoveryKey(admin signedIn) (code string, priv, pub []byte) {
	e.t.Helper()
	secret, code := e2ee.NewRecoveryCode()
	priv, pub = e2ee.GenerateKey()
	locked := e2ee.Lock(e2ee.SecretKey(secret, e2ee.PurposeRecovery), e2ee.RecoveryContext, priv)
	if r := e.sendJSON("PUT", "/api/recovery", admin.token, map[string]string{"public_key": b64u.EncodeToString(pub), "locked": b64u.EncodeToString(locked)}); r.status != http.StatusNoContent {
		e.t.Fatalf("recovery key: %d %s", r.status, r.body)
	}
	return code, priv, pub
}

// encrypt turns encryption on for a folder, with its key's first version.
func (k *keyring) encrypt(folderID string, recoveryPub []byte) response {
	k.e.t.Helper()
	return k.e.sendJSON("PUT", "/api/folders/"+folderID+"/encryption", k.who.token, map[string]any{"encrypted": true, "key": k.newFolderKey(folderID, 1, recoveryPub)})
}

// encrypted is a file as a device sends it into an encrypted folder: its key, header,
// sealed key and bytes.
type encrypted struct {
	key, header, sealed, data []byte
	version                   int
	plainSize                 int
}

func (k *keyring) encryptFile(folderID string, version int, plain []byte) encrypted {
	k.e.t.Helper()
	key := make([]byte, e2ee.FileKeySize)
	copy(key, randomBytes(k.e.t, e2ee.FileKeySize))
	h := e2ee.NewHeader()
	sealed, err := e2ee.Seal(k.folderPubs[fmt.Sprintf("%s:%d", folderID, version)], e2ee.PurposeFile, e2ee.FolderContext(folderID, version), key)
	if err != nil {
		k.e.t.Fatal(err)
	}
	return encrypted{key: key, header: h[:], sealed: sealed, data: e2ee.Encrypt(key, h, plain), version: version, plainSize: len(plain)}
}

func (f encrypted) enc() map[string]any {
	return map[string]any{"version": f.version, "key": b64u.EncodeToString(f.sealed), "header": b64u.EncodeToString(f.header), "plain_size": f.plainSize}
}

// tusEncrypted starts a tus upload of an encrypted file into a folder.
func (k *keyring) tusEncrypted(folderID, name string, f encrypted) response {
	enc, _ := json.Marshal(f.enc())
	meta := "filename " + base64.StdEncoding.EncodeToString([]byte(name)) + ",folder " + base64.StdEncoding.EncodeToString([]byte(folderID)) +
		",enc " + base64.StdEncoding.EncodeToString(enc)
	return k.e.do(nil, "POST", "/tus/", k.who.token, nil, tus{e: k.e, token: k.who.token}.headers(map[string]string{
		"Upload-Length": strconv.Itoa(len(f.data)), "Upload-Metadata": meta,
	}))
}

// sendEncrypted sends a file into an encrypted folder over tus, whole.
func (k *keyring) sendEncrypted(folderID, name string, plain []byte) (string, encrypted) {
	k.e.t.Helper()
	k.sync()
	version := 0
	for id := range k.folderPubs {
		var f string
		var v int
		fmt.Sscanf(id[len(folderID):], ":%d", &v)
		if f = id[:len(folderID)]; f == folderID && v > version {
			version = v
		}
	}
	f := k.encryptFile(folderID, version, plain)
	r := k.tusEncrypted(folderID, name, f)
	if r.status != http.StatusCreated {
		k.e.t.Fatalf("create: %d %s", r.status, r.body)
	}
	loc := r.header.Get("Location")
	tus{e: k.e, token: k.who.token}.send(loc, f.data, 0, 1<<20)
	return idOf(loc), f
}

// An encrypted folder from the start: a person's keys, the recovery key, the folder's key
// sealed for the admin and the recovery key, a member who gets it from the admin's to-do
// list, and a file the server only ever sees encrypted.
func TestEncryptedFolder(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	family := e.firstFolder()
	ak := e.keyring(admin)
	a := ak.sync()
	assertShape(t, "keys", readFixture(t, "api/keys.json")["response"], e.get("/api/keys", admin.token).json(t))
	if a.Person.PublicKey != nil || len(a.Folders) != 0 || a.RecoveryKey != nil {
		t.Fatalf("a new person's keys: %+v", a)
	}
	if r := e.sendJSON("PUT", "/api/keys/person", admin.token, map[string]string{"public_key": "AAAA", "sealed": "AAAA"}); r.status != http.StatusBadRequest {
		t.Errorf("a broken key: %d %s", r.status, r.body)
	}
	ak.makePersonKey()
	if r := e.sendJSON("PUT", "/api/keys/person", admin.token, map[string]string{"public_key": b64u.EncodeToString(ak.personPub),
		"sealed": b64u.EncodeToString(make([]byte, 113))}); r.status != http.StatusConflict || r.errorCode() != "key_exists" {
		t.Errorf("a second person key: %d %s", r.status, r.body)
	}
	ak.person = nil
	if ak.sync(); ak.person == nil {
		t.Fatal("the person key isn't sealed for the phone")
	}

	// The first folder to encrypt needs the recovery key.
	if r := ak.encrypt(family.ID, ak.devicePub); r.status != http.StatusConflict || r.errorCode() != "no_key" {
		t.Errorf("encrypting without a recovery key: %d %s", r.status, r.body)
	}
	_, recoveryPriv, recoveryPub := e.recoveryKey(admin)
	r := ak.encrypt(family.ID, recoveryPub)
	if r.status != http.StatusOK || r.json(t)["encrypted"] != true || r.json(t)["key_version"] != 1.0 {
		t.Fatalf("encrypting: %d %s", r.status, r.body)
	}
	assertShape(t, "encryption", readFixture(t, "api/folder_encryption.json")["response"], r.json(t))
	if r := ak.encrypt(family.ID, recoveryPub); r.status != http.StatusConflict || r.errorCode() != "key_exists" {
		t.Errorf("a second first key: %d %s", r.status, r.body)
	}
	if a := ak.sync(); len(ak.folders) != 1 || len(a.Todo.People) != 0 || len(a.Todo.Recovery) != 0 {
		t.Fatalf("after encrypting: %+v", a)
	}

	// The recovery key opens the folder key too.
	rec := e.get("/api/recovery", admin.token)
	assertShape(t, "recovery", readFixture(t, "api/recovery.json")["response"], rec.json(t))
	var recAnswer struct {
		Folders []struct {
			Folder  string `json:"folder"`
			Version int    `json:"version"`
			Sealed  string `json:"sealed"`
		} `json:"folders"`
	}
	json.Unmarshal(rec.body, &recAnswer)
	if len(recAnswer.Folders) != 1 {
		t.Fatalf("recovery: %s", rec.body)
	}
	if priv, err := e2ee.Open(recoveryPriv, e2ee.PurposeFolder, e2ee.FolderContext(family.ID, 1), ak.unb64(recAnswer.Folders[0].Sealed)); err != nil || !bytes.Equal(priv, ak.folders[family.ID+":1"]) {
		t.Errorf("the recovery key doesn't open the folder key: %v", err)
	}

	// Maria joins; the admin's phone finds her on its to-do list and seals the folder key.
	maria := e.accept(e.invite(admin, "Maria", db.RoleMember), "Maria's phone")
	mk := e.keyring(maria)
	mk.makePersonKey()
	a = ak.sync()
	if len(a.Todo.People) != 1 || a.Todo.People[0].User != maria.userID {
		t.Fatalf("the admin's to-do list: %+v", a.Todo)
	}
	ak.work(a)
	if mk.sync(); len(mk.folders) != 1 {
		t.Fatal("Maria can't open the folder")
	}
	if a := ak.sync(); len(a.Todo.People) != 0 {
		t.Errorf("still to do: %+v", a.Todo)
	}

	// Maria's new browser gets her key from her phone.
	phone := e.postJSON(nil, "/api/users/"+maria.userID+"/invites", admin.token, map[string]any{}, nil)
	browser := e.accept(phone.json(t)["token"].(string), "Maria's browser")
	bk := e.keyring(browser)
	if a := mk.sync(); len(a.Todo.Devices) != 1 || a.Todo.Devices[0].ID != deviceIDOf(browser) {
		t.Fatalf("Maria's to-do list: %+v", a.Todo)
	} else {
		mk.work(a)
	}
	if bk.sync(); bk.person == nil || len(bk.folders) != 1 {
		t.Fatal("Maria's browser can't open her keys")
	}

	// A file sent into the folder is stored encrypted, and listed with what opens it.
	plain := randomBytes(t, 3*e2ee.ChunkSize+1000)
	id, sent := mk.sendEncrypted(family.ID, "IMG_0001.jpg", plain)
	f := e.file(id)
	if f.State != db.StateReady || f.Enc == nil || f.Size != int64(len(sent.data)) || f.Enc.PlainSize != int64(len(plain)) || f.Mime != "image/jpeg" || f.Kind != db.KindPhoto {
		t.Fatalf("the file: %+v", f)
	}
	stored, err := os.ReadFile(e.disk(f))
	if err != nil || !bytes.Equal(stored, sent.data) {
		t.Fatalf("the stored bytes aren't what was sent: %v", err)
	}
	info := e.get("/api/files/"+id, admin.token)
	assertShape(t, "encrypted file", readFixture(t, "api/file_encrypted.json")["response"], info.json(t))
	var fi struct {
		Size int64 `json:"size"`
		Enc  struct {
			Version int    `json:"version"`
			Key     string `json:"key"`
			Header  string `json:"header"`
		} `json:"enc"`
	}
	json.Unmarshal(info.body, &fi)
	fileKey, err := e2ee.Open(ak.folders[fmt.Sprintf("%s:%d", family.ID, fi.Enc.Version)], e2ee.PurposeFile, e2ee.FolderContext(family.ID, fi.Enc.Version), ak.unb64(fi.Enc.Key))
	if err != nil || fi.Size != int64(len(plain)) {
		t.Fatalf("opening the file key: %v, size %d", err, fi.Size)
	}
	content := e.do(nil, "GET", "/api/files/"+id+"/content", admin.token, nil, nil)
	if got, err := e2ee.Decrypt(fileKey, content.body, int64(len(plain))); err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("decrypting what the server sends: %v", err)
	}
	if ct := content.header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("content type %q", ct)
	}

	// Its thumbnail comes sealed, and goes out as it came.
	if r := e.putThumb(maria.token, id, "?width=4032&height=3024", "image/jpeg", jpegBytes(t, 64, 48, 1)); r.status != http.StatusBadRequest || r.errorCode() != "bad_thumbnail" {
		t.Errorf("a plain thumbnail of an encrypted file: %d %s", r.status, r.body)
	}
	sealedThumb := e2ee.SealThumb(sent.key, jpegBytes(t, 64, 48, 1))
	if r := e.putThumb(maria.token, id, "?width=4032&height=3024", "application/octet-stream", sealedThumb); r.status != http.StatusNoContent {
		t.Fatalf("a sealed thumbnail: %d %s", r.status, r.body)
	}
	thumb := e.do(nil, "GET", "/api/files/"+id+"/thumb", admin.token, nil, nil)
	if !bytes.Equal(thumb.body, sealedThumb) || thumb.header.Get("Content-Type") != "application/octet-stream" {
		t.Errorf("the thumbnail: %d %q", thumb.status, thumb.header.Get("Content-Type"))
	}
	plainFile := e.put(family, "notes.txt", []byte("plain"))
	if r := e.putThumb(admin.token, plainFile.ID, "", "application/octet-stream", sealedThumb); r.status != http.StatusBadRequest {
		t.Errorf("a sealed thumbnail of a plain file: %d %s", r.status, r.body)
	}

	// The server leaves it out of its thumbnails, checksums and ZIPs.
	e.clock.Add(time.Hour)
	if photos, _ := e.app.DB.ThumbCandidates(context.Background(), e.clock.Now(), 10); len(photos) != 0 {
		t.Errorf("thumbnail candidates: %d", len(photos))
	}
	if files, _ := e.app.DB.CRCCandidates(context.Background(), 10); len(files) != 1 || files[0].ID != plainFile.ID {
		t.Errorf("checksum candidates: %+v", files)
	}
	z := e.askZip(admin.token, id, plainFile.ID)
	var zip struct {
		Count int `json:"count"`
		Files []struct {
			ID   string `json:"id"`
			Size int64  `json:"size"`
		} `json:"files"`
	}
	json.Unmarshal(z.body, &zip)
	if z.status != http.StatusCreated || zip.Count != 1 || len(zip.Files) != 2 {
		t.Fatalf("zip: %d %s", z.status, z.body)
	}
	for _, zf := range zip.Files {
		if zf.ID == id && zf.Size != int64(len(plain)) {
			t.Errorf("the encrypted file's size for saving: %d", zf.Size)
		}
	}
}

// Uploads into an encrypted folder must be encrypted, for the newest key; a folder that was
// never encrypted takes no encrypted ones; the bytes must start with their header.
func TestEncryptedUploadsAreChecked(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	family := e.firstFolder()
	plainFolder := e.newFolder("Plain")
	ak := e.keyring(admin)
	ak.makePersonKey()
	_, _, recoveryPub := e.recoveryKey(admin)
	if r := ak.encrypt(family.ID, recoveryPub); r.status != http.StatusOK {
		t.Fatalf("encrypting: %d %s", r.status, r.body)
	}
	c := tus{e: e, token: admin.token}
	if r := c.createWith("plain.jpg", 100, family.ID); r.status != http.StatusConflict || r.errorCode() != "encryption_required" {
		t.Errorf("a plain upload: %d %s", r.status, r.body)
	}
	f := ak.encryptFile(family.ID, 1, []byte("photo"))
	if r := ak.tusEncrypted(plainFolder.ID, "a.jpg", f); r.status != http.StatusConflict || r.errorCode() != "not_encrypted" {
		t.Errorf("into a plain folder: %d %s", r.status, r.body)
	}
	old := f
	old.version = 2
	if r := ak.tusEncrypted(family.ID, "a.jpg", old); r.status != http.StatusConflict || r.errorCode() != "key_outdated" {
		t.Errorf("for another version: %d %s", r.status, r.body)
	}
	short := f
	short.data = f.data[:len(f.data)-1]
	if r := ak.tusEncrypted(family.ID, "a.jpg", short); r.status != http.StatusBadRequest {
		t.Errorf("a size that doesn't match: %d %s", r.status, r.body)
	}
	// A header that isn't the one the bytes start with: the upload never finishes.
	wrong := f
	h := e2ee.NewHeader()
	wrong.header = h[:]
	r := ak.tusEncrypted(family.ID, "a.jpg", wrong)
	if r.status != http.StatusCreated {
		t.Fatalf("create: %d %s", r.status, r.body)
	}
	if r := c.patch(r.header.Get("Location"), 0, wrong.data); r.status != http.StatusInternalServerError || r.errorCode() != "finalize_failed" {
		t.Errorf("bytes with another header: %d %s", r.status, r.body)
	}
	// Turned off, the folder takes plain uploads again; encrypted ones still go in.
	if r := e.sendJSON("PUT", "/api/folders/"+family.ID+"/encryption", admin.token, map[string]any{"encrypted": false}); r.status != http.StatusOK || r.json(t)["encrypted"] != false {
		t.Fatalf("turning it off: %d %s", r.status, r.body)
	}
	if r := c.createWith("plain.jpg", 100, family.ID); r.status != http.StatusCreated {
		t.Errorf("a plain upload after turning it off: %d %s", r.status, r.body)
	}
	if r := ak.tusEncrypted(family.ID, "b.jpg", ak.encryptFile(family.ID, 1, []byte("photo"))); r.status != http.StatusCreated {
		t.Errorf("an encrypted upload after turning it off: %d %s", r.status, r.body)
	}
}

// Someone who loses a folder loses what was sealed for them; the next phone with the key
// makes a new version, which new files use.
func TestLosingAFolderMakesANewKey(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	family := e.firstFolder()
	ak := e.keyring(admin)
	ak.makePersonKey()
	_, _, recoveryPub := e.recoveryKey(admin)
	ak.encrypt(family.ID, recoveryPub)
	maria := e.accept(e.invite(admin, "Maria", db.RoleMember), "Maria's phone")
	mk := e.keyring(maria)
	mk.makePersonKey()
	ak.work(ak.sync())
	if mk.sync(); len(mk.folders) != 1 {
		t.Fatal("Maria has no key")
	}
	if r := e.do(nil, "DELETE", "/api/folders/"+family.ID+"/people/"+maria.userID, admin.token, nil, nil); r.status != http.StatusNoContent {
		t.Fatalf("taking the folder away: %d %s", r.status, r.body)
	}
	if a := mk.sync(); len(a.Folders) != 0 {
		t.Errorf("Maria still gets the folder's keys: %+v", a.Folders)
	}
	a := ak.sync()
	if len(a.Todo.Rekey) != 1 || a.Todo.Rekey[0] != family.ID {
		t.Fatalf("the admin's to-do list: %+v", a.Todo)
	}
	next := ak.newFolderKey(family.ID, 2, recoveryPub)
	next["version"] = 3
	if r := e.postJSON(nil, "/api/folders/"+family.ID+"/keys", admin.token, next, nil); r.status != http.StatusConflict || r.errorCode() != "key_outdated" {
		t.Errorf("skipping a version: %d %s", r.status, r.body)
	}
	next["version"] = 2
	if r := e.postJSON(nil, "/api/folders/"+family.ID+"/keys", maria.token, next, nil); r.status != http.StatusNotFound {
		t.Errorf("a new key from someone who lost the folder: %d %s", r.status, r.body)
	}
	r := e.postJSON(nil, "/api/folders/"+family.ID+"/keys", admin.token, next, nil)
	if r.status != http.StatusNoContent {
		t.Fatalf("the new version: %d %s", r.status, r.body)
	}
	if a := ak.sync(); len(a.Todo.Rekey) != 0 || len(ak.folders) != 2 {
		t.Errorf("after the new version: %+v", a.Todo)
	}
	if r := ak.tusEncrypted(family.ID, "a.jpg", ak.encryptFile(family.ID, 1, []byte("photo"))); r.errorCode() != "key_outdated" {
		t.Errorf("an upload for the old version: %d %s", r.status, r.body)
	}
	if _, f := ak.sendEncrypted(family.ID, "b.jpg", []byte("photo")); f.version != 2 {
		t.Errorf("sent for version %d", f.version)
	}
}

// Moving encrypted files needs their keys sealed for the target folder's newest key.
func TestMovingEncryptedFiles(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	family := e.firstFolder()
	trips := e.newFolder("Trips")
	plainFolder := e.newFolder("Plain")
	ak := e.keyring(admin)
	ak.makePersonKey()
	_, _, recoveryPub := e.recoveryKey(admin)
	ak.encrypt(family.ID, recoveryPub)
	ak.encrypt(trips.ID, recoveryPub)
	id, sent := ak.sendEncrypted(family.ID, "IMG.jpg", []byte("photo"))
	move := func(folder string, keys []map[string]any) response {
		return e.postJSON(nil, "/api/files/move", admin.token, map[string]any{"ids": []string{id}, "folder": folder, "keys": keys}, nil)
	}
	if r := move(plainFolder.ID, nil); r.status != http.StatusConflict || r.errorCode() != "not_encrypted" {
		t.Errorf("into a plain folder: %d %s", r.status, r.body)
	}
	if r := move(trips.ID, nil); r.status != http.StatusConflict || r.errorCode() != "key_outdated" {
		t.Errorf("without keys: %d %s", r.status, r.body)
	}
	resealed, err := e2ee.Seal(ak.folderPubs[trips.ID+":1"], e2ee.PurposeFile, e2ee.FolderContext(trips.ID, 1), sent.key)
	if err != nil {
		t.Fatal(err)
	}
	if r := move(trips.ID, []map[string]any{{"id": id, "version": 1, "key": b64u.EncodeToString(resealed)}}); r.status != http.StatusOK {
		t.Fatalf("moving: %d %s", r.status, r.body)
	}
	f := e.file(id)
	if f.FolderID != trips.ID || !bytes.Equal(f.Enc.Key, resealed) || !bytes.Equal(f.Enc.Header, sent.header) {
		t.Errorf("after the move: %+v", f)
	}
}

// Invites and PINs carry keys locked with their link's secret, which the server never sees.
func TestInvitesAndPinsWithSecrets(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	family := e.firstFolder()
	ak := e.keyring(admin)
	ak.makePersonKey()
	_, _, recoveryPub := e.recoveryKey(admin)
	ak.encrypt(family.ID, recoveryPub)

	// An invite for a new member brings the folder key.
	secret := randomBytes(t, 32)
	locked := e2ee.Lock(e2ee.SecretKey(secret, e2ee.PurposeInvite), e2ee.FolderContext(family.ID, 1), ak.folders[family.ID+":1"])
	r := e.postJSON(nil, "/api/invites", admin.token, map[string]any{"name": "Maria", "role": "member", "folders": []string{family.ID},
		"keys": []map[string]any{{"folder": family.ID, "version": 1, "locked": b64u.EncodeToString(locked)}}}, nil)
	if r.status != http.StatusCreated {
		t.Fatalf("invite: %d %s", r.status, r.body)
	}
	token := r.json(t)["token"].(string)
	maria := e.accept(token, "Maria's phone")
	keys := maria.body["keys"].([]any)
	if len(keys) != 1 {
		t.Fatalf("the invite's keys: %v", maria.body["keys"])
	}
	k := keys[0].(map[string]any)
	got, err := e2ee.Unlock(e2ee.SecretKey(secret, e2ee.PurposeInvite), e2ee.FolderContext(family.ID, 1), ak.unb64(k["locked"].(string)))
	if err != nil || !bytes.Equal(got, ak.folders[family.ID+":1"]) || k["public_key"] != b64u.EncodeToString(ak.folderPubs[family.ID+":1"]) {
		t.Fatalf("unlocking the invite's key: %v", err)
	}
	if left := dbtest.Count(t, e.app.DB, "SELECT (SELECT COUNT(*) FROM invite_keys) + (SELECT COUNT(*) FROM invites WHERE person_key IS NOT NULL)"); left != 0 {
		t.Errorf("%d invite keys left after accepting", left)
	}

	// A PIN that shows the folder: its secret sealed for the folder key, the key locked with it.
	pinSecret := randomBytes(t, 32)
	secretSealed, _ := e2ee.Seal(ak.folderPubs[family.ID+":1"], e2ee.PurposePin, e2ee.FolderContext(family.ID, 1), pinSecret)
	pinLocked := e2ee.Lock(e2ee.SecretKey(pinSecret, e2ee.PurposePin), e2ee.FolderContext(family.ID, 1), ak.folders[family.ID+":1"])
	pinReq := map[string]any{"kind": "day", "folder": family.ID, "shows_folder": true, "secret": map[string]any{
		"sealed": b64u.EncodeToString(secretSealed), "version": 1, "keys": []map[string]any{{"version": 1, "locked": b64u.EncodeToString(pinLocked)}}}}
	r = e.postJSON(nil, "/api/pins", admin.token, pinReq, nil)
	if r.status != http.StatusCreated {
		t.Fatalf("PIN: %d %s", r.status, r.body)
	}
	code := r.json(t)["code"].(string)
	guest, session := e.unlockApp(code, "")
	enc := session["session"].(map[string]any)["encrypt"].(map[string]any)
	if enc["version"] != 1.0 || enc["public_key"] != b64u.EncodeToString(ak.folderPubs[family.ID+":1"]) {
		t.Errorf("the session's key: %v", enc)
	}
	pk := e.get("/api/pin/keys", guest)
	assertShape(t, "pin keys", readFixture(t, "api/pin_keys.json")["response"], pk.json(t))
	if keys := pk.json(t)["keys"].([]any); len(keys) != 1 {
		t.Fatalf("the PIN's keys: %s", pk.body)
	}

	// After a new version, the admin's phone locks it for the PIN's link too.
	e.do(nil, "DELETE", "/api/folders/"+family.ID+"/people/"+maria.userID, admin.token, nil, nil)
	next := ak.newFolderKey(family.ID, 2, recoveryPub)
	next["version"] = 2
	if r := e.postJSON(nil, "/api/folders/"+family.ID+"/keys", admin.token, next, nil); r.status != http.StatusNoContent {
		t.Fatalf("the new version: %d %s", r.status, r.body)
	}
	a := ak.sync()
	if len(a.Todo.Pins) != 1 || a.Todo.Pins[0].Version != 2 {
		t.Fatalf("the PIN on the to-do list: %+v", a.Todo.Pins)
	}
	ak.work(a)
	if keys := e.get("/api/pin/keys", guest).json(t)["keys"].([]any); len(keys) != 2 {
		t.Errorf("the PIN's keys after the new version: %d", len(keys))
	}
	if a := ak.sync(); len(a.Todo.Pins) != 0 {
		t.Errorf("still to do: %+v", a.Todo.Pins)
	}

	// A PIN that only sends has no keys to read with.
	sender := e.newPin(db.PinDay)
	plainGuest, _ := e.unlockApp(sender.Code, "")
	if r := e.get("/api/pin/keys", plainGuest); r.status != http.StatusForbidden {
		t.Errorf("a sending PIN's keys: %d %s", r.status, r.body)
	}
}

// The password lock goes with password changes, and an admin's reset drops it.
func TestPasswordLock(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	maria := e.accept(e.invite(admin, "Maria", db.RoleMember), "Maria's phone")
	mk := e.keyring(maria)
	lock := e2ee.PasswordLock("correct horse", e2ee.PersonContext(maria.userID), make([]byte, e2ee.PrivateKeySize))
	if r := e.sendJSON("PUT", "/api/keys/password-lock", maria.token, map[string]string{"password_lock": b64u.EncodeToString(lock)}); r.status != http.StatusConflict || r.errorCode() != "no_key" {
		t.Errorf("a lock without a key: %d %s", r.status, r.body)
	}
	mk.makePersonKey()
	lock = e2ee.PasswordLock("correct horse", e2ee.PersonContext(maria.userID), mk.person)
	r := e.sendJSON("PUT", "/api/me/password", maria.token, map[string]string{"username": "maria", "password": "correct horse", "password_lock": b64u.EncodeToString(lock)})
	if r.status != http.StatusNoContent {
		t.Fatalf("password: %d %s", r.status, r.body)
	}
	a := mk.sync()
	if a.Person.PasswordLock == nil {
		t.Fatal("no password lock")
	}
	if got, err := e2ee.PasswordUnlock("correct horse", e2ee.PersonContext(maria.userID), mk.unb64(*a.Person.PasswordLock)); err != nil || !bytes.Equal(got, mk.person) {
		t.Errorf("unlocking: %v", err)
	}
	if r := e.postJSON(nil, "/api/users/"+maria.userID+"/password", admin.token, map[string]any{}, nil); r.status != http.StatusOK {
		t.Fatalf("reset: %d %s", r.status, r.body)
	}
	if a := mk.sync(); a.Person.PasswordLock != nil {
		t.Error("the lock with the forgotten password stays")
	}
	// Starting over drops what was sealed for the old key.
	mk2priv, mk2pub := e2ee.GenerateKey()
	sealed, _ := e2ee.Seal(mk.devicePub, e2ee.PurposePerson, e2ee.PersonContext(maria.userID), mk2priv)
	if r := e.sendJSON("PUT", "/api/keys/person", maria.token, map[string]any{"public_key": b64u.EncodeToString(mk2pub), "sealed": b64u.EncodeToString(sealed), "start_over": true}); r.status != http.StatusNoContent {
		t.Fatalf("start over: %d %s", r.status, r.body)
	}
	if a := mk.sync(); *a.Person.PublicKey != b64u.EncodeToString(mk2pub) {
		t.Error("the new person key isn't there")
	}
}

// held_by says how many of a person's phones and browsers hold their key. A laptop where Maria
// signs in with a password an admin gave her waits while her phone holds the key, and finds it
// lost once the phone is signed out: held by none, and no password lock.
func TestHeldBy(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	maria := e.accept(e.invite(admin, "Maria", db.RoleMember), "Maria's phone")
	mk := e.keyring(maria)
	mk.makePersonKey()
	if a := mk.sync(); a.Person.HeldBy != 1 {
		t.Errorf("held by %d after the phone made it", a.Person.HeldBy)
	}
	r := e.postJSON(nil, "/api/users/"+maria.userID+"/password", admin.token, map[string]any{"username": "maria"}, nil)
	if r.status != http.StatusOK {
		t.Fatalf("new password: %d %s", r.status, r.body)
	}
	np := r.json(t)
	r = e.postJSON(nil, "/api/auth/login", "", map[string]string{"username": np["username"].(string), "password": np["password"].(string), "device_name": "Laptop"}, nil)
	if r.status != http.StatusOK {
		t.Fatalf("login: %d %s", r.status, r.body)
	}
	v := r.json(t)
	laptop := e.keyring(signedIn{v["token"].(string), maria.userID, v})
	if a := laptop.sync(); a.Person.Sealed != nil || a.Person.PasswordLock != nil || a.Person.HeldBy != 1 {
		t.Errorf("the laptop waits for the phone: %+v", a.Person)
	}
	if r := e.postJSON(nil, "/api/auth/logout", maria.token, map[string]any{}, nil); r.status != http.StatusNoContent {
		t.Fatalf("logout: %d %s", r.status, r.body)
	}
	if a := laptop.sync(); a.Person.HeldBy != 0 || a.Person.PasswordLock != nil {
		t.Errorf("with the phone signed out, the key is lost: %+v", a.Person)
	}
}

func TestEncryptionSettings(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	r := e.get("/api/admin/settings", admin.token)
	assertShape(t, "settings", readFixture(t, "api/settings.json")["response"], r.json(t))
	if r.json(t)["new_folders_encrypted"] != false {
		t.Errorf("by default: %s", r.body)
	}
	if r := e.sendJSON("PUT", "/api/admin/settings", admin.token, map[string]bool{"new_folders_encrypted": true}); r.status != http.StatusOK || r.json(t)["new_folders_encrypted"] != true {
		t.Errorf("turning it on: %d %s", r.status, r.body)
	}
	maria := e.accept(e.invite(admin, "Maria", db.RoleMember), "Maria's phone")
	if r := e.get("/api/admin/settings", maria.token); r.status != http.StatusForbidden {
		t.Errorf("a member: %d", r.status)
	}
	if r := e.get("/api/recovery", maria.token); r.status != http.StatusForbidden {
		t.Errorf("a member asking for the recovery key: %d", r.status)
	}
}

// The contract's fixtures for the keys are what the server takes.
func TestKeyFixtures(t *testing.T) {
	for _, name := range []string{"keys_device", "keys_person", "keys_password_lock", "keys_grants", "recovery_put", "settings_put"} {
		fx := readFixture(t, "api/"+name+".json")
		if _, ok := fx["request"]; !ok {
			t.Errorf("%s: no request", name)
		}
	}
}

// In a bucket, an encrypted upload's parts are pieces of the encrypted stream, and the object
// must start with its header.
func TestEncryptedUploadToABucket(t *testing.T) {
	e := newS3Env(t)
	admin := e.admin()
	family := e.firstFolder()
	ak := e.keyring(admin)
	ak.makePersonKey()
	_, _, recoveryPub := e.recoveryKey(admin)
	if r := ak.encrypt(family.ID, recoveryPub); r.status != http.StatusOK {
		t.Fatalf("encrypting: %d %s", r.status, r.body)
	}
	phone := s3Client{e: e, token: admin.token}
	if r := phone.create("plain.jpg", 10, family.ID); r.status != http.StatusConflict || r.errorCode() != "encryption_required" {
		t.Errorf("a plain upload: %d %s", r.status, r.body)
	}
	ak.sync()
	plain := randomBytes(t, int(2*e.part))
	f := ak.encryptFile(family.ID, 1, plain)
	r := phone.post("/api/s3/uploads", map[string]any{"name": "VID_0412.mp4", "size": len(f.data), "last_modified_ms": 1758960000000, "folder": family.ID, "enc": f.enc()})
	if r.status != http.StatusCreated {
		t.Fatalf("create: %d %s", r.status, r.body)
	}
	assertShape(t, "encrypted S3 upload", readFixture(t, "api/s3_upload_create_encrypted.json")["response"], r.json(t))
	var plan struct {
		ID       string `json:"id"`
		PartSize int64  `json:"part_size"`
		Parts    int    `json:"parts"`
	}
	json.Unmarshal(r.body, &plan)
	if want := (int64(len(f.data)) + plan.PartSize - 1) / plan.PartSize; int64(plan.Parts) != want {
		t.Fatalf("%d parts for %d bytes", plan.Parts, len(f.data))
	}
	var full upload.S3Upload
	json.Unmarshal(r.body, &full)
	numbers := make([]int, plan.Parts)
	for i := range numbers {
		numbers[i] = i + 1
	}
	phone.sendParts(full, f.data, numbers...)
	if r := phone.complete(plan.ID); r.status != http.StatusOK {
		t.Fatalf("complete: %d %s", r.status, r.body)
	}
	got := e.file(plan.ID)
	if got.Enc == nil || got.Kind != db.KindVideo || got.Mime != "video/mp4" || !bytes.Equal(e.s3Object(got), f.data) {
		t.Errorf("in the bucket: %+v", got)
	}
}
