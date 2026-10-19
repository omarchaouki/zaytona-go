# SECURITY.md: security requirements (part of the spec)

Security is a feature with acceptance criteria. Every rule below is either implemented and tested, or listed in "Accepted exceptions" at the bottom with a reason and an owner.

## 1. Threat model (short)

| Asset | Who wants it | Main threats |
| --- | --- | --- |
| Admin accounts | Competitors, opportunists, credential-stuffing bots | Password guessing, phishing, session theft, CSRF, XSS in admin |
| Customer data (names, phones, cities, addresses) | Spammers, scammers | Data leak via SQLi/IDOR, verbose logs, CSV export abuse, backups |
| Order pipeline | Bots, pranksters, competitors | Fake orders (costly COD deliveries), flooding, duplicate orders |
| Reviews | Competitors, spammers | Fake reviews, stored XSS, malicious images |
| Settings (WhatsApp number) | Fraudsters | Changing the number to divert customers and cash |
| Server | Anyone | RCE through image parsing, SSRF via webhook URL, DoS via large bodies or slowloris |

Highest business risks: (1) attacker takes over the owner account and changes the WhatsApp number, (2) fake-order floods that cost real delivery money, (3) customer phone list leaks. Controls below are prioritized for these.

## 2. Secrets and keys

- One `APP_SECRET` (>= 32 random bytes, hex). Never log it. The example value is refused at boot outside development.
- Derive purpose-specific keys with HKDF-SHA256 (`crypto/hkdf`), one label per purpose: `session-csrf`, `form-token`, `confirmation-token`, `altcha-hmac`, `totp-encryption`. **Exception for compatibility**: the phone hash and IP hash must use the reference app's exact derivation (test vector required), otherwise existing data breaks.
- TOTP secrets encrypted at rest (AES-256-GCM with the derived key, random nonce, key id prefix for rotation).
- `.env` is never committed, never read by Claude Code (`.claude/settings.json` denies it), never baked into the image.
- Rotation note in README: rotating `APP_SECRET` invalidates sessions, pending form tokens and confirmation links, and breaks verified-review matching for past orders. Document this before anyone rotates it.

## 3. Transport and headers

Set by `internal/httpx` on every response:

- `Content-Security-Policy` built per request with a 128-bit random nonce:
  `default-src 'none'; script-src 'self' 'nonce-{n}' 'strict-dynamic'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'; manifest-src 'self'; worker-src 'self' blob:; upgrade-insecure-requests`
  Add third-party origins only when the matching feature is enabled (Meta Pixel, Plausible/Umami, Turnstile, map embed on click). `worker-src blob:` only if the ALTCHA widget needs workers; verify and remove otherwise. A unit test asserts the exact policy for each feature combination.
- `Strict-Transport-Security: max-age=63072000; includeSubDomains` when the request arrived over HTTPS (direct TLS or trusted proxy header).
- `X-Content-Type-Options: nosniff`, `Referrer-Policy: strict-origin-when-cross-origin` (`no-referrer` on confirmation and admin pages), `X-Frame-Options: DENY`, `Cross-Origin-Opener-Policy: same-origin`, `Cross-Origin-Resource-Policy: same-origin`, `Permissions-Policy: camera=(), microphone=(), geolocation=(), payment=(), usb=()` (do not add unknown or deprecated features: browsers log console warnings and Lighthouse Best Practices drops).
- With `'strict-dynamic'`, `'self'` is ignored by modern browsers: every `<script>` tag, including `src=` ones, must carry the nonce. Scripts loaded dynamically by a nonced script (ALTCHA widget, Meta Pixel) are then allowed.
- Admin and confirmation pages: `Cache-Control: no-store`.
- Remove `Server` and any version banners. Caddy terminates TLS (TLS 1.2+, automatic certificates).

## 4. HTTP server hardening

