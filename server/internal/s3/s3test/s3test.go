// Package s3test is an S3 server in memory for tests: one bucket, with enough of the API for
// minio-go, presigned uploads and downloads, and browser preflights. Every signature is
// checked, including a signed Content-Length, so a mistake in signing fails here as it would
// on a real bucket.
package s3test

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/config"
)

// The keys and the region the server accepts.
const (
	KeyID  = "AKIDS3TEST000000"
	Secret = "s3test/secret/key/0123456789abcdefghij"
	Region = "test-region-1"
)

// Server is one bucket at an https address on 127.0.0.1.
type Server struct {
	*httptest.Server
	Bucket string

	t        testing.TB
	mu       sync.Mutex
	objects  map[string]*object
	uploads  map[string]*upload // by upload id
	next     int
	calls    []string
	fails    map[string][]int
	down     bool
	minPart  int64
	pageSize int
	clock    func() time.Time
	skew     time.Duration
	noCORS   bool
}

type object struct {
	data        []byte
	contentType string
	disposition string
	etag        string
	modified    time.Time
}

type upload struct {
	key, id     string
	contentType string
	disposition string
	initiated   time.Time
	parts       map[int]part
}

type part struct {
	data     []byte
	etag     string
	modified time.Time
}

// New starts the server; it stops with the test.
func New(t testing.TB) *Server {
	s := &Server{
		Bucket:   "share-test",
		t:        t,
		objects:  map[string]*object{},
		uploads:  map[string]*upload{},
		fails:    map[string][]int{},
		minPart:  5 << 20,
		pageSize: 1000,
		clock:    time.Now,
	}
	s.Server = httptest.NewTLSServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

// Config is an "s3" setting for this server, with the keys under prefix.
func (s *Server) Config(prefix string) *config.S3 {
	return &config.S3{
		Endpoint: s.URL, Region: Region, Bucket: s.Bucket, Prefix: prefix,
		AccessKeyID: KeyID, SecretAccessKey: Secret, PathStyle: true,
	}
}

// SetMinPartSize changes the smallest part but the last that Complete accepts (5 MiB).
func (s *Server) SetMinPartSize(n int64) { s.lock(func() { s.minPart = n }) }

// SetPageSize changes how many entries one list answer holds at most (1000).
func (s *Server) SetPageSize(n int) { s.lock(func() { s.pageSize = n }) }

// SetClock gives the times of new uploads, parts and objects.
func (s *Server) SetClock(now func() time.Time) { s.lock(func() { s.clock = now }) }

// SetSkew puts the server's own clock ahead of this machine's (or behind it), for checking
// signatures and for the Date header.
func (s *Server) SetSkew(d time.Duration) { s.lock(func() { s.skew = d }) }

// SetCORS makes preflights pass (the default, like MinIO) or fail, like a bucket without rules.
func (s *Server) SetCORS(ok bool) { s.lock(func() { s.noCORS = !ok }) }

// Down makes the server drop every connection, like a network that is gone.
func (s *Server) Down(down bool) { s.lock(func() { s.down = down }) }

// Fail answers the next requests of an operation, such as "UploadPart", with these statuses.
func (s *Server) Fail(op string, statuses ...int) {
	s.lock(func() { s.fails[op] = append(s.fails[op], statuses...) })
}

// Calls are the operations so far, as "Operation key".
func (s *Server) Calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.calls)
}

// Object returns an object's bytes.
func (s *Server) Object(key string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.objects[key]
	if !ok {
		return nil, false
	}
	return slices.Clone(o.data), true
}

// ObjectHeaders are the Content-Type and Content-Disposition an object was stored with.
func (s *Server) ObjectHeaders(key string) (contentType, disposition string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o, ok := s.objects[key]; ok {
		return o.contentType, o.disposition
	}
	return "", ""
}

