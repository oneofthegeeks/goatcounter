# GoatCounter fork: terminal retheme + self-hosted deploy

Date: 2026-07-31
Status: approved, ready for implementation planning

## What this is

Retheme the GoatCounter UI to a dark-first "terminal" look and deploy the fork
to Poppy in place of the upstream image, preserving the existing database, site
configuration, and tracking snippet.

## Decisions

| Question | Decision |
|---|---|
| Scope | Retheme only — CSS. No template or Go changes. |
| Direction | Terminal: GitHub-dark surfaces, green accent, blue links. |
| Light mode | Retheme both files. `dark.css` = Terminal, `vars.css` = light counterpart sharing the same hues. |
| Typography | Light polish using existing assets: mono paths, tabular numerals, tighter spacing. No new font files. |
| Deploy | Convert from the Unraid Docker template to compose + GitHub Actions, matching the standard playbook. |

## Verified facts about the live system

Established by inspection on 2026-07-31, not assumed:

- Container `goatcounter`, image `arp242/goatcounter:2.7`, up as of inspection.
- Binary self-reports `7e91d8a9bdbb_2025-12-15T18:20:31Z-dev` — a dev build, not
  the 2.7 release, despite the image tag.
- Bind mount: `/mnt/user/appdata/goatcounter` → `/home/goatcounter/goatcounter-data`.
  Contains `db.sqlite3` (~500K) plus `-wal` (~1M) and `-shm`.
- Env: `GOATCOUNTER_LISTEN=:8080`, `GOATCOUNTER_AUTOMIGRATE=true`.
  Entrypoint `goatcounter`, cmd `serve -automigrate`.
- Host port mapping: `8092 → 8080`.
- Site 1: cname `stats.oneofthegeeks.com`, link_domain `https://oneofthegeeks.com`.
- No compose labels and no compose file on Poppy — the container was created
  from the Unraid Docker template UI.
- **Networks do not overlap.** `goatcounter` is on the default `bridge` network;
  `geeks-tracker-cloudflared` is on `springworksutah-task_appnet`. The tunnel
  therefore reaches the app over the **host at port 8092**, not container-to-container.
  Publishing 8092 on the host is the contract that must be preserved.
- The cloudflared container is token-based with remote ingress config, so its
  routing is managed in the Cloudflare dashboard and is not edited as part of this work.
- Poppy already has `ghcr.io` credentials in `/root/.docker/config.json`.
- `oneofthegeeks/goatcounter` is a **public** fork of `arp242/goatcounter`.
- Fork HEAD `00034b04` (2026-07-20) is identical to upstream `main` — zero divergence.

## How the UI is built

- Templates: `tpl/*.gohtml` (Go `html/template`). Not touched by this work.
- Styles: hand-written CSS in `public/`. No build step, no framework.
- Colour is fully tokenised: `public/vars.css` defines 85 custom properties for
  light, `public/dark.css` overrides the same names for dark.
- Theme selection is in `tpl/_backend_top.gohtml`: per-user setting of
  `light` / `dark` / unset, where unset loads both with
  `media="(prefers-color-scheme: dark)"` on the dark file.
- `public/*` and `tpl/*` are embedded into the binary via `go:embed` in
  `helper.go`, so shipping a change requires a rebuild — but `serve -dev` reads
  both from disk, giving an edit-and-refresh loop locally.
- A mono stack already exists in `shared.css` for `pre, code, .monospace`, and
  `td.right` already uses `tabular-nums`. The polish pass extends these
  patterns rather than introducing new ones.

## Migration risk

The live DB's newest applied migration is `2025-12-12-1-ref-scheme`. Five are pending:

| Migration | Effect |
|---|---|
| `2025-12-29-1-filter` | Adds `filters`, `filter_paths`. Additive. |
| `2026-02-12-1-rm-hit_stats` | **Drops `hit_stats`**; reindexes `hit_counts`. |
| `2026-02-13-1-hits-width` | **Rebuilds `hits`**: create `hits_new`, copy, drop `hits`, rename. |
| `2026-02-25-1-indexes` | Adds indexes on `ref_counts`, `browser_stats`, `system_stats`, `location_stats`. Additive. |
| `2026-03-07-1-jsonexport` | Rebuilds `exports` as `exports2`. |

At this data volume they complete in milliseconds. The risk is not duration, it
is reversibility: `GOATCOUNTER_AUTOMIGRATE=true` means they run silently on
first boot, and once `hit_stats` is dropped and `hits` rebuilt, **the old image
can no longer read the database**. Rollback is restore-from-backup, not
re-pulling the previous tag.

Mitigation, which is mandatory and ordered:

1. On Poppy, checkpoint the WAL and back up `db.sqlite3` — the main file alone
   is not a complete copy while a `-wal` file exists.
