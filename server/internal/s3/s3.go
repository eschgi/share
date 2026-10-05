// Package s3 keeps the library in an S3 bucket. Browsers and the app send and fetch the bytes
// there themselves, with links this package signs; the server only starts, finishes and
// removes uploads and objects, and reads the start of a file now and then.
package s3

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/eschgi/share/server/internal/config"
)

const (
	// MaxParts is the most parts an upload may have.
	MaxParts = 10000
	// PartURLExpiry is how long a link for sending one part works.
	PartURLExpiry = time.Hour
	// GetURLExpiry is how long a link for fetching a file works.
	GetURLExpiry = 12 * time.Hour
)

var (
	// ErrNoUpload means the bucket doesn't know the multipart upload (any more).
	ErrNoUpload = errors.New("s3: no such upload")
	// ErrNoObject means there is no such object; it is an fs.ErrNotExist.
	ErrNoObject = fmt.Errorf("s3: no such object: %w", fs.ErrNotExist)
	// ErrInvalidPart means a part named in Complete is missing, or was sent again meanwhile.
	ErrInvalidPart = errors.New("s3: a part is missing or was sent again")
	// ErrUnavailable means the bucket didn't answer, or answered that it can't right now.
	ErrUnavailable = errors.New("s3: the bucket can't be reached")
)

// Options changes how Open connects; the zero value is what the server uses.
type Options struct {
	Transport  http.RoundTripper // tests use their server's; nil is one with timeouts
	MaxRetries int               // per call; 0 is 3
}

// Bucket is the bucket of the "s3" setting.
type Bucket struct {
	core     *minio.Core
	http     *http.Client
	name     string
	prefix   string
	endpoint string
	origin   string
	dates    *dateRecorder
}

// Open prepares the bucket. It makes no network call: the region is set, so links are signed
// right here.
func Open(c *config.S3, o Options) (*Bucket, error) {
	u, err := url.Parse(c.Endpoint)
	if err != nil {
		return nil, err
	}
	transport := o.Transport
	if transport == nil {
		transport = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 2 * time.Minute, // finishing a big upload takes a while
			ExpectContinueTimeout: time.Second,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConnsPerHost:   16,
			DisableCompression:    true, // ranges must stay ranges of the stored bytes
			ForceAttemptHTTP2:     true,
		}
	}
	retries := o.MaxRetries
	if retries == 0 {
		retries = 3
	}
	lookup := minio.BucketLookupAuto
	if c.PathStyle {
		lookup = minio.BucketLookupPath
	}
	dates := &dateRecorder{next: transport}
	core, err := minio.NewCore(u.Host, &minio.Options{
		Creds:        credentials.NewStaticV4(c.AccessKeyID, c.SecretAccessKey, ""),
		Secure:       u.Scheme == "https",
		Region:       c.Region,
		BucketLookup: lookup,
		Transport:    dates,
		MaxRetries:   retries,
	})
	if err != nil {
		return nil, err
	}
	b := &Bucket{
		core: core, http: &http.Client{Transport: dates, Timeout: 30 * time.Second},
		name: c.Bucket, prefix: c.Prefix, endpoint: c.Endpoint, dates: dates,
	}
	link, err := core.PresignedGetObject(context.Background(), b.name, b.Key("origin"), time.Minute, nil)
	if err != nil {
		return nil, err
	}
	b.origin = link.Scheme + "://" + link.Host
	return b, nil
}

// Name is the bucket's name.
func (b *Bucket) Name() string { return b.name }

// Endpoint is the address of the service the bucket is at.
func (b *Bucket) Endpoint() string { return b.endpoint }

// Prefix is what every key starts with, "" or ending in a slash.
func (b *Bucket) Prefix() string { return b.prefix }

// Key is the object of a file: one per file id, whatever its name and folder.
func (b *Bucket) Key(id string) string { return b.prefix + "files/" + id }

// Origin is where browsers send and fetch the files: the scheme and host of the bucket's
// links, for the website's Content-Security-Policy.
func (b *Bucket) Origin() string { return b.origin }

// CreateUpload starts a multipart upload, without a checksum algorithm: browsers can't send
// the checksum headers.
func (b *Bucket) CreateUpload(ctx context.Context, key, contentType, disposition string) (string, error) {
	id, err := b.core.NewMultipartUpload(ctx, b.name, key, minio.PutObjectOptions{ContentType: contentType, ContentDisposition: disposition})
	return id, b.mapErr(ctx, err)
}

