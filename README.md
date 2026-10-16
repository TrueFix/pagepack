# Pagepack

**Pack a website. Keep a copy.**

A Go website archiver with Gorilla Mux, Valkey job state, S3-compatible ZIP storage,
and a React frontend built with Vite. Clones static pages and assets with GNU wget.

## Run

Requires Go 1.25+, Node.js 20.19+ (or 22.12+), GNU wget, a running Valkey instance,
and an existing S3 bucket. Build the frontend before starting Go.

```sh
git clone git@github.com:TrueFix/pagepack.git
cd pagepack
cp .env.example .env
# Edit .env with your Valkey and S3 settings before starting.
npm ci
npm run build
go run .
```

Open http://localhost:8000. All application settings are loaded and validated in
`pkg/config/config.go`. Pagepack automatically loads `.env` from the working directory;
exported environment variables take precedence. `.env.example` lists settings without
credentials, and `.env` is ignored by Git.

| Environment variable | Default | Purpose |
| --- | --- | --- |
| `HTTP_ADDR` | `localhost:8000` | HTTP listen address |
| `MAX_WEBSITE_BYTES` | `104857600` | Maximum total downloaded file bytes (100 MiB) |
| `MAX_CONCURRENT_JOBS` | `4` | Concurrent clone jobs per server process; excess requests return 429 |
| `JOB_TIMEOUT` | `15m` | Deadline for cloning, compression, and upload |
| `STATE_TTL` | `24h` | Job state lifetime, refreshed at each stage; must exceed the job timeout |
| `DOWNLOAD_TTL` | `15m` | Presigned link lifetime; maximum 7 days, credentials can expire earlier |
| `TEMP_DIR` | OS temp directory | Private per-job wget/compression scratch directories |
| `WGET_PATH` | `wget` | GNU wget executable |
| `WGET_TIMEOUT` | `10s` | Timeout for DNS lookup, connection, and read inactivity (minimum 1s) |
| `WGET_TRIES` | `1` | Total attempts per link, 1–10; one avoids waiting on repeated stalled requests |
| `VALKEY_URL` | `valkey://localhost:6379` | Valkey URL, including credentials and database index; `valkeys://` for TLS. `redis://` and `rediss://` are also accepted |
| `AWS_BUCKET` | required | Existing private archive bucket; `S3_BUCKET` is a fallback alias |
| `AWS_REGION` | `us-east-1` | S3 signing region |
| `AWS_ENDPOINT` | AWS default | Optional S3-compatible endpoint URL; `S3_ENDPOINT` is a fallback alias |
| `AWS_BUCKET_PATH` | `clones/` | Object key prefix; `S3_PREFIX` is a fallback alias |
| `S3_FORCE_PATH_STYLE` | `false` | Enable for providers requiring path-style addressing |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |
| `LOG_FORMAT` | `pretty` | `pretty` (coloured, human-readable), `json`, or `text` |

AWS authentication uses the official SDK default credential chain, including
`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and optional `AWS_SESSION_TOKEN`,
shared AWS profiles, and workload roles. No credentials are embedded in source.

## Temporary storage and retention

Wget needs a local filesystem while downloading and converting links. Each job
gets an isolated scratch directory that is removed on success, failure, or normal
shutdown. Only the finished ZIP is uploaded to S3; WeTransfer is no longer used.

Set `TEMP_DIR` in `.env` to choose where pages, assets, and ZIPs are downloaded:

```dotenv
TEMP_DIR=./tmp/pagepack
# Or use an absolute path on a larger disk:
# TEMP_DIR=/var/lib/pagepack/tmp
```

The example configuration uses `./tmp/pagepack`; without a setting, the app uses
the OS temp directory. Relative paths resolve from the server's working directory.
The server creates the directory automatically, then creates a private
`pagepack-*` subdirectory for each job. The directory must be writable by the
server user. `tmp/` is ignored by Git; use a dedicated directory for runtime data.

**Configure a bucket lifecycle rule for your `AWS_BUCKET_PATH` prefix** to delete temporary
archives after the desired retention period (for example, two days with the
default 24-hour state TTL). Valkey key expiry and presigned URL expiry do not delete
S3 objects. The application deliberately does not change bucket lifecycle rules.
The S3 identity needs `s3:PutObject`, `s3:GetObject`, and `s3:DeleteObject` on that prefix.

For DigitalOcean Spaces, set `AWS_ENDPOINT` to the regional endpoint (for example,
`https://blr1.digitaloceanspaces.com`), `AWS_REGION=blr1`, `AWS_BUCKET` to the Space
name, and `AWS_BUCKET_PATH` to a prefix such as `1site/pagepack`. Keep
`S3_FORCE_PATH_STYLE=false` for virtual-hosted Spaces URLs.

