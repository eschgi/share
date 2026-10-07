-- Share's schema, version 1. Ids are UUIDs and times are timestamptz, to the millisecond. Text
-- sorts by code point, as Go compares strings, and casefold() folds every letter whatever the
-- database's locale (pg_c_utf8, in a UTF8 database; PostgreSQL 18 or newer).

CREATE TABLE meta (
  key   TEXT COLLATE pg_c_utf8 PRIMARY KEY,
  value TEXT COLLATE pg_c_utf8 NOT NULL
);

-- When each periodic job last ran without an error.
CREATE TABLE job_runs (
  name   TEXT COLLATE pg_c_utf8 PRIMARY KEY,
  ran_at TIMESTAMPTZ(3) NOT NULL
);

-- A person. Their key (docs/e2ee-plan.md): the public key, and the private key locked with
-- their password. Their phones and browsers have keys of their own; the person's private key
-- is sealed for each of them in person_keys. Their note holds the root and the newest version
-- of each folder's key their phones and browsers have seen, locked with a key from their
-- private key, for their next phone or browser.
CREATE TABLE users (
  id            UUID PRIMARY KEY,
  name          TEXT COLLATE pg_c_utf8 NOT NULL,
  username      TEXT COLLATE pg_c_utf8,
  password_hash TEXT COLLATE pg_c_utf8,
  role          TEXT COLLATE pg_c_utf8 NOT NULL CHECK (role IN ('admin', 'member')),
  created_at    TIMESTAMPTZ(3) NOT NULL,
  created_by    TEXT COLLATE pg_c_utf8 NOT NULL, -- a user id, 'cli' or 'first-start'
  public_key    BYTEA,
  password_lock BYTEA,
  note          BYTEA
);
CREATE UNIQUE INDEX users_username ON users (casefold(username));

CREATE TABLE folders (
  id            UUID PRIMARY KEY,
  name          TEXT COLLATE pg_c_utf8 NOT NULL,
  dir           TEXT COLLATE pg_c_utf8 NOT NULL, -- the directory in storage_dir; '' is storage_dir itself
  renaming_from TEXT COLLATE pg_c_utf8,          -- set while files may still lie under this older dir
  created_by    TEXT COLLATE pg_c_utf8 NOT NULL, -- a user id, 'cli' or 'first-start'
  created_at    TIMESTAMPTZ(3) NOT NULL,
  deleted_at    TIMESTAMPTZ(3),                  -- its files are in the trash; restoring one brings it back
  deleted_by    TEXT COLLATE pg_c_utf8,
  -- The order of creation, for folders made in the same millisecond.
  seq           BIGINT GENERATED ALWAYS AS IDENTITY,
  -- New files are encrypted; and a new key version is due because someone lost the folder.
  encrypted     BOOLEAN NOT NULL DEFAULT FALSE,
  rekey         BOOLEAN NOT NULL DEFAULT FALSE,
  -- The root's signature that the folder sends plain, for its newest key version (0 without
  -- one) and its name; phones and browsers send nothing plain without it, once there is a root.
  plain_signature BYTEA
);
CREATE UNIQUE INDEX folders_dir ON folders (casefold(dir));
CREATE UNIQUE INDEX folders_live_name ON folders (casefold(name)) WHERE deleted_at IS NULL;

CREATE TABLE pins (
  id             UUID PRIMARY KEY,
  code           TEXT COLLATE pg_c_utf8 NOT NULL UNIQUE,
  kind           TEXT COLLATE pg_c_utf8 NOT NULL CHECK (kind IN ('permanent', 'day')),
  created_by     TEXT COLLATE pg_c_utf8 NOT NULL,
  created_at     TIMESTAMPTZ(3) NOT NULL,
  expires_at     TIMESTAMPTZ(3),
  ended_at       TIMESTAMPTZ(3),
  folder_id      UUID REFERENCES folders (id) ON DELETE SET NULL,
  shows_folder   BOOLEAN NOT NULL DEFAULT FALSE,
  -- The secret of the link of a PIN that shows an encrypted folder, locked with a key from a
  -- version of the folder's private key, so that later versions can be locked for it too
  -- (pin_keys); and the root, locked with the secret.
  secret_locked  BYTEA,
  secret_version INTEGER,
  root_locked    BYTEA
);
CREATE INDEX pins_by_folder ON pins (folder_id);

CREATE TABLE pin_sessions (
  id           UUID PRIMARY KEY,
  pin_id       UUID NOT NULL REFERENCES pins (id),
  token_hash   BYTEA NOT NULL UNIQUE,
  client       TEXT COLLATE pg_c_utf8 NOT NULL CHECK (client IN ('web', 'app')),
  created_at   TIMESTAMPTZ(3) NOT NULL,
  last_seen_at TIMESTAMPTZ(3) NOT NULL,
  revoked_at   TIMESTAMPTZ(3)
);
CREATE INDEX pin_sessions_by_pin ON pin_sessions (pin_id);

