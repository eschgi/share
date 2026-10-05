-- The whole schema of version 7 for PostgreSQL, the same as SQLite's after migrations 1 to 7.
-- Text is compared and sorted byte by byte (COLLATE "C"), as SQLite does, so lower() folds
-- ASCII letters only, like SQLite's NOCASE. Later migrations come in both folders.

CREATE TABLE meta (
  key   TEXT COLLATE "C" PRIMARY KEY,
  value TEXT COLLATE "C" NOT NULL
);

CREATE TABLE users (
  id            TEXT COLLATE "C" PRIMARY KEY,
  name          TEXT COLLATE "C" NOT NULL,
  username      TEXT COLLATE "C",
  password_hash TEXT COLLATE "C",
  role          TEXT COLLATE "C" NOT NULL CHECK (role IN ('admin', 'member')),
  created_at    BIGINT NOT NULL,
  created_by    TEXT COLLATE "C" NOT NULL -- a user id, 'cli' or 'first-start'
);
CREATE UNIQUE INDEX users_username ON users (lower(username));

CREATE TABLE folders (
  id            TEXT COLLATE "C" PRIMARY KEY,
  name          TEXT COLLATE "C" NOT NULL,
  dir           TEXT COLLATE "C" NOT NULL, -- the directory in storage_dir; '' is storage_dir itself
  renaming_from TEXT COLLATE "C",          -- set while files may still lie under this older dir
  created_by    TEXT COLLATE "C" NOT NULL, -- a user id, 'cli' or 'first-start'
  created_at    BIGINT NOT NULL,
  deleted_at    BIGINT,                    -- its files are in the trash; restoring one brings it back
  deleted_by    TEXT COLLATE "C",
  -- The order of creation, for folders made in the same millisecond: what SQLite's rowid gives.
  rowid         BIGINT GENERATED ALWAYS AS IDENTITY
);
CREATE UNIQUE INDEX folders_dir ON folders (lower(dir));
CREATE UNIQUE INDEX folders_live_name ON folders (lower(name)) WHERE deleted_at IS NULL;

