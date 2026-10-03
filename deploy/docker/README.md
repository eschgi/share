# Share in Docker

The image `ghcr.io/eschgi/share` holds the server with its website, for amd64 and arm64. Each
release is tagged with its version (`0.4.0`), its minor version (`0.4`) and `latest`. To build
it yourself instead, run `docker build -t share .` in the repository.

Two setups to copy, each a folder with a `compose.yaml`:

| Folder | For |
|--------|-----|
| [`caddy/`](caddy) | A server with a public address, e.g. a VPS. Caddy gets the certificates, and can serve other apps under their own names next to Share. |
| [`cloudflared/`](cloudflared) | A machine without a public address, e.g. at home, behind a Cloudflare Tunnel. Nothing needs to be opened in the firewall. |

## Starting

1. Copy the folder to the server, and make `config.json` from the example:
   `cp config.example.json config.json`. Set `public_url` to your address, and `time_zone`:
   containers run on UTC otherwise, and the day folders follow it.
   - For `caddy/`, put the same name into `Caddyfile`, and point its DNS record at the server.
   - For `cloudflared/`, put the tunnel's token into a file `.env`: `TUNNEL_TOKEN=…`. In the
     tunnel's public hostname, the service is `http://127.0.0.1:8080`
     ([the tunnel guide](../cloudflared/README.md) has the rest).
2. Prepare the storage folder in the volume, once: `docker compose run --rm share init`
3. Start: `docker compose up -d`
4. `docker compose logs share` shows the link for the first admin.

Share's commands run in the container, e.g. `docker compose exec share share pin create --day`
or `docker compose exec share share check`.

## The files

- Everything is in the volume `share`, mounted at `/data`: the files, a folder for each of
  Share's folders, and the database and thumbnails in `/data/.share`. Back that volume up;
  `docker volume ls` shows its full name.
- To keep the files in a folder of the server instead, mount it at `/data`
  (`- /srv/share:/data`) and give it to the container's user: `sudo chown -R 65532:65532 /srv/share`.
- `config.json` is read when Share starts: after a change, `docker compose restart share`.

## Upgrading

`docker compose pull && docker compose up -d`

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
