# Share

A self-hosted place where family and friends drop photos, videos and documents.

- **Sending** works in any browser with a 5-character PIN: no app, no account. Uploads go in
  pieces and continue after a dropped connection, so videos of several gigabytes get through,
  also behind Cloudflare's 100 MB request limit.
- **Seeing and downloading** everything is for people with an account, in an Android app.
  They join with an invite, without a password, and send from the app too. At home the app uses
  the server's local address and skips the internet. Admins manage PINs and people there, and
  deleted files wait 30 days in Recently deleted.
- Files are stored unchanged in one folder per upload day: `<storage_dir>/2026-09-30/IMG_0001.jpg`.

The server is one Go program without dependencies at runtime. It runs on a router or another
small Linux machine with a USB drive, or on a VPS. The website is embedded in it.

## Screenshots

**The website**, for sending with a PIN:

<table>
  <tr>
    <td align="center"><img src="docs/screenshots/web-pin.png" width="180" alt="Enter your PIN, with three of the five characters typed"><br><sub>Entering the PIN</sub></td>
    <td align="center"><img src="docs/screenshots/web-ready.png" width="180" alt="Send your files: this PIN works until tomorrow, 08:06"><br><sub>Ready, with a 24-hour PIN</sub></td>
    <td align="center"><img src="docs/screenshots/web-sending.png" width="180" alt="Sending your files: 5 of 12, about 3 minutes left"><br><sub>Sending</sub></td>
    <td align="center"><img src="docs/screenshots/web-join.png" width="180" alt="Stefan invited you: download the app, install it, come back and tap Join"><br><sub>An invite, before the app is installed</sub></td>
  </tr>
</table>

**On a computer and a tablet**, the same pages use the space:

<table>
  <tr>
    <td align="center"><img src="docs/screenshots/web-computer-ready.png" width="380" alt="Send your files, with an area to drop files or folders on"><br><sub>Dropping files or whole folders</sub></td>
    <td align="center"><img src="docs/screenshots/web-computer-sending.png" width="380" alt="Sending 6 of 24 files, the progress beside the tiles"><br><sub>Sending, the progress in view</sub></td>
  </tr>
  <tr>
    <td align="center"><img src="docs/screenshots/web-computer-join.png" width="380" alt="An invite opened on a computer, as a QR code to scan with the Android phone"><br><sub>An invite on a computer, for the phone</sub></td>
    <td align="center"><img src="docs/screenshots/web-tablet-sending.png" width="264" alt="Sending on a tablet: one card, five tiles to a row"><br><sub>Sending on a tablet</sub></td>
  </tr>
</table>

**The Android app**, for people with an account:

<table>
  <tr>
    <td align="center"><img src="docs/screenshots/app-library.png" width="180" alt="The library, grouped by upload day"><br><sub>The library, by day</sub></td>
    <td align="center"><img src="docs/screenshots/app-select.png" width="180" alt="Seven files selected, with a button to download them"><br><sub>Selecting to download</sub></td>
    <td align="center"><img src="docs/screenshots/app-send.png" width="180" alt="Sending 12 of 40 files, directly over the local Wi-Fi"><br><sub>Sending, over the home Wi-Fi</sub></td>
    <td align="center"><img src="docs/screenshots/app-settings.png" width="180" alt="An admin's settings: upload PINs, language, theme, server and people"><br><sub>Settings</sub></td>
  </tr>
  <tr>
    <td align="center"><img src="docs/screenshots/app-pins.png" width="180" alt="A permanent PIN and one for 24 hours"><br><sub>Upload PINs</sub></td>
    <td align="center"><img src="docs/screenshots/app-invite.png" width="180" alt="An invite for Oma Rosa as a QR code"><br><sub>Inviting someone</sub></td>
    <td align="center"><img src="docs/screenshots/app-recently-deleted.png" width="180" alt="Recently deleted files, with the days left"><br><sub>Recently deleted</sub></td>
    <td align="center"><img src="docs/screenshots/app-themes.png" width="180" alt="The theme picker: Automatic, five dark and two light themes"><br><sub>Seven themes</sub></td>
  </tr>
</table>

Both speak English, German and Italian.

## Status

