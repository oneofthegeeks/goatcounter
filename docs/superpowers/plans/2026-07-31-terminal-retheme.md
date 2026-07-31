# Terminal Retheme + Self-Hosted Deploy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Retheme GoatCounter's UI to a dark-first terminal look and deploy the fork to Poppy in place of the upstream image, without touching the database, the tunnel, or the tracking snippet.

**Architecture:** Colour in GoatCounter is fully tokenised — `public/vars.css` defines 85 CSS custom properties for light, `public/dark.css` overrides the same names for dark, and `tpl/_backend_top.gohtml` picks between them per user setting. The retheme is therefore a value-for-value swap in those two files plus a typography pass in `backend.css`/`shared.css`. No templates, no Go, no new assets. Deployment converts the hand-made Unraid container into a compose stack fed by GitHub Actions and GHCR, keeping the same bind mount and host port.

**Tech Stack:** Go 1.26 (toolchain at `~/.local/go`, CGO on for SQLite), plain CSS with custom properties, Docker multi-stage build, GitHub Actions, GHCR, Tailscale, Unraid.

## Global Constraints

- Changes are confined to `public/vars.css`, `public/dark.css`, `public/backend.css`, `public/shared.css`, `theme_test.go`, `docker-compose.yml`, `.github/workflows/`, and `docs/`. Do not modify `tpl/*.gohtml` or any existing `.go` file — that is what keeps `git rebase upstream/main` cheap.
- Host port **8092** must stay published. The `CloudflaredTunnelWebsites` container routes `stats.oneofthegeeks.com` to `http://172.16.0.69:8092` — Poppy's LAN address, not a container name or docker IP. Anything that keeps listening on that host port works; changing it breaks the site.
- Bind mount stays `/mnt/user/appdata/goatcounter:/home/goatcounter/goatcounter-data`.
- Container name stays `goatcounter`.
- Tag scheme is `geeks-v*` (e.g. `geeks-v1.0.0`). Upstream owns `v1.0`–`v2.7`; a bare `v*` scheme collides.
- Every `var(--x)` used anywhere in `public/*.css` must be defined in **both** `vars.css` and `dark.css`.
- Body text must meet WCAG AA contrast in both themes.
- Go is at `~/.local/go/bin`. Every shell step that runs `go` must first `export PATH=$HOME/.local/go/bin:$PATH`.
- Never commit `goatcounter-data/` or the compiled `goatcounter` binary — both are already in `.gitignore`.
- Work happens on branch `geeks-theme`, which already exists and holds the spec commit.

---

### Task 1: Fork hygiene — upstream remote and dead workflow

Upstream's `build.yml` pushes to arp242's registry using secrets this fork doesn't have, so it fails on every push. Remove it before adding our own. Adding the `upstream` remote now makes later rebases a one-liner.

**Files:**
- Delete: `.github/workflows/build.yml`
- Modify: git remotes (no file)

- [ ] **Step 1: Add the upstream remote**

```bash
cd ~/dev/goatcounter
git remote add upstream https://github.com/arp242/goatcounter.git
git remote -v
```

Expected: `origin` (oneofthegeeks, fetch+push) and `upstream` (arp242, fetch+push) both listed.

- [ ] **Step 2: Confirm build.yml is the Docker Hub one before deleting it**

```bash
grep -nE "docker|registry|secrets\." .github/workflows/build.yml | head
```

Expected: references to Docker Hub / `secrets.DOCKER_*` or similar. If it instead contains something this fork needs, stop and reassess rather than deleting.

- [ ] **Step 3: Delete it**

```bash
git rm .github/workflows/build.yml
```

- [ ] **Step 4: Verify test.yml survived**

```bash
ls .github/workflows/
```

Expected: `test.yml` only.

- [ ] **Step 5: Commit**

```bash
git add -A .github/workflows/
git commit -m "chore(ci): drop upstream build.yml, add upstream remote

build.yml pushes to arp242's registry and cannot succeed on this fork."
```

---

### Task 2: Bring production data down for the migration dry-run

**This task is a safety gate — nothing gets deployed until it passes.** The live DB is five migrations behind, two of which are destructive (`2026-02-12-1-rm-hit_stats` drops `hit_stats`; `2026-02-13-1-hits-width` drops and rebuilds `hits`). Once they run, the old image can no longer read the database.

The copy made here doubles as the local development database, so the retheme is judged against real data instead of an empty dashboard. That is deliberate: one artifact, both jobs.

**Files:**
- Create: `~/dev/goatcounter/goatcounter-data/db.sqlite3` (gitignored)
- Create: `/mnt/user/appdata/goatcounter/backups/db-pre-geeks.sqlite3` on Poppy

**Interfaces:**
- Produces: a working local instance at `hermes.tailc42fb.ts.net:8081` serving real data, used for visual verification in Tasks 4–6.

- [ ] **Step 1: Record the current live totals, to compare against after migrating**

```bash
ssh poppy 'docker exec goatcounter goatcounter db query \
  -db sqlite+/home/goatcounter/goatcounter-data/db.sqlite3 \
  "select count(*) as hits from hits"'
```

Write the number down. If `db query` rejects that syntax, run `docker exec goatcounter goatcounter db query -h` and use the form it documents.

- [ ] **Step 2: Checkpoint the WAL and back up on Poppy**

A `.sqlite3` file copied while a `-wal` exists is not a complete database. Checkpoint first.

