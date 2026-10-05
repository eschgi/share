# Share on Google Cloud Run

Cloud Run runs Share's image when someone visits and stops it when nobody does. It keeps nothing
on disk, so Share keeps its files in an S3 bucket and its records in PostgreSQL, and nothing on
the machine. All three have free allowances that a family usually stays within; check the
current terms of each.

| Part | For example | Holds |
|------|-------------|-------|
| Cloud Run | a service in `europe-west3` (Frankfurt) | Share itself, one instance |
| PostgreSQL | a free [Neon](https://neon.tech) project in AWS `eu-central-1` (Frankfurt) | names, folders, days, people, PINs |
| A bucket | Cloudflare R2 or Backblaze B2 | the files and their thumbnails |

Put Cloud Run and the database in the same city: every request asks the database a few times.

## Setting up

1. **The bucket**, as in [Files in an S3 bucket](../../README.md#files-in-an-s3-bucket): its
   CORS rules for your public address, a lifecycle rule, a key for this bucket only.
2. **The database:** a Neon project, and its connection string, the direct one rather than the
   pooled one. It ends in `sslmode=require`, which Share requires for a database on the internet.
3. **`config.json`** from [`config.example.json`](config.example.json): the public address, the
   connection string, the bucket. `data_dir` stays out: nothing is kept on the machine. The proxy
   line trusts Cloud Run's front end, which connects from `169.254.x.x`.
4. **Check it from your computer**, with the release's `share` program or the Docker image and
   the same `config.json`: `share check` says whether the database and the bucket answer, and
   prints the CORS rules if the bucket still needs them.
5. **The first admin, also from your computer:** `share invite --admin --name YOURNAME` prints
   the invite link. (The link a new server prints into its log is replaced at every start, and
   Cloud Run starts often.)

## Deploying

Cloud Run takes images from Google's Artifact Registry; a remote repository there passes on
`ghcr.io/eschgi/share`:

```sh
gcloud artifacts repositories create ghcr --location=europe-west3 \
  --repository-format=docker --mode=remote-repository --remote-docker-repo=https://ghcr.io
gcloud secrets create share-config --data-file=config.json
gcloud run deploy share \
  --image=europe-west3-docker.pkg.dev/PROJECT/ghcr/eschgi/share:VERSION \
  --region=europe-west3 --allow-unauthenticated --port=8080 \
  --min-instances=0 --max-instances=1 \
  --cpu=1 --memory=1Gi --no-cpu-throttling --cpu-boost \
  --set-secrets=/config/config.json=share-config:latest
```

`PROJECT` is your Google Cloud project, `VERSION` the release, such as `0.2.0`. Give the
service's account access to the secret (`roles/secretmanager.secretAccessor`) if
Cloud Run asks for it. Then open the service's address, or set your own: Cloud Run's domain
mapping where your region offers it.

- **`--max-instances=1`:** Share keeps some things in memory, such as the limits on wrong PINs,
  and a second instance would know nothing of them.
- **`--no-cpu-throttling`:** the CPU stays on while an instance runs, so the housekeeping that
  runs every few minutes, and the thumbnails the server makes for photos that came without one,
  aren't left halfway. Cloud
  Run then bills the instance's whole life, and it still stops when nobody visits.
- **1 GiB of memory:** making a thumbnail of a big photo takes up to 128 MiB, and Go needs room
  around it.
- The start waits for the database (a paused Neon database wakes on the first connection), does
  what was left over, and only then opens the port; Cloud Run's default startup probe waits for
  that.

**Once a day,** a Cloud Scheduler job can wake Share, so that it empties Recently deleted and
forgets old sessions even when nobody visited:

```sh
gcloud scheduler jobs create http share-daily --location=europe-west3 \
  --schedule="0 4 * * *" --uri=https://SERVICE-ADDRESS/healthz --http-method=GET
```

## Updating

Deploy the new version's image the same way. Share brings the database up to date when it
starts.

## Good to know

- **Never put Cloudflare's proxy in front** (the orange cloud): Share would see Cloudflare's
  addresses instead of your visitors', and they would share one limit on wrong PINs. A DNS-only
  record is fine.
- **Check once that Share tells your visitors apart:** enter a wrong PIN five times on the phone
  over mobile data, then try the PIN page on the computer at home. If the computer is locked out
  too, every visitor looks the same to Share.
- **Backups:** the bucket holds the files, the database everything else; Neon keeps a history to
  go back to. Back up both the way their services offer.
- **Logs** are in Cloud Logging, under the service.
- **Nothing to move:** a server that keeps its files on a drive can't be moved to a bucket; a
  Cloud Run server starts empty.
