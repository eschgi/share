package app

import (
	"bytes"
	"context"
	"encoding/json"
	"image/jpeg"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/api"
	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/db"
)

// noRedirects is a copy of a client that stops at a redirect, to look at it.
func noRedirects(c *http.Client) *http.Client {
	copied := *c
	copied.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &copied
}

func TestS3FilesComeFromTheBucket(t *testing.T) {
	e := newS3Env(t)
	admin := e.admin()
	data := randomBytes(t, int(e.part+100))
	id := s3Client{e: e, token: admin.token}.send("Kündigung.pdf", data, e.firstFolder().ID)

	// The app asks for the link, and fetches it without its key.
	if r := e.get("/api/files/"+id+"/content", admin.token); r.status != http.StatusConflict || r.errorCode() != "s3_use_url" {
		t.Errorf("/content with the key: %d %s", r.status, r.body)
	}
	r := e.get("/api/s3/files/"+id+"/url", admin.token)
	if r.status != http.StatusOK {
		t.Fatalf("url: %d %s", r.status, r.body)
	}
	assertShape(t, "url", readFixture(t, "api/s3_file_url.json")["response"], r.json(t))
	var link api.FileURL
	json.Unmarshal(r.body, &link)
	if until := time.Until(link.ExpiresAt); until < 11*time.Hour || until > 12*time.Hour {
		t.Errorf("the link works for %v", until)
	}
	res, err := s3Client{e: e}.bucketClient().Get(link.URL)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !bytes.Equal(got, data) ||
		res.Header.Get("Content-Disposition") != `attachment; filename="K_ndigung.pdf"; filename*=UTF-8''K%C3%BCndigung.pdf` ||
		res.Header.Get("Content-Type") != "application/pdf" {
		t.Errorf("GET the link: %d, %d bytes, %v", res.StatusCode, len(got), res.Header)
	}

	// A browser goes on to the bucket by itself, and nothing keeps the redirect.
	e.setPassword(admin.token, "stefan", "correct horse battery")
	browser := e.webBrowser()
	if r := e.signInWeb(browser, "stefan", "correct horse battery"); r.status != http.StatusOK {
		t.Fatalf("sign in: %d %s", r.status, r.body)
	}
	r = e.do(noRedirects(browser), "GET", "/api/files/"+id+"/content", "", nil, nil)
	if r.status != http.StatusFound || !strings.Contains(r.header.Get("Location"), "/files/"+id+"?") || r.header.Get("Cache-Control") != "private, no-store" {
		t.Errorf("/content in a browser: %d, Location %q, Cache-Control %q", r.status, r.header.Get("Location"), r.header.Get("Cache-Control"))
	}
	if e.fake != nil {
		r = e.do(browser, "GET", "/api/files/"+id+"/content", "", nil, map[string]string{"Range": "bytes=10-19"})
		if r.status != http.StatusPartialContent || !bytes.Equal(r.body, data[10:20]) {
			t.Errorf("following it with Range: %d, %q", r.status, r.body)
		}
	}

	// A PIN that shows its folder may fetch its files too; one that doesn't can only send.
	shows, err := e.app.Auth.CreatePin(context.Background(), auth.PinSpec{Kind: db.PinPermanent, FolderID: e.firstFolder().ID, ShowsFolder: true}, "test")
	if err != nil {
		t.Fatal(err)
	}
	guest, _ := e.unlockApp(shows.Code, "")
	if r := e.get("/api/s3/files/"+id+"/url", guest); r.status != http.StatusOK {
		t.Errorf("a PIN that shows its folder: %d %s", r.status, r.body)
	}
	hidden, _ := e.unlockApp(e.newPin(db.PinPermanent).Code, "")
	if r := e.get("/api/s3/files/"+id+"/url", hidden); r.status != http.StatusForbidden || r.errorCode() != "forbidden" {
		t.Errorf("a PIN that doesn't, which can't fetch /content either: %d %s", r.status, r.body)
	}
}

func TestS3ModeSendsNoZip(t *testing.T) {
	e := newS3Env(t)
	admin := e.admin()
	phone := s3Client{e: e, token: admin.token}
	ids := []string{phone.send("a.jpg", []byte("aaa"), e.firstFolder().ID), phone.send("b.jpg", []byte("bbb"), e.firstFolder().ID)}
	r := e.askZip(admin.token, ids...)
	if r.status != http.StatusCreated {
		t.Fatalf("POST /api/downloads, which saving into a folder uses: %d %s", r.status, r.body)
	}
	if r := e.get("/api/downloads/"+r.json(t)["id"].(string), admin.token); r.status != http.StatusConflict || r.errorCode() != "s3_no_zip" {
		t.Errorf("the ZIP: %d %s", r.status, r.body)
	}
}