```bash
ssh poppy 'mkdir -p /mnt/user/appdata/goatcounter/backups && \
  docker exec goatcounter goatcounter db query \
    -db sqlite+/home/goatcounter/goatcounter-data/db.sqlite3 \
    "pragma wal_checkpoint(truncate)" && \
  cp /mnt/user/appdata/goatcounter/db.sqlite3 \
     /mnt/user/appdata/goatcounter/backups/db-pre-geeks.sqlite3 && \
  ls -lh /mnt/user/appdata/goatcounter/backups/'
```

Expected: `db-pre-geeks.sqlite3` present, roughly the size of the live `db.sqlite3`. If the `-wal` file is still large after the checkpoint, stop the container, copy all three files (`db.sqlite3`, `-wal`, `-shm`), and restart it.

- [ ] **Step 3: Copy it to the dev box**

```bash
mkdir -p ~/dev/goatcounter/goatcounter-data
scp poppy:/mnt/user/appdata/goatcounter/backups/db-pre-geeks.sqlite3 \
    ~/dev/goatcounter/goatcounter-data/db.sqlite3
ls -lh ~/dev/goatcounter/goatcounter-data/
```

- [ ] **Step 4: Build the current binary**

```bash
cd ~/dev/goatcounter
export PATH=$HOME/.local/go/bin:$PATH
go build ./cmd/goatcounter && ./goatcounter version
```

Expected: version line with `cgo=true`. Without cgo there is no SQLite support.

- [ ] **Step 5: List the pending migrations against the copy**

```bash
./goatcounter db migrate -db sqlite+./goatcounter-data/db.sqlite3 pending
```

Expected, exactly these five:
`2025-12-29-1-filter`, `2026-02-12-1-rm-hit_stats`, `2026-02-13-1-hits-width`, `2026-02-25-1-indexes`, `2026-03-07-1-jsonexport`.

If the list differs, stop — the spec's risk assessment was based on this list and needs revisiting.

- [ ] **Step 6: Run the migrations on the copy**

```bash
./goatcounter db migrate -db sqlite+./goatcounter-data/db.sqlite3 all
./goatcounter db migrate -db sqlite+./goatcounter-data/db.sqlite3 pending
```

Expected: each migration logged, then `no pending migrations`.

- [ ] **Step 7: Confirm no data was lost**

```bash
./goatcounter db query -db sqlite+./goatcounter-data/db.sqlite3 "select count(*) as hits from hits"
```

Expected: the same count recorded in Step 1. A different number means the `hits` rebuild dropped rows — stop and investigate before going anywhere near production.

- [ ] **Step 8: Point the local copy's site at this box so it can be viewed remotely**

GoatCounter serves per-vhost. The copied site record still says `stats.oneofthegeeks.com`, which won't match requests to the dev box.

```bash
./goatcounter db update site -db sqlite+./goatcounter-data/db.sqlite3 \
  -find stats.oneofthegeeks.com -vhost hermes.tailc42fb.ts.net
./goatcounter db show site -db sqlite+./goatcounter-data/db.sqlite3 \
  -find hermes.tailc42fb.ts.net -format table | head -3
```

Expected: the site's cname is now `hermes.tailc42fb.ts.net`. This edits only the local copy; production is untouched.

- [ ] **Step 9: Start the dev server and confirm it renders**

Bind to the Tailscale address, not `0.0.0.0` — this box is a public VPS.

```bash
./goatcounter serve -dev -listen hermes.tailc42fb.ts.net:8081 -tls http
```

Run it in the background. Then from the Mac, open `http://hermes.tailc42fb.ts.net:8081` and log in with the existing account. Expected: the dashboard loads with real pageview history, in today's ugly theme.

`-dev` makes the server read `public/` and `tpl/` from disk, so every later CSS edit is visible on refresh with no rebuild.

- [ ] **Step 10: No commit**

Nothing here is committed — the database is gitignored. Confirm the tree is clean:

```bash
git status --short
```

Expected: no output.

---

### Task 3: Tokenise the hardcoded colours

Eight colours in `backend.css` and `shared.css` are hardcoded and bypass the token system. On a `#0d1117` background, `1px solid #ddd` borders and `#555` text look broken. These must be tokenised **before** the palettes are written, or the retheme will appear to work while leaving artefacts behind.

This task also adds the regression test that guards the whole effort.

**Files:**
- Create: `theme_test.go`
- Modify: `public/shared.css:48`, `public/shared.css:53`, `public/shared.css:134`, `public/backend.css:55`, `public/backend.css:163`, `public/backend.css:202`, `public/backend.css:320`, `public/backend.css:330`, `public/backend.css:383`, `public/backend.css:417`
- Modify: `public/vars.css`, `public/dark.css` (add the new token definitions)

**Interfaces:**
- Produces: tokens `--table-border`, `--heading-anchor`, `--rule`, `--muted-text`, `--detail-border`, `--divider`, `--noscript-bg`, `--noscript-border`, `--invalid-outline`, `--invalid-bg`, defined in both theme files and consumed by later tasks.

- [ ] **Step 1: Write the failing test**

Create `theme_test.go`:

