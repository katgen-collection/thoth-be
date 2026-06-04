# Thothai — Go backend

Go rewrite of the Thothai backend, following `docs/Implementation Plan.md`. One
binary, two systemd units (`--mode=serve` and `--mode=worker`), plus a `migrate`
mode for applying the schema.

## Status

| Phase | Scope | State |
|-------|-------|-------|
| 0 | Project setup & infra (config, pgx pool, migrations, auth middleware, health, asynq, CORS) | ✅ done |
| 1 | AI chat core (conversations CRUD, DeepSeek streaming, tool-calling loop) | ✅ done |
| 2 | Job search as async pipeline + SSE progress + history; `search_jobs` wired into chat | ✅ done |
| 3 | CV management: S3 storage, upload validation, pdf-parser, async parse, CV tools | ✅ done (Step 7 deploy polish pending) |
| 4 | Job workflow: saved-jobs tracker, SSRF-guarded URL analysis, interview prep | ✅ done |
| 5 | CV tailoring (`suggest_cv_edits`) | ✅ done |
| 3.5 | Antivirus scanning (`Scanner` pluggable, ships as `noop`) | ⬜ post-MVP |
| 7 | Deploy/ops polish (CI build→SCP→systemd) | ⬜ deferred |

All six build phases (0–5) are implemented. Phases 0 and 2 are a 1:1 port of the
Python scaffold in `../backend/`; Phases 1, 3, 4, 5 are new. Remaining work is
Phase 3.5 (AV) and Step 7 (deploy automation).

The SSRF guard (`internal/infra/fetch`) is now live behind `analyze_job_url` /
`POST /api/v1/jobs/analyze-url`.

### Phase 3 security (implemented)
- **Storage:** S3-compatible behind `internal/infra/storage` (`Storage` interface
  + `S3` impl; one binary targets Supabase/R2/S3 via `STORAGE_ENDPOINT`). Private
  bucket, opaque keys `cvs/{user_id}/{id}.pdf`, presigned-GET downloads with
  `Content-Disposition: attachment`.
- **Ingress validation** (`internal/infra/upload`): size cap before/while reading,
  magic-byte `%PDF-` check, encrypted-PDF heuristic, filename sanitization, SHA-256.
- **SSRF guard** (`internal/infra/fetch`): dialer-Control IP validation (blocks
  private/reserved incl. `169.254.169.254`), scheme allowlist, redirect re-check,
  size cap. Ready for portfolio-link / `analyze_job_url` use in Phase 4.
- **Parser sandbox:** `deploy/pdf-parser.service` hardened (`MemoryMax`, etc.).
- **Prompt injection:** CV/job text delimited as untrusted data in all CV prompts.
- **Quota:** `MAX_UPLOADS_PER_USER`. **AV:** deferred to Phase 3.5.

## Layout

```
cmd/thothai/main.go              # entry point, --mode=serve|worker|migrate
internal/
  config/                        # env-var config
  domain/search/                 # search job model, repository, service (pipeline)
  infra/
    db/                          # pgx pool + migration runner
    queue/                       # asynq client/server + task types
    redis/                       # go-redis client + pub/sub channel naming
    ai/deepseek.go               # DeepSeek param-extract + job-filter (OpenAI-compatible)
    serpapi/                     # Google Jobs HTTP client
    middleware/auth.go           # X-User-* header reader
  http/
    handlers/                    # health, search (+ SSE), history
    routes.go                    # Fiber app assembly
worker/tasks/search.go           # asynq handler -> search.Service.RunPipeline
migrations/001_initial.sql       # all 5 tables
deploy/                          # systemd units
```

## Running locally

Requires PostgreSQL and Redis reachable per `.env`.

```bash
cp .env.example .env          # fill in DEEPSEEK_API_KEY, SERPAPI_KEY, DATABASE_URL, REDIS_URL
export $(grep -v '^#' .env | xargs)   # or use systemd EnvironmentFile on the VPS

go run ./cmd/thothai --mode=migrate   # apply schema (idempotent)
go run ./cmd/thothai --mode=serve     # HTTP API on 127.0.0.1:8000
go run ./cmd/thothai --mode=worker    # asynq consumer (separate process)
```

Build a release binary:

```bash
go build -ldflags "-s -w" -o build/thothai ./cmd/thothai
```

## API (Phase 0 + 1 + 2)

All `/api/v1/*` routes expect `X-User-*` headers injected by api-gateway.

