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

**Live: `geeks-v1.0.0`, cut over 2026-07-31.** `stats.oneofthegeeks.com` now runs
this fork instead of `arp242/goatcounter:2.7`. Same bind mount, same port, same
database — the tracking snippet on oneofthegeeks.com was not touched.

**The automated deploy path is not yet working end to end.** The 2026-07-31 cutover
was done by hand. Before a tag can deploy on its own:
- `UNRAID_SSH_KEY` is the one repo secret still missing. A dedicated CI keypair was
  generated (`SHA256:Ec/z8Qys5xySrGTzqWV/qss//biMOlMrKO1iB/ZHdfs`) but its public
  half was never appended to Poppy's `/root/.ssh/authorized_keys`, so `deploy.yml`'s
  deploy job fails at the SSH step. The build job succeeds and does publish the
  image, which is why the manual cutover worked. The other five secrets are set:
  `TS_OAUTH_CLIENT_ID`/`_SECRET` (from Infisical Homelab `/Tailscale`),
  `UNRAID_USER=root`, `UNRAID_HOST=100.85.82.18`,
  `UNRAID_DEPLOY_PATH=/mnt/user/appdata/goatcounter`.
- The GHCR package was made **public** on 2026-07-31 so Poppy can pull it. It had to
  be: Poppy's stored `ghcr.io` credential is stale and the registry rejects it, and
  docker offers that credential instead of falling back to anonymous. Pulling
  required temporarily removing the `ghcr.io` entry from `/root/.docker/config.json`
  (backup at `/mnt/user/appdata/_ops/docker-config.bak.2026-07-31`, restored
  immediately after). Fix that credential before relying on `docker compose pull`
  for a private image.

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
- **The five schema migrations ran on 2026-07-31 and are not cleanly reversible.**
  `2026-02-12-1-rm-hit_stats` drops `hit_stats`; `2026-02-13-1-hits-width` rebuilds
  `hits`. The 2.7 image can no longer read this database — rolling back means
  restoring a backup, not re-pulling the old tag. Two backups on Poppy:
  `/mnt/user/appdata/goatcounter/backups/db-pre-geeks.sqlite3` and
  `db-pre-cutover-2026-07-31.sqlite3` (the latter is the one taken minutes before
  the swap, WAL-checkpointed). The old container's full config is beside them in
  `container-config.json` / `container-hostconfig.json` if it needs recreating.
  Verified across the cutover: `hit_counts`/`ref_counts`/`browser_stats`/
  `system_stats`/`location_stats`/`paths`/`sites`/`users` = 8/8/8/9/6/5/1/1 before
  and after, and a test pageview recorded afterwards, proving the rebuilt `hits`
  table still accepts writes. Do NOT verify with `select count(*) from hits` — that
  table is the transient ingest queue and is normally empty, so it proves nothing.
- **Container user uid is 1000** — confirmed 2026-07-31 against the built image
  (`docker run --rm --entrypoint id ghcr.io/oneofthegeeks/goatcounter:geeks-v1.0.0`
  → `uid=1000(goatcounter)`), matching the bind-mounted data dir's owner. The
  Dockerfile's `adduser -DG goatcounter goatcounter` carries no explicit `-u`, so
  re-check this if the base image ever changes; a mismatch leaves the container
  unable to write the database.
- **There is a synthetic pageview in the data**: path
  `/_deploy-check-geeks-v1.0.0`, recorded 2026-07-31 to prove the ingest path
  survived the `hits` rebuild. Safe to delete.
- **Two compose files, name both explicitly.** This repo has both upstream's
  `compose.yaml` (PostgreSQL, for running the test suite) and our
  `docker-compose.yml` (the deploy stack). Docker Compose's file discovery
  prefers `compose.yaml` when both exist, so a bare `docker compose ...` run
  from the repo root silently targets upstream's postgres stack, not ours. On
  Poppy this never bites — only `docker-compose.yml` gets copied into the
  deploy directory — but locally, target ours explicitly:
  `docker compose -f docker-compose.yml up -d`.
- **Never `docker login` on Poppy from the deploy.** Poppy pulls with its durable GHCR
  PAT; logging in with the job's `GITHUB_TOKEN` overwrote it with an expiring token and
  broke pulls for every app on the box (removed 2026-10-12).