| Part | State |
|------|-------|
| Server: PINs, uploads, storage, thumbnails | works |
| Server: accounts and invites, library and downloads, a local address for the app | works |
| Server: managing PINs and people, deleting and restoring files | works |
| Website: sending with a PIN, the invite page (English, German, Italian) | works |
| Website: continuing after the page was closed, install as an app | works |
| Website: layouts for tablets and computers, dropping files and folders, invites as a QR code | works |
| Android app: see and download | built and tested, not yet tried on a phone |
| Android app: send, manage PINs and people, Recently deleted | built and tested, not yet tried on a phone |
| Self-updating app, Google Play, a VPS setup | planned |

The plan and the screens are in [`docs/`](docs/).

## How it works

- An admin makes a PIN, in the app or with `share pin create`: a permanent one, for the family, or
  one for 24 hours, for a party. A PIN link (`https://share.example.com/#K7M2Q`) fills it in.
- The website sends with [tus](https://tus.io), in pieces of 20 MiB that the server confirms one by
  one, so a dropped connection costs at most one piece. If the page is closed in the middle, it
  offers to continue when it's opened again. It can be installed as an app.
- On a computer, files and whole folders can be dropped on the page, the tab shows how far sending is,
  and closing it in the middle asks first. An invite opened there shows a QR code for the phone.
- A finished file moves into the day's folder. The sender's browser or phone makes its thumbnail; for
  JPEG, PNG and GIF the server makes one itself if none came.
- The app shows the library by day. It saves photos and videos into the gallery, in the album
  "Share", and documents into Downloads. It sends the same way as the website. Transfers keep going
  when the app is closed, and continue where they stopped after an interruption.
- At home the app reaches the server on its local address, without Cloudflare and the internet.
- Deleted files stay in Recently deleted for 30 days (`trash_days`), and admins can bring them back.

## Security

- A PIN only lets people send. Nobody can see or download anything with it, and others with the same
  PIN don't see what was sent.
- Wrong PINs are limited: 5 from one browser or phone, or 30 from one address, within 10 minutes,
  then a 10-minute pause. Above 300 an hour in total, new unlocks pause; sending goes on. Sign-ins
  and invites have limits too.
- An invite works once, within 24 hours. Passwords are optional; the server keeps PBKDF2 hashes.
- Each phone has its own key. The server keeps only its SHA-256 hash, as for every token. On the
  phone it's encrypted with a key in the Android KeyStore and left out of backups. Admins can sign
  a phone out.
- The local address uses HTTPS with a certificate the server makes itself. The app trusts it only
  because it learned the fingerprint over the public address.
- PINs and invites in links come after the `#`, which browsers don't send to the server or to
  Cloudflare, and the pages remove them from the address bar.
- The website loads nothing from elsewhere, fonts included, and has a strict Content Security
  Policy. Its cookies are `__Host-`, HttpOnly and SameSite=Strict. Downloads are marked so that
  Cloudflare neither caches nor changes them.

## Try it locally

You need Go 1.25 or newer and Node 22.

```sh
cd web && npm ci && npm run build && cd ..        # the website, into server/internal/webui/dist
cd server && go build -o share ./cmd/share && cd ..

mkdir -p ~/share-files
cat > config.json <<EOF
{
  "public_url": "http://localhost:8080",
  "storage_dir": "$HOME/share-files",
  "time_zone": "Europe/Rome"
}
EOF
server/share init                  # prepares the storage folder and checks the drive
server/share pin create --day      # prints a PIN and a link
server/share serve                 # http://localhost:8080; also prints an invite for the first admin
```

Plain `http://` is accepted only for `localhost`.

For work on the website, `cd web && npm run dev` serves it with hot reload and forwards the API to
the server on `127.0.0.1:8080`.

To try it on a phone or tablet, the page has to come over https or from `localhost`. Browsers keep
the PIN's cookie only there, so with the PC's address (`http://192.168.1.20:8080`) every upload is
refused. Go through the public address (a Cloudflare Tunnel works for a PC too), or, with an Android
phone on USB, run `adb reverse tcp:5173 tcp:5173` (`tcp:8080` for `share serve`) and open
`http://localhost:5173` on the phone.

## Running it

1. Take the server for the machine from the [latest release](https://github.com/eschgi/share/releases/latest):
   `share-linux-arm64` for the router, `share-linux-amd64` for a VPS, `share-windows-amd64.exe` or
   `share-windows-arm64.exe` for a PC, checked against `SHA256SUMS`. Or build them:
   `scripts/build-linux.sh` makes the Linux ones in `dist/`, `scripts/build-windows.sh` the Windows
   ones, and on Windows the PowerShell scripts `scripts\build-linux.ps1` and
   `scripts\build-windows.ps1` do the same.
2. Write `config.json` from `config.example.json`, with at least `public_url` and `storage_dir`.
3. With the drive mounted, run `share init` once. `share check` says whether the drive suits: ext4
   is best, FAT32 can't hold files over 4 GiB.
4. Run `share serve` as a service. The first start prints an invite for the first admin, who opens
   it on their phone.
5. Put it behind a Cloudflare Tunnel ([`deploy/cloudflared`](deploy/cloudflared/README.md)), or let
   it serve HTTPS itself with `tls_cert_file` and `tls_key_file`.

On Windows:

- Write paths in `config.json` with forward slashes, `"storage_dir": "D:/Share"`, or with doubled
  backslashes.
- `share check` can't ask a Windows drive for its file system and free space yet, so nothing holds
  uploads back before the drive is full, and FAT32's 4 GiB limit goes unnoticed. Use an NTFS drive.
- If PowerShell refuses to run the scripts, start them with
  `powershell -ExecutionPolicy Bypass -File scripts\build-windows.ps1`.

`share` without arguments lists the other commands: PINs, invites, people, passwords and the local
certificate.

People get the app from the invite page, which offers the APK set in `app.apk_file` and then hands
the invite to the app. Each release has it as `share.apk`, with `share.apk.json` (its version) to put
next to it. How to build the APK yourself is in [`app/README.md`](app/README.md).

## Configuration

`config.example.json` has the common settings. Everything except `public_url` and `storage_dir`
has a default. Unknown or misspelled fields stop the server with a message saying which one.

Behind a Cloudflare Tunnel, [`deploy/cloudflared`](deploy/cloudflared/README.md) lists the
hostname and the Cloudflare settings Share needs.

For the app, two settings matter:

- `local`: a second address on the home network, over HTTPS with a certificate the server makes
  itself (`share cert` shows it). The app trusts it only because it learned the certificate's
  fingerprint over the public address, and uses it whenever the phone can reach it.
- `app.apk_file`: the APK the invite page offers for download, with `share.apk.json` next to it for
  its version. Without it, the page only offers `app.play_store_url`, once there is one.

## Repository

| Folder | What's in it |
|--------|--------------|
| `server/` | The Go server (`cmd/share`), with the website embedded |
| `web/` | The website: Vite, TypeScript, Preact and Uppy |
| `app/` | The Android app: Flutter, with Kotlin for transfers, the local address and the phone's key ([README](app/README.md)) |
| `contract/` | JSON fixtures the server, website and app tests share: PIN rules, error codes, API responses |
| `deploy/` | The Cloudflare Tunnel settings |
| `scripts/` | `build-linux` and `build-windows`, each as `.sh` and `.ps1`: the website, then the server for arm64 and amd64 |
| `docs/` | The plan, the screen mockups and the screenshots above |

## Development

```sh
(cd server && go vet ./... && go test ./...)   # -short skips the 120 MiB upload test
(cd web && npm run typecheck && npm test)
(cd app && flutter analyze && flutter test)
(cd app/android && ./gradlew testDirectDebugUnitTest testPlayDebugUnitTest)
scripts/build-linux.sh                         # dist/share-linux-arm64, dist/share-linux-amd64
scripts/build-windows.sh                       # dist/share-windows-{amd64,arm64}.exe
```

CI checks every push and pull request. A push to `main` also leaves the server for Linux and
Windows as the artifact `share-server` for a day, and the signed APK as `share-apk` once the
signing secrets are set ([`app/README.md`](app/README.md)). A version tag makes a release with all of
them and `SHA256SUMS`:

```sh
git tag v0.4.0 && git push origin v0.4.0      # a tag with a dash (v0.4.0-rc1) is a pre-release
```

## License

Share is under the [Apache License 2.0](LICENSE). The fonts and icons it includes keep their own
licenses (SIL Open Font License 1.1, ISC); [`NOTICE`](NOTICE) lists them.
