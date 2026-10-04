# S3 storage plan

Keeping Share's files in an S3-compatible bucket (AWS S3, Backblaze B2, Cloudflare R2, MinIO), with
uploads and downloads going straight to and from the bucket. Planned on 2026-10-03; not built yet.

## Context

Today every byte passes through the Go server, and behind Cloudflare also through the tunnel:
- uploads arrive by tus in `<storage_dir>/.uploads` and are renamed into `<storage_dir>/<Folder>/<day>/<name>`;
- downloads, the viewer, videos, ZIPs and the app's player read the files back from the disk.

This plan adds a second kind of storage: an S3-compatible bucket. Browsers and the app move file bytes
**straight to and from the bucket with presigned URLs**. The server keeps only the API, the database and
the thumbnails. Disk mode, today's behaviour, stays exactly as it is.

Decided with the user:
- **One storage per server**: `config.json` has either `storage_dir` (disk mode) or `s3` (S3 mode). No mixed
  mode, no migration command.
- **One object key per file id**: `<prefix>files/<id>`.
  - Folder, day, name and trash state live in the database only.
  - Moving files, renaming or deleting folders, trash and restore change only the database.
  - Purge deletes the object.
- **No ZIP in S3 mode.** "Download N" downloads the files one by one, each straight from S3. Chrome and Edge
  keep "save into a folder". So no CRC-32 is needed, and the CRC worker doesn't run.
- **minio-go** (`github.com/minio/minio-go/v7`, pure Go) as the server's S3 client.
- **Naming:**
  - S3 in every S3 name.
  - "disk" for the other storage. Not "local", which already means the home address; not "folder",
    which means Share's folders. Warnings about the physical drive (FAT32, drive full) keep saying
    "drive".
  - Upload code named by protocol: tus or S3.
  - The existing paths stay.

Also decided:
- **Clients send straight to the bucket, not by tus to the server and then on to S3.** Going through
  the server would:
  - keep every byte on the server and the tunnel, sending it out a second time to the bucket;
  - keep Cloudflare's limits;
  - need staging space on the disk;
  - leave each file in two places until it has been pushed. That is the mixed mode ruled out above.
- Thumbnails and the database stay in `data_dir`, with the same endpoints.
- tus stays for disk mode.
- The website gets its own small Uppy uploader plugin for S3, with no new npm package. `@uppy/aws-s3`
  6.x would make the browser call Create, List and Complete on S3 itself, with client-chosen keys, and
  would need ETag exposure.
- No compatibility with old apps, since the server and every app are updated together; `api_version`
  becomes 3.

## Naming

| Where | S3 names | Disk/tus names |
|---|---|---|
| Go | package `internal/s3` (`s3.Bucket`, `s3.Open`: the package name is the prefix, not `s3.S3Bucket`), fake `internal/s3/s3test`; files `config/s3.go`, `storage/s3.go`, `upload/s3.go`, `api/s3.go`, `app/s3_test.go`, `cmd/share/s3.go`; `NewS3Library`, `Library.s3`, `finalizeS3`, `terminateS3`, `reconcileS3`, `CheckS3`, `S3PartSize`/`S3PartCount`/`S3PartLen`, `upload.S3Handler`, `API.S3`, `Options.S3`, `newS3Env`, `e.s3Object` | `upload/upload.go` → `upload/tus.go`, `Handler`/`New` → `TusHandler`/`NewTusHandler` (tests follow); a disk-only `Library.OpenFile`; shared admission in `upload/admit.go` |
| Database | `0007_s3.sql`: `files.s3_upload_id`, `files.s3_part_size`, table `s3_garbage`; `SetS3UploadID`, `PurgeToS3Garbage`, `S3Garbage`, `ForgetS3Garbage` | meta key `storage` = `disk` or `s3:<bucket>/<prefix>` |
| API | `/api/s3/uploads…`, `/api/s3/files/{id}/url`; codes `s3_unavailable`, `s3_upload_finished`, `s3_parts_missing`, `s3_use_url`, `s3_no_zip`; findings `s3_unreachable`, `s3_denied`, `s3_cors`, `s3_clock_skew`; fixtures `contract/api/s3_*.json`, `play_s3` | `/api/info` `storage: "disk"\|"s3"`; `/tus/` unchanged |
| Website | `src/s3parts.ts`, `src/s3upload.ts` (class `S3Upload`), types `S3NewUpload`/`S3PartUrls`/`S3UploadStatus`, file state `s3`, `test/s3upload.test.ts` | the tus setup stays in `uploader.ts` |
| App | Kotlin `net/S3Connection.kt`, `transfer/S3Uploader.kt`, `transfer/S3Links.kt`, tests `S3UploaderTest`, `S3ConnectionTest`, `S3LinksTest`; Dart `Api.s3Bytes`, `LibraryRepository._onS3`, `test/s3_library_test.dart` | `transfer/Uploader.kt` → `transfer/TusUploader.kt` (`TusUploader`), `UploaderTest` → `TusUploaderTest`; `Uploader.Outcome`, `outcomeOf`, `copy` and `SourceError` move to a shared `transfer/UploadOutcome.kt`; `ServerInfo.Storage {DISK, S3}`, Dart `enum Storage {disk, s3}` |

