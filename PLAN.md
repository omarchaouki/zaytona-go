# PLAN.md

Go rewrite of the Huilerie El Hadaji site. Spec: [PROMPT.md](PROMPT.md), security: [SECURITY.md](SECURITY.md), feature checklist: [docs/PARITY.md](docs/PARITY.md).

## Decisions that change the spec

- **New database, new schema (owner decision, 2026-10-15).** The Go app runs on a fresh PostgreSQL database with its own clean schema (`internal/db/migrations/00001_init.sql`). It does not adopt the TypeScript app's Drizzle database. Consequences:
  - PROMPT.md section 4 items 1 to 3 and 6 (Drizzle baseline, adoption, schema-equivalence test, reference hash test vectors) are dropped.
  - Existing admin passwords, sessions, orders, reviews and verified-purchase hashes are not carried over. Admins are created by `zaytona seed` (owner) and later from the Admins page.
  - Keyed hashes (phone, IP) use HKDF-derived keys (`phone-hash`, `ip-hash`) like every other key, not the reference's raw `APP_SECRET` HMAC.
  - Feature parity with the reference is unchanged: same concepts (litre offers, COD orders, reviews, certificates, settings, content).
- **Reference repo path.** The TypeScript reference lives at `../ZIIIT` (not `../zaytona`).
- **Local DB port 5434, not 5433.** A system PostgreSQL 18 service already listens on 5433 on the dev machine (left untouched).
- **Task** installed with `go install github.com/go-task/task/v3/cmd/task@latest` (it was missing on the dev machine).
- **golangci-lint v2.14.0, not v2.5.0.** v2.5.0 cannot read Go 1.27 export data ("export data version 5 is greater than maximum supported"); the Taskfile pin was bumped.
- **`go test -race` skipped on Windows** (needs cgo and a C compiler, absent on the dev machine); CI on Linux keeps `-race`.
- **Stagger delays as classes.** The reference set `--d` through `style=""`; the strict CSP forbids inline styles, so `app.css` defines `stagger-05` to `stagger-100` (0.05 s steps).
- **Buttons set no display in `btn()`**: callers add `inline-flex` or `hidden sm:inline-flex` (the reference relied on tailwind-merge to drop the conflicting class).
- **Button class overrides are explicit.** The reference merged classes with tailwind-merge (`px-3` beat `px-5`, `whitespace-normal` beat `whitespace-nowrap`). Plain CSS lets the later stylesheet rule win, which made the ONSSA WhatsApp button overflow 360 px screens (caught by the audit). `btn(size, overrides...)` takes `px-*` and `whitespace-normal` as arguments instead.
- **Offer size label** is derived from the litres (`30 L` / `30 لتر`, from the approved `orderForm.litersUnit` key) since the new schema has no free-text size label; the reference showed `30 litres` in French.
- **Native elements instead of Radix**: FAQ is `<details name="faq">` (one open at a time, works without JS), the certificate scan opens in a `<dialog>` (a plain link to the image without JS), the footer map is a link replaced by the iframe on click.
- **WhatsApp click limit 30 per minute per IP** (SECURITY.md) instead of the reference's 30 per hour.
- **Floating WhatsApp tooltip close button is 44 px** (reference 32 px) to pass the tap-target audit.
- **Photos copied, not re-encoded yet.** `web/static/images` holds the reference's AVIF/WebP set (`internal/view/images.json` lists widths and sizes). The `zaytona images` re-encoder comes with the upload pipeline (`internal/media`, milestone 5/6) so one encoder serves both.
- **Order form posts work in two ways.** A plain post returns a full page (303 to the confirmation, or the landing page with values and errors, 422); the script sends with fetch and `Accept: application/json` for the retry and recovery box. The proof of work needs JS, as PROMPT.md allows.
- **Proof of work solved by our own 60-line module** (`web/static/js/lib/pow.js`, `crypto.subtle`), not the ALTCHA widget: the reference did the same with `altcha-lib`'s solver. A Go test pins the derivation it must match.
- **Offer buttons** link to `?offer=<slug>#order` (server preselects without JS); with JS they select in place without reloading.
- **Order line names** are "<offer name> <litres> L" in both languages (snapshotted AR and FR); custom quantities use the approved `orderForm.customLine` text.
- **Confirmation token**: HMAC of the reference with the `confirmation-token` key, 22 characters, 30-day page lifetime.
- **Gzip only, no Brotli** for precompressed assets and HTML: stdlib only; Caddy can add zstd/br at the edge.
- **No htmx (yet).** In milestone 4 the only partial update is the 10 s new-order poll; `admin.js` does it with `fetch` and a server-rendered templ fragment (same CSRF header, same-origin, no eval, no `hx-on`). Every admin form is a plain POST + redirect. Vendoring htmx is reconsidered if later milestones need more partial updates.
- **Admin routes have their own mux**, dispatched before the public one ("/admin/{x}" would overlap with "/{locale}/privacy" in one ServeMux).
- **Marking an order seen is a POST** (sent by `admin.js` when the order page shows, and implied by any action on the order), never a side effect of GET (SECURITY.md section 5). Without JS an order turns "seen" on the first action or with "mark all as seen".
- **Delete confirmation without JS**: the delete forms post `confirm=0` and get a confirmation page; with JS a `<dialog>` asks and posts `confirm=1`.
- **2FA enrollment state**: a pending TOTP secret is stored encrypted in `admins.totp_secret_enc` with `totp_enabled_at` null; only a valid code enables it. Secrets are AES-256-GCM with a key-id byte, key derived from `APP_SECRET` (`totp-encryption`).
- **Recent authentication for 2FA changes** is a password prompt on the Security page (valid 10 minutes, also set by login and by the TOTP step).
- **New UI strings** (2FA, Security page, password-policy messages, ONSSA warning, filter button, timeline labels) were written for this port in AR and FR and are flagged for the client's review: `admin.security.*`, `admin.login.{twoFactor*,recoveryHint,code,verify,codeInvalid,back,expired}`, `admin.password.{tooShort,tooLong,common,containsEmail,same,othersSignedOut}`, `admin.nav.security`, `admin.common.*`, `admin.dashboard.{warnNo2FA,warnNoOnssa}`, `admin.orders.filter`, `admin.event.{note,deleted,restored}`, `admin.order.seen`.
- **Admin editors without JS**: each item's edit form sits in a `<details>`; a failed save shows the page again (422) with the posted values and the messages next to the fields. Deletes post `confirm=0` and get a confirmation page; with JS a `<dialog>` asks first.
- **Staff can read** offers, settings, certificates, FAQ and content (fields disabled), as in the reference; every change is owner-only (handler) and audited. Admins and the audit log are owner-only, reading included. Admin account changes re-check the role and the last-owner rule in the store, under an advisory lock.
- **Password re-check (10 minutes)** before changing the WhatsApp number or the order phone, and before any admin account change; the prompt sits in the page and comes back to it.
- **Upload pipeline** (`internal/media`): sniffed JPEG/PNG/WebP, 6000 px side / 24 MP cap before decoding, EXIF orientation applied (own bounds-checked parser), CatmullRom resize to 480/960/1600 (never upscaled), AVIF (speed 10, q55) + WebP (q78) + JPEG (q82, alpha flattened on white). The 9 encodes of an upload run in parallel through one pool of `NumCPU` slots shared by all uploads, with a queue of 4 images per CPU (beyond: "try again"), 2 min per image; the WASM encoders are compiled at startup. AVIF speed 10 instead of 8: about 4x faster for files ~30% larger (measured: 1600 px AVIF 3 s instead of 5 s on the dev machine).
- **Media fallback is JPEG** (`<img src>`), AVIF and WebP in the `srcset`s; the static photo set keeps WebP (no JPEG copies were in the reference build).
- **Body limit**: 16 KiB for every form, `MAX_UPLOAD_MB` + 64 KiB only for multipart POSTs under `/admin/` (session and CSRF are checked before anything is read).
- **Private uploads**: certificate scans (and later review photos) are served only while their row is public, otherwise only to a signed-in admin with `Cache-Control: private, no-store`; unknown or unlisted names are 404.
- **Upload size hint** shows `MAX_UPLOAD_MB` (`{mb}` placeholder) instead of the reference's fixed "8 Mo".
- **Webhook addresses** with an IP literal are checked against the blocklist when saved (host names stay checked at dial time).
- **Delivery preview** is server-rendered and updated live by `admin.js` from the same sentences as the site, in both languages.
- **Offer photo**: optional upload per offer (the reference reused one stock photo); legacy offers keep the stock photo on the site.
- **Content blocks** (hero, story, policy texts, privacy and terms pages): empty fields fall back to the translation defaults; policy page HTML is filtered by the bluemonday allow-list on save and again on render; "reviewed by a lawyer" is one switch for both languages.
- **Review summary counts real reviews only** (PROMPT.md: example reviews are excluded from every aggregate). The reference's section summary also counted the example reviews; here, while only examples exist, the cards show (with their badge) and the summary stays hidden. The "real customers, checked before publishing" note shows only when no example review is listed.
- **Review form without JS**: the "Write a review" link opens a `<dialog>` with JS and the form page `/{locale}/reviews/new` without it. The proof of work needs JS, so without it the form shows a note instead of failing silently. With JS, the script solves the proof of work and then lets the browser post the multipart form natively; the answer is a full page (thank-you, or the form again with values and messages), not an in-dialog update as in the reference.
- **Review filters and "load more"** are links (`?stars=`, `?rpage=` on the landing, showing the pages cumulatively) that the script turns into fragment fetches (`/{locale}/reviews?page=&stars=`, `?list=1` for a filter).
- **Verified purchase** is decided when the review is sent (the phone is matched to the latest delivered order, then only its keyed hash is kept), as in the reference.
- **Review moderation is open to staff**, deleting is owner-only (handler and store). Moderation and edits refresh the site cache at once.
- **Structured data** lists up to 5 real published reviews (`Review` with first name + initial, never examples) next to the `AggregateRating`.
- **Per-route form allow-lists** (`internal/web/admin/fields.go`): every admin POST route declares its fields (and its one file field); the guard refuses anything else with 400, and registering a POST route without a list panics at startup. The public review form has its own list.
- **No `worker-src blob:`** in the CSP: only the ALTCHA widget needed workers; our proof of work runs in the page.
- **Docker image verified in CI, not locally**: Docker Desktop cannot start on the dev machine; the CI `image` job builds the image, checks non-root/no shell, and boots it read-only against PostgreSQL with the health check.
- **`zaytona images [dir]`** re-encodes source photos (default `assets/source`) into `web/static/images` (AVIF + WebP) and rewrites `internal/view/images.json`; no source photos are in the repository yet (the reference shipped encoded copies only).
- **New UI strings for milestone 5** (flagged for the client's review): `admin.nav.{content,faq,admins,audit}`, `admin.common.*`, `admin.offers.{photo,deleted,rangeError}`, `admin.settings.{legal*,ice,rc,showLegal,email*,hours*,response*,map*,socials*,templates*,template*,notify*,webhook*,toggles*,show*}`, `admin.trust.{viewScan,deleted}`, `admin.faq.*`, `admin.content.*`, `admin.gallery.*`, `admin.admins.*`, `admin.audit.*`.
- **Brand redesign (owner request, 2026-10-17)** with the taste-skill (`leonxlnx/taste-skill` at f624f37, installed in `.claude/skills/design-taste-frontend`, MIT). It replaces the reference's look (PROMPT.md "Type: Cairo + IBM Plex Sans Arabic, Fraunces + Inter", the arch windows, the trust ticker, gradients and glows). Direction "split stage" (seed 273246635):
  - Idea: the litre, the unit the mill sells by, is the measure of the page: the price per litre at figure size in the hero, the offers as a cut price list with dot leaders, and a litre gauge (the one live thing) that fills a drawn bidon and prices any quantity from the real price per litre. Without JS the gauge is a GET form to `?offer=custom&liters=<n>#order`, which preselects the order form.
  - Layout: the first chapter (hero, offers, ONSSA, benefits) is a reading column beside a sticky stage of the mill's photographs that change by chapter; after it every section has its own composition (moss field of reasons, reviews as signed letters, the story as a letter, the steps as a slip, a dark contact panel with the phone number at figure size, FAQ as reading text instead of an accordion, a plain closing sentence, the name returning at full width in the footer).
  - Palette (dealt "sand and moss", kept): sand ground #ddd6c6, bone sheets #e6e4dd, charcoal ink #2a2a26, moss #4f6238 on the action only, oil gold #c8961c for stars and the gauge. Token names are unchanged, so the admin follows.
  - Type: one family per locale, Archivo (variable, Fontsource 5.3.0) in French, IBM Plex Sans Arabic in Arabic; Cairo, Fraunces and Inter removed. Five public steps (small, body, lead, head, figure).
  - House rules: square corners, no gradients, no shadows at rest (only dialogs, menus and the floating button), no eyebrows above headings, no icon cards, headlines two lines or fewer.
  - Copy: new default hero headline (two lines at every width, no superlative): `hero.title`; new keys `products.{gaugeTitle,gaugeHint,gaugeResult,gaugeSubmit,perLiterUnit}`, `nav.{offers,onssa,reviews,faq,main}`. Flagged for the client's review. The hero eyebrow text is now the caption on the stage photograph.
  - Removed from the page: trust ticker, arch windows, olive-branch drawing, per-offer photos in the price list (the order form never showed them; the admin upload stays).
  - Checks: the skill's `lint.mjs` 0 FAIL 0 WARN; `inspect.mjs` on `/fr` and `/ar` at 1440/768/390: only `covered-text` at 390, which is the fixed phone order bar passing over a heading while scrolling (a real overlay the skill allows), and `no-footage` (no clip of this mill exists; a video would also break the hero budget).
- **Olive branding round (owner request, 2026-10-18).** On top of the split-stage redesign, the owner asked for olive green branding, an olive texture, rounded images, a letter-by-letter hero headline, a margin around the hero photo and an "Order" button always on screen on desktop. These override the taste-skill house rules on purpose (rounded corners, background texture):
  - Palette: pale olive ground #edefe0, sheets #f6f7ee, olive black ink #1f2614, olive #556b2f on the action, dark olive bands #3a4b1b and #1d2410, oil gold #c9a227 kept for stars and the gauge.
  - Texture: a drawn 240px tile of olive sprigs (leaves and fruit, alpha baked in) on the public ground (`.olive-texture`, fixed from tablets up so the sticky header shares it) and a light variant on the dark bands (`.olive-texture-dark`).
  - Corners: photographs and sheets 24px, fields 12px, buttons and chips fully round; the receipt slips keep their cut bottom edge.
  - Hero photo: inset in a rounded frame (`.stage-frame`), 1rem from the header and the edges on desktop, 0.75rem on phones.
  - Headline: `lib/letters.js` splits the hero h1 into letters that rise in turn (28ms apart), word by word in Arabic (its letters join), with the whole sentence kept as the accessible name; plain text without JS or with reduced motion.
  - Conversion: the header is sticky from 768px up with its "Commander" button; phones keep the bottom order bar.
- **In-place navigation (owner request, 2026-10-18: "the whole app as a SPA, no refreshing").** Not a client-side SPA (that would drop server rendering, the no-JS forms and the strict CSP): `web/static/js/lib/nav.js` fetches same-site pages and form posts and swaps them in (title, head metadata, `<html>` lang/dir, body), with pushState, back/forward with scroll kept, fragment kept, focus on `#main`, the title announced, and a view transition when motion is allowed. Public pages and the admin both use it.
  - Page code runs through `onPage(fn)` with a per-page `AbortSignal` (window listeners, observers, the dashboard refresh timer are released on leaving); delegated listeners and the admin new-order poll run once per visit and look elements up at each use; CSRF and language are read at use (sign-in changes the token, the language switch changes the locale).
  - Full load instead of a swap when it could be wrong: another part of the site (different stylesheet or scripts: public vs admin), a page with its own `<script>` in the body (Turnstile), a non-HTML response, a network or parse failure. A POST is never sent twice: if its answer cannot be swapped, the browser goes to where it led (or reloads the page).
  - A refused POST answered in place (422) keeps the address of the page it was sent from, so a reload never re-posts. Links with `download` (CSV export) or `data-no-swap`, and `/static`, `/uploads`, `/api` paths are left to the browser.
  - Without JS nothing changes: links and forms are plain.
- **Empty file inputs (bug found while testing in-place posts).** A file input left empty is sent by browsers as a part with `filename=""`, which Go reads as an empty text value: the review form refused every review without a photo (400 "unexpected form field"), and admin forms with an optional photo would have too. The allow-lists now accept an empty value for a declared file field only (`emptyValues` in `internal/web/admin/fields.go`, the `photo` case in `reviewFieldsOK`); tests send what a browser sends.
- **New UI strings for milestone 6** (flagged for review): `reviews.form.noscript`, `admin.nav.reviewsShort`, `admin.reviews.{rejectShort,deleteBody}`, `admin.dashboard.pendingReviews`.

## Stack (installed versions)

| Concern | Module | Version |
| --- | --- | --- |
| Go | `go 1.27.0`, `toolchain go1.27.2` (1.27.0 has 9 reachable stdlib vulns fixed in 1.27.2) | 1.27.2 |
| Postgres driver | `github.com/jackc/pgx/v5` | v5.11.0 |
| Migrations | `github.com/pressly/goose/v3` | v3.29.0 |
| Queries | `github.com/sqlc-dev/sqlc` (go tool) | v1.31.1 |
| Money | `github.com/shopspring/decimal` + `jackc/pgx-shopspring-decimal` | v1.5.0 |
| UUID | `github.com/google/uuid` | v1.6.0 |
| Local/test Postgres | `github.com/fergusstrange/embedded-postgres` (Postgres 16) | v1.34.0 |
| Password hashing | `golang.org/x/crypto/argon2` | v0.58.0 |
| Formatting | `mvdan.cc/gofumpt` (go tool) | v0.12.0 |
| Vulnerabilities | `golang.org/x/vuln/cmd/govulncheck` (go tool) | v1.9.0 |
| Templates | `github.com/a-h/templ` (+ go tool) | v0.3.1070 |
| JS bundling | `github.com/evanw/esbuild` (Go API, build time) | v0.28.2 |
| CSS | Tailwind CSS standalone CLI, sha256-pinned per platform in `tools/fetchtools` | v4.3.3 (same as the reference) |
| Lint | golangci-lint (go install, see Taskfile) | v2.14.0 |
| HTML sanitizer | `github.com/microcosm-cc/bluemonday` | v1.0.27 |
| Proof of work | `github.com/altcha-org/altcha-lib-go/v2` | v2.4.0 |
| Email | `github.com/wneessen/go-mail` | v0.8.1 |
| Browser automation (audit, shots, e2e) | `github.com/chromedp/chromedp` (v0.21 generic API: `Run`/`Do`/`Evaluate[T]`) | v0.21.0 |
| Images | `golang.org/x/image` (WebP decode, CatmullRom resize), `github.com/gen2brain/webp` v0.6.4 and `github.com/gen2brain/avif` v0.6.0 (WASM via `tetratelabs/wazero` v1.12.0, build tag `nodynamic`) | x/image v0.47.0 |
| TOTP | `github.com/pquerna/otp` (+ `boombuler/barcode` for the server-side QR PNG) | v1.5.0 |
| Fonts | Archivo variable (French), IBM Plex Sans Arabic 400/500/700 (Arabic) (Fontsource woff2 files, OFL) | 5.3.0 |

Not installed yet (later milestones): goldmark (policies are stored as filtered HTML, so it may never be needed). htmx: not needed so far (see Decisions).

## Database (new schema)

One migration, `00001_init.sql`. Conventions: uuid ids (`gen_random_uuid()`), `timestamptz`, money `numeric(10,2)` mapped to `decimal.Decimal`, token and keyed hashes as `bytea`, `updated_at` maintained by a trigger, check constraints on every bounded text, partial indexes for the hot admin queries.

| Table | Purpose | Changes vs reference |
| --- | --- | --- |
| `admins` | Owner / staff accounts | + TOTP secret (encrypted), last TOTP step, `password_changed_at`, `updated_at` |
| `admin_recovery_codes` | 2FA recovery codes (hashed, single use) | new |
| `sessions` | Hashed session tokens | `bytea` id, + idle and absolute expiry, `mfa_pending`, `recent_auth_at`, IP stored as keyed hash |
| `settings` | Typed groups (JSON object per key) | same idea, JSON object enforced |
| `content_blocks` | Hero, story, policies, privacy/terms pages AR/FR | same |
| `media` | Uploaded images (re-encoded) | new: replaces free-form URLs; public visibility derived from the owner row |
| `offers` | Litre offers (auto or fixed price) | replaces `products` + `product_images`; litres required; SEO/size-label columns dropped |
| `orders` | COD orders, `OL-YYYY-0NNNNN` from `order_number_seq` (starts 10001) | user agent no longer stored; IP as `bytea` keyed hash |
| `order_items` | Lines with AR+FR name, litres, unit price snapshots | custom quantity = 1 unit of N litres (was quantity = litres) |
| `order_events` | Timeline | + `kind` (created, status, paid, unpaid, edited, note, deleted, restored) |
| `reviews` | Public reviews + moderation | photo via `media`, `language` is the `locale` enum |
| `faqs`, `certifications`, `lab_reports`, `gallery_images` | Content | lab reports no longer tied to a product |
| `whatsapp_clicks`, `audit_logs` | Telemetry | identity ids; audit IP as keyed hash, not `inet` |
| `rate_limits`, `pow_used` | Anti-abuse | `pow_used` is new (ALTCHA single use) |

## Folder structure (so far)

```
cmd/zaytona/            serve | migrate | seed | admin reset-password | healthcheck | version
internal/config/        env parsing + validation, HKDF keys
internal/db/            pool (extended protocol, decimal codec, 5 s statement timeout), embedded goose migrations
internal/db/migrations/ 00001_init.sql
internal/db/queries/    sqlc SQL
internal/db/dbq/        sqlc output (generated, committed)
internal/db/dbtest/     embedded Postgres for tests and db:local
internal/store/         Store (pool + queries + InTx), SiteCache (settings, hero, offers; Invalidate on admin save)
internal/domain/        typed settings with defaults, WhatsApp helpers, Moroccan number/MAD format, *emphasis* titles
internal/auth/          argon2id PHC hashing
internal/security/      keyed hashes, phone redaction, forbidden-pattern guard test
internal/httpx/         request id, recover, real IP, nonce CSP + security headers, CrossOriginProtection, body limit, gzip HTML, redacted request log
internal/i18n/          ar.json + fr.json (verbatim from the reference), keys_gen.go (695 typed keys), T()
internal/assets/        manifest lookup + /static/ handler (immutable hashed files, .gz variants)
internal/view/          templ: layout, chrome (footer, sticky bar, floating WhatsApp), all landing sections, primitives (Picture, ArchWindow, SectionBackdrop, OliveBranch, Stars), policy + 404 pages, safehtml.go (only templ.Raw, bluemonday)
internal/web/           router + middleware chain; public/ handlers (landing data builder, policies, sitemap, robots, JSON-LD, wa-click, 404)
internal/seed/          idempotent seed; data.json exported from the reference seed
internal/notify/        new-order email (go-mail) and SSRF-safe signed webhook, bounded queue drained on shutdown
web/static/             css/app.css (Tailwind entry + reference tokens), fonts/ (+ LICENSES), images/ (reference photo set), favicon.svg, js/landing.js + js/lib/
web/embed.go            embeds web/static/dist
tools/dblocal/          task db:local
tools/fetchtools/       Tailwind CLI download + sha256 check
tools/assetbuild/       Tailwind + esbuild + hashing + .gz + manifest.json (-watch)
tools/i18ngen/          key parity check + keys_gen.go
tests/e2e/cmd/audit/    chromedp mobile audit (task audit:mobile)
tests/e2e/cmd/shots/    per-section screenshots (task shots)
```

## Local database

```
task db:local      # Postgres 16 on 127.0.0.1:5434, data in .data/pg (first run downloads binaries)
task db:migrate
task db:seed
```

`.env` for this: `APP_ENV=development`, `DATABASE_URL=postgres://postgres:postgres@127.0.0.1:5434/postgres`.

## Routes (so far)

| Route | Handler |
| --- | --- |
| `GET /` | 301 to `/ar` |
| `GET /{locale}` | landing (`ar`, `fr`; anything else 404) |
| `GET /static/...` | hashed assets |
| `GET /{locale}/privacy`, `GET /{locale}/terms` | policy pages (admin HTML sanitized with bluemonday; Meta Pixel paragraph while the pixel is on) |
| `POST /api/wa-click` | anonymous WhatsApp click counter (placement + locale), 2 KiB body, 30/min per IP |
| `POST /{locale}/order` | place an order (full page or JSON) |
| `GET /{locale}/order` | back to `/{locale}#order` (refresh after a failed post) |
| `GET /{locale}/order/{reference}?t=` | confirmation (signed token, 30 days, noindex, no-referrer, no-store) |
| `GET /api/pow` | ALTCHA challenge, 30/min per IP, no-store |
| `GET /sitemap.xml` | 6 URLs with hreflang alternates |
| `GET /robots.txt` | disallow `/admin`, sitemap line |
| `GET /healthz`, `GET /readyz` | liveness, DB ping |
| `GET/POST /admin/login`, `GET/POST /admin/2fa`, `POST /admin/logout` | sign-in (throttled 5/email, 10/IP per 15 min, failures only), TOTP or recovery code step |
| `GET/POST /admin/password`, `POST /admin/locale`, `GET /admin/more` | password change (forced on first login), admin language, "More" page without JS |
| `GET /admin/security`, `POST /admin/security/{reauth,totp/start,totp/enable,totp/cancel,totp/disable,recovery,signout-others}` | 2FA and sessions |
| `GET /admin` | dashboard |
| `GET /admin/notifications?skip=` | live fragment polled every 10 s |
| `GET /admin/orders`, `GET /admin/orders.csv` | list (search, filters, sort, pages) and CSV export |
| `POST /admin/orders/{bulk,seen-all,restore}` | bulk status/paid/delete, mark all seen, undo delete (owner) |
| `GET /admin/orders/{id}`, `POST /admin/orders/{id}/{seen,status,paid,edit,note,delete}` | order page and its actions (delete: owner) |
| `GET /admin/offers`, `POST /admin/offers/pricing`, `POST /admin/offers[/{id}]`, `POST /admin/offers/{id}/delete` | price per litre (re-prices automatic offers), offers with photo |
| `GET /admin/settings`, `POST /admin/settings/{identity,contact,delivery,templates,notifications,toggles,marketing}` | settings groups |
| `GET /admin/certificates`, `POST /admin/certificates[/{id}]`, `POST /admin/certificates/{id}/delete` | certificates with scan |
| `GET /admin/content`, `POST /admin/content/{hero,story,policies,page_privacy,page_terms}`, `POST /admin/gallery[/{id}[/move,/delete]]` | texts and gallery |
| `GET /admin/faq`, `POST /admin/faq[/{id}[/move,/delete]]` | questions with reorder |
| `GET /admin/admins`, `POST /admin/admins`, `POST /admin/admins/{id}/{role,reset,disable,enable}` | accounts (owner) |
| `GET /admin/audit?actor=&action=&from=&to=&page=` | audit log (owner) |
| `GET /{locale}/reviews?page=&stars=[&list=1]` | reviews fragment ("load more", star filter) |
| `GET /{locale}/reviews/new[?sent=1]` | review form page (no JS, errors) and thank-you |
| `POST /{locale}/reviews` | submit a review (multipart, optional photo) |
| `GET /admin/reviews?status=&q=&page=`, `POST /admin/reviews/bulk`, `POST /admin/reviews/{id}/{approve,reject,pin,unpin,edit,delete}` | moderation (delete: owner) |
| `GET /uploads/<id>-<width>.<avif|webp|jpg>` | uploaded images (immutable when public; private scans to admins only) |
| anything else | 404 page in the locale of the path |

## Milestones

1. Foundation (done, see Status).
2. Landing page (done except reviews list and uploads, see Status).
3. Orders (done, see Status).
4. Admin core (done, see Status).
5. Admin catalogue and settings (done, see Status).
6. Reviews (done, see Status).
7. Hardening and handover (done except the items listed open in Status).
8. Brand redesign with the taste-skill (owner request, see Decisions and Status).

## Status

- [x] **Milestone 1: foundation**
  - [x] go.mod, pinned tools (sqlc, templ, gofumpt, govulncheck as `go tool`; Tailwind via fetchtools; golangci-lint and Task via go install)
  - [x] New schema `00001_init.sql`, sqlc queries for every table, generated `dbq`
  - [x] `internal/config` with validation and HKDF-derived keys
  - [x] `zaytona serve | migrate | seed | admin reset-password | healthcheck | version`, graceful shutdown (20 s drain)
  - [x] Embedded Postgres (`task db:local`, integration tests)
  - [x] Seed ported from the reference (3 demo offers at 90 MAD/L flagged, 4 example reviews, 10 demo FAQs, draft privacy/terms, owner with forced password change)
  - [x] Local database created on 2026-10-15: embedded Postgres 16 on :5434, migrated to version 1 and seeded (no owner yet: set ADMIN_EMAIL / ADMIN_PASSWORD in `.env` and run `zaytona seed` again)
  - [x] httpx middleware with a per-request nonce CSP (exact policy tested per feature combination), security headers, admin no-store/noindex, HSTS only over https, CrossOriginProtection, 16 KiB body limit, panic page with request id, redacted request logs
  - [x] i18n: reference JSON verbatim, generated typed keys, tests for key parity, placeholders and Western digits
  - [x] Layout: `/ar` RTL, `/fr` LTR, `/` 301, hreflang + x-default + canonical, OG basics, 2 fonts preloaded per locale, skip link, sticky header with language switch keeping the query, hero text + price anchor from the DB, trust ticker, minimal footer, 404 page
  - [x] Design tokens and components ported from the reference `app.css`; self-hosted fonts with unicode-range; landing CSS 9 KB gzip
  - [x] Checked in the browser at 375 and 320 px, AR and FR: no horizontal scroll, no text under 12 px, no tap target under 44 px, no console or CSP errors
  - [x] Tests: config, argon2id, WhatsApp, settings, money format, emphasis, httpx (CSP, headers, real IP, cross-origin, body limit, recover, gzip, log redaction), i18n, router integration (redirect, 404s, lang/dir, hreflang, nonce = CSP, no inline style/handlers), DB integration, seed, forbidden-pattern guard
  - [x] golangci-lint v2.14.0: 0 issues; govulncheck: no reachable vulnerabilities
  - [x] `task check` steps fmt, generate (no diff), vet, lint, test, vuln all green on 2026-10-15
  - [ ] `go mod verify` hung locally (40+ min of CPU, no disk reads; likely antivirus on the module cache), left to CI
  - [ ] CI not run yet (no GitHub repository); `task audit:mobile` and e2e arrive with chromedp in milestone 2
- [ ] **Milestone 2: landing page** (done except the parts listed as open below)
  - [x] Every section ported from the reference to templ, same order and copy: hero (golden-hour background preloaded as LCP, arch with crossfading photos, olive branch, price anchor, CTAs, trust row, rating snippet from real reviews only), trust ticker, offers (cards from the DB, price per litre, "other quantity" card, `#order` links that also remember the offer), benefits, reasons (data-driven cards), ONSSA section (seal, number/dates, scan dialog or WhatsApp request), why us (policy texts from Admin content first, delivery rule wording, contact facts, legal ids, delivered counter behind its toggle), how to order, story + process + gallery, order section (WhatsApp/phone until the form in M3), FAQ, final CTA, full footer (address, map on click, phones, WhatsApp, email, hours, socials, legal links, ONSSA number, year in Morocco time)
  - [x] Sticky phone bar (Order + WhatsApp), floating WhatsApp from tablets with a once-per-session tooltip, every WhatsApp link from the one Settings number and hidden without it (tested on 9 placements)
  - [x] `landing.js` (1.7 KB gzip): reveals with stagger, sticky bar, click beacon, tooltip, offer pick, map on click, dialog, loading line on navigation and form submit
  - [x] Site data cached in memory (settings, content, offers, certificates, FAQ, gallery, review aggregate, counters) with `Invalidate()` for admin saves
  - [x] Policy pages, sitemap.xml, robots.txt, OG image per locale, JSON-LD (LocalBusiness, Product for non-demo offers, FAQPage, AggregateRating from approved non-demo reviews), `<`/`>`/`&`/U+2028/U+2029 escaped (tested)
  - [x] CSP-safe port: no `style=""` anywhere (stagger, slide, backdrop position, glint and clip-path became classes); guard test stays green
  - [x] `task audit:mobile` (chromedp port of the reference audit + fixed-element overlap check, with `-selftest` proving the checks fire): **0 findings** on `/ar`, `/fr`, privacy and terms at 320, 360, 390, 412, 740x360, 768x1024, 1024x768, with and without a WhatsApp number
  - [x] Budgets so far: landing HTML 18.8 KB gzip (limit 60), CSS 14.9 KB gzip (limit 30), JS 1.7 KB gzip (limit 25), 2 preloaded fonts
  - [x] Tests: landing sections, JSON-LD excludes demo data, WhatsApp number on every button, policies, sitemap, wa-click validation + body limit + rate limit, sanitizer, phone normalization (+ fuzz), JSON-LD escaping
  - [ ] Reviews section (summary, cards, form): milestone 6 (the hero/reasons rating and JSON-LD already read the real-review aggregate)
  - [ ] `/uploads/...` serving for admin images (offer photos, certificate scans, gallery): milestone 5 with `internal/media`
  - [x] Meta Pixel + consent bar: done in milestone 3
  - [ ] `zaytona images` re-encoder: with `internal/media`
  - [ ] PARITY.md boxes not ticked yet: they need a side-by-side session with the reference app running (code was ported section by section, not compared in two browsers)
- [x] **Milestone 3: orders**
  - [x] Order form ported from the reference: 3 steps (quantity, details, review and send) with stepper, per-step validation, focus on the step heading, edit links, server errors routed to their step; offer cards or "other quantity" in litres (limits and price per litre from Settings); Moroccan phone normalized to E.164 (Arabic-Indic digits included, fuzzed); searchable city list (83 cities, accent and Arabic-variant tolerant, free text allowed); live total with the delivery rules
  - [x] Works without JS as a plain post (values and errors re-rendered); with JS: fetch, one automatic retry, then a recovery box (details kept, Retry, Send by WhatsApp with the order filled in); draft kept 7 days (values + step) and cleared on success
  - [x] Server: validation first (a typo does not burn the proof of work), honeypot, signed render-time token (3 s to 24 h), optional Turnstile, ALTCHA v2 proof of work single use (`pow_used`), rate limits 5 per 10 min per IP and per phone (keyed hashes), prices from the DB only, duplicate guard behind a per-phone advisory lock with "send anyway"
  - [x] Confirmation page: drawn check, reference ticket with copy, summary, WhatsApp confirm with the admin template, phone; signed link, 30 days, noindex, no-referrer, no-store
  - [x] Attribution: first-touch utm_* + fbclid kept 30 days on the device, cleaned and stored in `orders.attribution`
  - [x] Meta Pixel (off by default): PageView, Lead once per reference with value/currency/eventID, optional consent bar; Meta origins in the CSP only while on; privacy page paragraph while on
  - [x] Notifications after commit from a bounded queue: email (SMTP_URL, Settings recipient) and webhook (Settings or ORDER_WEBHOOK_URL; HTTPS only in production, private/loopback/link-local/CGNAT/metadata ranges refused in the dialer, IPv4-mapped included, no redirects, 5 s, 64 KiB, `X-Zaytona-Signature: sha256=<HMAC>`); drained on shutdown
  - [x] Tests: order rules, totals, attribution, form token, PoW (single use, replay, forged, tampered, expired, browser derivation), store (25 parallel submits from one phone = 1 order + 24 duplicate warnings; 25 phones = 25 references), handlers (full page and JSON, confirmation token, bots, validation re-render, custom litres, duplicate + send anyway, unavailable, IP rate limit, pixel gating, `?offer=`), notify (SSRF blocklist, loopback refused at dial time, signature, redirects, queue)
  - [x] Browser e2e (`task e2e`, 390x844): order in Arabic and French to the confirmation page, draft restored after reload, network failure shows the recovery box and Retry succeeds
  - [x] `task audit:mobile`: 0 findings on landing (all 3 form steps), privacy, terms and (with `AUDIT_ORDER=1` on a throwaway DB) the confirmation page, AR and FR, 7 sizes
  - [x] Landing JS 8.5 KB gzip (city list included); golangci-lint 0 issues; govulncheck 0 reachable
  - Note: e2e and `AUDIT_ORDER=1` place real orders: run them against a throwaway database (locally a `zaytona_e2e` database on the same embedded Postgres, server on :3001)
- [x] **Milestone 4: admin core**
  - [x] Sessions: 256-bit token, SHA-256 stored, `__Host-zaytona_session` over https (`zaytona_session` on http://localhost), HttpOnly, SameSite=Lax; 7 days sliding (touched at most every 5 min), 30 days absolute; rotated on login, 2FA step, password change, 2FA on/off; password change signs out every other session; "sign out other devices"
  - [x] Per-session CSRF token on every admin POST (form field `csrf` or `X-CSRF-Token`), constant-time; `http.CrossOriginProtection` stays in front
  - [x] Login: timing-safe (dummy argon2 for unknown emails), one generic error, throttled per email (5) and per IP (10) per 15 min counting failures only, every failure audited, rehash on login, safe `next` (same-site `/admin` paths only)
  - [x] Forced password change on first login (12 to 128 chars, embedded common-password list, not the email name, not the current one); navigation hidden until done
  - [x] TOTP 2FA (RFC 6238, 30 s, 6 digits, +/-1 step, each step used once): server-side QR PNG, encrypted secret, activation only after a valid code, 10 single-use recovery codes shown once and stored hashed, code step throttled (5 per 15 min), password re-check within 10 minutes before 2FA changes, owner dashboard warning while 2FA is off
  - [x] Admin shell ported: sidebar on computers, top bar + bottom tabs + "More" sheet (`<dialog>`, a page without JS) on phones, admin language per account (POST), view site, sign out; tabs only for pages that exist
  - [x] Dashboard: money card (collected, this month, delivered unpaid), new / to deliver with cash to collect / today / delivered / total / WhatsApp clicks 30 days, setup warnings (2FA, WhatsApp number, demo price, demo offers, example reviews, ONSSA), 30-day chart as inline SVG (no style attributes), latest 6 orders; reloads every 30 s while visible
  - [x] Orders list: search (name, reference, phone in any Moroccan format), filters (status, paid/unpaid/unopened, offer, city, date range), sort on date/customer/city/total/status, 10 to 50 per page remembered per device, unopened orders in red, bulk status/paid/delete with selection count, mark all seen, CSV export (UTF-8 BOM, formula-safe cells, attribution columns, audited with row count and filter names only); all as GET/POST forms that work without JS
  - [x] Order page: customer card (call, WhatsApp with the customer template in the order's language, city, language, source, address, note), items with editable quantities and delivery fee (totals recomputed from stored unit prices, at least one line kept), status with note, timeline with actor, internal note, paid toggle with date, printable packing slip, soft delete (owner) with confirmation and Undo
  - [x] Roles: owner/staff checked in the handler; delete and restore re-checked in the store (`ErrForbidden`); tested
  - [x] Audit rows: login, failed login, throttled login, logout, password change (and failures), 2FA on/off, recovery code use and regeneration, sign out others, locale, order status/paid/unpaid/edit/note/delete/restore/seen/seen-all, CSV export
  - [x] Live notifications: 10 s poll of a server-rendered fragment, red badges and "(n)" in the tab title, newest unseen order card (slim bar on phones), close per browser session, chime through one shared AudioContext unlocked on the first gesture, bell toggle remembered per device; swapped only when the content changes
  - [x] Tests: auth units (secret box, TOTP drift and replay window, recovery codes, password policy, tokens/CSRF); handler integration (redirects without session, no-store/noindex, CSRF refusals, generic login errors, throttling per email and per IP, open-redirect, forced password change with every policy error and session rotation, 2FA enrollment/login/replay/recovery single use/disable, recent-auth gate, sign out others, locale); orders (search by name/phone/reference, filters, wildcard input, sort, invalid params, GET changes nothing, seen POST, notifications fragment and skip, status/paid/edit/note, staff forbidden in handler and store, bulk status, delete with and without confirmation, undo, CSV BOM/formula/attribution/audit)
  - [x] Browser e2e (`TestAdminFlow`, 390x844, `E2E_ADMIN_EMAIL`/`E2E_ADMIN_PASSWORD`): sign in, "(n)" title from the poll, bulk bar on check, opening an order lowers the unseen count, status change, delete dialog cancel
  - [x] `task audit:mobile` now also audits the admin (`AUDIT_ADMIN_EMAIL`/`AUDIT_ADMIN_PASSWORD`, `-admin-only`): login, dashboard (+ "More" sheet, warnings open), orders (+ bulk bar, delete dialog), order page and its edit form, security, password, more; AR and FR, 7 sizes: **0 findings**. Audit rules refined: ellipsis truncation is not overflow; an open modal dialog (top layer over an inert page) is not a fixed-element overlap
  - [x] Admin JS 3.1 KB gzip
  - [x] `task check` steps on 2026-10-17: fmt, generate, vet, golangci-lint (0 issues; gosec redirect/cookie/int findings annotated where the value is validated or bounded, contextcheck excluded for templ components in `internal/web/admin`), all tests green, govulncheck 0 reachable (`go mod verify` still left to CI)
  - [x] Public audit rerun with the refined rules: still 0 findings (the `AUDIT_ORDER=1` confirmation pass hit the 5-orders-per-10-min IP limit right after the e2e run; the confirmation page did not change in this milestone)
  - [ ] Not audited by the tool: the 2FA code page and the recovery-codes panel (need a 2FA-enabled test admin); checked by hand in the browser at 375 px
  - [ ] PARITY.md boxes still need the side-by-side session with the reference
- [x] **Milestone 5: admin catalogue and settings**
  - [x] Upload pipeline `internal/media` + `/uploads/` serving (nosniff, `default-src 'none'`, immutable when public, 404 for anything not matching `<id>-<w>.<ext>`), 9 variants per photo, files and rows removed when nothing uses them; `zaytona images` for the static set; fuzzed decoder (`FuzzDecodeUpload`)
  - [x] Offers and prices: price per litre (re-prices every automatic offer in the same transaction, clears the demo flag), "other quantity" switch and limits, offer CRUD (litres, names, taglines, auto or fixed price with the live formula, visible/hidden, order, optional photo, remove photo), delete with confirmation
  - [x] Settings: name and address AR/FR + legal ids; WhatsApp number (test link) and order phone (password re-check), contact e-mail, hours, response time, Google Maps link and embed, social links (https only); delivery rules (flat, free above, always free, confirmed by phone) with times and a live AR/FR preview; WhatsApp templates AR/FR; new-order e-mail and webhook (blocklisted addresses refused); section toggles; Meta Pixel (id checked, consent bar); each group audited with old and new values, phone numbers masked
  - [x] Certificates: ONSSA/organic/IGP/ISO/other, number, labels, dates, scan upload, public switch, "visible on the site" only with number + scan + public; scans private until public
  - [x] FAQ: create, edit, hide, delete, move up/down; content blocks (hero, story, policy texts, privacy and terms pages with sanitized HTML and the lawyer-review switch); gallery (upload, captions, order, delete)
  - [x] Admins (owner): add with a one-time temporary password (forced change at first login), change role, new temporary password (sessions ended), disable/enable (sessions ended); never the actor's own account, never the last active owner; audit log viewer filtered by admin, action and dates, 50 per page
  - [x] Navigation: Offers joins the bottom tabs; certificates, content, FAQ, settings, admins and audit (owner) in "More" and the sidebar
  - [x] Tests: media (sniffing, size and pixel caps with a forged PNG header, truncated files, EXIF rotation, widths, 9 files written, no EXIF kept, serving rules, private images, removal) + fuzz; handlers (pricing and re-pricing, min/max error keeping values, offer with photo shown on the site, non-image refused, fixed price + photo removal deletes files, delete confirmation, audit rows, staff read-only and refused; settings re-auth for numbers, masked audit, invalid inputs per group, delivery rule and pixel on the site; scan private then public then gone; FAQ order and site; hero on the site; HTML sanitized on save; gallery add/delete; admins add/duplicate/temporary login/self-protection/disable ends sessions/role/reset/stale auth/staff refused; audit filter); CSV escaping moved to `domain.CSVCell` with `FuzzCSVEscape`
  - [x] Browser e2e: `TestAdminDeliveryPreview` (live preview while typing) next to `TestAdminFlow`
  - [x] `task audit:mobile` admin pages now include offers, settings, certificates, content, FAQ, admins and audit, with every editor opened: 210 page/size checks, AR and FR, **0 findings**
  - [x] Checked in the browser: offers, settings (live preview in both languages), certificates
  - [x] `task check` steps on 2026-10-17: fmt, generate, vet, golangci-lint 0 issues, all tests green (now with `-tags nodynamic`, like the release build), govulncheck 0 reachable; browser e2e green; public audit rerun: 0 findings; fuzz task names aligned (`FuzzDecodeUpload`, `FuzzCSVEscape`)
  - [ ] Not covered by the browser tools: picking a file in the upload fields (covered by the handler tests with real multipart uploads)
  - [ ] PARITY.md boxes still need the side-by-side session with the reference
- [x] **Milestone 6: reviews**
  - [x] Public section after the ONSSA band (hidden with the "show reviews" switch, routes 404 then): average with partial stars and per-star bars from real reviews only, bars filter the list, cards (first name + initial, city, stars, verified badge, date in Morocco time, example badge with its note, photo with a lightbox `<dialog>`, the cooperative's reply in the page language with the other as fallback), pinned first then newest, 6 per page with "load more", empty state
  - [x] Form: native radio stars (CSS `:has`, keyboard focus ring), name, optional city, comment 10 to 600, optional photo (5 MB, re-encoded like the admin uploads), optional phone (never shown, only its keyed hash; links a delivered order), honeypot, signed render-time token, optional Turnstile, ALTCHA proof of work, 3 per hour per IP; values and messages kept on errors; pending until moderated; photo private until the review is published
  - [x] Moderation (`/admin/reviews`): tabs pending / published / refused / all with counts, search, bulk publish/refuse, refuse with an internal reason, pin/unpin, fix the text (customer's original kept and shown), replies AR/FR, owner-only delete with confirmation (photo files removed), verified order reference, gold pending badge in the navigation and a "reviews to check" counter on the dashboard; every action audited
  - [x] JSON-LD: `AggregateRating` and up to 5 `Review` objects from real published reviews
  - [x] Tests: domain (validation, display name, language detection); handlers (full submission with a photo and a delivered-order phone, phone not stored in clear, pending review and photo hidden, staff publish/pin/edit with original kept, site shows name + initial, verified badge, reply, photo and `Review` JSON-LD, refused review and reason hidden, staff cannot delete, owner deletes with photo files, audit rows; validation messages with values kept, honeypot, missing proof of work, too-fast token, invalid photo, 3 per hour; example reviews out of the summary and JSON-LD, real summary and filter links, star filter, fragments, switch off hides everything)
  - [x] Checked in the browser at 375 px: section, dialog, star input, a real submission (proof of work then native post, thank-you page), moderation page
  - [x] `task audit:mobile` now covers the review form page, the landing with the review dialog open, and `/admin/reviews` (all statuses, bulk bar and editors open): 0 findings after two fixes (star row 8 px too wide at 320 px on the form page; the order step slide-in ignored `prefers-reduced-motion`, now off when reduced motion is asked). New `-page` flag reruns one public page (`-page=landing`, `-page=/reviews/new`; in Git Bash prefix `MSYS_NO_PATHCONV=1`)
  - [x] Migrations now run on their own short-lived pool without the 5 s `statement_timeout` of the application pool (found when a migration timed out in a loaded test run; a long index build in production would have failed the same way)
  - [x] `task check` steps on 2026-10-17: fmt, generate, vet, golangci-lint 0 issues (the templ-closure `contextcheck` exclusion now covers `internal/web/public` too), all tests green, govulncheck 0 reachable, browser e2e green
  - [ ] PARITY.md boxes still need the side-by-side session with the reference
- [ ] **Milestone 7: hardening and handover** (done except the open items below)
  - [x] SECURITY.md checklist (each item with its tests):
    - [x] Headers + nonce CSP per feature combination (`internal/httpx` tests; `worker-src blob:` removed)
    - [x] CrossOriginProtection + admin CSRF tokens + no GET mutations (`TestCrossOriginRejectsCrossSitePost`, `TestAdminLoginAndLogout`, `TestGETNeverChangesData` over 30 admin and public pages)
    - [x] argon2id + rehash + timing-safe login + throttling (`internal/auth` tests, `TestLoginTimingUnknownVsKnown`, `TestAdminLoginThrottle`); reference hash compatibility is an accepted exception (new database)
    - [x] `__Host-` cookie, rotation, idle + absolute expiry (`TestAdminLoginAndLogout`, `TestAdminForcedPasswordChange`, `TestSessionIdleAndAbsoluteExpiry`)
    - [x] TOTP + recovery codes + recent-auth for sensitive changes (`TestAdminTwoFactor`, `TestAdminTwoFactorNeedsRecentAuth`, `TestAdminSettings`, `TestAdminAdminsAndAudit`)
    - [x] HKDF keys, TOTP secrets encrypted (`internal/config` tests, `TestSecretBox` in `internal/auth`)
    - [x] Body limits, server timeouts, panic recovery, real-IP rules (`internal/httpx` tests; multipart caps per route; unknown fields and extra files refused: `TestAdminRejectsUnknownFormFields`)
    - [x] Image pipeline guards + fuzz test (`internal/media` tests, `FuzzDecodeUpload`, `TestPolyglotUploadIsReencoded`)
    - [x] PoW single use, honeypot, form token, Postgres rate limits (`internal/security`, order and review tests)
    - [x] SSRF-safe webhook client + signed payloads (`internal/notify` tests; IP literals now also refused when saved)
    - [x] PII-redacted logs, audit coverage, retention job (`TestLoggerDropsSensitiveQuery` in httpx and the redaction helpers' tests, audit assertions in the admin tests, `TestCleanupRetention`; daily job in `serve`)
    - [x] govulncheck, gosec, vendored-file hashes (`web/vendor_test.go` over `web/static/vendor/MANIFEST.txt`), pinned Tailwind checksum, distroless non-root image (CI `image` job)
    - [x] Accepted exceptions written in SECURITY.md (to be accepted by the owner)
  - [x] IDOR: unknown or malformed ids answer 404 on every admin action and page (`TestUnknownIDsAre404`), confirmation pages need their token
  - [x] Budgets enforced by tests: landing JS 9.3 KB, CSS 19.6 KB, admin JS 3.6 KB gzip (budgets 25/30/25), landing HTML 24.8 KB gzip (60), 2 preloaded fonts, hero 960 AVIF 34 KB (120); CI runs the asset budgets after the build
  - [x] CI: first-login tool (`tests/e2e/cmd/firstlogin`) so e2e and the admin audit run with the seeded owner; `image` job (build, non-root, no shell, read-only run, migrate, seed, health check); size budgets step
  - [x] README (setup on Windows and Linux, configuration, commands, architecture, VPS and Docker deployment, backups and restore with a monthly restore test, webhook signature with `zaytona admin webhook-key`, `APP_SECRET` rotation), DATA_MIGRATION.md (what moves with the new database, cutover checklist, rehearsal), CLIENT_HANDOVER.md (Darija and French: what to provide, login and 2FA, orders, reviews, prices and settings, staff, security tips)
  - [x] PARITY.md: every item checked against the reference source and in the Go app (method written at the top of the file)
  - [x] `task check` steps on 2026-10-17: fmt, generate, vet, golangci-lint 0 issues, all tests green (16 packages), govulncheck 0 reachable; browser e2e green
  - [ ] Lighthouse mobile: configured in CI, not run yet (no repository / CI run)
  - [ ] Docker image: built and run only in CI (Docker Desktop does not start on the dev machine)
  - [ ] Two-browser session with the reference app running, and the optional hero video (PARITY.md "Still open")
  - [ ] `go mod verify` left to CI (hangs on the dev machine); `generate:check` compares against git, which has no commit yet
  - [ ] Owner: accept or reject the exceptions in SECURITY.md; review the new UI strings listed in "Decisions"
- [ ] **Milestone 8: brand redesign with the taste-skill** (owner request, 2026-10-17; see Decisions)
  - [x] taste-skill installed in `.claude/skills/` (design-taste-frontend and redesign-existing-projects, MIT, source and commit in `SOURCE.txt`); scripts reviewed before use (network calls only to open image collections, a headless Chrome)
  - [x] Direction dealt and adopted ("split stage"); palette, type, tokens and house rules applied site-wide (public and admin through the shared tokens)
  - [x] Landing rebuilt (`internal/view/landing.templ`), header and footer (`layout.templ`, `chrome.templ`), order section and form, reviews, confirmation, 404 and policy pages restyled; `Stage` component and `lib/stage.js` (photo by chapter), `lib/gauge.js` (litre gauge)
  - [x] Fonts: Archivo variable added to `web/static/fonts` with its OFL and a MANIFEST hash; Cairo, Fraunces and Inter removed
  - [x] Tests: `TestGaugePrefillsCustomLiters` (no-JS path, digits only), landing and review assertions moved to stable markers, e2e `TestLitreGaugeFillsOrderForm` (JS path), e2e taps scroll the target to the middle first (the phone order bar covers the bottom edge, as for a thumb)
  - [x] Skill gate: `lint.mjs` 0 FAIL 0 WARN on the stylesheet and scripts; `inspect.mjs` `/fr` and `/ar`: remaining `covered-text` is the fixed phone order bar, `no-footage` defended (Decisions)
  - [x] Mobile audit: all public and admin pages, AR and FR, 7 sizes: 0 findings after the fixes (price rows and gauge spilled, FAQ heading at 1024 px); a horizontal overflow in Arabic from the ONSSA seal's stamp animation was found by the skill's screenshots and fixed
  - [x] Budgets: CSS 16 KB gzip (was 19.6), one preloaded font in French, two in Arabic
  - [ ] Client: review the new hero headline and gauge strings; replace the stock photographs with the mill's own when available (the stage reads best with portrait photos of at least 1000 px height)
  - [x] In-place navigation for the whole app (public and admin), see Decisions; e2e `TestInPlaceNavigationPublic` and `TestInPlaceNavigationAdmin` (a page-memory marker proves no reload: footer link, back, language switch, `#order` from another page, sign-in post, order list, back/forward); full e2e suite green (8 tests); review sent in place checked in a headless browser
  - [x] Fixed: reviews without a photo and admin forms with an empty optional photo were refused (400); tests added
  - [ ] Lighthouse after the redesign (CI)
  - [x] Olive branding round (2026-10-18): palette, olive texture, rounded photos and sheets, inset hero photo, letter-by-letter headline, sticky desktop header with "Commander" (see Decisions)
  - [x] Offers, steps and footer rebuilt (owner: "poorly designed", 2026-10-18): offer cards with a jerrycan filled to scale against the largest offer (`OfferCard.Fill`, `fill-NN` classes) and their own order button (the dotted price list is gone); the three order steps as illustrated cards on a dark olive band joined by a path, closed by a gold "you pay at the door" line; a dark olive footer with the olive texture (mill, ONSSA badge, Order and WhatsApp, contact, address and map, legal links, the name large behind the last line; the contact column hides when no contact is set). The skill's inspector flags the step cards as `icon-cards`: kept on purpose, the owner asked for a richer design. Mobile audit 0 findings (landing, privacy, review form), e2e green
