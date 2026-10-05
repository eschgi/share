package main

import (
	"context"
	"fmt"
	"os"

	"github.com/eschgi/share/server/internal/config"
	"github.com/eschgi/share/server/internal/s3"
	"github.com/eschgi/share/server/internal/storage"
)

// openBucket opens the bucket of the "s3" setting; tests replace it.
var openBucket = func(c *config.S3) (*s3.Bucket, error) { return s3.Open(c, s3.Options{}) }

// initS3 is `share init` with the files in a bucket, which needs nothing made: only the data
// folder, for the database and the thumbnails. Then it checks as `share check` does.
func initS3(cfg *config.Config) error {
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}
	fmt.Printf("Data folder ready: %s\n", cfg.DataDir)
	return checkS3(cfg)
}

// checkS3 is `share check` for a bucket. When the bucket's CORS rules don't let Share's pages
// in, it prints the rules to set.
func checkS3(cfg *config.Config) error {
	b, err := openBucket(cfg.S3)
	if err != nil {
		return fmt.Errorf("s3: %w", err)
	}
	fmt.Println(bucketLine(cfg.S3))
	r := storage.CheckS3(context.Background(), cfg.DataDir, b, cfg.Origins())
	err = printReport(r)
	for _, p := range r.Problems {
		if p.Code == "s3_cors" {
			fmt.Printf("\nThe CORS rules for the bucket, e.g. for `aws s3api put-bucket-cors --bucket %s --cors-configuration file://cors.json`\n"+
				"(Cloudflare's dashboard takes the list inside the brackets):\n%s\n", cfg.S3.Bucket, s3.CORSRules(cfg.Origins()))
			break
		}
	}
	return err
}

// bucketLine says which bucket Share uses, for `share check`.
func bucketLine(c *config.S3) string {
	where := "the whole bucket"
	if c.Prefix != "" {
		where = "keys under " + c.Prefix
	}
	return fmt.Sprintf("Bucket:        %s at %s (region %s), %s", c.Bucket, c.Endpoint, c.Region, where)
}
