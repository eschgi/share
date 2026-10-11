package app

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/ids"
)

// sendTaken uploads a file that its phone says was taken at taken (none when zero).
func (c tus) sendTaken(name string, data []byte, taken time.Time) string {
	c.e.t.Helper()
	meta := "filename " + base64.StdEncoding.EncodeToString([]byte(name))
	if strings.HasPrefix(c.token, ids.PrefixDevice) {
		meta += ",folder " + base64.StdEncoding.EncodeToString([]byte(c.e.firstFolder().ID))
	}
	if !taken.IsZero() {
		meta += ",takenAt " + base64.StdEncoding.EncodeToString([]byte(strconv.FormatInt(taken.UnixMilli(), 10)))
	}
	r := c.e.do(nil, "POST", "/tus/", c.token, nil, c.headers(map[string]string{"Upload-Length": strconv.Itoa(len(data)), "Upload-Metadata": meta}))
	if r.status != http.StatusCreated {
		c.e.t.Fatalf("create %s: %d %s", name, r.status, r.body)
	}
	loc := r.header.Get("Location")
	c.send(loc, data, 0, len(data))
	return idOf(loc)
}

// The library sorted by when files arrived, when they were taken, or who sent them, newest or
// oldest first (docs/sort-plan.md); the files come in the groups' order, page by page.
func TestSortingTheLibrary(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	maria := e.accept(e.invite(admin, "Maria", db.RoleMember), "Maria's phone")
	pin := e.newPin(db.PinPermanent)
	pinToken, _ := e.unlockApp(pin.Code, "")
	rome, _ := time.LoadLocation("Europe/Rome")

	// 27 September: Stefan sends a photo of June 2025, a guest one without a date.
	old := tus{e, admin.token}.sendTaken("IMG_4471.jpg", randomBytes(t, 400), time.Date(2025, 6, 14, 18, 32, 0, 0, rome))
	e.clock.Add(time.Minute)
	guest := tus{e, pinToken}.sendTaken("guest.jpg", randomBytes(t, 300), time.Time{})
	// 28 September: Maria sends one taken on the 26th, just before midnight in Rome; then Stefan a document.
	e.clock.Add(24 * time.Hour)
	late := tus{e, maria.token}.sendTaken("IMG_5000.jpg", randomBytes(t, 200), time.Date(2026, 9, 26, 23, 50, 0, 0, rome))
	e.clock.Add(time.Minute)
	doc := tus{e, admin.token}.sendTaken("Menu.pdf", randomBytes(t, 100), time.Time{})

	type group struct{ key, from, lastDay string }
	groups := func(query string) []group {
		t.Helper()
		r := e.get("/api/library?"+query, admin.token)
		wantStatus(t, query, r, http.StatusOK, "")
		var out []group
		for _, g := range r.json(t)["groups"].([]any) {
			m := g.(map[string]any)
			from, _ := m["from"].(string)
			out = append(out, group{m["key"].(string), from, m["last_day"].(string)})
		}
		return out
	}
	// files reads every page, limit files at a time.
	files := func(query string, limit int) []string {
		t.Helper()
		var out []string
		cursor := ""
		for range 10 {
			r := e.get("/api/files?"+query+"&limit="+strconv.Itoa(limit)+cursor, admin.token)
			wantStatus(t, query, r, http.StatusOK, "")
			page := r.json(t)
			for _, f := range page["files"].([]any) {
				out = append(out, f.(map[string]any)["id"].(string))
			}
			next, ok := page["next_cursor"].(string)
			if !ok {
				return out
			}
			cursor = "&cursor=" + next
		}
		t.Fatalf("%s: no end", query)
		return nil
	}
	same := func(name string, got, want any) {
		t.Helper()
		if !equalish(got, want) {
			t.Errorf("%s:\n got %v\nwant %v", name, got, want)
		}
	}

	same("received", groups(""), []group{{"2026-09-28", "", "2026-09-28"}, {"2026-09-27", "", "2026-09-27"}})
	same("received files", files("", 1), []string{doc, late, guest, old})
	same("received, oldest first", files("order=oldest", 3), []string{old, guest, late, doc})

	same("taken", groups("sort=taken"), []group{
		{"2026-09-28", "", "2026-09-28"}, {"2026-09-27", "", "2026-09-27"}, {"2026-09-26", "", "2026-09-28"}, {"2025-06-14", "", "2026-09-27"},
	})
	same("taken files", files("sort=taken", 1), []string{doc, guest, late, old})
	same("taken, oldest first", files("sort=taken&order=oldest", 2), []string{old, late, guest, doc})
	same("taken, one day", files("sort=taken&group=2026-09-26", 10), []string{late})

	same("sender", groups("sort=sender"), []group{{admin.userID, "Admin", "2026-09-28"}, {maria.userID, "Maria", "2026-09-28"}, {db.SenderPIN, "", "2026-09-27"}})
	same("sender files", files("sort=sender", 1), []string{doc, old, late, guest})
	same("sender, oldest first", groups("sort=sender&order=oldest"), []group{{admin.userID, "Admin", "2026-09-28"}, {db.SenderPIN, "", "2026-09-27"}, {maria.userID, "Maria", "2026-09-28"}})
	same("sender, oldest first files", files("sort=sender&order=oldest", 2), []string{old, doc, guest, late})
	same("the guests", files("sort=sender&group=pin", 10), []string{guest})
	ids := e.get("/api/files/ids?sort=sender&group="+maria.userID, admin.token).json(t)["ids"].([]any)
	same("Maria's ids", ids, []any{late})

	info := e.get("/api/files/"+late, admin.token).json(t)
	same("taken_at", info["taken_at"], "2026-09-26T21:50:00Z")
	same("taken_day, in Rome", info["taken_day"], "2026-09-26")
	same("from_id", info["from_id"], maria.userID)
	if g := e.get("/api/files/"+guest, admin.token).json(t); g["taken_at"] != nil || g["from_id"] != nil {
		t.Errorf("the guest's photo: %v", g)
	}

	for _, q := range []string{"sort=size", "order=sideways", "sort=taken&group=yesterday", "sort=sender&group=someone"} {
		wantStatus(t, q, e.get("/api/files?"+q, admin.token), http.StatusBadRequest, "bad_request")
	}
	wantStatus(t, "a cursor from elsewhere", e.get("/api/files?cursor=bm9wZQ", admin.token), http.StatusBadRequest, "bad_request")
}

// equalish compares what two values print as, so that a []any from JSON matches a []string.
func equalish(got, want any) bool { return fmt.Sprint(got) == fmt.Sprint(want) }
