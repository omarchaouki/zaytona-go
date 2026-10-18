# Claude Code Prompt: Huilerie El Hadaji, Go rewrite (landing page + orders + admin)

## 0. Role, goal and working method

You are a senior Go engineer, application security engineer and mobile-first product designer. Rebuild the existing Huilerie El Hadaji web app (today a TanStack Start / TypeScript app, repo `zaytona`) in **Go**, with **feature parity**, **stronger security**, and **better mobile performance**.

This is a **parity rewrite against the same PostgreSQL database**, not a new product:

- The TypeScript app is the **reference implementation**. Clone it read-only next to this repo (`../zaytona`). When this prompt and the reference disagree on a behavior, the **reference behavior wins** unless this prompt explicitly changes it (marked **CHANGE**). When the reference README and its older `PROMPT.md` disagree, the README and `PLAN.md` "Status" section win (they describe the product as it is now).
- The Go app must run on the **existing production database without data loss**: same tables, same enums, same order reference sequence, same password hashes, same keyed phone hashes. Admins log in with their current passwords after the switch.
- Feature checklist to tick: [docs/PARITY.md](docs/PARITY.md). Security rules: [SECURITY.md](SECURITY.md). Persistent rules: [CLAUDE.md](CLAUDE.md).
- Already in this repo (use them, adjust if a tool version forces it, and say why in PLAN.md): `Taskfile.yml`, `.golangci.yml`, `sqlc.yaml`, `Dockerfile`, `docker-compose.yml`, `deploy/`, `.github/` (CI, Lighthouse budgets, Dependabot), `.env.example`, `.claude/settings.json`, `docs/DEPENDENCIES.md`. Go module path: `github.com/omarchaouki/zaytona-go` (change everywhere if the repo lives elsewhere).

Working method:

1. Read `CLAUDE.md`, `SECURITY.md`, `docs/PARITY.md`, then skim the reference repo (`../zaytona/src`, `../zaytona/drizzle`, `../zaytona/src/i18n/*.json`, `../zaytona/PLAN.md`). Use the `frontend-design` skill for visual work.
2. Write `PLAN.md` (stack with exact versions you installed, folder structure, route map, milestone list, design tokens). Keep it updated as milestones land, with a "Status" section like the reference.
3. Implement milestone by milestone (section 20). After each milestone run `task check` (format, generated code up to date, vet, lint, tests, govulncheck) and, for UI work, `task audit:mobile`. Fix everything before moving on.
4. Never invent facts: no fake reviews, numbers, awards, certificate numbers or prices. Placeholders are marked `TODO_CLIENT` and demo data is flagged `is_demo` exactly like the reference.
5. Every new third-party dependency needs one line of justification in `docs/DEPENDENCIES.md` (what it does, why the standard library is not enough, maintenance status). Prefer the standard library.
6. Copy rule: never use the em dash character in UI copy, docs or commit messages.

## 1. Business context (current state)

- Business: **معصرة الهداجي لزيت الزيتون / Huilerie El Hadaji**, an olive oil press (not a cooperative) in **Louanasda, El Kelaâ des Sraghna 43000, Maroc** (الوناسدة، قلعة السراغنة 43000، المغرب). Name and address are editable in admin settings in both languages.
- Product: **olive oil only, sold by volume**. Offers in litres (seed: 30 / 60 / 90 L), price per litre set in admin (seed placeholder 90 MAD/L, flagged as example), automatic or fixed price per offer, plus an optional "other quantity" (customer types litres within min/max).
- Audience: Moroccan buyers on mid-range Android phones over 4G, arriving mostly from **Facebook / Instagram ads**; Arabic first, French second.
- Payment: **cash on delivery only**. No deposit, no bank details, no online payment.
- ONSSA authorization: section and badge driven by admin data (number, dates, optional scan). Hidden when no public number exists. Never shown with fake data.
- Single job of the public page: turn a visit into a COD order (or a WhatsApp conversation that becomes one) with the least friction.

## 2. Stack (mandatory unless a reason is written in PLAN.md)

