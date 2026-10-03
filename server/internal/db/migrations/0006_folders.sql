-- 0006: folders. Every file lies in exactly one folder, and every folder has its own people:
-- admins see every folder, members the ones they were given. On the drive a folder is a
-- directory in storage_dir with the day folders inside, and files.rel_path is relative to it
-- (2026-09-27/IMG_1.jpg), so renaming a folder changes one row. The program makes the first
-- folder right after this migration (storage.EnsureFirstFolder), named after the server; it
-- gets every file, every PIN and every member there is.
CREATE TABLE folders (
  id            TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  dir           TEXT NOT NULL,   -- the directory in storage_dir; '' is storage_dir itself
  renaming_from TEXT,            -- set while files may still lie under this older dir ('' = storage_dir)
  created_by    TEXT NOT NULL,   -- a user id, 'cli' or 'first-start'
  created_at    INTEGER NOT NULL,
  deleted_at    INTEGER,         -- its files are in the trash; restoring one brings the folder back
  deleted_by    TEXT
) STRICT;
CREATE UNIQUE INDEX folders_live_name ON folders (name COLLATE NOCASE) WHERE deleted_at IS NULL;
-- A deleted folder keeps its directory until its last file is purged: restores go back there.
CREATE UNIQUE INDEX folders_dir ON folders (dir COLLATE NOCASE);

-- The members who see a folder. Admins see every folder without a row.
CREATE TABLE folder_people (
  folder_id TEXT NOT NULL REFERENCES folders (id) ON DELETE CASCADE,
  user_id   TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  PRIMARY KEY (folder_id, user_id)
) STRICT, WITHOUT ROWID;
CREATE INDEX folder_people_by_user ON folder_people (user_id);

-- The folders a new person gets when accepting an invite.
CREATE TABLE invite_folders (
  invite_id TEXT NOT NULL REFERENCES invites (id) ON DELETE CASCADE,
  folder_id TEXT NOT NULL REFERENCES folders (id) ON DELETE CASCADE,
  PRIMARY KEY (invite_id, folder_id)
) STRICT, WITHOUT ROWID;
CREATE INDEX invite_folders_by_folder ON invite_folders (folder_id);

-- With foreign keys on, added columns that reference a table must default to NULL; the
-- program fills folder_id in for every file and PIN and never leaves it out.
ALTER TABLE files ADD COLUMN folder_id TEXT REFERENCES folders (id);
-- A move to another folder in progress: '<folder id>/<rel_path>' the bytes come from.
ALTER TABLE files ADD COLUMN moved_from TEXT;
ALTER TABLE pins ADD COLUMN folder_id TEXT REFERENCES folders (id) ON DELETE SET NULL;
-- Guests with the PIN also see and download what is in its folder.
ALTER TABLE pins ADD COLUMN shows_folder INTEGER NOT NULL DEFAULT 0 CHECK (shows_folder IN (0, 1));

-- Paths are unique within a folder; folder directories are unique, so on the drive too.
DROP INDEX files_rel_path;
CREATE UNIQUE INDEX files_rel_path ON files (folder_id, rel_path COLLATE NOCASE) WHERE state IN ('finalizing', 'ready');
CREATE INDEX files_by_folder ON files (folder_id, state, uploaded_at DESC, id);
CREATE INDEX files_moving ON files (id) WHERE moved_from IS NOT NULL;
CREATE INDEX pins_by_folder ON pins (folder_id);