Pieces used by both modes keep neutral names: `upload/admit.go`, `UploadEngine.kt`, `ServerInfo.kt`,
`HomeHosts.kt`, `downloadOneByOne`.

## How S3 mode works

**Sending**, the same for the website and the app:
1. `POST /api/s3/uploads`. The server:
   - runs the same admission as tus;
   - inserts the `receiving` row;
   - calls `CreateMultipartUpload` with a Content-Type guessed from the name and an attachment
     Content-Disposition;
   - answers the plan and the first part URLs.
2. The client PUTs the parts straight to the bucket. Each presigned URL is valid for about 1 h and has
   the part's exact `Content-Length` signed in, so a part of any other size gets a 403.
3. `POST /api/s3/uploads/{id}/complete`. The server:
   - lists the parts and checks every size;
   - calls `CompleteMultipartUpload` and `StatObject`;
   - finalizes in the database as today: claims the name and day, then `MarkReady`.
4. The client PUTs the thumbnail to the server, as now.

**What is kept where while sending:**
- The server's row: file id, folder, name, size, state `receiving`, `s3_upload_id` and `s3_part_size`.
- The bucket: the parts received so far, as an open multipart upload. After Complete, it holds the
  object `files/<id>`.
- The client:
  - the website: the upload id in the Uppy file state, which Golden Retriever persists, plus the file;
  - the app: the upload id in `TransferDb`, plus the file's URI.
  - Presigned URLs are never stored, because they expire.

**When sending fails.** Every failure costs at most one part (20 MiB by default), as tus behind
Cloudflare does today:

| What happens | The client sees | What happens next |
|---|---|---|
| Connection drops during a part | the PUT fails | The part is retried with today's backoff (website `retryDelays`, app 8 attempts). Progress falls back to the finished parts. |
| A part URL expired (after 1 h) | 403; on R2 a browser sees status 0, since the 403 has no CORS headers | New URLs are fetched and the part is retried. |
| Tab closed, app killed, phone off | – | On return, `GET /api/s3/uploads/{id}` lists the parts already in the bucket and only the missing ones are sent. On the website, a file too big for IndexedDB has to be picked again (a "ghost"), as with tus today. |
| Session ended (PIN expired, signed out) | 401 | Uploads pause as today. After a new unlock the server moves the rows to the new session and they continue. |
| Server down or busy | 5xx, 429 or a network error | Retry with backoff. The parts already sent stay in the bucket. |
| A part has the wrong size | the bucket answers 403, because Content-Length is signed | Complete checks every size with ListParts and answers 409 `s3_parts_missing`. The client asks for the status and re-sends those parts. |
| A part is re-sent while Complete runs | `CompleteMultipartUpload` answers InvalidPart | The server answers 409 `s3_parts_missing`; the client asks for the status and completes again. |
| Complete's answer gets lost (e.g. Cloudflare's 524 after 100 s) | a timeout | The client retries Complete. It is idempotent: the object is there with the right size, so the server finalizes and answers the same `{id}`. The 5-minute reconcile also finishes rows stuck in `finishing`. |
| The server crashed between steps | – | Reconcile drops rows without an S3 upload id after 1 h, aborts multipart uploads without a row after 1 h, and finalizes rows stuck in `finishing`. |
| The upload is abandoned | – | After `incomplete_ttl_hours` (7 days) idle, the server aborts it in the bucket, which frees the parts, and drops the row. The bucket's lifecycle rule is the safety net. |
| The bucket lost the upload (e.g. its own 7-day abort) | 404 on status or parts | The client starts a new upload. |
| The folder was deleted meanwhile | 404 `folder_gone` | As today the upload pauses. Deleting the folder also aborts its unfinished uploads. |
| Cancelled by the user | – | `DELETE` aborts the upload and removes the row. |
| Bucket misconfigured (CORS, keys, clock) | every PUT fails (status 0 or 403) | Retries, then the file fails. `share check` and the admin's storage warnings name the cause: `s3_cors`, `s3_denied` or `s3_clock_skew`. |