// Keys are the keys of every object.
func (s *Server) Keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.objects))
	for k := range s.objects {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// PutObject stores an object, as if it had been uploaded.
func (s *Server) PutObject(key string, data []byte) {
	s.lock(func() { s.objects[key] = &object{data: slices.Clone(data), etag: etagOf(data), modified: s.clock()} })
}

// StartUpload starts a multipart upload, as a client that went away would have, and returns
// its id.
func (s *Server) StartUpload(key string, initiated time.Time) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.newUpload(key, "", "", initiated).id
}

// Uploads are the ids of the multipart uploads in progress.
func (s *Server) Uploads() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.uploads))
	for id := range s.uploads {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// UploadParts are the sizes of an upload's parts so far, by number.
func (s *Server) UploadParts(id string) map[int]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	sizes := map[int]int{}
	if u, ok := s.uploads[id]; ok {
		for n, p := range u.parts {
			sizes[n] = len(p.data)
		}
	}
	return sizes
}

func (s *Server) lock(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f()
}

func (s *Server) newUpload(key, contentType, disposition string, initiated time.Time) *upload {
	s.next++
	u := &upload{
		key: key, id: fmt.Sprintf("upload-%04d", s.next), contentType: contentType, disposition: disposition,
		initiated: initiated.UTC(), parts: map[int]part{},
	}
	s.uploads[u.id] = u
	return u
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	down, now := s.down, time.Now().Add(s.skew)
	s.mu.Unlock()
	if down {
		if conn, _, err := http.NewResponseController(w).Hijack(); err == nil {
			conn.Close()
		}
		return
	}
	w.Header().Set("Date", now.UTC().Format(http.TimeFormat))
	if r.Header.Get("Origin") != "" {
		w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
		w.Header().Set("Access-Control-Expose-Headers", "ETag, Content-Length, Content-Range")
	}

	bucket, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	q := r.URL.Query()
	op := operation(r.Method, key, q)
	s.mu.Lock()
	s.calls = append(s.calls, strings.TrimSpace(op+" "+key))
	s.mu.Unlock()
	if bucket != s.Bucket {
		s.fail(w, r, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.")
		return
	}
	if op == "Preflight" {
		s.preflight(w, r)
		return
	}
	if e := s.verify(r, now); e != nil {
		s.fail(w, r, e.status, e.code, e.message)
		return
	}
	if status := s.injected(op); status != 0 {
		code := "InternalError"
		switch status {
		case http.StatusServiceUnavailable:
			code = "SlowDown"
		case http.StatusForbidden:
			code = "AccessDenied"
		}
		s.fail(w, r, status, code, "Made to fail by the test.")
		return
	}

	switch op {
	case "CreateMultipartUpload":
		s.createUpload(w, r, key)
	case "UploadPart":
		s.uploadPart(w, r, q)
	case "ListParts":
		s.listParts(w, r, key, q)
	case "CompleteMultipartUpload":
		s.complete(w, r, key, q)
	case "AbortMultipartUpload":
		s.abort(w, r, q)
	case "ListMultipartUploads":
		s.listUploads(w, r, q)
	case "PutObject":
		s.putObject(w, r, key)
	case "HeadObject", "GetObject":
		s.getObject(w, r, key, q)
	case "DeleteObject":
		s.lock(func() { delete(s.objects, key) })
		w.WriteHeader(http.StatusNoContent)
	case "GetBucketLocation":
		s.t.Errorf("s3test: GetBucketLocation was asked; the region must be set")
		writeXML(w, http.StatusOK, struct {
			XMLName xml.Name `xml:"LocationConstraint"`
			Region  string   `xml:",chardata"`
		}{Region: Region})
	default:
		s.fail(w, r, http.StatusNotImplemented, "NotImplemented", op+" isn't part of the test server.")
	}
}

// operation names a request like the S3 API does.
func operation(method, key string, q map[string][]string) string {
	has := func(k string) bool { _, ok := q[k]; return ok }
	switch {
	case method == http.MethodOptions:
		return "Preflight"
	case key == "" && method == http.MethodGet && has("uploads"):
		return "ListMultipartUploads"
	case key == "" && method == http.MethodGet && has("location"):
		return "GetBucketLocation"
	case key == "":
		return method + "Bucket"
	case method == http.MethodPost && has("uploads"):
		return "CreateMultipartUpload"
	case method == http.MethodPost && has("uploadId"):
		return "CompleteMultipartUpload"
	case method == http.MethodPut && has("partNumber") && has("uploadId"):
		return "UploadPart"
	case method == http.MethodPut:
		return "PutObject"
	case method == http.MethodGet && has("uploadId"):
		return "ListParts"
	case method == http.MethodGet:
		return "GetObject"
	case method == http.MethodHead:
		return "HeadObject"
	case method == http.MethodDelete && has("uploadId"):
		return "AbortMultipartUpload"
	case method == http.MethodDelete:
		return "DeleteObject"
	}
	return method
}

func (s *Server) injected(op string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.fails[op]) == 0 {
		return 0
	}
	status := s.fails[op][0]
	s.fails[op] = s.fails[op][1:]
	return status
}

