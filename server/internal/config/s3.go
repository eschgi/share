package config

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

// S3MaxFileSize is the largest object S3 keeps, and so the largest file in a bucket.
const S3MaxFileSize = 5 << 40

// S3 is a bucket that holds the library instead of storage_dir: Amazon S3, or a service that
// speaks its API, such as Cloudflare R2, Backblaze B2 or MinIO. Browsers and the app send
// and fetch the files there directly, and the thumbnails go there too; the records stay in
// PostgreSQL.
type S3 struct {
	Endpoint        string `json:"endpoint"` // the service's address, e.g. https://s3.eu-central-1.amazonaws.com
	Region          string `json:"region"`   // e.g. eu-central-1; "auto" on R2
	Bucket          string `json:"bucket"`
	Prefix          string `json:"prefix"` // the keys start with it, e.g. "share/"; "" for the whole bucket
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	// PathStyle puts the bucket into the path (https://host/bucket/key) instead of the host
	// name, for services without a name per bucket, such as MinIO at home.
	PathStyle bool `json:"path_style"`
}

// String names the bucket without its keys, so they never end up in a log.
func (s S3) String() string {
	return fmt.Sprintf("bucket %s, prefix %q, at %s", s.Bucket, s.Prefix, s.Endpoint)
}

// GoString is String for %#v.
func (s S3) GoString() string { return s.String() }

var (
	regionPattern  = regexp.MustCompile(`^[a-z0-9-]+$`)
	bucketPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{1,61}[A-Za-z0-9]$`)
	segmentPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// complete checks the bucket's settings and puts the endpoint and the prefix into one form.
func (s *S3) complete(bad func(field, format string, args ...any)) {
	if s.Endpoint == "" {
		bad("s3.endpoint", "is required, e.g. https://s3.eu-central-1.amazonaws.com")
	} else if u, err := parseOriginLike(s.Endpoint, "https://s3.eu-central-1.amazonaws.com", "for a bucket at home, such as http://192.168.1.20:9000"); err != nil {
		bad("s3.endpoint", "%v", err)
	} else {
		// The host is part of every signature: the default port goes, as browsers drop it.
		if port := u.Port(); u.Scheme == "https" && port == "443" || u.Scheme == "http" && port == "80" {
			u.Host = hostOnly(u.Hostname())
		}
		if s.Bucket != "" && strings.HasPrefix(u.Hostname(), strings.ToLower(s.Bucket)+".") {
			bad("s3.endpoint", "%q already names the bucket: give the service's address without it", s.Endpoint)
		}
		s.Endpoint = u.String()
	}
	if s.Region == "" {
		bad("s3.region", `is required, e.g. eu-central-1, or "auto" on Cloudflare R2`)
	} else if !regionPattern.MatchString(s.Region) {
		bad("s3.region", "%q is not a region name like eu-central-1", s.Region)
	}
	if s.Bucket == "" {
		bad("s3.bucket", "is required")
	} else if !bucketPattern.MatchString(s.Bucket) || strings.Contains(s.Bucket, "..") || net.ParseIP(s.Bucket) != nil {
		bad("s3.bucket", "%q is not a bucket name: 3 to 63 letters, digits, dots, dashes or underscores", s.Bucket)
	}
	s.Prefix = strings.TrimPrefix(s.Prefix, "/")
	if s.Prefix != "" {
		s.Prefix = strings.TrimSuffix(s.Prefix, "/")
		for _, seg := range strings.Split(s.Prefix, "/") {
			if !segmentPattern.MatchString(seg) || seg == "." || seg == ".." {
				bad("s3.prefix", "%q may only have letters, digits, dots, dashes and underscores between its slashes", s.Prefix)
				break
			}
		}
		if len(s.Prefix) > 512 {
			bad("s3.prefix", "is longer than 512 characters")
		}
		s.Prefix += "/"
	}
	if s.AccessKeyID == "" {
		bad("s3.access_key_id", "is required")
	}
	if s.SecretAccessKey == "" {
		bad("s3.secret_access_key", "is required")
	}
}

// hostOnly is a host name or address as it goes into a URL, IPv6 in brackets.
func hostOnly(host string) string {
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
}
