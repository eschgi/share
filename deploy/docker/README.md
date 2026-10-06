# Share in Docker

The image `ghcr.io/eschgi/share` holds the server with its website, for amd64 and arm64. Each
release is tagged with its version (`0.1.0`), its minor version (`0.1`) and `latest`. To build
it yourself instead, run `docker build --build-arg VERSION=$(scripts/version.sh) -t share .` in
the repository.

Two setups to copy, each a folder with a `compose.yaml` that runs Share and its database,
PostgreSQL 18:

| Folder | For |
|--------|-----|
| [`caddy/`](caddy) | A server with a public address, e.g. a VPS. Caddy gets the certificates, and can serve other apps under their own names next to Share. |
| [`cloudflared/`](cloudflared) | A machine without a public address, e.g. at home, behind a Cloudflare Tunnel. Nothing needs to be opened in the firewall. |

## Starting

1. Copy the folder to the server, and make `config.json` from the example:
   `cp config.example.json config.json`. Set `public_url` to your address, and `time_zone`:
   containers run on UTC otherwise, and the day folders follow it.
   - Make a password for the database in a file `.env` next to `compose.yaml`:
     `echo "POSTGRES_PASSWORD=$(openssl rand -hex 24)" >> .env`. Docker hands it to the database
     and to Share; `config.json` names the database without it.
   - For `caddy/`, put the same name into `Caddyfile`, and point its DNS record at the server.
   - For `cloudflared/`, put the tunnel's token into `.env` too: `TUNNEL_TOKEN=…`. In the
     tunnel's public hostname, the service is `http://127.0.0.1:8080`
     ([the tunnel guide](../cloudflared/README.md) has the rest).
2. Start: `docker compose up -d`. The database starts first; Share makes its tables in it.
3. Open your address: the setup page shows the storage folder in the volume and its drive, sets
   it up, and makes your account as the admin. From outside the network at home this works in the
   first 15 minutes after Share starts; later, `docker compose restart share` opens them again,
   or `docker compose logs share` has a link that works until the next start.

Share's commands run in the container, e.g. `docker compose exec share share pin create --day`
or `docker compose exec share share check`.

## The files

- The files are in the volume `share`, mounted at `/data`: a folder for each of Share's
  folders, and the thumbnails in `/data/.share`. The records (the files' names, folders and
  days, the people, the PINs) are in the database, in the volume `db`. Back both up;
  `docker volume ls` shows their full names.
- A copy of the database: `docker compose exec db pg_dump -U share share > share.sql`. Into an
  empty database it goes back with `docker compose exec -T db psql -U share share < share.sql`.
- To keep the files in a folder of the server instead, mount it at `/data`
  (`- /srv/share:/data`) and give it to the container's user: `sudo chown -R 65532:65532 /srv/share`.
- `config.json` is read when Share starts: after a change, `docker compose restart share`.

## Files in an S3 bucket

With the files in a bucket ([the README](../../README.md#files-in-an-s3-bucket) has how to set it
up), the thumbnails go there too, and the volume `share` stays empty. In `config.json`, `s3`
takes the place of `storage_dir`:

```json
{
  "public_url": "https://share.example.com",
  "database": {"postgres": "postgres://share@db/share?sslmode=disable"},
  "time_zone": "Europe/Rome",
  "proxy": {"headers": "x-forwarded", "trusted_proxies": ["172.30.0.2"]},
  "s3": {
    "endpoint": "https://<account id>.r2.cloudflarestorage.com",
    "region": "auto",
    "bucket": "family-share",
    "access_key_id": "…",
    "secret_access_key": "…"
  }
}
```

There is no folder to set up then: the setup page goes straight to your account.
`docker compose run --rm share check` says whether the bucket answers, and prints the CORS rules
when the bucket still needs them.

## Upgrading

`docker compose pull && docker compose up -d`. Share brings its tables up to date when it
starts. The database stays on PostgreSQL 18: a new major version of PostgreSQL needs the data
copied over with `pg_dump` and back, which the image doesn't do by itself.

From a build before 0.1.0, which kept its records in a file in the volume `share`: see
[the README](../../README.md#upgrading). Share starts with an empty database.

## Good to know

- **Never publish Share's plain-http port on every address** (`8080:8080`). Docker passes some
  connections on from its own private addresses, e.g. over IPv6 or from the server itself, and
  Share would take those visitors for visitors at home. Publish it only on the address in the
  home network, for the app at home (`home_url`, see `cloudflared/compose.yaml`), or not at all.
- The container runs as the user 65532, not as root.
- The image checks itself with `share health`; `docker compose ps` says whether it is healthy.
  A proxy's own health checks go to `/healthz`.
- Share gives running requests 20 seconds when it stops, so the setups give it 30
  (`stop_grace_period`) instead of Docker's 10.
- With Traefik instead of Caddy, see [the reverse-proxy guide](../reverse-proxy/README.md).
