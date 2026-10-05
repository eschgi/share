# End-to-end encryption plan

Encrypting a folder's files so that only the family's phones and browsers can read them: not the
bucket's provider, not the database's host, not whoever copies the server's disk. Planned on
2026-10-05, on the branch `feature/cloud`, after PostgreSQL, the thumbnails in the bucket and Cloud
Run. The formats are in [`contract/crypto`](../contract/crypto), with vectors that the server's Go
code (`server/internal/e2ee`), the website and the app are tested against.

## Decided with the user

- **Encrypted:** the contents of files and their thumbnails. Names, folders, days, sizes, photo
  dimensions and video lengths stay readable on the server, so listing, sorting and search work as
  before.
- **Per folder:** admins turn it on or off for each folder, and an admin setting "new folders are
  encrypted" makes it the default. Each file records whether it is encrypted, so a folder can hold
  both. Turning it on encrypts new files only; turning it off makes new files plain again, and the
  encrypted ones stay readable on the devices. The folder list shows a lock on encrypted folders.
- **Thumbnails:** the devices make them for all files, as before, and seal them for encrypted ones.
  The server can only check their size, so the website and the app read every thumbnail's
  dimensions from its JPEG header before decoding it.
- **Recovery:** each person's key is also kept on the server locked with their password, and admins
  get a recovery code that opens every encrypted folder.
- **Both storage modes:** a drive keeps an encrypted folder's files as ciphertext in the day folders
  (names readable), tus uploads encrypt while sending, ZIPs leave encrypted files out, and sealed
  thumbnails stay in `data_dir/thumbs` or the bucket.
- **The command line has no keys:** `share invite`, `share pin create --show` and anything else that
  would hand out an encrypted folder refuse it and point to the app or the website.
- **No new packages:** WebCrypto in the browser, `javax.crypto` on Android, Go's standard library on
  the server.
- **The recovery code has 160 random bits**, not the 256 first planned: 32 characters to type from
  paper instead of 52, still far out of reach of guessing (contract/crypto/recovery.json).

## What it protects, and what not

It protects the contents and thumbnails of encrypted folders from the bucket's provider, the
database's host, leaked bucket keys or links, and anyone who copies the server's disk or database.

It doesn't protect:
- names and the other readable data above;
- against a server that has been taken over and sends the browsers changed website code, since the
  website comes from the server (the Android app's code doesn't);
- a PIN link that shows its folder: whoever has the link can read the folder, by design;
- files a removed person already downloaded;
- a password backup better than the password: the server sees passwords at sign-in.

The README says so.

## Keys

All keys are P-256. A public key is sent as 65 bytes (0x04, X, Y), a private key as its 32-byte
scalar, everything binary in JSON as base64url without padding.

- **Device key**, one per signed-in phone or browser. The private key never leaves the device: a
  non-extractable WebCrypto key in IndexedDB, or in the app a software key kept in `SecretStore`
  (AES-GCM under an Android KeyStore key, like the device token).
- **Person key**, one per person. The private key is sealed for each of the person's devices, and
  locked with their password when they have one. Granting a folder seals the folder key for the
  person, not for every device.
- **Folder key**, per encrypted folder, in versions: a new version when someone loses the folder.
  The public key is on the server for everyone who may send into the folder, PIN guests included;
  the private key is there only sealed: for each person who sees the folder (every admin does), for
  the recovery key, and locked for PIN links that show the folder.
- **File key**, 32 random bytes per file, sealed for the folder key's newest version when the upload
  starts. The contents' and the thumbnail's keys come from it with HKDF. So a PIN guest can send into
  an encrypted folder but can't read it.
- **Recovery key**, one per server. Its private key is locked with the recovery code, which the
  admin who turns on the first encrypted folder sees once.

## Formats (contract/crypto)

- **Sealing** a key for a public key: HPKE base mode (RFC 9180), DHKEM(P-256, HKDF-SHA256),
  HKDF-SHA256, AES-256-GCM. The sealed bytes are the encapsulated key (65 bytes), then the
  ciphertext. HPKE's info names the purpose, its associated data the context:

  | What | Purpose (info) | Context (aad) |
  |------|----------------|---------------|
  | a file key, for a folder key | `share-e2ee-v1/file` | `folder:<folder id>:<version>` |
  | a folder key, for a person or the recovery key | `share-e2ee-v1/folder` | `folder:<folder id>:<version>` |
  | a person key, for a device | `share-e2ee-v1/person` | `person:<user id>` |

  The context keeps a server from passing one folder's key off as another's, which would make a
  member encrypt into a folder that others see.