CREATE TABLE devices (
  id           UUID PRIMARY KEY,
  user_id      UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  token_hash   BYTEA NOT NULL UNIQUE,
  name         TEXT COLLATE pg_c_utf8 NOT NULL,
  created_at   TIMESTAMPTZ(3) NOT NULL,
  last_seen_at TIMESTAMPTZ(3) NOT NULL,
  revoked_at   TIMESTAMPTZ(3),
  client       TEXT COLLATE pg_c_utf8 NOT NULL DEFAULT 'app' CHECK (client IN ('app', 'web')),
  home_only    BOOLEAN NOT NULL DEFAULT FALSE,
  public_key   BYTEA
);
CREATE INDEX devices_by_user ON devices (user_id);

CREATE TABLE invites (
  id         UUID PRIMARY KEY,
  token_hash BYTEA NOT NULL UNIQUE,
  name       TEXT COLLATE pg_c_utf8 NOT NULL,
  role       TEXT COLLATE pg_c_utf8 NOT NULL CHECK (role IN ('admin', 'member')),
  user_id    UUID REFERENCES users (id) ON DELETE CASCADE,
  created_by TEXT COLLATE pg_c_utf8 NOT NULL,
  created_at TIMESTAMPTZ(3) NOT NULL,
  expires_at TIMESTAMPTZ(3) NOT NULL,
  used_at    TIMESTAMPTZ(3),
  device_id  UUID,
  revoked_at TIMESTAMPTZ(3),
  -- The person's own key locked with the secret of the link, for a new phone or browser of
  -- someone who has an account; it goes out once (docs/e2ee-plan.md). The root, locked the same
  -- way, for the new phone or browser to trust.
  person_key  BYTEA,
  root_locked BYTEA
);

CREATE TABLE folder_people (
  folder_id UUID NOT NULL REFERENCES folders (id) ON DELETE CASCADE,
  user_id   UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  PRIMARY KEY (folder_id, user_id)
);
CREATE INDEX folder_people_by_user ON folder_people (user_id);

CREATE TABLE invite_folders (
  invite_id UUID NOT NULL REFERENCES invites (id) ON DELETE CASCADE,
  folder_id UUID NOT NULL REFERENCES folders (id) ON DELETE CASCADE,
  PRIMARY KEY (invite_id, folder_id)
);
CREATE INDEX invite_folders_by_folder ON invite_folders (folder_id);

CREATE TABLE files (
  id                 UUID PRIMARY KEY,
  state              TEXT COLLATE pg_c_utf8 NOT NULL CHECK (state IN ('receiving', 'finalizing', 'ready', 'trashed')),
  name               TEXT COLLATE pg_c_utf8 NOT NULL,
  size               BIGINT NOT NULL, -- of the stored bytes
  received           BIGINT NOT NULL DEFAULT 0,
  mime               TEXT COLLATE pg_c_utf8 NOT NULL DEFAULT '',
  kind               TEXT COLLATE pg_c_utf8 NOT NULL DEFAULT 'document' CHECK (kind IN ('photo', 'video', 'document')),
  rel_path           TEXT COLLATE pg_c_utf8,
  upload_day         DATE,
  created_at         TIMESTAMPTZ(3) NOT NULL,
  updated_at         TIMESTAMPTZ(3) NOT NULL,
  uploaded_at        TIMESTAMPTZ(3),
  client_modified_at TIMESTAMPTZ(3),
  width              INTEGER,
  height             INTEGER,
  duration_ms        INTEGER,
  thumb              TEXT COLLATE pg_c_utf8 NOT NULL DEFAULT 'none' CHECK (thumb IN ('none', 'client', 'server', 'failed')),
  pin_id             UUID REFERENCES pins (id),
  pin_session_id     UUID,
  user_id            UUID,
  device_id          UUID,
  deleted_at         TIMESTAMPTZ(3),
  deleted_by         TEXT COLLATE pg_c_utf8,
  crc32              BIGINT CHECK (crc32 BETWEEN 0 AND 4294967295),
  folder_id          UUID REFERENCES folders (id),
  moved_from         TEXT COLLATE pg_c_utf8,
  s3_upload_id       TEXT COLLATE pg_c_utf8,
  s3_part_size       BIGINT,
  -- An encrypted file: the folder key version its key is sealed for, that sealed key, the
  -- header of its contents and its plain size.
  enc_version        INTEGER,
  enc_key            BYTEA,
  enc_header         BYTEA,
  plain_size         BIGINT
);
CREATE INDEX files_by_folder ON files (folder_id, state, uploaded_at DESC, id);
-- The server neither reads the checksums of encrypted files nor makes their thumbnails.
CREATE INDEX files_crc_pending ON files (uploaded_at) WHERE state = 'ready' AND crc32 IS NULL AND enc_version IS NULL;
CREATE INDEX files_moving ON files (id) WHERE moved_from IS NOT NULL;
CREATE INDEX files_ready_by_day ON files (upload_day, kind) WHERE state = 'ready';
CREATE INDEX files_ready_by_time ON files (uploaded_at DESC, id) WHERE state = 'ready';
CREATE INDEX files_receiving_by_session ON files (pin_session_id) WHERE state = 'receiving';
CREATE UNIQUE INDEX files_rel_path ON files (folder_id, casefold(rel_path)) WHERE state IN ('finalizing', 'ready');
CREATE INDEX files_thumb_pending ON files (uploaded_at) WHERE state = 'ready' AND thumb = 'none' AND kind = 'photo' AND enc_version IS NULL;
CREATE INDEX files_unfinished ON files (state, updated_at) WHERE state IN ('receiving', 'finalizing');

