# Sending photos as a ZIP — plan

WhatsApp makes photos smaller and videos much smaller, unless they are sent as a document. A ZIP
is a document, so photos and videos packed into one arrive exactly as they were taken. Share makes
such ZIPs on the phone, from the gallery's share sheet, the library or the Send tab, and Share on
the other phone saves what's inside into its gallery. Packing and opening need no server, no
account and no internet. Planned on 2026-10-09 from the user's drawings, and built the same day
(commits 2f12638, 77a792b and 0b1ad9d); the screens are 97 to 110 of
[share-mockup.html](share-mockup.html). What only a phone can show is under On the Pixel, below.

Asked by the user:
- **Only in the Android app, for now.** The server and the website don't change.
- **Offline, without the server.** Packing and opening happen on the phones. The only step that
  uses the server is taking library files that aren't on the phone yet (101).
- **WhatsApp takes at most 2 GB**, so bigger sets are split, and **the size is chosen when
  sending** (98, 103).
- **Share on the other phone opens the parts together** and puts files that were cut back
  together (108 to 110).
- **A name for the ZIP**, which Share proposes (98).
- **Each ZIP holds a `share.json`** with what's in it.
- **As easy as possible.**

Decided with the user on 2026-10-09, after the drawings:
- **Into the Share folders, as downloads go now.** What a ZIP brings, whole or put back together,
  goes into the album "Share" (`Pictures/Share`) and documents into `Download/Share`. No album
  for each ZIP.
- **The five choices** of where a ZIP goes, below, are right.
- **Packed ZIPs delete themselves** a day after packing.
- **The screens as drawn**, so a set's later parts are saved by themselves once one is.

Also part of the drawings:
- **Each ZIP opens on its own.** Files stay whole; only a file bigger than a whole ZIP is cut
  into pieces, and only its pieces need Share, or 7-Zip on a computer, to be joined. No split
  archive (`.z01`, `.z02`, `.zip`), which nothing opens part by part.
- **Where it goes, not a size.** The sheet names apps, says how many ZIPs each one makes of these
  files, and remembers the choice.
- **Later parts follow the first.** Once a part of a set is saved, the set's other parts are saved
  the same way as soon as they're opened.
- **Nothing piles up.** Packed ZIPs go a day after packing, and pieces waiting for their other
  part go after two weeks, both by themselves.

## Sending

### Where it starts (97, 101, 102)

- **Android's share sheet**: a second entry, Send as ZIP, next to Send with Share. It is an
  `<activity-alias>` of `MainActivity` with its own label and icon (Share's, with an archive
  mark); the intent's component tells the two apart.
  - Its filter names what it packs: `image/*`, `video/*`, `audio/*`, `text/*`, `application/pdf`
    and the office formats. So a share of only ZIPs doesn't offer it, while a mix, which arrives
    as `*/*`, still matches.
  - The files are read straight from the URIs the sharing app lends, while the screen is open,
    without a copy into the outbox first. If Android ends Share before packing, the person shares
    again.
  - It works signed out too: the ZIP screen opens over the first screen.
- **The library**: Share in the selection and in the viewer opens a sheet with As a ZIP, in full
  quality, or One by one, which is today's sharing. Files already on the phone are taken from
  there (`Downloads.savedUri`). The others are fetched from the server first, one at a time, as
  sharing does today (`Fetcher`), decrypted for an encrypted folder.
- **The Send tab**: a card, Send as ZIP, picks photos and videos with Android's photo picker, which
  needs no permission, and opens the same screen. Signed out, the first screen (7) offers it as a
  third way, with no server needed.

### The name (98)

The proposal is a word for what the files are, and when they were taken:
- **The word**: Photos (photos, or photos and videos), Videos, Documents, or Files for a mix with
  documents. Files from one folder of the library get the folder's name instead: Dolomites
  15–18 Oct 2026.
