package s3test_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/eschgi/share/server/internal/s3/s3test"
)

func client(t *testing.T, s *s3test.Server, secret string) *minio.Core {
	t.Helper()
	c, err := minio.NewCore(strings.TrimPrefix(s.URL, "https://"), &minio.Options{
		Creds:        credentials.NewStaticV4(s3test.KeyID, secret, ""),
		Secure:       true,
		Region:       s3test.Region,
		BucketLookup: minio.BucketLookupPath,
		Transport:    s.Client().Transport,
		MaxRetries:   1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func code(err error) string { return minio.ToErrorResponse(err).Code }

// put sends a part to a presigned URL, as a browser does, with a body of n bytes.
func put(t *testing.T, s *s3test.Server, u string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, u, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res
}

func partURL(t *testing.T, c *minio.Core, bucket, key, uploadID string, n int, size int) string {
	t.Helper()
	u, err := c.PresignHeader(context.Background(), http.MethodPut, bucket, key, time.Hour,
		url.Values{"partNumber": {strconv.Itoa(n)}, "uploadId": {uploadID}}, http.Header{"Content-Length": {strconv.Itoa(size)}})
	if err != nil {
		t.Fatal(err)
	}
	return u.String()
}

func TestPresignedMultipartUpload(t *testing.T) {
	s := s3test.New(t)
	c := client(t, s, s3test.Secret)
	ctx := context.Background()
	key := "share/files/abc"
	id, err := c.NewMultipartUpload(ctx, s.Bucket, key, minio.PutObjectOptions{ContentType: "image/jpeg", ContentDisposition: `attachment; filename="a.jpg"`})
	if err != nil {
		t.Fatal(err)
	}
	one, two := bytes.Repeat([]byte{1}, 5<<20), []byte("the rest")
	if res := put(t, s, partURL(t, c, s.Bucket, key, id, 1, len(one)), one[:100]); res.StatusCode != http.StatusForbidden {
		t.Errorf("a part of another length: %s", res.Status)
	}
	for n, data := range [][]byte{one, two} {
		if res := put(t, s, partURL(t, c, s.Bucket, key, id, n+1, len(data)), data); res.StatusCode != http.StatusOK {
			t.Fatalf("part %d: %s", n+1, res.Status)
		}
	}
	listed, err := c.ListObjectParts(ctx, s.Bucket, key, id, 0, 0)
	if err != nil || len(listed.ObjectParts) != 2 || listed.ObjectParts[1].Size != int64(len(two)) {
		t.Fatalf("parts %+v, %v", listed.ObjectParts, err)
	}
	var parts []minio.CompletePart
	for _, p := range listed.ObjectParts {
		parts = append(parts, minio.CompletePart{PartNumber: p.PartNumber, ETag: p.ETag})
	}
	if _, err := c.CompleteMultipartUpload(ctx, s.Bucket, key, id, parts, minio.PutObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Object(key)
	if !ok || !bytes.Equal(got, append(one, two...)) {
		t.Fatalf("object of %d bytes, %v", len(got), ok)
	}
	if ct, cd := s.ObjectHeaders(key); ct != "image/jpeg" || cd != `attachment; filename="a.jpg"` {
		t.Errorf("stored with %q, %q", ct, cd)
	}
	if info, err := c.StatObject(ctx, s.Bucket, key, minio.StatObjectOptions{}); err != nil || info.Size != int64(len(got)) {
		t.Errorf("stat %d, %v", info.Size, err)
	}
	if len(s.Uploads()) != 0 {
		t.Errorf("uploads left: %v", s.Uploads())
	}

	// A seekable reader, as the server's thumbnails read originals.
	obj, err := c.Client.GetObject(ctx, s.Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := obj.Seek(int64(len(one)), io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if rest, err := io.ReadAll(obj); err != nil || string(rest) != "the rest" {
		t.Errorf("after the seek %q, %v", rest, err)
	}
	obj.Close()

	// A presigned GET names the file, and resumes with Range.
	link, err := c.PresignedGetObject(ctx, s.Bucket, key, time.Hour, url.Values{
		"response-content-disposition": {`attachment; filename="IMG_1.jpg"`}, "response-content-type": {"image/png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, link.String(), nil)
	req.Header.Set("Range", "bytes=5242880-")
	res, err := s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusPartialContent || string(body) != "the rest" ||
		res.Header.Get("Content-Disposition") != `attachment; filename="IMG_1.jpg"` || res.Header.Get("Content-Type") != "image/png" {
		t.Errorf("GET %s %q, headers %v", res.Status, body, res.Header)
	}
}

func TestAnswersLikeS3(t *testing.T) {
	s := s3test.New(t)
	c := client(t, s, s3test.Secret)
	ctx := context.Background()
	key := "files/x"
	id, err := c.NewMultipartUpload(ctx, s.Bucket, key, minio.PutObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for n, size := range []int{1000, 10} {
		if res := put(t, s, partURL(t, c, s.Bucket, key, id, n+1, size), make([]byte, size)); res.StatusCode != http.StatusOK {
			t.Fatal(res.Status)
		}
	}
	listed, _ := c.ListObjectParts(ctx, s.Bucket, key, id, 0, 0)
	parts := []minio.CompletePart{{PartNumber: 1, ETag: listed.ObjectParts[0].ETag}, {PartNumber: 2, ETag: listed.ObjectParts[1].ETag}}
	if _, err := c.CompleteMultipartUpload(ctx, s.Bucket, key, id, parts, minio.PutObjectOptions{}); code(err) != "EntityTooSmall" {
		t.Errorf("a small first part: %v", err)
	}
	s.SetMinPartSize(100)
	wrong := []minio.CompletePart{{PartNumber: 1, ETag: `"0000"`}, parts[1]}
	if _, err := c.CompleteMultipartUpload(ctx, s.Bucket, key, id, wrong, minio.PutObjectOptions{}); code(err) != "InvalidPart" {
		t.Errorf("a part that was sent again: %v", err)
	}
	if _, err := c.CompleteMultipartUpload(ctx, s.Bucket, key, id, []minio.CompletePart{parts[1], parts[0]}, minio.PutObjectOptions{}); code(err) != "InvalidPartOrder" {
		t.Errorf("parts out of order: %v", err)
	}
	if err := c.AbortMultipartUpload(ctx, s.Bucket, key, id); err != nil {
		t.Fatal(err)
	}
	if err := c.AbortMultipartUpload(ctx, s.Bucket, key, id); code(err) != "NoSuchUpload" {
		t.Errorf("aborting twice: %v", err)
	}
	if _, err := c.ListObjectParts(ctx, s.Bucket, key, id, 0, 0); code(err) != "NoSuchUpload" {
		t.Errorf("parts of an aborted upload: %v", err)
	}
	if _, err := c.StatObject(ctx, s.Bucket, key, minio.StatObjectOptions{}); code(err) != "NoSuchKey" {
		t.Errorf("stat of nothing: %v", err)
	}
	if _, err := c.PutObject(ctx, s.Bucket, "files/empty", bytes.NewReader(nil), 0, "", "", minio.PutObjectOptions{ContentType: "text/plain"}); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveObject(ctx, s.Bucket, "files/empty", minio.RemoveObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	if keys := s.Keys(); len(keys) != 0 {
		t.Errorf("objects left: %v", keys)
	}
}

func TestListsComeInPages(t *testing.T) {
	s := s3test.New(t)
	s.SetPageSize(2)
	c := client(t, s, s3test.Secret)
	ctx := context.Background()
	old := time.Now().Add(-2 * time.Hour)
	for _, key := range []string{"a/files/1", "a/files/2", "a/files/2", "b/files/3", "a/files/4"} {
		s.StartUpload(key, old)
	}
	var found []string
	keyMarker, idMarker := "", ""
	for {
		res, err := c.ListMultipartUploads(ctx, s.Bucket, "a/", keyMarker, idMarker, "", 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, u := range res.Uploads {
			found = append(found, u.Key)
			if !u.Initiated.Equal(old.UTC().Truncate(time.Second)) {
				t.Errorf("initiated %v", u.Initiated)
			}
		}
		if !res.IsTruncated {
			break
		}
		keyMarker, idMarker = res.NextKeyMarker, res.NextUploadIDMarker
	}
	if fmt.Sprint(found) != "[a/files/1 a/files/2 a/files/2 a/files/4]" {
		t.Errorf("found %v", found)
	}

	id, _ := c.NewMultipartUpload(ctx, s.Bucket, "a/files/5", minio.PutObjectOptions{})
	for n := 1; n <= 5; n++ {
		put(t, s, partURL(t, c, s.Bucket, "a/files/5", id, n, 3), []byte("abc"))
	}
	var numbers []int
	marker := 0
	for {
		res, err := c.ListObjectParts(ctx, s.Bucket, "a/files/5", id, marker, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range res.ObjectParts {
			numbers = append(numbers, p.PartNumber)
		}
		if !res.IsTruncated {
			break
		}
		marker = res.NextPartNumberMarker
	}
	if fmt.Sprint(numbers) != "[1 2 3 4 5]" {
		t.Errorf("parts %v", numbers)
	}
}

func TestSignaturesAreChecked(t *testing.T) {
	s := s3test.New(t)
	ctx := context.Background()
	if _, err := client(t, s, "a wrong secret").NewMultipartUpload(ctx, s.Bucket, "k", minio.PutObjectOptions{}); code(err) != "SignatureDoesNotMatch" {
		t.Errorf("a wrong key: %v", err)
	}
	c := client(t, s, s3test.Secret)
	id, err := c.NewMultipartUpload(ctx, s.Bucket, "k", minio.PutObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	link := partURL(t, c, s.Bucket, "k", id, 1, 3)
	s.SetSkew(20 * time.Minute)
	if _, err := c.ListObjectParts(ctx, s.Bucket, "k", id, 0, 0); code(err) != "RequestTimeTooSkewed" {
		t.Errorf("a clock 20 minutes off: %v", err)
	}
	s.SetSkew(61 * time.Minute)
	if res := put(t, s, link, []byte("abc")); res.StatusCode != http.StatusForbidden {
		t.Errorf("an expired link: %s", res.Status)
	}
	s.SetSkew(0)
	if res := put(t, s, link, []byte("abc")); res.StatusCode != http.StatusOK {
		t.Errorf("the link in time: %s", res.Status)
	}
	if res := put(t, s, strings.Replace(link, "partNumber=1", "partNumber=2", 1), []byte("abc")); res.StatusCode != http.StatusForbidden {
		t.Errorf("a changed link: %s", res.Status)
	}
}

func TestPreflights(t *testing.T) {
	s := s3test.New(t)
	preflight := func() *http.Response {
		req, _ := http.NewRequest(http.MethodOptions, s.URL+"/"+s.Bucket+"/files/x", nil)
		req.Header.Set("Origin", "https://share.example.test")
		req.Header.Set("Access-Control-Request-Method", "GET")
		req.Header.Set("Access-Control-Request-Headers", "range,if-range")
		res, err := s.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}
	if res := preflight(); res.StatusCode != http.StatusOK || res.Header.Get("Access-Control-Allow-Origin") != "https://share.example.test" ||
		res.Header.Get("Access-Control-Allow-Headers") != "range,if-range" {
		t.Errorf("preflight %s %v", res.Status, res.Header)
	}
	s.SetCORS(false)
	if res := preflight(); res.StatusCode != http.StatusForbidden || res.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("without CORS rules %s %v", res.Status, res.Header)
	}
}

func TestFailuresOnDemand(t *testing.T) {
	s := s3test.New(t)
	c := client(t, s, s3test.Secret)
	ctx := context.Background()
	id, _ := c.NewMultipartUpload(ctx, s.Bucket, "k", minio.PutObjectOptions{})
	s.Fail("UploadPart", http.StatusServiceUnavailable)
	link := partURL(t, c, s.Bucket, "k", id, 1, 3)
	if res := put(t, s, link, []byte("abc")); res.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("first try %s", res.Status)
	}
	if res := put(t, s, link, []byte("abc")); res.StatusCode != http.StatusOK {
		t.Errorf("second try %s", res.Status)
	}
	s.Down(true)
	if _, err := c.ListObjectParts(ctx, s.Bucket, "k", id, 0, 0); err == nil || code(err) != "" {
		t.Errorf("with the server down: %v", err)
	}
	s.Down(false)
	if got := s.Calls(); got[0] != "CreateMultipartUpload k" || got[1] != "UploadPart k" {
		t.Errorf("calls %v", got)
	}
}

// recorder keeps the test's errors, to check the server reports misuse.
type recorder struct {
	testing.TB
	mu     sync.Mutex
	errors []string
}

func (r *recorder) Errorf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

func TestAChecksumFailsTheTest(t *testing.T) {
	rec := &recorder{TB: t}
	s := s3test.New(rec)
	c := client(t, s, s3test.Secret)
	_, err := c.NewMultipartUpload(context.Background(), s.Bucket, "k", minio.PutObjectOptions{UserMetadata: map[string]string{"X-Amz-Checksum-Algorithm": "CRC32C"}})
	if err == nil || len(rec.errors) != 1 || !strings.Contains(rec.errors[0], "checksum") {
		t.Errorf("err %v, test errors %q", err, rec.errors)
	}
}