CREATE TABLE pins (
  id           TEXT COLLATE "C" PRIMARY KEY,
  code         TEXT COLLATE "C" NOT NULL UNIQUE,
  kind         TEXT COLLATE "C" NOT NULL CHECK (kind IN ('permanent', 'day')),
  created_by   TEXT COLLATE "C" NOT NULL,
  created_at   BIGINT NOT NULL,
  expires_at   BIGINT,
  ended_at     BIGINT,
  folder_id    TEXT COLLATE "C" REFERENCES folders (id) ON DELETE SET NULL,
  shows_folder BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX pins_by_folder ON pins (folder_id);

CREATE TABLE pin_sessions (
  id           TEXT COLLATE "C" PRIMARY KEY,
  pin_id       TEXT COLLATE "C" NOT NULL REFERENCES pins (id),
  token_hash   BYTEA NOT NULL UNIQUE,
  client       TEXT COLLATE "C" NOT NULL CHECK (client IN ('web', 'app')),
  created_at   BIGINT NOT NULL,
  last_seen_at BIGINT NOT NULL,
  revoked_at   BIGINT
);
CREATE INDEX pin_sessions_by_pin ON pin_sessions (pin_id);

CREATE TABLE devices (
  id           TEXT COLLATE "C" PRIMARY KEY,
  user_id      TEXT COLLATE "C" NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  token_hash   BYTEA NOT NULL UNIQUE,
  name         TEXT COLLATE "C" NOT NULL,
  created_at   BIGINT NOT NULL,
  last_seen_at BIGINT NOT NULL,
  revoked_at   BIGINT,
  client       TEXT COLLATE "C" NOT NULL DEFAULT 'app' CHECK (client IN ('app', 'web')),
  home_only    BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX devices_by_user ON devices (user_id);

CREATE TABLE invites (
  id         TEXT COLLATE "C" PRIMARY KEY,
  token_hash BYTEA NOT NULL UNIQUE,
  name       TEXT COLLATE "C" NOT NULL,
  role       TEXT COLLATE "C" NOT NULL CHECK (role IN ('admin', 'member')),
  user_id    TEXT COLLATE "C" REFERENCES users (id) ON DELETE CASCADE,
  created_by TEXT COLLATE "C" NOT NULL,
  created_at BIGINT NOT NULL,
  expires_at BIGINT NOT NULL,
  used_at    BIGINT,
  device_id  TEXT COLLATE "C",
  revoked_at BIGINT
);

CREATE TABLE folder_people (
  folder_id TEXT COLLATE "C" NOT NULL REFERENCES folders (id) ON DELETE CASCADE,
  user_id   TEXT COLLATE "C" NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  PRIMARY KEY (folder_id, user_id)
);
CREATE INDEX folder_people_by_user ON folder_people (user_id);

CREATE TABLE invite_folders (
  invite_id TEXT COLLATE "C" NOT NULL REFERENCES invites (id) ON DELETE CASCADE,
  folder_id TEXT COLLATE "C" NOT NULL REFERENCES folders (id) ON DELETE CASCADE,
  PRIMARY KEY (invite_id, folder_id)
);
CREATE INDEX invite_folders_by_folder ON invite_folders (folder_id);

CREATE TABLE files (
  id                 TEXT COLLATE "C" PRIMARY KEY,
  state              TEXT COLLATE "C" NOT NULL CHECK (state IN ('receiving', 'finalizing', 'ready', 'trashed')),
  name               TEXT COLLATE "C" NOT NULL,
  size               BIGINT NOT NULL,
  received           BIGINT NOT NULL DEFAULT 0,
  mime               TEXT COLLATE "C" NOT NULL DEFAULT '',
  kind               TEXT COLLATE "C" NOT NULL DEFAULT 'document' CHECK (kind IN ('photo', 'video', 'document')),
  rel_path           TEXT COLLATE "C",
  upload_day         TEXT COLLATE "C",
  created_at         BIGINT NOT NULL,
  updated_at         BIGINT NOT NULL,
  uploaded_at        BIGINT,
  client_modified_at BIGINT,
  width              BIGINT,
  height             BIGINT,
  duration_ms        BIGINT,
  thumb              TEXT COLLATE "C" NOT NULL DEFAULT 'none' CHECK (thumb IN ('none', 'client', 'server', 'failed')),
  pin_id             TEXT COLLATE "C" REFERENCES pins (id),
  pin_session_id     TEXT COLLATE "C",
  user_id            TEXT COLLATE "C",
  device_id          TEXT COLLATE "C",
  deleted_at         BIGINT,
  deleted_by         TEXT COLLATE "C",
  crc32              BIGINT CHECK (crc32 BETWEEN 0 AND 4294967295),
  folder_id          TEXT COLLATE "C" REFERENCES folders (id),
  moved_from         TEXT COLLATE "C",
  s3_upload_id       TEXT COLLATE "C",
  s3_part_size       BIGINT
);
CREATE INDEX files_by_folder ON files (folder_id, state, uploaded_at DESC, id);
CREATE INDEX files_crc_pending ON files (uploaded_at) WHERE state = 'ready' AND crc32 IS NULL;
CREATE INDEX files_moving ON files (id) WHERE moved_from IS NOT NULL;
CREATE INDEX files_ready_by_day ON files (upload_day, kind) WHERE state = 'ready';
CREATE INDEX files_ready_by_time ON files (uploaded_at DESC, id) WHERE state = 'ready';
CREATE INDEX files_receiving_by_session ON files (pin_session_id) WHERE state = 'receiving';
CREATE UNIQUE INDEX files_rel_path ON files (folder_id, lower(rel_path)) WHERE state IN ('finalizing', 'ready');
CREATE INDEX files_thumb_pending ON files (uploaded_at) WHERE state = 'ready' AND thumb = 'none' AND kind = 'photo';
CREATE INDEX files_unfinished ON files (state, updated_at) WHERE state IN ('receiving', 'finalizing');

CREATE TABLE s3_garbage (
  key        TEXT COLLATE "C" PRIMARY KEY,
  created_at BIGINT NOT NULL
);
