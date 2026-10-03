# Share behind a reverse proxy

On a server with a public address, a reverse proxy such as Caddy, nginx or Traefik takes the
visitors' https, gets the certificates, and passes the requests on to Share's plain-http port. One
proxy can serve several apps on the same server, each under its own name.

## Share's side

In `config.json`:

```json
"http": {"listen": "127.0.0.1:8080"},
"proxy": "x-forwarded"
```

- `127.0.0.1` keeps everyone out but the proxy on this machine.
- `"x-forwarded"`: Share takes the visitor's address from `X-Forwarded-For`, and whether they came
  over https from `X-Forwarded-Proto`, but only from this machine. For a proxy elsewhere, e.g. in
  another container or on another machine at home, name its address:
  `"proxy": {"headers": "x-forwarded", "trusted_proxies": ["172.30.0.2"]}`. It must be an
  address at home or on this machine, because the proxy passes requests on over plain http.
- The proxy must connect to Share's http port, not its https port, and keep the visitor's `Host`.
- `share check` says which proxy Share trusts. A request from the proxy that doesn't name the
  visitor gets the error `proxy_headers`; one through a proxy Share wasn't told about gets
  `proxy_untrusted`. In both cases the log says how to fix it.
- The proxy's own health checks go to `/healthz`, which answers without those headers.

## Caddy

Caddy gets and renews the certificates by itself, sends both headers, and keeps the `Host`.
[`Caddyfile`](Caddyfile), for Share and another app:

```
share.example.com {
	reverse_proxy 127.0.0.1:8080
}

other.example.com {
	reverse_proxy 127.0.0.1:3000
}
```

Point each name's DNS record at the server, then `sudo systemctl reload caddy`.

## nginx

nginx needs to be told what Caddy does by itself: keep the `Host`, name the visitor, let uploads
through (its default limit is 1 MB, Share's uploads come in pieces of 20 MiB) and pass them on as
they arrive. [`nginx.conf`](nginx.conf) has a server block with all of that. The certificate
comes from certbot: `sudo certbot --nginx -d share.example.com`.

## Traefik

Traefik keeps the `Host` and sends both headers. In Docker, Share's container gets labels like:

```yaml
    labels:
      - traefik.enable=true
      - traefik.http.routers.share.rule=Host(`share.example.com`)
      - traefik.http.routers.share.entrypoints=websecure
      - traefik.http.routers.share.tls.certresolver=letsencrypt
      - traefik.http.services.share.loadbalancer.server.port=8080
```

Give Traefik a fixed address on its network, as Caddy has in
[the Docker setup](../docker/caddy/compose.yaml), and name it in `trusted_proxies`.

## Cloudflare in front as well

Set the name's DNS record at Cloudflare to *DNS only*. Through Cloudflare's proxy,
`X-Forwarded-For` would name Cloudflare's servers instead of the visitors, and uploads would meet
its 100 MB limit.

## Checking

```sh
curl -s https://share.example.com/healthz
curl -sI https://share.example.com/ | grep -i -E 'strict-transport|content-security'
```

Then sign in on the website. The browser should keep a cookie named `__Host-share_session`. If it
keeps `share_session` instead, Share took the requests for plain http at home: check the proxy's
headers and Share's `proxy` setting.