| Concern | Choice | Notes |
| --- | --- | --- |
| Language | **Go 1.27** (go.mod `go 1.27`) | Stdlib first. `encoding/json/v2` allowed. |
| HTTP | `net/http` with Go 1.22+ `ServeMux` patterns, own small middleware chain | No web framework. Fewer deps, fewer surprises. |
| CSRF | `http.CrossOriginProtection` (Go 1.25+) **plus** SameSite=Lax cookie **plus** per-session synchronizer token on admin mutations | Defense in depth: the stdlib middleware only protects modern browsers. |
| Templates | **templ** (`github.com/a-h/templ`), generated code committed | Type-safe, auto-escaping, compiles to Go. Do not use templ `css` components (inline styles break CSP). |
| Admin interactivity | **htmx 2.0.x** (vendored, pinned, self-hosted) | Not htmx 4 (not stable "latest" until 2027). Config in section 6. |
| Public page JS | Small vanilla ES modules bundled and minified with the **esbuild Go API** (`github.com/evanw/esbuild/pkg/api`) | No htmx on the landing page: keeps initial JS minimal. |
| CSS | **Tailwind CSS v4** via the **standalone CLI** (no Node), version pinned, downloaded and checksum-verified by `tools/fetchtools` | Logical utilities only (`ms-/me-/ps-/pe-/start-/end-`). |
| DB driver | **pgx v5 >= 5.9.2** (`github.com/jackc/pgx/v5`) | 5.9.2 fixes CVE-2026-41889. Never enable the simple protocol. |
| Queries | **sqlc** (`pgx/v5` output), generated code committed | All SQL in `.sql` files, parameterized. No string-built SQL anywhere. |
| Migrations | **goose v3** Provider API, migrations embedded with `embed.FS` | Baseline from the existing Drizzle schema (section 4). |
| Money | `numeric(10,2)` in DB, `github.com/shopspring/decimal` in Go (+ `pgx-shopspring-decimal`) | Never `float64` for money. |
| Passwords | argon2id, PHC string format, compatible with the reference hashes (`@node-rs/argon2`) | `github.com/alexedwards/argon2id` or `x/crypto/argon2` + PHC parser. Test with a real hash fixture from the reference. |
| 2FA (**CHANGE**, new) | TOTP (`github.com/pquerna/otp`), optional per admin, strongly recommended for owners | Recovery codes hashed. |
| Anti-bot | ALTCHA proof of work, `github.com/altcha-org/altcha-lib-go/v2`, widget JS self-hosted | Must be a version that includes the 2025 fix for challenge splicing and replay. |
| Images | Decode: stdlib + `golang.org/x/image/webp`; resize: `golang.org/x/image/draw`; encode: `github.com/gen2brain/webp` and `github.com/gen2brain/avif` (CGo-free via WASM, build tag `nodynamic`) | Static binary, no libvips. |
| Rich text | Markdown (goldmark) rendered then sanitized with `bluemonday`; if the reference stores HTML, sanitize that with bluemonday on save and on render | |
| Email | `github.com/wneessen/go-mail` (SMTP_URL) | Optional, never blocks an order. |
| Logging | `log/slog` JSON, PII redaction (section 16) | |
| Local DB without Docker | `github.com/fergusstrange/embedded-postgres` (`task db:local`, port 5433) | Replaces `pnpm db:local` (PGlite). Also used by integration tests. |
| E2E + mobile audit | `github.com/chromedp/chromedp` (Go, no Node) | Ports `pnpm audit:mobile` and the Playwright scenarios. |
| Lint / security | golangci-lint v2 (incl. gosec), `govulncheck`, `go vet`, `gofumpt` | |
| Task runner | **Task** (`Taskfile.yml`), works on Windows, macOS, Linux | The client develops on Windows. No Makefile. |

Record the exact versions you install in `PLAN.md`. Pin tool versions with `go get -tool` (go.mod `tool` directives) where possible.

## 3. Architecture and folder structure

Single binary, subcommands: `zaytona serve | migrate | seed | admin reset-password | images | healthcheck | version`.