As with tus, there is no end-to-end checksum: TLS protects the bytes on the way, and every part's size
is enforced.

**Viewing and downloading:**
- The website keeps its URLs: `/api/files/{id}/content` with the cookie answers with a 302 to a presigned
  GET. It is valid for 12 h, forces an attachment with the real name, carries the stored mime, and is
  sent with `Cache-Control: private, no-store`.
- The app asks `GET /api/s3/files/{id}/url` and then fetches or streams that URL **without its key**.
- `/content` with an `Authorization` header is refused in S3 mode. media3 re-sends headers on redirects,
  so a redirect could otherwise carry the phone's key to the bucket.

**What the server still reads from the bucket:**
- up to 512 bytes, to sniff a file type the name doesn't tell;
- the originals of JPEG, PNG and GIF photos that came without a thumbnail (up to 128 MiB, as today).

**Not available in S3 mode:**
- ZIP: `GET /api/downloads/{id}` refuses, but `POST` stays because "save into a folder" uses its paths.
- tus.
- Free-space checks.
- File bytes over the home address. It still carries the API calls and the thumbnails.

## API (contract; all three parts build on this)

**Unchanged in disk mode:**
- tus at `/tus/`: requests, metadata, hooks and errors;
- `/api/files/{id}/content`, the ZIP downloads, and the thumbnail PUT and GET;
- every other endpoint.

The only disk-mode changes:
- `/api/info` gains `storage: "disk"` and reports `api_version: 3`.
- `preCreate`'s admission moves into `upload/admit.go` with the same rules and errors.
- The tus code is renamed (`TusHandler`, `TusUploader`) without any behaviour change.
- The app also sends `lastModified` in the tus metadata. The server already accepts it, and the website
  already sends it.

S3 mode only (there `/tus/` answers 404):

