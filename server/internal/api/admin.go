package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"slices"
	"sort"
	"time"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/httpx"
	"github.com/eschgi/share/server/internal/ids"
	"github.com/eschgi/share/server/internal/storage"
)

// The admin API: PINs, people and their phones and browsers, deleting files and the trash,
// storage. Everything here needs an admin, except signing out one's own phone or browser.

func (a *API) registerAdmin(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/pins", a.pins)
	mux.HandleFunc("GET /api/pins/suggest", a.suggestPin)
	mux.HandleFunc("POST /api/pins", a.createPin)
	mux.HandleFunc("POST /api/pins/{id}/new-code", a.newPinCode)
	mux.HandleFunc("POST /api/pins/{id}/end", a.endPin)

	mux.HandleFunc("GET /api/users", a.people)
	mux.HandleFunc("PATCH /api/users/{id}", a.changeRole)
	mux.HandleFunc("DELETE /api/users/{id}", a.removePerson)
	mux.HandleFunc("POST /api/users/{id}/password", a.newPassword)
	mux.HandleFunc("DELETE /api/devices/{id}", a.signOutPhone)
	mux.HandleFunc("POST /api/invites", a.invite)
	mux.HandleFunc("POST /api/users/{id}/invites", a.invitePhone)
	mux.HandleFunc("DELETE /api/invites/{id}", a.withdrawInvite)

	mux.HandleFunc("POST /api/folders", a.createFolder)
	mux.HandleFunc("PATCH /api/folders/{id}", a.renameFolder)
	mux.HandleFunc("DELETE /api/folders/{id}", a.deleteFolder)

	mux.HandleFunc("POST /api/files/delete", a.deleteFiles)
	mux.HandleFunc("GET /api/trash", a.trash)
	mux.HandleFunc("POST /api/trash/restore", a.restore)
	mux.HandleFunc("POST /api/trash/purge", a.purge)
	mux.HandleFunc("GET /api/admin/storage", a.storageInfo)
}

// admin authenticates a request that only an admin's phone or browser may make.
func (a *API) admin(w http.ResponseWriter, r *http.Request) (*auth.Principal, bool) {
	p, ok := a.device(w, r)
	if !ok {
		return nil, false
	}
	if p.Role != db.RoleAdmin {
		httpx.WriteError(w, http.StatusForbidden, "forbidden", "Only admins can do this.")
		return nil, false
	}
	return p, true
}

// PinInfo is an upload PIN as the admin screens show it.
type PinInfo struct {
	ID        string     `json:"id"`
	Code      string     `json:"code"`
	Kind      string     `json:"kind"` // "permanent" or "day"
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at"` // for a 24-hour PIN
	Link      string     `json:"link"`       // the website with the PIN filled in
	Files     int        `json:"files"`      // sent with it and still in the library
	Phones    int        `json:"phones"`     // browsers and phones that unlocked it
}

func (a *API) pinInfo(p db.Pin, s db.PinStat) PinInfo {
	return PinInfo{
		ID: p.ID, Code: p.Code, Kind: p.Kind, CreatedAt: p.CreatedAt, ExpiresAt: p.ExpiresAt,
		Link: a.Cfg.PublicURL + "/#" + p.Code, Files: s.Files, Phones: s.Phones,
	}
}

// PinList is the live PINs: permanent ones first, then the newest.
type PinList struct {
	Pins []PinInfo `json:"pins"`
}

func (a *API) pins(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.admin(w, r); !ok {
		return
	}
	all, err := a.Auth.DB.Pins(r.Context())
	if err != nil {
		internal(w, "pins", err)
		return
	}
	stats, err := a.Auth.DB.PinStats(r.Context())
	if err != nil {
		internal(w, "pins", err)
		return
	}
	list := PinList{Pins: []PinInfo{}}
	now := a.Now()
	for _, p := range all {
		if p.LiveAt(now) {
			list.Pins = append(list.Pins, a.pinInfo(p, stats[p.ID]))
		}
	}
	sort.SliceStable(list.Pins, func(i, j int) bool {
		return list.Pins[i].Kind == db.PinPermanent && list.Pins[j].Kind != db.PinPermanent
	})
	httpx.WriteJSON(w, http.StatusOK, list)
}