```
cmd/zaytona/            main.go, subcommand wiring only
internal/
  config/               env parsing + validation (hand-written, typed, tested), derived keys (HKDF)
  httpx/                middleware: request id, recover, real IP, security headers + CSP nonce,
                        cross-origin protection, body limits, timeouts, gzip/brotli for HTML, logging
  web/                  router, handlers grouped by area:
    public/             landing, order, confirmation, reviews, policies, sitemap, robots, wa-click, pow
    admin/              auth, dashboard, orders, offers, reviews, certificates, faq, content, settings,
                        admins, audit, notifications poll
  view/                 templ components (layout, landing sections, admin pages, ui primitives)
  i18n/                 ar.json, fr.json (embedded), generated typed keys (keys_gen.go), locale helpers
  domain/               pure logic, no I/O: phone normalization, pricing, delivery fee, order totals,
                        reference format, WhatsApp link + templates, review rules, CSV escaping
  store/                repositories over sqlc queries; transactions; advisory locks
  db/
    migrations/         goose SQL migrations (embedded)
    queries/            sqlc .sql files
    dbq/                sqlc generated code (committed)
  auth/                 password hashing, sessions, CSRF tokens, TOTP, login throttling
  security/             keyed hashes (phone, IP), signed tokens (form render time, confirmation token),
                        ALTCHA, rate limiter (Postgres-backed)
  media/                upload validation, decode limits, EXIF orientation, resize, WebP/AVIF encode,
                        storage driver interface (local now, S3/R2 later)
  notify/               email + webhook (SSRF-safe client)
  seed/                 idempotent seed, demo flags, ported from the reference
web/
  static/               css/app.css (Tailwind entry + tokens), js/ (ES modules), fonts/ (woff2 + licenses),
                        vendor/ (htmx, altcha widget; each with version + sha256 in vendor/MANIFEST.txt),
                        images/ (generated responsive images)
  embed.go              //go:embed of built assets (hashed filenames)
assets/source/          original photos (input for `zaytona images`)
tools/                  fetchtools (Tailwind binary, checksum verified), i18ngen, assetbuild (esbuild + hashing + precompression)
tests/e2e/              chromedp scenarios + mobile audit
deploy/                 Caddyfile, systemd unit example
docs/                   PARITY.md, DEPENDENCIES.md, DATA_MIGRATION.md (you write it)
```

Rules: handlers are thin (parse, validate, call store/domain, render). `domain/` has no imports from `store`, `web` or `net/http`. Every handler takes `context.Context` from the request; all DB calls have timeouts.

## 4. Database: same schema, new tooling

1. **Baseline**: build `internal/db/migrations/00001_baseline.sql` from the reference's Drizzle migrations `0000` to `0007` (or a `pg_dump --schema-only` of a database migrated with them). The resulting schema must be **identical** (tables, columns, types, defaults, enums, indexes, constraints, sequences incl. `order_number_seq` and its current start of 10001).
2. **Adopting an existing database**: `zaytona migrate` detects a Drizzle-managed database (`drizzle.__drizzle_migrations` present, all 8 entries applied), verifies the schema fingerprint matches the baseline, then records goose version 1 as applied without running it. If the fingerprint does not match, it refuses and prints the differences. Fresh databases run the baseline normally.
3. **Schema test**: an integration test applies the reference Drizzle SQL files to one embedded Postgres and the goose baseline to another, then compares `information_schema` + `pg_catalog` dumps. They must match.
4. New tables or columns for Go-only features (TOTP, recovery codes) come as later goose migrations, additive only, so the reference app could still run against the database during the transition.
5. Order references: `OL-YYYY-` + 6-digit number from `nextval('order_number_seq')`. Duplicate guard runs inside the order transaction behind a per-phone advisory lock (same as reference; test with 25 parallel submits).
6. Keyed hashes (phone for verified reviews, IP): reproduce the reference algorithm **exactly** (read `../zaytona/src/server`) with the same `APP_SECRET`, otherwise existing "verified purchase" matches and rate-limit rows break. Add a test vector computed from the reference code.
7. Write `docs/DATA_MIGRATION.md`: cutover steps (backup, stop TS app, `zaytona migrate`, start Go app, smoke test, rollback = start TS app again since no destructive change was made), and local dev data import.

