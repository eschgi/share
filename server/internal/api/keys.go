package api

// The keys of end-to-end encryption (docs/e2ee-plan.md): what a phone or browser needs to open
// what its person may read and what it can seal for others, a folder's switch and key versions,
// and the recovery key, which is the root that signs them. The server checks sizes, signatures
// and who may give which key to whom; it can't open any of them.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/e2ee"
	"github.com/eschgi/share/server/internal/httpx"
	"github.com/eschgi/share/server/internal/ids"
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

// Sizes of what clients send: a private key sealed for a public key, one locked with a link's
// secret, and a public key locked with one.
const (
	sealedKeySize  = e2ee.PrivateKeySize + e2ee.SealOverhead
	lockedKeySize  = e2ee.PrivateKeySize + e2ee.LockOverhead
	lockedRootSize = e2ee.PublicKeySize + e2ee.LockOverhead
	// What the asking device hands on after Allow, and a person's note: small JSON, locked.
	maxConfirmation = 1 << 10
	maxNote         = 1 << 20
)

func (a *API) registerKeys(mux routes) {
	mux.HandleFunc("GET /api/keys", a.keys)
	mux.HandleFunc("PUT /api/keys/device", a.putDeviceKey)
	mux.HandleFunc("PUT /api/keys/person", a.putPersonKey)
	mux.HandleFunc("PUT /api/keys/password-lock", a.putPasswordLock)
	mux.HandleFunc("PUT /api/keys/note", a.putNote)
	mux.HandleFunc("POST /api/keys/grants", a.grants)
	mux.HandleFunc("POST /api/keys/checks", a.newCheck)
	mux.HandleFunc("PUT /api/keys/checks/{id}/answer", a.answerCheck)
	mux.HandleFunc("PUT /api/keys/checks/{id}/reveal", a.revealCheck)
	mux.HandleFunc("PUT /api/keys/checks/{id}/confirm", a.confirmCheck)
	mux.HandleFunc("DELETE /api/keys/checks/{id}", a.deleteCheck)
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
	DeviceKey  B64         `json:"device_key"` // this device's public key, once it sent one
	Person     PersonKey   `json:"person"`
	Folders    []FolderKey `json:"folders"`     // every version of the keys of the folders the person sees
	Roots      []RootInfo  `json:"roots"`       // the chain of roots, the first first; the newest is the recovery key
	RootSealed B64         `json:"root_sealed"` // the newest root's private key, sealed for the person (admins)
	Todo       KeysTodo    `json:"todo"`
	Checks     []CheckInfo `json:"checks"` // the open checks this device takes part in
}

// RootInfo is a root key: its public key, signed by the one before, which phones and browsers
// follow from the root they trust to the newest.
type RootInfo struct {
	PublicKey B64 `json:"public_key"`
	Signature B64 `json:"signature"` // null for the first
}

// CheckInfo is an open check this device takes part in (docs/e2ee-plan.md): one it asks, or
// one for it or its person. The server relays the commitment, the two one-time keys and the
// confirmation, in order.
type CheckInfo struct {
	ID           string  `json:"id"`
	Asking       bool    `json:"asking"`       // this device asks
	Device       *string `json:"device"`       // the device it is for; null for a person
	User         *string `json:"user"`         // the person it is for; null for a device
	From         string  `json:"from"`         // the asking device's name
	Commitment   B64     `json:"commitment"`   // to the asking device's one-time key
	Answer       B64     `json:"answer"`       // the waiting side's one-time key; null until it answers
	Answered     bool    `json:"answered"`     // this device answered
	Reveal       B64     `json:"reveal"`       // the asking device's one-time key; null until it reveals it
	Confirmation B64     `json:"confirmation"` // what the asking device hands on after Allow; null until then
}

// PersonKey is the person's key: public, sealed for this device, locked with the password, and
// how many of the person's phones and browsers hold it, with their note. Held by none, without a
// password lock, it is lost, and a device that waits for it makes a new one.
type PersonKey struct {
	PublicKey    B64 `json:"public_key"`
	Sealed       B64 `json:"sealed"`
	PasswordLock B64 `json:"password_lock"`
	HeldBy       int `json:"held_by"`
	Note         B64 `json:"note"` // the root and the newest versions the person's devices have seen, locked
}

