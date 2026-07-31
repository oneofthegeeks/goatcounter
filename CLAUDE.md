## What This Is
Self-hosted fork of [arp242/goatcounter](https://github.com/arp242/goatcounter) (privacy-friendly
web analytics), retheme in progress to a terminal-style look. Serves
`stats.oneofthegeeks.com` for Taylor.

## Stack
Go 1.26 + SQLite (CGO-backed, via `mattn/go-sqlite3` — CGO must stay **enabled**, there
is no pure-Go SQLite path here) or PostgreSQL. Server-rendered Go templates (`tpl/`) +
plain CSS (`public/`), no frontend framework/build step.

## Local Development
Ubuntu's apt Go is 1.22 — too old for this repo's `go 1.26.0` toolchain requirement.
Go 1.26 lives at `~/.local/go` instead:

```bash
export PATH="$HOME/.local/go/bin:$PATH"
export CGO_ENABLED=1        # required — SQLite support depends on it
go build ./cmd/goatcounter
./goatcounter serve -dev
```

`-dev` reads `public/` and `tpl/` straight off disk, so CSS edits are visible on
refresh — no rebuild needed. Only Go source changes need a rebuild.

## Testing
`go test ./...` needs the PostgreSQL container from `compose.yaml`
(`docker compose up -d postgres`), which cannot run in this environment (no docker
access as `taylor` here). The two theme regression tests run standalone without it:

```bash
export PATH="$HOME/.local/go/bin:$PATH"
go test -run 'TestTheme|TestNoHardcoded' .
```

## Deployment
Tag-driven on `geeks-v*` (deliberately **not** bare `v*` — upstream owns `v1.0`
through `v2.7` on this same repo history). `.github/workflows/deploy.yml` builds the
image, pushes to `ghcr.io/oneofthegeeks/goatcounter`, then scps `docker-compose.yml`
to Poppy over Tailscale and runs `docker compose pull && up -d --remove-orphans`.

## Key Files
- `public/vars.css` — light theme tokens.
- `public/dark.css` — dark theme tokens.
- `public/backend.css` — has a typography block appended at the end (font stack,
  sizing, rhythm); the rest of the file is upstream's.
- `tpl/` — deliberately **untouched** by the retheme. Keeping template diffs at zero
  is what makes `git merge upstream/main` (remote `upstream`,
  `github.com/arp242/goatcounter`) cheap to keep rebasing onto.

## Gotchas
- **Port 8092 must always stay published.** The `CloudflaredTunnelWebsites`
  container on Poppy routes `stats.oneofthegeeks.com` → `http://172.16.0.69:8092`.
  This is a hardcoded upstream route, not something this repo's compose file
  controls — changing the published port breaks the live site with no error here.
  The docker network the container sits on otherwise doesn't matter.
- **First boot of the retheme image runs five schema migrations that are not
  cleanly reversible.** A pre-cutover backup is kept at
  `/mnt/user/appdata/goatcounter/backups/db-pre-geeks.sqlite3` on Poppy — restore
  from that, don't try to migrate backwards.
- **Container user uid must stay 1000** — the bind-mounted data dir on Poppy
  (`/mnt/user/appdata/goatcounter`) is owned by uid 1000, and the Dockerfile's
  `adduser -DG goatcounter goatcounter` (no explicit `-u`) is expected to land on
  1000 on a fresh Alpine base since it's the first non-system user created in that
  build stage — but this has not been confirmed against a real build (no docker
  access in this environment). Verify with
  `docker run --rm --entrypoint id ghcr.io/oneofthegeeks/goatcounter:<tag>` before
  the first deploy; if it differs from `uid=1000(goatcounter)`, add `-u 1000` to the
  `adduser` line.
- **Two compose files, name both explicitly.** This repo has both upstream's
  `compose.yaml` (PostgreSQL, for running the test suite) and our
  `docker-compose.yml` (the deploy stack). Docker Compose's file discovery
  prefers `compose.yaml` when both exist, so a bare `docker compose ...` run
  from the repo root silently targets upstream's postgres stack, not ours. On
  Poppy this never bites — only `docker-compose.yml` gets copied into the
  deploy directory — but locally, target ours explicitly:
  `docker compose -f docker-compose.yml up -d`.
- **`GHCR_TOKEN` sits in the remote process environment, not just its command
  line.** `deploy.yml`'s "Pull & deploy" step passes `GITHUB_TOKEN` to the
  Poppy-side `bash -s` via a `VAR=value` prefix rather than embedding it in the
  script text — that's an injection-safety measure, not a confidentiality one.
  The token is still readable from `/proc/<pid>/environ` by root or a same-uid
  process on Poppy for the life of that `bash -s` process. Accepted trade-off:
  the token is short-lived and narrowly scoped (`GITHUB_TOKEN`, not a long-lived
  PAT), and Poppy is a single-operator box — piping it over stdin instead isn't
  worth it since the heredoc already occupies stdin.
