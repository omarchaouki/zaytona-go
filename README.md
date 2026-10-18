# zaytona-go: Huilerie El Hadaji

Website and admin panel of Huilerie El Hadaji (معصرة الهداجي لزيت الزيتون), El Kelaâ des Sraghna. Go rewrite of the TypeScript app:

- Bilingual landing page (Arabic RTL at `/ar`, French LTR at `/fr`), olive oil sold by the litre, cash on delivery.
- Order form in 3 steps with anti-spam (proof of work, honeypot, signed token, rate limits), confirmation page, WhatsApp hand-off, new-order e-mail and webhook.
- Customer reviews with photo, verified-purchase badge and moderation.
- Admin panel: dashboard, orders (search, filters, CSV, bulk actions, packing slip, live alerts with a chime), offers and prices, settings, certificates, FAQ, page texts and gallery, administrators (owner/staff), audit log, optional two-step login (TOTP).

One static Go binary serves everything; PostgreSQL is the only service it needs.

| Document | For |
| --- | --- |
| `PROMPT.md`, `SECURITY.md`, `docs/PARITY.md` | The specification, the security requirements, the feature list |
| `PLAN.md` | Decisions, versions, routes and the status of every milestone |
| `CLIENT_HANDOVER.md` | The owner: using the admin, what to provide (Darija and French) |
| `DATA_MIGRATION.md` | Moving from the TypeScript app: what moves, what does not |
| `docs/DEPENDENCIES.md` | Why each third-party module is there |

## Important decisions

- **New database, new schema.** The Go app runs on a fresh PostgreSQL database (`internal/db/migrations`). Nothing is read from the TypeScript app's database: orders, reviews and admin passwords are not carried over (see `DATA_MIGRATION.md`).
- **Nothing is invented.** Prices, the ONSSA number, reviews and counters come from the database; seeded examples are flagged (`is_demo`, "Exemple" badge, dashboard warnings) until the owner replaces them.
- **Every form works without JavaScript.** Scripts only enhance (the order and review proof of work needs JavaScript; a note says so when it is off).

## Requirements

