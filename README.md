# 📦 Pagepack

**Pack a website. Keep a copy.**

Mirror static sites with **wget**, zip them, store on **S3-compatible** storage, download via presigned links. Go API + React UI in one binary.

**v1.0.0** · [Changelog](CHANGELOG.md)

---

## ⚡ Quick start

**Needs:** Go 1.25+, Node 20+, **GNU wget**, **Valkey**, an **S3 bucket** (you create it).

```sh
cp .env.example .env   # set AWS_BUCKET, Valkey, credentials
npm ci && npm run build
go run .
```

→ [http://localhost:8000](http://localhost:8000)

`.env` loads automatically; exported env vars win. Secrets stay out of git.

---

## 🐳 Docker Compose

```sh
cp .env.example .env   # S3 + optional AWS keys
docker compose up --build -d
```

Starts **Pagepack + Valkey**. Image embeds the frontend; host only needs Docker.  
Change host port: `PAGEPACK_PORT=8080` in `.env`.

Set a **bucket lifecycle rule** on your archive prefix so old ZIPs expire (the app does not delete S3 objects when Valkey keys TTL out).

---

## 🔧 Config (essentials)

| Variable | Default | Notes |
| --- | --- | --- |
| `HTTP_ADDR` | `localhost:8000` | Listen address |
| `VALKEY_URL` | `valkey://localhost:6379` | Job state (`redis://` ok) |
| `AWS_BUCKET` | *required* | Private bucket |
| `AWS_REGION` | `us-east-1` | |
| `AWS_ENDPOINT` | — | MinIO / Spaces / etc. |
| `AWS_BUCKET_PATH` | `clones/` | Object prefix |
| `MAX_WEBSITE_BYTES` | `104857600` | 100 MiB download cap |
| `MAX_CONCURRENT_JOBS` | `4` | Extra jobs → **429** |
| `JOB_TIMEOUT` | `15m` | Per-job deadline |
| `TEMP_DIR` | OS temp | Scratch per job |

Full list: `.env.example` · validation: `pkg/config/config.go`

---

## 🌐 API

Base: `/api/v1` (JSON)

| Method | Path | What |
| --- | --- | --- |
| `POST` | `/jobs` | `{"url":"https://…"}` → **202** + job id |
| `GET` | `/jobs/{id}` | Progress or result + fresh download URL |
| `POST` | `/jobs/{id}/cancel` | Stop + cleanup |
| `GET` | `/config` | Public limits |
| `GET` | `/health` | Workers + Valkey (**503** if degraded) |

Legacy: `POST /clone`, `POST /status`, `GET /api/config`

**Stages:** `pending` → `cloning` → `zipping` → `uploading` → `done` (or `error` / `canceled`)

---

## 🛠 Dev

```sh
go run .              # API + embedded UI
npm run dev           # Vite on another terminal (proxies /api → :8000)
go test -race ./...
```

Rebuild frontend after UI changes: `npm run build`

---

## 📌 Notes

- **Static only** — no headless browser; SPAs need something else.
- **Jobs run in-process** on the server that queued them; cancel only works there.
- **History** lives in the browser (localStorage), not on the server.
- **Don’t expose raw to the internet** without auth — anyone who can reach it can trigger wget + S3 uploads.

Made for the static web 🌐