```
GET  /health                          # liveness, no auth

# Chat (Phase 1)
POST   /api/v1/chat/conversations              # { "title"?: "..." } -> conversation
GET    /api/v1/chat/conversations              # list user conversations
GET    /api/v1/chat/conversations/:id          # conversation + messages
DELETE /api/v1/chat/conversations/:id          # delete (messages cascade)
POST   /api/v1/chat/conversations/:id/messages # { "content": "..." } -> SSE stream

# Search (Phase 2)
POST /api/v1/search/initiate          # { "text": "..." } -> { "task_id": "..." }
GET  /api/v1/search/stream/:task_id   # SSE pipeline progress
GET  /api/v1/search/results/:task_id  # completed result from DB
GET  /api/v1/search/history           # ?limit&offset
GET  /api/v1/history                  # alias -> /api/v1/search/history

# CV management (Phase 3)
POST   /api/v1/cvs                    # multipart upload (field "file"), PDF only
GET    /api/v1/cvs                    # list user's CVs
GET    /api/v1/cvs/:id                # CV details + parsed_data
DELETE /api/v1/cvs/:id                # delete row + stored object
PATCH  /api/v1/cvs/:id/default        # set as default CV
POST   /api/v1/cvs/:id/analyze        # (re)run extraction pipeline
GET    /api/v1/cvs/:id/download       # 307 redirect to presigned URL
POST   /api/v1/cvs/match              # { cv_id?, job_description } -> fit score
POST   /api/v1/cvs/:id/cover-letter   # { job_description } -> cover letter
POST   /api/v1/cvs/:id/suggest-edits  # { job_description } -> tailoring suggestions

# Job workflow (Phase 4)
POST   /api/v1/jobs/save                       # save a job to the tracker
GET    /api/v1/jobs/saved                       # list saved jobs (?status filter)
PATCH  /api/v1/jobs/saved/:id/status            # { status } update tracker stage
DELETE /api/v1/jobs/saved/:id                   # remove saved job
POST   /api/v1/jobs/analyze-url                 # { url } SSRF-guarded fetch + analysis
POST   /api/v1/jobs/saved/:id/interview-prep    # generate interview Q&A
```

### Chat tools (all wired into the chat tool-calling loop)
`search_jobs` · `analyze_cv` · `match_cv_to_job` · `generate_cover_letter` ·
`suggest_cv_edits` · `save_job` · `get_saved_jobs` · `analyze_job_url` ·
`prep_interview` — 9 tools.

### Chat SSE event stream

`POST .../messages` streams these frames (one JSON object per `data:` line):

```
data: {"type":"text","content":"Oke, saya cariin dulu ya..."}
data: {"type":"tool_call","tool":"search_jobs","args":{"query":"backend engineer remote"}}
data: {"type":"tool_progress","status":"extracting","progress":10}
data: {"type":"tool_progress","status":"fetching_jobs","progress":40}
data: {"type":"tool_progress","status":"ai_filtering","progress":70}
data: {"type":"tool_result","tool":"search_jobs","summary":"Ditemukan 8 posisi relevan"}
data: {"type":"text","content":"Ini 8 posisi yang saya temukan..."}
data: {"type":"done"}
```

The chat loop sends history + tool schemas to DeepSeek, streams text deltas, and
when the model requests a tool it executes it (streaming `tool_progress`), feeds
the result back, and loops until the model produces a final answer (max 5 tool
iterations). The `search_jobs` tool runs the Phase 2 pipeline **synchronously
in-process** (`search.RunForChat`) — no worker/Redis dependency — while still
persisting the `search_jobs` row so it shows in search history.

### Standalone search SSE

`GET /search/stream/:task_id` subscribes to the Redis channel
`task_stream_<task_id>`, which the **worker** publishes to at each pipeline step
— identical convention to the Python scaffold. This path requires the worker
process and Redis pub/sub; the chat tool path does not.

## Cloud infra notes

Configured for managed services (keeps the VPS light):

- **Supabase (Postgres):** `DATABASE_URL` must include `?sslmode=require`. Use
  the pooler/session connection URI.
- **Upstash (Redis):** use the `rediss://` (TLS) URL. `redis.ParseURL` and
  `asynq.ParseRedisURI` both honor the `rediss` scheme and enable TLS.
  - ⚠️ Verify asynq works on your Upstash plan (it relies on Lua scripts and
    blocking list ops). If not, the **chat `search_jobs` path still works**
    (it bypasses asynq); only the standalone `/search/initiate` async endpoint
    needs a fully asynq-compatible Redis.

`.env` currently holds **placeholders** — fill in real credentials as infra
comes online.