- **Locking** with a key: a random 12-byte nonce, then AES-256-GCM. A link's secret (an invite's or a
  PIN's, 32 random bytes) or the recovery code gives the key with HKDF-SHA256 (no salt, the purpose
  as info: `share-e2ee-v1/invite`, `share-e2ee-v1/pin`, `share-e2ee-v1/recovery`). A password lock
  starts with a 16-byte salt and the iteration count (4 bytes, big endian; 600 000 today); the key is
  PBKDF2-HMAC-SHA256 of the password.
- **Contents:** a 16-byte header ("SHE1", the chunk size 65536, a random 7-byte nonce prefix, a zero
  byte), then chunks of 64 KiB, each AES-256-GCM with a key from the file key (HKDF, salted with the
  header) and the nonce prefix, the chunk's number and a last-chunk flag. Encrypted size = 16 +
  plain size + 16 per chunk. The same file key, header and contents always give the same bytes, so a
  piece sent again after a reload is the one sent before, and any range can be read from the chunks
  around it.
- **Thumbnails:** the JPEG locked with a key from the file key (HKDF, `share-e2ee-v1/thumb`).
- **Recovery code:** 20 random bytes as 32 characters of Crockford's base32, in groups of four.

## How it works

**Every start of the website or the app, signed in,** asks `GET /api/keys` and does what is due:
- sends this device's public key, if the server doesn't have it (new device, or a browser whose
  storage was cleared);
- makes the person key if the person has none yet, sealed for this device;
- opens the person key sealed for this device, then the folder keys sealed for the person;
- then works through the server's to-do list, with the keys it holds: seals the person key for the
  person's other devices that lack it, seals folder keys for people who see the folder and lack
  them, seals folder keys for the recovery key, makes a folder's next key version when one is due,
  and locks new versions for PIN links that show the folder.

So sealing for others always happens the same way: whoever is online with the key does it. Turning
on encryption only seals the new folder key for the admin who does it and for the recovery key; the
admin's device then finds everyone else on its to-do list at once.

**A device that has no person key yet** (a new browser after a sign-in, a phone whose key was lost)
shows encrypted files as locked and says it waits for another phone or browser of the person. A
password sign-in opens the person key from its password lock at once. Admins can also use the
recovery code. If no other device will come, "start over" makes a new person key: the person loses
their folder keys until someone who has them is online, which admins always are sooner or later.

**Turning encryption on** for a folder (admins): the device makes the recovery key first if there is
none and shows its code, then the folder key, seals it for the admin's person key and the recovery
key, and the server marks the folder encrypted. New uploads into it must be encrypted from then on;
uploads that started before stay plain. Turning it off marks the folder plain again.

**Sending** into an encrypted folder: the device makes a file key and a header, seals the file key
for the folder key's newest version, and keeps the file key with the upload until it is done, so a
reload or an interruption continues with the same bytes. tus and S3 get the encrypted size and the
pieces of the encrypted stream; the server also gets the plain size and the header, and checks that
the sizes match. The thumbnail is sealed before it is sent.

**Reading:** thumbnails and photos are fetched as before and decrypted on the device. The website's
service worker serves videos and downloads decrypted, with ranges, so a video seeks; without a
service worker (the first visit, `npm run dev`), the page decrypts into memory. The app decrypts
into its cache to play, save, share or open a file. Downloading several encrypted files goes one by
one; saving into a folder (Chrome and Edge) decrypts while writing.

**Invites:** the inviting device locks every version of the invite's encrypted folders' keys (all
encrypted folders for an admin) with a secret that only the link carries, after a dot:
`/join#shi_<token>.<secret>`, and in the app's link as `&key=<secret>`. Accepting the invite returns
the locked keys; the new device opens them, makes its keys and seals the folder keys for the new
person. An invite for a new phone or browser of someone locks their person key the same way. A link
without the secret still works; the device then waits for the others like any new device.

