package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/ids"
)

// askZip asks for a download of fileIDs as one ZIP.
func (e *env) askZip(token string, fileIDs ...string) response {
	e.t.Helper()
	return e.postJSON(nil, "/api/downloads", token, map[string][]string{"ids": fileIDs}, nil)
}

// zipFiles uploads files over two days, with names that collide, and returns their contents
// by id.
func zipFiles(t *testing.T, e *env) map[string][]byte {
	t.Helper()
	token, _ := e.unlockApp(e.newPin(db.PinPermanent).Code, "")
	up := tus{e, token}
	sent := map[string][]byte{}
	send := func(name string, size int) string {
		data := randomBytes(t, size)
		id := up.sendFile(name, data)
		sent[id] = data
		return id
	}
	send("IMG_0001.jpg", 3000)
	send("IMG_0001.jpg", 5000) // the drive numbers it: IMG_0001 (2).jpg
	send("Ärger.txt", 10)      // the drive tells these two apart, Windows wouldn't
	send("ärger.txt", 20)
	e.clock.Add(24 * time.Hour)
	send("Kündigung.pdf", 7000)
	return sent
}

func TestDownloadAsZip(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	sent := zipFiles(t, e)
	var asked []string
	for id := range sent {
		asked = append(asked, id)
	}
	token, _ := e.unlockApp(e.newPin(db.PinPermanent).Code, "")
	gone := tus{e, token}.sendFile("gone.txt", randomBytes(t, 50))
	if r := e.postJSON(nil, "/api/files/delete", admin.token, map[string][]string{"ids": {gone}}, nil); r.status != http.StatusOK {
		t.Fatalf("delete: %d %s", r.status, r.body)
	}
	r := e.askZip(admin.token, append(asked, gone, ids.New())...) // a deleted one, and one that never was
	if r.status != http.StatusCreated {
		t.Fatalf("ask: %d %s", r.status, r.body)
	}
	info := r.json(t)
	assertShape(t, "downloads", readFixture(t, "api/downloads.json")["response"], info)
	if info["count"] != 5.0 || info["name"] != "Share 2026-09-28.zip" {
		t.Errorf("info: %v", info)
	}
	paths := map[string]bool{}
	for _, f := range info["files"].([]any) {
		paths[f.(map[string]any)["path"].(string)] = true
	}
	for _, want := range []string{"2026-09-27/IMG_0001.jpg", "2026-09-27/IMG_0001 (2).jpg", "2026-09-27/Ärger.txt",
		"2026-09-27/ärger (2).txt", "2026-09-28/Kündigung.pdf"} {
		if !paths[want] {
			t.Errorf("no %s in %v", want, paths)
		}
	}

	get := e.do(nil, "GET", "/api/downloads/"+info["id"].(string), admin.token, nil, nil)
	if get.status != http.StatusOK || get.header.Get("Content-Type") != "application/zip" {
		t.Fatalf("download: %d %s", get.status, get.header.Get("Content-Type"))
	}
	if n, _ := strconv.Atoi(get.header.Get("Content-Length")); n != len(get.body) || float64(n) != info["size"] {
		t.Errorf("Content-Length %s, %d bytes, size %v", get.header.Get("Content-Length"), len(get.body), info["size"])
	}
	if cd := get.header.Get("Content-Disposition"); cd != `attachment; filename="Share 2026-09-28.zip"; filename*=UTF-8''Share%202026-09-28.zip` {
		t.Errorf("Content-Disposition %q", cd)
	}
	if cc := get.header.Get("Cache-Control"); !strings.Contains(cc, "no-store") || !strings.Contains(cc, "no-transform") {
		t.Errorf("Cache-Control %q", cc)
	}
	if !strings.HasPrefix(get.header.Get("ETag"), `"z1-`) {
		t.Errorf("ETag %q", get.header.Get("ETag"))
	}
	// Stored, with the checksum before the data, so every unzip reads it.
	if flags := binary.LittleEndian.Uint16(get.body[6:8]); flags&0x0008 != 0 || flags&0x0800 == 0 {
		t.Errorf("flags of the first entry: %04x", flags)
	}
	zr, err := zip.NewReader(bytes.NewReader(get.body), int64(len(get.body)))
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]string{}
	for _, f := range info["files"].([]any) {
		f := f.(map[string]any)
		byPath[f["path"].(string)] = f["id"].(string)
	}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc) // archive/zip checks the CRC-32 at the end
		rc.Close()
		if err != nil || f.Method != zip.Store || !bytes.Equal(data, sent[byPath[f.Name]]) {
			t.Errorf("%s: %v, method %d, %d bytes", f.Name, err, f.Method, len(data))
		}
	}
	if len(zr.File) != 5 {
		t.Errorf("%d files in the ZIP", len(zr.File))
	}
	// The checksums are kept for the next download.
	for id := range sent {
		if _, ok, _ := e.app.DB.FileCRC32(context.Background(), id); !ok {
			t.Errorf("no CRC-32 kept for %s", id)
		}
	}
}

