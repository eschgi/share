package s3_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/config"
	"github.com/eschgi/share/server/internal/s3"
	"github.com/eschgi/share/server/internal/s3/s3test"
)

func open(t *testing.T, fake *s3test.Server, c *config.S3) *s3.Bucket {
	t.Helper()
	b, err := s3.Open(c, s3.Options{Transport: fake.Client().Transport, MaxRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

const disposition = `attachment; filename="K_ndigung 1.jpg"; filename*=UTF-8''K%C3%BCndigung%201.jpg`

// flow sends a file the way browsers do and reads it back the ways the server does.
func flow(t *testing.T, b *s3.Bucket, client *http.Client, strict bool) {
	ctx := context.Background()
	key := b.Key(newID())
	id, err := b.CreateUpload(ctx, key, "image/jpeg", disposition)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 5<<20+7)
	rand.Read(data)
	sizes := []int{5 << 20, 7}
	put := func(n int, body []byte, size int) int {
		t.Helper()
		link, expires, err := b.PartURL(ctx, key, id, n, int64(size))
		if err != nil {
			t.Fatal(err)
		}
		if until := time.Until(expires); until < 59*time.Minute || until > time.Hour {
			t.Errorf("the link works for %v", until)
		}
		req, _ := http.NewRequest(http.MethodPut, link, bytes.NewReader(body))
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if status := put(1, data[:1000], sizes[0]); status != http.StatusForbidden {
		t.Errorf("a part of another length: %d; the bucket must check the signed Content-Length", status)
	}
	if status := put(1, data[:sizes[0]], sizes[0]); status != http.StatusOK {
		t.Fatalf("part 1: %d", status)
	}
	if status := put(2, data[sizes[0]:], sizes[1]); status != http.StatusOK {
		t.Fatalf("part 2: %d", status)
	}
	parts, err := b.Parts(ctx, key, id)
	if err != nil || len(parts) != 2 || parts[1].Size != int64(sizes[0]) || parts[2].Size != int64(sizes[1]) {
		t.Fatalf("parts %+v, %v", parts, err)
	}
	if err := b.Complete(ctx, key, id, []s3.Part{parts[1], parts[2]}); err != nil {
		t.Fatal(err)
	}
	if size, err := b.Stat(ctx, key); err != nil || size != int64(len(data)) {
		t.Errorf("stat %d, %v", size, err)
	}
	if head, err := b.Head(ctx, key, 512); err != nil || !bytes.Equal(head, data[:512]) {
		t.Errorf("head of %d bytes, %v", len(head), err)
	}
	obj, err := b.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := obj.Seek(int64(sizes[0]), io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if rest, err := io.ReadAll(obj); err != nil || !bytes.Equal(rest, data[sizes[0]:]) {
		t.Errorf("after the seek %d bytes, %v", len(rest), err)
	}
	obj.Close()

	link, expires, err := b.GetURL(ctx, key, disposition, "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if until := time.Until(expires); until < 11*time.Hour || until > 12*time.Hour {
		t.Errorf("the link works for %v", until)
	}
	req, _ := http.NewRequest(http.MethodGet, link, nil)
	req.Header.Set("Range", "bytes=0-9")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusPartialContent || !bytes.Equal(body, data[:10]) {
		t.Errorf("GET %s, %d bytes", res.Status, len(body))
	}
	if res.Header.Get("Content-Disposition") != disposition || res.Header.Get("Content-Type") != "image/jpeg" {
		t.Errorf("GET answered with %q, %q", res.Header.Get("Content-Disposition"), res.Header.Get("Content-Type"))
	}
	if err := b.Remove(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Stat(ctx, key); !errors.Is(err, fs.ErrNotExist) || !errors.Is(err, s3.ErrNoObject) {
		t.Errorf("stat after removing: %v", err)
	}

	// An upload that goes away.
	gone, err := b.CreateUpload(ctx, key, "", disposition)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	if err := b.Uploads(ctx, func(u s3.Upload) error {
		found = found || u.UploadID == gone && u.Key == key
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !found && strict {
		t.Error("the open upload isn't listed")
	}
	if err := b.Abort(ctx, key, gone); err != nil {
		t.Fatal(err)
	}
	if err := b.Abort(ctx, key, gone); err != nil {
		t.Errorf("aborting twice: %v", err)
	}
	if _, err := b.Parts(ctx, key, gone); !errors.Is(err, s3.ErrNoUpload) {
		t.Errorf("parts of an aborted upload: %v", err)
	}

	empty := b.Key(newID())
	if err := b.PutEmpty(ctx, empty, "text/plain", `attachment; filename="empty.txt"`); err != nil {
		t.Fatal(err)
	}
	if size, err := b.Stat(ctx, empty); err != nil || size != 0 {
		t.Errorf("empty file: %d, %v", size, err)
	}
	if err := b.Remove(ctx, empty); err != nil {
		t.Fatal(err)
	}
	if err := b.Reach(ctx); err != nil {
		t.Errorf("reach: %v", err)
	}
}

func TestFlow(t *testing.T) {
	fake := s3test.New(t)
	b := open(t, fake, fake.Config("share/"))
	flow(t, b, fake.Client(), true)
	if keys := fake.Keys(); len(keys) != 0 {
		t.Errorf("objects left: %v", keys)
	}
	if b.Key("abc") != "share/files/abc" || b.Name() != fake.Bucket || b.Endpoint() != fake.URL || b.Origin() != fake.URL {
		t.Errorf("key %q, name %q, endpoint %q, origin %q", b.Key("abc"), b.Name(), b.Endpoint(), b.Origin())
	}
}

// TestLive runs the flow against a real bucket when SHARE_TEST_S3 holds its "s3" setting,
// on a fresh prefix: this is where a provider shows it checks the signed length and honours
// response-content-disposition.
func TestLive(t *testing.T) {
	setting := os.Getenv("SHARE_TEST_S3")
	if setting == "" {
		t.Skip(`set SHARE_TEST_S3 to a bucket's "s3" setting to run against it`)
	}
	cfg, err := config.Parse([]byte(`{"public_url": "https://share.example.test", "data_dir": "/tmp", "s3": ` + setting + `}`))
	if err != nil {
		t.Fatal(err)
	}
	cfg.S3.Prefix += "share-test-" + newID() + "/"
	b, err := s3.Open(cfg.S3, s3.Options{})
	if err != nil {
		t.Fatal(err)
	}
	flow(t, b, &http.Client{Timeout: 2 * time.Minute}, false)
	t.Logf("CORS for https://share.example.test: %v", b.CORS(context.Background(), "https://share.example.test"))
}

func TestErrors(t *testing.T) {
	fake := s3test.New(t)
	b := open(t, fake, fake.Config(""))
	ctx := context.Background()
	key := b.Key("x")
	id, err := b.CreateUpload(ctx, key, "", "")
	if err != nil {
		t.Fatal(err)
	}
	fake.Fail("ListParts", http.StatusInternalServerError)
	if _, err := b.Parts(ctx, key, id); !errors.Is(err, s3.ErrUnavailable) {
		t.Errorf("a 500: %v", err)
	}
	if err := b.Complete(ctx, key, id, []s3.Part{{Number: 1, ETag: `"00"`}}); !errors.Is(err, s3.ErrInvalidPart) {
		t.Errorf("a part that isn't there: %v", err)
	}
	fake.Down(true)
	if _, err := b.Stat(ctx, key); !errors.Is(err, s3.ErrUnavailable) {
		t.Errorf("no network: %v", err)
	}
	fake.Down(false)
	if err := b.Abort(ctx, key, id); err != nil {
		t.Fatal(err)
	}
	if err := b.Complete(ctx, key, id, []s3.Part{{Number: 1, ETag: `"00"`}}); !errors.Is(err, s3.ErrNoUpload) {
		t.Errorf("completing an aborted upload: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := b.Stat(cancelled, key); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled request: %v", err)
	}
}

func TestUploadsInPages(t *testing.T) {
	fake := s3test.New(t)
	fake.SetPageSize(2)
	b := open(t, fake, fake.Config("mine/"))
	ours := map[string]bool{}
	for range 5 {
		ours[fake.StartUpload(b.Key(newID()), time.Now())] = true
	}
	fake.StartUpload("theirs/files/x", time.Now())
	fake.StartUpload("mine/other", time.Now())
	n := 0
	if err := b.Uploads(context.Background(), func(u s3.Upload) error {
		if !ours[u.UploadID] || !strings.HasPrefix(u.Key, "mine/files/") {
			t.Errorf("not ours: %+v", u)
		}
		n++
		return nil
	}); err != nil || n != 5 {
		t.Errorf("%d uploads, %v", n, err)
	}
}

func TestReach(t *testing.T) {
	fake := s3test.New(t)
	ctx := context.Background()
	b := open(t, fake, fake.Config(""))
	kind := func(err error) string {
		var re *s3.ReachError
		switch {
		case err == nil:
			return "ok"
		case !errors.As(err, &re):
			return err.Error()
		case re.Kind == s3.Denied:
			return "denied"
		case re.Kind == s3.ClockSkew && re.Err == nil:
			return "skew, still accepted"
		case re.Kind == s3.ClockSkew:
			return "skew"
		}
		return "unreachable"
	}
	if got := kind(b.Reach(ctx)); got != "ok" {
		t.Errorf("reach: %s", got)
	}
	wrong := fake.Config("")
	wrong.SecretAccessKey = "not the secret"
	if got := kind(open(t, fake, wrong).Reach(ctx)); got != "denied" {
		t.Errorf("a wrong secret: %s", got)
	}
	if err := open(t, fake, wrong).WaitReachable(ctx, t.Logf); kind(err) != "denied" {
		t.Errorf("waiting with a wrong secret: %v", err)
	}
	fake.SetSkew(20 * time.Minute)
	if got := kind(b.Reach(ctx)); got != "skew" {
		t.Errorf("20 minutes off: %s", got)
	}
	fake.SetSkew(-6 * time.Minute)
	if got := kind(b.Reach(ctx)); got != "skew, still accepted" {
		t.Errorf("6 minutes off: %s", got)
	}
	if err := b.WaitReachable(ctx, t.Logf); err != nil {
		t.Errorf("waiting while still accepted: %v", err)
	}
	fake.SetSkew(0)
	fake.Down(true)
	if got := kind(b.Reach(ctx)); got != "unreachable" {
		t.Errorf("no network: %s", got)
	}
	short, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if err := b.WaitReachable(short, t.Logf); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("waiting without a network: %v", err)
	}
}

func TestCORS(t *testing.T) {
	fake := s3test.New(t)
	b := open(t, fake, fake.Config("share/"))
	ctx := context.Background()
	if err := b.CORS(ctx, "https://share.example.test"); err != nil {
		t.Errorf("CORS: %v", err)
	}
	fake.SetCORS(false)
	var ce *s3.CORSError
	if err := b.CORS(ctx, "https://share.example.test"); !errors.As(err, &ce) || ce.Method != http.MethodPut {
		t.Errorf("without rules: %v", err)
	}
	rules := string(s3.CORSRules([]string{"https://share.example.test", "http://192.168.8.52:8080"}))
	for _, want := range []string{`"CORSRules"`, `"https://share.example.test"`, `"http://192.168.8.52:8080"`, `"PUT"`, `"ETag"`} {
		if !strings.Contains(rules, want) {
			t.Errorf("rules without %s: %s", want, rules)
		}
	}
}