**Losing a folder:** when someone is removed, or no longer sees a folder, the server drops what was
sealed for them and marks the folder for a new key version, which the next device online with the
key makes. New files use the new version; older ones keep theirs.

**Moving** encrypted files into another folder: the admin's device seals their file keys for the
target folder's newest key, and the server takes the move only with them. Encrypted files can't move
into a folder that has never been encrypted.

**PINs:** a PIN that sends into an encrypted folder gets the folder's public key with its session,
and the guest's browser encrypts. A PIN that also shows the folder needs a link with a secret
(`/#<code>.<secret>`): the admin's device locks the folder's keys with it, and seals the secret for
the folder key, so that later versions can be locked for it too. The code alone still sends, and
shows encrypted files as locked.

## API (contract/api)

New:
- `GET /api/keys`: this device's public key, the person's public key with the person key sealed for
  this device and its password lock, the folder keys sealed for the person (folder, version, public
  key, newest or not), the recovery public key, and the to-do list (devices, people, recovery, new
  versions, PINs), each with what sealing it needs.
- `PUT /api/keys/device` `{public_key}`; changing it drops what was sealed for the old one.
- `PUT /api/keys/person` `{public_key, sealed}`: the person key, sealed for this device; with
  `"start_over": true` it replaces one, dropping what was sealed for the old one.
- `PUT /api/keys/password-lock` `{password_lock}`, also accepted with `PUT /api/me/password`.
- `POST /api/keys/grants` `{devices, people, recovery, pins}`: what this device sealed or locked from
  its to-do list. The server takes only what the caller may give: its own person's devices, and
  folders that both the caller and the receiver see.
- `PUT /api/folders/{id}/encryption` `{encrypted, key?}`: turning it on the first time brings
  version 1 of the folder key, sealed for the caller and for the recovery key.
- `POST /api/folders/{id}/keys`: the next version, sealed the same way.
- `GET` and `PUT /api/recovery` (admins): the recovery key, locked with the code, and the folder keys
  sealed for it.
- `GET` and `PUT /api/admin/settings` `{new_folders_encrypted}`.
- `GET /api/pin/keys`: for a PIN session that shows its folder, the folder keys locked for its link.

