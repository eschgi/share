-- 0001: PINs, PIN sessions and files (uploads and the library in one table).
-- Timestamps are Unix milliseconds in UTC.

CREATE TABLE meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
) STRICT;

-- Codes are never reused, so a code stays here after its PIN ends and old links stay dead.
CREATE TABLE pins (
  id         TEXT PRIMARY KEY,
  code       TEXT NOT NULL UNIQUE,
  kind       TEXT NOT NULL CHECK (kind IN ('permanent', 'day')),
  created_by TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  expires_at INTEGER,
  ended_at   INTEGER
) STRICT;

-- A session is valid only while it isn't revoked and its PIN is live, so ending a PIN or
-- giving it a new code needs no writes here.
CREATE TABLE pin_sessions (
  id           TEXT PRIMARY KEY,
  pin_id       TEXT NOT NULL REFERENCES pins (id),
  token_hash   BLOB NOT NULL UNIQUE,
  client       TEXT NOT NULL CHECK (client IN ('web', 'app')),
  created_at   INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL,
  revoked_at   INTEGER
) STRICT;

CREATE INDEX pin_sessions_by_pin ON pin_sessions (pin_id);

-- The id is the tus upload id. A 'ready' row doubles as the tombstone that answers tus HEAD
-- requests after the upload's own files are gone.
CREATE TABLE files (
  id                 TEXT PRIMARY KEY,
  state              TEXT NOT NULL CHECK (state IN ('receiving', 'finalizing', 'ready', 'trashed')),
  name               TEXT NOT NULL,
  size               INTEGER NOT NULL,
  received           INTEGER NOT NULL DEFAULT 0,
  mime               TEXT NOT NULL DEFAULT '',
  kind               TEXT NOT NULL DEFAULT 'document' CHECK (kind IN ('photo', 'video', 'document')),
  rel_path           TEXT,
  upload_day         TEXT,
  created_at         INTEGER NOT NULL,
  updated_at         INTEGER NOT NULL,
  uploaded_at        INTEGER,
  client_modified_at INTEGER,
  width              INTEGER,
  height             INTEGER,
  duration_ms        INTEGER,
  thumb              TEXT NOT NULL DEFAULT 'none' CHECK (thumb IN ('none', 'client', 'server', 'failed')),
  pin_id             TEXT REFERENCES pins (id),
  pin_session_id     TEXT,
  user_id            TEXT,
  device_id          TEXT,
  deleted_at         INTEGER,
  deleted_by         TEXT
) STRICT;

CREATE INDEX files_ready_by_time ON files (uploaded_at DESC, id) WHERE state = 'ready';
CREATE INDEX files_ready_by_day ON files (upload_day, kind) WHERE state = 'ready';
CREATE INDEX files_unfinished ON files (state, updated_at) WHERE state IN ('receiving', 'finalizing');
CREATE INDEX files_receiving_by_session ON files (pin_session_id) WHERE state = 'receiving';
CREATE UNIQUE INDEX files_rel_path ON files (rel_path COLLATE NOCASE) WHERE state IN ('finalizing', 'ready');