// PartURL is a link for sending part n of an upload with a PUT. Its length is signed: a body
// of another size is refused by the bucket.
func (b *Bucket) PartURL(ctx context.Context, key, uploadID string, n int, size int64) (string, time.Time, error) {
	expires := time.Now().Add(PartURLExpiry) // what minio-go signs with, not the app's clock
	u, err := b.core.PresignHeader(ctx, http.MethodPut, b.name, key, PartURLExpiry,
		url.Values{"partNumber": {strconv.Itoa(n)}, "uploadId": {uploadID}},
		http.Header{"Content-Length": {strconv.FormatInt(size, 10)}})
	if err != nil {
		return "", time.Time{}, err
	}
	return u.String(), expires, nil
}

// Part is a part of an upload that the bucket has.
type Part struct {
	Number int
	Size   int64
	ETag   string
}

// Parts lists what the bucket has of an upload, by part number.
func (b *Bucket) Parts(ctx context.Context, key, uploadID string) (map[int]Part, error) {
	parts := map[int]Part{}
	marker := 0
	for {
		res, err := b.core.ListObjectParts(ctx, b.name, key, uploadID, marker, 1000)
		if err != nil {
			return nil, b.mapErr(ctx, err)
		}
		for _, p := range res.ObjectParts {
			parts[p.PartNumber] = Part{Number: p.PartNumber, Size: p.Size, ETag: p.ETag}
		}
		if !res.IsTruncated {
			return parts, nil
		}
		if res.NextPartNumberMarker <= marker {
			return nil, fmt.Errorf("%w: the list of parts of %s doesn't move on", ErrUnavailable, key)
		}
		marker = res.NextPartNumberMarker
	}
}

// Complete puts the parts together into the object.
func (b *Bucket) Complete(ctx context.Context, key, uploadID string, parts []Part) error {
	list := make([]minio.CompletePart, len(parts))
	for i, p := range parts {
		list[i] = minio.CompletePart{PartNumber: p.Number, ETag: p.ETag}
	}
	_, err := b.core.CompleteMultipartUpload(ctx, b.name, key, uploadID, list, minio.PutObjectOptions{})
	return b.mapErr(ctx, err)
}

// Abort drops an unfinished upload and its parts; an upload the bucket doesn't know is done.
func (b *Bucket) Abort(ctx context.Context, key, uploadID string) error {
	err := b.mapErr(ctx, b.core.AbortMultipartUpload(ctx, b.name, key, uploadID))
	if errors.Is(err, ErrNoUpload) {
		return nil
	}
	return err
}

// Upload is a multipart upload the bucket has open.
type Upload struct {
	Key       string
	UploadID  string
	Initiated time.Time
}

// Uploads calls each for every open upload of the library's keys.
func (b *Bucket) Uploads(ctx context.Context, each func(Upload) error) error {
	prefix := b.prefix + "files/"
	keyMarker, idMarker := "", ""
	for {
		res, err := b.core.ListMultipartUploads(ctx, b.name, prefix, keyMarker, idMarker, "", 1000)
		if err != nil {
			return b.mapErr(ctx, err)
		}
		for _, u := range res.Uploads {
			if !strings.HasPrefix(u.Key, prefix) {
				continue
			}
			if err := each(Upload{Key: u.Key, UploadID: u.UploadID, Initiated: u.Initiated}); err != nil {
				return err
			}
		}
		if !res.IsTruncated {
			return nil
		}
		if res.NextKeyMarker == keyMarker && res.NextUploadIDMarker == idMarker {
			return fmt.Errorf("%w: the list of uploads doesn't move on", ErrUnavailable)
		}
		keyMarker, idMarker = res.NextKeyMarker, res.NextUploadIDMarker
	}
}

// PutEmpty stores an empty file, which needs no upload.
func (b *Bucket) PutEmpty(ctx context.Context, key, contentType, disposition string) error {
	_, err := b.core.PutObject(ctx, b.name, key, bytes.NewReader(nil), 0, "", "", minio.PutObjectOptions{ContentType: contentType, ContentDisposition: disposition})
	return b.mapErr(ctx, err)
}

// Stat is the size of an object; ErrNoObject if there is none.
func (b *Bucket) Stat(ctx context.Context, key string) (int64, error) {
	info, err := b.core.StatObject(ctx, b.name, key, minio.StatObjectOptions{})
	if err != nil {
		return 0, b.mapErr(ctx, err)
	}
	return info.Size, nil
}

// Open opens an object for reading. It reads as it goes, and a Seek starts a new ranged GET.
func (b *Bucket) Open(ctx context.Context, key string) (*minio.Object, error) {
	obj, err := b.core.Client.GetObject(ctx, b.name, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, b.mapErr(ctx, err)
	}
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		return nil, b.mapErr(ctx, err)
	}
	return obj, nil
}

