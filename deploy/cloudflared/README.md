# Share behind a Cloudflare Tunnel

`cloudflared` forwards the requests to Share's http port (`http.listen`, `:8080` unless set
otherwise). Cloudflare handles HTTPS, and nothing needs to be opened on the router. Share knows
these requests from the tunnel by `CF-Connecting-IP`, so its cookies stay secure; plain http from
the internet, through the tunnel or not, is turned away.

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
(e.g. `http://192.168.8.1:8080`) and say where `cloudflared` connects from:

```json
"cloudflare": {"trusted_proxies": ["192.168.8.20"]}
```

Share takes the visitor's address from `CF-Connecting-IP`, and only from those addresses (by
default this machine). It needs the address to limit wrong PIN tries per visitor.

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
  use the address at home (`home_url`) when the phone is there. On a VPS, set the hostname to *DNS
  only* and let Share serve HTTPS itself:
  `"https": {"listen": ":443", "certificate": {"cert_file": "…", "key_file": "…"}}`.

## Checking

```sh
curl -s https://share.example.com/api/info
curl -sI https://share.example.com/ | grep -i -E 'cf-cache-status|content-security-policy'
```

`cf-cache-status` should be `DYNAMIC` or `BYPASS`, never `HIT`.