| Endpoint (S3 mode) | Request → answer | Errors |
|---|---|---|
| `GET /api/info` | `storage: "s3"`, `api_version: 3` | |
| `POST /api/s3/uploads` (pin, device) | `{name, size, last_modified_ms, folder}` → 201 `{id, part_size, parts, urls:[{number,url,size}], expires_at}`; `urls` covers the first ≤10 parts; `parts` is 0 for an empty file | as tus: unauthorized/session_ended, bad_request, too_large, too_many_uploads, no_folder, folder_gone; 503 `s3_unavailable` |
| `POST /api/s3/uploads/{id}/parts` | `{parts:[n…]}` (1–100) → `{urls:[…], expires_at}`; also keeps the idle TTL fresh (`SetReceived`) | 404 not_found (unknown or not the owner's, as tus), 409 `s3_upload_finished` |
| `GET /api/s3/uploads/{id}` | → `{id, state: receiving\|finishing\|complete, size, part_size, parts, done_parts}`; a finished upload lists every part as done (tombstone) | 404: start over |
| `POST /api/s3/uploads/{id}/complete` | `{}` → `{id}`; idempotent | 409 `s3_parts_missing` |
| `DELETE /api/s3/uploads/{id}` | → 204: abort + delete the row | 403 forbidden once finished |
| `GET /api/s3/files/{id}/url` | → `{url, expires_at}`; same rights as `/content`, so PINs that show their folder too | |
| `GET /api/files/{id}/content` | cookie → 302 | Bearer → 409 `s3_use_url` |
| `GET /api/downloads/{id}` | | 409 `s3_no_zip` |
| `GET /api/admin/storage` | adds `storage` (both modes), `s3_bucket`, `s3_endpoint`; disk fields empty or 0 | |

New storage findings, all problems: `s3_unreachable`, `s3_denied`, `s3_cors`, `s3_clock_skew`.

Contract changes:
- Changed: `contract/api/info.json`, `storage.json`, `errors.json`, `contract/storage_warnings.json`, and
  the notes in `file.json`, `downloads.json` and `tus.json`.
- New: `s3_upload_create.json`, `s3_upload_parts.json`, `s3_upload_status.json`, `s3_upload_complete.json`
  and `s3_file_url.json`.
- `contract/app/platform.json` gets `play_s3`: an AWS-style URL with `%2F %3B %20` and so on, and only a
  User-Agent header.

## Server (`server/`)

1. **Config** (`internal/config/config.go`, plus new `config/s3.go`):
   - New `S3 *S3` with `endpoint, region, bucket, prefix, access_key_id, secret_access_key, path_style`.
   - Exactly one of `storage_dir` and `s3` (see 336).
   - `data_dir` is required with `s3`; today it defaults to `<storage_dir>/.share` (343-349).
   - Endpoint: https, or http only for a home address (reuse `parseOrigin` / homenet).
   - Region is required; it keeps presigning local, with no GetBucketLocation call.
   - Bucket name is checked, and `prefix` is normalised to end in `/`.
   - `upload.chunk_size_mib` is at least 5; the largest file is capped at 5 TiB.
   - Never log the `s3` object. Add tests to `config_test.go`.
2. **New package `internal/s3`**, wrapping `minio.Core`. `s3.Open` makes no network call and uses:
   - static V4 credentials;
   - `BucketLookupPath` when `path_style` is set, else `Auto`;
   - transport timeouts and 3 retries.
   `s3.Bucket` provides:
   - `Key(id)`, and `Origin()` for the CSP: the scheme and host of a presigned GET.
   - `CreateUpload`, with **no checksum algorithm**: browsers can't send `x-amz-checksum-*` headers.
   - `PartURL`: `PresignHeader(PUT, {partNumber, uploadId}, {"Content-Length": size})`. Verified:
     minio-go signs every extra header.
   - `Parts`: paginated, and `ErrNoUpload` when the upload is gone.
   - `Complete`, and `Abort`, which treats NoSuchUpload as done.
   - `Uploads`: ListMultipartUploads under the prefix.
   - `PutEmpty`, `Stat`, and `Open`. `Open` returns `*minio.Object`, which streams and does a ranged GET
     on Seek.
   - `Head(n)`, `Remove`.
   - `GetURL`: `PresignedGetObject` with `response-content-disposition`, from the existing
     `contentDisposition()` at `api/library.go:347`, and `response-content-type`.
   - `Reach` / `WaitReachable`, which tell denied apart from clock skew and network errors.
   - `CORS(origin)`: OPTIONS preflights for PUT and for GET with Range.
   - `CORSRules(origins)`: the JSON for `share check` to print.

   `internal/s3/s3test` holds the fake S3 for tests (see Verification).
3. **DB** (`internal/db`):
   - `migrations/0007_s3.sql`:
     - `files.s3_upload_id`;
     - `files.s3_part_size`;
     - table `s3_garbage(key, created_at)`, so a purge that hits a network outage is retried.
   - `files.go`: the struct, `fileColumns`, `scanFile` and `InsertReceiving` get the new fields. New
     functions: `SetS3UploadID`, `PurgeToS3Garbage` (in one transaction), `S3Garbage`, `ForgetS3Garbage`,
     `HasFiles`.
   - A meta key `storage` (`disk` or `s3:<bucket>/<prefix>`) for the mode lock.
4. **Storage, S3 mode** (`internal/storage`): a nil-able `Library.s3 *s3.Bucket`, not an interface. The
   database steps stay shared; only the byte moves differ.
   - **Constructor:** `NewS3Library(d, b, loc, now, logf)`, without an `os.Root`.
   - **New `storage/s3.go`** holds:
     - part planning: `S3PartSize` = max(chunk, ceil(size/10000) rounded up to a MiB), `S3PartCount`,
       `S3PartLen`;
     - `finalizeS3`: per-id striped locks, holding `lib.mu` only around `claimPath`. Empty files use
       `PutEmpty`. When Complete answers NoSuchUpload, a `Stat` with the right size counts as done.
       InvalidPart or InvalidPartOrder (a part re-sent meanwhile) becomes `ErrIncomplete`, which the
       API answers with 409 `s3_parts_missing`;
     - classify by name first (split a `byName` out of `Classify` in `names.go`), and otherwise read
       `Head(512)`, skipping that for size 0;
     - `terminateS3`;
     - `reconcileS3`:
       - Finalize the finalizing rows;
       - drop receiving rows that have no S3 upload id and are older than 1 h;
       - after the TTL, Finalize or Terminate;
       - abort multipart uploads that have no row and are older than 1 h;
       - empty `s3_garbage`.
   - **One-line guards in the disk code:**
     - `exists` returns false without a root. This covers `freePath`, `freeDir` and `locate`.
     - `restorePath` (`trash.go:106`) uses `lib.exists`.
     - These are skipped: `relocateFolder`, the `MkdirAll`/rename loops in CreateFolder and MoveFiles,
       `moveToTrash` and `moveFromTrash`, and `removeEmptyDirs`.
     - `Purge` calls `PurgeToS3Garbage`, then removes the object.
     - `Close` handles a nil root.
     - `EnsureFirstFolder` (`folders.go:28`) handles an empty `storageDir`.
   - **`Open`** returns `storage.File` (`Reader + ReaderAt + Seeker + Closer`). A disk-only `OpenFile`
     (`*os.File`) stays for `/content`'s sendfile and for checksum. Avoid the typed-nil trap.
   - **`storage.go`**:
     - `CheckS3(ctx, dataDir, b, origins)` uses the four new codes as literals, because
       `TestFindingsMatchContract` scans `storage.go`. It shares the data-dir checks.
     - `ClaimStorage` refuses to start when the mode, bucket or prefix changed, or when switching to S3
       on a database that already has files.
5. **Uploads** (`internal/upload`):
   - First, a pure rename: `upload.go` → `tus.go`, `Handler`/`New` → `TusHandler`/`NewTusHandler`, the
     tests to match.
   - Then move the admission of `preCreate` (`upload.go:294-337`) into `admit.go`, returning `*Refusal`.
     tus maps a Refusal to `tusError`, and the free-space check runs only when `free` is set.
   - New `upload/s3.go` (`S3Handler`, `NewS3Handler(...).Register(mux)`) with the five `/api/s3/uploads`
     endpoints:
     - ownership through `p.Owns`, with 404 for others;
     - complete runs Finalize under `context.WithoutCancel` with a 2 min timeout;
     - a receiving upload the provider lost is Terminated and answers 404, so the client starts over;
     - when CreateUpload fails, delete the row and answer 503 `s3_unavailable`.
6. **API** (`internal/api`):
   - `Version` 3 and `Info.Storage` (`api.go:26, 94-100`).
   - `content` (`library.go:285-316`): 409 `s3_use_url` when an `Authorization` header is sent, else a
     302 to `GetURL` with no-store.
   - New `api/s3.go`: the `/api/s3/files/{id}/url` handler, registered in S3 mode only, using `readyFile`,
     plus the redirect helper.
   - `download` (`downloads.go:99`): 409 `s3_no_zip`.
   - Admin storage (`admin.go:671-719`): `storage`, plus `s3_bucket` and `s3_endpoint`, and no
     `storage.Stat` in S3 mode.
   - New field `API.S3`.
7. **CSP** (`internal/webui/webui.go:38-39, 203-212`): the constant becomes a field built in
   `New(cfg, s3Origin)`, with the bucket origin added to `img-src`, `media-src` and `connect-src`.
   `setPageHeaders` becomes a method.
8. **Wiring** (`internal/app/app.go:60-200`). S3 mode:
   - skips the marker; `WaitReachable` replaces it when `WaitForStorage` is set;
   - runs `ClaimStorage` and `NewS3Library`;
   - registers `S3Handler` instead of `TusHandler` and answers JSON 404 at `/tus/`;
   - has no checksum store or `CRCs.Run`, and `OnReady` stays nil;
   - gives thumbs `Open` through a closure, with no `Root`;
   - gains an `Options.S3` hook so tests can use the fake.

   **CLI** (`cmd/share/main.go:151-199`, `accounts.go:23-32`, new `cmd/share/s3.go`):
   - `init` makes only `data_dir`;
   - `check` prints the bucket line, and the CORS JSON when `s3_cors` is found;
   - `openDB` handles S3 mode;
   - the help text mentions S3.
9. **Dependency**: `go get github.com/minio/minio-go/v7`. Compare the arm64 binary size before and after,
   for the router, and run govulncheck in CI as now.

## Website (`web/`, no new packages, hand-formatted)

1. **`src/api.ts`**:
   - `Info.storage: 'disk' | 's3'`, and the `storage`/`s3_bucket`/`s3_endpoint` storage fields;
   - the types `S3NewUpload`, `S3PartUrls` and `S3UploadStatus`;
   - a `contentPath(id)` that replaces the hardcoded copies at `account/library/actions.ts:22` and
     `account/save/folder.ts:164`.
2. **New `src/s3parts.ts`**, pure functions:
   - part spans and missing or done bytes;
   - `stale()`: 45 min, measured with `performance.now()`;
   - `s3FailureAction(source, status, code)`, which returns retry, refresh, restart, resync or refuse;
   - `Slots(limit)`.
   Note that status 0 from the bucket means refresh: R2 answers an expired URL with a 403 that has no
   CORS headers.
3. **New `src/s3upload.ts`**: class `S3Upload`, an Uppy `BasePlugin` of type uploader, built like
   `@uppy/tus` 6.0.0 (`addUploader`, `resumableUploads`, EventManager).
   - **State:** the file state `s3: {id, partSize, parts}` is persisted by Golden Retriever; the URLs are
     kept only in memory.
   - **Scheduling:** at most 3 files at once, and one file's parts go one after another.
   - **Sending a part:** an XHR PUT of `data.slice()` with no type and no headers, reporting
     `upload-progress`. A PUT with no progress for 60 s is aborted.
   - **Resuming** goes through `GET /api/s3/uploads/{id}`.
   - **Pause, resume, cancel and remove** follow the tus plugin, and cancel uses the exported
     `abortS3Upload(id)` (DELETE with keepalive).
   - **Finishing:** emit `upload-success` with `uploadURL: '/api/files/' + id`, so `uploader.ts:391`
     stays as it is.
   - **Transport:** a seam `{api, put, sleep, online}` for tests. Don't use `request()`: its 401 handler
     would leave the page.
4. **`src/uploader.ts:136-157`**:
   - `info.storage === 's3'` uses `S3Upload`, otherwise Tus.
   - The rules at 143-156 move into `refused(status, code)`, shared by both.
   - `terminate` (367-374) calls `abortS3Upload` for files that have `s3` state.
5. **Downloads**:
   - `actions.ts` gets `downloadOneByOne(ids…)`: hidden `<a download>` links to `contentPath`, one per
     second, cancellable, at most 100. On iPhone and iPad, a "Next" toast asks for one tap per file.
   - `Library.tsx:327-367`, `guest/See.tsx:81-114` and `save/SaveChoice.tsx`, in S3 mode:
     - the choice becomes "save into a folder" or "one by one";
     - a progress toast;
     - "Download again" re-runs the same ids.
   - Save engine:
     - `folder.ts` reports `remote` when `res.url` is on another origin.
     - `engine.ts:175-178` treats 401/403 as signed out only when the answer isn't remote. A bucket 403
       is retried up to 3 times, then that file fails.
   - The viewer and `sw.ts` don't change.
6. **Settings and admin screens**:
   - `account/settings/Settings.tsx:242-243, 296, 339`: show the bucket and the endpoint host instead of
     the folder and free space.
   - `account/admin/Folders.tsx:281`: hide `folders.renameHelp` in S3 mode.
   - New keys in `account/i18n/{en,de,it}.json`:
     - `storage.warn.{s3_unreachable,s3_denied,s3_cors,s3_clock_skew}`, `storage.used`;
     - `save.each`, `save.eachDetail`, `save.cantPickEach`;
     - `each.starting`, `each.started`, `each.tooMany`, `each.next`.
     `test/admin.test.ts` enforces the warning keys.
7. **Tests**:
   - `test/s3upload.test.ts`:
     - the `s3parts` functions;
     - `S3Upload` with a FakeApi and FakeBucket, as in `save.test.ts`: happy path, resume, 403 refresh,
       409 resync, 404 restart, 401 refuse and pause, the folder-gone flag, limit 3, cancel and
       DELETE, ghosts;
   - `test/save.test.ts`: a remote 403 against a server 403;
   - `test/download.test.ts`: `downloadOneByOne`.

## App (`app/`, no new packages, no dart format)

1. **Renames first, with no behaviour change:**
   - `transfer/Uploader.kt` → `transfer/TusUploader.kt` (`TusUploader`), and `UploaderTest` →
     `TusUploaderTest`.
   - `Outcome`, `outcomeOf`, `copy` and `SourceError` move to `transfer/UploadOutcome.kt`, shared by both
     uploaders.
2. **Kotlin network pieces**:
   - `net/HomeHosts.kt`: `isHomeHost`, matching IP literals only, tested on `contract/home_hosts.json`.
   - `net/S3Connection.kt`: `S3Connection.open(url, method)`, the counterpart of `ServerConnection`,
     with **no token parameter**:
     - https, or http only for a home host;
     - system trust, no Authorization, redirects and caches off;
     - the query is stripped from errors and logs.
   - `net/ServerInfo.kt`: `Storage {DISK, S3}` and the chunk size from `/api/info`, cached per server.
     It replaces `UploadEngine.chunkSize()` (173-190).
3. **Uploads**:
   - New `transfer/S3Uploader.kt`, returning `UploadOutcome`:
     - create, or resume from the status answer;
     - batches of URLs, refetched after 45 min or a bucket 403;
     - `setFixedLengthStreamingMode(part.size)` and no extra headers;
     - complete, with a 409 going back to the status, at most twice.
     - Bucket answers have their own mapping: 403 refreshes the URLs, NoSuchUpload resyncs, 5xx/429
       retries. They never go through `outcomeOf`, where a 401 would sign out.
     - JSON requests are buffered, not streamed, so error bodies stay readable.
   - `UploadEngine.kt:100-118` picks `TusUploader` or `S3Uploader` by storage. `progress.local` is true
     only for disk mode. It now passes `lastModified` from the new `Outbox.lastModified()`.
   - `Uploads.kt:65-78`: cancel sends `DELETE /api/s3/uploads/{id}` in S3 mode.
   - `TusUploader` sends `lastModified` in the tus metadata.
   - `TransferDb` needs no migration: `upload_id` is the file id, and the server knows the parts.
4. **Downloads**:
   - New `transfer/S3Links.kt`: `S3Links.fetch(id, open)` asks for `/api/s3/files/{id}/url` over the
     route. It answers a link, SignedOut, Gone, Retry or Failed, and is shared with Playback.
   - `Downloader.kt` then does a key-less `S3Connection` GET with `Range` and no If-Range:
     - a 403 retries with a fresh link and keeps the partial file;
     - a 404 asks the server whether the file is gone;
     - a 401 from the bucket is never SignedOut.
   - `DownloadEngine.kt:170-174` and `Fetcher.kt:96-97` pass `S3Connection::open` in S3 mode and set
     `local = false`.
5. **Playback** (`Playback.kt:52-64`): in S3 mode, after the copy check, `S3Links.fetch` over the route;
   the source is the URL with only a User-Agent header. `way()` stays disk-only.
   `ui/media_page.dart:80-83`: Retry fetches a new link and continues from the last position.
6. **Dart**:
   - `data/models.dart:96-108`: `ServerIdentity.storage` (`enum Storage {disk, s3}`).
   - `StorageInfo` (481-505) gets `storage`, `s3Bucket` and `s3Endpoint`.
   - `data/api.dart`: new `s3Bytes(link)` using `Uri.parse` as is (never `replace(queryParameters:)`),
     the public client, no Authorization, and never `onSignedOut`.
   - `data/library.dart:90-106`: `original()` goes through `/api/s3/files/{id}/url` when `_onS3()`.
   - `ui/settings_screen.dart:27-39, 209` shows the bucket, plus the four new warnings in
     `l10n/app_{en,de,it}.arb` (`storageWarnS3Unreachable` and so on); `test/storage_warnings_test.dart`
     enforces them.
   - `ui/admin/folders_screen.dart:402`: hide `folderRenameHelp` in S3 mode.
   - The "At home" labels follow `local=false` without other changes.
7. **Tests**:
   - Kotlin:
     - `S3UploaderTest`, where `TestServer` plays both the API and the bucket: the reassembled bytes,
       the query arriving unchanged, no Authorization at the bucket, Content-Length, resume, 403, cut
       connections, 409, empty files, and refusing http to a non-home host;
     - `S3ConnectionTest`, `S3LinksTest`;
     - `TusUploaderTest` (renamed), `DownloaderTest`, `PlaybackTest`;
     - `PlatformContractTest:114-124` against `play_s3`;
     - `ServerInfoTest`, `HomeHostsTest`.
   - Dart:
     - `platform_test:58-66`, `player_test:26-27`;
     - `models_test`;
     - a new `test/s3_library_test.dart` for disk, S3 and PIN-in-S3, with the `/api/s3` routes and the
       bucket host added to `support/fake_server.dart`;
     - `api_test` for `s3Bytes`;
     - a Uri round-trip of AWS, R2, B2 and MinIO links.

## Docs

- `README.md`:
  - "How it works", "Security" and "Status";
  - "Configuration": disk or S3, and the `s3` object with one example each for R2, B2, AWS and MinIO.
  - Bucket setup:
    - CORS: origins `public_url`, the `home_url` origin and `http://localhost:5173`; methods GET and
      PUT; headers `*`; expose ETag, Content-Length and Content-Range;
    - a lifecycle rule that aborts incomplete multipart uploads after 7 days;
    - a key limited to the bucket.
  - The limits in S3 mode: no ZIP; bytes skip the home address; a presigned link is a bearer token for
    one file for 12 h, even after trash.
- `docs/share-plan.md`: Parts, Library (70-73) and Network (90-91).
- `config.example.json` stays disk-based.

## Order of work (on branch `feature/s3_support`, plain-sentence commits, not pushed)

1. Name the tus code after its protocol: `upload/tus.go` with `TusHandler`, and `TusUploader.kt` with
   `UploadOutcome.kt`. A pure rename; every test still passes.
2. Server: config, the `s3` package and fake, migration, storage S3 mode, uploads API, API, CSP, CLI,
   contract, tests. Disk mode keeps passing all of today's tests at every step.
3. Website.
4. App.
5. README and `docs/share-plan.md`.

## Verification

- Server: `cd server && go vet ./... && GOOS=windows go vet ./... && go test -race ./...`.
  - `s3test` is a TLS httptest server with maps behind a mutex. It mimics what minio-go expects:
    - path-style at 127.0.0.1;
    - XML errors;
    - ListParts and ListMultipartUploads pagination;
    - Range through `ServeContent`;
    - the `response-*` parameters;
    - a 5 MiB minimum part;
    - signature checks on presigned PUTs, including content-length;
    - failing the test if `x-amz-checksum-algorithm` is set.
  - `newS3Env` in `app/s3_test.go`, mirroring `newEnvWith` (`app_test.go:55-92`), and an `e.s3Object`
    helper replacing `e.disk` cover:
    - upload, then 302 or 409 on `/content`;
    - an idempotent complete, and one with missing parts;
    - ownership and ended PINs;
    - the sweeps;
    - trash, restore, move and rename with zero bucket calls;
    - purge retried through `s3_garbage`;
    - an empty file;
    - the server-made thumbnail;
    - the CSP;
    - the mode lock;
    - the contract shapes.
  - Opt-in: `SHARE_TEST_S3='{…config.S3…}'` runs the flow against MinIO in Docker and a real R2 or B2
    bucket, on a random prefix. This is where minio-go details and provider support for
    `response-content-disposition` and the signed Content-Length get confirmed.
- Website: `cd web && npm run typecheck && npm test && npm run build`.
- App: `cd app && flutter analyze && flutter test`, and
  `cd app/android && ./gradlew testDirectDebugUnitTest testPlayDebugUnitTest lintDirectDebug`.
- By hand, with an R2 or MinIO bucket and `share check` passing:
  - The website on the phone through the tunnel:
    - a multi-GB video;
    - close the tab halfway and continue;
    - a PIN upload;
    - view a photo and seek in a video;
    - download one file and check its name;
    - "Download N" one by one, and save into a folder in Chrome on the PC.
  - The app: send, save to the gallery, play a video, a full-size photo.
  - Move, rename, delete and restore change nothing in the bucket; a purge removes the object.
  - The server's traffic shows no file bytes except thumbnails.
  - A disk-mode server still sends, downloads and zips exactly as before.

## Pitfalls

- **Clock:** a router without a real-time clock signs invalid URLs until NTP syncs. That's the reason
  for `s3_clock_skew` and `WaitReachable`.
- **Server-side calls:** never set a checksum algorithm on CreateMultipartUpload. The region must be set.
- **R2:** every part except the last must be the same size; the server's `part_size` gives that.
- **Links to the bucket:** Dart must not re-encode a presigned query. Hosts are signed without their
  default port.
- **MinIO at home:**
  - an https site can't use it over http;
  - Chrome asks for Local Network Access;
  - certificates installed by the user aren't trusted by the app.
- **Long Complete calls:** more than 100 s through Cloudflare gives a 524. That's harmless, because
  complete is idempotent and the clients retry through the status call.
