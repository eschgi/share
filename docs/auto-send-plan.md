# Sending a day's photos by themselves — plan

For a day others should see as it happens, a wedding, a trip, a birthday: turned on in the
morning, every photo and video taken with the phone's camera goes into a chosen folder by itself,
and at midnight it stops by itself. Planned on 2026-10-07; the screens are 61 to 68 of
[share-mockup.html](share-mockup.html).

Decided with the user:
- **Today only.** It always ends at midnight on the phone; the next day it is one tap again. No
  "longer" for trips.
- **The sender can take a photo back** that went automatically, until midnight of the day it
  arrived, without an admin. It goes to Recently deleted, where admins can bring it back for
  30 days.
- **The others get no notifications.** They see who is sending into the folder, and who shared
  each photo.
- **Only the camera's folder.** The person gives Share that one folder once, in Android's folder
  picker. Share sees nothing else of the gallery, and needs none of the photo permissions that
  Google Play reviews (`READ_MEDIA_IMAGES`, `READ_MEDIA_VIDEO`).
- **Only the Android app** can send automatically: a browser never sees a phone's gallery.
  Everyone sees the results, in the app and on the website.
- **Big videos over mobile data only when allowed** (2026-10-08). Under Videos too, Also over
  mobile data is off until the person switches it on; until then big videos wait for Wi-Fi.

In an encrypted folder the photos go up encrypted, as always.

## On the sender's phone

### Turning it on (61, 62)

- The Send tab has a card, Send automatically, under picking by hand. Turn on for today opens a
  sheet:
  - **Into**: the folder open in the library, or another one the person can send into. An
    encrypted folder whose keys this phone doesn't have yet can't be chosen, as when sending by hand.
  - **From**: the camera's folder. The first time, it opens Android's folder picker at
    `DCIM/Camera` (`ACTION_OPEN_DOCUMENT_TREE` with `EXTRA_INITIAL_URI`), and the app keeps that
    access (`takePersistableUriPermission`). Change picks another one, for phones that save on an
    SD card or into another folder.
  - **Videos too**, on by default, and under it **Also over mobile data**, off by default: big
    videos wait for Wi-Fi unless it is on.
  - Start for today.
- Starting keeps on the phone the folder, the tree's address, when it started, until when (the
  next midnight on the phone), whether videos count and whether big ones may go over mobile
  data. It tells the server (`PUT /api/auto-send`), shows the notification (64), and arms what
  notices new photos.
- If Android took the folder's access away meanwhile, the card says so and asks for it again.

### Noticing new photos

What counts, checked in one place (`AutoScanner`):
- files directly in the chosen folder, not in folders inside it;
- photos and videos by their type, but no RAW files (`image/x-adobe-dng`), which are big and show
  nowhere;
- changed at or after the start, and before midnight;
- not hidden (names starting with `.`, as files the camera is still writing), and unchanged for
  5 seconds;
- not queued before: the phone keeps the day's document ids.

When it looks:
- **While Share runs**: a `ContentObserver` on MediaStore's images and videos, then a scan of the
  folder, at most every few seconds.
- **With Share closed**: a `JobScheduler` job with content URI triggers on the same MediaStore
  addresses. Android fires it when the camera adds a photo, also for an app without photo
  permissions; this must be confirmed on the Pixel first (Risks). The job scans, queues and sends,
  then arms itself again.
- **As a safety net**: a periodic check every 15 minutes while the day runs, and whenever Share
  comes to the front.

A scan is one query of the folder's children (`DocumentsContract`), which takes well under a
second even for thousands of photos.

### Sending

- New files go into the existing upload queue: one batch for the day, into the folder, marked
  as automatic. Each upload carries `auto` (tus metadata `auto=1`; with a bucket `"auto": true`).
- **Share in front**: the transfer job takes them, as when sending by hand.
- **Share closed**: the trigger job sends them itself, within the ten minutes Android gives a
  job in the background. Uploads go in pieces, so what isn't done goes on at the next run. Photos
  fit easily; a long video may take several runs. User-initiated jobs (Android 14 on) and the
  foreground service before can't start from the background, so they aren't used here.
- **Videos** over 50 MB wait for Wi-Fi (an unmetered network), unless Also over mobile data is on;
  with Videos too off, videos are left out.
- **Thumbnails** are made on the phone, as when sending by hand, and sealed for an encrypted
  folder.
- The phone's own keys work while it is locked (the Keystore key needs no unlocked screen), so
  encrypted folders go on with the phone in a pocket.

### While it runs (63, 64)

- A line over the library: "Sending automatically · 38 photos, 2 videos today · until midnight",
  with Pause.
- One ongoing notification for the day, not one per photo, with the counts, a bar while sending,
  Pause, and Stop for today. Without notification permission it runs all the same, and the line in
  the app is the only sign.
- **Pause**: photos taken while paused are left out for good, for a while nobody needs to see.
  Line and notification say Paused, with Go on. The server forgets the sending while paused, so the
  others' line goes too.
- **Stop for today**: ends the day early, as midnight does.

### At midnight (66)

- A job at the phone's midnight (WorkManager; a few minutes late does no harm, since files are
  checked against midnight anyway) ends the day: it removes the triggers, tells the server
  (`DELETE /api/auto-send`; the server forgets it at its end anyway), and turns the notification
  into "Sent automatically yesterday: 86 photos and 4 videos went to Wedding Anna & Marco."
