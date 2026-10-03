# Share behind a Cloudflare Tunnel

For a machine without a public address, e.g. a small server at home: `cloudflared` connects out
to Cloudflare and forwards the requests to Share's http port (`http.listen`, `:8080` unless set
otherwise). Cloudflare handles HTTPS, and nothing needs to be opened in the firewall. With Docker,
[`../docker/cloudflared`](../docker/cloudflared/compose.yaml) runs both.

Tell Share about the tunnel in `config.json`:

```json
"proxy": "cloudflare"
```

Share then knows the tunnel's requests by `CF-Connecting-IP` and `Cf-Visitor`, so its cookies stay
secure, and plain http from the internet, through the tunnel or not, is turned away. Without that
line, Share refuses what comes through the tunnel, and its log says so.

## Public hostname

In Zero Trust → Networks → Tunnels → your tunnel → Public hostname, add:

| Field    | Value                             |
|----------|-----------------------------------|
| Hostname | `share.example.com`               |
| Service  | `http://127.0.0.1:8080`           |

If the tunnel is configured with a `config.yml` instead:

```yaml
ingress:
  - hostname: share.example.com
    service: http://127.0.0.1:8080
  # ... your other hostnames ...
  - service: http_status:404
```

If `cloudflared` runs on another machine than Share, point the service at Share's address at home
(e.g. `http://192.168.1.20:8080`) and say where `cloudflared` connects from:

```json
"proxy": {"headers": "cloudflare", "trusted_proxies": ["192.168.1.30"]}
```

Share takes the visitor's address from `CF-Connecting-IP`, and only from those addresses (with
`"proxy": "cloudflare"`, this machine). It needs the address to limit wrong PIN tries per visitor.

## No Cloudflare Access

The website must open without a Cloudflare login; the PIN is the protection. Check that no Access
application covers `share.example.com`, including wildcard applications such as `*.example.com`.
With one in front, the family would get a Cloudflare login page, and uploads and the app would fail.

## Settings for the hostname

In the Cloudflare dashboard for your domain:

- **Caching → Cache Rules**: a rule "Hostname equals `share.example.com`" → *Bypass cache*.
  Share already marks its answers as not cacheable; the rule makes sure.
- **Rules → Configuration Rules**, for the same hostname:
  - *Rocket Loader* off. It rewrites the page's scripts, which the page's security policy blocks.
  - *Email Obfuscation* off. It injects a script as well.
- **Security → Bots**: *Bot Fight Mode* challenges requests that don't come from a browser, which
  would include the app's uploads and downloads. Keep it off for the zone, or check after
  installing the app that sending and downloading still work.

## Limits to know

- Requests can be at most 100 MB (Free and Pro plans). Uploads go in pieces of `chunk_size_mib`
  (20 MiB by default, at most 90), so files of any size get through.
- Cloudflare gives up on a request after roughly two minutes without an answer. A 20 MiB piece
  gets through well within that on uplinks faster than about 2 Mbit/s. If people send from
  slower connections, lower `chunk_size_mib`.
- Cloudflare's terms don't want the CDN to serve large amounts of video. Downloads through the app
  use the address at home (`home_url`) when the phone is there. On a server with a public address,
  e.g. a VPS, a tunnel isn't needed: set the hostname to *DNS only* and put a reverse proxy in front
  ([`../reverse-proxy`](../reverse-proxy/README.md)), or let Share serve HTTPS itself.

## Checking

```sh
curl -s https://share.example.com/api/info
curl -sI https://share.example.com/ | grep -i -E 'cf-cache-status|content-security-policy'
```

`cf-cache-status` should be `DYNAMIC` or `BYPASS`, never `HIT`.
