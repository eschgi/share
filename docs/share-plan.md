# Share — plan

Status: built; what works is in the README's status. Screens: [share-mockup.html](share-mockup.html)
(numbers below refer to its screens).

Share is a self-hosted place to collect files. Anyone with a PIN sends photos, videos and documents
through a website, without an account. People with an account see and download everything in an
Android app or on the website, and admins run it from either. Nothing is family-specific: friends
and anyone else with a PIN or an invite use the same screens. The project is open source, and the
app will go on Google Play later.

## Parts

- **Server**: one Go binary. It serves the website, the API, uploads and downloads, and keeps every
  file in a folder set in `config.json`. It runs on the GL.iNet router for now and maybe on a Hetzner VPS
  later, so it must not depend on anything router-specific.
- **Website** (PWA): for sending with a PIN (1–6), and for people with an account everything the app
  does (22–37). Uppy runs headless under our own screens: tus for uploads, Golden Retriever to survive
  a closed tab. Phones get one column and the app's bars; tablets and computers a card in the middle,
  or two panes from 1024 points wide, and the account's pages a header with Library, Send and
  Settings. Computers can drop files and folders, and an invite opened there shows a QR code for the
  phone, or signs in the browser.
- **App** (Flutter, Android): library, bulk download, sending, and the admin screens (7–21).

## Access

- **Upload PINs**: 5 letters or digits, not case-sensitive, without look-alikes such as 0/O and 1/I.
  Either permanent or valid for 24 hours; no names, the code is the label (18, 19). A PIN only allows
  sending. The link you share can carry the PIN (`…/#K7M2Q`). After 5 wrong tries, a device waits
  10 minutes.
- **Accounts**: the roles are admin and member. People join with username and password, or with a
  one-time invite (QR code or link, valid 24 hours) that needs no password (10, 20). Each phone gets its
  own key, which an admin can revoke.
- **Admins** delete files (kept 30 days in Recently deleted), manage PINs, and invite, remove or promote
  people.
- **Signed-in users** send without a PIN (15, 29).
- **Browsers** sign in like phones, with the password or an invite (22, 23), and keep their key in a
  cookie the page can't read. One that signs in at home, over plain http, gets a session that works
  only at home: a cookie belongs to an address, and someone else's network has the same addresses.
- **No uploader names**: the website doesn't ask for one.

## Library

- Uploads go straight into the library, without a review step.
- Grouped by upload day, newest first (11).
- Downloads: photos and videos go into a "Share" album in the gallery, documents into Downloads; files
  already on the phone are skipped (13).
- In a browser (24–28): one file downloads as it is; several as one ZIP, stored without compression
  and laid out before the files are read, so its size is exact and the browser resumes it. Chrome
  and Edge on a computer can save them into a folder instead, a folder per day, skipping files that
  are there already (26, 27).

## Network

- **Two addresses per phone** (16). The public address is required and goes through Cloudflare. The
  local address is optional and is preferred whenever it answers. The app checks when the network changes
  and before each batch, with a short timeout. Without a local address everything uses the public one,
  e.g. on a VPS. Both addresses come with an invite; nothing changes on the router.
- **Is the local address really this server?** Many homes use 192.168.8.x, so over https the server uses
  its own self-signed certificate there, and the app pins that certificate's fingerprint, which it learns
  over the public address. Over plain http there is no certificate: before the app sends its key there,
  the server proves it keeps the hash of that key, with an HMAC of a nonce the app picked. It gives that
  proof only to the home network, so it can't be fetched through the tunnel and passed on.
- **Plain http only at home.** The server speaks plain http to home networks and to itself, so trying
  Share needs no certificate; from anywhere else pages go to the public https address and the API turns
  requests away.
- **The website always uses the public address.** Browsers don't let a public page switch to a local one.
- **Uploads** use tus in chunks below Cloudflare's 100 MB request limit (Free and Pro plans), e.g. 50 MB.
  A running upload can switch between the two addresses, because both reach the same tus upload.
- **Downloads** have no size limit through Cloudflare. They resume with HTTP range requests.
- **Cloudflare's terms** want video and other large files served through its paid products, not the
  normal proxy. Downloads over the local address avoid it at home. On a VPS, Cloudflare can be DNS-only,
  which also removes the 100 MB limit.

## Languages

German, Italian and English. The website picks the browser's language and has a switch on its first
screens; the app follows the phone and can be changed in settings. The strings live in files (ARB in the
app, JSON on the website), so others can add languages.

## Getting the app

- **Now**: the server offers the APK itself (9). Invite links open that page when the app isn't
  installed.
- **Later on Google Play**, and planned for from the start:
  - **Two build flavors.** `direct` updates itself from the server. `play` leaves updates to Play,
    because Play forbids apps that update themselves any other way. Only `direct` asks for permission
    to install packages.
  - **One signing key and a final application ID from day one**, and Play App Signing set up with that
    same key. Then the APK from the server and the Play build can replace each other without
    uninstalling.
  - **Links work with anyone's server.** Invite and PIN links are ordinary https links to the server's
    own page. That page hands over to the app with an Android intent link ("Already installed? Join",
    9), and the app can scan the same QR code itself (7, 8). Verified App Links would only work for
    domains built into the app, which doesn't fit other people's servers.
  - **No broad media permissions.** Picking uses the system photo picker; Play restricts the
    `READ_MEDIA_*` permissions to apps that need them for their core function. Saving into the gallery
    and Downloads needs no permission.
  - **Background transfers** with WorkManager, and user-initiated data transfer jobs on Android 14 and
    later.
  - **Store requirements**: in-app account deletion (everyone can delete their own account in
    settings), a privacy policy, the Data safety form, and for Play review a working demo server with a
    test login.
  - **Store title**: it can be longer than the name under the icon, which stays "Share".

## Open source

- Its own public repository, `eschgi/share`.
- No secrets in the repository: commit `config.example.json`, keep the real `config.json` out of git.
- Nothing hard-coded: addresses, storage folder, ports, languages and the optional Play link come from
  configuration.
- The server stays pure Go (no cgo), so it cross-compiles for the router and the VPS; for a database,
  e.g. SQLite through `modernc.org/sqlite`. CI builds release binaries for those platforms.
- Apache-2.0 ([`LICENSE`](../LICENSE)): anyone may use, change and host it, also commercially, as long
  as they keep the notices. The fonts and icons keep their own licenses, listed in
  [`NOTICE`](../NOTICE).