## 5. Routes

| Method + path | Purpose |
| --- | --- |
| `GET /` | 301 to `/ar` (Arabic default for everyone, as in the reference) |
| `GET /{locale}` | Landing page, `locale` in `ar` / `fr`, else 404 |
| `GET /{locale}/privacy`, `/{locale}/terms` | Policy pages (admin-editable) |
| `POST /{locale}/order` | Place order (works without htmx; JS enhances) |
| `GET /{locale}/order/{reference}?t=` | Confirmation page, signed token required (references are sequential), `noindex`, `Referrer-Policy: no-referrer` |
| `GET /{locale}/reviews?page=` | Reviews fragment for "load more" |
| `POST /{locale}/reviews` | Submit review (multipart when photo) |
| `GET /api/pow` | ALTCHA challenge |
| `POST /api/wa-click` | Anonymous WhatsApp click counter (placement, locale) |
| `GET /uploads/{id}.{ext}` | Uploaded images (random ids only) |
| `GET /static/...` | Hashed, immutable assets |
| `GET /sitemap.xml`, `/robots.txt` | SEO |
| `GET /healthz` | Liveness (no DB), `GET /readyz` readiness (DB ping) |
| `/admin/...` | Login, 2FA, forced password change, dashboard, orders (list, detail, print slip, CSV), offers, reviews, certificates, FAQ, content, settings, admins, audit log, notifications poll |

Admin routes: `X-Robots-Tag: noindex, nofollow`, `Cache-Control: no-store`.

## 6. Rendering and front-end rules

