# CLAUDE.md

Go rewrite of the Huilerie El Hadaji site (bilingual AR RTL / FR LTR landing page, cash-on-delivery litre orders, reviews, admin panel). Full spec: `PROMPT.md`. Security spec: `SECURITY.md`. Feature checklist: `docs/PARITY.md`. Progress: `PLAN.md` (keep its Status section current).

## Reference implementation

The TypeScript app lives at `../ZIIIT` (read-only). It defines the expected behavior, copy (i18n JSON) and design tokens. Its database schema (`drizzle/`) is only a guide for the data concepts. When unsure how something should behave, read the reference before guessing. Never write into `../ZIIIT`.

## Commands (Task, cross-platform)

```
task tools            # download pinned Tailwind CLI (checksum verified), install go tools
task generate         # templ + sqlc + i18n keys
task dev              # db must be running; templ watch + tailwind watch + server on :3000
task db:local         # embedded Postgres on :5434 (no Docker), data in .data/pg
task db:migrate       # goose up (adopts a Drizzle database if detected)
task db:seed          # idempotent seed, demo data flagged
task test             # unit + integration (embedded Postgres)
task e2e              # chromedp scenarios (needs a running server)
task audit:mobile     # chromedp mobile audit, exits non-zero on findings
task lint             # golangci-lint v2 (incl. gosec)
task vuln             # govulncheck
task check            # fmt check + generate check + vet + lint + test + vuln (run before saying "done")
task build            # assets + release binary in ./bin
```

## Architecture map

`cmd/zaytona` (subcommands) → `internal/web/{public,admin}` handlers → `internal/store` (sqlc in `internal/db/dbq`) and `internal/domain` (pure logic) → `internal/view` (templ). Cross-cutting: `internal/httpx` (middleware, CSP), `internal/auth`, `internal/security`, `internal/media`, `internal/notify`, `internal/i18n`, `internal/config`.

## Hard rules

Security (details in SECURITY.md):
- All SQL lives in `internal/db/queries/*.sql` and goes through sqlc. Never build SQL with `fmt.Sprintf` or `+`.
- Never call `templ.Raw` outside `internal/view/safehtml.go` (bluemonday-sanitized input only).
- No inline scripts without the request nonce, no inline event handlers, no `style=""` attributes, no `eval`, no `hx-on`.
- Every handler that reads a body uses `http.MaxBytesReader`. Every outbound call has a context timeout.
- Every admin mutation: role check in the handler, CSRF token check, audit log row. Destructive store methods re-check the role.
- Never log phones, names, addresses, notes, tokens or IPs in clear. Use the redaction helpers.
- Never read or print `.env`. Use `.env.example` for variable names.
- New dependency: justify it in `docs/DEPENDENCIES.md` first. Prefer the standard library.

Mobile and UI:
- Design at 360 px first. Logical properties only (`ms-`, `me-`, `ps-`, `pe-`, `start-`, `end-`); never `ml-`, `mr-`, `left-`, `right-`, `text-left`, `text-right`.
- Tap targets >= 44x44 px, input font size >= 16 px, text >= 12 px, no horizontal scroll at 320 px.
- Every form works without JS (`method="post"`, full-page response). JS/htmx only enhance.
- Respect `prefers-reduced-motion` and `Save-Data`.
- Run `task audit:mobile` after any UI change, in both languages.

Content:
- Never invent reviews, ratings, counts, prices, certificate numbers or claims. Placeholders are `TODO_CLIENT`; demo rows are flagged `is_demo`.
- UI strings only in `internal/i18n/ar.json` and `fr.json`, ported from the reference. Western digits in both locales.
- Do not use the em dash character in UI copy, docs, comments or commit messages.

Database:
- The Go app uses its own new schema on a fresh database (owner decision, see PLAN.md "Decisions"). No Drizzle adoption, no compatibility with the reference's data or hashes. Changes come as new goose migrations; no destructive migration without an explicit request.
- Money is `decimal.Decimal`, never float.

## Definition of done (per change)

`task check` passes; generated code is committed and up to date; new behavior has tests; UI changes pass `task audit:mobile`; PLAN.md Status updated; PARITY.md boxes ticked only after side-by-side verification with the reference.
