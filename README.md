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

## Status

| Part | State |
|------|-------|
| Server: PINs, uploads, storage, thumbnails | works |
| Server: accounts and invites, library and downloads, a local address for the app | works |
| Server: managing PINs and people, deleting and restoring files | works |
| Website: sending with a PIN, the invite page (English, German, Italian) | works |
| Website: continuing after the page was closed, install as an app | works |
| Android app: see and download | built and tested, not yet tried on a phone |
| Android app: send, manage PINs and people, Recently deleted | built and tested, not yet tried on a phone |
| Self-updating app, Google Play, a VPS setup | planned |

The plan and the screens are in [`docs/`](docs/).

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

## Configuration

`config.example.json` has the common settings. Everything except `public_url` and `storage_dir`
has a default. Unknown or misspelled fields stop the server with a message saying which one.

Behind a Cloudflare Tunnel, [`deploy/cloudflared`](deploy/cloudflared/README.md) lists the
hostname and the Cloudflare settings Share needs.

For the app, two settings matter:

- `local`: a second address on the home network, over HTTPS with a certificate the server makes
  itself (`share cert` shows it). The app trusts it only because it learned the certificate's
  fingerprint over the public address, and uses it whenever the phone can reach it.
- `app.apk_file`: the APK the invite page offers for download. Without it, the page only offers
  `app.play_store_url`, once there is one.

## Repository

| Folder | What's in it |
|--------|--------------|
| `server/` | The Go server (`cmd/share`), with the website embedded |
| `web/` | The website: Vite, TypeScript, Preact and Uppy |
| `app/` | The Android app: Flutter, with Kotlin for downloads, the local address and the phone's key ([README](app/README.md)) |
| `contract/` | JSON fixtures the server, website and app tests share: PIN rules, error codes, API responses |
| `deploy/` | The Cloudflare Tunnel settings |
| `scripts/` | `build.sh` and `build.ps1`: the website, then the server for linux/arm64 and linux/amd64 |
| `docs/` | The plan and the screen mockups |

## Development

```sh
(cd server && go vet ./... && go test ./...)   # -short skips the 120 MiB upload test
(cd web && npm run typecheck && npm test)
(cd app && flutter analyze && flutter test)
(cd app/android && ./gradlew testDirectDebugUnitTest testPlayDebugUnitTest)
scripts/build.sh                               # dist/share-linux-arm64, dist/share-linux-amd64
```

## License

Not decided yet. Until there is a license file, all rights are reserved.
