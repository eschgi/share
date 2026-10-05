-- 0007: files in an S3 bucket. There a receiving upload is a multipart upload in the bucket:
-- its id there and the size of its parts are kept, so a client that comes back learns which
-- parts are missing. The bytes of every file are the object <prefix>files/<id>; folders,
-- days, names and the trash live only in this database.
ALTER TABLE files ADD COLUMN s3_upload_id TEXT;
ALTER TABLE files ADD COLUMN s3_part_size INTEGER;

-- Objects of purged files that the bucket hasn't removed yet, e.g. while the network was
-- down; the reconciler tries again.
CREATE TABLE s3_garbage (
  key        TEXT PRIMARY KEY,
  created_at INTEGER NOT NULL
) STRICT, WITHOUT ROWID;