Changed:
- `FileInfo` gets `enc` (`{version, key, header}` or null); `size` is the plain size.
- `FolderInfo` gets `encrypted` and `key_version` (the newest, or null).
- tus metadata gets `enc`, S3's `POST /api/s3/uploads` gets `enc`: `{version, key, header,
  plain_size}`; the upload's size is the encrypted size.
- `PUT /api/files/{id}/thumb` takes `application/octet-stream` for encrypted files.
- `POST /api/files/move` gets `keys` for encrypted files.
- `POST /api/invites` gets `keys`, `POST /api/users/{id}/invites` gets `person_key`, and accepting an
  invite returns them.
- `POST /api/pins` gets `secret` and `keys` for a PIN that shows an encrypted folder; `GET
  /api/session` gets `encrypt` (the folder's newest public key, or null).
- New error codes: `encryption_required` (409, an upload into an encrypted folder came plain),
  `key_outdated` (409, sealed for a key version that isn't the newest), `not_encrypted` (409, moving
  encrypted files into a folder without keys), `keys_needed` (409, the command line asked for an
  encrypted folder).

## Server (`server/`)

- `internal/e2ee`: the formats in Go, the vectors' source (`go test ./internal/e2ee -update`). The
  server only checks: public keys are P-256 points, sealed and locked keys have the right sizes, a
  header parses, and the encrypted size matches the plain size.
- Migration 0008 in both dialects:
  - `users.public_key`, `users.password_lock`, `devices.public_key`;
  - `person_keys (device_id, sealed)`, `folder_keys (folder_id, version, public_key,
    recovery_sealed, …)`, `folder_grants (folder_id, version, user_id, sealed)`, `invite_keys`,
    `pin_keys`;
  - `pins.secret_sealed`, `pins.secret_version`;
  - `folders.encrypted`, `folders.rekey`;
  - `files.enc_version`, `files.enc_key`, `files.enc_header`, `files.plain_size`;
  - the recovery key and the setting in `meta`.
- `size` keeps meaning the stored bytes everywhere on the server; `plain_size` is only for the
  devices.
- Encrypted files are left alone by the thumbnail fallback, the type sniffing (the name decides),
  the CRC worker and ZIPs. An encrypted upload that is still receiving doesn't move into another
  PIN's folder when someone unlocks again.
- Who loses a folder, from `SetFolderPerson`, `DeleteUser` and folder deletion, drops their grants
  and marks the folder for a new version.
- An admin's password reset drops the person's password lock.

## Website (`web/`, no new packages, hand-formatted)

- `src/e2ee/`: bytes and base64url, HPKE (WebCrypto ECDH, HMAC and AES-GCM), locks, contents,
  thumbnails, recovery codes; tested against `contract/crypto` in Node's WebCrypto.
- The key store: the device key in IndexedDB (`share-keys`); the keys opened at each page load; the
  to-do list worked through in the background.
- Sending: tus-js-client's `fileReader` gives the encrypted stream; the S3 plugin reads its parts
  from it; Golden Retriever keeps the file key with the upload; thumbnails are sealed.
- Reading: encrypted thumbnails and photos through decrypting hooks into blob URLs (the CSP allows
  `blob:` for images and media); videos and downloads through the service worker; saving into a
  folder decrypts while writing; "Download N" goes one by one when any file is encrypted.
- Admin pages: the folder's switch, the default for new folders, the recovery code (shown once,
  copy, print), using it; the waiting state with "start over"; invites and PINs with secrets.
- Strings in English, German and Italian.

## App (`app/`, no new packages, no dart format)

- Kotlin `e2ee/`: the same formats in plain JVM code (unit-tested against `contract/crypto`; the
  Android stubs make `android.*` useless in unit tests).
- A key manager in Kotlin, which the uploads and downloads use in the background and Dart calls
  through the platform channel: syncing with `/api/keys`, turning folders on, the recovery code,
  invites and PINs with secrets, sealing for moves.
- Uploads: the per-file key and header kept in `transfers.db` with the upload; an encrypting source
  that starts at any offset; sealed thumbnails.
- Reading: thumbnails decrypted through the channel; photos, videos and "open" or "share" decrypted
  into the cache by the fetcher; saving several decrypts while writing and resumes at a chunk.
- Dart screens for the same switches, codes and states as the website; strings in the three arb
  files.

## Order of work

1. This plan, the vectors and the Go reference.
2. The website's crypto core and the app's Kotlin crypto core, against the vectors.
3. The server: migration, keys API, uploads, thumbnails, skips, moves, invites, PINs; tests.
4. The website.
5. The app.
6. README and docs.

## Verification

- `go test ./internal/e2ee`, the website's and the app's crypto tests: all pass the same vectors.
- Server tests, on SQLite and PostgreSQL, with keys and encrypted uploads made by `internal/e2ee`:
  fixture shapes, who may give which keys, the size checks, plain uploads refused in encrypted
  folders, thumbnails and ZIPs skipping encrypted files, grants dropped and new versions due when
  someone loses a folder.
- Playwright against `share serve`, on a drive and with AIStor: an encrypted folder's objects and
  files start with the header and contain nothing of the original; the tiles, the viewer and a
  seeking video show them; a download and "save into a folder" give the original bytes; a second
  browser waits until the first grants it, or opens with the password; the recovery code restores an
  admin; a PIN guest sends but can't read back; turning encryption off makes new files plain.
- Kotlin tests for the encrypting source and the decrypting sinks; on the phone (by the user):
  send, view, play and save in an encrypted folder.

## Pitfalls

- A file's key must stay the same through every retry of its upload, or a resumed upload mixes two
  encryptions; it goes into the upload's saved state before the upload is created.
- HKDF-Extract with no salt uses 32 zero bytes as HMAC's key; WebCrypto refuses an empty HMAC key.
- WebCrypto's ECDH gives P-256's shared x-coordinate, which is HPKE's DH output; a non-extractable
  private key can still derive bits, so the device key never needs exporting.
- Service workers are gone after a while without clients and lose what the page told them: the page
  registers each stream just before using it, and decrypts itself when that fails.
- PBKDF2 with 600 000 iterations takes about half a second; do it once per sign-in, not per request.
