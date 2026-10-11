# Sorting the library — plan

The library sorts by the day files arrived. Asked on 2026-10-11: let people choose to sort by
the date a photo or video was taken instead. The screens are 120 to 125 of
[share-mockup.html](share-mockup.html). Nothing is built yet.

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

- **The app** sends it with each upload: Android's gallery knows it for photos and videos
  (`DATE_TAKEN`).
- **The website** reads it from the file before sending: EXIF `DateTimeOriginal` in JPEG and
  HEIC, the creation time in MP4 and MOV.
- **The server** reads it itself for files that came without it, from the first bytes on the
  drive or in the bucket. A job in the background does the files already there, as the
  thumbnails are made for photos that came without one.
- **A photo's clock** has no time zone in EXIF: the date counts as written, in the server's
  `time_zone`, where nothing says otherwise.
- **Nothing moves on the drive:** files stay in the folder of the day they arrived.

## Open questions

- **Encrypted folders:** the server can't read their files. Either the sending phone or browser
  sends the date taken unencrypted, which tells the server when each photo was taken, or those
  folders sort by date taken only on the phone, from dates kept inside the encryption.
- **One choice for every folder,** or one for each folder, for example oldest first only for
  the wedding?