The size limit measures the total file size of downloaded pages and assets,
not the compressed ZIP. A 250 ms monitor cancels oversized downloads and a final
check rejects them before compression/upload. An in-progress download can briefly
overshoot the limit between checks; use filesystem/container quotas if a strict
scratch disk ceiling is required.

### Stalled or broken links

Each link defaults to one attempt with a 10-second DNS/connect/read-inactivity
timeout. If a linked page stops responding, wget gives up on it and continues
with the remaining links. Redirect chains are limited to ten hops per request.
Network failures no longer discard successfully downloaded pages: completed
files are archived with warnings, and any unfinished files are removed first.
If nothing was downloaded successfully, the job still fails with `network_error`.

Tune `WGET_TIMEOUT=5s` for faster skipping, or increase it for slow sites. This is
an inactivity timeout, not an absolute deadline per URL: a file that keeps sending
data can take longer. `JOB_TIMEOUT` remains the deadline for the entire job.
Timeout/network diagnostics are logged with the affected URL; skipped requests
are included in API counters and archive warnings.

Job execution is in-process, with Valkey storing state rather than acting as a
durable task queue. Graceful shutdown cancels jobs and records failures. A hard
process crash can leave an unfinished state until its TTL expires.

## Frontend

The frontend uses React 19, Vite, Tailwind CSS v4, and shadcn Tabs, Drawer, and Empty
components. Its daylight glass workbench and archive shelf are implemented in
`frontend/src/components/`; theme tokens and materials live in `frontend/app.css`.
Geist, Geist Mono, and Bricolage Grotesque are bundled locally.
Active archives refresh every two seconds, showing downloaded size, file count,
rate, compression progress, and upload progress. Finished archives show ZIP size,
file count, space saved, and elapsed time. Download totals are unknown while crawling:
`limit_percent` measures usage of the configured size limit, not completion.
The archive collection has All, In Progress, Ready, and Stopped
filters. Job history is kept exclusively in each browser’s local storage; there is
no shared collection or cross-device history. Valkey stores individual job states,
while S3 holds ZIP files. Clearing finished history removes local entries only.
Status requests refresh active jobs and download actions generate fresh presigned
links without persisting them in local storage.

```sh
npm ci
npm run build
go run .
```

Vite writes the production build to `dist/`. Go embeds that build into the binary
and serves the frontend and API on the same port. Re-run `npm run build` and restart
Go after changing frontend source. `dist/` is generated and ignored by Git; the build
must exist before `go run`, `go build`, or `go test ./...` because of Go embedding.

For frontend development, keep `go run .` running and use `npm run dev` in another
terminal. Vite proxies API requests to `http://localhost:8000`. Production does not
need a separate Node.js server.

## API

All endpoints return JSON. Version 1 lives under `/api/v1`.

