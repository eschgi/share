# Share — plan

Status: built; the README says what it does, [s3-plan.md](s3-plan.md) how the files can be in an
S3 bucket, and [e2ee-plan.md](e2ee-plan.md) how a folder is encrypted end to end. Screens:
[share-mockup.html](share-mockup.html) (numbers below refer to its screens).

Share is a self-hosted place to collect files. Anyone with a PIN sends photos, videos and documents
through a website, without an account. People with an account see and download the folders they
were given in an Android app or on the website, and admins run it from either. Nothing is family-specific: friends
and anyone else with a PIN or an invite use the same screens. The project is open source, and the
app will go on Google Play later.

## Parts

- **Server**: one Go binary. It serves the website, the API, uploads and downloads, and keeps every
  file in one of its folders, each a directory inside the storage folder set in `config.json`. Or
  the files are in an S3 bucket, and the website and the app send to it and fetch from it directly,
  with links the server signs. It runs wherever its owner wants: a small computer at home, a VPS,
  or a container. So it must not depend on anything machine-specific, and it works behind a tunnel,
  behind a reverse proxy, or on its own.
- **Website** (PWA): for sending with a PIN (1–6), and for people with an account everything the app
  does (22–37). Uppy runs headless under our own screens: tus for uploads, or with a bucket our own
  plugin that sends the parts there, and Golden Retriever to survive a closed tab. Phones get one
  column and the app's bars; tablets and computers a card in the middle, or two panes from 1024
  points wide, and the account's pages a header with Library, Send and Settings. Computers can drop
  files and folders, and an invite opened there shows a QR code for the phone, or signs in the
  browser.
- **App** (Flutter, Android): library, bulk download, sending, and the admin screens (7–21).

## Access

- **Upload PINs**: 5 letters or digits, not case-sensitive. An admin may choose any, such as ANNA1;
  the ones suggested leave out look-alikes such as 0/O and 1/I.
  Either permanent or valid for 24 hours; no names, the code is the label (18, 19). A PIN sends into
  one folder (43, 45); only a PIN made to show its folder lets guests see it too (46). The link you share can carry the PIN (`…/#K7M2Q`). After 5 wrong tries, a device waits
  10 minutes.
- **Printing a PIN** (69–72): a permanent PIN prints as a poster (one A4 page) or as four table cards
  to cut out, with a title, a line, the date and the code to type, in the server's language
  (`default_language`). The app draws the page and hands it to Android's printing; the website shows
  a preview beside the form and uses the browser's print dialog. Both save a PDF too.
- **Accounts**: the roles are admin and member. People join with username and password, or with a
  one-time invite (QR code or link, valid 24 hours) that needs no password (10, 20). Each phone gets its
  own key, which an admin can revoke.
- **Admins** delete files (kept 30 days in Recently deleted), manage PINs, and invite, remove or promote
  people, or give someone who forgot a password a new one.
- **Signed-in users** send without a PIN (15, 29), into a folder they see (48).
- **Browsers** sign in like phones, with the password or an invite (22, 23), and keep their key in a
  cookie the page can't read. One that signs in at home, over plain http, gets a session that works
  only at home: a cookie belongs to an address, and someone else's network has the same addresses.
- **No uploader names**: the website doesn't ask for one.
- **The first admin** makes their account on the setup page, which a new server shows while nobody
  has an account: from home at any time, from elsewhere in the first 15 minutes after a start, or
  with the link in the server's log. On a drive, the page sets up the storage folder first.

## Folders

- **Every file lies in exactly one folder**, a real directory on the drive:
  `<storage>/Wedding Anna & Marco/2026-09-26/IMG_0001.jpg`. The database keeps each file's path
  within its folder, so renaming a folder renames one directory and changes one row.
- **In a bucket** every file is one object named after its id, and its folder, day and name are
  only rows in the database: moving, renaming, deleting and restoring change nothing in the bucket,
  and only deleting for good removes the object.
- **Who sees what**: admins see every folder; members see the folders they were given, with a switch
  per person (41, 42) and per invite (47). Nothing tells members about the others.
- **The library** shows one folder or all of them, and the choice stays on the phone or in the
  browser (38, 39, 44). Sending goes into the folder open in the library, or another one (48); files
  shared from other apps wait for the choice when there is one.