// Head reads the first n bytes of an object, or all of a shorter one. An empty object has
// none to read: don't ask.
func (b *Bucket) Head(ctx context.Context, key string, n int) ([]byte, error) {
	var opts minio.GetObjectOptions
	if err := opts.SetRange(0, int64(n)-1); err != nil {
		return nil, err
	}
	body, _, _, err := b.core.GetObject(ctx, b.name, key, opts)
	if err != nil {
		return nil, b.mapErr(ctx, err)
	}
	defer body.Close()
	head, err := io.ReadAll(io.LimitReader(body, int64(n)))
	if err != nil {
		return nil, b.mapErr(ctx, err)
	}
	return head, nil
}

// Remove deletes an object; one that isn't there is gone already.
func (b *Bucket) Remove(ctx context.Context, key string) error {
	return b.mapErr(ctx, b.core.RemoveObject(ctx, b.name, key, minio.RemoveObjectOptions{}))
}

// GetURL is a link for fetching an object. The bucket answers it with this
// Content-Disposition and Content-Type, whatever the object was stored with.
func (b *Bucket) GetURL(ctx context.Context, key, disposition, contentType string) (string, time.Time, error) {
	expires := time.Now().Add(GetURLExpiry)
	q := url.Values{"response-content-disposition": {disposition}}
	if contentType != "" {
		q.Set("response-content-type", contentType)
	}
	u, err := b.core.PresignedGetObject(ctx, b.name, key, GetURLExpiry, q)
	if err != nil {
		return "", time.Time{}, err
	}
	return u.String(), expires, nil
}

// ReachKind says why the bucket can't be used.
type ReachKind int

const (
	// Unreachable: no answer, or the service has trouble of its own.
	Unreachable ReachKind = iota
	// Denied: the bucket refuses the keys, or there is no such bucket.
	Denied
	// ClockSkew: this machine's clock is off, and signatures are or soon will be refused.
	ClockSkew
)

// ReachError is why Reach failed.
type ReachError struct {
	Kind ReachKind
	Skew time.Duration // how far the bucket's clock is ahead of this machine's
	Err  error         // nil for a clock that is off but still accepted
}

func (e *ReachError) Error() string {
	switch e.Kind {
	case Denied:
		return fmt.Sprintf("the bucket refuses Share's keys: %v", e.Err)
	case ClockSkew:
		return fmt.Sprintf("this machine's clock is %v off the bucket's", e.Skew.Abs().Round(time.Second))
	}
	return fmt.Sprintf("the bucket can't be reached: %v", e.Err)
}

func (e *ReachError) Unwrap() error { return e.Err }

