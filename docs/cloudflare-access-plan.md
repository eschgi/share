# Cloudflare Access in the app — plan

Cloudflare Access can stand in front of a server and let through only who it knows. Browsers get
past it by signing in to Cloudflare, with an email code for example. The app can't do that, so it
shows Cloudflare a **service token** instead: a Client ID and a Client Secret, sent as the headers
`CF-Access-Client-Id` and `CF-Access-Client-Secret` with every request. Proposed on 2026-10-11,
from the user's screenshot of another app; the screens are 111 to 119 of
[share-mockup.html](share-mockup.html). Nothing is built yet.

Asked by the user:
- **Configure Cloudflare Access in the app**, as in the screenshot: a switch, the Client ID and
  the Client Secret.
- **Optional:** off unless the server needs it.
- **More in the settings**, and **in Share's own style**.

## What changed from the screenshot

- **Share's look:** its colours, fields and switch rows, not the other app's lime and navy.
- **Names from Cloudflare's dashboard,** "Client ID" and "Client secret", rather than the header
  names. The secret is hidden, with the eye to show it.
- **Paste from Cloudflare** (112) fills both fields at once. When a token is made, Cloudflare
  copies both headers together; the app reads them from the clipboard.
- **Found by itself** (113): when Cloudflare's login comes back instead of Share, the app says
  that Cloudflare Access is in the way, instead of showing a vague error. Most people never see
  the switch.
- **Checked right away:** Continue (112) and Check again (114) ask Cloudflare whether the token
  works.
- **Passed on with the invite** (117, 118). An admin's phone that has a token puts it into the
  invite's QR code, so nobody else types it. This is on by default, and only in the QR code: a
  link sent by message would leave the secret in the chat.
- **When the token stops working** (116), because it expired or was revoked in Cloudflare, the
  library says so in a banner that leads to the fix. Nothing pops up.

## How it works

- **Kept on the phone, with the server's addresses.** It goes into the `ServerConfig` that Dart
  and Kotlin share, with the secret encrypted by the Android Keystore and kept out of backups. The
  server never sees it or stores it. It belongs to the phone, not the account, so signing out
  keeps it.
- **Sent only to the public address.** Dart's API client, Kotlin's `ServerConnection`, the
  uploads and downloads, and the thumbnails and photos the app loads all add the two headers. They
  never go to the local address, which doesn't pass through Cloudflare, or to an S3 bucket.
- **Spotting Cloudflare Access:** the server's answer is Cloudflare's instead of Share's JSON: a
  redirect to `<team>.cloudflareaccess.com`, or a 403 page from Cloudflare. With no token, the
  app shows 113; with a token, 116. **To check on a real setup before building:** exactly what
  Cloudflare answers to the app with no token, and with a wrong or expired one.
- **In the invite:** the QR code's link carries the token after the `#`, which never reaches a
  server: `<server>/join#shi_…&cfa=<id>:<secret>`. The app reads it when it scans the code;
  a browser ignores it.
- **The website stays almost the same.** Browsers handle Cloudflare Access themselves. The one
  change: an invite made on a computer can't bring the token, because neither the browser nor the
  server knows it, and the dialog says so (119). Decided with the user on 2026-10-11: the server
  doesn't keep the token, so invites with it come only from an admin's phone. The website shows
  the line only if the server was told that Cloudflare Access is on; how it learns that is still
  open, for example a flag the admin's app sets, without the token itself.

## For whoever sets up Cloudflare

- Cloudflare Zero Trust → Access → Service Tokens: make a token, and add a **Service Auth**
  policy for it to the server's Access application.
- Guests open PIN links and invite pages in a browser. Without a Cloudflare login of their own,
  they need a **Bypass** policy for those pages. Android also needs to reach
  `/.well-known/assetlinks.json`, so that invite links open the app.
- Service tokens expire, after a year by default. A new token means new invites, or entering it
  again on each phone (116).