// PinSuggestion is a code no PIN has had yet.
type PinSuggestion struct {
	Code string `json:"code"`
}

func (a *API) suggestPin(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.admin(w, r); !ok {
		return
	}
	code, err := a.Auth.SuggestCode(r.Context())
	if err != nil {
		internal(w, "suggest PIN", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, PinSuggestion{Code: code})
}

type createPinRequest struct {
	Kind string `json:"kind"`
	Code string `json:"code,omitempty"` // empty: a random one
}

func (a *API) createPin(w http.ResponseWriter, r *http.Request) {
	p, ok := a.admin(w, r)
	if !ok {
		return
	}
	var req createPinRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Kind != db.PinPermanent && req.Kind != db.PinDay {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "kind must be permanent or day.")
		return
	}
	folder, err := a.oldestFolder(r.Context())
	if err != nil {
		internal(w, "create PIN", err)
		return
	}
	pin, err := a.Auth.CreatePin(r.Context(), auth.PinSpec{Kind: req.Kind, Code: req.Code, FolderID: folder.ID}, p.UserID)
	switch {
	case err == nil:
		httpx.WriteJSON(w, http.StatusCreated, a.pinInfo(pin, db.PinStat{}))
	case errors.Is(err, auth.ErrPINFormat):
		httpx.WriteError(w, http.StatusBadRequest, "pin_format", "A PIN has 5 letters or numbers.")
	case errors.Is(err, auth.ErrPinTaken):
		httpx.WriteError(w, http.StatusConflict, "pin_taken", "That PIN was used before. Pick another one.")
	default:
		internal(w, "create PIN", err)
	}
}

// livePin loads the PIN a request is about, if it still works.
func (a *API) livePin(w http.ResponseWriter, r *http.Request) (db.Pin, bool) {
	pin, err := a.Auth.DB.PinByID(r.Context(), r.PathValue("id"))
	if errors.Is(err, db.ErrNotFound) || (err == nil && !pin.LiveAt(a.Now())) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such PIN, or it has ended.")
		return pin, false
	}
	if err != nil {
		internal(w, "PIN", err)
		return pin, false
	}
	return pin, true
}

