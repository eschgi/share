# Share — plan

Status: planning. Screens: [share-mockup.html](share-mockup.html) (numbers below refer to its screens).

Share is a self-hosted place to collect files. Anyone with a PIN sends photos, videos and documents
through a website, without an account. People with an account see and download everything in an
Android app, and admins run it from there. Nothing is family-specific: friends and anyone else with a
PIN or an invite use the same screens. The project will be open source, and the app will go on Google
Play later.

## Parts

- **Server**: one Go binary. It serves the website, the API, uploads and downloads, and keeps every
  file in a folder set in `config.json`. It runs on the GL.iNet router for now and maybe on a Hetzner VPS
  later, so it must not depend on anything router-specific.
- **Website** (PWA): for sending only (1–6). Uppy runs headless under our own screens: tus for uploads,
  Golden Retriever to survive a closed tab.
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
- **Signed-in users** send without a PIN (15).
- **No uploader names**: the website doesn't ask for one.

## Library

- Uploads go straight into the library, without a review step.
- Grouped by upload day, newest first (11).
- Downloads: photos and videos go into a "Share" album in the gallery, documents into Downloads; files
  already on the phone are skipped (13).

## Network

- **Two addresses per phone** (16). The public address is required and goes through Cloudflare. The
  local address is optional and is preferred whenever it answers. The app checks when the network changes
  and before each batch, with a short timeout. Without a local address everything uses the public one,
  e.g. on a VPS. Both addresses come with an invite; nothing changes on the router.
- **Is the local address really this server?** Many homes use 192.168.8.x, so the server uses its own
  self-signed certificate there, and the app pins that certificate's fingerprint, which it learns over
  the public address.
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

- Its own repository, `eschgi/share` (private for now), apart from `eschgi/home`, whose history contains
  TrueNAS API keys.
- No secrets in the repository: commit `config.example.json`, keep the real `config.json` out of git.
- Nothing hard-coded: addresses, storage folder, ports, languages and the optional Play link come from
  configuration.
- The server stays pure Go (no cgo), so it cross-compiles for the router and the VPS; for a database,
  e.g. SQLite through `modernc.org/sqlite`. CI builds release binaries for those platforms.
- Pick the license before the first outside contribution. For example, AGPL-3.0 keeps hosted forks
  open; Apache-2.0 or MIT allow the widest reuse.