- `http.Server`: `ReadHeaderTimeout 5s`, `ReadTimeout 15s`, `WriteTimeout 30s`, `IdleTimeout 120s`, `MaxHeaderBytes 16 KiB`.
- `http.MaxBytesReader` on every handler that reads a body: 16 KiB for form posts, `MAX_UPLOAD_MB` (default 5) + 64 KiB for multipart uploads, 2 KiB for `/api/wa-click`. Multipart: `ParseMultipartForm` with a small memory cap; only expected field names accepted; one file max.
- Panic recovery returns a generic 500 page with the request id, never a stack trace.
- Real client IP: only from `X-Forwarded-For`/`X-Real-IP` when `TRUST_PROXY=true`, and only the right-most untrusted hop. Otherwise `RemoteAddr`.
- Every outbound request (webhook, SMTP, Turnstile) has a timeout and a context.

## 5. CSRF and cross-origin

1. `http.CrossOriginProtection` wraps the whole mux (rejects cross-site unsafe methods from modern browsers using `Sec-Fetch-Site` / `Origin`).
2. Session cookie `SameSite=Lax`.
3. Admin mutations additionally require a per-session synchronizer token (form field `csrf` or header `X-CSRF-Token` for htmx), compared in constant time. Reason: the stdlib middleware only protects browsers that send Fetch Metadata or Origin.
4. Public POSTs (order, review, wa-click, pow) are protected by (1) plus the signed form token and ALTCHA.
5. No state change on GET, ever. Tested.

## 6. Authentication and sessions

- Passwords: argon2id, PHC string. Parameters for new hashes: `m=19456 KiB, t=2, p=1` (OWASP minimum) or stronger if login stays under 300 ms on the VPS. Verify existing reference hashes using the parameters encoded in each hash; rehash on successful login if parameters are below the current target.
- Login is timing-safe: unknown email still runs one argon2 verification against a fixed dummy hash. Same generic error for wrong email and wrong password.
- Throttling (Postgres `rate_limits`): 5 failures per email and 10 per IP per 15 minutes, only failures counted, every failure audited. After the limit: generic message, no hint which limit fired.
- New password: 12+ characters, max 128, checked against an embedded list of the most common passwords and against the email local part. No composition rules.
- Sessions: 256-bit random token, only its SHA-256 stored (reference format), cookie `__Host-zaytona_session` in production (`Secure; HttpOnly; SameSite=Lax; Path=/`, no Domain), plain name on `http://localhost` in development only. Rotate the token on login, on password change and on enabling/disabling 2FA. Idle timeout 7 days sliding, absolute timeout 30 days. Password change signs out all other sessions.
- TOTP (RFC 6238, 30 s, 6 digits, +/-1 step): enrollment shows a QR code rendered server-side (SVG or PNG data URI, no third-party service) and requires a valid code before activation; 10 single-use recovery codes, shown once, stored hashed; codes cannot be reused within their window (store last used step). Owners see a dashboard warning until 2FA is on.
- Owner actions that change the WhatsApp number, phone, admins or 2FA require a recent authentication (password or TOTP within the last 10 minutes).
- Authorization: every admin handler declares its required role; destructive store methods re-check the role passed from the handler. Tests for every staff-forbidden action.

## 7. Input validation and output encoding

- Validate on the server with explicit, hand-written validators per form (length limits, allowed characters where relevant, enum membership, numeric ranges). Reject unknown form fields on admin forms.
- Normalize text: trim, collapse control characters, NFC normalization (`golang.org/x/text/unicode/norm`), reject invalid UTF-8.
- templ auto-escapes. `templ.Raw` / `@templ.Raw` is forbidden except in one reviewed helper that only accepts bluemonday-sanitized HTML (enforce with a grep test in CI).
- URLs from settings (map link, social links, webhook) validated: `https` only (http allowed for localhost webhook in development), no credentials in URL, length cap. Rendered links to external sites get `rel="noopener noreferrer"`.
- JSON-LD: `encoding/json` then escape `<`, `>`, `&`, U+2028, U+2029.
- CSV export: prefix cells starting with `=`, `+`, `-`, `@`, tab or carriage return with a single quote; UTF-8 BOM; `Content-Disposition: attachment`; owner and staff only, audited with row count.

## 8. Database

