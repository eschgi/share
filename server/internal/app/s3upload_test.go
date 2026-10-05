package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/upload"
)

func TestS3UploadGoesStraightToTheBucket(t *testing.T) {
	e := newS3Env(t)
	admin := e.admin()
	phone := s3Client{e: e, token: admin.token}
	data := randomBytes(t, int(2*e.part+e.part/2))
	plan := phone.start("IMG_1.jpg", int64(len(data)), e.firstFolder().ID)
	if plan.Parts != 3 || plan.PartSize != e.part || len(plan.URLs) != 3 || plan.URLs[2].Size != e.part/2 {
		t.Fatalf("plan %+v", plan)
	}
	if until := time.Until(plan.ExpiresAt); until < 50*time.Minute || until > time.Hour {
		t.Errorf("the links work for %v", until)
	}
	phone.sendParts(plan, data, 1)
	r := phone.status(plan.ID)
	var st upload.S3UploadStatus
	json.Unmarshal(r.body, &st)
	if r.status != http.StatusOK || st.State != "receiving" || !slices.Equal(st.DoneParts, []int{1}) || st.Parts != 3 {
		t.Fatalf("status %d %s", r.status, r.body)
	}
	phone.sendParts(plan, data, 2, 3)
	if r := phone.complete(plan.ID); r.status != http.StatusOK {
		t.Fatalf("complete: %d %s", r.status, r.body)
	}
	f := e.file(plan.ID)
	if f.State != db.StateReady || f.RelPath != "2026-09-27/IMG_1.jpg" || f.Kind != db.KindPhoto || f.ClientModifiedAt == nil || f.UserID != admin.userID {
		t.Errorf("in the library: %+v", f)
	}
	if !bytes.Equal(e.s3Object(f), data) {
		t.Error("the bucket has other bytes")
	}
	// A complete that is asked again, whose first answer got lost, answers the same.
	if r := phone.complete(plan.ID); r.status != http.StatusOK || r.json(t)["id"] != plan.ID {
		t.Errorf("complete again: %d %s", r.status, r.body)
	}
	json.Unmarshal(phone.status(plan.ID).body, &st)
	if st.State != "complete" || !slices.Equal(st.DoneParts, []int{1, 2, 3}) {
		t.Errorf("status after: %+v", st)
	}
	if r := phone.post("/api/s3/uploads/"+plan.ID+"/parts", map[string]any{"parts": []int{1}}); r.status != http.StatusConflict || r.errorCode() != "s3_upload_finished" {
		t.Errorf("links for a finished upload: %d %s", r.status, r.body)
	}
	if r := e.do(nil, "DELETE", "/api/s3/uploads/"+plan.ID, admin.token, nil, nil); r.status != http.StatusForbidden || r.errorCode() != "forbidden" {
		t.Errorf("cancelling a finished upload: %d %s", r.status, r.body)
	}
}

func TestS3UploadOfManyParts(t *testing.T) {
	e := newS3Env(t)
	if e.fake == nil {
		t.Skip("12 parts of 5 MiB on a real bucket")
	}
	phone := s3Client{e: e, token: e.admin().token}
	data := randomBytes(t, int(11*e.part+1))
	id := phone.send("clip.mp4", data, e.firstFolder().ID)
	if f := e.file(id); f.State != db.StateReady || f.Kind != db.KindVideo || !bytes.Equal(e.s3Object(f), data) {
		t.Errorf("12 parts: %+v", f)
	}
}

func TestS3UploadNeedsEveryPart(t *testing.T) {
	e := newS3Env(t)
	phone := s3Client{e: e, token: e.admin().token}
	data := randomBytes(t, int(e.part+10))
	plan := phone.start("doc.pdf", int64(len(data)), e.firstFolder().ID)
	phone.sendParts(plan, data, 2)
	if r := phone.complete(plan.ID); r.status != http.StatusConflict || r.errorCode() != "s3_parts_missing" {
		t.Fatalf("complete without part 1: %d %s", r.status, r.body)
	}
	// A part of the wrong size never gets into the bucket: its length is signed.
	if status := phone.put(plan.URLs[0], data[:10]); status != http.StatusForbidden {
		t.Errorf("a part of another size: %d", status)
	}
	phone.sendParts(plan, data, 1)
	if r := phone.complete(plan.ID); r.status != http.StatusOK {
		t.Fatalf("complete: %d %s", r.status, r.body)
	}
}