- **When**: the days the files were taken. That is MediaStore's `DATE_TAKEN`, else the photo's
  EXIF date, else when the file last changed; for library files, the day they arrived.
  - one day: 4 Oct 2026;
  - days in one month: 15–18 Oct 2026;
  - across months: 28 Sep – 3 Oct 2026;
  - across years: 28 Dec 2025 – 3 Jan 2026;
  - more than two months: only the months, Jul – Sep 2026.

  Dates are written in the phone's language: Fotos 4. Okt. 2026, Foto 4 ott 2026.
- **Editing**: the field can be changed; `.zip` stays outside it.
  - Characters that file systems refuse (`/ \ : * ? " < > |` and control characters) become
    `_`, and leading and trailing dots and spaces go.
  - The name is cut at 80 characters, and an empty one goes back to the proposal.
- The name becomes:
  - the ZIP's file name, `<name>.zip`, or for several `<name> (1 of 4).zip` in the phone's
    language;
  - the name in `share.json`, which Share on the other phone shows over the files.

### Where it goes (98, 103)

| Choice | Each ZIP at most | Why |
|---|---|---|
| WhatsApp, Telegram | 1,950,000,000 bytes | under WhatsApp's 2 GB, however it counts them; Telegram takes 2 GB too |
| Signal | 95,000,000 bytes | Signal takes 100 MB |
| Email | 14,000,000 bytes | an attachment grows by a third in the mail; this stays under 20 MB |
| Quick Share, a computer | no limit | one ZIP, with Zip64 above 4 GB |
| Somewhere else | the MB typed, 1 to 4,000 | 1 MB is 1,000,000 bytes |

- WhatsApp comes first, and the phone remembers the last choice (`writeSecret('zip_where')`).
- Each row says how many ZIPs it makes of these files. A choice that would make more than 100
  can't be picked, and says Too many.
- Under the choice, the screen says what it makes: all in one ZIP, or 4 ZIPs and a video in
  2 pieces.

### The parts

`ZipPlan` lays out every part before a byte is read, as the server's `zipstream` does for
downloads: a ZIP of stored files is exactly as big as its names, sizes and headers. So the counts
in the sheet, the parts' sizes and their names are exact.
- Files go in the order they were taken. Each goes into the first part with room for it, so a
  later photo fills the gap a video left.
- Only a file bigger than a whole part is cut. Its first piece fills what's left of the last part,
  if that's at least a tenth of the limit, and the rest go into new parts, each as full as allowed.
- Every part keeps room for its `share.json`, its central directory and its end record, counted
  exactly.

### Packing (99)

- `ZipSending` writes the parts one after the other into Share's own storage, `files/zips/<id>/`,
  on a thread of its own while the screen is open. Packing is copying: seconds for a few hundred
  MB, about a minute for 7 GB.
- `ZipWriter` reads each file once. It writes the local header with zeros for the CRC and sizes,
  copies the data while computing the CRC-32, then writes both into the header. This needs no
  data descriptors, which Android's `ZipInputStream` can't read for stored files.
- **Room** is checked first: the set's size plus the 200 MB the outbox keeps free
  (`Outbox.SPARE`). Without it, the screen says how much is missing, with Free up space (Android's
  `ACTION_MANAGE_STORAGE`).
- **A file that can't be read**: a lent file that can't be read when it arrives is left out, and
  the screen says how many. One that changes while packing stops it, and the screen names it.
- **Progress** goes out as events: file i of n, bytes, part j of k.
- **Stopping**: Stop packing deletes `files/zips/<id>`. If Android ends Share during packing, the
  half-made folder goes a day later, as packed ones do.
- **Library files** that aren't on the phone are fetched into the cache one at a time, packed and
  deleted at once, so they need room for one file at a time.

### Sending and keeping (100, 104)

- **Send ZIP** opens Android's share sheet (`Intent.createChooser`):
  - `ACTION_SEND` or `ACTION_SEND_MULTIPLE`, type `application/zip`;
  - the parts as `content://` URIs from the existing FileProvider, which gets
    `<files-path name="zips" path="zips/" />`, with read permission granted;
  - Share itself left out (`EXTRA_EXCLUDE_COMPONENTS`), so a ZIP isn't sent to the server or
    packed again by mistake.
