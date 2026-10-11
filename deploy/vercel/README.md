# Share on Vercel

Vercel runs Share when someone visits and stops it when nobody does, as Cloud Run does. It keeps
nothing on disk, so Share keeps its files in an S3 bucket and its records in PostgreSQL. Vercel
builds Share from this repository itself, with the website inside, and takes the settings from
environment variables instead of `config.json`. Vercel's Go support is in beta, on every plan,
the free Hobby plan too; check the current terms of each service.

| Part | For example | Holds |
|------|-------------|-------|
| Vercel | a project from your copy of this repository, its functions in Frankfurt (`fra1`) | Share itself |
| PostgreSQL | a free [Neon](https://neon.tech) project in AWS `eu-central-1` (Frankfurt) | names, folders, days, people, PINs |
| A bucket | Cloudflare R2 or Backblaze B2 | the files and their thumbnails |

Put Vercel's functions and the database in the same city: every request asks the database a
few times.

## Setting up

1. **The bucket**, as in [Files in an S3 bucket](../../README.md#files-in-an-s3-bucket): its
   CORS rules for your public address, a lifecycle rule, a key for this bucket only.
2. **The database:** a Neon project on PostgreSQL 18, the default for new ones, and its direct
   connection string, not the pooled one (with `-pooler` in the host). Neon from Vercel's
   Marketplace works too: its direct string is the one in `DATABASE_URL_UNPOOLED`.
3. **The project:** on vercel.com, *Add New… → Project*, import your copy of this repository,
   and set *Root Directory* to `server`. [`server/vercel.json`](../../server/vercel.json) does
   the rest: the Go preset, and the build, which makes the website and then the server
   ([`scripts/build-vercel.sh`](../../scripts/build-vercel.sh)). It reads `web/` and `scripts/`
   next to `server/`, which Vercel allows unless *Include files outside the root directory* was
   switched off.
4. **The settings:** under *Environment Variables*, paste
   [`share.env.example`](share.env.example) with your own values: the public address, the
   connection string, the bucket. Tick *Production* only (below, *Previews*). Each variable is
   a setting of `config.json`, as in [Configuration](../../README.md#configuration).
5. **The region:** under *Settings → Functions*, the region next to the database, such as
   Frankfurt (`fra1`). Vercel starts with Washington, D.C. (`iad1`).
6. **Deploy.** Vercel builds and starts Share; its *Logs* show the start. Then add your domain
   under *Settings → Domains*, the same as `SHARE_PUBLIC_URL`.
7. **The first admin:** from your computer, with the release's `share` program and the same
   variables in a file `share.env`:

   ```sh
   set -a; . ./share.env; set +a
   share check
   share invite --admin --name YOURNAME
   ```

   `share check` says whether the database and the bucket answer, and prints the CORS rules if
   the bucket still needs them. The invite's link signs you in. The setup page works too, within
   15 minutes of a start, but Vercel starts Share so often that the invite is the safer way.

## Good to know

- **`SHARE_PROXY=x-forwarded`:** Vercel names the visitor in `X-Forwarded-For`, which it writes
  itself, whatever the visitor sent. `x-forwarded` believes it from a proxy on the same machine.
  Should the website say that a request *came through a proxy that Share doesn't trust*, Vercel
  connected from elsewhere, and the log names the address; then set
  `SHARE_PROXY={"headers": "x-forwarded", "trusted_proxies": ["THAT-ADDRESS"]}`.
- **Copies of Share:** Vercel may run more than one copy at a time, and starts fresh ones often.
  The limits on wrong PINs and passwords live in each copy's memory, so each copy counts on its
  own, and a fresh copy starts at zero: they hold back a guesser less than on a server of your
  own. Everything else is in the database, and copies that start together take turns updating
  it.
- **Once a day,** around 04:00 UTC, Vercel's cron job (in `vercel.json`) wakes Share, so that it
  empties Recently deleted and forgets old sessions even when nobody visited. Share does this
  housekeeping, and the thumbnails for photos that came without one, while a copy runs; Vercel
  may pause a copy between visits, and what was left over is done on a later one.
- **Previews:** Vercel builds every branch and pull request as a preview. With the variables on
  *Production* only, previews find no settings and stop, saying so. A preview with the production variables would use the same database and bucket, and
  update the database to a version that isn't released yet; give previews a database and a
  bucket of their own if you want them to run.
- **Updating:** every push to the production branch (`main`) deploys. Share brings the database
  up to date when it starts. The About screens show the version with Vercel's commit, such as
  `0.1.0-dev+223d776`.
- **Sizes:** Vercel takes requests of up to 4.5 MB. Browsers and the app send files straight to
  the bucket, so only small requests reach Share.
- **Never put Cloudflare's proxy in front** (the orange cloud): Share would see Cloudflare's
  addresses instead of your visitors', and they would share one limit on wrong PINs. A DNS-only
  record pointing at Vercel is fine.
- **Backups:** the bucket holds the files, the database everything else; Neon keeps a history to
  go back to. Back up both the way their services offer.