func TestS3UploadsAreTheSendersOwn(t *testing.T) {
	e := newS3Env(t)
	admin := e.admin()
	maria := e.accept(e.invite(admin, "Maria", "member"), "Maria's phone")
	plan := s3Client{e: e, token: admin.token}.start("a.jpg", 10, e.firstFolder().ID)
	other := s3Client{e: e, token: maria.token}
	for name, r := range map[string]response{
		"status":   other.status(plan.ID),
		"parts":    other.post("/api/s3/uploads/"+plan.ID+"/parts", map[string]any{"parts": []int{1}}),
		"complete": other.complete(plan.ID),
		"cancel":   e.do(nil, "DELETE", "/api/s3/uploads/"+plan.ID, maria.token, nil, nil),
	} {
		if r.status != http.StatusNotFound || r.errorCode() != "not_found" {
			t.Errorf("%s by someone else: %d %s", name, r.status, r.body)
		}
	}
	if r := (s3Client{e: e}).create("a.jpg", 10, ""); r.status != http.StatusUnauthorized {
		t.Errorf("without a key: %d %s", r.status, r.body)
	}
	if r := (s3Client{e: e, token: maria.token}).create("a.jpg", 10, ""); r.status != http.StatusBadRequest || r.errorCode() != "bad_request" {
		t.Errorf("without a folder: %d %s", r.status, r.body)
	}
	if r := (s3Client{e: e, token: maria.token}).create("a.jpg", 10, "f4mily5x2k7mbqz4bwdbyj6qsq"); r.status != http.StatusNotFound || r.errorCode() != "folder_gone" {
		t.Errorf("into a folder she doesn't see: %d %s", r.status, r.body)
	}
}

func TestS3UploadWithAPIN(t *testing.T) {
	e := newS3Env(t)
	first := e.newPin(db.PinDay)
	token, _ := e.unlockApp(first.Code, "")
	guest := s3Client{e: e, token: token}
	data := randomBytes(t, int(e.part+5))
	plan := guest.start("IMG_2.jpg", int64(len(data)), "")
	guest.sendParts(plan, data, 1)

	// The 24-hour PIN ends mid-upload; after unlocking another, the upload goes on.
	e.clock.Add(25 * time.Hour)
	if r := guest.status(plan.ID); r.status != http.StatusUnauthorized || r.errorCode() != "session_ended" {
		t.Fatalf("after the PIN ended: %d %s", r.status, r.body)
	}
	second := e.newPin(db.PinDay)
	newToken, v := e.unlockApp(second.Code, token)
	if v["moved_uploads"].(float64) != 1 {
		t.Fatalf("moved_uploads = %v", v["moved_uploads"])
	}
	guest.token = newToken
	guest.sendParts(plan, data, 2)
	if r := guest.complete(plan.ID); r.status != http.StatusOK {
		t.Fatalf("complete after the new unlock: %d %s", r.status, r.body)
	}
	if f := e.file(plan.ID); f.State != db.StateReady || f.PinID != second.ID || !bytes.Equal(e.s3Object(f), data) {
		t.Errorf("the PIN's file: %+v", f)
	}
}

func TestS3UploadCancelAndEmptyFiles(t *testing.T) {
	e := newS3Env(t)
	admin := e.admin()
	phone := s3Client{e: e, token: admin.token}
	plan := phone.start("big.mov", 3*e.part, e.firstFolder().ID)
	if r := e.do(nil, "DELETE", "/api/s3/uploads/"+plan.ID, admin.token, nil, nil); r.status != http.StatusNoContent {
		t.Fatalf("cancel: %d %s", r.status, r.body)
	}
	if _, err := e.app.DB.FileByID(context.Background(), plan.ID); err != db.ErrNotFound {
		t.Errorf("the row after cancelling: %v", err)
	}
	if fake := e.fake; fake != nil && len(fake.Uploads()) != 0 {
		t.Errorf("the bucket still has %v", fake.Uploads())
	}
	if r := phone.status(plan.ID); r.status != http.StatusNotFound {
		t.Errorf("status after cancelling: %d", r.status)
	}

	empty := phone.start("empty.txt", 0, e.firstFolder().ID)
	if empty.Parts != 0 || len(empty.URLs) != 0 {
		t.Fatalf("empty file: %+v", empty)
	}
	if r := phone.complete(empty.ID); r.status != http.StatusOK {
		t.Fatalf("complete: %d %s", r.status, r.body)
	}
	if f := e.file(empty.ID); f.State != db.StateReady || len(e.s3Object(f)) != 0 {
		t.Errorf("empty file: %+v", f)
	}
}

