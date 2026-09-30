# Share

A self-hosted place where family and friends drop photos, videos and documents.

- **Sending** works in any browser with a 5-character PIN: no app, no account. Uploads go in
  pieces and continue after a dropped connection, so videos of several gigabytes get through,
  also behind Cloudflare's 100 MB request limit.
- **Seeing and downloading** everything is for people with an account, in an Android app
  (planned).
- Files are stored unchanged in one folder per upload day: `<storage_dir>/2026-09-30/IMG_0001.jpg`.

The server is one Go program without dependencies at runtime. It runs on a router or another
small Linux machine with a USB drive, or on a VPS. The website is embedded in it.

## Status

| Part | State |
|------|-------|
| Server: PINs, uploads, storage, thumbnails | works |
| Website: sending with a PIN (English, German, Italian) | works |
| Website: continuing after the page was closed, install as an app | works |
| Android app: see and download, then send and manage | next |

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
server/share serve                 # http://localhost:8080
```

Plain `http://` is accepted only for `localhost`.

For work on the website, `cd web && npm run dev` serves it with hot reload and forwards the API to
the server on `127.0.0.1:8080`.

## Configuration

`config.example.json` has the common settings. Everything except `public_url` and `storage_dir`
has a default. Unknown or misspelled fields stop the server with a message saying which one.

Behind a Cloudflare Tunnel, [`deploy/cloudflared`](deploy/cloudflared/README.md) lists the
hostname and the Cloudflare settings Share needs.

## Repository

| Folder | What's in it |
|--------|--------------|
| `server/` | The Go server (`cmd/share`), with the website embedded |
| `web/` | The website: Vite, TypeScript, Preact and Uppy |
| `contract/` | JSON fixtures the server and website tests share: PIN rules, error codes, API responses |
| `deploy/` | The Cloudflare Tunnel settings |
| `scripts/` | `build.sh` and `build.ps1`: the website, then the server for linux/arm64 and linux/amd64 |
| `docs/` | The plan and the screen mockups |

## Development

```sh
(cd server && go vet ./... && go test ./...)   # -short skips the 120 MiB upload test
(cd web && npm run typecheck && npm test)
scripts/build.sh                               # dist/share-linux-arm64, dist/share-linux-amd64
```

## License

Not decided yet. Until there is a license file, all rights are reserved.
