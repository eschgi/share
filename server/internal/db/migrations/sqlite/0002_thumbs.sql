-- 0002: photos that still wait for a thumbnail, so the thumbnail worker doesn't scan the
-- whole library every minute.
CREATE INDEX files_thumb_pending ON files (uploaded_at) WHERE state = 'ready' AND thumb = 'none' AND kind = 'photo';