// FolderKey is a version of a folder's key, signed by the root, with its private key sealed for
// the person, or null while nobody has sealed it for them yet.
type FolderKey struct {
	Folder    string `json:"folder"`
	Version   int    `json:"version"`
	PublicKey B64    `json:"public_key"`
	Signature B64    `json:"signature"`
	Sealed    B64    `json:"sealed"`
}

// KeysTodo is what this device can seal for others with the keys its person holds.
type KeysTodo struct {
	Devices  []DeviceTodo `json:"devices"`  // the person's devices that lack the person's key
	People   []PersonTodo `json:"people"`   // people who see a folder and lack a version of its key
	Roots    []RootTodo   `json:"roots"`    // admins who lack the root's private key the person holds
	Recovery []VersionRef `json:"recovery"` // folder keys not yet sealed for the recovery key
	Rekey    []string     `json:"rekey"`    // encrypted folders whose key needs a new version
	Pins     []PinTodo    `json:"pins"`     // PIN links that show a folder and lack a version of its key
}

// RootTodo is an admin whose person lacks the newest root's private key: given like folder keys.
type RootTodo struct {
	User      string `json:"user"`
	Name      string `json:"name"`
	PublicKey B64    `json:"public_key"`
	Active    bool   `json:"active"` // a device of theirs with their key was seen lately, so it can answer a check
}

// DeviceTodo is a device of the person that lacks their key: whoever passes it on checks first.
type DeviceTodo struct {
	ID        string    `json:"id"`
	PublicKey B64       `json:"public_key"`
	Name      string    `json:"name"`
	Client    string    `json:"client"`     // "app" or "web"
	CreatedAt time.Time `json:"created_at"` // when it signed in
	Active    bool      `json:"active"`     // seen lately, so it can answer a check
}

type PersonTodo struct {
	Folder    string `json:"folder"`
	Version   int    `json:"version"`
	User      string `json:"user"`
	Name      string `json:"name"`
	PublicKey B64    `json:"public_key"`
	Active    bool   `json:"active"` // a device of theirs with their key was seen lately, so it can answer a check
}

type VersionRef struct {
	Folder  string `json:"folder"`
	Version int    `json:"version"`
}

