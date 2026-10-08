# Share

> [!WARNING]
> **Share is in early development and has not reached version 1.0.0.**
>
> Until the 1.0.0 release:
> - **Updates may not install smoothly.** A new version can require manual steps, such as setting
>   up the database again, and updating in place is not guaranteed to work.
> - **Bugs are to be expected.** Features may be incomplete or behave unexpectedly.
> - **The structure can change considerably** between versions, including the database, the
>   configuration, the API, the way files are stored and the apps, without a migration path.
>
> Use it to try Share out, and keep your own copies of anything you can't afford to lose.
>
> **Planning to use Share?** Please say so in a
> [GitHub Discussion](https://github.com/eschgi/share/discussions). Knowing that Share is in use
> helps me keep breaking changes to a minimum and plan updates with existing installations in mind.

A self-hosted place where family and friends drop photos, videos and documents.

- **Sending** works in any browser with a 5-character PIN: no app, no account. Uploads go in
  pieces and continue after a dropped connection, so videos of several gigabytes get through,
  also behind proxies that limit a request's size, such as Cloudflare's 100 MB.
- **Seeing and downloading** is for people with an account, in the Android app or in any browser,
  on iPhones and computers too. They join with an invite, without a password, and send from there
  as well, without a PIN. With the server at home, the app uses its address at home there and
  skips the internet.
  Admins manage PINs, people and folders in either, and deleted files wait 30 days in Recently
  deleted.
- **Folders, each with its own people.** Admins see every folder; everyone else sees the folders
  they were given. A PIN sends into one folder, and one made to show it lets guests see and download
  it too, as at a wedding. Someone who sees only one folder gets the library and sending as they
  always were.
- **End-to-end encryption, per folder.** Its files are encrypted on the phone or browser that sends
  them, and only the phones and browsers of the people who see the folder open them: not the
  server, its drive or bucket, or its database. [End-to-end encryption](#end-to-end-encryption)
  says how, and what it doesn't protect.
- Files are stored as they were sent, in a directory per folder with one folder per upload day
  inside: `<storage_dir>/Family/2026-09-30/IMG_0001.jpg`, an encrypted folder's as their sender
  encrypted them. Or in an S3 bucket, such as Cloudflare R2, Backblaze B2, Amazon S3 or MinIO,
  which browsers and the app send to and fetch from directly.

The server is one Go program with the website embedded in it, and keeps its records in PostgreSQL
18 or newer. It runs on Linux and Windows: on a small computer at home such as a Raspberry Pi or a
NAS, on a VPS, in Docker, or on Google Cloud Run. [Hosting](#hosting) shows the ways to reach it.

## Screenshots

**The website**, for sending with a PIN:

<table>
  <tr>
    <td align="center"><img src="docs/screenshots/web-pin.png" width="180" alt="Enter your PIN, with three of the five characters typed"><br><sub>Entering the PIN</sub></td>
    <td align="center"><img src="docs/screenshots/web-ready.png" width="180" alt="Send your files: this PIN works until tomorrow, 08:06"><br><sub>Ready, with a 24-hour PIN</sub></td>
    <td align="center"><img src="docs/screenshots/web-sending.png" width="180" alt="Sending your files: 5 of 12, about 3 minutes left"><br><sub>Sending</sub></td>
    <td align="center"><img src="docs/screenshots/web-join.png" width="180" alt="Stefan invited you: download the app, install it, come back and tap Join, or use Share in this browser"><br><sub>An invite, on Android</sub></td>
  </tr>
</table>

**On a computer and a tablet**, the same pages use the space:

<table>
  <tr>
    <td align="center"><img src="docs/screenshots/web-computer-ready.png" width="380" alt="Send your files, with an area to drop files or folders on"><br><sub>Dropping files or whole folders</sub></td>
    <td align="center"><img src="docs/screenshots/web-computer-sending.png" width="380" alt="Sending 6 of 24 files, the progress beside the tiles"><br><sub>Sending, the progress in view</sub></td>
  </tr>
  <tr>
    <td align="center"><img src="docs/screenshots/web-computer-join.png" width="380" alt="An invite opened on a computer: a QR code to scan with the Android phone, or Use Share in this browser"><br><sub>An invite on a computer: the phone or this browser</sub></td>
    <td align="center"><img src="docs/screenshots/web-tablet-sending.png" width="264" alt="Sending on a tablet: one card, five tiles to a row"><br><sub>Sending on a tablet</sub></td>
  </tr>
</table>

**The website, for people with an account**, does what the app does:

<table>
  <tr>
    <td align="center"><img src="docs/screenshots/web-join-iphone.png" width="180" alt="An invite on an iPhone: this browser will be signed in as Maria, with the button Use Share in this browser"><br><sub>Joining in the browser, on an iPhone</sub></td>
    <td align="center"><img src="docs/screenshots/web-library.png" width="180" alt="The library on a phone, by upload day"><br><sub>The library, by day</sub></td>
    <td align="center"><img src="docs/screenshots/web-select.png" width="180" alt="Eight files selected, with Delete and Download 8 at the bottom"><br><sub>Selecting many</sub></td>
    <td align="center"><img src="docs/screenshots/web-viewer.png" width="180" alt="One photo, with the film strip, Download, Details and Delete"><br><sub>One file</sub></td>
  </tr>
</table>

<table>
  <tr>
    <td align="center"><img src="docs/screenshots/web-computer-library.png" width="380" alt="The library on a computer, eight tiles to a row, with the search in the header"><br><sub>The library on a computer</sub></td>
    <td align="center"><img src="docs/screenshots/web-computer-select.png" width="380" alt="Twelve files selected, the actions floating at the bottom"><br><sub>Selecting with the mouse and Shift</sub></td>
  </tr>
  <tr>
    <td align="center"><img src="docs/screenshots/web-computer-save.png" width="380" alt="Saving 12 files into the folder Share, a folder for each day"><br><sub>Saving into a folder, in Chrome and Edge</sub></td>
    <td align="center"><img src="docs/screenshots/web-computer-viewer.png" width="380" alt="One photo, with arrows, the film strip and its details beside it"><br><sub>One file, with its details</sub></td>
  </tr>
  <tr>
    <td align="center"><img src="docs/screenshots/web-computer-settings.png" width="380" alt="An admin's settings: the list on the left, with folders and end-to-end encryption, the people on the right"><br><sub>An admin's settings, with the people</sub></td>
    <td align="center"><img src="docs/screenshots/web-computer-pins.png" width="380" alt="Upload PINs: a permanent one and one for 24 hours"><br><sub>Upload PINs</sub></td>
  </tr>
</table>

**Folders**, each with its own people:

<table>
  <tr>
    <td align="center"><img src="docs/screenshots/web-computer-folders.png" width="380" alt="The library on a computer, with the folders in a column beside it: all folders, Family, Wedding Anna & Marco, Kindergarten and Taxes 2026"><br><sub>The library and its folders, on a computer</sub></td>
    <td align="center"><img src="docs/screenshots/app-choose-folder.png" width="180" alt="Choosing a folder in the app: all folders or one of them, each with how many files it holds"><br><sub>Choosing a folder in the app</sub></td>
  </tr>
</table>

<table>
  <tr>
    <td align="center"><img src="docs/screenshots/app-folders.png" width="180" alt="An admin's folders, each with its files, its PINs and who sees it"><br><sub>Folders, for an admin</sub></td>
    <td align="center"><img src="docs/screenshots/app-folder.png" width="180" alt="A folder: a switch to encrypt its new files, who sees it, with a switch for each person, and the PINs that send into it"><br><sub>A folder: encryption, and who sees it</sub></td>
    <td align="center"><img src="docs/screenshots/app-pin-sees.png" width="180" alt="A PIN that shows its folder: its files by day and Download all, with Send and See in a bar at the bottom"><br><sub>A PIN that shows its folder</sub></td>
  </tr>
</table>

**The Android app**, for people with an account:

<table>
  <tr>
    <td align="center"><img src="docs/screenshots/app-library.png" width="180" alt="The library, grouped by upload day"><br><sub>The library, by day</sub></td>
    <td align="center"><img src="docs/screenshots/app-select.png" width="180" alt="Seven files selected, with a button to download them"><br><sub>Selecting to download</sub></td>
    <td align="center"><img src="docs/screenshots/app-send.png" width="180" alt="Sending 12 of 40 files, directly over the local Wi-Fi"><br><sub>Sending, over the home Wi-Fi</sub></td>
    <td align="center"><img src="docs/screenshots/app-settings.png" width="180" alt="An admin's settings: upload PINs, folders, language, theme, server and people"><br><sub>Settings</sub></td>
  </tr>
  <tr>
    <td align="center"><img src="docs/screenshots/app-pins.png" width="180" alt="A permanent PIN and one for 24 hours"><br><sub>Upload PINs</sub></td>
    <td align="center"><img src="docs/screenshots/app-invite.png" width="180" alt="An invite for Oma Rosa as a QR code"><br><sub>Inviting someone</sub></td>
    <td align="center"><img src="docs/screenshots/app-recently-deleted.png" width="180" alt="Recently deleted files, with the days left"><br><sub>Recently deleted</sub></td>
    <td align="center"><img src="docs/screenshots/app-themes.png" width="180" alt="The theme picker: Automatic, five dark and two light themes"><br><sub>Seven themes</sub></td>
  </tr>
</table>

Both speak English, German and Italian.

## How it works

- A new server shows a setup page at its address: the storage folder and its drive, then the
  first admin's account ([Hosting](#hosting), step 6).
- An admin makes a PIN, in the app, on the website or with `share pin create`: a permanent one, for
  the family, or one for 24 hours, for a party. A PIN link (`https://share.example.com/#K7M2Q`)
  fills it in. Its code is made up, or chosen, such as ANNA1.
- **A PIN prints for guests.** A permanent PIN prints as a poster for the entrance or as four cards
  for the tables, in the app or on the website, or saves as a PDF: its QR code, three steps and the
  code to type, in the server's language, so nobody has to send the guests a link.
- The website sends with [tus](https://tus.io), in pieces of 20 MiB that the server confirms one by
  one, so a dropped connection costs at most one piece. If the page is closed in the middle, it
  offers to continue when it's opened again. It can be installed as an app.
- **Files in a bucket.** With an S3 bucket instead of a drive, the website and the app send the
  pieces straight to the bucket and fetch the files from there, with links the server signs for
  each piece and each file. The bytes skip the server, and a tunnel in front of it with its limits;
  the thumbnails go into the bucket too, through the server. [Files in an S3 bucket](#files-in-an-s3-bucket)
  has the setup.
- On a computer, files and whole folders can be dropped on the page, the tab shows how far sending is,
  and closing it in the middle asks first. An invite opened there shows a QR code for the phone.
- A finished file moves into the day's folder. The sender's browser or phone makes its thumbnail; for
  JPEG, PNG and GIF the server makes one itself if none came, except in an encrypted folder.
- The app shows the library by day. It saves photos and videos into the gallery, in the album
  "Share", and documents into `Download/Share`. It sends the same way as the website. Transfers
  keep going when the app is closed, and continue where they stopped after an interruption.
- With the server at home, the app reaches it there on its address at home (`home_url`), plain
  http or https, without the internet.
- People with an account can use the website instead of the app, signed in with their password or
  an invite: the library by day, a viewer that also plays videos, selecting many, sending without a
  PIN, and an admin's settings. One file downloads as it is; several as one ZIP, whose exact size is
  known at once and which the browser's download list resumes. Chrome and Edge on a computer can
  also save them straight into a folder, a folder per day (per folder, then day, for files from
  several folders), skipping files already there. With a bucket there is no ZIP: several files
  download one by one.
- **Folders.** Every file lies in one folder: a directory on the drive, or with a bucket a name in
  the database. Admins see every folder, make, rename and delete them, switch who sees each one,
  per person or per invite, and move files between them; someone who loses a folder stops seeing
  its files. Members see the folders they were given. The library shows one folder or all of them,
  and sending goes into the folder open in the library unless another one is chosen. Deleting a
  folder sends its files to Recently deleted, and restoring one brings the folder back. Someone who
  sees only one folder gets the library and sending as before.
- **A PIN sends into one folder.** Made with "Guests also see this folder", everyone with it also
  sees and downloads what is in that folder, but deletes nothing and sees nobody's name: for a
  wedding, everyone's photos for everyone.
- Deleted files stay in Recently deleted for 30 days (`trash_days`), and admins can bring them back.
- **Share into Share.** In another app's share sheet, Share sends what was picked there: the app as
  "Send with Share", and on Android also the website installed as an app, under the server's name.
  Signed in, it goes at once, or for someone who sees several folders once they chose one on the
  Send tab; with a PIN, with the PIN; with neither, it waits for one, or can be dropped. The files
  wait on the phone until the server has them.
- The app plays videos and sound itself: a copy on the phone if there is one, else straight from the
  server, at home over its local address, or from the bucket. The website's viewer zooms into
  photos, with two fingers, a double tap, the mouse wheel or the keyboard.
- Files saved into a folder on a computer get a mark in the library, as files saved on the phone do
  in the app. A transfer that ends while the page is in the background can say so in a
  notification, once the browser was allowed to show one.
- Admins can give someone who forgot a password a new one, which the server makes up and shows once,
  and see what `share check` finds about the drive or the bucket, such as a full drive, FAT32 or a
  bucket without CORS rules. Settings show the version running on the server.

## End-to-end encryption

A folder can be encrypted end to end: then only the phones and browsers of the people who see it can
open its files, not the server, its drive or bucket, its database, or whoever gets hold of them.
The design and the formats are in [docs/e2ee-plan.md](docs/e2ee-plan.md).

- **Per folder.** An admin turns it on in the folder's settings, on the website or in the app, and
  can make it the default for new folders. From then on each new file is encrypted on the phone or
  browser that sends it, with a key of its own, contents and thumbnail alike; files already in the
  folder stay as they are. Turning it off makes new files plain again, and the encrypted ones still
  open. Names, days, sizes, photo dimensions and video lengths, who sent each file and when, and who
  sees which folder stay readable to the server, so the library, search and sorting work as before.
- **Keys pass on with an OK.** Every phone and browser has a key of its own, which never leaves it.
  Signing in with the password opens the keys at once, once a phone or browser of the person has
  locked them with it; a new password from an admin ends that lock, and the next phone or browser
  waits instead. An invite carries the keys in its link, after the token, where the server never
  sees them: a new person's invite, and one someone makes for their own new phone or browser, but
  not an admin's invite for someone else's phone. A new phone or browser without keys shows
  encrypted files as locked, and waits: the person's other phones and browsers list it under
  "Waiting for your OK". Show opens it, and both screens show the same code of 6 digits; Allow
  passes the keys on, and Not me signs the new one out. Nothing opens by itself. Someone given a
  folder gets its keys the same way, once an admin compared the code on their screen: once per
  person and phone or browser, and again only when their key changes. Only admins are asked about
  other people; members only about their own new phones and browsers. So
  someone who can change the server's database can't slip in a phone or a person of their own. A
  member whose key is lost, with no phone or browser left that holds it and no password that opens
  it, gets a new one by itself, which an admin then allows.
- **Signed by the recovery key.** The recovery key signs every key of every encrypted folder, and
  every decision that a folder sends plain, under its name. Phones and browsers check that before
  they send, and keep what they saw: a folder whose key it didn't sign, or with fewer keys than
  before, gets nothing, and the page or the app says so; a folder switched off without its
  signature still gets encrypted files. So someone who can change the database can't switch a
  folder's encryption off, name a key of their own for it, or pass another folder off under its
  name; nor get folder keys sealed for a recovery key or a PIN secret of their own. Admins' phones
  and browsers hold the recovery key too, passed on with an OK like folder keys, and only they make
  folders, switch encryption and rename folders that send plain; an admin's phone that doesn't
  hold it yet says so.
- **The recovery code.** The first encrypted folder shows its admin a recovery code, once: written
  down or printed, it opens every encrypted folder again should all phones and browsers be lost.
  A new one can be made in the settings, which ends the old one: the old recovery key signs the new
  one, which signs everything anew, and the other admins get it with an OK.
- **PINs.** Guests with a PIN send into an encrypted folder encrypted, and can't read anything back.
  A PIN's link into an encrypted folder names the recovery key after a dot, which the guest's
  browser or phone checks the folder's key with. A PIN that also shows an encrypted folder needs
  its whole link, which carries a secret after the dot that opens the folder; the code alone sends,
  and shows encrypted files as locked.
- **What stays possible.** Videos play and seek, downloads, "one by one" and saving into a folder
  give the original files, decrypted on the device. ZIPs leave encrypted files out; they download
  one by one instead. Encrypted files move only into a folder that has a key: one that is
  encrypted, or was. The command line has no keys: `share invite` and `share pin create --show`
  refuse encrypted folders and point to the app or the website, and `share pin new-code` leaves the
  whole link of a PIN that shows one to them. `share pin create` into an encrypted folder prints a
  link that names the recovery key as the database has it.
- **What it doesn't protect:** the names and other details above; files someone already downloaded;
  anyone holding the whole link of a PIN that shows an encrypted folder, by design (its secret stays
  in the guest's browser until the PIN is forgotten there); a server that was taken over and sends
  browsers changed website code, since the website comes from the server, while the app's code
  doesn't. Where a phone or browser has nothing to check the database with, it takes its word: a
  guest who types a PIN's code instead of opening its link, or has a link made while the folder was
  plain; a phone or browser used before the recovery key was made, which takes the first one the
  server names; and one signed in for the first time with only the password, if the database also
  brings back an older note of that person's and leaves out a folder's newest keys. Someone who can
  change the database can also hide a folder, so that files go into another one, plain if that one
  is. A password opens the keys, so they are as safe as the password, which the server sees at
  sign-in.

## Security

- A PIN only lets people send, unless it was made to show its folder: then everyone with it sees
  and downloads that folder's files, but deletes nothing and sees no names. Otherwise nobody can see
  or download anything with it, and others with the same PIN don't see what was sent.
- Wrong PINs are limited: 5 from one browser or phone, or 30 from one address, within 10 minutes,
  then a 10-minute pause. Above 300 an hour in total, new unlocks pause; sending goes on. Sign-ins
  and invites have limits too.
- An invite works once, within 24 hours. Passwords are optional; the server keeps PBKDF2 hashes.
- Each phone has its own key. The server keeps only its SHA-256 hash, as for every token. On the
  phone it's encrypted with a key in the Android KeyStore and left out of backups. Admins can sign
  a phone out.
- A browser signs in like a phone, with a password or an invite, and keeps its key in a cookie
  that the page's scripts can't read. It stays signed in until 400 days after it was last used.
  Admins can sign it out like a phone, and everyone can sign out their own phones and browsers.
- Cookies belong to an address, not to a network: a browser signed in at `http://192.168.1.20:8080`
  would hand its key to whatever has that address on someone else's Wi-Fi. So a browser that signs
  in at home (plain http, or the https port from the home network) gets a session that works only
  at home; seen anywhere else, it is signed out at once.
- Plain http works only on a home network and on the server itself; from anywhere else pages are
  sent to the https address and the API turns requests away. At home it is unencrypted, so anyone
  on the same Wi-Fi could read along; an https port avoids that.
- Before the app sends its key to an address over plain http, the server there has to prove it is
  the phone's own: an HMAC of a nonce the phone picked, keyed with the hash of the phone's key, which
  only that server keeps. So at someone else's home, with the same 192.168.x addresses, the app
  doesn't hand its key to another device. The server gives the proof only to the home network.
- An https port at home uses a certificate the server makes itself. The app trusts it only because
  it learned the fingerprint over the public address.
- PINs and invites in links come after the `#`, which browsers don't send to the server or to a
  proxy in front of it, and the pages remove them from the address bar.
- A password an admin makes up for someone is shown once and kept only as a hash; it isn't for the
  admin's own account, the person's phones and browsers stay signed in, and the log notes only who
  gave it to whom, by id. The app copies it marked as sensitive, so Android hides it in the
  clipboard's preview.
- The server's version is only for people with an account, not in the public `/api/info`. With
  `app.apk_file` set, though, the invite page's public `/api/app` names the APK's version, which
  in a release is the server's too.
- Files shared into the installed website go to its service worker and from there into the
  browser's storage; the server never reads them on the way, and answers a share that reaches it
  without them. Signing out drops them. The app keeps shared files, or copies of them, in its own
  storage until they are sent, and a week at most.
- The app's player gets the phone's key only where the app's own requests would send it: plain http
  only after the proof above, and never to the https port at home, whose files it fetches first.
- Encrypted folders' files and thumbnails are encrypted on the sending device and decrypted only on
  the family's devices; see [End-to-end encryption](#end-to-end-encryption) for what that protects
  and what not.
- With a bucket, its keys stay on the server. Browsers and the app get links signed for one piece
  of an upload, for an hour and for exactly that piece's size, or for one file, for 12 hours. Such a
  link works for anyone who has it until it ends, even after the file was deleted. The app never
  sends its key to the bucket: it asks the server for a link and fetches that without the key.
- The website loads nothing from elsewhere, fonts included, but with a bucket the files and
  thumbnails from there, and has a strict Content Security Policy. Its cookies are HttpOnly and
  SameSite=Strict, and `__Host-` except over plain http at home, where browsers keep no secure
  ones. Only Share's own pages can change something: a request
  from another site, or one with the website's cookie that doesn't say where it comes from, is
  turned away. Downloads are marked so that proxies such as Cloudflare neither cache nor change
  them.
- Behind a proxy, Share takes the visitor's address and whether they came over https only from the
  proxy `config.json` names (`proxy`), and only from addresses at home or on the server itself.
  That proxy must say who is visiting, or its requests are refused. A proxy Share wasn't told about
  is refused too, so it can't make its visitors look like visitors at home, with the home proof and
  sessions meant for home.

## Try it locally

You need Go 1.26 or newer, Node 22, and PostgreSQL 18 or newer for the records, here from Docker.
To run everything in Docker instead, see [`deploy/docker`](deploy/docker/README.md).

```sh
docker run -d --name share-db -p 127.0.0.1:5432:5432 -e POSTGRES_PASSWORD=share postgres:18
cd web && npm ci && npm run build && cd ..        # the website, into server/internal/webui/dist
cd server && go build -o share ./cmd/share && cd ..
until docker exec share-db pg_isready -q -h 127.0.0.1; do sleep 1; done   # until the database is up

mkdir -p ~/share-files
cat > config.json <<EOF
{
  "public_url": "http://localhost:8080",
  "storage_dir": "$HOME/share-files",
  "database": {"postgres": "postgres://postgres:share@localhost/postgres?sslmode=disable"},
  "time_zone": "Europe/Rome"
}
EOF
server/share init                  # prepares the storage folder and checks the drive
server/share pin create --day      # prints a PIN and a link
server/share serve                 # logs http://localhost:8080/setup: make the first admin there
```

Plain `http://` works on the server itself and from the home network; from anywhere else Share
wants https. `share serve` also prints the address on your network, e.g. `http://192.168.1.20:8080`:
open that on a phone or tablet. To have links and invites point there too, use it as
`public_url`.

For work on the website, `cd web && npm run dev` serves it with hot reload and forwards the API to
the server on `127.0.0.1:8080`; `npm run dev -- --host` makes it reachable from phones at home
(`http://192.168.1.20:5173`). Encrypted folders need the browser's Web Crypto, which only https and
`localhost` have: with a phone on USB, `adb reverse tcp:5173 tcp:5173` lets it open
`http://localhost:5173`. Opened over plain http, the website says so over the library, sends only
into folders that aren't encrypted, and asks before making an invite that couldn't carry the keys.

## Hosting

Share runs wherever you like, and keeps its records in PostgreSQL. The easiest is Docker, whose
setups run Share and its database together. How visitors reach it decides the rest:

| Where Share runs | How visitors reach it | Guide |
|------------------|-----------------------|-------|
| Docker, on any of the machines below, with PostgreSQL in a container next to Share (recommended) | The image `ghcr.io/eschgi/share`, with Caddy or a tunnel | [`deploy/docker`](deploy/docker/README.md) |
| A computer at home, such as a small server, a NAS or a Raspberry Pi, without a public address | A Cloudflare Tunnel: nothing to open in the firewall | [`deploy/cloudflared`](deploy/cloudflared/README.md) |
| A VPS or another server with a public address, also next to other apps under other names | A reverse proxy that makes the certificates: Caddy, nginx or Traefik | [`deploy/reverse-proxy`](deploy/reverse-proxy/README.md) |
| A server with a public address, without a proxy | Share's own HTTPS, with a certificate from files | [below](#https-without-a-proxy) |
| Google Cloud Run, which runs Share only while someone visits, with the files in a bucket and the records in PostgreSQL | Its own address, or your domain | [`deploy/cloud-run`](deploy/cloud-run/README.md) |

The release files and the image `ghcr.io/eschgi/share` come with the first release, 0.1.0; until
it is out, build them as in [Development](#development).

Without Docker:

1. Take the server for the machine from the [latest release](https://github.com/eschgi/share/releases/latest):
   `share-linux-arm64` or `share-linux-amd64` for Linux on arm64 (e.g. a Raspberry Pi) or amd64,
   `share-windows-amd64.exe` or `share-windows-arm64.exe` for Windows, checked against
   `SHA256SUMS`. Or build them: `scripts/build-linux.sh` makes the Linux ones in `dist/`,
   `scripts/build-windows.sh` the Windows ones, and on Windows the PowerShell scripts
   `scripts\build-linux.ps1` and `scripts\build-windows.ps1` do the same.
2. Set up a database in PostgreSQL 18 or newer ([The database](#the-database)), e.g. on the same
   machine: `sudo -u postgres createuser --pwprompt share`, then
   `sudo -u postgres createdb --owner share --encoding UTF8 --template template0 share`. On Windows,
   the same commands are in the `bin` folder of PostgreSQL's installation, with `-U postgres`
   instead of `sudo -u postgres`.
3. Write `config.json` from `config.example.json`, with at least `public_url`, `storage_dir` (or
   `s3`, see [Files in an S3 bucket](#files-in-an-s3-bucket)) and `database`, and the `proxy`
   line from the guide you follow.
4. With the files on a drive of their own, mount it. `share check` says whether the database
   answers, and with a bucket whether the bucket answers and lets Share's pages in.
5. Run `share serve` as a service, on Linux with
   [`deploy/systemd/share.service`](deploy/systemd/share.service). On Windows, which has no
   service for it yet, start it at boot with the Task Scheduler.
6. Open your address. While nobody has an account, it shows the setup page: the storage folder
   and its drive, with what suits it (ext4 is best, FAT32 can't hold files over 4 GiB), and then
   your account as the admin. From outside the network at home this works in the first 15
   minutes after Share starts; later, restart it, or open the link from its log, which works until
   the next start. `share init` sets the folder up on the command line instead; with a bucket
   there is no folder to set up.

### HTTPS without a proxy

Share can serve HTTPS itself, on a server with a public address:
`"https": {"listen": ":443", "certificate": {"cert_file": "…", "key_file": "…"}}`, e.g. with a
certificate from Let's Encrypt's `certbot`. Share reads the certificate when it starts, and the
user `share` can't read certbot's key: let certbot's deploy hook copy both files where it can, and
restart Share, also after each renewal:
`--deploy-hook 'install -o share -m 600 $RENEWED_LINEAGE/privkey.pem $RENEWED_LINEAGE/fullchain.pem /etc/share/ && systemctl restart share'`,
with `/etc/share/privkey.pem` and `/etc/share/fullchain.pem` in `config.json`. Port 443 needs the
right to bind it: uncomment `AmbientCapabilities=CAP_NET_BIND_SERVICE` in the systemd unit.
Visitors then reach Share directly, without Cloudflare's limits.

### On Windows

- Write paths in `config.json` with forward slashes, `"storage_dir": "D:/Share"`, or with doubled
  backslashes.
- `share check` can't ask a Windows drive for its file system and free space yet, so nothing holds
  uploads back before the drive is full, and FAT32's 4 GiB limit goes unnoticed. Use an NTFS drive.
- If PowerShell refuses to run the scripts, start them with
  `powershell -ExecutionPolicy Bypass -File scripts\build-windows.ps1`.

### Commands and the app

`share` without arguments lists the other commands: folders, PINs, invites, people, passwords and
the https port's own certificate. Once there are several folders, `share pin create`,
`share invite` and `share password` for a new member need `--folder NAME` to say which; an admin
sees every folder, so `share invite --admin` takes none.

People get the app from the invite page, which offers the APK set in `app.apk_file` and then hands
the invite to the app; or they use the website in their browser instead, as on an iPhone. Each
release has the APK as `share.apk`, with `share.apk.json` (its version) to put next to it: a file
named as the APK, with `.json` added. With Docker, copy both into the volume, e.g.
`docker compose cp share.apk share:/data/`, and set `app.apk_file` to `/data/share.apk`. How to
build the APK yourself is in [`app/README.md`](app/README.md).

### Upgrading

A new version: replace the program and restart it (with Docker, `docker compose pull` and
`docker compose up -d`). Share brings its tables up to date when it starts; an older Share
refuses a database that a newer one has brought up to date.

From a build that kept its records in SQLite (`<data_dir>/share.db`, and no `database` in
`config.json`): Share now keeps them in PostgreSQL and starts empty there, without the old build's
people, PINs, folders and list of files; everyone joins again with a new invite. Set `database` in
`config.json` (with Docker, take the setup's new `compose.yaml` and its `.env`). Give the new Share
an empty storage folder or volume: it never overwrites the old files, but it doesn't show them
either. With Docker in the same folder, the new setup keeps the old volume `share`, which does no
harm: the old files stay in it, unseen. Keep the old folder, with `<data_dir>/share.db` in it,
until you no longer need the old build.

From a version before `proxy`: Share no longer trusts Cloudflare's tunnel by default. Behind a
tunnel, add `"proxy": "cloudflare"` to `config.json` (the old `cloudflare` setting is refused with
that hint). Without it, every request through the tunnel is turned away, and the log says why.

## Configuration

`config.example.json` has the common settings. Everything except `public_url`, where the files go
(`storage_dir` or `s3`) and the database has a default or can be left out; with a bucket,
`data_dir` is needed only for the https port's own certificate. Unknown or misspelled fields stop
the server with a message saying which one, and settings of earlier versions with where they went.

- `name`: what the pages and the app call the server, 1 to 40 characters; `Share` unless set.
- `languages` and `default_language`: from `en`, `de` and `it`; all three, and `en`, unless set.
- `time_zone`, e.g. `Europe/Rome`: which day an upload belongs to. Without it, the machine's own,
  which in the Docker image is UTC.
- `data_dir`: an absolute path for the thumbnails of files on a drive and the https port's own
  certificate; `<storage_dir>/.share` unless set. `share check` objects to one in memory (tmpfs)
  or inside a container.
- `upload`: `chunk_size_mib`, the size of the pieces (20, from 1 to 90, at least 5 with a bucket);
  `max_file_size_gib` (0, no limit, unless set; at most 5120 with a bucket); `min_free_space_mib`,
  what uploads leave free on the drive (2048); `incomplete_ttl_hours`, when an unfinished upload is
  dropped (168, from 1 to 720).
- `trash_days`: how long Recently deleted keeps files, 30, from 1 to 365.

The ports:

- `http`, `{"listen": ":8080"}` unless set otherwise: the website, the API and uploads over plain
  http. A tunnel or a reverse proxy passes requests on here; with the proxy on the same machine,
  `"listen": "127.0.0.1:8080"` keeps everyone else out. Browsers and the app may use it directly
  only from a home network (`192.168.…`, `10.…`, `172.16–31.…`, `fc…` and `fd…`, and the
  link-local `169.254.…` and `fe80::…`) or the server itself;
  pages from anywhere else go to `public_url`, and the API turns them away. `"http": null`
  switches it off.
- `https`, off unless set, e.g. `{"listen": ":8443"}`: the same over https, with a certificate the
  server makes itself (`share cert` shows it), or with
  `"certificate": {"cert_file": "…", "key_file": "…"}`.
- `proxy`, none unless set: what passes requests on to Share. `"cloudflare"` for `cloudflared` on
  the same machine, `"x-forwarded"` for Caddy, nginx, Traefik and the like on the same machine, or
  `{"headers": "cloudflare" or "x-forwarded", "trusted_proxies": ["192.168.1.30"]}` for a proxy
  elsewhere, e.g. on another machine at home or in another container. Share takes the visitor's
  address and https from these headers only from those addresses, which must be at home or on this
  machine. `share check` says which proxy Share trusts.

The guides in [`deploy/`](deploy/) show each setup, with the settings it needs.

For the app, two settings matter:

- `home_url`: with the server at home, the address the app uses whenever the phone reaches it
  there: plain http, e.g. `http://192.168.1.20:8080`, or the https port, e.g.
  `https://192.168.1.20:8443`. The app trusts
  the https port's own certificate only because it learned the fingerprint over the public address.
- `app.apk_file`: the APK the invite page offers for download, with the same name plus `.json`
  (e.g. `share.apk.json`) next to it for its version. Without it, the page only offers
  `app.play_store_url`, once there is one.

### Files in an S3 bucket

Instead of `storage_dir`, Share can keep the files in a bucket of Amazon S3 or of a service that
speaks its API, such as Cloudflare R2, Backblaze B2 or MinIO. It is one or the other: a server
doesn't mix them, and it remembers where its files are: while it has files, it refuses to start
when `config.json` switches between a drive and a bucket, or names another bucket or prefix. The
thumbnails go into the bucket too, so nothing needs to stay on the machine; `data_dir` is only
needed for the https port's own certificate:

```json
{
  "public_url": "https://share.example.com",
  "database": {"postgres": "postgres://share:PASSWORD@localhost/share"},
  "time_zone": "Europe/Rome",
  "s3": {
    "endpoint": "https://<account id>.r2.cloudflarestorage.com",
    "region": "auto",
    "bucket": "family-share",
    "prefix": "share/",
    "access_key_id": "…",
    "secret_access_key": "…"
  }
}
```

| Service | `endpoint` | `region` |
|---------|------------|----------|
| Cloudflare R2 | `https://<account id>.r2.cloudflarestorage.com` | `auto` |
| Backblaze B2 | the bucket's endpoint, e.g. `https://s3.eu-central-003.backblazeb2.com` | `eu-central-003` |
| Amazon S3 | `https://s3.eu-central-1.amazonaws.com` | `eu-central-1` |
| MinIO or AIStor | e.g. `https://minio.example.com`, with `"path_style": true` | the one it was given, `us-east-1` unless set |

The endpoint is the service's address without the bucket in it. Plain http is for a bucket at home
only, and works only for pages that are opened over plain http too: browsers don't let an https
page send to an http address. `prefix` starts every key, `<prefix>files/<id>`; without it Share
uses the whole bucket. The thumbnails are `<prefix>thumbs/<id>.jpg`.

Setting up the bucket:

1. **CORS rules**, so that Share's pages may send to the bucket and fetch from it: `share check`
   tries them and, when they don't let the pages in, prints the rules to set, for `public_url` and
   `home_url`. On R2 they go into the bucket's settings; elsewhere, e.g.,
   `aws s3api put-bucket-cors --bucket family-share --cors-configuration file://cors.json`, with
   `--endpoint-url` for B2. For `npm run dev`, add `http://localhost:5173` to the origins.
2. **A lifecycle rule** that aborts unfinished multipart uploads after 7 days, as a safety net:
   Share drops the uploads it gives up on itself, after `upload.incomplete_ttl_hours`. R2 adds
   such a rule to new buckets. With `incomplete_ttl_hours` above 168, make the rule longer too:
   it counts from an upload's start, Share from its last piece.
3. **A key for this bucket only**, which reads, writes and deletes objects and multipart uploads:
   on R2 an API token with "Object Read & Write" for the bucket, on B2 an application key for the
   bucket, on Amazon S3 a user with a policy like this one:

   ```json
   {
     "Version": "2012-10-17",
     "Statement": [
       {"Effect": "Allow", "Action": ["s3:ListBucket", "s3:ListBucketMultipartUploads"], "Resource": "arn:aws:s3:::family-share"},
       {"Effect": "Allow", "Action": ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:AbortMultipartUpload", "s3:ListMultipartUploadParts"], "Resource": "arn:aws:s3:::family-share/share/*"}
     ]
   }
   ```

   Without `s3:ListBucket`, Amazon S3 answers "access denied" for a file that isn't there,
   instead of "not found".
4. **Back up the database.** It is the only record of the files' current names, folders and days;
   the bucket holds their bytes and thumbnails under ids.
5. **One prefix per server.** Two servers on the same bucket and prefix drop each other's uploads.

What is different with a bucket:

- There is no ZIP. "Download N" downloads the files one by one; Chrome and Edge on a computer can
  still save them into a folder.
- The files come from the bucket also at home: the home address carries only the API and the
  thumbnails.
- A link to a file works for anyone who has it, for 12 hours, even after the file was deleted.
- There is no free-space check; the bucket's own limits apply, and files can have up to 5 TiB.
  Pieces have at least 5 MiB (`upload.chunk_size_mib`).
- `share serve` waits at the start until the bucket answers, with a clock close to its own: a
  computer without a clock of its own, such as a Raspberry Pi, signs links that fail until it has
  the time. Keys the bucket refuses, or a bucket that isn't there, stop it at once.
- MinIO and AIStor drop unfinished uploads a day after they started (`stale_uploads_expiry`), so an
  upload paused for longer starts over.
- The website's Content Security Policy lets its pages reach the bucket, so anyone can read the
  bucket's address there, on R2 with the account's id. Every link names the access key's id, but
  not its secret.

### The database

Share keeps its records (the files' names, folders and days, the people, the PINs, the keys of
encrypted folders) in PostgreSQL 18 or newer, in a database with the UTF8 encoding:

```json
"database": {"postgres": "postgres://share:PASSWORD@localhost/share"}
```

The address is a URL, `postgres://` or `postgresql://`, with a host and a database; Unix sockets
and `key=value` strings aren't taken.

- The Docker setups run it in a container next to Share and hand it the password from `.env`, as
  `PGPASSWORD`, so the address in their `config.json` names none:
  `postgres://share@db/share?sslmode=disable`.
- Elsewhere: Ubuntu 26.04 comes with PostgreSQL 18; on Debian 13 or Ubuntu 24.04 take it from
  [postgresql.org](https://www.postgresql.org/download/), which also has the installer for
  Windows. A free [Neon](https://neon.tech) project is enough for a family; new ones run 18.
- Without `sslmode=require`, `verify-ca` or `verify-full`, Share takes only a database on the same
  machine or at home: an address such as `127.0.0.1`, `192.168.…` or `fe80::…`, `localhost`, a
  name ending in `.local`, `.home.arpa` or `.internal`, or a name without a dot, such as the Docker
  setups' `db`. Any other name needs one of those three, also one that only the router at home
  knows, such as `nas.lan`.
- Share makes its tables and brings them up to date when it starts. It waits while the database
  doesn't answer, as a sleeping Neon database does for a moment, and stops at once when the
  database refuses it: a wrong password, a database or user that isn't there, an encoding other
  than UTF8, or a PostgreSQL older than 18.
- With the files in a bucket, nothing stays on the machine: `data_dir` can be left out, unless
  the https port uses Share's own certificate, which lives there. That is how Share runs on
  [Google Cloud Run](deploy/cloud-run/README.md).
- `share check` says whether the database answers. Back it up with `pg_dump` (in the Docker
  setups `docker compose exec db pg_dump -U share share > share.sql`), or the way its host offers.

## Repository

| Folder | What's in it |
|--------|--------------|
| `server/` | The Go server (`cmd/share`), with the website embedded |
| `web/` | The website: Vite, TypeScript, Preact and Uppy |
| `app/` | The Android app: Flutter, with Kotlin for transfers, the local address, the phone's key and the keys of encrypted folders ([README](app/README.md)) |
| `contract/` | JSON fixtures the server, website and app tests share: PIN rules, error codes, API responses, the website's paths (`web_routes.json`), the encryption's test vectors (`crypto/`), and what the app's Dart and Kotlin hand each other (`app/platform.json`) |
| `deploy/` | Hosting guides: a Cloudflare Tunnel, a reverse proxy (Caddy, nginx, Traefik), Docker, Google Cloud Run, a systemd service |
| `Dockerfile` | The Docker image: the website and the server, built for amd64 and arm64 |
| `scripts/` | `build-linux` and `build-windows`, each as `.sh` and `.ps1`: the website, then the server for arm64 and amd64; `version.sh` for the version (`version.ps1` only prints it) |
| `docs/` | The plans (`share-plan.md`, `s3-plan.md`, `e2ee-plan.md`) and proposals, the screen mockups and the screenshots above |
| `VERSION`, `config.example.json`, `NOTICE` | The version, an example `config.json`, and the licenses of the fonts and icons |
| `.github/workflows/ci.yml` | CI: the tests, the builds, the signed APK and the releases |

## Development

Besides Go 1.26 and Node 22, the app needs Flutter 3.47 and JDK 21, as CI uses them. The server's
tests need PostgreSQL 18: each test makes a schema of its own in the database that
`SHARE_TEST_POSTGRES` names, and drops it at the end. Without it they fail and say so.

```sh
docker run -d --name share-test-db -p 127.0.0.1:5433:5432 -e POSTGRES_PASSWORD=test postgres:18
export SHARE_TEST_POSTGRES='postgres://postgres:test@127.0.0.1:5433/postgres?sslmode=disable'
```

```sh
(cd server && go vet ./... && go test ./...)   # -short sends 3 MiB, not 120, in the upload test
(cd web && npm run typecheck && npm test)
(cd app && flutter analyze && flutter test)
(cd app && flutter build apk --config-only)     # once: makes app/android/gradlew
(cd app/android && ./gradlew testDirectDebugUnitTest testPlayDebugUnitTest lintDirectDebug)
scripts/build-linux.sh                         # dist/share-linux-arm64, dist/share-linux-amd64
scripts/build-windows.sh                       # dist/share-windows-{amd64,arm64}.exe
docker build --build-arg VERSION=$(scripts/version.sh) -t share .   # the Docker image
```

The server's tests use a bucket of their own. To run the bucket tests against a real one instead,
on a fresh prefix, set `SHARE_TEST_S3` to its `s3` setting:
`(cd server && SHARE_TEST_S3='{"endpoint": …}' go test ./internal/s3 ./internal/app)`. New test
vectors for the encryption come from `(cd server && go test ./internal/e2ee -update)`. The app's
screenshots, for comparing with the mockups, are made with
`(cd app && flutter test --run-skipped --tags golden --update-goldens)`; CI leaves them out.

CI checks pull requests, pushes to `main` and tags: the commands above, and `go test -race` with
the 120 MiB upload, `go vet` for Windows, govulncheck, and the Docker setup started with its
database. It leaves the server for Linux and Windows as the artifact `share-server` for a week.
Pushes to `main` and pull requests from this repository's own branches also leave the signed APK
as `share-apk`, once the signing secrets are set ([`app/README.md`](app/README.md)).

Share has one version, for the server, its website and the app: the one in `VERSION`. A build of
the commit tagged with it is that version, e.g. `0.1.0`; one of another commit by the scripts, CI
or `docker build --build-arg VERSION=…` says what it was made from, e.g. `0.1.0-dev+abc1234`, in
`share version` and on the About pages. A plain `go build` says `dev`, and the app built with
Flutter alone the version in `pubspec.yaml`. The app's versionCode follows from the version (0.1.0
is 100, 1.2.3 is 10203), so it grows with it; a pre-release such as 0.2.0-rc1 has the code of
0.2.0. `scripts/version.sh`, a shell script (on Windows, run it in Git Bash), prints a build's
version, and makes a new one for a release:

```sh
scripts/version.sh set 0.2.0                   # VERSION and app/pubspec.yaml
git commit -am "Release 0.2.0" && git tag v0.2.0 && git push origin main v0.2.0
```

The tag makes a release with the server, the APK with `share.apk.json`, and `SHA256SUMS`, and
publishes the Docker image as `ghcr.io/eschgi/share` (after the first one, make the package public
once in GitHub's package settings). CI refuses a tag that isn't `VERSION`'s; one with a dash, such
as `v0.2.0-rc1`, is a pre-release.

## License

Share is under the [Apache License 2.0](LICENSE). The fonts and icons it includes keep their own
licenses (SIL Open Font License 1.1, ISC and MIT); [`NOTICE`](NOTICE) lists them.
