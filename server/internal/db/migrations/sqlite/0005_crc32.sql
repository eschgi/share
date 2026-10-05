-- 0005: each file's CRC-32. A ZIP download has to send it before the file's bytes, and needs it
-- to know the archive's size and to resume, so it is worked out once, soon after the upload; a
-- file's bytes never change.
ALTER TABLE files ADD COLUMN crc32 INTEGER CHECK (crc32 BETWEEN 0 AND 4294967295);

CREATE INDEX files_crc_pending ON files (uploaded_at) WHERE state = 'ready' AND crc32 IS NULL;