type sigError struct {
	status        int
	code, message string
}

// verify checks a request's signature (AWS Signature Version 4), in the query of a presigned
// URL or in the Authorization header.
func (s *Server) verify(r *http.Request, now time.Time) *sigError {
	denied := func(code, message string) *sigError { return &sigError{http.StatusForbidden, code, message} }
	q := r.URL.Query()
	var credential, signedHeaders, signature, date, payload string
	presigned := q.Has("X-Amz-Algorithm")
	if presigned {
		if q.Get("X-Amz-Algorithm") != "AWS4-HMAC-SHA256" {
			return denied("AuthorizationQueryParametersError", "X-Amz-Algorithm must be AWS4-HMAC-SHA256.")
		}
		credential, signedHeaders, signature, date = q.Get("X-Amz-Credential"), q.Get("X-Amz-SignedHeaders"), q.Get("X-Amz-Signature"), q.Get("X-Amz-Date")
		payload = "UNSIGNED-PAYLOAD"
		q.Del("X-Amz-Signature")
	} else {
		auth, ok := strings.CutPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ")
		if !ok {
			return denied("AccessDenied", "Anonymous access is not allowed.")
		}
		for _, field := range strings.Split(auth, ",") {
			k, v, _ := strings.Cut(strings.TrimSpace(field), "=")
			switch k {
			case "Credential":
				credential = v
			case "SignedHeaders":
				signedHeaders = v
			case "Signature":
				signature = v
			}
		}
		date, payload = r.Header.Get("X-Amz-Date"), r.Header.Get("X-Amz-Content-Sha256")
	}
	t, err := time.Parse("20060102T150405Z", date)
	if err != nil {
		return denied("AccessDenied", "X-Amz-Date is missing or malformed.")
	}
	if presigned {
		secs, err := strconv.Atoi(q.Get("X-Amz-Expires"))
		if err != nil || secs < 1 || secs > 7*24*3600 {
			return denied("AuthorizationQueryParametersError", "X-Amz-Expires must be 1 to 604800 seconds.")
		}
		if now.After(t.Add(time.Duration(secs) * time.Second)) {
			return denied("AccessDenied", "Request has expired")
		}
		if t.After(now.Add(15 * time.Minute)) {
			return denied("AccessDenied", "Request is not valid yet")
		}
	} else if d := now.Sub(t); d > 15*time.Minute || d < -15*time.Minute {
		return denied("RequestTimeTooSkewed", "The difference between the request time and the server's time is too large.")
	}
	scope := strings.Split(credential, "/")
	if len(scope) != 5 || scope[0] != KeyID {
		return denied("InvalidAccessKeyId", "The access key ID you provided does not exist in our records.")
	}
	if scope[1] != t.Format("20060102") || scope[2] != Region || scope[3] != "s3" || scope[4] != "aws4_request" {
		return &sigError{http.StatusBadRequest, "AuthorizationHeaderMalformed", "The credential scope is wrong; the region is " + Region + "."}
	}
	var headers strings.Builder
	for _, h := range strings.Split(signedHeaders, ";") {
		v := strings.Join(strings.Fields(r.Header.Get(h)), " ")
		switch h {
		case "host":
			v = r.Host
		case "content-length":
			v = strconv.FormatInt(r.ContentLength, 10)
		}
		headers.WriteString(h + ":" + v + "\n")
	}
	canonical := strings.Join([]string{
		r.Method, r.URL.EscapedPath(), strings.ReplaceAll(q.Encode(), "+", "%20"), headers.String(), signedHeaders, payload,
	}, "\n")
	sum := sha256.Sum256([]byte(canonical))
	toSign := "AWS4-HMAC-SHA256\n" + date + "\n" + strings.Join(scope[1:], "/") + "\n" + hex.EncodeToString(sum[:])
	key := []byte("AWS4" + Secret)
	for _, step := range scope[1:] {
		key = hmacSum(key, step)
	}
	if !hmac.Equal([]byte(hex.EncodeToString(hmacSum(key, toSign))), []byte(signature)) {
		return denied("SignatureDoesNotMatch", "The request signature we calculated does not match the signature you provided.")
	}
	return nil
}

