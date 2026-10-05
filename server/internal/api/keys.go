package api

// The keys of end-to-end encryption (docs/e2ee-plan.md): what a phone or browser needs to open
// what its person may read and what it can seal for others, a folder's switch and key versions,
// and the recovery key. The server checks sizes and who may give which key to whom; it can't
// open any of them.

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/e2ee"
	"github.com/eschgi/share/server/internal/httpx"
)

// B64 is binary data in JSON: base64url without padding, null when there is none.
type B64 []byte

func (b B64) MarshalJSON() ([]byte, error) {
	if b == nil {
		return []byte("null"), nil
	}
	return json.Marshal(base64.RawURLEncoding.EncodeToString(b))
}

func (b *B64) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*b = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	v, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return fmt.Errorf("not base64url without padding: %w", err)
	}
	*b = v
	return nil
}

// Sizes of what clients send: a private key sealed for a public key, and one locked with a
// link's secret.
const (
	sealedKeySize = e2ee.PrivateKeySize + e2ee.SealOverhead
	lockedKeySize = e2ee.PrivateKeySize + e2ee.LockOverhead
)

func (a *API) registerKeys(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/keys", a.keys)
	mux.HandleFunc("PUT /api/keys/device", a.putDeviceKey)
	mux.HandleFunc("PUT /api/keys/person", a.putPersonKey)
	mux.HandleFunc("PUT /api/keys/password-lock", a.putPasswordLock)
	mux.HandleFunc("POST /api/keys/grants", a.grants)
	mux.HandleFunc("PUT /api/folders/{id}/encryption", a.folderEncryption)
	mux.HandleFunc("POST /api/folders/{id}/keys", a.newFolderKey)
	mux.HandleFunc("GET /api/recovery", a.recovery)
	mux.HandleFunc("PUT /api/recovery", a.putRecovery)
	mux.HandleFunc("GET /api/admin/settings", a.settings)
	mux.HandleFunc("PUT /api/admin/settings", a.putSettings)
	mux.HandleFunc("GET /api/pin/keys", a.pinKeys)
}

// Keys is what GET /api/keys answers.
type Keys struct {
	DeviceKey   B64         `json:"device_key"` // this device's public key, once it sent one
	Person      PersonKey   `json:"person"`
	Folders     []FolderKey `json:"folders"`      // every version of the keys of the folders the person sees
	RecoveryKey B64         `json:"recovery_key"` // the recovery key's public key, once there is one
	Todo        KeysTodo    `json:"todo"`
}

// PersonKey is the person's key: public, sealed for this device, locked with the password.
type PersonKey struct {
	PublicKey    B64 `json:"public_key"`
	Sealed       B64 `json:"sealed"`
	PasswordLock B64 `json:"password_lock"`
}

// FolderKey is a version of a folder's key, with its private key sealed for the person, or
// null while nobody has sealed it for them yet.
type FolderKey struct {
	Folder    string `json:"folder"`
	Version   int    `json:"version"`
	PublicKey B64    `json:"public_key"`
	Sealed    B64    `json:"sealed"`
}

// KeysTodo is what this device can seal for others with the keys its person holds.
type KeysTodo struct {
	Devices  []DeviceTodo `json:"devices"`  // the person's devices that lack the person's key
	People   []PersonTodo `json:"people"`   // people who see a folder and lack a version of its key
	Recovery []VersionRef `json:"recovery"` // folder keys not yet sealed for the recovery key
	Rekey    []string     `json:"rekey"`    // folders whose key needs a new version
	Pins     []PinTodo    `json:"pins"`     // PIN links that show a folder and lack a version of its key
}

type DeviceTodo struct {
	ID        string `json:"id"`
	PublicKey B64    `json:"public_key"`
}

type PersonTodo struct {
	Folder    string `json:"folder"`
	Version   int    `json:"version"`
	User      string `json:"user"`
	PublicKey B64    `json:"public_key"`
}

type VersionRef struct {
	Folder  string `json:"folder"`
	Version int    `json:"version"`
}