| Method & path | Purpose |
| --- | --- |
| `POST /api/v1/jobs` `{"url":"https://example.com"}` | Start a job. `202` with the job and a `Location` header |
| `GET /api/v1/jobs/{id}` | Job state with live progress, or the result and a fresh download link |
| `POST /api/v1/jobs/{id}/cancel` | Stop an active job and clean up its files and attempted upload |
| `GET /api/v1/config` | Public limits (size, workers, TTLs, stages); no secrets |
| `GET /api/v1/health` | Worker usage, uptime, and job-state storage health (`503` when degraded) |

Stages: `pending` → `cloning` → `zipping` → `uploading` → `done`, or `error` / `canceled`.
Every job response includes `stage` (`label`, `step` of `steps`), a one-line
`summary`, `elapsed_ms`, and timestamps. Depending on the stage it also includes:

- **`progress`** (while running): `downloaded_bytes`/`downloaded` ("12.4 MB"),
  `files`, `bytes_per_second`/`rate`, `limit_percent` of the size limit,
  `requests_ok`, `requests_failed`, `last_url`, and `stage_percent` for
  zipping (`zipped_files`) and uploading (`uploaded_bytes`, `archive_size`).
- **`result`** (when done): `archive_bytes`/`archive_size`, `site_bytes`/`site_size`,
  `files`, `compression_ratio`, `space_saved_percent`, `sha256`, `filename`,
  `requests_failed`, `average_download_rate`, and `durations_ms`
  (`queue`, `download`, `zip`, `upload`, `total`).
- **`download`** (when done): presigned `url`, `filename`, `expires_at`, `expires_in_seconds`.
- **`error`** (when failed): `code`, `message`, and the `stage` it failed in.
  Codes: `too_large`, `timeout`, `network_error`, `tls_error`, `auth_required`,
  `server_error`, `empty_site`, `wget_failed`, `archive_failed`, `upload_failed`,
  `state_unavailable`, `canceled`, `cleanup_failed`, `internal_error`.
- **`warnings`**: for example, links that returned 404 and were skipped. A page with a
  few broken links still produces an archive.

Request errors share one shape: `{"status":"error","code":"busy","error":"…","msg":"…"}`
with codes `invalid_json`, `invalid_url`, `invalid_id`, `busy` (429 with `Retry-After`),
`not_found`, `method_not_allowed`, `state_unavailable`, and `download_link_unavailable`.

The original endpoints still work and return the same v1 shapes:
`POST /clone`, `POST /status` with `{"request_id":"…"}` (includes `download_url`),
and `GET /api/config`.

### Cancel a job

```sh
curl -X POST http://localhost:8000/api/v1/jobs/REQUEST_ID/cancel
```

No request body is required. Cancellation stops wget, compression, or the upload,
removes the private scratch directory (downloaded files and local ZIP), deletes any
attempted S3 upload, and releases the worker slot. A `200` response contains the
final job state: `canceled` if cleanup succeeded, or `error` with `cleanup_failed`
if cleanup failed. The job record is retained for history and diagnostics.

If the worker has not stopped within ten seconds, the endpoint returns `202` with
`status: "canceling"`; poll `GET /api/v1/jobs/{id}` until it finishes. Repeating the
request for a canceled job returns its final state. Completed/failed jobs return
`409 job_finished`, and jobs not running on this process return `409 job_not_running`.
Jobs execute in-process, so send cancellation to the process running the job.
Active archives also have a Cancel button; canceled entries appear under Stopped.

## Logs

Logs go to stderr. `LOG_FORMAT=pretty` (default) prints one coloured line per event,
e.g. `INF downloading  job=3d809ef3 downloaded=12.4 MB files=132 rate=1.1 MB/s limit=12%`.
Each job logs when it is queued and started, its download progress every few seconds,
then summaries for download, archive, upload, and completion or failure. Routine
requests (static files, status polling) are logged only at `LOG_LEVEL=debug`, which also
shows every URL wget fetches. Use `LOG_FORMAT=json` for log collectors. Colours turn off
automatically when stderr is not a terminal or `NO_COLOR` is set.

```sh
go test -race ./...
go vet ./...
```