func TestS3UploadWhenTheBucketFails(t *testing.T) {
	e := newS3EnvWith(t, `"upload": {"max_file_size_gib": 1}`)
	fake := e.needFake()
	admin := e.admin()
	phone := s3Client{e: e, token: admin.token}
	if r := phone.create("huge.mkv", 2<<30, e.firstFolder().ID); r.status != http.StatusRequestEntityTooLarge || r.errorCode() != "too_large" {
		t.Errorf("too big: %d %s", r.status, r.body)
	}
	fake.Fail("CreateMultipartUpload", http.StatusServiceUnavailable)
	if r := phone.create("a.jpg", 10, e.firstFolder().ID); r.status != http.StatusServiceUnavailable || r.errorCode() != "s3_unavailable" || r.header.Get("Retry-After") == "" {
		t.Errorf("bucket busy: %d %s", r.status, r.body)
	}
	if n, _ := e.app.DB.UnfinishedCount(context.Background(), "", admin.userID); n != 0 {
		t.Errorf("%d rows were left", n)
	}

	// The bucket dropped an upload: the client hears to start over.
	plan := phone.start("lost.jpg", 10, e.firstFolder().ID)
	if err := e.app.S3.Abort(context.Background(), e.app.S3.Key(plan.ID), e.file(plan.ID).S3UploadID); err != nil {
		t.Fatal(err)
	}
	if r := phone.status(plan.ID); r.status != http.StatusNotFound || r.errorCode() != "not_found" {
		t.Errorf("status of a lost upload: %d %s", r.status, r.body)
	}
	if _, err := e.app.DB.FileByID(context.Background(), plan.ID); err != db.ErrNotFound {
		t.Errorf("the lost upload's row: %v", err)
	}

	// While the bucket is away, status and complete say so, and nothing is lost.
	plan = phone.start("later.jpg", 10, e.firstFolder().ID)
	phone.sendParts(plan, []byte("0123456789"), 1)
	fake.Down(true)
	if r := phone.complete(plan.ID); r.status != http.StatusServiceUnavailable || r.errorCode() != "s3_unavailable" {
		t.Errorf("complete without the bucket: %d %s", r.status, r.body)
	}
	fake.Down(false)
	if r := phone.complete(plan.ID); r.status != http.StatusOK {
		t.Errorf("complete when it is back: %d %s", r.status, r.body)
	}
}

func TestS3UploadsInABrowser(t *testing.T) {
	e := newS3Env(t)
	admin := e.admin()
	e.setPassword(admin.token, "stefan", "correct horse battery")
	browser := e.webBrowser()
	if r := e.signInWeb(browser, "stefan", "correct horse battery"); r.status != http.StatusOK {
		t.Fatalf("sign in: %d %s", r.status, r.body)
	}
	web := s3Client{e: e, client: browser}
	id := web.send("Kündigung.pdf", randomBytes(t, 100), e.firstFolder().ID)
	if f := e.file(id); f.State != db.StateReady || f.Name != "Kündigung.pdf" {
		t.Errorf("sent from a browser: %+v", f)
	}
	// A browser's POST must say JSON, even for complete.
	if r := e.do(browser, "POST", "/api/s3/uploads/"+id+"/complete", "", nil, fromPage); r.status != http.StatusUnsupportedMediaType {
		t.Errorf("complete without JSON: %d %s", r.status, r.body)
	}
}

func TestS3UploadContract(t *testing.T) {
	e := newS3Env(t)
	admin := e.admin()
	phone := s3Client{e: e, token: admin.token}
	created := phone.create("IMG_1.jpg", 2*e.part, e.firstFolder().ID)
	fixture := readFixture(t, "api/s3_upload_create.json")
	assertShape(t, "create", fixture["response"], created.json(t))
	if created.status != int(fixture["status"].(float64)) {
		t.Errorf("create: %d", created.status)
	}
	var plan upload.S3Upload
	json.Unmarshal(created.body, &plan)
	parts := phone.post("/api/s3/uploads/"+plan.ID+"/parts", map[string]any{"parts": []int{2}})
	assertShape(t, "parts", readFixture(t, "api/s3_upload_parts.json")["response"], parts.json(t))
	phone.sendParts(plan, randomBytes(t, int(2*e.part)), 1, 2)
	assertShape(t, "status", readFixture(t, "api/s3_upload_status.json")["response"], phone.status(plan.ID).json(t))
	assertShape(t, "complete", readFixture(t, "api/s3_upload_complete.json")["response"], phone.complete(plan.ID).json(t))
}