func TestZipDownloadResumes(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	sent := zipFiles(t, e)
	var asked []string
	for id := range sent {
		asked = append(asked, id)
	}
	path := "/api/downloads/" + e.askZip(admin.token, asked...).json(t)["id"].(string)
	whole := e.do(nil, "GET", path, admin.token, nil, nil)
	etag := whole.header.Get("ETag")

	for _, from := range []int{0, 31, 4000, len(whole.body) - 30, len(whole.body) - 1} {
		part := e.do(nil, "GET", path, admin.token, nil, map[string]string{"Range": "bytes=" + strconv.Itoa(from) + "-", "If-Range": etag})
		if part.status != http.StatusPartialContent || !bytes.Equal(part.body, whole.body[from:]) {
			t.Errorf("from %d: %d, %d bytes", from, part.status, len(part.body))
		}
	}
	changed := e.do(nil, "GET", path, admin.token, nil, map[string]string{"Range": "bytes=100-", "If-Range": `"z1-other"`})
	if changed.status != http.StatusOK || !bytes.Equal(changed.body, whole.body) {
		t.Errorf("If-Range with another ETag: %d, %d bytes", changed.status, len(changed.body))
	}
	if r := e.do(nil, "GET", path, admin.token, nil, map[string]string{"Range": "bytes=" + strconv.Itoa(len(whole.body)) + "-"}); r.status != http.StatusRequestedRangeNotSatisfiable {
		t.Errorf("past the end: %d", r.status)
	}
	head := e.do(nil, "HEAD", path, admin.token, nil, nil)
	if head.status != http.StatusOK || head.header.Get("Content-Length") != strconv.Itoa(len(whole.body)) || head.header.Get("ETag") != etag {
		t.Errorf("HEAD: %d, Content-Length %s", head.status, head.header.Get("Content-Length"))
	}
}

func TestZipChangesWhenFilesAreDeleted(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	sent := zipFiles(t, e)
	var asked []string
	for id := range sent {
		asked = append(asked, id)
	}
	path := "/api/downloads/" + e.askZip(admin.token, asked...).json(t)["id"].(string)
	before := e.do(nil, "GET", path, admin.token, nil, nil)
	if r := e.postJSON(nil, "/api/files/delete", admin.token, map[string][]string{"ids": asked[:1]}, nil); r.status != http.StatusOK {
		t.Fatalf("delete: %d %s", r.status, r.body)
	}
	after := e.do(nil, "GET", path, admin.token, nil, map[string]string{"Range": "bytes=100-", "If-Range": before.header.Get("ETag")})
	if after.status != http.StatusOK || after.header.Get("ETag") == before.header.Get("ETag") || len(after.body) >= len(before.body) {
		t.Errorf("after deleting a file: %d, ETag %s, %d bytes", after.status, after.header.Get("ETag"), len(after.body))
	}
	if zr, err := zip.NewReader(bytes.NewReader(after.body), int64(len(after.body))); err != nil || len(zr.File) != 4 {
		t.Errorf("the ZIP after deleting one: %v", err)
	}
}

func TestZipDownloadsBelongToTheirPerson(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	maria := e.accept(e.invite(admin, "Maria", db.RoleMember), "Pixel 8")
	sent := zipFiles(t, e)
	var asked []string
	for id := range sent {
		asked = append(asked, id)
	}
	path := "/api/downloads/" + e.askZip(admin.token, asked...).json(t)["id"].(string)
	wantStatus(t, "someone else", e.get(path, maria.token), http.StatusNotFound, "not_found")
	pinToken, _ := e.unlockApp(e.newPin(db.PinDay).Code, "")
	wantStatus(t, "a PIN", e.askZip(pinToken, asked...), http.StatusForbidden, "forbidden")
	wantStatus(t, "none of them", e.askZip(admin.token, ids.New()), http.StatusNotFound, "not_found")
	wantStatus(t, "no ids", e.askZip(admin.token), http.StatusBadRequest, "bad_request")
	wantStatus(t, "not an id", e.askZip(admin.token, "../x"), http.StatusBadRequest, "bad_request")
	tooMany := make([]string, maxZipFilesForTest+1)
	for i := range tooMany {
		tooMany[i] = ids.New()
	}
	wantStatus(t, "too many", e.askZip(admin.token, tooMany...), http.StatusBadRequest, "bad_request")

	e.clock.Add(25 * time.Hour)
	wantStatus(t, "a day later", e.get(path, admin.token), http.StatusNotFound, "not_found")
}

// maxZipFilesForTest is the API's limit of files per ZIP.
const maxZipFilesForTest = 10_000

func TestFolderSavingResumesFiles(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	browser := e.webBrowser()
	e.postJSON(browser, "/api/invites/accept", "", map[string]string{"token": e.invite(admin, "Maria", db.RoleMember), "client": "web"}, fromPage)
	sent := zipFiles(t, e)
	for id, data := range sent {
		r := e.do(browser, "GET", "/api/files/"+id+"/content", "", nil, map[string]string{"Range": "bytes=5-", "If-Range": `"` + id + `"`})
		if r.status != http.StatusPartialContent || !bytes.Equal(r.body, data[5:]) {
			t.Errorf("%s from byte 5: %d, %d bytes", id, r.status, len(r.body))
		}
	}
}