- **A part's arrow** sends just that part, for apps that take one at a time, as email does. The
  chooser's callback (`EXTRA_CHOSEN_COMPONENT_INTENT_SENDER`, `ZipChosenReceiver`) tells which app
  took it, so the row can say "Sent with WhatsApp".
- **Save to Downloads** copies the ZIPs into `Download/Share/` through MediaStore.
- **Cleanup**: other apps read a ZIP when they like (WhatsApp while it uploads), so it can't go
  at once. Folders in `files/zips/` older than a day are deleted when Share starts and before the
  next packing, as `Fetcher` trims its cache.

## Inside each ZIP

### Layout

- **Entries**: `share.json` first, then the files in the plan's order, with their own names and no
  folders.
  - Two files with the same name get " (2)" before the extension, and a file of the set called
    `share.json` becomes `share (2).json`.
  - The pieces of a cut file are called `<name>.001`, `.002` and so on. 7-Zip joins those by
    itself, and so do `copy /b` on Windows and `cat` elsewhere.
- **Stored, not compressed**: photos and videos don't get smaller in a ZIP, and they stay exactly
  as they were.
- **The format follows the server's
  [`zipstream`](../server/internal/zipstream/zipstream.go)**:
  - CRC-32 and sizes in each local header, and no data descriptor;
  - the UTF-8 flag on names;
  - Info-ZIP's extended timestamp next to the MS-DOS time;
  - Zip64 fields only for what doesn't fit in 32 bits, which happens only with Quick Share, a
    computer.

  Each entry's time is when its file was taken, or last changed; for library files, when the file
  arrived.

### share.json

`share.json` is what Share on the other phone reads to know the set, the part and the pieces.
Here it is for part 3 of Stefan's Dolomites (104):

```json
{
  "share_zip": 1,
  "about": "Made with Share. Open every part with Share on an Android phone to save everything and put cut files back together. On a computer, 7-Zip joins the .001 and .002 files.",
  "set": "1c6f3a62-5d0e-4c9b-9f7e-2b8f0f4d6a11",
  "name": "Dolomites 15–18 Oct 2026",
  "made": "2026-10-19T20:07:12+02:00",
  "app": "0.2.0",
  "part": 3,
  "parts": 4,
  "set_files": 52,
  "set_bytes": 7012345678,
  "files": [
    {"entry": "IMG_20261017_101502.jpg", "type": "image/jpeg", "size": 4123456, "taken": "2026-10-17T10:15:02+02:00"},
    {"entry": "VID_20261017_141502.mp4.001", "type": "video/mp4", "size": 1105000000, "taken": "2026-10-17T14:15:02+02:00", "piece": {"file": "VID_20261017_141502.mp4", "number": 1, "pieces": 2, "offset": 0, "total": 2412345678, "parts": [3, 4]}}
  ]
}
```

- `share_zip` is the format's version, 1. A ZIP of a later version opens as an ordinary ZIP, with a
  line saying that a newer Share made it.
- `set` is new for each packing (a random UUID) and the same in all its parts; `part` counts from 1.
- `files` lists every other entry of this ZIP, in its order:
  - `type` decides between the gallery and Downloads;
  - `taken` orders the grid on the other phone.
- `piece` says which file an entry belongs to and where its bytes go: at `offset` in a file of
  `total` bytes. `parts` names the parts its pieces are in, so a part can say where the file
  continues. Each piece's CRC-32 is in the ZIP itself.
- `about` is in the sender's language, for someone who unpacks the ZIP without Share.
- It says nothing about people, the server or the phone, only what the files are. It is written
  the same way every time, with two-space indents and one line for each file, so it reads well in
  an editor, and its exact size is part of the plan.