func hmacSum(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

func (s *Server) preflight(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	noCORS := s.noCORS
	s.mu.Unlock()
	origin, method := r.Header.Get("Origin"), r.Header.Get("Access-Control-Request-Method")
	if noCORS || origin == "" || method == "" {
		w.Header().Del("Access-Control-Allow-Origin")
		w.Header().Del("Access-Control-Expose-Headers")
		s.fail(w, r, http.StatusForbidden, "AccessForbidden", "CORSResponse: This CORS request is not allowed.")
		return
	}
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", origin)
	h.Set("Access-Control-Allow-Methods", "GET, PUT, HEAD")
	if headers := r.Header.Get("Access-Control-Request-Headers"); headers != "" {
		h.Set("Access-Control-Allow-Headers", headers)
	}
	h.Set("Access-Control-Max-Age", "3600")
	h.Set("Vary", "Origin")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) createUpload(w http.ResponseWriter, r *http.Request, key string) {
	for h := range r.Header {
		if strings.HasPrefix(strings.ToLower(h), "x-amz-checksum-") || strings.HasPrefix(strings.ToLower(h), "x-amz-sdk-checksum-") {
			s.t.Errorf("s3test: CreateMultipartUpload with %s; browsers can't send checksums", h)
			s.fail(w, r, http.StatusBadRequest, "InvalidRequest", "No checksums here.")
			return
		}
	}
	s.mu.Lock()
	u := s.newUpload(key, r.Header.Get("Content-Type"), r.Header.Get("Content-Disposition"), s.clock())
	s.mu.Unlock()
	writeXML(w, http.StatusOK, struct {
		XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
		Bucket   string
		Key      string
		UploadID string `xml:"UploadId"`
	}{Bucket: s.Bucket, Key: key, UploadID: u.id})
}

func (s *Server) uploadPart(w http.ResponseWriter, r *http.Request, q map[string][]string) {
	n, err := strconv.Atoi(first(q, "partNumber"))
	if err != nil || n < 1 || n > 10000 {
		s.fail(w, r, http.StatusBadRequest, "InvalidArgument", "Part number must be an integer between 1 and 10000.")
		return
	}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return // the client went away; like S3, keep nothing of the part
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.uploads[first(q, "uploadId")]
	if !ok {
		s.fail(w, r, http.StatusNotFound, "NoSuchUpload", "The specified upload does not exist.")
		return
	}
	p := part{data: data, etag: etagOf(data), modified: s.clock()}
	u.parts[n] = p // a part sent again replaces the one before
	w.Header().Set("ETag", p.etag)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) listParts(w http.ResponseWriter, r *http.Request, key string, q map[string][]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.uploads[first(q, "uploadId")]
	if !ok || u.key != key {
		s.fail(w, r, http.StatusNotFound, "NoSuchUpload", "The specified upload does not exist.")
		return
	}
	marker, _ := strconv.Atoi(first(q, "part-number-marker"))
	limit := s.pageSize
	if m, err := strconv.Atoi(first(q, "max-parts")); err == nil && m > 0 && m < limit {
		limit = m
	}
	numbers := make([]int, 0, len(u.parts))
	for n := range u.parts {
		if n > marker {
			numbers = append(numbers, n)
		}
	}
	sort.Ints(numbers)
	type xmlPart struct {
		PartNumber   int
		LastModified string
		ETag         string
		Size         int64
	}
	res := struct {
		XMLName              xml.Name `xml:"ListPartsResult"`
		Bucket               string
		Key                  string
		UploadID             string `xml:"UploadId"`
		PartNumberMarker     int
		NextPartNumberMarker int
		MaxParts             int
		IsTruncated          bool
		Parts                []xmlPart `xml:"Part"`
	}{Bucket: s.Bucket, Key: key, UploadID: u.id, PartNumberMarker: marker, MaxParts: limit}
	for i, n := range numbers {
		if i == limit {
			res.IsTruncated = true
			break
		}
		p := u.parts[n]
		res.Parts = append(res.Parts, xmlPart{n, p.modified.UTC().Format(time.RFC3339), p.etag, int64(len(p.data))})
		res.NextPartNumberMarker = n
	}
	writeXML(w, http.StatusOK, res)
}

func (s *Server) complete(w http.ResponseWriter, r *http.Request, key string, q map[string][]string) {
	var req struct {
		Parts []struct {
			PartNumber int
			ETag       string
		} `xml:"Part"`
	}
	if err := xml.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Parts) == 0 {
		s.fail(w, r, http.StatusBadRequest, "MalformedXML", "The XML you provided was not well-formed.")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.uploads[first(q, "uploadId")]
	if !ok || u.key != key {
		s.fail(w, r, http.StatusNotFound, "NoSuchUpload", "The specified upload does not exist.")
		return
	}
	for i := 1; i < len(req.Parts); i++ {
		if req.Parts[i].PartNumber <= req.Parts[i-1].PartNumber {
			s.fail(w, r, http.StatusBadRequest, "InvalidPartOrder", "The list of parts was not in ascending order.")
			return
		}
	}
	var data []byte
	var sums []byte
	for i, want := range req.Parts {
		p, ok := u.parts[want.PartNumber]
		if !ok || strings.Trim(p.etag, `"`) != strings.Trim(want.ETag, `"`) {
			s.fail(w, r, http.StatusBadRequest, "InvalidPart", "One or more of the specified parts could not be found.")
			return
		}
		if i < len(req.Parts)-1 && int64(len(p.data)) < s.minPart {
			s.fail(w, r, http.StatusBadRequest, "EntityTooSmall", "Your proposed upload is smaller than the minimum allowed object size.")
			return
		}
		data = append(data, p.data...)
		sum, _ := hex.DecodeString(strings.Trim(p.etag, `"`))
		sums = append(sums, sum...)
	}
	all := md5.Sum(sums)
	etag := fmt.Sprintf(`"%s-%d"`, hex.EncodeToString(all[:]), len(req.Parts))
	s.objects[key] = &object{data: data, contentType: u.contentType, disposition: u.disposition, etag: etag, modified: s.clock()}
	delete(s.uploads, u.id)
	writeXML(w, http.StatusOK, struct {
		XMLName  xml.Name `xml:"CompleteMultipartUploadResult"`
		Location string
		Bucket   string
		Key      string
		ETag     string
	}{Location: s.URL + "/" + s.Bucket + "/" + key, Bucket: s.Bucket, Key: key, ETag: etag})
}

func (s *Server) abort(w http.ResponseWriter, r *http.Request, q map[string][]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := first(q, "uploadId")
	if _, ok := s.uploads[id]; !ok {
		s.fail(w, r, http.StatusNotFound, "NoSuchUpload", "The specified upload does not exist.")
		return
	}
	delete(s.uploads, id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listUploads(w http.ResponseWriter, r *http.Request, q map[string][]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prefix, keyMarker, idMarker := first(q, "prefix"), first(q, "key-marker"), first(q, "upload-id-marker")
	limit := s.pageSize
	if m, err := strconv.Atoi(first(q, "max-uploads")); err == nil && m > 0 && m < limit {
		limit = m
	}
	var all []*upload
	for _, u := range s.uploads {
		if strings.HasPrefix(u.key, prefix) && (u.key > keyMarker || u.key == keyMarker && u.id > idMarker) {
			all = append(all, u)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].key != all[j].key {
			return all[i].key < all[j].key
		}
		return all[i].id < all[j].id
	})
	type xmlUpload struct {
		Key       string
		UploadID  string `xml:"UploadId"`
		Initiated string
	}
	res := struct {
		XMLName            xml.Name `xml:"ListMultipartUploadsResult"`
		Bucket             string
		KeyMarker          string
		UploadIDMarker     string `xml:"UploadIdMarker"`
		NextKeyMarker      string
		NextUploadIDMarker string `xml:"NextUploadIdMarker"`
		Prefix             string
		MaxUploads         int
		IsTruncated        bool
		Uploads            []xmlUpload `xml:"Upload"`
	}{Bucket: s.Bucket, KeyMarker: keyMarker, UploadIDMarker: idMarker, Prefix: prefix, MaxUploads: limit}
	for i, u := range all {
		if i == limit {
			res.IsTruncated = true
			break
		}
		res.Uploads = append(res.Uploads, xmlUpload{u.key, u.id, u.initiated.Format(time.RFC3339)})
		res.NextKeyMarker, res.NextUploadIDMarker = u.key, u.id
	}
	writeXML(w, http.StatusOK, res)
}

func (s *Server) putObject(w http.ResponseWriter, r *http.Request, key string) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return
	}
	o := &object{data: data, contentType: r.Header.Get("Content-Type"), disposition: r.Header.Get("Content-Disposition"), etag: etagOf(data)}
	s.lock(func() {
		o.modified = s.clock()
		s.objects[key] = o
	})
	w.Header().Set("ETag", o.etag)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) getObject(w http.ResponseWriter, r *http.Request, key string, q map[string][]string) {
	s.mu.Lock()
	o, ok := s.objects[key]
	s.mu.Unlock()
	if !ok {
		s.fail(w, r, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.")
		return
	}
	h := w.Header()
	h.Set("ETag", o.etag)
	h.Set("Content-Type", firstNonEmpty(first(q, "response-content-type"), o.contentType, "binary/octet-stream"))
	if d := firstNonEmpty(first(q, "response-content-disposition"), o.disposition); d != "" {
		h.Set("Content-Disposition", d)
	}
	if c := first(q, "response-cache-control"); c != "" {
		h.Set("Cache-Control", c)
	}
	http.ServeContent(w, r, "", o.modified.Truncate(time.Second), bytes.NewReader(o.data))
}

// fail answers with an S3 error: XML, or only the status for HEAD.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	if r.Method == http.MethodHead {
		w.WriteHeader(status)
		return
	}
	writeXML(w, status, struct {
		XMLName   xml.Name `xml:"Error"`
		Code      string
		Message   string
		Resource  string
		RequestID string `xml:"RequestId"`
	}{Code: code, Message: message, Resource: r.URL.Path, RequestID: "s3test"})
}

func writeXML(w http.ResponseWriter, status int, v any) {
	body, err := xml.Marshal(v)
	if err != nil {
		panic(err)
	}
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Content-Length", strconv.Itoa(len(xml.Header)+len(body)))
	w.WriteHeader(status)
	io.WriteString(w, xml.Header)
	w.Write(body)
}

func etagOf(data []byte) string {
	sum := md5.Sum(data)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

func first(q map[string][]string, k string) string {
	if v := q[k]; len(v) > 0 {
		return v[0]
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