- **Progressive enhancement**: every form works as a plain `method="post"` form with a full-page response. JS and htmx only enhance. (This removes the reference's "submit before hydration" class of bugs.) Exception: ALTCHA needs JS; without JS the order section shows WhatsApp and phone ordering in a `<noscript>` block.
- **CSP compatible by construction**: no inline `<script>` without the per-request nonce, no inline event handlers, no `style=""` attributes, no `eval`. Dynamic widths (rating bars, progress) use `<meter>`/`<progress>`, a fixed set of classes, or CSSOM from JS.
- htmx config (admin only), via `<meta name="htmx-config">`: `allowEval: false`, `allowScriptTags: false`, `includeIndicatorStyles: false`, `selfRequestsOnly: true`, `historyCacheSize: 0` (admin pages hold personal data). No `hx-on` attributes. htmx requests carry the CSRF token header.
- Assets: esbuild bundles `web/static/js/*.js` per page (landing, admin), Tailwind builds `app.css`, then `tools/assetbuild` content-hashes filenames, writes a manifest used by templ helpers, and precompresses (`.br`, `.gz`). Served with `Cache-Control: public, max-age=31536000, immutable`.
- Fonts: self-hosted woff2, subset Arabic + Latin, `font-display: swap`, licenses in `web/static/fonts/LICENSES/`. Preload only the 2 fonts the current locale uses above the fold.
- Images: `<picture>` with AVIF, WebP, JPEG fallbacks at 480/960/1600 px, explicit `width`/`height`, `loading="lazy"` below the fold, hero preloaded with `fetchpriority="high"`.

## 7. Internationalization

- `/ar` (RTL) and `/fr` (LTR); `<html lang dir>` per route; `hreflang` alternates + canonical.
- Port `../zaytona/src/i18n/ar.json` and `fr.json` **as they are** (copy is client-approved). `tools/i18ngen` generates typed key constants; the generator and a test fail if the two files ever have different keys or an unused/missing key is referenced.
- Dynamic content stays in DB columns `*_ar` / `*_fr`.
- Western digits in both locales; Moroccan formatting `1.250,5 درهم` / `1.250,5 MAD` (implement in `domain/money` with tests; Go has no CLDR, do not pull a large i18n lib for two formats).
- Dates in admin pinned to `Africa/Casablanca` (embed tzdata with `import _ "time/tzdata"` so the static container has it).

## 8. Design system (carry over, do not redesign)

Port the reference design: tokens from `../zaytona/src/styles/app.css` and `PLAN.md` section 6 plus the later client changes recorded in its Status section:

- Palette: limewash `#F6F2E7`, sand `#ECE5D3`, olive `#2E3B1F`, leaf `#5C6B34`, oil `#D9A21B`, oil-deep `#8A5F0C`, ink `#23241F`, ink-soft `#55564C`, WhatsApp `#25D366` with `#073B1C` text, progress line fresh olive green. Bolder olive identity (deep olive hero, why-us, story, footer with gold accents) per client request.
- Type: Cairo 700 headings + IBM Plex Sans Arabic body (line-height 1.85, no letter-spacing on Arabic); Fraunces headings + Inter body for French. Golden-ratio type scale and spacing tokens (`g0` to `g6`) as in the reference.
- Signature: the bab horseshoe arch around the hero photo with the amber line; round ink seal for ONSSA; olive-sprig line-art wallpaper tile (alpha baked in, 13% light / 9% dark); 1.5 px loading line on navigation and form submits.
- Motion: entrance sequence, scroll reveals with stagger (IntersectionObserver, also observes content added later), seal "pressed" on scroll. Everything off under `prefers-reduced-motion`; no video under reduced motion or `Save-Data`.

## 9. Mobile optimization (measurable gates, not slogans)

"100% mobile optimized" is defined as passing all of these, on every public page in both languages and every admin page in both languages:

1. `task audit:mobile` (chromedp port of the reference audit): zero findings at 320, 360, 390, 412 px portrait, phone landscape (e.g. 740x360), 768x1024 and 1024x768. Checks: horizontal overflow of the page, elements spilling out of their box, text under 12 px, tap targets under 44x44 px, text fields under 120 px wide, fixed elements overlapping (floating WhatsApp vs sticky bar vs notification cards), content under iOS safe areas.
2. Lighthouse mobile (CI, simulated 4G, mid-tier device): Performance >= 95, Accessibility 100, Best Practices 100, SEO 100. LCP < 2.0 s, CLS < 0.05, INP < 200 ms, TBT < 100 ms.
3. Budgets, enforced by a test on the built manifest: landing initial JS <= 25 KB gzip (ALTCHA widget loaded only when the form approaches the viewport, not counted), landing CSS <= 30 KB gzip, landing HTML <= 60 KB gzip, max 2 preloaded fonts, hero image <= 120 KB at 960 px AVIF.
4. Server: p95 TTFB for `/ar` under 100 ms on the VPS with a warm cache. Landing data (settings, offers, approved reviews summary) cached in memory with invalidation on admin save (no stale WhatsApp number: invalidate synchronously).
5. Inputs: correct `inputmode`, `autocomplete`, `enterkeyhint`; phone field opens the numeric keypad; 16 px minimum input font size (prevents iOS zoom); sticky bottom bar on phones with Order + WhatsApp; viewport `width=device-width, initial-scale=1, viewport-fit=cover`; no user-scaling lock.
6. Admin on phones: bottom tab bar (dashboard, orders, offers, reviews, More sheet), orders as cards up to 1279 px (2 columns from 768 px), table from 1280 px, one slim notification bar on phones, French labels shortened on phones where the reference does it.

## 10. Public landing page

Port the section order, copy and behavior of the reference landing route (`../zaytona/src/routes/$locale/index.tsx` and `src/components/landing/`). As of the reference README/PLAN that is: sticky header (logo, AR | FR, Order), hero (headline, price anchor "from X MAD, Y MAD/L" from the cheapest available offer, Order CTA, WhatsApp CTA, trust row: COD, delivery everywhere, ONSSA; hero rating snippet from approved non-demo reviews only), trust strip, litre offers (compact horizontal cards on phones, 2 columns from 640 px, 4 from 1024 px, plus "other quantity" card), benefits of olive oil (authorised-claim wording only, general-information note), why us (data-driven reason cards that only appear with real data), ONSSA section (`#onssa`), how to order (vertical timeline on phones), story, reviews, order form (`#order`), FAQ (accordion with `<details>`/`<summary>`, `FAQPage` JSON-LD), final CTA, footer (name, address, map link loaded on click, phone, WhatsApp, hours, socials, legal links, ONSSA number, year computed in Morocco time).

Conversion rules stay: one primary action per screen, no pop-ups, no autoplay audio, no newsletter wall. Every "Order" button is a plain `#order` link that also selects the offer via a small script; works before JS loads.

SEO: unique title/description per locale, Open Graph + Twitter cards with `og-ar.jpg` / `og-fr.jpg`, `sitemap.xml`, `robots.txt` (disallow `/admin`), JSON-LD `LocalBusiness`, `Product` (non-demo offers), `FAQPage`, `AggregateRating` and `Review` from approved non-demo reviews only. All JSON-LD built with `encoding/json` and emitted in a nonce'd `<script type="application/ld+json">` with `<` escaped.

## 11. Order flow

Same as the reference: 3 steps (offer or custom litres, contact details, review and send) in one HTML form with per-step validation in JS and full validation on the server.

- Fields: offer or litres, full name, Moroccan phone (accept any common format, normalize to `+212XXXXXXXXX`, mobile prefixes 5/6/7 as in reference rules), city (searchable list of Moroccan cities AR/FR, accent and spelling tolerant, free text allowed), optional address, optional note.
- Prices come from the DB only; the client total is display only. Delivery fee rules from settings (fixed, free above amount, always free, confirmed by phone) plus optional delivery time text.
- Anti-spam layers: ALTCHA proof of work (fetched when the form nears the viewport, solved in background), honeypot, HMAC-signed render-time token (reject under 3 s), Postgres rate limits (5 per 10 min per IP and per phone), optional Turnstile behind env flag.
- Duplicate guard (same phone + same offer within 10 min) with "send anyway".
- Draft kept in `localStorage` for 7 days (values + step), restored on load, cleared after success. One automatic retry on network failure, then a recovery box (details kept, Retry, Send by WhatsApp with the full order prefilled).
- Success: redirect (303) to the signed confirmation URL: drawn check, reference ticket with copy, summary, WhatsApp confirm button with the admin template, phone option. Meta Pixel `Lead` fired once per order (event ID = reference) when the pixel is enabled and consented.
- Attribution: first-touch `utm_*` + `fbclid` kept 30 days on the device, saved in `orders.attribution`.
- Notifications (email, webhook) sent asynchronously after commit with a bounded worker and timeouts; never block or fail the order.

## 12. Reviews

Same as the reference: public list (approved only, featured first, "load more"), summary with average, partial stars, per-star bars that filter, photo lightbox, cooperative replies AR/FR, verified badge via keyed phone hash match to a delivered order, example reviews flagged `is_demo` with badge and excluded from every aggregate and JSON-LD. Submit form: name, rating (native radio group, keyboard accessible), comment 10 to 600 chars, optional city, optional phone (never displayed, stored only as keyed hash), optional photo (re-encoded, EXIF stripped, 5 MB max), honeypot + signed token + ALTCHA + 3 per hour per IP. Status `pending` until moderated.

Admin: tabs pending / published / refused, search, bulk publish/refuse, refuse with internal reason, pin, edit text (original kept), replies AR/FR, owner-only delete, pending badge in nav.

## 13. Admin panel

Port everything the reference admin does (see `docs/PARITY.md`), then finish what the reference left open:

- Auth: login, forced password change on first login (12+ chars, checked against a small embedded list of common passwords), optional TOTP 2FA (**CHANGE**), sessions 7 days sliding + 30 days absolute (**CHANGE**: absolute cap), "sign out other sessions", per-account language (AR/FR).
- Dashboard: money card (collected all time, this month, delivered unpaid), counters (today, new, to deliver with cash to collect, delivered, total, WhatsApp clicks, pending reviews), 30-day orders chart as server-rendered inline SVG, setup warnings (demo price, example reviews, no WhatsApp number, no ONSSA), latest orders.
- Orders: server-side search (name, phone in any format, reference), filters (status, paid/unpaid/unopened, offer, city, date range), sorting, pagination with 10/20/30/40/50 per page (`?size=`, remembered per device), bulk status / paid / delete, CSV export (UTF-8 BOM, formula-injection safe, includes attribution), unseen orders red until opened, "mark all as seen". Order page: call, WhatsApp with template in the customer's language, edit quantities and delivery fee (totals recomputed), status with timeline, internal note, mark paid, printable slip (print CSS), soft delete (owner) with Undo.
- Live: poll unseen orders every 10 s (htmx `hx-trigger="every 10s"`, returns a small fragment + count), red badge, "(n)" in tab title, chime through one shared AudioContext unlocked on first user gesture, bell toggle, notifications closed per browser session.
- Offers and prices, Reviews, Certificates (ONSSA + other types, scan upload, owner only), Settings (WhatsApp number with test link, order phone, name and address AR/FR, delivery fee rules with live preview, WhatsApp templates AR/FR with `{name} {reference} {items} {total}`, notifications email/webhook, section toggles, Marketing: Meta Pixel on/off, Pixel ID, consent bar), all owner-only and audited.
- **Finish** (open in the reference): Admins page (add/remove, roles owner/staff, staff cannot change settings or delete), Audit log viewer (filter by actor, action, date), FAQ CRUD with reorder (up/down buttons, drag optional), Content blocks (hero texts, story, gallery, policies AR/FR).
- Staff vs owner enforced **in the handler layer and re-checked in the store layer** for destructive actions, with tests.

## 14. WhatsApp

One number from settings drives every WhatsApp link (floating button from tablets up, sticky bar, hero, order section, final CTA, footer, confirmation). Empty number hides all of them and shows a dashboard warning. Links built in `domain/whatsapp` (`https://wa.me/<digits>?text=<url-encoded>`), tested. Click counter via `navigator.sendBeacon` to `/api/wa-click` (rate limited, no PII). Floating button: brand green with dark icon, `aria-label` per locale, bottom-end, never overlaps the sticky bar, safe-area aware, tooltip once per session, no pulsing.

## 15. Marketing and analytics

Meta Pixel exactly as the reference (off by default, PageView per page, Lead per order with value/currency/eventID, optional consent bar, privacy page paragraph appears while enabled). The CSP is built per request: Meta domains are added to `script-src`, `img-src`, `connect-src` **only** when the pixel is enabled (and consent given if required). Optional Plausible/Umami behind env flags, same CSP rule.

## 16. Security

Follow [SECURITY.md](SECURITY.md) in full. It is part of this prompt. Highlights: strict nonce CSP, `http.CrossOriginProtection` + session CSRF tokens, `__Host-` session cookie, argon2id with timing-safe login and no user enumeration, TOTP, HKDF-derived keys from one `APP_SECRET`, request body limits on every route, server timeouts, decompression-bomb-safe image pipeline, SSRF-safe webhook client, PII-redacted logs, Postgres rate limits, distroless non-root container, govulncheck + gosec in CI.

## 17. Privacy (Law 09-08, CNDP)

Same draft privacy page as the reference (flagged for lawyer review), phone numbers stored only for orders, IPs and review phones stored only as keyed hashes, a documented retention job (`zaytona` scheduled task or SQL) for rate-limit rows and old session rows, and logs that never contain full phone numbers, names, addresses or IPs.

## 18. Tests

- Unit (table-driven): phone normalization, money format, pricing (auto/fixed/custom litres), delivery fee rules, order totals, reference format, WhatsApp links/templates, review rules, CSV escaping, keyed hash test vector from the reference, argon2 PHC fixture from the reference, CSP builder, config validation, i18n parity.
- Integration (embedded Postgres): schema equivalence with Drizzle migrations, Drizzle adoption path, order creation incl. 25 parallel submits and duplicate guard, rate limits, PoW single use + replay + forged + expired, review moderation, staff vs owner permissions, soft delete + restore, session expiry and rotation, login throttling, CSRF rejection (missing token, wrong token, cross-site `Sec-Fetch-Site`).
- E2E (chromedp, mobile viewport 390x844): order flow AR and FR, admin login + status change, review submit then approve, WhatsApp number change reflected on every button, order draft restored after reload, network failure path shows the recovery box.
- `task audit:mobile` as in section 9. Fuzz tests (`go test -fuzz`) for phone normalization, CSV escaping and the multipart/image decoder entry point.

## 19. Ops and deployment

- `Dockerfile` (provided): multi-stage, `CGO_ENABLED=0`, `-trimpath`, distroless static non-root, read-only root FS, uploads on a volume, `HEALTHCHECK` via `zaytona healthcheck`.
- `docker-compose.yml` (provided): Postgres for local dev, app service optional.
- `deploy/Caddyfile` (provided): HTTPS, zstd/gzip, request size cap, reverse proxy to the app.
- systemd unit example with hardening (`NoNewPrivileges`, `ProtectSystem=strict`, `ReadWritePaths` uploads only, `PrivateTmp`, `CapabilityBoundingSet=`).
- Graceful shutdown (SIGTERM: stop accepting, drain 20 s, flush notification queue).
- Backups: daily `pg_dump` + uploads folder; README explains restore and a monthly restore test.
- README.md (setup on Windows and Linux, env vars, commands, deploy, cutover) and CLIENT_HANDOVER.md (simple Darija and French: what the client must provide, how to use the admin, how to turn on 2FA).

## 20. Milestones

1. **Foundation**: go.mod, Taskfile, tools (fetchtools, i18ngen, assetbuild), config + HKDF keys, httpx middleware (headers, CSP nonce, cross-origin protection, limits, timeouts, recover, logging), baseline migration + Drizzle adoption + schema equivalence test, sqlc setup, embedded Postgres for `db:local` and tests, i18n with typed keys, layout with `/ar` RTL and `/fr` LTR, tokens + fonts, CI green.
2. **Landing page** (read-only data): all sections, settings and content from DB with cache, sticky bar, floating WhatsApp, click counter, policies, SEO + JSON-LD, sitemap, robots, images pipeline. Mobile audit clean.
3. **Orders**: form, PoW, anti-spam, duplicate guard, confirmation, drafts, retry + WhatsApp fallback, attribution, pixel, notifications.
4. **Admin core**: auth (argon2 compat, sessions, CSRF, throttling, forced change, TOTP), shell (bottom tabs / sidebar), dashboard, orders (list, filters, CSV, detail, slip, paid, delete + undo, live polling + chime).
5. **Admin catalogue and settings**: offers and prices, certificates, settings (all groups), admins, audit viewer, FAQ, content blocks.
6. **Reviews**: public + moderation, verified badge, JSON-LD.
7. **Hardening and handover**: full SECURITY.md checklist, fuzzing, e2e, Lighthouse in CI, budgets test, Dockerfile verified, DATA_MIGRATION.md rehearsed on a copy of production, README, CLIENT_HANDOVER.md, every PARITY.md box ticked.

## 21. Acceptance checklist

- [ ] Every item in `docs/PARITY.md` ticked and verified against the reference app side by side.
- [ ] Go app starts against a copy of the production database adopted from Drizzle, with no data change except the goose version table; existing admins log in with their current passwords; existing verified badges still show.
- [ ] Section 9 gates pass (audit 0 findings, Lighthouse scores, budgets, TTFB).
- [ ] Every item in the SECURITY.md checklist is done or has a written, accepted exception.
- [ ] `task check` green in CI; `govulncheck` reports no reachable vulnerabilities.
- [ ] No fabricated claims, numbers, reviews, certificates or prices anywhere.
- [ ] README, `.env.example`, seed, tests, DATA_MIGRATION.md and CLIENT_HANDOVER.md complete.

Start now: read CLAUDE.md, SECURITY.md and docs/PARITY.md, skim the reference repo, write PLAN.md, then implement milestone 1.
