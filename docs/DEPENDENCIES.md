# DEPENDENCIES.md

Every third-party dependency has one line: what it does, why the standard library is not enough, maintenance status at the time it was added. Add the line **before** adding the dependency. Record the exact version you install in `PLAN.md`.

Initial list (from the spec; installed versions are recorded in PLAN.md):

| Module | Purpose | Why not stdlib |
| --- | --- | --- |
| `github.com/jackc/pgx/v5` (>= 5.9.2) | Postgres driver + pool | `database/sql` has no Postgres driver; pgx is the de facto standard. 5.9.2 fixes CVE-2026-41889. |
| `github.com/sqlc-dev/sqlc` (tool) | Generates typed Go from SQL files | Removes hand-written scanning and string-built SQL. Build-time only. |
| `github.com/pressly/goose/v3` | Embedded SQL migrations | Versioning, locking and up/down bookkeeping. |
| `github.com/a-h/templ` | Type-safe HTML components | `html/template` is runtime-typed; templ catches errors at compile time and still auto-escapes. |
| `github.com/evanw/esbuild` | Bundles and minifies the small JS modules | No JS bundler in stdlib; Go API means no Node. Build-time only. |
| `github.com/shopspring/decimal` (+ `jackc/pgx-shopspring-decimal`) | Money arithmetic | `float64` is wrong for money; `math/big` lacks decimal rounding helpers. |
| `github.com/google/uuid` | UUID type for sqlc overrides | Clearer than `pgtype.UUID` in domain code. Drop it if Go 1.27's stdlib UUID support fits sqlc overrides. |
| `github.com/alexedwards/argon2id` or `golang.org/x/crypto/argon2` | argon2id hashing | Not in stdlib. Must verify reference PHC hashes. |
| `github.com/pquerna/otp` | TOTP + otpauth URI | RFC 6238 helpers and QR payload; small and widely used. |
| `github.com/altcha-org/altcha-lib-go/v2` | ALTCHA proof-of-work challenges | Must interoperate with the ALTCHA widget; includes the 2025 splicing/replay fix. |
| `golang.org/x/image` | WebP decode, high-quality resize | Not in stdlib `image`. |
| `github.com/gen2brain/webp`, `github.com/gen2brain/avif` | WebP / AVIF encode, CGo-free (WASM via wazero) | No encoders in stdlib; keeps the binary static. Use build tag `nodynamic`. AVIF encoding is slow (seconds per image): uploads only, in a bounded worker pool. |
| `golang.org/x/text` | Unicode NFC normalization | Not in stdlib. |
| `github.com/microcosm-cc/bluemonday` | HTML sanitizer for admin rich text | Not in stdlib. |
| `github.com/yuin/goldmark` | Markdown to HTML (if policies are stored as Markdown) | Not in stdlib. Skip if the reference stores HTML. |
| `github.com/wneessen/go-mail` | SMTP with TLS and auth | `net/smtp` is frozen and minimal. |
| `github.com/fergusstrange/embedded-postgres` | Real Postgres for `task db:local` and integration tests, no Docker | Tests and Windows dev without Docker. Dev/test only. |
| `github.com/chromedp/chromedp` | Browser automation for e2e and the mobile audit | Replaces Playwright without Node. Test only. |
| `mvdan.cc/gofumpt` (tool) | Stricter formatting | Tooling. |
| `golang.org/x/vuln/cmd/govulncheck` (tool) | Reachable-vulnerability scan | Tooling. |

Front-end files vendored under `web/static/vendor/` (listed with version, source URL and SHA-256 in `web/static/vendor/MANIFEST.txt`): htmx 2.0.x (admin only), ALTCHA widget. Fonts under `web/static/fonts/` with their OFL licenses: Archivo (variable, French) and IBM Plex Sans Arabic (Arabic), one family per locale since the 2026-10-17 redesign (Cairo, Fraunces and Inter were removed).

Design tooling (not shipped, not a Go or runtime dependency): the taste-skill (`github.com/leonxlnx/taste-skill` at f624f37, MIT) is installed for Claude Code in `.claude/skills/design-taste-frontend`. Its Node scripts (`lint.mjs`, `inspect.mjs`, `draw.mjs`) need Node 22+ and a local Chrome; they install nothing and write only to `.taste-skill/` (git-ignored).

External binaries: Tailwind CSS v4 standalone CLI, version and per-platform SHA-256 pinned in `tools/fetchtools` (take the hashes from the release's `sha256sums.txt` and verify them once by hand).

Transitive modules worth knowing: `github.com/lib/pq` (pulled by goose's Postgres dialect; the app itself connects through pgx only).
