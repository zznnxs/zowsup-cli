# Zowsup-Go

> A pure-Go re-implementation of [`zowsup-cli`](../README.md), running entirely
> on `modernc.org/sqlite` (no CGO), with a Vite + React + Ant Design frontend.

## Status

This is **M0 — foundation only**. None of the WhatsApp commands are runnable yet.
M0 ships the project skeleton, the Noise XX handshake, the WhatsApp binary
XMPP node codec, the per-account proxy transport, the account manager
scaffolding, the modernc.org/sqlite schema and migrations, and the Vite
frontend skeleton. See [`ROADMAP.md`](./ROADMAP.md) for the milestone plan
and per-command completion status.

## Quick start

```bash
# 1. Build the server
cd go-server
go build ./...
go test ./...

# 2. Run the server (creates ./data/zowsup.db on first launch)
go run ./cmd/zowsup-go

# 3. Start the frontend in a second shell
cd go-server/frontend
npm install
npm run dev   # http://localhost:5173, proxies /api → :8080
```

## Layout

```
go-server/
├── cmd/zowsup-go/        # main entry — opens db, mounts API, signals shutdown
├── internal/
│   ├── accountmgr/       # per-account goroutine + lifecycle
│   ├── api/              # chi REST + coder/websocket events stream
│   ├── binxmpp/          # WhatsApp binary XMPP node codec (encoder + decoder)
│   ├── config/           # server config loader (env + flags)
│   ├── db/               # modernc.org/sqlite open + migrations (embedded SQL)
│   ├── eventbus/         # in-process pub-sub (account → API/WS)
│   ├── noise/            # Noise XX (WhatsApp variant) handshake
│   ├── proxy/            # per-account http.Transport + placeholders
│   └── store/            # high-level CRUD: accounts / proxies / ...
├── frontend/             # Vite + React + AntD dashboard
└── ROADMAP.md
```

`internal/db/migrations/` holds the SQL migration files; they are embedded
into the binary at compile time via `//go:embed`, so the server has no
filesystem dependency on the migrations directory at runtime.

## Design notes

- **Pure Go, no CGO.** SQLite is `modernc.org/sqlite`. The crypto stack uses
  the standard library plus `golang.org/x/crypto` (curve25519 / hkdf / chacha20poly1305).
- **One account == one goroutine + one `http.Transport`.** Per-account proxy
  setting (SOCKS5 / HTTP / HTTPS) lives on the account row; the
  `proxy.Manager` builds a fresh transport every time the proxy URL changes,
  with `{session_id}` / `{location}` placeholder expansion that mirrors the
  Python `--proxy host:port:user:pass` flag.
- **Storage is single-file SQLite (WAL).** Identity, sessions, prekeys,
  sender keys, contacts, chats and messages all live in the same DB; the
  schema is in [`internal/db/migrations/001_init.sql`](./internal/db/migrations/001_init.sql).
- **No CGO required.** `go build ./...` produces a single static binary on
  every supported platform.
- **Frontend in dev:** `npm run dev` (Vite at :5173, proxies `/api` to
  `:8080`). For production, `npm run build` emits `frontend/dist/` and the
  Go server can serve it directly via `-frontend ./frontend/dist`.
