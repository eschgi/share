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
Keys go to a new phone, browser or person only after a check whose code the person compares on
both screens (below), so someone who can change the database can't slip in a device or a person of
their own either; and the recovery key signs every folder's keys and every decision to send a folder
plain, so they can't switch a folder's encryption off or name a key of their own for it.

It doesn't protect:
- names and the other readable data above;
- against a server that has been taken over and sends the browsers changed website code, since the
  website comes from the server (the Android app's code doesn't);
- a PIN link that shows its folder: whoever has the link can read the folder, by design;
- the few cases where a phone or browser can't check what the server says about a folder's keys
  (under "What a changed database can't fake", below): a guest who types a PIN's code, a device that
  never learned the root before the database was changed, hidden folders;
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
- **Recovery key**, one per server, also the root key that signs folder keys (below). Its private
  key is locked with the recovery code, which the admin who turns on the first encrypted folder sees
  once, and sealed for each admin's person key.

## Formats (contract/crypto)

- **Sealing** a key for a public key: HPKE base mode (RFC 9180), DHKEM(P-256, HKDF-SHA256),
  HKDF-SHA256, AES-256-GCM. The sealed bytes are the encapsulated key (65 bytes), then the
  ciphertext. HPKE's info names the purpose, its associated data the context:

  | What | Purpose (info) | Context (aad) |
  |------|----------------|---------------|
  | a file key, for a folder key | `share-e2ee-v1/file` | `folder:<folder id>:<version>` |
  | a folder key, for a person or the recovery key | `share-e2ee-v1/folder` | `folder:<folder id>:<version>` |
  | a person key, for a device | `share-e2ee-v1/person` | `person:<user id>` |
  | the recovery key's private key, for an admin's person key | `share-e2ee-v1/root` | `root` |

  The context keeps a server from passing one folder's key off as another's, which would make a
  member encrypt into a folder that others see.
- **Locking** with a key: a random 12-byte nonce, then AES-256-GCM. A link's secret (an invite's or a
  PIN's, 32 random bytes) or the recovery code gives the key with HKDF-SHA256 (no salt, the purpose
  as info: `share-e2ee-v1/invite`, `share-e2ee-v1/pin`, `share-e2ee-v1/recovery`). A password lock
  starts with a 16-byte salt and the iteration count (4 bytes, big endian; 600 000 today); the key is
  PBKDF2-HMAC-SHA256 of the password. A key's private key gives keys the same way: a folder key's
  for its PIN links' secrets (`share-e2ee-v1/pin-secret`, aad `folder:<folder id>:<version>`), a
  person key's for their note (`share-e2ee-v1/note`, aad `note:<user id>`). An invite's or a PIN's
  link locks the root's public key with aad `root`.
- **Signatures** (sign.json): ECDSA on P-256 with SHA-256 over the messages under "What a changed
  database can't fake", as r‖s in 64 bytes.
- **Contents:** a 16-byte header ("SHE1", the chunk size 65536, a random 7-byte nonce prefix, a zero
  byte), then chunks of 64 KiB, each AES-256-GCM with a key from the file key (HKDF, salted with the
  header) and the nonce prefix, the chunk's number and a last-chunk flag. Encrypted size = 16 +
  plain size + 16 per chunk. The same file key, header and contents always give the same bytes, so a
  piece sent again after a reload is the one sent before, and any range can be read from the chunks
  around it.
- **Thumbnails:** the JPEG locked with a key from the file key (HKDF, `share-e2ee-v1/thumb`).
- **Recovery code:** 20 random bytes as 32 characters of Crockford's base32, in groups of four.
- **Checks** (check.json): the commitment is SHA-256 of `share-e2ee-v1/check` and the asking
  device's one-time public key; the code is the first 4 bytes of SHA-256 of `share-e2ee-v1/code`,
  both one-time keys and the public key that gets the keys, as a big-endian number modulo a million,
  in 6 digits. The confirmation is locked with HKDF-SHA256 of the two one-time keys' ECDH secret,
  salted with SHA-256 of both one-time keys and the key that gets the keys
  (`share-e2ee-v1/confirm`), aad `check:<check id>`.

## How it works

