# Sorting the library — plan

The library sorts by the day files arrived. Asked on 2026-10-11: let people choose to sort by
the date a photo or video was taken instead. The screens are 120 to 125 of
[share-mockup.html](share-mockup.html). Built on 2026-10-11 in the server, the website and the
app, as the mockups draw it.

Asked by the user:
- **Sort by date received**, as now, or **by date taken**.
- **Configurable.**
- Which other options make sense.

## The choices

- **Date taken:** the day a photo or video shows. Files without a date taken, such as documents,
  screenshots and files whose date was stripped, count on the day they arrived.
- **Date received:** as now, and the start.
- **Who sent it:** a group for each person, the one who sent last on top. Guests with a PIN have
  no name, so their files share one group.
- **Newest first or oldest first**, for each of the three. Oldest first reads an event from start
  to end.
- **Remembered on each phone and browser,** for every folder. The filters (All, Photos, Videos,
  Documents) and the search keep working on top.
- **The details show both dates** (123), whatever the sorting.

Left out: by name and by size. Neither helps with photos, and search finds names already.

## Where the date taken comes from

Decided with the user on 2026-10-11: **the app and the website send it** with each upload; the
server doesn't read files for it.

- **The app** (`TakenDate.kt`): what Android's gallery or the photo picker say (`DATE_TAKEN`),
  else a photo's EXIF (`DateTimeOriginal`, with `OffsetTimeOriginal` when there is one) or a
  video's date (`MediaMetadataRetriever`). The ZIPs use the same.
- **The website** (`web/src/taken.ts`): reads it from the file before sending: the EXIF of a
  JPEG, HEIC or AVIF, or the creation time in an MP4 or MOV header (`moov` may be at the end).
  EXIF without an offset counts as the browser's own time.
- **Sent** as Unix ms: `takenAt` in the tus metadata, `taken_at_ms` with a bucket
  (contract/api/tus.json, s3_upload_create.json). The server keeps `taken_at` and its day in its
  `time_zone` (`taken_day`), and drops a date from before 1900 or more than two days ahead.
- **Encrypted folders:** never sent, and the server drops one if it came; those files count on
  the day they arrived, and the server never learns when they were taken. (Chosen while the
  question was open; easy to change.)
- **Files sent before** have no date taken and count on the day they arrived.
- **Nothing moves on the drive:** files stay in the folder of the day they arrived.

## How it works

- `GET /api/library?sort=received|taken|sender&order=newest|oldest` answers `groups` instead of
  days: a day, or a sender's id (`"pin"` for PIN guests), with `last_day`, the day its newest file
  arrived, and a sender's name in `from`. `/api/files` and `/api/files/ids` take the same sort and
  `group`; the files come in the groups' order, a page at a time (contract/api/library.json,
  files.json).
- Files say `taken_at`, `taken_day` and `from_id`, so the website and the app group what they
  load the way the server groups it.
- **Remembered** in the app as the secret `library_sort`, on the website in `localStorage`, for
  every folder. While the library isn't by the day files arrived newest first, new files may
  belong anywhere, so a change on the server reloads the list instead of adding on top.
- PIN sessions that see a folder can't sort by sender: they see no names.