- Go 1.27 (the toolchain line in `go.mod` downloads 1.27.2 automatically), Git, [Task](https://taskfile.dev).
- PostgreSQL 16 for production. For development and tests, nothing else: an embedded PostgreSQL is downloaded and started by `task db:local` and by the tests.
- Chrome or Chromium for the end-to-end tests and the mobile audit (optional).

## Development setup

Windows (PowerShell):

```powershell
winget install GoLang.Go Git.Git Task.Task   # then reopen PowerShell
git clone <this repository> zaytona-go
cd zaytona-go
copy .env.example .env                       # then set APP_SECRET (see below)
task tools                                   # Tailwind CLI (checksum verified) and Go tools
```

Linux / macOS:

```sh
git clone <this repository> zaytona-go && cd zaytona-go
cp .env.example .env
task tools
```

Set a random `APP_SECRET` in `.env` (64 hex characters). Generate one with `openssl rand -hex 32`, or in PowerShell:

```powershell
-join ((1..32) | ForEach-Object { '{0:x2}' -f (Get-Random -Maximum 256) })
```

Then:

```sh
task db:local      # terminal 1: PostgreSQL 16 on 127.0.0.1:5434, data in .data/pg
task db:migrate    # create the schema
task db:seed       # starting content (examples flagged), and the owner if ADMIN_EMAIL/ADMIN_PASSWORD are set
task dev           # terminal 2: http://localhost:3000 (templ and Tailwind watch, server restarts)
```

The owner account is created by `task db:seed` from `ADMIN_EMAIL` and `ADMIN_PASSWORD` (12+ characters) and must choose a new password at first login (`/admin/login`). The variables can be removed from `.env` afterwards.

## Configuration

All configuration comes from the environment (`.env` in development). `.env` is never committed and never baked into the image.

| Variable | Default | Meaning |
| --- | --- | --- |
| `APP_ENV` | `production` | `development` or `production` (production refuses the example secret and requires https for webhooks) |
| `PORT` | `3000` | HTTP port |
| `SITE_URL` | `http://localhost:3000` | Public URL (canonical links, sitemap, cookies: `https` gives the `__Host-` session cookie) |
| `DATABASE_URL` | | `postgres://user:password@host:5432/db` |
| `DATABASE_POOL_MAX` | `10` | Connection pool size |
| `APP_SECRET` | | 32+ random bytes in hex. Every key (sessions, form tokens, confirmation links, proof of work, TOTP encryption, phone and IP hashes, webhook signature) is derived from it (HKDF) |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` (JSON logs, phones and IPs redacted) |
| `TRUST_PROXY` | `false` | `true` behind Caddy/nginx: the client IP is read from `X-Real-IP`/`X-Forwarded-For` |
| `UPLOAD_DIR` | `./uploads` | Uploaded photos (re-encoded); back it up |
| `MAX_UPLOAD_MB` | `5` | Largest admin photo upload (review photos are capped at 5 MB) |
| `STORAGE_DRIVER` | `local` | Only `local` today |
| `ADMIN_EMAIL`, `ADMIN_PASSWORD` | | Owner created by `seed`; also used by `admin reset-password` |
| `SEED_WHATSAPP_NUMBER` | | Optional WhatsApp number written by the first seed |
| `SMTP_URL`, `NOTIFY_EMAIL_FROM` | | New-order e-mail, e.g. `smtps://user:pass@smtp.example.com:465` |
| `ORDER_WEBHOOK_URL` | | Fallback new-order webhook (the admin Settings value wins) |
| `TURNSTILE_SITE_KEY`, `TURNSTILE_SECRET_KEY` | | Optional Cloudflare Turnstile on the public forms |
| `UMAMI_WEBSITE_ID` | | Optional Umami analytics (cloud) |

**Rotating `APP_SECRET`** signs everyone out, invalidates pending form tokens and confirmation links, makes stored TOTP secrets unreadable (admins must set up two-step login again), and stops verified-purchase matching for past orders. Do it only after a leak.

## Commands

| Command | What it does |
| --- | --- |
| `task tools` | Download the pinned Tailwind CLI (SHA-256 checked) and install the Go tools |
| `task generate` | templ, sqlc and the typed i18n keys |
| `task dev` | Development server with watchers (needs `task db:local`) |
| `task db:local`, `task db:migrate`, `task db:seed` | Local PostgreSQL, schema, starting content |
| `task test` | Unit and integration tests (embedded PostgreSQL) |
| `task e2e` | Browser scenarios (needs a running server, `BASE_URL`, and `E2E_ADMIN_EMAIL`/`E2E_ADMIN_PASSWORD` for the admin ones) |
| `task audit:mobile` | Mobile audit, 320 to 1024 px, Arabic and French (`AUDIT_ADMIN_EMAIL`/`AUDIT_ADMIN_PASSWORD` add the admin pages; `-page=landing` reruns one public page) |
| `task lint`, `task vuln` | golangci-lint (gosec included), govulncheck |
| `task check` | Everything that must be green before a change is done |
| `task build` | Assets and the release binary in `./bin` |
| `task images` | Re-encode photos of `assets/source` into the site's AVIF/WebP set |
| `zaytona admin reset-password` | Reset `ADMIN_EMAIL` to `ADMIN_PASSWORD`, sign out its sessions, force a new password |
| `zaytona admin webhook-key` | Print the webhook signing key (hex) for the system that receives new orders |

The end-to-end tests and the audit with `AUDIT_ORDER=1` place real orders: run them against a throwaway database. `go run ./tests/e2e/cmd/firstlogin` completes the forced first password change of a freshly seeded owner (used by CI).

## Architecture

`cmd/zaytona` (serve, migrate, seed, admin, images, healthcheck) → `internal/web/{public,admin}` handlers → `internal/store` (sqlc queries in `internal/db/dbq`, transactions, audit) and `internal/domain` (pure rules) → `internal/view` (templ). Cross-cutting: `internal/httpx` (middleware, nonce CSP), `internal/auth` (passwords, sessions, TOTP), `internal/security` (rate limits, tokens, proof of work, keyed hashes), `internal/media` (uploads), `internal/notify` (e-mail, webhook), `internal/i18n`, `internal/config`. Front-end: `web/static` (Tailwind CSS, small JS modules bundled by `tools/assetbuild`).

Pages change in place like a single-page app, but every page stays server-rendered: `web/static/js/lib/nav.js` fetches same-site links and form posts and swaps the page in (history, back/forward, scroll and focus kept), falling back to a normal load whenever a swap could be wrong. Page code registers with `onPage(fn)` and gets an `AbortSignal` for anything it starts on `window`, `document` or a timer. Without JavaScript every link and form works as a plain request. Add `data-no-swap` to a link or form that must load the browser's way.

## Deployment (VPS)

1. **PostgreSQL 16.** Create a database and a login role that owns nothing but that database (no superuser). Migrations can run with a separate owner role if you prefer; the app role then needs only `SELECT, INSERT, UPDATE, DELETE` on the tables and `USAGE` on the sequence.
2. **Binary.** `task build` produces `bin/zaytona` (static, `CGO_ENABLED=0`, `-tags nodynamic`). Copy it to `/opt/zaytona/zaytona`.
3. **Environment.** `/etc/zaytona/zaytona.env` (owner root, mode 600) with `APP_ENV=production`, `SITE_URL=https://your-domain.ma`, `DATABASE_URL`, `APP_SECRET`, and optionally `ADMIN_EMAIL`/`ADMIN_PASSWORD` for the first seed.
4. **Schema and content.** `zaytona migrate`, then `zaytona seed` once (it never overwrites existing rows).
5. **Service.** `deploy/zaytona.service` (hardened systemd unit, uploads in `/var/lib/zaytona/uploads`, `TRUST_PROXY=true`): `systemctl daemon-reload && systemctl enable --now zaytona`.
6. **HTTPS.** `deploy/Caddyfile` (automatic certificates, zstd/gzip, 6 MB body cap, no `Server` header, confirmation tokens removed from access logs). Replace the domain.

Docker instead: `docker compose --profile app up -d --build` runs the production image (distroless, non-root, read-only file system, only the uploads volume writable, health check) next to a local PostgreSQL. Put Caddy in front of it the same way.

Shutdown is graceful: on SIGTERM the server stops accepting, drains requests for 20 s and flushes the notification queue. A daily job deletes expired sessions, rate-limit rows older than two days and spent proof-of-work signatures.

### Backups and restore

Back up both the database and the uploads folder every day, and keep several days:

```sh
pg_dump --format=custom --file=/var/backups/zaytona/db-$(date +%F).dump "$DATABASE_URL"
tar -czf /var/backups/zaytona/uploads-$(date +%F).tgz -C /var/lib/zaytona uploads
```

Restore (into an empty database, service stopped):

```sh
pg_restore --clean --if-exists --no-owner --dbname="$DATABASE_URL" db-YYYY-MM-DD.dump
tar -xzf uploads-YYYY-MM-DD.tgz -C /var/lib/zaytona
systemctl start zaytona
```

Once a month, restore the latest backup into a scratch database and open the admin against it: a backup that was never restored is not a backup.

### New-order webhook

When a webhook address is set (Settings → Alertes, or `ORDER_WEBHOOK_URL`), each new order is posted as JSON (`{"event":"order.created","order":{...}}`) with the header `X-Zaytona-Signature: sha256=<hex>`: the HMAC-SHA256 of the raw body with the webhook key. Receivers verify it before trusting the payload. The key is derived from `APP_SECRET`: `zaytona admin webhook-key` prints it in hex on the server (the HMAC key is the decoded bytes). Store it only in the receiving system. Addresses must be https (production) and may not point to private, loopback or link-local networks; redirects are not followed; 5 s timeout.

## Security

See `SECURITY.md` (requirements, checklist and accepted exceptions) and `PLAN.md` (what is implemented and tested). Highlights: nonce-based CSP, cross-origin protection plus per-session CSRF tokens, argon2id passwords with throttled, timing-safe login, optional TOTP with recovery codes, password re-check before changing the WhatsApp number or admins, owner/staff roles checked in handlers and store, audit log, sniffed and re-encoded uploads, SSRF-safe webhook, PII-redacted logs, `govulncheck` and gosec in CI.

## Testing

`task check` runs formatting, generated-code, vet, lint, unit and integration tests and govulncheck. CI (`.github/workflows/ci.yml`) also builds, checks size budgets, starts the app on a fresh PostgreSQL, completes the first login, runs the browser scenarios and the mobile audit, and runs Lighthouse (mobile) with the thresholds in `.github/lighthouserc.json`.