```go
package goatcounter

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// themedCSS are the stylesheets that consume theme tokens.
var themedCSS = []string{"backend.css", "shared.css", "style.css", "i18n.css"}

// Every var(--x) used in the CSS must be defined in BOTH vars.css (light) and
// dark.css (dark). A token defined in only one file silently keeps the other
// theme's value, which is the classic way a retheme ends up half-applied.
func TestThemeTokensDefinedInBothFiles(t *testing.T) {
	used := make(map[string]bool)
	useRe := regexp.MustCompile(`var\(\s*(--[a-z0-9-]+)`)
	for _, f := range themedCSS {
		b, err := os.ReadFile("public/" + f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range useRe.FindAllStringSubmatch(string(b), -1) {
			used[m[1]] = true
		}
	}
	if len(used) == 0 {
		t.Fatal("found no var(--x) usages; the test is not reading the right files")
	}

	defRe := regexp.MustCompile(`(?m)^\s*(--[a-z0-9-]+)\s*:`)
	for _, f := range []string{"vars.css", "dark.css"} {
		b, err := os.ReadFile("public/" + f)
		if err != nil {
			t.Fatal(err)
		}
		defined := make(map[string]bool)
		for _, m := range defRe.FindAllStringSubmatch(string(b), -1) {
			defined[m[1]] = true
		}

		var missing []string
		for tok := range used {
			if !defined[tok] {
				missing = append(missing, tok)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			t.Errorf("public/%s does not define %d token(s) the CSS uses: %s",
				f, len(missing), strings.Join(missing, ", "))
		}
	}
}

// Colours belong in vars.css and dark.css, never inline in a stylesheet, or
// they survive the theme switch and look wrong on the opposite background.
func TestNoHardcodedColours(t *testing.T) {
	comments := regexp.MustCompile(`(?s)/\*.*?\*/`)
	// A hex colour in a declaration value: after the ':' of a property.
	value := regexp.MustCompile(`:[^;{}]*`)
	hex := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)

	for _, f := range []string{"backend.css", "shared.css"} {
		b, err := os.ReadFile("public/" + f)
		if err != nil {
			t.Fatal(err)
		}
		src := comments.ReplaceAllString(string(b), "")

		var found []string
		for _, decl := range value.FindAllString(src, -1) {
			found = append(found, hex.FindAllString(decl, -1)...)
		}
		if len(found) > 0 {
			t.Errorf("public/%s has %d hardcoded colour(s), move them to vars.css and dark.css: %s",
				f, len(found), strings.Join(found, ", "))
		}
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
export PATH=$HOME/.local/go/bin:$PATH
go test -run 'TestTheme|TestNoHardcoded' -v .
```

Expected: `TestThemeTokensDefinedInBothFiles` PASSES (upstream is already consistent), `TestNoHardcodedColours` FAILS listing roughly `#ddd, #999, #666, #555, #bbb, #aaa, #ffcfcf, #f88, #f00, #feeaea`.

If the token test fails here, upstream has a pre-existing gap — note it and fix it in this task too.

- [ ] **Step 3: Add the new tokens to `public/vars.css`**

Append before the closing `}`:

```css
    --table-border:      #ddd;                             /* <tr> rules in generic tables */
    --heading-anchor:    #999;                             /* ¶ anchor links next to headings */
    --rule:              #666;                             /* h3.border underline */
    --muted-text:        #555;                             /* .page-n and similar de-emphasised text */
    --detail-border:     #bbb;                             /* .hchart .detail */
    --divider:           #aaa;                             /* tab nav and settings dividers */
    --noscript-bg:       #ffcfcf;                          /* <noscript> warning banner */
    --noscript-border:   #f88;
    --invalid-outline:   #f00;                             /* input:user-invalid */
    --invalid-bg:        #feeaea;
```

- [ ] **Step 4: Add the same ten tokens to `public/dark.css`**

Values chosen for the current dark background; Task 4 replaces them with the Terminal palette.

```css
    --table-border:      #333;
    --heading-anchor:    #777;
    --rule:              #666;
    --muted-text:        #aaa;
    --detail-border:     #444;
    --divider:           #555;
    --noscript-bg:       #4a1f1f;
    --noscript-border:   #a33;
    --invalid-outline:   #ff6b6b;
    --invalid-bg:        #3a1a1a;
```

- [ ] **Step 5: Replace the hardcoded values**

In `public/shared.css`:

```css
tr           { border-bottom: 1px solid var(--table-border); }
tfoot tr:first-child { border-top: 1px solid var(--table-border); }
h1 a, h2 a, h3 a, h4 a, h5 a, h6 a { font-size: .7em; line-height: 0; color: var(--heading-anchor); }
```

In `public/backend.css`:

```css
h3.border        { border-bottom:1px solid var(--rule); margin-bottom:.2em; }
.page-n      { color: var(--muted-text); font-size: .8em; }
.hchart .detail      { padding: 0 3em; border-bottom: 1px solid var(--detail-border); }
.tab-nav a             { padding: 0 .9em; border-right: 1px solid var(--divider); }
#page-settings-main #secret { display:none; margin-left:1em; padding-left:1em; border-left:1px solid var(--divider); }
noscript   { display: block; padding: .4em; text-align: center; background-color: var(--noscript-bg); border-bottom: 1px solid var(--noscript-border); }
input:user-invalid { outline: 1px solid var(--invalid-outline) !important; box-shadow: 0 0 .2em var(--invalid-outline) !important; background-color: var(--invalid-bg); }
```

- [ ] **Step 6: Run the tests and watch them pass**

```bash
go test -run 'TestTheme|TestNoHardcoded' -v .
```

Expected: both PASS.

- [ ] **Step 7: Eyeball it**

Refresh `http://hermes.tailc42fb.ts.net:8081`. Expected: **no visible change at all** — this task swapped values for identical values. Anything that moved means a selector was mistyped.

- [ ] **Step 8: Commit**

```bash
git add theme_test.go public/vars.css public/dark.css public/backend.css public/shared.css
git commit -m "refactor(css): tokenise hardcoded colours, add theme regression test

Ten colours bypassed the custom-property system and would survive the
theme switch. Adds a test asserting every token is defined in both
theme files and no colour is inlined."
```

---

### Task 4: The Terminal palette in `dark.css`

**Files:**
- Modify: `public/dark.css`

**Interfaces:**
- Consumes: the ten tokens added in Task 3.
- Produces: the dark palette that Task 5's light theme mirrors.

- [ ] **Step 1: Rewrite the colour values in `public/dark.css`**

Keep every token name and the file's existing comment layout. Replace values:

```css
    --text:              #e6edf3;
    --bg:                #0d1117;
    --header-border:     #30363d;
    --backdrop:          #161b22;
    --target-bg:         #2d4a22;
    --loading-text:      #8b949e;

    --link:              #58a6ff;
    --link-hover:        #79c0ff;

    --input-text:        #e6edf3;
    --input-bg:          #0d1117;
    --input-border:      #30363d;
    --input-focus:       #58a6ff;
    --fieldset-border:   #30363d;
    --form-error-text:   #ff7b72;
    --input-error-border: #f85149;

    --btn-text:          #e6edf3;
    --btn-bg:            linear-gradient(#21262d, #161b22);
    --btn-border:        #30363d;
    --btn-border-outset: #3d444d;
    --btn-hover-bg:      linear-gradient(#30363d, #21262d);
    --btn-active-bg:     linear-gradient(#161b22, #21262d);

    --code-text:         #e6edf3;
    --code-bg:           #161b22;
    --code-border:       #30363d;

    --tooltip-text:      #e6edf3;
    --tooltip-bg:        #161b22;
    --tooltip-border:    #30363d;
    --tooltip-shadow:    #010409;

    --chart-line:        #3fb950;
    --chart-fill:        #11291a;
    --chart-grid:        #21262d;
    --hchart-border:     #2ea043;
    --hchart-bar:        #238636;
    --hchart-bar-hover:  #2ea043;

    --nav-bg:            #161b22;
    --nav-border:        #30363d;
    --footer-shadow:     #010409;
    --filter-kw-text:    #d29922;

    --info-bg:           #0c2d6b;
    --info-border:       #1f6feb;
    --error-bg:          #3a1519;
    --error-border:      #f85149;

    --event-bg:          #1c2b1a;
    --pageviews-text:    #8b949e;

    --datepicker-border:             #30363d;
    --datepicker-shadow:             rgba(1,4,9,.8);
    --datepicker-dayname-text:       #8b949e;
    --datepicker-day-text:           #e6edf3;
    --datepicker-day-bg:             #161b22;
    --datepicker-day-hover-bg:       #238636;
    --datepicker-today-text:         #58a6ff;
    --datepicker-selected-shadow:    #1f6feb;
    --datepicker-selected-text:      #58a6ff;
    --datepicker-future-text:        #6e7681;
    --datepicker-future-bg:          #0d1117;
    --datepicker-outside-month-text: #6e7681;

    --text-table-even-bg:      #161b22;
    --text-table-border:       #21262d;
    --text-table-rank-text:    #8b949e;

    --widget-border:           #30363d;
    --widget-dragging-border:  #d29922;
    --widget-dragging-bg:      #2d2a1a;
    --widget-handle-bg:        #21262d;
    --widget-handle-text:      #8b949e;
    --widget-handle-hover-bg:  #30363d;

    --box-bg:                  #161b22;
    --box-shadow:              #010409;

    --plus:                    #3fb950;
    --minus:                   #f85149;

    --table-border:      #21262d;
    --heading-anchor:    #6e7681;
    --rule:              #30363d;
    --muted-text:        #8b949e;
    --detail-border:     #30363d;
    --divider:           #30363d;
    --noscript-bg:       #3a1519;
    --noscript-border:   #f85149;
    --invalid-outline:   #f85149;
    --invalid-bg:        #3a1519;
```

- [ ] **Step 2: Run the tests**

```bash
export PATH=$HOME/.local/go/bin:$PATH
go test -run 'TestTheme|TestNoHardcoded' -v .
```

Expected: both PASS. A failure here means a token name was renamed or dropped while retyping.

- [ ] **Step 3: Look at it in dark mode**

Set the user theme to dark: **Settings → User → Theme → Dark**, or with the OS in dark mode leave it on auto. Refresh `http://hermes.tailc42fb.ts.net:8081`.

Check specifically: the dashboard chart, the pages list, the Browsers/Systems horizontal bars, the date picker (open it), the settings pages, and a form with a validation error. Every surface should be on the `#0d1117`/`#161b22` family with green charts and blue links. Anything still light-grey is a token that was missed.

- [ ] **Step 4: Check contrast**

Body text `#e6edf3` on `#0d1117` is roughly 15:1, muted `#8b949e` on `#0d1117` roughly 6:1 — both clear AA. Confirm with the browser's accessibility inspector on one body paragraph and one muted label rather than trusting these numbers.

- [ ] **Step 5: Commit**

```bash
git add public/dark.css
git commit -m "feat(theme): terminal palette for dark mode

GitHub-dark surfaces, green chart accent, blue links."
```

---

### Task 5: The light counterpart in `vars.css`

Same hues, darkened for contrast on white, so switching themes changes brightness rather than identity.

**Files:**
- Modify: `public/vars.css`

**Interfaces:**
- Consumes: the token names and the ten additions from Task 3.

- [ ] **Step 1: Rewrite the colour values in `public/vars.css`**

```css
    --text:              #1f2328;
    --bg:                #ffffff;
    --header-border:     #d0d7de;
    --backdrop:          #f6f8fa;
    --target-bg:         #fff8c5;
    --loading-text:      #656d76;

    --link:              #0969da;
    --link-hover:        #0550ae;

    --input-text:        #1f2328;
    --input-bg:          #ffffff;
    --input-border:      #d0d7de;
    --input-focus:       #0969da;
    --fieldset-border:   #d0d7de;
    --form-error-text:   #cf222e;
    --input-error-border: #cf222e;

    --btn-text:          #1f2328;
    --btn-bg:            linear-gradient(#ffffff, #f6f8fa);
    --btn-border:        #d0d7de;
    --btn-border-outset: #afb8c1;
    --btn-hover-bg:      linear-gradient(#f6f8fa, #eaeef2);
    --btn-active-bg:     linear-gradient(#eaeef2, #f6f8fa);

    --code-text:         #1f2328;
    --code-bg:           #f6f8fa;
    --code-border:       #d0d7de;

    --tooltip-text:      #1f2328;
    --tooltip-bg:        #ffffff;
    --tooltip-border:    #d0d7de;
    --tooltip-shadow:    #8c959f;

    --chart-line:        #1a7f37;
    --chart-fill:        #dafbe1;
    --chart-grid:        #eaeef2;
    --hchart-border:     #1a7f37;
    --hchart-bar:        #aceebb;
    --hchart-bar-hover:  #6fdd8b;

    --nav-bg:            #f6f8fa;
    --nav-border:        #d0d7de;
    --footer-shadow:     #d8dee4;
    --filter-kw-text:    #9a6700;

    --info-bg:           #ddf4ff;
    --info-border:       #54aeff;
    --error-bg:          #ffebe9;
    --error-border:      #cf222e;

    --event-bg:          #dafbe1;
    --pageviews-text:    #656d76;

    --datepicker-border:             #d0d7de;
    --datepicker-shadow:             rgba(31,35,40,.15);
    --datepicker-dayname-text:       #656d76;
    --datepicker-day-text:           #1f2328;
    --datepicker-day-bg:             #f6f8fa;
    --datepicker-day-hover-bg:       #aceebb;
    --datepicker-today-text:         #0969da;
    --datepicker-selected-shadow:    #0969da;
    --datepicker-selected-text:      #0969da;
    --datepicker-future-text:        #8c959f;
    --datepicker-future-bg:          #f6f8fa;
    --datepicker-outside-month-text: #8c959f;

    --text-table-even-bg:      #f6f8fa;
    --text-table-border:       #eaeef2;
    --text-table-rank-text:    #656d76;

    --widget-border:           #d0d7de;
    --widget-dragging-border:  #bf8700;
    --widget-dragging-bg:      #fff8c5;
    --widget-handle-bg:        #f6f8fa;
    --widget-handle-text:      #656d76;
    --widget-handle-hover-bg:  #eaeef2;

    --box-bg:                  #f6f8fa;
    --box-shadow:              #d8dee4;

    --plus:                    #1a7f37;
    --minus:                   #cf222e;

    --table-border:      #d0d7de;
    --heading-anchor:    #8c959f;
    --rule:              #d0d7de;
    --muted-text:        #656d76;
    --detail-border:     #d0d7de;
    --divider:           #d0d7de;
    --noscript-bg:       #ffebe9;
    --noscript-border:   #cf222e;
    --invalid-outline:   #cf222e;
    --invalid-bg:        #ffebe9;
```

- [ ] **Step 2: Run the tests**

```bash
go test -run 'TestTheme|TestNoHardcoded' -v .
```

Expected: both PASS.

- [ ] **Step 3: Look at it in light mode**

Switch **Settings → User → Theme → Light** and refresh. Walk the same surfaces as Task 4 Step 3. Then set Theme back to auto and toggle the Mac between light and dark to confirm the automatic switch still works — that path uses `media="(prefers-color-scheme: dark)"` in `tpl/_backend_top.gohtml`, which this work does not touch.

- [ ] **Step 4: Check contrast**

`#1f2328` on white is roughly 14:1 and `#656d76` roughly 5:1 — both AA. Verify one body paragraph and one muted label with the accessibility inspector.

- [ ] **Step 5: Commit**

```bash
git add public/vars.css
git commit -m "feat(theme): light counterpart to the terminal palette

Same hues darkened for contrast on white; auto-switching unchanged."
```

---

### Task 6: Typography and spacing polish

Colour alone leaves it reading as "GoatCounter with dark colours". This is the pass that sells the terminal feel, using only what the codebase already has: the mono stack declared in `shared.css:32` and the `tabular-nums` pattern at `shared.css:50`.

**Files:**
- Modify: `public/shared.css`, `public/backend.css`

- [ ] **Step 1: Give the mono stack a name in `public/shared.css`**

Replace line 32's declaration with a token-driven one. First add the stack to **both** `public/vars.css` and `public/dark.css` (identical value in each — it is not a colour, but keeping it beside the other tokens keeps theming in one place):

```css
    --mono: 'DejaVu Sans Mono', 'Bitstream Vera Sans Mono', 'Consolas', monospace;
```

Then in `shared.css`:

```css
pre, code, .monospace { font-family: var(--mono); }
```

- [ ] **Step 2: Run the tests**

```bash
go test -run 'TestTheme|TestNoHardcoded' -v .
```

Expected: both PASS. The token test now also covers `--mono`, which is why it goes in both files.

- [ ] **Step 3: Set paths in mono and align the figures, in `public/backend.css`**

Append at the end of the file, so the intent is obvious and a rebase conflict is trivial to resolve:

```css
/*** Terminal theme: typography
 ****************************/
/* Paths are identifiers, not prose — mono makes them scannable and stops
 * proportional spacing from making /a/b/c and /a/b/d look alike. */
.rlink, .page-title, .count-list .col-path { font-family: var(--mono); font-size: .92em; }

/* Any column of stacked figures gets fixed-width digits so they line up.
 * The Totals widget renders through table.count-list too, so these two
 * selectors cover both the Pages list and Totals. */
.count-list .col-count,
.count-list .col-count-diff,
.chart-left, .chart-right { font-variant-numeric: tabular-nums; font-feature-settings: 'tnum' on; }

/* Tighter rows: the default spacing wastes vertical space on a dense list. */
.count-list td      { padding-top: .15em; padding-bottom: .15em; }
.count-list-text td { padding: .35em; }

/* Calmer headings — the defaults are larger than the content warrants. */
h2 { font-size: 1.25rem; letter-spacing: -.01em; }
h3 { font-size: 1.05rem; }
```

- [ ] **Step 4: Verify against the real dashboard**

Refresh and check: paths render in mono, the count column's digits line up vertically across rows, rows are tighter without text touching, and headings no longer dominate. Compare a wide path such as `/blog/…` at mobile width to confirm the `.rlink` ellipsis still works — it is set at `backend.css:110-112` with `overflow:hidden`.

- [ ] **Step 5: Check the widths did not break**

`.count-list .col-path` is `20rem` and `.rlink` has `max-width: 17.5rem` at `backend.css:111`. Mono is wider per character than Lato, so confirm at both desktop and narrow widths that paths still truncate cleanly rather than wrapping or overflowing. If they overflow, raise `.col-path` to `22rem` in the appended block rather than editing the original rule.

- [ ] **Step 6: Commit**

```bash
git add public/vars.css public/dark.css public/shared.css public/backend.css
git commit -m "feat(theme): mono paths, tabular figures, tighter rhythm

Reuses the mono stack and tnum pattern already in shared.css."
```

---

### Task 7: Compose file and deploy workflow

**Files:**
- Create: `docker-compose.yml`
- Create: `.github/workflows/deploy.yml`
- Modify: `CLAUDE.md` (create — the repo has none)

- [ ] **Step 1: Confirm the repo's Actions secrets exist**

The fork is freshly cloned and its secrets have never been checked.

```bash
gh secret list --repo oneofthegeeks/goatcounter
gh variable list --repo oneofthegeeks/goatcounter
```

Expected: the Tailscale OAuth pair and the Poppy SSH key used by the other repos. If any are missing, copy the names from a working repo (`gh secret list --repo oneofthegeeks/<a-deployed-app>`) and add them from the standard sources. Do not print secret values.

- [ ] **Step 2: Confirm the rebuilt image still uses uid 1000**

The data files on Poppy are owned by uid 1000. If the image's `goatcounter` user lands on a different uid, the container cannot write the database.

```bash
grep -n "adduser\|addgroup" Dockerfile
```

Alpine's `adduser -DG goatcounter goatcounter` assigns the first free uid, which is 1000 on this base image — the same one the live container already runs as. Verify after the first build with:

```bash
docker run --rm --entrypoint id ghcr.io/oneofthegeeks/goatcounter:<tag>
```

Expected: `uid=1000(goatcounter)`. If it differs, add an explicit `-u 1000` to the `adduser` line in the Dockerfile.

- [ ] **Step 3: Write `docker-compose.yml`**

```yaml
name: 'goatcounter'

services:
  goatcounter:
    image:          'ghcr.io/oneofthegeeks/goatcounter:${IMAGE_TAG}'
    container_name: 'goatcounter'
    restart:        'unless-stopped'
    # CloudflaredTunnelWebsites routes stats.oneofthegeeks.com to
    # http://172.16.0.69:8092 (Poppy's LAN address). Changing this port breaks
    # the site; the docker network this container sits on does not matter.
    ports:          ['8092:8080']
    volumes:        ['/mnt/user/appdata/goatcounter:/home/goatcounter/goatcounter-data']
    environment:
      'GOATCOUNTER_LISTEN':      ':8080'
      'GOATCOUNTER_AUTOMIGRATE': 'true'
```

- [ ] **Step 4: Write `.github/workflows/deploy.yml`**

This mirrors `~/dev/geeks-tracker/.github/workflows/deploy.yml`, which is the established pattern, minus the Infisical render step (this app has no secrets):

```yaml
# Deploy on geeks-v* tags: build image -> push to GHCR -> scp compose to Poppy over
# Tailscale -> pull & up. Tag prefix is geeks-v because upstream owns v1.0-v2.7.
# Repo secrets: TS_OAUTH_CLIENT_ID, TS_OAUTH_CLIENT_SECRET, UNRAID_SSH_KEY,
#   UNRAID_HOST, UNRAID_USER, UNRAID_DEPLOY_PATH.  Repo var: TS_TAGS.
name: Deploy
on:
  push:
    tags: ["geeks-v*"]
  workflow_dispatch:

env:
  IMAGE: ghcr.io/oneofthegeeks/goatcounter

jobs:
  build-and-push:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write
    steps:
      - uses: actions/checkout@v7
      - uses: docker/setup-buildx-action@v4
      - uses: docker/login-action@v4
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - uses: docker/build-push-action@v7
        with:
          context: .
          push: true
          tags: |
            ${{ env.IMAGE }}:${{ github.ref_name }}
            ${{ env.IMAGE }}:${{ github.sha }}
            ${{ env.IMAGE }}:latest
          cache-from: type=gha
          cache-to: type=gha,mode=max

  deploy:
    needs: build-and-push
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - name: Connect to Tailscale
        uses: tailscale/github-action@v4
        with:
          oauth-client-id: ${{ secrets.TS_OAUTH_CLIENT_ID }}
          oauth-secret: ${{ secrets.TS_OAUTH_CLIENT_SECRET }}
          tags: ${{ vars.TS_TAGS || 'tag:github-actions' }}
      - name: Add SSH key
        uses: webfactory/ssh-agent@v0.10.0
        with:
          ssh-private-key: ${{ secrets.UNRAID_SSH_KEY }}
      - name: Copy compose to Poppy
        run: |
          ssh -o StrictHostKeyChecking=accept-new ${{ secrets.UNRAID_USER }}@${{ secrets.UNRAID_HOST }} \
            "mkdir -p ${{ secrets.UNRAID_DEPLOY_PATH }}"
          scp -o StrictHostKeyChecking=accept-new docker-compose.yml \
            ${{ secrets.UNRAID_USER }}@${{ secrets.UNRAID_HOST }}:${{ secrets.UNRAID_DEPLOY_PATH }}/docker-compose.yml
      - name: Pull & deploy
        run: |
          ssh -o StrictHostKeyChecking=accept-new ${{ secrets.UNRAID_USER }}@${{ secrets.UNRAID_HOST }} bash -s <<'EOF'
            set -e
            cd ${{ secrets.UNRAID_DEPLOY_PATH }}
            echo "${{ secrets.GITHUB_TOKEN }}" | docker login ghcr.io -u ${{ github.actor }} --password-stdin
            export IMAGE_TAG=${{ github.ref_name }}
            docker compose pull
            docker compose up -d --remove-orphans
            docker compose ps
          EOF
```

`UNRAID_DEPLOY_PATH` must be `/mnt/user/appdata/goatcounter` for this app, since the compose file bind-mounts that same directory. Confirm its value in Step 1 rather than assuming it — it is per-repo, and other repos point it elsewhere.

The Go build takes about 80 seconds cold, so `cache-from: type=gha` matters here.

- [ ] **Step 5: Write `CLAUDE.md`**

The repo has none. Use the standard template: What This Is (fork of arp242/goatcounter, retheme, serves stats.oneofthegeeks.com), Stack, Local Development (the Task 2 loop — Go 1.26 at `~/.local/go`, CGO required, `serve -dev` reads CSS from disk), Testing (`go test ./...`; full suite needs the Postgres in `compose.yaml`, and docker is unusable as `taylor` on the dev box), Deployment (tag `geeks-v*`), Key Files (`public/vars.css` and `dark.css` are the theme; `tpl/` deliberately untouched), Gotchas (port 8092 and the tunnel; the five migrations and no clean rollback; rebase onto `upstream/main`).

- [ ] **Step 6: Commit**

```bash
git add docker-compose.yml .github/workflows/deploy.yml CLAUDE.md
git commit -m "feat(deploy): compose stack and GHCR deploy workflow

Same bind mount, same port 8092, container name unchanged."
```

- [ ] **Step 7: Merge to main and push — but do not tag**

```bash
git checkout main
git merge --no-ff geeks-theme -m "feat: terminal retheme and self-hosted deploy"
git push -u origin main
```

**Do not create or push the `geeks-v1.0.0` tag in this task.** Pushing it fires
`deploy.yml`, which runs `docker compose up -d` on Poppy while the hand-made
Unraid container still holds the name `goatcounter` — docker refuses to reuse a
container name, so that deploy fails. The tag is pushed in Task 8, after the old
container has been removed. This ordering was agreed with Taylor before execution.

- [ ] **Step 8: Confirm the workflow is syntactically valid without running it**

```bash
gh workflow list --repo oneofthegeeks/goatcounter
```

Expected: `Deploy` appears in the list, meaning GitHub parsed the YAML. It will
not have run — its only trigger is a `geeks-v*` tag.

Task 7 ends here. Nothing has been deployed and production is untouched.

---

### Task 8: Production cutover

**Warning:** this stops the running analytics container. Do it knowingly, and confirm with Taylor before starting — the site stops recording pageviews for the duration.

**Files:** none — this is operational.

- [ ] **Step 1: Re-verify the backup exists and is current**

```bash
ssh poppy 'ls -lh /mnt/user/appdata/goatcounter/backups/'
```

If it predates recent traffic worth keeping, redo Task 2 Step 2.

- [ ] **Step 2: Record the live container's full settings, for rollback**

```bash
ssh poppy 'docker inspect goatcounter --format "{{json .Config}}" > /mnt/user/appdata/goatcounter/backups/container-config.json; \
  docker inspect goatcounter --format "{{json .HostConfig}}" > /mnt/user/appdata/goatcounter/backups/container-hostconfig.json; \
  ls -lh /mnt/user/appdata/goatcounter/backups/'
```

- [ ] **Step 3: Record the aggregate row counts, to compare after**

Do **not** count `hits` — it is the transient ingest queue, normally empty, so
comparing it proves nothing. The tables below are what the dashboard renders.

```bash
ssh poppy 'docker exec goatcounter goatcounter db query \
  -db sqlite+/home/goatcounter/goatcounter-data/db.sqlite3 \
  "select (select count(*) from hit_counts) as hit_counts,
          (select count(*) from ref_counts) as ref_counts,
          (select count(*) from browser_stats) as browsers,
          (select count(*) from system_stats) as systems,
          (select count(*) from location_stats) as locations,
          (select count(*) from paths) as paths,
          (select count(*) from sites) as sites,
          (select count(*) from users) as users"'
```

Expected as of 2026-07-31: `8, 8, 8, 9, 6, 5, 1, 1`. `hit_stats` is deliberately
absent from this list — migration `2026-02-12-1-rm-hit_stats` drops it, and its
6 rows are superseded by `hit_counts`.

- [ ] **Step 4: Stop and remove the template container**

```bash
ssh poppy 'docker stop goatcounter && docker rm goatcounter'
```

The bind-mounted data at `/mnt/user/appdata/goatcounter` is untouched by this.

- [ ] **Step 5: Push the tag and let the workflow deploy**

The container name is now free, so `docker compose up -d` can take it.

```bash
git tag -a geeks-v1.0.0 -m "geeks-v1.0.0: terminal retheme"
git push origin geeks-v1.0.0
gh run list --workflow deploy.yml --limit 3
gh run watch <run-id> --exit-status
```

Expected: green. Then confirm what landed:

```bash
ssh poppy 'docker ps --filter name=goatcounter --format "{{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}"'
```

Expected: the container up, image `ghcr.io/oneofthegeeks/goatcounter:geeks-v1.0.0`, ports showing `0.0.0.0:8092->8080/tcp`.

If the workflow fails after the old container is already gone, the site is down —
bring it back immediately with the rollback at the end of this task, then debug
the workflow with production restored.

- [ ] **Step 6: Confirm the migrations ran and the data survived**

```bash
ssh poppy 'docker logs goatcounter 2>&1 | tail -20'
ssh poppy 'docker exec goatcounter goatcounter db query \
  -db sqlite+/home/goatcounter/goatcounter-data/db.sqlite3 \
  "select (select count(*) from hit_counts) as hit_counts,
          (select count(*) from ref_counts) as ref_counts,
          (select count(*) from browser_stats) as browsers,
          (select count(*) from system_stats) as systems,
          (select count(*) from location_stats) as locations,
          (select count(*) from paths) as paths,
          (select count(*) from sites) as sites,
          (select count(*) from users) as users"'
```

Expected: the five migrations logged once, and counts matching Step 3 — allowing for growth from pageviews recorded between Step 3 and the stop. Any count that *dropped* means data loss: roll back.

- [ ] **Step 7: Verify the public site**

From the Mac, load `https://stats.oneofthegeeks.com`. Expected: it loads over the tunnel with no DNS or certificate change, the dashboard shows the terminal theme, and the historical charts are intact.

- [ ] **Step 8: Confirm tracking still works**

Visit `https://oneofthegeeks.com`, then reload the dashboard. Expected: the pageview appears. This is the proof that the tracking snippet needed no change.

- [ ] **Step 9: Update the docs**

Per the standing rule that a release is not finished until the docs match reality: update `CLAUDE.md` with the deployed tag and the cutover date, and write a memory recording that `stats.oneofthegeeks.com` now runs the fork from `ghcr.io/oneofthegeeks/goatcounter`, deployed by `geeks-v*` tags, with the pre-cutover backup at `/mnt/user/appdata/goatcounter/backups/db-pre-geeks.sqlite3`.

- [ ] **Step 10: Commit the doc updates**

```bash
git add CLAUDE.md
git commit -m "docs: record geeks-v1.0.0 cutover"
git push
```

**Rollback, if any step fails:** restore the backup over `/mnt/user/appdata/goatcounter/db.sqlite3`, remove the compose stack, and recreate the Unraid template container from `container-config.json` using image `arp242/goatcounter:2.7`.

---

## Notes for the implementer

- The dev server from Task 2 stays running throughout Tasks 3–6. `-dev` reads CSS from disk, so no rebuild is needed between edits — just refresh.
- If it is ever restarted: `export PATH=$HOME/.local/go/bin:$PATH` first, and bind to `hermes.tailc42fb.ts.net:8081`, never `0.0.0.0` — this is a public VPS.
- `go test ./...` in full needs the Postgres from `compose.yaml`, which needs docker, which the `taylor` user cannot currently reach. Run the two theme tests specifically; CI covers the rest.
- The visual companion server from the design session is still running on port 62646 and can be stopped with `scripts/stop-server.sh` under the brainstorming skill directory, or left to time out after four hours idle.