// PinTodo is a version of a folder's key to lock for a PIN link, whose secret is locked with a
// key from the folder key's version secret_version.
type PinTodo struct {
	Pin           string `json:"pin"`
	Folder        string `json:"folder"`
	Version       int    `json:"version"`
	SecretVersion int    `json:"secret_version"`
	SecretLocked  B64    `json:"secret_locked"`
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
	roots, err := a.roots(ctx)
	if err != nil {
		internal(w, "keys", err)
		return
	}
	todo, err := a.Auth.DB.TodoOf(ctx, p.UserID, p.Role == db.RoleAdmin, a.Now())
	if err != nil {
		internal(w, "keys", err)
		return
	}
	out := Keys{
		DeviceKey:  k.DevicePublic,
		Person:     PersonKey{PublicKey: k.PersonPublic, Sealed: k.PersonSealed, PasswordLock: k.PasswordLock, HeldBy: k.HeldBy, Note: k.Note},
		Folders:    []FolderKey{},
		Roots:      roots,
		RootSealed: k.RootSealed,
		Todo:       KeysTodo{Devices: []DeviceTodo{}, People: []PersonTodo{}, Roots: []RootTodo{}, Recovery: []VersionRef{}, Rekey: []string{}, Pins: []PinTodo{}},
		Checks:     []CheckInfo{},
	}
	for _, f := range k.Folders {
		out.Folders = append(out.Folders, FolderKey{Folder: f.FolderID, Version: f.Version, PublicKey: f.PublicKey, Signature: f.Signature, Sealed: f.Sealed})
	}
	for _, n := range todo.Roots {
		out.Todo.Roots = append(out.Todo.Roots, RootTodo{User: n.UserID, Name: n.Name, PublicKey: n.PublicKey, Active: n.Active})
	}
	for _, d := range todo.Devices {
		out.Todo.Devices = append(out.Todo.Devices, DeviceTodo{ID: d.ID, PublicKey: d.PublicKey, Name: d.Name, Client: d.Client, CreatedAt: d.CreatedAt, Active: d.Active})
	}
	for _, n := range todo.People {
		out.Todo.People = append(out.Todo.People, PersonTodo{Folder: n.FolderID, Version: n.Version, User: n.UserID, Name: n.Name, PublicKey: n.PublicKey, Active: n.Active})
	}
	checks, err := a.Auth.DB.ChecksOf(ctx, p.DeviceID, p.UserID, a.Now())
	if err != nil {
		internal(w, "keys", err)
		return
	}
	for _, c := range checks {
		out.Checks = append(out.Checks, checkInfo(c, p.DeviceID))
	}
	for _, v := range todo.Recovery {
		out.Todo.Recovery = append(out.Todo.Recovery, VersionRef{Folder: v.FolderID, Version: v.Version})
	}
	out.Todo.Rekey = append(out.Todo.Rekey, todo.Rekey...)
	for _, n := range todo.Pins {
		out.Todo.Pins = append(out.Todo.Pins, PinTodo{Pin: n.PinID, Folder: n.FolderID, Version: n.Version, SecretVersion: n.SecretVersion, SecretLocked: n.SecretLocked})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// roots is the chain of roots, for GET /api/keys, a PIN's session and the recovery key.
func (a *API) roots(ctx context.Context) ([]RootInfo, error) {
	roots, err := a.Auth.DB.Roots(ctx)
	out := []RootInfo{}
	for _, r := range roots {
		out = append(out, RootInfo{PublicKey: r.PublicKey, Signature: r.Signature})
	}
	return out, err
}

func checkInfo(c db.Check, device string) CheckInfo {
	ref := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	return CheckInfo{
		ID: c.ID, Asking: c.Asker == device, Device: ref(c.DeviceID), User: ref(c.UserID), From: c.AskerName,
		Commitment: c.Commitment, Answer: c.Answer, Answered: c.AnsweredBy == device, Reveal: c.Reveal, Confirmation: c.Confirmation,
	}
}

type checkRequest struct {
	Device     string `json:"device,omitempty"` // a device of this person that waits for their key
	User       string `json:"user,omitempty"`   // a person who waits for folder keys this device holds
	Commitment B64    `json:"commitment"`
}

// newCheck opens a check before this device passes keys on: its person's key to another of
// their devices, or, for an admin, folder keys to another person, and the root's private key to
// another admin. It replaces this device's earlier check for the same one.
func (a *API) newCheck(w http.ResponseWriter, r *http.Request) {
	p, ok := a.device(w, r)
	if !ok {
		return
	}
	var req checkRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if (req.Device == "") == (req.User == "") {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "Name a device or a person.")
		return
	}
	if req.User != "" && p.Role != db.RoleAdmin {
		httpx.WriteError(w, http.StatusForbidden, "forbidden", "Only admins pass folder keys on to other people.")
		return
	}
	if (req.Device != "" && !ids.Valid(req.Device)) || (req.User != "" && !ids.Valid(req.User)) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "Nothing has that id.")
		return
	}
	if len(req.Commitment) != sha256.Size {
		badKey(w, "commitment")
		return
	}
	id := ids.New()
	switch err := a.Auth.DB.NewCheck(r.Context(), id, p.DeviceID, p.UserID, req.Device, req.User, req.Commitment, a.Now()); {
	case errors.Is(err, db.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "Nobody there waits for keys this device holds.")
		return
	case err != nil:
		internal(w, "check", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

type answerRequest struct {
	Key B64 `json:"key"` // the waiting side's one-time public key
}

func goodKey(w http.ResponseWriter, key []byte, field string) bool {
	if e2ee.CheckPublicKey(key) != nil {
		badKey(w, field)
		return false
	}
	return true
}

// checkID is the check named in the path; an id no check can have finds none.
func checkID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if !ids.Valid(id) {
		noCheck(w)
		return "", false
	}
	return id, true
}

func noCheck(w http.ResponseWriter) {
	httpx.WriteError(w, http.StatusNotFound, "not_found", "No such check is open for this device.")
}

// answerCheck keeps the waiting side's one-time key, from the device a check is for, or from a
// device of the person it is for that holds their key.
func (a *API) answerCheck(w http.ResponseWriter, r *http.Request) {
	p, ok := a.device(w, r)
	if !ok {
		return
	}
	id, ok := checkID(w, r)
	if !ok {
		return
	}
	var req answerRequest
	if !httpx.DecodeJSON(w, r, &req) || !goodKey(w, req.Key, "key") {
		return
	}
	switch err := a.Auth.DB.AnswerCheck(r.Context(), id, p.DeviceID, p.UserID, req.Key, a.Now()); {
	case errors.Is(err, db.ErrNotFound):
		noCheck(w)
	case err != nil:
		internal(w, "check", err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

type revealRequest struct {
	Key    B64 `json:"key"`    // the asking device's one-time public key
	Answer B64 `json:"answer"` // the answer the asking device saw, which its code is made from
}

// revealCheck keeps the asking device's one-time key, after the answer, if it matches the
// commitment and the answer is still the one the asking device saw.
func (a *API) revealCheck(w http.ResponseWriter, r *http.Request) {
	p, ok := a.device(w, r)
	if !ok {
		return
	}
	id, ok := checkID(w, r)
	if !ok {
		return
	}
	var req revealRequest
	if !httpx.DecodeJSON(w, r, &req) || !goodKey(w, req.Key, "key") || !goodKey(w, req.Answer, "answer") {
		return
	}
	ctx := r.Context()
	c, err := a.Auth.DB.CheckByID(ctx, id, a.Now())
	if errors.Is(err, db.ErrNotFound) || (err == nil && c.Asker != p.DeviceID) {
		noCheck(w)
		return
	}
	if err != nil {
		internal(w, "check", err)
		return
	}
	if !bytes.Equal(e2ee.Commitment(req.Key), c.Commitment) {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "The key isn't the one committed to.")
		return
	}
	switch err := a.Auth.DB.RevealCheck(ctx, c.ID, p.DeviceID, req.Key, req.Answer, a.Now()); {
	case errors.Is(err, db.ErrNotFound):
		httpx.WriteError(w, http.StatusConflict, "not_answered", "The check has no answer to reveal for, another one by now, or was revealed.")
	case err != nil:
		internal(w, "check", err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

type confirmRequest struct {
	Confirmation B64 `json:"confirmation"`
}

// confirmCheck keeps what the asking device hands on after Allow, locked for the waiting side;
// that side closes the check once it read it.
func (a *API) confirmCheck(w http.ResponseWriter, r *http.Request) {
	p, ok := a.device(w, r)
	if !ok {
		return
	}
	id, ok := checkID(w, r)
	if !ok {
		return
	}
	var req confirmRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if len(req.Confirmation) <= e2ee.LockOverhead || len(req.Confirmation) > maxConfirmation {
		badKey(w, "confirmation")
		return
	}
	switch err := a.Auth.DB.ConfirmCheck(r.Context(), id, p.DeviceID, req.Confirmation, a.Now()); {
	case errors.Is(err, db.ErrNotFound):
		httpx.WriteError(w, http.StatusConflict, "not_answered", "The check isn't revealed yet, was confirmed, or is closed.")
	case err != nil:
		internal(w, "check", err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// deleteCheck closes a check, for the device that asks or the side it is for.
func (a *API) deleteCheck(w http.ResponseWriter, r *http.Request) {
	p, ok := a.device(w, r)
	if !ok {
		return
	}
	id, ok := checkID(w, r)
	if !ok {
		return
	}
	switch err := a.Auth.DB.DeleteCheck(r.Context(), id, p.DeviceID, p.UserID); {
	case errors.Is(err, db.ErrNotFound):
		noCheck(w)
	case err != nil:
		internal(w, "check", err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
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

type noteRequest struct {
	Note B64 `json:"note"`
}

// putNote keeps the person's note: the root and the newest versions of folder keys their
// phones and browsers have seen, locked with a key from their private key.
func (a *API) putNote(w http.ResponseWriter, r *http.Request) {
	p, ok := a.device(w, r)
	if !ok {
		return
	}
	var req noteRequest
	if !httpx.DecodeJSONLimit(w, r, &req, 2*maxNote) {
		return
	}
	if len(req.Note) <= e2ee.LockOverhead || len(req.Note) > maxNote {
		badKey(w, "note")
		return
	}
	switch err := a.Auth.DB.SetNote(r.Context(), p.UserID, req.Note); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, db.ErrNoKey):
		noKey(w, "This person has no key yet.")
	default:
		internal(w, "note", err)
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
	Roots []struct {
		User   string `json:"user"`
		Sealed B64    `json:"sealed"`
	} `json:"roots"`
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
	if len(req.Devices)+len(req.People)+len(req.Recovery)+len(req.Pins)+len(req.Roots) > 1000 {
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
	for _, g := range req.Roots {
		if len(g.Sealed) != sealedKeySize {
			badKey(w, "sealed key")
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
	for _, g := range req.Roots {
		if !keep(d.GrantRoot(ctx, p.UserID, g.User, g.Sealed, now)) {
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// newKey is a version of a folder's key, made on the caller's device: its public key, signed by
// the root, its private key sealed for the caller and for the recovery key.
type newKey struct {
	PublicKey      B64 `json:"public_key"`
	Signature      B64 `json:"signature"`
	Sealed         B64 `json:"sealed"`
	RecoverySealed B64 `json:"recovery_sealed"`
}

// check answers 400 for a malformed key, and makes it db's, which checks the signature.
func (k newKey) check(w http.ResponseWriter, folderID string, version int, by string) (db.NewFolderKey, bool) {
	if e2ee.CheckPublicKey(k.PublicKey) != nil {
		badKey(w, "public_key")
		return db.NewFolderKey{}, false
	}
	if len(k.Sealed) != sealedKeySize || len(k.RecoverySealed) != sealedKeySize {
		badKey(w, "sealed key")
		return db.NewFolderKey{}, false
	}
	if len(k.Signature) != e2ee.SignatureSize {
		badKey(w, "signature")
		return db.NewFolderKey{}, false
	}
	return db.NewFolderKey{FolderID: folderID, Version: version, PublicKey: k.PublicKey, Signature: k.Signature, Sealed: k.Sealed, RecoverySealed: k.RecoverySealed, By: by}, true
}

// signatureError answers what the root's signatures can go wrong with; it reports whether err
// was one of them.
func signatureError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, db.ErrBadSignature):
		httpx.WriteError(w, http.StatusBadRequest, "bad_signature", "The recovery key's signature doesn't verify: sign again with the newest one.")
	case errors.Is(err, db.ErrNoKey):
		noKey(w, "Make the recovery key first.")
	default:
		return false
	}
	return true
}

type encryptionRequest struct {
	Encrypted bool    `json:"encrypted"`
	Key       *newKey `json:"key"` // turning it on: the next version of the folder's key
	// PlainSignature: turning it off, the root's plain statement for the folder's newest version
	// and its name.
	PlainSignature B64 `json:"plain_signature"`
}

// folderEncryption makes a folder's new files encrypted, with the next version of its key, or
// plain again, with the root's plain statement. The recovery key must exist for the first.
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
	case req.Encrypted && req.Key == nil:
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "Turning encryption on needs the folder key's next version.")
		return
	case !req.Encrypted && req.Key != nil:
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "Turning encryption off takes no key.")
		return
	case req.Key != nil:
		k, ok := req.Key.check(w, f.ID, f.KeyVersion+1, p.UserID)
		if !ok {
			return
		}
		key = &k
	}
	switch err := a.Auth.DB.EncryptFolder(ctx, f.ID, req.Encrypted, key, req.PlainSignature, a.Now()); {
	case signatureError(w, err):
		return
	case errors.Is(err, db.ErrConflict):
		httpx.WriteError(w, http.StatusConflict, "key_outdated", "Someone made that version a moment ago.")
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

// newFolderKey records the next version of a folder's key, made and signed by someone who holds
// the newest one and the root, when someone lost the folder. A folder switched off gets it when
// encryption is turned on again.
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
	if !f.Encrypted {
		httpx.WriteError(w, http.StatusConflict, "not_encrypted", "This folder is switched off: turning it on makes its next version.")
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
	case signatureError(w, err):
	case errors.Is(err, db.ErrConflict):
		httpx.WriteError(w, http.StatusConflict, "key_outdated", "Someone made that version a moment ago.")
	case err != nil:
		internal(w, "folder key", err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// Recovery is the recovery key, for admins: the chain of roots, the newest one's private key
// locked with the recovery code, the folder keys sealed for it, and what a new recovery code
// signs anew.
type Recovery struct {
	PublicKey B64         `json:"public_key"`
	Locked    B64         `json:"locked"`
	Roots     []RootInfo  `json:"roots"`
	Folders   []FolderKey `json:"folders"`
	Sign      ToSign      `json:"sign"`
}

// ToSign is what a new recovery code signs anew, after checking what the one before signed:
// every version of every folder's key, and every folder that sends plain (before the first
// recovery key, every folder, without a signature yet).
type ToSign struct {
	FolderKeys []FolderKey   `json:"folder_keys"`
	Plain      []PlainFolder `json:"plain"`
}

// PlainFolder is a folder's plain statement: what the root signs for a folder that sends plain.
type PlainFolder struct {
	Folder    string `json:"folder"`
	Version   int    `json:"version"` // its newest key version; 0 without one
	Name      string `json:"name"`
	Signature B64    `json:"signature"`
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
	roots, err := a.roots(ctx)
	if err != nil {
		internal(w, "recovery", err)
		return
	}
	sign, err := a.Auth.DB.ToSign(ctx)
	if err != nil {
		internal(w, "recovery", err)
		return
	}
	out := Recovery{PublicKey: public, Locked: locked, Roots: roots, Folders: []FolderKey{}, Sign: ToSign{FolderKeys: []FolderKey{}, Plain: []PlainFolder{}}}
	for _, k := range sign.FolderKeys {
		out.Sign.FolderKeys = append(out.Sign.FolderKeys, FolderKey{Folder: k.FolderID, Version: k.Version, PublicKey: k.PublicKey, Signature: k.Signature})
	}
	for _, f := range sign.Plain {
		out.Sign.Plain = append(out.Sign.Plain, PlainFolder{Folder: f.FolderID, Version: f.Version, Name: f.Name, Signature: f.Signature})
	}
	if public != nil {
		keys, err := a.Auth.DB.RecoverableKeys(ctx)
		if err != nil {
			internal(w, "recovery", err)
			return
		}
		for _, k := range keys {
			out.Folders = append(out.Folders, FolderKey{Folder: k.FolderID, Version: k.Version, PublicKey: k.PublicKey, Signature: k.Signature, Sealed: k.RecoverySealed})
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// resigned is a new recovery key's signature of a version of a folder's key, or of a folder's
// plain statement (no version).
type resigned struct {
	Folder    string `json:"folder"`
	Version   int    `json:"version,omitempty"`
	Signature B64    `json:"signature"`
}

type recoveryRequest struct {
	PublicKey  B64        `json:"public_key"`
	Locked     B64        `json:"locked"`    // its private key, locked with the recovery code
	Signature  B64        `json:"signature"` // by the recovery key before it; null for the first
	Sealed     B64        `json:"sealed"`    // its private key, sealed for the caller's person key
	FolderKeys []resigned `json:"folder_keys"`
	Plain      []resigned `json:"plain"`
}

// putRecovery records a new recovery key, signed by the one before, with everything it signs
// anew; what was sealed for an older one goes, and the admins' devices seal the folder keys for
// the new one, and pass its private key on to the other admins.
func (a *API) putRecovery(w http.ResponseWriter, r *http.Request) {
	p, ok := a.admin(w, r)
	if !ok {
		return
	}
	var req recoveryRequest
	if !httpx.DecodeJSONLimit(w, r, &req, 4<<20) {
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
	if len(req.Sealed) != sealedKeySize {
		badKey(w, "sealed key")
		return
	}
	root := db.NewRoot{PublicKey: req.PublicKey, Signature: req.Signature, Locked: req.Locked, Sealed: req.Sealed, By: p.UserID}
	for _, k := range req.FolderKeys {
		root.FolderKeys = append(root.FolderKeys, db.Resigned{FolderID: k.Folder, Version: k.Version, Signature: k.Signature})
	}
	for _, f := range req.Plain {
		root.Plain = append(root.Plain, db.Resigned{FolderID: f.Folder, Signature: f.Signature})
	}
	switch err := a.Auth.DB.AddRoot(r.Context(), root, a.Now()); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, db.ErrBadSignature):
		httpx.WriteError(w, http.StatusBadRequest, "bad_signature", "A signature doesn't verify: the new recovery key signs everything anew, and the one before signs it.")
	case errors.Is(err, db.ErrConflict):
		httpx.WriteError(w, http.StatusConflict, "key_outdated", "The folders' keys changed meanwhile: sign again what /api/recovery lists now.")
	case errors.Is(err, db.ErrNoKey):
		noKey(w, "Your person needs a key first.")
	default:
		internal(w, "recovery", err)
	}
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

// PinKeys are the folder keys locked for the link of a PIN that shows its folder, with the root
// locked the same way.
type PinKeys struct {
	Folder string       `json:"folder"`
	Keys   []PinKeyInfo `json:"keys"`
	Root   B64          `json:"root"`
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
	if s, err := a.Auth.DB.PinSecretOf(ctx, p.PinID); err != nil {
		internal(w, "pin keys", err)
		return
	} else if s != nil {
		out.Root = s.Root
	}
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