2. Copy the backup to the dev box.
3. Run the newly built binary against the **copy**, with automigrate, locally.
4. Confirm the dashboard renders and pageview totals match the live instance.
5. Only then cut over production.

## Repo shape

- Add `upstream` remote pointing at `arp242/goatcounter`.
- `main` is the fork's line: upstream plus our changes.
- Implementation happens on branch `geeks-theme`, merged to `main`.
- Changes are confined to `public/vars.css`, `public/dark.css`,
  `public/backend.css`, `public/shared.css`, plus new deploy files and docs.
  Keeping template and Go files untouched is what keeps
  `git rebase upstream/main` cheap.
- Delete `.github/workflows/build.yml` — it targets arp242's registry and would
  fail on every push to this fork. Keep `test.yml`.
- `.superpowers/` is excluded via `.git/info/exclude` (local-only, so the
  tracked `.gitignore` stays identical to upstream).

## The theme

Both token files already declare the same 85 names, so this is a value-for-value
swap with no new mechanism.

`dark.css` — Terminal:

- surfaces `#0d1117` base, `#161b22` raised
- text `#e6edf3`, muted `#8b949e`
- borders `#30363d`, grid `#21262d`
- charts: line `#3fb950`, fill `#11291a`, hchart bar `#238636`
- links `#58a6ff`

`vars.css` — light counterpart, same hues at light-safe contrast:

- surfaces `#fff` base, `#f6f8fa` raised
- text `#1f2328`, muted `#656d76`
- borders `#d0d7de`
- charts: green `#1a7f37`, links `#0969da`

Polish pass in `backend.css` / `shared.css`:

- paths and other identifier-like strings use the existing mono stack
- `tabular-nums` wherever figures stack vertically
- tighter row padding, reduced heading sizes, more consistent vertical rhythm

Both themes must stay legible at WCAG AA for body text; the accent colours above
were chosen with that in mind and should be checked, not assumed, during
implementation.

## Deploy

- `docker-compose.yml` in the repo:
  - image `ghcr.io/oneofthegeeks/goatcounter:${IMAGE_TAG}`
  - ports `8092:8080` — preserves the tunnel's path to the app
  - bind mount `/mnt/user/appdata/goatcounter:/home/goatcounter/goatcounter-data`
  - env `GOATCOUNTER_LISTEN=:8080`, `GOATCOUNTER_AUTOMIGRATE=true`
  - container_name `goatcounter`
- `.github/workflows/deploy.yml`: triggers on `geeks-v*` tags, builds the
  existing multi-stage `Dockerfile`, pushes to GHCR, connects to Poppy over
  Tailscale, and runs `docker login` → `compose pull` → `compose up -d`.
- Tag scheme is `geeks-v1.0.0`. Upstream already owns `v1.0`–`v2.7`, so a bare
  `v*` scheme would collide as soon as upstream tags are fetched.
- No Infisical project is needed: this app has no secrets. Its only
  configuration is the two non-secret env vars, which live in compose, so there
  is no `render-env.sh` step in this deploy.
- Deploy credentials are repo secrets. This fork is newly cloned and its
  Actions secrets have not been checked — confirm `TS_OAUTH_CLIENT_ID` /
  `TS_OAUTH_SECRET` and the Poppy SSH key exist on
  `oneofthegeeks/goatcounter` and add them from the standard sources if not.
  That is a plan step, not an assumption.

## Cutover

1. Back up the DB and validate the migration against the copy (see above).
2. Stop and remove the Unraid template container. Note its full settings first
   so it can be recreated if rollback is needed.
3. `compose up -d` with the same bind mount and port. The data files on Poppy
   are owned by uid 1000, which is the `goatcounter` user the Dockerfile
   creates — confirm the rebuilt image still lands on uid 1000, since a
   mismatch would leave the container unable to write the database.
4. Verify: container healthy, `stats.oneofthegeeks.com` loads over the tunnel,
   dashboard shows the retheme, historical pageview totals match what was
   recorded before cutover, and a fresh pageview from oneofthegeeks.com registers.

Rollback: restore the backed-up DB and recreate the template container.

## Explicitly unchanged

The bind mount path, host port 8092, the cloudflared container and its ingress,
DNS, and the site record in the database. The tracking snippet on
oneofthegeeks.com references the site's domain, not the image, so **no change to
the website is required**.

## Testing

- `go test ./...` needs PostgreSQL from `compose.yaml`; docker is currently
  unusable as the `taylor` user, so the full suite is out of reach on the dev
  box. CI (`test.yml`) covers it.
- This change is CSS-only, so the meaningful verification is visual against a
  local instance seeded with realistic data, plus the migration dry-run against
  a copy of production data.
- Smoke test after deploy: container up, site loads, dashboard renders, totals
  intact, new pageview recorded.

## Out of scope

Template or layout changes, new fonts, dashboard restructuring, a staging
instance, and any change to the tracking snippet or the website.