CREATE TABLE s3_garbage (
  key        TEXT COLLATE pg_c_utf8 PRIMARY KEY,
  created_at TIMESTAMPTZ(3) NOT NULL
);

-- End-to-end encryption (docs/e2ee-plan.md). The server keeps public keys, and private keys
-- only sealed for someone or locked with a secret or a password: nothing here opens a file.

-- The person's private key, sealed for one of their phones or browsers.
CREATE TABLE person_keys (
  device_id  UUID PRIMARY KEY REFERENCES devices (id) ON DELETE CASCADE,
  sealed     BYTEA NOT NULL,
  created_at TIMESTAMPTZ(3) NOT NULL
);

-- The root key, which is the recovery key, in the order they were made: each signed by the one
-- before, and its private key locked with the recovery code, until a newer one comes. The newest
-- signs the folders' keys and plain statements (docs/e2ee-plan.md).
CREATE TABLE roots (
  seq        INTEGER PRIMARY KEY,
  public_key BYTEA NOT NULL,
  signature  BYTEA,
  locked     BYTEA,
  created_at TIMESTAMPTZ(3) NOT NULL
);

-- The newest root's private key, sealed for an admin's person key.
CREATE TABLE root_grants (
  user_id    UUID PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
  sealed     BYTEA NOT NULL,
  created_at TIMESTAMPTZ(3) NOT NULL
);

-- An encrypted folder's key pairs: version 1, and a new one each time someone loses the folder
-- or encryption is turned on again, each signed by the root. recovery_sealed is the private key
-- sealed for the recovery key.
CREATE TABLE folder_keys (
  folder_id       UUID NOT NULL REFERENCES folders (id) ON DELETE CASCADE,
  version         INTEGER NOT NULL,
  public_key      BYTEA NOT NULL,
  signature       BYTEA NOT NULL,
  recovery_sealed BYTEA,
  created_by      TEXT COLLATE pg_c_utf8 NOT NULL,
  created_at      TIMESTAMPTZ(3) NOT NULL,
  PRIMARY KEY (folder_id, version)
);

-- A folder's private keys, sealed for each person who sees the folder.
CREATE TABLE folder_grants (
  folder_id  UUID NOT NULL,
  version    INTEGER NOT NULL,
  user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  sealed     BYTEA NOT NULL,
  created_at TIMESTAMPTZ(3) NOT NULL,
  PRIMARY KEY (folder_id, version, user_id),
  FOREIGN KEY (folder_id, version) REFERENCES folder_keys (folder_id, version) ON DELETE CASCADE
);
CREATE INDEX folder_grants_by_user ON folder_grants (user_id);

-- Folder keys locked with the secret of an invite's link, for a new person.
CREATE TABLE invite_keys (
  invite_id UUID NOT NULL REFERENCES invites (id) ON DELETE CASCADE,
  folder_id UUID NOT NULL,
  version   INTEGER NOT NULL,
  locked    BYTEA NOT NULL,
  PRIMARY KEY (invite_id, folder_id, version)
);

-- Folder keys locked with the secret of a PIN link that shows its folder.
CREATE TABLE pin_keys (
  pin_id  UUID NOT NULL REFERENCES pins (id) ON DELETE CASCADE,
  version INTEGER NOT NULL,
  locked  BYTEA NOT NULL,
  PRIMARY KEY (pin_id, version)
);

-- Checks before keys are passed on (docs/e2ee-plan.md, contract/crypto/check.json): a device
-- with keys asks the device or person that gets them to show the same code. The server only
-- relays the commitment, the two one-time keys and the confirmation; a check is good for 15
-- minutes.
CREATE TABLE key_checks (
  id           UUID PRIMARY KEY,
  asker        UUID NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
  device_id    UUID REFERENCES devices (id) ON DELETE CASCADE, -- a device of the asker's person, waiting for the person's key
  user_id      UUID REFERENCES users (id) ON DELETE CASCADE,   -- another person, waiting for folder keys
  commitment   BYTEA NOT NULL,
  answer       BYTEA,                                          -- the waiting side's one-time key
  answered_by  UUID REFERENCES devices (id) ON DELETE CASCADE,
  reveal       BYTEA,                                          -- the asker's one-time key, after the answer
  confirmation BYTEA,                                          -- what the asker hands on after Allow
  created_at   TIMESTAMPTZ(3) NOT NULL,
  CHECK ((device_id IS NULL) <> (user_id IS NULL))
);
CREATE UNIQUE INDEX key_checks_device ON key_checks (asker, device_id) WHERE device_id IS NOT NULL;
CREATE UNIQUE INDEX key_checks_user ON key_checks (asker, user_id) WHERE user_id IS NOT NULL;
CREATE INDEX key_checks_for_device ON key_checks (device_id);
CREATE INDEX key_checks_for_user ON key_checks (user_id);