- pgx v5 >= 5.9.2, default extended protocol only (never `QueryExecModeSimpleProtocol`; CVE-2026-41889 affects the simple protocol path).
- All SQL through sqlc. Dynamic filters use fixed SQL with nullable parameters (`sqlc.narg`) or a whitelist of sort columns mapped to fixed SQL fragments. Grep test: no `fmt.Sprintf` / string concatenation feeding `Exec`/`Query`.
- App DB role has no superuser, no `CREATE` on schema in production; migrations run with a separate role (documented, optional for small VPS).
- Statement timeout per query context (default 5 s) and pool limits. It is set on each new connection with one fixed, parameterised `set_config` call in `internal/db` (outside sqlc: connection setup, not a query of the app), so it also holds behind a session pooler.
- A remote database is reached over TLS only: in production the app refuses a non-local `DATABASE_URL` without `sslmode=require`, `verify-ca` or `verify-full` (prefer `verify-full` with the provider's CA in `sslrootcert`).
- Hosted databases that publish tables over an HTTP API (Supabase's Data API, roles `anon` and `authenticated`): migration `00002_lock_data_api.sql` removes every right of those roles (existing and future tables) and turns on row-level security on every table, so orders and other personal data cannot be read with the public API key; the app connects as the owner of the tables. Turn the Data API off in the dashboard as well. New tables in later migrations get no grants to those roles (default privileges); enable row-level security on them in the same migration.
- Supabase's transaction pooler (port 6543) is refused at start (prepared statements and session settings would break); use the direct connection or the session pooler.

## 9. Uploads and images (highest RCE / DoS risk)

- Accept only JPEG, PNG, WebP (sniff magic bytes, ignore client MIME and filename). `accept="image/jpeg,image/png,image/webp"` on inputs so iOS converts HEIC.
- Before decoding: `image.DecodeConfig` and reject width or height > 6000 or pixels > 24 MP (decompression bomb guard). Size cap from `MAX_UPLOAD_MB`.
- Decode, apply EXIF orientation, resize to 480/960/1600, re-encode to AVIF + WebP + JPEG. The original bytes are never stored or served. Re-encoding strips EXIF/GPS.
- Run encoding in a bounded worker pool (max concurrent = number of CPUs, queue limit) with a per-image timeout.
- Stored under random ids (`crypto/rand`, 128 bits) in `UPLOAD_DIR`; paths built only from validated ids; served with correct `Content-Type`, `nosniff`, `Cache-Control: public, max-age=31536000, immutable`, `Content-Security-Policy: default-src 'none'`.
- Certificate scans: owner only; served publicly only if the certificate is marked public.
- Fuzz test on the upload entry point.

## 10. Anti-abuse on public forms

- ALTCHA PoW: challenge HMAC-signed with the derived key, 2 h expiry, solution accepted once (single-use row in `rate_limits` or a dedicated table), cost tuned to about 1 s on a mid-range Android. Library version must include the fix for the 2025 challenge splicing and replay advisory.
- Honeypot field (visually hidden with CSS class, `tabindex="-1"`, `autocomplete="off"`, label for screen readers telling humans to leave it empty).
- Signed render-time token: reject under 3 s or older than 24 h.
- Rate limits: orders 5 per 10 min per IP and per phone; reviews 3 per hour per IP; wa-click 30 per minute per IP; pow 30 per minute per IP. Fixed-window counters in Postgres (survive restarts).
- Optional Turnstile behind `TURNSTILE_*` env.

## 11. Outbound requests (SSRF)

Webhook URL is admin-controlled, so treat it as hostile:
- HTTPS only in production. Resolve the host, reject private, loopback, link-local, multicast, CGNAT and unspecified ranges (IPv4 and IPv6, incl. IPv4-mapped IPv6), and dial the vetted IP via a custom `net.Dialer.Control` so DNS rebinding cannot swap it.
- No redirects followed. 5 s timeout. Response body read capped at 64 KiB and discarded.
- Payload signed with HMAC-SHA256 (`X-Zaytona-Signature`), documented in README so receivers can verify.

## 12. Logging, audit, privacy

- `log/slog` JSON. Redaction helper: phones logged as `+2126****12`, names/addresses/notes never logged, IPs only as keyed hash prefix. Request logs exclude query strings on `/admin` and confirmation routes (tokens).
- Audit log rows for: login success/failure, logout, password change, 2FA on/off, recovery code use, every settings change (old and new value, with phone/WhatsApp values masked), order status/paid/delete/restore/edit, review moderation, offer and certificate changes, admin user changes, CSV export.
- Retention: rate-limit rows older than 2 days and expired sessions deleted daily.

## 13. Supply chain and build

- `go.sum` committed, `go mod verify` in CI, `govulncheck ./...` in CI (fails on reachable vulns), golangci-lint with gosec, Dependabot for Go modules, GitHub Actions and Docker base images.
- Vendored front-end files (htmx, ALTCHA widget, fonts) listed in `web/static/vendor/MANIFEST.txt` with version, source URL and SHA-256; a test recomputes and compares the hashes.
- Tailwind standalone binary downloaded by `tools/fetchtools` with a pinned version and pinned SHA-256 per platform; refuse to run on mismatch.
- Release build: `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=..." -tags nodynamic`.
- Container: distroless static, `nonroot` user, read-only root FS, only the uploads volume writable, no shell.

## 14. Security tests (must exist)

CSP header per feature combination; CSRF rejections (no token, wrong token, cross-site Sec-Fetch-Site, GET does not mutate); login timing (unknown vs known email within a tolerance, on average); throttling; session rotation and absolute expiry; staff forbidden actions; IDOR (an admin id or order id that does not exist returns 404, confirmation page without valid token returns 404); upload bombs and polyglot files rejected; SSRF blocklist (127.0.0.1, ::1, 169.254.169.254, 10/8, fd00::/8, IPv4-mapped); CSV injection; JSON-LD escaping; PoW replay; grep tests for `templ.Raw` and string-built SQL.

## 15. Checklist (tick in PLAN.md Status)

- [ ] Headers + nonce CSP, tested per feature combination
- [ ] CrossOriginProtection + admin CSRF tokens + no GET mutations
- [ ] argon2id compat + rehash + timing-safe login + throttling
- [ ] `__Host-` cookie, rotation, idle + absolute expiry
- [ ] TOTP + recovery codes + recent-auth for sensitive changes
- [ ] HKDF keys, compat hashes with test vectors, TOTP secrets encrypted
- [ ] Body limits, server timeouts, panic recovery, real-IP rules
- [ ] Image pipeline guards + fuzz test
- [ ] PoW single use, honeypot, form token, Postgres rate limits
- [ ] SSRF-safe webhook client + signed payloads
- [ ] PII-redacted logs, audit coverage, retention job
- [ ] govulncheck, gosec, vendored-file hashes, pinned Tailwind checksum, distroless non-root image

## Accepted exceptions

Proposed by the developer on 2026-10-17; each needs the owner's acceptance (or a fix) before go-live.

| Rule | Exception | Why it is safe enough | Owner |
| --- | --- | --- | --- |
| Section 2: phone and IP hashes with the reference's derivation and test vectors | Not done: keys are HKDF-derived like every other key | The owner chose a new database (PLAN.md "Decisions"); no old hash has to match | Owner (decision of 2026-10-15) |
| Section 13: htmx and the ALTCHA widget vendored with hashes | Neither is used: the admin poll uses `fetch`, the proof of work is solved by `web/static/js/lib/pow.js`. `web/static/vendor/MANIFEST.txt` lists the vendored fonts, and a test checks their SHA-256 | Less third-party code to trust | Developer |
| Section 5: synchronizer token on every state change | The login form (no session yet) relies on `http.CrossOriginProtection`, `SameSite=Lax` and login throttling | Login CSRF would only sign a victim into the attacker's own account; the other two controls block it in modern browsers | Developer |
| Section 8: separate migration role | Optional: documented in README (app role without superuser; migrations may run as the owner role) | Small VPS, one app | Owner |
| systemd hardening | `MemoryDenyWriteExecute=false` in `deploy/zaytona.service` | The WASM image encoders (wazero) compile to native code at runtime | Developer |
| Section 13: base images pinned by digest, actions pinned by SHA | Pending until the GitHub repository exists (Dependabot then keeps them current) | Tags are official images and actions | Owner, at repository creation |