- **Admins** create, rename and delete folders (40, 41) and move files between them (49). A deleted
  folder's files go to Recently deleted, its PINs end, and restoring one of its files brings it back.
  The last folder can't be deleted.
- **PINs** send into one folder (43, 45). "Guests also see this folder" (43) lets everyone with the
  PIN see and download what is in it, without names and without deleting anything (46).
- **One folder looks like none**: the folder's name, the choices and the folder column appear only
  with the second folder.
- **Encrypted folders** (e2ee-plan.md): an admin turns end-to-end encryption on per folder, or for
  new folders by default. Their new files and thumbnails are encrypted on the sending phone or
  browser, and open only on the devices of the people who see the folder; names, days and sizes
  stay readable to the server. Keys reach new devices and people from whoever is online with them,
  with the password, through invite and PIN links, or with the admins' recovery code.
- **Upgrading** puts everything that was there into a first folder named after the server, and
  moves the day folders into its directory; the moves resume after an interruption. The clients name
  the folders of every upload, PIN and invite, and the server refuses those that don't (API
  version 2).

## Library

- Uploads go straight into the library, without a review step.
- Grouped by upload day, newest first (11), in one folder or all of them.
- Downloads: photos and videos go into a "Share" album in the gallery, documents into Downloads; files
  already on the phone are skipped (13).
- In a browser (24–28): one file downloads as it is; several as one ZIP, stored without compression
  and laid out before the files are read, so its size is exact and the browser resumes it. Chrome
  and Edge on a computer can save them into a folder instead, a folder per day, skipping files that
  are there already (26, 27). With a bucket there is no ZIP: several files download one by one,
  each straight from the bucket.

## Network

- **How visitors reach it.** At home without a public address, through a Cloudflare Tunnel; on a server
  with one, behind a reverse proxy such as Caddy, nginx or Traefik, or with Share's own HTTPS; on
  either, in Docker. Or on Google Cloud Run, with the files in a bucket and the records in
  PostgreSQL, so that nothing stays on a machine. `config.json` names the proxy (`proxy`), and Share believes a visitor's address and
  https only from it. A proxy Share wasn't told about is refused, so it can't make the internet look
  like the home network.
- **Two addresses per phone** (16). The public address is required. The local address is optional,
  for a server at home, and is preferred whenever it answers. The app checks when the network changes
  and before each batch, with a short timeout. A local address that didn't answer is checked again after
  half a minute, and right away when the public one doesn't answer either (e.g. not set up yet), so a
  moment without it, such as the server restarting, heals by itself. Without a local address everything
  uses the public one, e.g. on a VPS. Both addresses come with an invite; nothing needs to be opened in
  the firewall at home.
- **Is the local address really this server?** Many homes use the same 192.168.x addresses, so over https the server uses
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
  With a bucket, the parts go straight to it, each with a link the server signs, past Cloudflare
  and the server; which parts the bucket has always comes from the server, so an upload goes on
  wherever it stopped.
- **Downloads** have no size limit through Cloudflare. They resume with HTTP range requests. With a
  bucket they come from there, also at home, with links that work for 12 hours.
- **Cloudflare's terms** want video and other large files served through its paid products, not the
  normal proxy. Downloads over the local address avoid it at home. On a server with a public address,
  Cloudflare can be DNS-only in front of a reverse proxy, which also removes the 100 MB limit. With
  a bucket, the files don't pass its proxy at all.
- **Videos and sound play in the app.** A copy on the phone plays first. Otherwise the player streams
  with the phone's key: at home over the local address (plain http after the proof), away through the
  public address, which loads only what is watched rather than the whole file. Over the https port at
  home the file is fetched first, because the player can't pin Share's own certificate. With a
  bucket, the player streams a link from the server, without the phone's key.

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
  - **The camera only for scanning.** The QR code scanner is part of the app (CameraX and ZXing), so
    it works without Google Play services. It asks for the camera the first time someone scans, and
    its pictures never leave the phone.
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
- The server stays pure Go (no cgo), so it cross-compiles for Linux and Windows on arm64 and amd64; for
  the records, PostgreSQL 18 through `pgx`. CI builds release binaries for those platforms
  and a Docker image.
- Apache-2.0 ([`LICENSE`](../LICENSE)): anyone may use, change and host it, also commercially, as long
  as they keep the notices. The fonts and icons keep their own licenses, listed in
  [`NOTICE`](../NOTICE).