**Every start of the website or the app, signed in,** asks `GET /api/keys` and does what is due,
and so does a check-in every half minute while the page is open (behind other tabs too, which
browsers allow about once a minute) or the app is in front, and when either comes back (quietly:
the status doesn't pass through loading, and stays as it is without an answer):
- sends this device's public key, if the server doesn't have it (new device, or a browser whose
  storage was cleared);
- makes the person key if the person has none yet, sealed for this device;
- opens the person key sealed for this device, then the folder keys sealed for the person;
- answers the checks other devices ask of it (below);
- then works through the server's to-do list, with the keys it holds: asks before passing the
  person key on to the person's other devices that lack it, and, on an admin's device, folder keys
  to people who see the folder and lack them, unless their key was checked on this device before; seals folder keys for
  the recovery key, makes a folder's next key version when one is due, and locks new versions for
  PIN links that show the folder.

So passing keys on always happens the same way: whoever has Share open with the key lists who
waits, the person opens one with Show, compares the code and allows it, and the device that waits
opens the key at its own next check-in. Turning on encryption only seals the new folder key for the admin who does it and for the
recovery key; the admin's device then finds everyone else on its to-do list at once.

**Checks** (decided on 2026-10-06, screens 50 to 55 of the mockup): before a device passes keys on,
both screens show the same code of 6 digits, made from the key that would get them. The server only
relays it, and can't show the code of a key of its own:
- the asking device opens a check with a commitment to a one-time public key
  (`POST /api/keys/checks`);
- the device it is for, or a device of the person it is for that holds their key, answers with a
  one-time public key of its own, and keeps the commitment it saw;
- the asking device reveals its key for the answer it saw, which the server takes only while it
  is still the answer; from then on the answer can't change;
- both make the code from both one-time keys and the key that gets the keys: the waiting device
  from its own key, or its person's, after checking the revealed key against the commitment it
  kept; the asking device from the key on its to-do list and the answer it revealed for. Neither
  one-time key can be chosen after seeing the other, so whoever relays them has one chance in a
  million to make two keys show the same code;
- after Allow, the asking device puts its confirmation on the check (`PUT
  /api/keys/checks/{id}/confirm`), which only the waiting side opens, as only the two of them have
  the secret of the two one-time keys.

Nothing opens by itself (the user's choice): the asking device lists who waits over the library,
under "Waiting for your OK": a new phone or browser of the person, with its name, or, on an admin's
phones and browsers, a person whose folders wait. Only admins pass folder keys on to other people
(also the user's choice): a member's to-do list names no one else, the server refuses a member's
check for someone else and leaves out folder keys a member seals for someone else; folder keys a
person seals for themselves, from an invite's link or the recovery code, still count. Show opens a dialog (a sheet in the app) with when the phone or browser signed in, or
the person's folders, and only then starts the check, so the code comes on both screens a few
seconds later. Allow hands the person's key to a device of theirs in the confirmation, which the
device seals for itself, or seals folder keys, and the root's private key for an admin, for the
person's key that was checked; Not me signs that phone or browser out; Not now, or closing the dialog, ends the check, and the ask stays listed. Several are listed
together, each opened on its own, with its own check and code. A person's key, once allowed, is
remembered on that device (in the device's IndexedDB record, or the app's `SecretStore`), so new
versions and new folders reach them at once, until their key changes. Only those seen in the last
15 minutes are listed (`active`), as a check needs someone to answer it, and nobody is listed while
the person sees no encrypted folder. While a check runs, opened with Show or answered, devices
check in every 2 seconds; a device that waits for keys every 5 seconds; otherwise every half
minute. Signing in with the password and joining with an invite link ask nothing: they bring their
keys with them.

**What a changed database can't fake** (decided on 2026-10-07): someone who can change the
database, its host or whoever has its password, must not be able to make a phone or browser send a
file plain, or encrypted for a key of theirs, where the folder's admins didn't decide so; nor get
folder keys sealed or locked for a key or secret of theirs.

- **The root key** is the recovery key. It signs (ECDSA on P-256 with SHA-256, the signature as r‖s
  in 64 bytes, contract/crypto/sign.json):

  | What is signed | The message |
  |----------------|-------------|
  | a version of a folder's key | `share-e2ee-v1/sign/folder-key` ‖ `folder:<id>:<version>` ‖ its public key |
  | a folder that sends plain: never encrypted, or switched off | `share-e2ee-v1/sign/plain` ‖ `folder:<id>:<its newest version, or 0>` ‖ `\n` ‖ its name |
  | the key of a new recovery code | `share-e2ee-v1/sign/root` ‖ the new public key, signed with the old one |

  Admins' phones and browsers hold its private key too, sealed for the admin's person key
  (`root_grants`; HPKE, `share-e2ee-v1/root`, aad `root`): given like folder keys, with an OK, or at
  once to an admin whose key was checked on that device before. With it they make folders, switch
  encryption, rename a folder that sends plain, and make a folder's next key version; nothing is
  signed by itself otherwise. Turning encryption on always makes a new version, so a plain statement
  signed for an older one can't be shown again; renaming a plain folder signs it anew. A new
  recovery code signs its key with the old one, and signs every version and every plain statement
  anew, after checking each under the old one; phones and browsers follow the chain of roots and
  take only signatures by the newest. The first recovery key signs every folder there is as plain.
- **Each phone and browser trusts one root,** and keeps it with the person's public key and the
  newest version of each folder's key it has seen (IndexedDB, or the app's `SecretStore`). It learns
  the root where the database can't change it: the device made it, or opened it with the recovery
  code; an invite's link carries it, locked with the link's secret (aad `root`); a check hands it on
  (below); or **the person's note**, which holds the root and those versions, locked with a key from
  the person's private key (HKDF, `share-e2ee-v1/note`; AES-GCM, aad `note:<user id>`), so their
  next phone or browser, e.g. one signed in with the password, finds them. Each device writes what it
  saw into the note. A device that has none of these (it was used before encryption was set up)
  takes the root the server shows the first time, and keeps it.
- **Sending into a folder**, once there is a root: the newest version of the folder's key must be
  signed by the root, or nothing is sent; an encrypted folder gets the file encrypted for it. A
  folder sends plain only with its plain statement, signed for its newest version and its name;
  without one, a folder with keys gets the file encrypted, which the server takes, and one without
  keys gets nothing. A folder showing fewer versions than seen before gets nothing either. Before
  any root, files go plain, as no folder can have keys.
- **Keys that are opened** must match: a folder's private key its signed public key; the person's
  key the one the device keeps, or the one a check, the password, an invite or the recovery code
  gave.
- **The recovery key's seals** go only to the root the device trusts: a database naming another
  recovery key gets nothing sealed for it.
- **A PIN link's secret** is locked with a key from the folder's private key (HKDF,
  `share-e2ee-v1/pin-secret`; aad `folder:<id>:<version>`), not sealed for its public key, so only
  someone who holds the folder's key can make one, and phones and browsers lock new versions only
  for a secret that opens that way.
- **Checks use one-time keys** instead of nonces: the asking device commits to its one-time public key
  (SHA-256 of `share-e2ee-v1/check` ‖ key); the other side answers with its own; the asking device
  reveals its key; the code is made from `share-e2ee-v1/code` ‖ the asking key ‖ the answering key ‖
  the key that gets the keys. Both sides make the same secret from the two one-time keys (ECDH).
  After Allow, the asking device puts a confirmation on the check, locked with a key from that
  secret (HKDF-SHA256, salted with SHA-256 of the asking key, the answering key and the key that
  gets the keys, `share-e2ee-v1/confirm`; AES-GCM, aad `check:<id>`): the root, and for a device of
  its person the person's key pair, which the device then seals for itself. Folder keys and the root's
  private key for another person are sealed for their checked key, as before. The waiting side keeps
  its one-time private key in memory until it reads the confirmation, then closes the check.
- **PIN links** into an encrypted folder carry the root: a PIN that only sends has its fingerprint
  after the code (`#<code>.<the first 16 bytes of SHA-256 of the root, base64url>`), a PIN that shows
  its folder has the root locked with its link's secret (aad `root`). A guest with such a link
  always sends encrypted, for the newest version, signed by the root the link names (following the
  chain); never plain.
- **What still can't be checked:** a guest who types a PIN's code, or has a link made while the
  folder was plain; a phone or browser that never learned the root, while the database is changed
  before it first sees one; a phone or browser that signs in for the first time with only the
  password, while the database also brings back an older note of the person and leaves out the
  versions since; folders hidden, so that the person sends into another one, plain if it is; and
  changed website code from a server that was taken over, as the website comes from the server.

**A device that has no person key yet** (a new browser after a sign-in, a phone whose key was lost)
shows encrypted files as locked and says it waits for approval on another phone or browser of the
person, where Share lists it under Show, with the code that one shows too once it is opened. A person whose folder keys wait for an admin sees the code
to tell them. A password sign-in opens the person key from its password lock at once. Without a lock (the person
joined with an invite, or an admin gave them a new password), the password typed stays in the
device's memory until the key comes, and then locks it, so the next device opens it at once.
Admins can also use the recovery code. If no other device will come, "start over" makes a new person
key: the person loses their folder keys until someone who has them is online, which admins always
are sooner or later. A member's device does that by itself when the key is lost, which `held_by`
says: no phone or browser holds it any more, and no password lock opens it. An admin's device asks
instead, since the recovery code opens every folder again.

**Turning encryption on** for a folder (admins): the device makes the recovery key first if there is
none and shows its code, then the folder key's next version, signs it with the root, seals it for
the admin's person key and the recovery key, and the server marks the folder encrypted. New uploads
into it must be encrypted from then on; uploads that started before stay plain. Turning it off
signs the folder's plain statement for its newest version and name, and marks the folder plain
again. A new folder comes with either, under an id the device picks, so that its signature can name
it. All three need the root's private key on the device: an admin whose device doesn't hold it yet
waits for another admin's OK.

**Sending** into an encrypted folder: the device makes a file key and a header, seals the file key
for the folder key's newest version, and keeps the file key with the upload until it is done, so a
reload or an interruption continues with the same bytes. tus and S3 get the encrypted size and the
pieces of the encrypted stream; the server also gets the plain size and the header, and checks that
the sizes match. The thumbnail is sealed before it is sent.

**Reading:** thumbnails and photos are fetched as before and decrypted on the device. The website's
service worker serves videos and downloads decrypted, with ranges, so a video seeks; without a
service worker (the first visit, `npm run dev`), the page decrypts into memory. The app's player
plays an encrypted video or sound from a small server on 127.0.0.1 that decrypts while it streams,
with ranges, the way the service worker does; saving into the gallery or Downloads decrypts while
writing and resumes at a chunk; opening or sharing a file decrypts into the cache. Downloading
several encrypted files goes one by one; saving into a folder (Chrome and Edge) decrypts while
writing.

**Invites:** the inviting device locks every version of the invite's encrypted folders' keys (all
encrypted folders for an admin) with a secret that only the link carries, after a dot:
`/join#shi_<token>.<secret>`, and in the app's link as `&key=<secret>`, and locks the root with it
too, so a link has a secret whenever there is a root. Accepting the invite returns the locked keys;
the new device opens them, takes the root, makes its keys and seals the folder keys for the new
person. An invite for a new phone or browser of someone locks their person key the same way. A link
without the secret still works; the device then waits for the others like any new device.

**Losing a folder:** when someone is removed, or no longer sees a folder, the server drops what was
sealed for them and marks the folder for a new key version, which the next device online with the
key makes. New files use the new version; older ones keep theirs.

**Moving** encrypted files into another folder: the admin's device seals their file keys for the
target folder's newest key, and the server takes the move only with them. Encrypted files can't move
into a folder that has never been encrypted.

**PINs:** a PIN that sends into an encrypted folder gets the folder's newest public key, with its
signature and the chain of roots, with its session, and the guest's browser encrypts; its link names
the root (`/#<code>.<fingerprint>`). A PIN that also shows the folder needs a link with a secret
(`/#<code>.<secret>`): the admin's device locks the folder's keys and the root with it, and locks the
secret with a key from the folder's key, so that later versions can be locked for it too. The code
alone still sends, unchecked, and shows encrypted files as locked.

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
- Checks: `POST /api/keys/checks` `{device | user, commitment}`, `PUT /api/keys/checks/{id}/answer`
  `{nonce}`, `PUT /api/keys/checks/{id}/reveal` `{nonce, answer}`, `DELETE /api/keys/checks/{id}`;
  `GET /api/keys` lists the open checks a device takes part in, and its to-do list names the
  devices and people that wait, and whether they were seen lately (`active`). A check lives 15
  minutes.

Changed:
- `FileInfo` gets `enc` (`{version, key, header, plain_size}` or null); `size` is the plain size.
- `FolderInfo` gets `encrypted` and `key_version` (the newest, or null).
- tus metadata gets `enc`, S3's `POST /api/s3/uploads` gets `enc`: `{version, key, header,
  plain_size}`; the upload's size is the encrypted size.
- `PUT /api/files/{id}/thumb` takes `application/octet-stream` for encrypted files.
- `POST /api/files/move` gets `keys` for encrypted files.
- `POST /api/invites` gets `keys`, `POST /api/users/{id}/invites` gets `person_key`, and accepting an
  invite returns them.
- `POST /api/pins` gets `secret` (`{sealed, version, keys}`) for a PIN that shows an encrypted
  folder; `GET /api/pins` and the answers about a PIN give `{sealed, version}` of it back, so an
  admin's device opens the secret with the folder key and hands on the whole link again. `GET
  /api/session` and unlocking a PIN give `encrypt` (the folder's newest public key, or null).
- `POST /api/downloads` lists each file's folder and `enc`, so one by one and saving into a folder
  can decrypt.
- What a changed database can't fake (2026-10-07):
  - `GET /api/keys` adds `person.note`, `folders[].signature`, `roots` (the chain, oldest first:
    `{public_key, signature}`, the first without one), `root_sealed` (the newest root's private key
    sealed for the person), `todo.roots` (admins whose person lacks it: `{user, name, public_key,
    active}`, for a device whose person holds it) and `checks[].confirm`; `recovery_key` goes, as the
    newest root is it. `PUT /api/keys/note` `{note}`. `POST /api/keys/grants` adds `roots` (`{user,
    sealed}`).
  - Checks: `PUT …/answer` `{key}`, `PUT …/reveal` `{key, answer}`, and `PUT …/confirm`
    `{confirm}` by the asking device after its reveal. A device with a confirmation waiting isn't on
    anyone's to-do list.
  - Folders, once there is a root: `POST /api/folders` `{id, name, key | plain_signature}`; `PATCH
    /api/folders/{id}` `{name, plain_signature}` for a folder that sends plain; `PUT
    /api/folders/{id}/encryption` `{encrypted: true, key}` (always the next version) or `{encrypted:
    false, plain_signature}`; a folder key's version always brings its `signature`. `FolderInfo` adds
    `plain_signature`. The server checks every signature against the newest root.
  - `GET /api/recovery` adds `roots` and `sign`: `{folder_keys: [{folder, version, public_key,
    signature}], plain: [{folder, version, name, signature}]}`, all a new recovery code signs anew;
    `PUT /api/recovery` `{public_key, locked, signature, sealed, folder_keys, plain}` takes it only
    complete, and with the old root's signature once there is one.
  - Invites add `root` (the root locked with the link's secret), and accepting one returns it. A
    PIN's `secret` is `{locked, version, keys, root}`, `GET /api/pins` gives `{locked, version}`
    back, and `GET /api/pin/keys` adds `root`. A PIN session's `encrypt` becomes `folder_key`:
    `{folder, version, public_key, signature, encrypted}`, the folder's newest key whenever it has
    one, with `roots`.
- New error codes: `encryption_required` (409, an upload into an encrypted folder came plain),
  `key_outdated` (409, sealed for a key version that isn't the newest), `not_encrypted` (409, moving
  encrypted files into a folder without keys, or an encrypted upload into one), `key_exists` (409,
  a person key that is there already), `no_key` (409, what needs a key that isn't there yet),
  `not_answered` (409, a check revealed before its answer, for an answer that changed, or twice). The
  command line refuses what would give access to an encrypted folder by itself.

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
- `key_checks`: the asking device, the device or person it is for, the commitment, the answer with
  the device that gave it, the reveal and the confirmation. The server checks who may ask whom, that
  the reveal matches the commitment and comes for the answer that is there, and forgets checks after
  15 minutes.
- What a changed database can't fake, in the first schema (no release yet, so no migration):
  `roots (seq, public_key, signature, locked)` instead of the recovery key in `meta`, `root_grants
  (user_id, sealed)`, `folder_keys.signature`, `folders.plain_signature`, `users.note`,
  `invites.root_locked`, `pins.root_locked`, and `pins.secret_locked` instead of `secret_sealed`.

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
- `e2ee/Keyring.kt`, the website's keyring in Kotlin, which the uploads, downloads and the player use
  in the background and Dart calls through the platform channel (`keys.*`, and `keys` events,
  contract/app/platform.json): syncing with `/api/keys`, turning folders on, the recovery code,
  invites and PINs with secrets, sealing for moves. `Keys` wires it to Android: the device key in
  `SecretStore`, the server on the route the app picked, and after a restart it opens the keys again
  by itself. A PIN link's secret stays in `SecretStore` while the PIN works on the phone.
- Uploads: each file's seal (its key, header and sealed key, with its size and time) kept in
  `transfers.db` (version 6) until it is sent; an encrypting source that starts at any offset; tus
  and S3 get `enc`; sealed thumbnails; a 409 for another key version seals anew.
- Reading: thumbnails opened through the channel, which also checks their size; photos decrypted
  through the channel; videos and sound from `LocalStream` on 127.0.0.1; saving and "open" or
  "share" decrypt while writing, and resume at a chunk.
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
- Server tests, on PostgreSQL, with keys and encrypted uploads made by `internal/e2ee`:
  fixture shapes, who may give which keys, the size checks, plain uploads refused in encrypted
  folders, thumbnails and ZIPs skipping encrypted files, grants dropped and new versions due when
  someone loses a folder.
- Playwright against `share serve`, on a drive and with AIStor: an encrypted folder's objects and
  files start with the header and contain nothing of the original; the tiles, the viewer and a
  seeking video show them; a download and "save into a folder" give the original bytes; a second
  browser waits until the first grants it, or opens with the password; the recovery code restores an
  admin; a PIN guest sends but can't read back; turning encryption off makes new files plain.
- Checks in the browser (`approve.mjs` next to `e2e.mjs`): nothing opens by itself; after Show, a
  browser that lost its keys and the one that asks show the same code, and Allow opens its folder
  within seconds; Not now ends the check and keeps the ask listed; Not me signs a browser
  out; a member who joined without the link's secret waits for an admin with the code, and once
  allowed, a later folder reaches them without asking. The keyring tests on both sides fake a server
  that swaps a key, and one that changes what it relayed after the fact: the codes differ, or stay
  as they were.
- Kotlin tests: the keyring's flows against a fake keys API, as the website's test; downloads that
  decrypt, resume at a chunk and refuse a changed chunk; the local stream's ranges; a tus upload of
  the encrypted stream with its `enc`. Dart tests: links with secrets, the screens and what they
  hand Kotlin and the server. On the phone (by the user): send, view, play and save in an encrypted
  folder.

## Pitfalls

- A file's key must stay the same through every retry of its upload, or a resumed upload mixes two
  encryptions; it goes into the upload's saved state before the upload is created.
- The same key and header must never encrypt other bytes: the chunks' nonces would repeat. A file
  picked again, or changed in place, starts over with a new key and a new upload: the website
  compares the file's time, the app its size and time, and an app upload under way whose time can't
  be told starts over too.
- tus-js-client counts what a slice of its `fileReader` holds by its `size`: the encrypted pieces
  are Blobs, not byte arrays, or the upload stalls.
- Uppy's tus plugin sends every allowed meta field, a missing one as "undefined": `enc` is empty for
  plain files.
- Chrome sends a link's `download` past the service worker: decrypted downloads go through a hidden
  frame. While a worker streams, the page sends it a message now and then, so it isn't stopped.
- Refreshing the keys must not empty the open folder keys on the way, or a thumbnail decrypting just
  then shows a lock until the page reloads.
- HKDF-Extract with no salt uses 32 zero bytes as HMAC's key; WebCrypto refuses an empty HMAC key.
- WebCrypto's ECDH gives P-256's shared x-coordinate, which is HPKE's DH output; a non-extractable
  private key can still derive bits, so the device key never needs exporting.
- Service workers are gone after a while without clients and lose what the page told them: the page
  registers each stream just before using it, and decrypts itself when that fails.
- PBKDF2 with 600 000 iterations takes about half a second; do it once per sign-in, not per request.