- **Reading is strict.** Each of these makes the ZIP an ordinary one:
  - JSON that doesn't parse, or a missing field;
  - a negative or overlapping offset;
  - an entry that isn't in the ZIP, or a size that doesn't match.

  Unknown fields are ignored, so later versions can add some.
- `contract/app/share_zip.json` holds this example and broken variants of it, which the Kotlin
  tests read.

## Receiving

### Opening (105, 108)

- **Open with**: an intent filter for `ACTION_VIEW` with `application/zip`,
  `application/x-zip-compressed` and `application/x-zip`. WhatsApp opens documents that way, and
  so does Files.
- **Several at once**: a third entry in the share sheet, Open in Share. It is an `<activity-alias>`
  with `ACTION_SEND` and `ACTION_SEND_MULTIPLE` for the same two types. From the Files app, or from
  WhatsApp if it lets several documents be shared, all parts arrive together and open as one
  set (108).
- **Signed in or out**, the ZIP screen opens over whatever is there, as an invite does. Closing
  it goes back to the app the ZIP came from, so the next part is a tap away.
- **Reading**: `ZipReader` opens the URI with `openFileDescriptor("r")`, reads the end record (and
  Zip64's), then the central directory: names, sizes, CRCs and offsets.
  - Names are UTF-8, or the old IBM PC code page when the UTF-8 flag isn't set.
  - Stored and deflated files open. Encrypted ones, or other compression methods, show as files
    that can't be opened.
  - Where the descriptor can't seek (a pipe), the ZIP is copied into the cache first.
- **What's inside** shows before anything is saved (106), as a grid like the library's, with counts,
  size, and where the files will go. The thumbnails are made from the ZIP itself:
  - photos through `BitmapFactory` with `inSampleSize`, on the entry's bytes;
  - videos stored without compression through `MediaMetadataRetriever.setDataSource(fd, offset,
    length)`.
- **The URI only lasts while the screen does**, so everything happens while it is open. A ZIP closed
  before saving is opened again from the chat.

### Saving (106, 107)

- **Save to this phone** puts photos and videos into the album "Share" (`Pictures/Share`) and
  documents into `Download/Share`, as downloads do: through `MediaStoreSink`, pending until
  complete, with each file's CRC-32 checked while it's copied. A name that's there already gets
  MediaStore's own " (1)".
- **Room** is checked first. Without it, the screen says how much is missing, with Free up space.
- **Nothing twice**: what was saved is remembered by set and entry (for an ordinary ZIP, by its
  name, size and CRCs), so a part opened again says it's saved.
- **Send into <folder>**, signed in: the files are copied into the outbox, as files shared from
  another app are today when their URIs can't be kept (`Outbox`). `SharedSender` then sends them
  into the folder open in the library, or asks which one, as it does today. With a PIN or signed
  out, there is only Save.
- **Saved** (107) says where the files went. When the ZIP came from WhatsApp (by the URI's
  authority), it adds that WhatsApp still keeps it, and how much room deleting it frees. Open
  gallery opens the phone's gallery app (`CATEGORY_APP_GALLERY`).

### Parts and pieces (108 to 110)

- **What's kept**: `ZipInbox`, in Share's own storage, `files/zip-in/<set>/`:
  - `set.json`: the set's name and parts, the parts and entries saved, and where its files went;
  - for each cut file, its bytes so far (`<key>.part`) and which pieces are in (`<key>.json`).

  Plain files rather than tables, so the plain-Kotlin tests cover them.
- **Chips** show which parts this phone has saved.
- **Later parts follow**: once a part was saved or sent into a folder, the set's other parts do the
  same as soon as they're opened (109); into a folder only while sending still goes there. The
  screen shows the progress, then what was saved.
- **Pieces** are written at their offset into their file's `<key>.part`, and each is checked
  against its CRC-32 as it's written. When the last one is in and the file has its `total` length,
  the file goes where the set's files went, and its pieces go (110). Saving also delivers a whole
  file left from an earlier try.
- **A broken piece** is thrown away and named, so only its part has to be opened again ("Part 3 is
  damaged: open it again from the chat"). The other pieces stay.
- **Pieces waiting more than two weeks** go by themselves when Share starts. The set's screen then
  asks for those parts again.

### Any ZIP

A ZIP without `share.json`, from a computer or another app, opens the same way. Its files show,
and Save puts photos and videos into the album "Share" and everything else into `Download/Share`,
without the ZIP's folders. Pieces made by other tools aren't joined.

Safety, for any ZIP:
- A name from a ZIP never leads outside Share's places: names are cleaned as above, and `..` and
  leading slashes go.
- Sizes are checked against the room before anything is saved. A deflated entry stops at its
  declared size and must match its CRC-32.
- `share.json` counts only where it agrees with the ZIP itself. Pieces join only within one set,
  at offsets inside the declared total and without overlaps.

## App parts

### Android

- **Manifest**: the two aliases (Send as ZIP, Open in Share), the `VIEW` filter, and the
  FileProvider's new path.
- **Strings** in English, German and Italian: `send_as_zip` (Send as ZIP, Als ZIP senden, Invia
  come ZIP) and `open_in_share` (Open in Share, In Share öffnen, Apri in Share).
- **An icon** for each alias: Share's, with an archive mark.
- **No new permissions**: the share sheet and the picker lend their files, and the app writes only
  its own MediaStore rows.

### Kotlin

A new package, `zip`. Plain Kotlin, covered by JVM tests:
- `ZipLayout` and `ZipWriter`: a ZIP of stored files, its exact size, and writing it, each header's
  CRC-32 filled in after its data.
- `ZipPlan`: the parts, their files and pieces, and exact sizes and names (`Names`).
- `ShareJson`: writes and reads `share.json`.
- `ZipReader`: the central directory, the entries, Zip64, deflate, and a stream for each entry.
- `ZipInbox`: the sets and the pieces this phone keeps, and their cleanup.
- `ZipEvents`: the maps the Dart side reads.

On Android:
- `ZipSending`: the files waiting (lent URIs, saved copies, fetched files), the plans, packing, the
  room check, sending and cleanup; `ZipChosenReceiver` hears which app took a part.
- `ZipOpening`: the ZIPs open, their contents and thumbnails, saving, the outbox and pieces.
- `Outbox.keepMade` takes a file made on the phone into the outbox; `Fetcher.forget` drops a
  fetched copy once it's packed.

In `PlatformChannel`, `onIntent` tells the aliases and `VIEW` apart from today's sharing.

### Platform channel

In `contract/app/platform.json`, with a fixture for each answer and event (zip_*):
- An event `zip_in` when something arrives from another app; `zip.take` answers it once: files to
  pack, or ZIPs to open.
- `zip.pick` (the photo picker) and `zip.library {files, taken, auth}` answer the files to pack,
  each with name, size, type, when it was taken, and whether it's on the phone.
- `zip.thumb {index}`: a small JPEG of a file to pack.
- `zip.plan {name, about, limit}`: what a choice makes, the parts with their files, sizes and
  pieces, or too many.
- `zip.pack {name, about, part_name, limit}` and `zip.stop`; events `zip` with packing, ready,
  no_room, failed or stopped.
- `zip.send {part}` (all parts without one), with an event `zip_sent`; `zip.save_downloads`; and
  `zip.close`.
- `zip.contents`: the ZIPs open, their set, the files and the cut files with their pieces;
  `zip.open_thumb {index}`: a JPEG and a video's length.
- `zip.save {to: "phone" | "folder", folder}`, with events `zip_save`; `zip.close_opened`.
- `gallery.open`, and `storage.free` for Free up space.

### Dart

- `lib/data/zip.dart`: what the screens see, the choices (`ZipWhere`), and `zipName()`, the
  proposal, a plain function with its own tests.
- `lib/ui/zip/zip_screen.dart`: `ZipScreen`, with name, where it goes, packing, ready and the
  parts (98 to 100, 104), and `ZipWhereSheet` (103).
- `lib/ui/zip/zip_entry.dart`: Share's sheet in the library and the viewer (101), the card on the
  Send tab (102), and the third way on the first screen.
- `lib/ui/zip/zip_open_screen.dart`: `ZipOpenScreen` (106 to 110).
- `app.dart` opens the screens for what arrives from another app.
- Strings in `app_en.arb`, `app_de.arb` and `app_it.arb`; five more Lucide icons (archive, send,
  scissors, mail, message-circle) in the icon font.

## Tests

- **Kotlin** unit tests, in `app/android/app/src/test/kotlin/com/eschgi/share/zip`:
  - `ZipPlanTest`:
    - each limit;
    - files in order, with gaps filled;
    - only what can't fit in a part on its own is cut;
    - every part written and measured: exactly its planned size;
    - more than 100 parts refused;
    - names: " (2)", cleaning, a file named like a piece.
  - `ZipWriterTest`: `java.util.zip.ZipFile` and `ZipInputStream` read its ZIPs with the same
    bytes and CRCs; times; UTF-8 names; a file that changed; Zip64 with a sparse 4.3 GB file.
  - `ShareJsonTest`: written and read back, and every case in `contract/app/share_zip.json`.
  - `ZipReaderTest`, on ZIPs made in the test by Java's `ZipOutputStream`: deflated with data
    descriptors, stored, names in the old code page, a comment, an encrypted entry, cut off, and
    one that would inflate into more than it says.
  - `ZipInboxTest`: pieces in order, backwards and twice, a broken one, the sets, two weeks.
  - `PlatformContractTest`: the zip_* fixtures.
- **Dart**:
  - `test/zip_test.dart`: `zipName()` in three languages, cleaning, the remembered choice, and the
    screens with the fake platform, from the share sheet, the library and a chat;
  - `test/platform_test.dart`: the zip_* fixtures;
  - goldens of 98 to 110, without the other apps' screens (97, 105).
- **On the Pixel**, with a second phone:
  - Send as ZIP in the gallery's share sheet, and not in a share of ZIPs;
  - Open with from WhatsApp; Open in Share for several parts from Files, and from WhatsApp if it
    allows it;
  - WhatsApp: whether it takes 4 ZIPs at once, and the exact limit, with a ZIP of 1,950,000,000
    bytes and one of 2,000,000,000;
  - Telegram, and Gmail with 14 MB parts;
  - packing 7 GB: the time and the room;
  - opening on a phone that's signed out;
  - a 2.4 GB video put back together, from parts opened in any order;
  - a full phone, and the library's encrypted folder.

## Risks

- **WhatsApp's limits**: whether its 2 GB is 2,000,000,000 bytes or 2 GiB, and whether it takes
  several documents in one share. If it doesn't, Send 4 ZIPs offers the parts one by one. This is
  the first thing to try.
- **Open with**: WhatsApp must open a ZIP as `application/zip`. If some phones get another type,
  WhatsApp's own share button and Open in Share are the way in.
- **Room**: packing needs as much free room as the files, and WhatsApp keeps its own copy of what
  it sent and received. Saved (107) points that out on the other phone.
- **Without Share**: an iPhone's Files app unpacks the ZIPs, but nothing there joins a cut video.
  Videos over 1.95 GB are rare: a few minutes of 4K.
- **No phone here**: this machine has no emulator, so everything Android does (share sheets,
  WhatsApp, MediaStore) is first seen on the Pixel.

## Status

Built on 2026-10-09: the ZIP core with its tests, the Android side, the screens, the README and
this plan. All of it runs in tests here; what Android, WhatsApp and the gallery do is first seen on
the Pixel (the list under Tests), WhatsApp's limit and several documents at once first.

## Not in this step

- The website and the server: nothing changes there.
- ZIPs with a password: a ZIP is as readable as the photos in it, also those from an encrypted
  folder. For such files, the ZIP screen (98) adds a line saying that the ZIP isn't encrypted.