// PinTodo is a version of a folder's key to lock for a PIN link, whose secret is sealed for
// the folder key's version secret_version.
type PinTodo struct {
	Pin           string `json:"pin"`
	Folder        string `json:"folder"`
	Version       int    `json:"version"`
	SecretVersion int    `json:"secret_version"`
	SecretSealed  B64    `json:"secret_sealed"`
}

func (a *API) keys(w http.ResponseWriter, r *http.Request) {
	p, ok := a.device(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	k, err := a.Auth.DB.KeysOf(ctx, p.UserID, p.DeviceID)
	if err != nil {
		internal(w, "keys", err)
		return
	}
	recovery, _, err := a.Auth.DB.RecoveryKey(ctx)
	if err != nil {
		internal(w, "keys", err)
		return
	}
	todo, err := a.Auth.DB.TodoOf(ctx, p.UserID, recovery != nil, a.Now())
	if err != nil {
		internal(w, "keys", err)
		return
	}
	out := Keys{
		DeviceKey:   k.DevicePublic,
		Person:      PersonKey{PublicKey: k.PersonPublic, Sealed: k.PersonSealed, PasswordLock: k.PasswordLock},
		Folders:     []FolderKey{},
		RecoveryKey: recovery,
		Todo:        KeysTodo{Devices: []DeviceTodo{}, People: []PersonTodo{}, Recovery: []VersionRef{}, Rekey: []string{}, Pins: []PinTodo{}},
	}
	for _, f := range k.Folders {
		out.Folders = append(out.Folders, FolderKey{Folder: f.FolderID, Version: f.Version, PublicKey: f.PublicKey, Sealed: f.Sealed})
	}
	for _, d := range todo.Devices {
		out.Todo.Devices = append(out.Todo.Devices, DeviceTodo{ID: d.ID, PublicKey: d.PublicKey})
	}
	for _, n := range todo.People {
		out.Todo.People = append(out.Todo.People, PersonTodo{Folder: n.FolderID, Version: n.Version, User: n.UserID, PublicKey: n.PublicKey})
	}
	for _, v := range todo.Recovery {
		out.Todo.Recovery = append(out.Todo.Recovery, VersionRef{Folder: v.FolderID, Version: v.Version})
	}
	out.Todo.Rekey = append(out.Todo.Rekey, todo.Rekey...)
	for _, n := range todo.Pins {
		out.Todo.Pins = append(out.Todo.Pins, PinTodo{Pin: n.PinID, Folder: n.FolderID, Version: n.Version, SecretVersion: n.SecretVersion, SecretSealed: n.SecretSealed})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func badKey(w http.ResponseWriter, field string) {
	httpx.WriteError(w, http.StatusBadRequest, "bad_request", "The "+field+" isn't a key Share takes.")
}

func noKey(w http.ResponseWriter, what string) {
	httpx.WriteError(w, http.StatusConflict, "no_key", what)
}

type deviceKeyRequest struct {
	PublicKey B64 `json:"public_key"`
}

// putDeviceKey records this device's public key. A new one, from a browser whose storage was
// cleared, drops the person's key sealed for the old one.
func (a *API) putDeviceKey(w http.ResponseWriter, r *http.Request) {
	p, ok := a.device(w, r)
	if !ok {
		return
	}
	var req deviceKeyRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if e2ee.CheckPublicKey(req.PublicKey) != nil {
		badKey(w, "public_key")
		return
	}
	if err := a.Auth.DB.SetDeviceKey(r.Context(), p.DeviceID, req.PublicKey); err != nil {
		internal(w, "device key", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type personKeyRequest struct {
	PublicKey B64  `json:"public_key"`
	Sealed    B64  `json:"sealed"` // the private key, sealed for this device
	StartOver bool `json:"start_over,omitempty"`
}

// putPersonKey records the person's key, made on this device. Starting over replaces one:
// what was sealed for the old one goes, and the person waits for others to seal again.
func (a *API) putPersonKey(w http.ResponseWriter, r *http.Request) {
	p, ok := a.device(w, r)
	if !ok {
		return
	}
	var req personKeyRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if e2ee.CheckPublicKey(req.PublicKey) != nil {
		badKey(w, "public_key")
		return
	}
	if len(req.Sealed) != sealedKeySize {
		badKey(w, "sealed key")
		return
	}
	switch err := a.Auth.DB.SetPersonKey(r.Context(), p.UserID, p.DeviceID, req.PublicKey, req.Sealed, req.StartOver, a.Now()); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, db.ErrConflict):
		httpx.WriteError(w, http.StatusConflict, "key_exists", "This person has a key already: open it, or start over.")
	case errors.Is(err, db.ErrNoKey):
		noKey(w, "Send this device's key first.")
	default:
		internal(w, "person key", err)
	}
}

type passwordLockRequest struct {
	PasswordLock B64 `json:"password_lock"`
}

// putPasswordLock keeps the person's key locked with their password.
func (a *API) putPasswordLock(w http.ResponseWriter, r *http.Request) {
	p, ok := a.device(w, r)
	if !ok {
		return
	}
	var req passwordLockRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if e2ee.CheckPasswordLock(req.PasswordLock) != nil {
		badKey(w, "password_lock")
		return
	}
	switch err := a.Auth.DB.SetPasswordLock(r.Context(), p.UserID, req.PasswordLock); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, db.ErrNoKey):
		noKey(w, "This person has no key to lock yet.")
	default:
		internal(w, "password lock", err)
	}
}

type grantsRequest struct {
	Devices []struct {
		Device string `json:"device"`
		Sealed B64    `json:"sealed"`
	} `json:"devices"`
	People []struct {
		Folder  string `json:"folder"`
		Version int    `json:"version"`
		User    string `json:"user"`
		Sealed  B64    `json:"sealed"`
	} `json:"people"`
	Recovery []struct {
		Folder  string `json:"folder"`
		Version int    `json:"version"`
		Sealed  B64    `json:"sealed"`
	} `json:"recovery"`
	Pins []struct {
		Pin     string `json:"pin"`
		Version int    `json:"version"`
		Locked  B64    `json:"locked"`
	} `json:"pins"`
}

// grants keeps what this device sealed or locked from its to-do list. Whatever the server
// may not take, because the to-do list changed meanwhile, is left out.
func (a *API) grants(w http.ResponseWriter, r *http.Request) {
	p, ok := a.device(w, r)
	if !ok {
		return
	}
	var req grantsRequest
	if !httpx.DecodeJSONLimit(w, r, &req, 1<<20) {
		return
	}
	if len(req.Devices)+len(req.People)+len(req.Recovery)+len(req.Pins) > 1000 {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "At most 1000 keys at a time.")
		return
	}
	for _, g := range req.Devices {
		if len(g.Sealed) != sealedKeySize {
			badKey(w, "sealed key")
			return
		}
	}
	for _, g := range req.People {
		if len(g.Sealed) != sealedKeySize {
			badKey(w, "sealed key")
			return
		}
	}
	for _, g := range req.Recovery {
		if len(g.Sealed) != sealedKeySize {
			badKey(w, "sealed key")
			return
		}
	}
	for _, g := range req.Pins {
		if len(g.Locked) != lockedKeySize {
			badKey(w, "locked key")
			return
		}
	}
	ctx, now, d := r.Context(), a.Now(), a.Auth.DB
	keep := func(err error) bool {
		if err != nil && !errors.Is(err, db.ErrNotFound) {
			internal(w, "grants", err)
			return false
		}
		return true
	}
	for _, g := range req.Devices {
		if !keep(d.GrantDevice(ctx, p.UserID, g.Device, g.Sealed, now)) {
			return
		}
	}
	for _, g := range req.People {
		if !keep(d.GrantFolder(ctx, p.UserID, g.Folder, g.Version, g.User, g.Sealed, now)) {
			return
		}
	}
	for _, g := range req.Recovery {
		if !keep(d.GrantRecovery(ctx, p.UserID, g.Folder, g.Version, g.Sealed)) {
			return
		}
	}
	for _, g := range req.Pins {
		if !keep(d.GrantPin(ctx, p.UserID, g.Pin, g.Version, g.Locked)) {
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// newKey is a version of a folder's key, made on the caller's device: its public key, its
// private key sealed for the caller and for the recovery key.
type newKey struct {
	PublicKey      B64 `json:"public_key"`
	Sealed         B64 `json:"sealed"`
	RecoverySealed B64 `json:"recovery_sealed"`
}

// check answers 400 for a malformed key, and makes it db's.
func (k newKey) check(w http.ResponseWriter, folderID string, version int, by string) (db.NewFolderKey, bool) {
	if e2ee.CheckPublicKey(k.PublicKey) != nil {
		badKey(w, "public_key")
		return db.NewFolderKey{}, false
	}
	if len(k.Sealed) != sealedKeySize || len(k.RecoverySealed) != sealedKeySize {
		badKey(w, "sealed key")
		return db.NewFolderKey{}, false
	}
	return db.NewFolderKey{FolderID: folderID, Version: version, PublicKey: k.PublicKey, Sealed: k.Sealed, RecoverySealed: k.RecoverySealed, By: by}, true
}

type encryptionRequest struct {
	Encrypted bool    `json:"encrypted"`
	Key       *newKey `json:"key"` // the first version of the folder's key, the first time
}

// folderEncryption makes a folder's new files encrypted, or plain again. The first time, the
// admin's device brings the folder key's first version; the recovery key must exist by then.
func (a *API) folderEncryption(w http.ResponseWriter, r *http.Request) {
	p, ok := a.admin(w, r)
	if !ok {
		return
	}
	var req encryptionRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	ctx, id := r.Context(), r.PathValue("id")
	f, err := a.Auth.DB.FolderByID(ctx, id)
	if errors.Is(err, db.ErrNotFound) || (err == nil && f.DeletedAt != nil) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such folder.")
		return
	}
	if err != nil {
		internal(w, "encryption", err)
		return
	}
	var key *db.NewFolderKey
	switch {
	case req.Key != nil && f.KeyVersion > 0:
		httpx.WriteError(w, http.StatusConflict, "key_exists", "This folder has a key already.")
		return
	case req.Key == nil && req.Encrypted && f.KeyVersion == 0:
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "The first time, the folder's key is needed.")
		return
	case req.Key != nil:
		if recovery, _, err := a.Auth.DB.RecoveryKey(ctx); err != nil {
			internal(w, "encryption", err)
			return
		} else if recovery == nil {
			noKey(w, "Make the recovery key first.")
			return
		}
		k, ok := req.Key.check(w, f.ID, 1, p.UserID)
		if !ok {
			return
		}
		key = &k
	}
	switch err := a.Auth.DB.EncryptFolder(ctx, f.ID, req.Encrypted, key, a.Now()); {
	case errors.Is(err, db.ErrConflict):
		httpx.WriteError(w, http.StatusConflict, "key_exists", "This folder has a key already.")
		return
	case errors.Is(err, db.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such folder.")
		return
	case err != nil:
		internal(w, "encryption", err)
		return
	}
	if f, err = a.Auth.DB.FolderByID(ctx, f.ID); err != nil {
		internal(w, "encryption", err)
		return
	}
	a.writeFolder(w, r, p, http.StatusOK, f)
}

type folderKeyRequest struct {
	Version int `json:"version"`
	newKey
}

// newFolderKey records the next version of a folder's key, made by someone who holds the
// newest one, when someone lost the folder.
func (a *API) newFolderKey(w http.ResponseWriter, r *http.Request) {
	p, ok := a.device(w, r)
	if !ok {
		return
	}
	var req folderKeyRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	ctx, id := r.Context(), r.PathValue("id")
	f, err := a.Auth.DB.FolderByID(ctx, id)
	if errors.Is(err, db.ErrNotFound) || (err == nil && (f.DeletedAt != nil || f.KeyVersion == 0)) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such encrypted folder.")
		return
	}
	if err != nil {
		internal(w, "folder key", err)
		return
	}
	if holds, err := a.Auth.DB.HoldsFolderKey(ctx, p.UserID, f.ID, f.KeyVersion); err != nil {
		internal(w, "folder key", err)
		return
	} else if !holds {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such encrypted folder.")
		return
	}
	if req.Version != f.KeyVersion+1 {
		httpx.WriteError(w, http.StatusConflict, "key_outdated", fmt.Sprintf("The folder's next key is version %d.", f.KeyVersion+1))
		return
	}
	k, ok := req.check(w, f.ID, req.Version, p.UserID)
	if !ok {
		return
	}
	switch err := a.Auth.DB.AddFolderKey(ctx, k, a.Now()); {
	case errors.Is(err, db.ErrConflict):
		httpx.WriteError(w, http.StatusConflict, "key_outdated", "Someone made that version a moment ago.")
	case err != nil:
		internal(w, "folder key", err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// Recovery is the recovery key, for admins: its public key, its private key locked with the
// recovery code, and the folder keys sealed for it.
type Recovery struct {
	PublicKey B64         `json:"public_key"`
	Locked    B64         `json:"locked"`
	Folders   []FolderKey `json:"folders"`
}

func (a *API) recovery(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.admin(w, r); !ok {
		return
	}
	ctx := r.Context()
	public, locked, err := a.Auth.DB.RecoveryKey(ctx)
	if err != nil {
		internal(w, "recovery", err)
		return
	}
	out := Recovery{PublicKey: public, Locked: locked, Folders: []FolderKey{}}
	if public != nil {
		keys, err := a.Auth.DB.RecoverableKeys(ctx)
		if err != nil {
			internal(w, "recovery", err)
			return
		}
		for _, k := range keys {
			out.Folders = append(out.Folders, FolderKey{Folder: k.FolderID, Version: k.Version, PublicKey: k.PublicKey, Sealed: k.RecoverySealed})
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

type recoveryRequest struct {
	PublicKey B64 `json:"public_key"`
	Locked    B64 `json:"locked"`
}

// putRecovery records a new recovery key; what was sealed for an older one goes, and the
// admins' devices seal the folder keys for the new one.
func (a *API) putRecovery(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.admin(w, r); !ok {
		return
	}
	var req recoveryRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if e2ee.CheckPublicKey(req.PublicKey) != nil {
		badKey(w, "public_key")
		return
	}
	if len(req.Locked) != lockedKeySize {
		badKey(w, "locked key")
		return
	}
	if err := a.Auth.DB.SetRecoveryKey(r.Context(), req.PublicKey, req.Locked); err != nil {
		internal(w, "recovery", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Settings are the admins' settings for the whole server.
type Settings struct {
	NewFoldersEncrypted bool `json:"new_folders_encrypted"`
}

func (a *API) settings(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.admin(w, r); !ok {
		return
	}
	on, err := a.Auth.DB.NewFoldersEncrypted(r.Context())
	if err != nil {
		internal(w, "settings", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, Settings{NewFoldersEncrypted: on})
}

func (a *API) putSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.admin(w, r); !ok {
		return
	}
	var req Settings
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if err := a.Auth.DB.SetNewFoldersEncrypted(r.Context(), req.NewFoldersEncrypted); err != nil {
		internal(w, "settings", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, req)
}

// PinKeys are the folder keys locked for the link of a PIN that shows its folder.
type PinKeys struct {
	Folder string       `json:"folder"`
	Keys   []PinKeyInfo `json:"keys"`
}

type PinKeyInfo struct {
	Version   int `json:"version"`
	PublicKey B64 `json:"public_key"`
	Locked    B64 `json:"locked"`
}

// pinKeys gives a PIN session that shows its folder the folder's keys, locked with the secret
// that only the PIN's link carries.
func (a *API) pinKeys(w http.ResponseWriter, r *http.Request) {
	p, err := a.Auth.Authenticate(r.Context(), r)
	if err != nil {
		httpx.WriteAuthError(w, r, err)
		return
	}
	if p.Kind != auth.KindPin || !p.PinShowsFolder {
		httpx.WriteError(w, http.StatusForbidden, "forbidden", "Only a PIN that shows its folder has these keys.")
		return
	}
	ctx := r.Context()
	keys, err := a.Auth.DB.PinKeys(ctx, p.PinID)
	if err != nil {
		internal(w, "pin keys", err)
		return
	}
	out := PinKeys{Folder: p.PinFolderID, Keys: []PinKeyInfo{}}
	for _, k := range keys {
		fk, err := a.Auth.DB.FolderKeyOf(ctx, p.PinFolderID, k.Version)
		if err != nil {
			internal(w, "pin keys", err)
			return
		}
		out.Keys = append(out.Keys, PinKeyInfo{Version: k.Version, PublicKey: fk.PublicKey, Locked: k.Locked})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
