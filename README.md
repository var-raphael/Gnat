<p align="center">
  <img src="assets/gnat-logo.svg" width="96" height="96" alt="Gnat logo" />
</p>

<h1 align="center">Gnat</h1>

<p align="center">
  Small, quiet, everywhere. Self-hosted analytics without the bloat.
</p>

<p align="center">
  <a href="#usage">Usage</a> ·
  <a href="#features">Features</a> ·
  <a href="#why-gnat">Why Gnat</a> ·
  <a href="#configuration">Configuration</a> ·
  <a href="#roadmap">Roadmap</a> ·
  <a href="#license">License</a>
</p>

---

Gnat is a single Go binary for self-hosted web analytics. No Docker, no multi-service stack, no per-language SDKs.

## Why Gnat

Existing self-hosted tools force a tradeoff. Gnat aims for analytical depth in a single binary, plus an MCP layer for querying analytics via AI agents.

Gnat is built for one site per instance, matching the single-binary model. Features that only make sense once a team or several properties are involved (multi-site management, SSO, audit logs) are tracked separately on the roadmap rather than bolted onto the core tool.

## Usage

### Step 1: Get the binary

Pick one.

**Option A: Download a release (fastest, no Go toolchain needed)**

Grab the archive for your platform from the [Releases page](https://github.com/var-raphael/Gnat/releases), then extract it:

```bash
tar xzf gnat-vX.Y.Z-linux-amd64.tar.gz
cd gnat-vX.Y.Z-linux-amd64
chmod +x gnat
```

Windows users can extract the `.zip` instead, `chmod` is not needed there. Each archive includes the `gnat` binary itself plus `README.md` and `LICENSE`, nothing else to install.

**Option B: Build from source**

Requires Go 1.26 or newer.

```bash
git clone https://github.com/var-raphael/gnat.git
cd gnat
go build -o gnat ./cmd/gnat
```

Either path produces the same single `gnat` binary. The dashboard's HTML, CSS, and JS are compiled directly into it, so nothing else needs to sit next to it on disk.

### Step 2: Configure it

Gnat is configured entirely through environment variables, no config file. Create a `.env` file next to the binary, it is loaded automatically on startup:

```bash
# --- Database (sqlite is the default, zero setup required) ---
GNAT_DB_DRIVER=sqlite
GNAT_DB_PATH=./analytics.db

# --- Required secrets, generate your own random values for these ---
GNAT_API_KEY=generate-a-random-secret-here
GNAT_DASHBOARD_PASSWORD=another-random-secret

# --- The one site this instance tracks. This is the domain of the
# site sending events (e.g. your portfolio, your app), not the domain
# gnat itself is hosted on. Gnat supports a single site per instance
# right now; multi-site support is planned but not yet available. ---
GNAT_SITES=example.com

# --- Where gnat itself is reachable. Used for links the dashboard
# generates. This is gnat's own domain, separate from GNAT_SITES
# above, they are often different hosts (e.g. gnat runs on
# gnat.onrender.com while GNAT_SITES is your-portfolio.com). ---
GNAT_PUBLIC_URL=http://localhost:8080

# --- Optional, default shown ---
GNAT_BIND_PORT=8080
```

`GNAT_BIND_PORT` can usually be left out entirely. Gnat also reads the platform-standard `PORT` variable if it is set, which most hosts (Render, Heroku, Railway) inject automatically, so it binds to the right port with no extra configuration on those platforms.

See the [Configuration](#configuration) section below for every available variable, including Postgres and MySQL setup.

### Step 3: Run it

```bash
./gnat
```

You should see a line like:

```
gnat starting on :8080 (db: sqlite, sites: [example.com])
```

Visit `http://localhost:8080` (or your `GNAT_PUBLIC_URL`), it redirects straight to `/dashboard`. Log in with `GNAT_DASHBOARD_PASSWORD`.

### Step 4: Add the tracker to your site

Add one script tag before the closing `</body>` on any page you want tracked, using the same `GNAT_API_KEY` from your `.env`:

```html
<script
  src="https://your-gnat-domain.com/tracker.js"
  data-site-key="your-GNAT_API_KEY-here"
></script>
```

For a Next.js site using `next/script`:

```tsx
<Script
  src="https://your-gnat-domain.com/tracker.js"
  data-site-key="your-GNAT_API_KEY-here"
  strategy="afterInteractive"
/>
```

That is the entire integration. Pageviews are sent automatically. To track custom events (button clicks, signups, whatever matters to you), call `window.gnat.track(eventName, properties)` from anywhere on the page after the script has loaded:

```js
window.gnat.track("signup_completed", { plan: "pro" });
```

The site sending events must match `GNAT_SITES` on the server exactly, or ingestion is silently rejected.

### Step 5: Check the dashboard

Back on `/dashboard`, Top Pages, Custom Events, and Live Visitors should reflect what you just sent within a few seconds. Referrer and country data depend on real distinct visitors and real navigation, they will look empty during local/manual testing and fill in once real traffic arrives.

## Features

- **Single Go binary, no CGO required.** One executable, no runtime dependencies to install alongside it.
- **SQLite, Postgres, or MySQL, your choice.** Pick the backend that fits how you already run infrastructure.
- **Raw HTTP ingestion, no SDKs needed.** Any backend language can send events with a plain JSON POST.
- **Funnels, cohorts, retention curves, and auto-discovered path analysis.** The analytical depth that most single-binary tools skip.
- **Built-in MCP server.** AI agents and assistants can query your analytics directly, no separate export or import step.
- **CSV, JSON, and raw SQL export.** It is your data, in a format you can actually use elsewhere.

## Configuration

Every setting is an environment variable. There is no YAML or JSON config file, secrets and settings live in one place rather than split across a file and overrides.

| Variable | Default | Notes |
|---|---|---|
| `GNAT_BIND_PORT` | `8080` | Port the HTTP server listens on. `PORT` is checked first if set, so most PaaS hosts need no port configuration at all |
| `GNAT_PUBLIC_URL` | `http://localhost:8080` | The domain gnat itself is reachable at, used for links generated by the dashboard. Separate from `GNAT_SITES` |
| `GNAT_DB_DRIVER` | `sqlite` | `sqlite`, `postgres`, or `mysql` |
| `GNAT_DB_PATH` | `./analytics.db` | SQLite only |
| `GNAT_DB_HOST` | none | Postgres/MySQL only |
| `GNAT_DB_PORT` | driver default | Postgres/MySQL only |
| `GNAT_DB_USER` | none | Postgres/MySQL only |
| `GNAT_DB_PASSWORD` | none | Postgres/MySQL only |
| `GNAT_DB_NAME` | none | Postgres/MySQL only |
| `GNAT_DB_SSLMODE` | `disable` | Postgres only |
| `GNAT_API_KEY` | required | Authorizes writes to `/api/event`, also the `data-site-key` value the tracker script uses |
| `GNAT_DASHBOARD_PASSWORD` | required | Gates the dashboard and all `/api/stats` and `/api/export` endpoints |
| `GNAT_SITES` | required | The single domain this instance tracks. Multi-site is planned but not yet supported |

`GNAT_API_KEY` protects the ingest endpoint only. `GNAT_DASHBOARD_PASSWORD` protects everything else, including reads. They are deliberately independent so rotating one never affects the other.

### Sending events without the tracker script

Any backend can POST directly to `/api/event` instead of using `tracker.js`, useful for server-side events or non-browser clients:

```bash
curl -X POST http://localhost:8080/api/event \
  -H "Content-Type: application/json" \
  -H "X-API-Key: generate-a-random-secret-here" \
  -H "Origin: https://example.com" \
  -d '{
    "event_name": "pageview",
    "distinct_id": "some-stable-visitor-id",
    "properties": { "referrer": "https://google.com" }
  }'
```

The `Origin` header must match `GNAT_SITES` exactly, or the event is silently dropped. This is the same allowlist check the tracker script relies on, meant to prevent random third parties from writing events even if they obtain the ingest key.

## Architecture

Everything runs in one process:

- HTTP server and event ingestion
- Storage via GORM, swappable between SQLite, Postgres, and MySQL through a small dialect layer that isolates the handful of SQL fragments that actually differ per engine
- Query API that powers both the embedded dashboard and any custom UI you build against it
- Embedded dashboard, served as static files straight out of the Go binary
- Background jobs for path precomputation
- MCP server for AI agent access, authenticated by its own token, independent of dashboard sessions

## Roadmap

Nothing below exists yet. Listed here so it is clear what is planned versus what is already in the binary today.

- **Multi-site management.** One dashboard across several properties, instead of one instance per site.
- **SSO and team access control.** Real accounts and roles, instead of one shared password for everyone.
- **Managed hosting.** For anyone who wants Gnat without running and maintaining the binary themselves.
- **Audit logs.** A record of who exported data and who changed configuration, once more than one person has access.
- **Postgres and full multi-driver test coverage in CI**, alongside the SQLite and MySQL paths already covered.

## License

Gnat is licensed under the GNU Affero General Public License v3.0, AGPLv3. See [`LICENSE`](LICENSE) for the full text.

In short: you are free to self-host, modify, and use Gnat for any purpose, including commercially, inside your own organization. If you run a modified version of Gnat as a network service that other people use, you are required to make your modified source available to them under the same license. This is the same license Plausible uses, and it exists to keep the project genuinely open while preventing silent, uncredited SaaS resale of the exact same code.

## Contact

Found a bug, or want to report an issue? Open a GitHub issue, or reach out directly.

- Portfolio: [var-raphael.vercel.app](https://var-raphael.vercel.app)
- Email: [samuelraphael925@gmail.com](mailto:samuelraphael925@gmail.com)
