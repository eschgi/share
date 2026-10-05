-- 0008: end-to-end encryption (docs/e2ee-plan.md). The server keeps public keys, and private
-- keys only sealed for someone or locked with a secret or a password: nothing here opens a
-- file.

-- A person's key: its public key, and its private key locked with their password. Their phones
-- and browsers have keys of their own; the person's private key is sealed for each of them in
-- person_keys.
ALTER TABLE users ADD COLUMN public_key BLOB;
ALTER TABLE users ADD COLUMN password_lock BLOB;
ALTER TABLE devices ADD COLUMN public_key BLOB;

CREATE TABLE person_keys (
  device_id  TEXT PRIMARY KEY REFERENCES devices (id) ON DELETE CASCADE,
  sealed     BLOB NOT NULL,
  created_at INTEGER NOT NULL
) STRICT, WITHOUT ROWID;

-- An encrypted folder's key pairs: version 1, and a new one each time someone loses the
-- folder. recovery_sealed is the private key sealed for the recovery key.
CREATE TABLE folder_keys (
  folder_id       TEXT NOT NULL REFERENCES folders (id) ON DELETE CASCADE,
  version         INTEGER NOT NULL,
  public_key      BLOB NOT NULL,
  recovery_sealed BLOB,
  created_by      TEXT NOT NULL,
  created_at      INTEGER NOT NULL,
  PRIMARY KEY (folder_id, version)
) STRICT, WITHOUT ROWID;

-- A folder's private keys, sealed for each person who sees the folder.
CREATE TABLE folder_grants (
  folder_id  TEXT NOT NULL,
  version    INTEGER NOT NULL,
  user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  sealed     BLOB NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (folder_id, version, user_id),
  FOREIGN KEY (folder_id, version) REFERENCES folder_keys (folder_id, version) ON DELETE CASCADE
) STRICT, WITHOUT ROWID;
CREATE INDEX folder_grants_by_user ON folder_grants (user_id);

-- Keys locked with the secret of an invite's link: folder keys for a new person, or the
-- person's own key ('' and 0) for a new phone or browser.
CREATE TABLE invite_keys (
  invite_id TEXT NOT NULL REFERENCES invites (id) ON DELETE CASCADE,
  folder_id TEXT NOT NULL,
  version   INTEGER NOT NULL,
  locked    BLOB NOT NULL,
  PRIMARY KEY (invite_id, folder_id, version)
) STRICT, WITHOUT ROWID;

-- Folder keys locked with the secret of a PIN link that shows its folder. The secret itself is
-- sealed for a version of the folder's key, so that later versions can be locked for it too.
CREATE TABLE pin_keys (
  pin_id  TEXT NOT NULL REFERENCES pins (id) ON DELETE CASCADE,
  version INTEGER NOT NULL,
  locked  BLOB NOT NULL,
  PRIMARY KEY (pin_id, version)
) STRICT, WITHOUT ROWID;
ALTER TABLE pins ADD COLUMN secret_sealed BLOB;
ALTER TABLE pins ADD COLUMN secret_version INTEGER;

-- A folder whose new files are encrypted, and one that needs a new key version because
-- someone lost it.
ALTER TABLE folders ADD COLUMN encrypted INTEGER NOT NULL DEFAULT 0 CHECK (encrypted IN (0, 1));
ALTER TABLE folders ADD COLUMN rekey INTEGER NOT NULL DEFAULT 0 CHECK (rekey IN (0, 1));

-- An encrypted file: the folder key version its key is sealed for, that sealed key, the header
-- of its contents and its plain size. size stays the size of the stored bytes.
ALTER TABLE files ADD COLUMN enc_version INTEGER;
ALTER TABLE files ADD COLUMN enc_key BLOB;
ALTER TABLE files ADD COLUMN enc_header BLOB;
ALTER TABLE files ADD COLUMN plain_size INTEGER;

-- The server neither makes thumbnails of encrypted files nor reads their checksums.
DROP INDEX files_thumb_pending;
CREATE INDEX files_thumb_pending ON files (uploaded_at) WHERE state = 'ready' AND thumb = 'none' AND kind = 'photo' AND enc_version IS NULL;
DROP INDEX files_crc_pending;
CREATE INDEX files_crc_pending ON files (uploaded_at) WHERE state = 'ready' AND crc32 IS NULL AND enc_version IS NULL;