func (a *API) newPinCode(w http.ResponseWriter, r *http.Request) {
	p, ok := a.admin(w, r)
	if !ok {
		return
	}
	old, ok := a.livePin(w, r)
	if !ok {
		return
	}
	pin, err := a.Auth.NewCode(r.Context(), old.ID, p.UserID)
	if err != nil {
		internal(w, "new PIN code", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.pinInfo(pin, db.PinStat{}))
}

func (a *API) endPin(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.admin(w, r); !ok {
		return
	}
	pin, ok := a.livePin(w, r)
	if !ok {
		return
	}
	if err := a.Auth.EndPin(r.Context(), pin.ID); err != nil {
		internal(w, "end PIN", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PersonInfo is someone with an account, as the admin's people list shows them.
type PersonInfo struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Role        string      `json:"role"`
	Username    *string     `json:"username"`
	HasPassword bool        `json:"has_password"`
	Me          bool        `json:"me"`
	CreatedAt   time.Time   `json:"created_at"`
	LastSeenAt  *time.Time  `json:"last_seen_at"` // on any phone or browser; null without one
	Phones      []PhoneInfo `json:"phones"`       // phones and browsers
}

// PhoneInfo is one of a person's signed-in phones or browsers.
type PhoneInfo struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Client     string    `json:"client"`    // "app" or "web"
	HomeOnly   bool      `json:"home_only"` // a browser that signed in at home; it works only there
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	This       bool      `json:"this"` // the phone or browser asking
}

func phoneInfo(dv db.Device, asking string) PhoneInfo {
	return PhoneInfo{
		ID: dv.ID, Name: dv.Name, Client: dv.Client, HomeOnly: dv.HomeOnly,
		CreatedAt: dv.CreatedAt, LastSeenAt: dv.LastSeenAt, This: dv.ID == asking,
	}
}

// OpenInvite is an invite nobody has used yet.
type OpenInvite struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	UserID    *string   `json:"user_id"` // set: it adds a phone for this person
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

func openInvite(in db.Invite) OpenInvite {
	o := OpenInvite{ID: in.ID, Name: in.Name, Role: in.Role, CreatedAt: in.CreatedAt, ExpiresAt: in.ExpiresAt}
	if in.UserID != "" {
		o.UserID = &in.UserID
	}
	return o
}

// People is everyone with an account, and the invites still open.
type People struct {
	Users   []PersonInfo `json:"users"`
	Invites []OpenInvite `json:"invites"`
}

func (a *API) people(w http.ResponseWriter, r *http.Request) {
	p, ok := a.admin(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	users, err := a.Auth.DB.Users(ctx)
	if err != nil {
		internal(w, "people", err)
		return
	}
	phones, err := a.Auth.DB.SignedInDevices(ctx)
	if err != nil {
		internal(w, "people", err)
		return
	}
	invites, err := a.Auth.DB.OpenInvites(ctx, a.Now())
	if err != nil {
		internal(w, "people", err)
		return
	}
	out := People{Users: []PersonInfo{}, Invites: []OpenInvite{}}
	for _, u := range users {
		info := userInfo(u)
		person := PersonInfo{
			ID: u.ID, Name: u.Name, Role: u.Role, Username: info.Username, HasPassword: info.HasPassword,
			Me: u.ID == p.UserID, CreatedAt: u.CreatedAt, Phones: []PhoneInfo{},
		}
		for _, dv := range phones[u.ID] {
			person.Phones = append(person.Phones, phoneInfo(dv, p.DeviceID))
			if person.LastSeenAt == nil || dv.LastSeenAt.After(*person.LastSeenAt) {
				seen := dv.LastSeenAt
				person.LastSeenAt = &seen
			}
		}
		out.Users = append(out.Users, person)
	}
	for _, in := range invites {
		out.Invites = append(out.Invites, openInvite(in))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

type roleRequest struct {
	Role string `json:"role"`
}

func (a *API) changeRole(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.admin(w, r); !ok {
		return
	}
	var req roleRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Role != db.RoleAdmin && req.Role != db.RoleMember {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "role must be admin or member.")
		return
	}
	a.personChange(w, "change role", a.Auth.DB.SetRole(r.Context(), r.PathValue("id"), req.Role))
}

func (a *API) removePerson(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.admin(w, r); !ok {
		return
	}
	a.personChange(w, "remove person", a.Auth.DB.DeleteUser(r.Context(), r.PathValue("id")))
}

func (a *API) personChange(w http.ResponseWriter, what string, err error) {
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, db.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such person.")
	case errors.Is(err, db.ErrLastAdmin):
		httpx.WriteError(w, http.StatusConflict, "last_admin", "This is the only admin. Make someone else admin first.")
	default:
		internal(w, what, err)
	}
}

type newPasswordRequest struct {
	Username string `json:"username"`
}

// NewPassword is a password made up for someone, shown only now; the server keeps only its
// hash.
type NewPassword struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// newPassword gives someone who forgot their password a new one, for the admin to hand over.
// Admins change their own in Settings, with the current one.
func (a *API) newPassword(w http.ResponseWriter, r *http.Request) {
	p, ok := a.admin(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if id == p.UserID {
		httpx.WriteError(w, http.StatusForbidden, "forbidden", "Change your own password in Settings, with the current one.")
		return
	}
	var req newPasswordRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	username, password, err := a.Auth.ResetPassword(r.Context(), id, req.Username)
	var input *auth.InputError
	switch {
	case err == nil:
		log.Printf("api: admin %s gave %s a new password", p.UserID, id)
		httpx.WriteJSON(w, http.StatusOK, NewPassword{Username: username, Password: password})
	case errors.As(err, &input):
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "The "+input.Field+" "+input.Problem+".")
	case errors.Is(err, db.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such person.")
	case errors.Is(err, auth.ErrUsernameTaken):
		httpx.WriteError(w, http.StatusConflict, "username_taken", "Someone else has that username.")
	default:
		internal(w, "new password", err)
	}
}

// signOutPhone signs out a phone or browser: admins anyone's, everyone else their own.
func (a *API) signOutPhone(w http.ResponseWriter, r *http.Request) {
	p, ok := a.device(w, r)
	if !ok {
		return
	}
	dv, err := a.Auth.DB.DeviceByID(r.Context(), r.PathValue("id"))
	if errors.Is(err, db.ErrNotFound) || (err == nil && (dv.RevokedAt != nil || (p.Role != db.RoleAdmin && dv.UserID != p.UserID))) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such phone or browser.")
		return
	}
	if err == nil {
		err = a.Auth.DB.RevokeDevice(r.Context(), dv.ID, a.Now())
	}
	if err != nil {
		internal(w, "sign out phone", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type newInviteRequest struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

// NewInvite is a fresh invite. The token and the link are shown only now; the server keeps
// only the token's hash.
type NewInvite struct {
	Token  string     `json:"token"`
	Link   string     `json:"link"`
	Invite OpenInvite `json:"invite"`
}

func (a *API) invite(w http.ResponseWriter, r *http.Request) {
	p, ok := a.admin(w, r)
	if !ok {
		return
	}
	var req newInviteRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	a.newInvite(w, r, p, req.Name, req.Role, "")
}

func (a *API) invitePhone(w http.ResponseWriter, r *http.Request) {
	p, ok := a.admin(w, r)
	if !ok {
		return
	}
	a.newInvite(w, r, p, "", "", r.PathValue("id"))
}

func (a *API) newInvite(w http.ResponseWriter, r *http.Request, p *auth.Principal, name, role, forUser string) {
	token, in, err := a.Auth.CreateInvite(r.Context(), name, role, forUser, p.UserID, nil, auth.InviteLifetime)
	var input *auth.InputError
	switch {
	case err == nil:
		httpx.WriteJSON(w, http.StatusCreated, NewInvite{Token: token, Link: a.Cfg.PublicURL + "/join#" + token, Invite: openInvite(in)})
	case errors.As(err, &input):
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "The "+input.Field+" "+input.Problem+".")
	case errors.Is(err, db.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such person.")
	default:
		internal(w, "invite", err)
	}
}

func (a *API) withdrawInvite(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.admin(w, r); !ok {
		return
	}
	switch err := a.Auth.DB.RevokeInvite(r.Context(), r.PathValue("id"), a.Now()); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, db.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such invite.")
	default:
		internal(w, "withdraw invite", err)
	}
}

// maxIDs is how many files one request may change; apps send bigger selections in parts.
const maxIDs = 1000

type idsRequest struct {
	IDs []string `json:"ids"`
}

// fileIDsIn reads {"ids": [...]} and checks them.
func fileIDsIn(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	var req idsRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return nil, false
	}
	if len(req.IDs) == 0 || len(req.IDs) > maxIDs {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "Send between 1 and 1000 ids.")
		return nil, false
	}
	for _, id := range req.IDs {
		if !ids.Valid(id) {
			httpx.WriteError(w, http.StatusBadRequest, "bad_request", "That isn't a file id: "+id)
			return nil, false
		}
	}
	return req.IDs, true
}

// Changed says how many of the files a request changed; the others were already so.
type Changed struct {
	Changed int `json:"changed"`
}

func (a *API) deleteFiles(w http.ResponseWriter, r *http.Request) {
	p, ok := a.admin(w, r)
	if !ok {
		return
	}
	fileIDs, ok := fileIDsIn(w, r)
	if !ok {
		return
	}
	files, err := a.Lib.Trash(r.Context(), fileIDs, p.UserID)
	if err != nil {
		internal(w, "delete", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, Changed{Changed: len(files)})
}

// TrashedFile is a file in Recently deleted.
type TrashedFile struct {
	FileInfo
	DeletedAt time.Time `json:"deleted_at"`
	DeletedBy *string   `json:"deleted_by"` // the admin's name, if they still have an account
	PurgeAt   time.Time `json:"purge_at"`   // when it goes for good
}

// Trash is everything in Recently deleted, the most recently deleted first, with the folders
// its files are from: deleted folders aren't in /api/folders any more.
type Trash struct {
	Files     []TrashedFile   `json:"files"`
	Folders   []TrashedFolder `json:"folders"`
	TrashDays int             `json:"trash_days"`
}

// TrashedFolder is a folder that files in Recently deleted are from.
type TrashedFolder struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"` // the folder went too; restoring a file brings it back
}

func (a *API) trash(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.admin(w, r); !ok {
		return
	}
	files, err := a.Auth.DB.Trashed(r.Context())
	if err != nil {
		internal(w, "trash", err)
		return
	}
	names, err := a.userNames(r)
	if err != nil {
		internal(w, "trash", err)
		return
	}
	out := Trash{Files: []TrashedFile{}, Folders: []TrashedFolder{}, TrashDays: a.Cfg.TrashDays}
	var folderIDs []string
	for _, f := range files {
		if !slices.Contains(folderIDs, f.FolderID) {
			folderIDs = append(folderIDs, f.FolderID)
		}
	}
	folders, err := a.Auth.DB.FoldersByID(r.Context(), folderIDs)
	if err != nil {
		internal(w, "trash", err)
		return
	}
	for _, f := range folders {
		out.Folders = append(out.Folders, TrashedFolder{ID: f.ID, Name: f.Name, Deleted: f.DeletedAt != nil})
	}
	for _, f := range files {
		t := TrashedFile{FileInfo: fileInfo(f, names)}
		if f.DeletedAt != nil {
			t.DeletedAt = *f.DeletedAt
			t.PurgeAt = f.DeletedAt.Add(time.Duration(a.Cfg.TrashDays) * 24 * time.Hour)
		}
		if name, ok := names[f.DeletedBy]; ok {
			t.DeletedBy = &name
		}
		out.Files = append(out.Files, t)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) restore(w http.ResponseWriter, r *http.Request) {
	a.trashChange(w, r, "restore", a.Lib.Restore)
}

func (a *API) purge(w http.ResponseWriter, r *http.Request) {
	a.trashChange(w, r, "purge", a.Lib.Purge)
}

func (a *API) trashChange(w http.ResponseWriter, r *http.Request, what string, change func(context.Context, []string) ([]db.File, error)) {
	if _, ok := a.admin(w, r); !ok {
		return
	}
	fileIDs, ok := fileIDsIn(w, r)
	if !ok {
		return
	}
	files, err := change(r.Context(), fileIDs)
	if err != nil {
		internal(w, what, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, Changed{Changed: len(files)})
}

// StorageInfo is where the files are, how full the drive is, and what an admin should know
// about it.
type StorageInfo struct {
	StorageDir string           `json:"storage_dir"`
	FSType     string           `json:"fs_type"`
	TotalBytes int64            `json:"total_bytes"`
	FreeBytes  int64            `json:"free_bytes"`
	Files      int              `json:"files"` // in the library
	Bytes      int64            `json:"bytes"`
	TrashFiles int              `json:"trash_files"`
	TrashBytes int64            `json:"trash_bytes"`
	TrashDays  int              `json:"trash_days"`
	Warnings   []StorageWarning `json:"warnings"` // problems first
}

// StorageWarning is what `share check` finds: a problem keeps Share from working well, a
// warning is worth knowing. The website and the app translate the code
// (contract/storage_warnings.json); the message is for the console, in English.
type StorageWarning struct {
	Code    string `json:"code"`
	Level   string `json:"level"` // "problem" or "warning"
	Message string `json:"message"`
}

func (a *API) storageInfo(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.admin(w, r); !ok {
		return
	}
	ctx := r.Context()
	info := StorageInfo{StorageDir: a.Cfg.StorageDir, TrashDays: a.Cfg.TrashDays}
	var err error
	if info.Files, info.Bytes, err = a.Auth.DB.LibraryStats(ctx); err == nil {
		info.TrashFiles, info.TrashBytes, err = a.Auth.DB.TrashStats(ctx)
	}
	if err != nil {
		internal(w, "storage", err)
		return
	}
	if fs, err := storage.Stat(a.Cfg.StorageDir); err == nil {
		info.FSType, info.TotalBytes, info.FreeBytes = fs.Type, fs.Total, fs.Free
	}
	report := a.CheckStorage()
	info.Warnings = make([]StorageWarning, 0, len(report.Problems)+len(report.Warnings))
	for _, f := range report.Problems {
		info.Warnings = append(info.Warnings, StorageWarning{Code: f.Code, Level: "problem", Message: f.Message})
	}
	for _, f := range report.Warnings {
		info.Warnings = append(info.Warnings, StorageWarning{Code: f.Code, Level: "warning", Message: f.Message})
	}
	httpx.WriteJSON(w, http.StatusOK, info)
}