// Reach asks the bucket for the parts of an upload that doesn't exist: any key that can send
// files may do that, and the answer tells unknown keys, a clock that is off and a network
// that is down apart.
func (b *Bucket) Reach(ctx context.Context) error {
	_, err := b.core.ListObjectParts(ctx, b.name, b.Key("reach"), "share-reach", 0, 1)
	skew, seen := b.dates.skew()
	off := func(limit time.Duration) bool { return seen && skew.Abs() > limit }
	if err == nil {
		err = errors.New("the bucket has an upload that can't exist")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var res minio.ErrorResponse
	switch {
	case !errors.As(err, &res) || res.Code == "" || res.StatusCode >= 500 || res.StatusCode == http.StatusTooManyRequests:
		return &ReachError{Kind: Unreachable, Err: err}
	case res.Code == "RequestTimeTooSkewed" || off(15*time.Minute):
		return &ReachError{Kind: ClockSkew, Skew: skew, Err: err}
	case res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusUnauthorized || slices.Contains(deniedCodes, res.Code):
		return &ReachError{Kind: Denied, Err: err}
	case off(5 * time.Minute):
		return &ReachError{Kind: ClockSkew, Skew: skew}
	}
	return nil // NoSuchUpload, or another answer that came signed and on time
}

var deniedCodes = []string{
	"AccessDenied", "SignatureDoesNotMatch", "InvalidAccessKeyId", "AuthorizationHeaderMalformed",
	"NoSuchBucket", "InvalidBucketName", "AllAccessDisabled", "InvalidToken", "ExpiredToken",
}

// reachEvery is how often WaitReachable asks again.
var reachEvery = 5 * time.Second

// WaitReachable waits until the bucket answers with a clock that is close enough, e.g. while
// a machine without a clock of its own still waits for the time. Keys the bucket refuses end
// it at once: waiting won't change them.
func (b *Bucket) WaitReachable(ctx context.Context, logf func(string, ...any)) error {
	start, lastLog := time.Now(), time.Time{}
	for {
		err := b.Reach(ctx)
		var re *ReachError
		switch {
		case err == nil:
			return nil
		case errors.As(err, &re) && re.Kind == Denied:
			return err
		case errors.As(err, &re) && re.Kind == ClockSkew && re.Err == nil:
			return nil // still accepted; the admins' storage page warns
		}
		if time.Since(lastLog) >= time.Minute {
			logf("storage: waiting for the bucket %s: %v", b.name, err)
			lastLog = time.Now()
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("gave up waiting for the bucket after %v: %w", time.Since(start).Round(time.Second), ctx.Err())
		case <-time.After(reachEvery):
		}
	}
}

// CORSError means the bucket doesn't let browsers on Origin do what Share's pages do.
type CORSError struct {
	Origin, Method string
}

func (e *CORSError) Error() string {
	return fmt.Sprintf("the bucket's CORS rules don't let pages on %s %s", e.Origin, e.Method)
}

// CORS asks the bucket the preflights a browser on origin asks before it sends a part (PUT)
// and before it continues a download (GET with Range and If-Range).
func (b *Bucket) CORS(ctx context.Context, origin string) error {
	link, err := b.core.PresignedGetObject(ctx, b.name, b.Key("cors"), time.Minute, nil)
	if err != nil {
		return err
	}
	link.RawQuery = ""
	for _, ask := range []struct {
		method  string
		headers []string
	}{{http.MethodPut, nil}, {http.MethodGet, []string{"range", "if-range"}}} {
		req, err := http.NewRequestWithContext(ctx, http.MethodOptions, link.String(), nil)
		if err != nil {
			return err
		}
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", ask.method)
		if ask.headers != nil {
			req.Header.Set("Access-Control-Request-Headers", strings.Join(ask.headers, ","))
		}
		res, err := b.http.Do(req)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		res.Body.Close()
		if res.StatusCode >= 500 {
			return fmt.Errorf("%w: a preflight got %s", ErrUnavailable, res.Status)
		}
		h := res.Header
		allowed := h.Get("Access-Control-Allow-Origin")
		ok := res.StatusCode/100 == 2 && (allowed == origin || allowed == "*") && listHas(h.Get("Access-Control-Allow-Methods"), ask.method)
		for _, need := range ask.headers {
			ok = ok && (listHas(h.Get("Access-Control-Allow-Headers"), need) || listHas(h.Get("Access-Control-Allow-Headers"), "*"))
		}
		if !ok {
			return &CORSError{Origin: origin, Method: ask.method}
		}
	}
	return nil
}

// listHas reports whether a comma-separated header names s, ignoring case.
func listHas(list, s string) bool {
	for _, x := range strings.Split(list, ",") {
		if strings.EqualFold(strings.TrimSpace(x), s) {
			return true
		}
	}
	return false
}

// CORSRules are the bucket's CORS rules for Share's pages on origins, as JSON for
// `aws s3api put-bucket-cors` (Cloudflare's dashboard takes the list inside).
func CORSRules(origins []string) []byte {
	rules := map[string]any{"CORSRules": []map[string]any{{
		"AllowedOrigins": origins,
		"AllowedMethods": []string{"GET", "PUT"},
		"AllowedHeaders": []string{"*"},
		"ExposeHeaders":  []string{"ETag", "Content-Length", "Content-Range"},
		"MaxAgeSeconds":  3600,
	}}}
	out, _ := json.MarshalIndent(rules, "", "  ")
	return out
}

// mapErr turns minio-go's errors into this package's.
func (b *Bucket) mapErr(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var res minio.ErrorResponse
	if !errors.As(err, &res) {
		return fmt.Errorf("%w: %w", ErrUnavailable, err) // no answer at all
	}
	switch {
	case res.Code == "NoSuchUpload":
		return fmt.Errorf("%w: %w", ErrNoUpload, err)
	case res.Code == "NoSuchKey" || res.Code == "NotFound":
		return fmt.Errorf("%w (%w)", ErrNoObject, err)
	case res.Code == "InvalidPart" || res.Code == "InvalidPartOrder":
		return fmt.Errorf("%w: %w", ErrInvalidPart, err)
	case res.Code == "" || res.StatusCode >= 500 || res.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return err
}

// dateRecorder notes how far the bucket's clock is from this machine's, from the Date of
// every answer.
type dateRecorder struct {
	next   http.RoundTripper
	offset atomic.Int64 // nanoseconds the bucket's clock is ahead
	seen   atomic.Bool
}

func (d *dateRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	res, err := d.next.RoundTrip(req)
	if err == nil {
		if t, err := http.ParseTime(res.Header.Get("Date")); err == nil {
			d.offset.Store(int64(time.Until(t)))
			d.seen.Store(true)
		}
	}
	return res, err
}

func (d *dateRecorder) skew() (time.Duration, bool) {
	return time.Duration(d.offset.Load()), d.seen.Load()
}