func TestS3ModeSaysWhereTheFilesAre(t *testing.T) {
	e := newS3Env(t)
	info := e.get("/api/info", "").json(t)
	assertShape(t, "info", readFixture(t, "api/info.json")["response"], info)
	if info["storage"] != "s3" || info["api_version"].(float64) != 3 || info["max_file_size_bytes"].(float64) != 5<<40 || info["chunk_size_bytes"].(float64) != 20<<20 {
		t.Errorf("info %v", info)
	}
	admin := e.admin()
	r := e.get("/api/admin/storage", admin.token)
	assertShape(t, "storage", readFixture(t, "api/storage.json")["response"], r.json(t))
	got := r.json(t)
	if got["storage"] != "s3" || got["s3_bucket"] != e.cfg.S3.Bucket || got["s3_endpoint"] != e.cfg.S3.Endpoint || got["storage_dir"] != "" || got["total_bytes"].(float64) != 0 {
		t.Errorf("storage %v", got)
	}
	disk := newEnv(t)
	if r := disk.get("/api/s3/files/aaaaaaaaaaaaaaaaaaaaaaaaaa/url", disk.admin().token); r.status != http.StatusNotFound || r.errorCode() != "not_found" {
		t.Errorf("links on a drive: %d %s", r.status, r.body)
	}
}

// Moving, renaming, deleting and restoring through the API leave the bucket alone; only a
// purge removes an object.
func TestS3LibraryThroughTheAPI(t *testing.T) {
	e := newS3Env(t)
	fake := e.needFake()
	admin := e.admin()
	phone := s3Client{e: e, token: admin.token}
	a := phone.send("a.jpg", []byte("aaa"), e.firstFolder().ID)
	b := phone.send("b.jpg", []byte("bbb"), e.firstFolder().ID)
	before := len(fake.Calls())

	folder := e.sendJSON("POST", "/api/folders", admin.token, map[string]any{"name": "Holidays"})
	if folder.status != http.StatusCreated {
		t.Fatalf("new folder: %d %s", folder.status, folder.body)
	}
	holidays := folder.json(t)["id"].(string)
	for _, step := range []struct {
		method, path string
		body         any
		want         int
	}{
		{"POST", "/api/files/move", map[string]any{"ids": []string{a}, "folder": holidays}, http.StatusOK},
		{"PATCH", "/api/folders/" + holidays, map[string]any{"name": "Summer"}, http.StatusOK},
		{"POST", "/api/files/delete", map[string]any{"ids": []string{b}}, http.StatusOK},
		{"POST", "/api/trash/restore", map[string]any{"ids": []string{b}}, http.StatusOK},
		{"POST", "/api/files/delete", map[string]any{"ids": []string{a, b}}, http.StatusOK},
	} {
		if r := e.sendJSON(step.method, step.path, admin.token, step.body); r.status != step.want {
			t.Fatalf("%s %s: %d %s", step.method, step.path, r.status, r.body)
		}
	}
	if calls := fake.Calls()[before:]; len(calls) != 0 {
		t.Errorf("the bucket was asked: %v", calls)
	}
	if r := e.sendJSON("POST", "/api/trash/purge", admin.token, map[string]any{"ids": []string{a}}); r.status != http.StatusOK {
		t.Fatalf("purge: %d %s", r.status, r.body)
	}
	if calls := fake.Calls()[before:]; !slices.Equal(calls, []string{"DeleteObject " + e.app.S3.Key(a)}) {
		t.Errorf("purging asked the bucket %v", calls)
	}
	if _, ok := fake.Object(e.app.S3.Key(b)); !ok {
		t.Error("the trashed file's object is gone")
	}
}

func TestS3ServerMakesThumbnailsFromTheBucket(t *testing.T) {
	e := newS3Env(t)
	admin := e.admin()
	id := s3Client{e: e, token: admin.token}.send("IMG_3.jpg", jpegBytes(t, 600, 400, 1), e.firstFolder().ID)
	e.clock.Add(2 * time.Minute)
	if n, err := e.app.Thumbs.MakePending(context.Background()); err != nil || n != 1 {
		t.Fatalf("MakePending: %d, %v", n, err)
	}
	f := e.file(id)
	if f.Thumb != db.ThumbServer || *f.Width != 600 {
		t.Fatalf("thumb %s, width %v", f.Thumb, f.Width)
	}
	r := e.get("/api/files/"+id+"/thumb", admin.token)
	if _, err := jpeg.Decode(bytes.NewReader(r.body)); r.status != http.StatusOK || err != nil {
		t.Errorf("thumbnail: %d, %v", r.status, err)
	}
}