- What is still in the queue goes on sending.
- The next time, the Send tab shows the same as a card (66), with OK and Again today.

### Taking a photo back (65)

- In the viewer, a photo the person sent automatically today has Take back. It asks first, then
  calls `POST /api/files/{id}/take-back`: the photo goes to Recently deleted for everyone, with the
  person as who deleted it.
- The website's viewer has the same, for the same files.

## Everyone else (67, 68)

- **Who is sending**: a line over the folder's library, "Stefan is sending today's photos", from
  the folder's `auto_senders`: the people sending into it now, without the viewer's own phones.
  PIN guests see nobody's name, so they don't get the line.
- **Who shared a photo**: tiles of photos from someone else carry that person's initial, and the
  viewer's top line says "From Stefan · 15:48", with "sent automatically" where it was. Until now
  the app showed the sender only under Details, and the website not at all. Files from PIN guests
  stay without a name.
- No notifications.

## Server

Schema (in `0001_schema.sql`, until the release; databases are made again):
- `files.auto BOOLEAN NOT NULL DEFAULT false`;
- `auto_sends (device_id UUID PRIMARY KEY REFERENCES devices (id) ON DELETE CASCADE, folder_id UUID
  NOT NULL REFERENCES folders (id), until TIMESTAMPTZ(3) NOT NULL, started_at TIMESTAMPTZ(3) NOT
  NULL)`.

API (with `contract/api` fixtures for each):
- **Uploads** take `auto` only from a person's phone or browser, not from a PIN; `files.auto`
  keeps it.
- **`PUT /api/auto-send`** `{folder, until}`: this device sends into folder until then, at most
  24 hours ahead; the person must be able to send into the folder (403 otherwise).
  **`DELETE /api/auto-send`** ends it. A device signed out takes its entry with it.
- **`FolderInfo.auto_senders`**: the names of the people whose phones send into the folder now
  (`until` in the future), without the caller's own phones; empty for PIN guests.
- **`FileInfo`** gets `auto` (sent automatically), `mine` (sent by the caller, from any of their
  phones and browsers) and `take_back_until` (until when the caller may take it back, or null:
  their own, automatic, and before the end of its upload day in the server's time zone).
- **`POST /api/files/{id}/take-back`**: for those files only; it trashes the file as an admin's
  delete does, with the person as `deleted_by`. 404 for someone else's file, 409 `too_late` after
  the day (`contract/errors.json`).

## Website

- No turning on: the browser has no gallery.
- The folder's line (67), initials on others' tiles, the viewer's "From Stefan · 15:48 · sent
  automatically", and Take back for the person's own automatic files of today.

## App

- **Dart**: the card and the sheet on the Send tab, the line in the library, the summary card,
  Take back in the viewer, initials on tiles and the sender in the viewer's top line.
- **Platform channel** (`contract/app/platform.json`): `auto.pick_folder`, `auto.start {folder,
  videos, mobile_data}`, `auto.pause`, `auto.resume`, `auto.stop`, `auto.status`, and events with
  the day's counts.
- **Kotlin**:
  - `AutoSend`: the day's settings, in the app's preferences;
  - `AutoScanner`: the folder's listing and what counts;
  - `AutoTriggerJob`: the content URI trigger, armed again after each run, which scans, queues and
    sends;
  - `AutoDayEnd`: the job at midnight;
  - a notification channel, Sending automatically.

## Tests

- **Go**:
  - `auto` only from devices;
  - `auto-send` entries: start, end, expiry, folder access, and nothing for PIN guests;
  - take-back: own, automatic, the same day; someone else's file 404; the next day 409; into
    Recently deleted with the right `deleted_by`;
  - the new `FileInfo` fields.
- **Website** (vitest): the line, the initials, the viewer's line, and Take back only where allowed.
- **Dart**: the card, the sheet (today only, folder choice), the line, the summary, Take back;
  goldens of 61 to 68.
- **Kotlin** unit tests:
  - the scanner's choice: types, hidden files, still being written, before the start or after
    midnight, already queued;
  - big videos: held for Wi-Fi, or sent over mobile data when that is allowed;
  - the day's end across a change to or from summer time.
- **On the Pixel**:
  - the trigger with Share closed and no photo permission;
  - a whole day's battery and data;
  - an encrypted folder with the phone locked.

## Risks

- **The trigger without photo permissions**: if Android doesn't fire the content URI trigger for
  an app that can't read the photos, the 15-minute check and opening Share remain. Photos then
  arrive later, but none are lost. This is the first thing to try, before the rest is built.
- **Other camera folders**: some phones save elsewhere, on an SD card or into another folder;
  Change covers them.
- **Doze**: on a phone left untouched for long, Android delays background jobs. At an event the
  phone is in use, so this matters little.

## Order of work

1. **A spike on the Pixel**: the folder picker and the content URI trigger without photo
   permissions. Kotlin only, not kept.
2. **Server**: schema, `auto` on uploads, the `FileInfo` fields, `auto-send`, take-back, contracts
   and tests.
3. **Website**: the line, the initials, the viewer's line, and Take back.
4. **App**: the Kotlin part and the platform contract, then the Dart screens, tests and goldens.
5. **README**, and this plan updated to what was built.
