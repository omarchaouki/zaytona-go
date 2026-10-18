# PARITY.md: feature parity with the TypeScript app

The spec asks to tick a box after checking the Go app and the reference app side by side (same data, same viewport, both languages). Items marked **NEW** do not exist in the reference.

**Redesign (2026-10-17, owner request).** The look no longer follows the reference (see PLAN.md, Decisions, "Brand redesign"). Parity below is about content and behaviour; where the redesign changed a behaviour on purpose, the item says so.

**How the boxes were checked (2026-10-17).** Each section was ported by reading the reference source (`../ZIIIT/src`) next to the Go code, then checked in the Go app in a browser (375 px and desktop, AR and FR), by the tests and e2e scenarios named in `PLAN.md`, and by the mobile audit. A two-browser session with the reference **running** was not possible here: it needs its own `.env` (off-limits), a database server on port 5433 (taken by the machine's PostgreSQL 18), and `vite dev` writes into the reference folder (read-only). That session stays open for the owner or a later run (last section).

## Data
Owner decision (2026-10-15): the Go app uses a **new database with its own schema**; nothing is adopted from the Drizzle database. The compatibility items of the original checklist (baseline identical to Drizzle, adoption, hash compatibility, reused uploads) no longer apply.
- [x] **NEW** clean schema `00001_init.sql`, migrate up/down tested
- [x] `order_number_seq` starts at 10001 (`OL-YYYY-010001`), 25 parallel orders get distinct references (tested)
- [x] Seed idempotent, never overwrites edits, demo flags as in the reference (tested)

## Public site
- [x] `/` 301 to `/ar`; `/ar` RTL, `/fr` LTR; switcher in header; hreflang + canonical
- [x] Sticky header (logo, AR | FR, Order)
- [x] Hero: headline, price anchor from cheapest available offer, Order + WhatsApp CTAs, trust row, rating snippet (approved non-demo only)
- [ ] Hero entrance sequence; optional hero video (`assets/source/hero.mp4`), photo Ken Burns + crossfade fallback; no video for reduced motion / Save-Data. Entrance, Ken Burns and crossfade are ported; **the optional video is not** (no video was provided, so both apps show the photo crossfade today)
- [x] Trust strip (ONSSA, natural, direct, COD, delivery). Redesign: the ticker is gone; the same promises sit as one line under the hero action and in the reasons
- [x] Litre offers: volume, price, price per litre, other quantity. Redesign: a price list with dot leaders, and a litre gauge for any quantity (works without JS); offer photos are no longer shown on the landing
- [x] Offer buttons are `#order` links that select the offer, work before JS
- [x] Benefits of olive oil section with authorised-claim wording and general-information note
- [x] Why us: data-driven reason cards (only with real data)
- [x] ONSSA section `#onssa`: meaning, seal pressed on scroll, number/dates, scan dialog or WhatsApp request; hidden without public number
- [x] How to order: timeline on phones, 3 columns from tablet
- [x] Story section with arch photo
- [x] Reviews: summary (average, partial stars, per-star bars that filter), cards, photo lightbox, replies, verified badge, example badge, load more, empty state (summary counts real reviews only, per PROMPT.md)
- [x] Review form: stars (radio group), comment 10 to 600, city, phone (hashed), photo (re-encoded), anti-spam, pending message (thank-you page instead of an in-dialog message)
- [x] FAQ + FAQPage JSON-LD. Redesign: answers set as reading text in two columns (no accordion)
- [x] Final CTA band
- [x] Footer: name, address, map on click, phone, WhatsApp, hours, socials, legal links, ONSSA number, Morocco-time year
- [x] Privacy and terms pages (admin-editable, drafts flagged for lawyer review); pixel paragraph while pixel on
- [x] Sticky mobile bar (Order + WhatsApp); floating WhatsApp from tablets up, tooltip once per session, no overlap
- [x] WhatsApp click counter (limit 30 per minute per IP, per SECURITY.md)
- [x] Olive-sprig wallpaper, section backdrops (light / dark / photo veils), scroll reveals with stagger, loading line
- [x] SEO: titles, descriptions, OG/Twitter images per locale, sitemap, robots, JSON-LD (LocalBusiness, Product, FAQPage, AggregateRating, Review)

## Order flow
- [x] 3 steps with stepper, per-step validation, focus to step heading, edit links on review step, server errors routed to the right step
- [x] Offer or custom litres (min/max from settings, priced per litre, one order line with null product)
- [x] Moroccan phone normalization to E.164 (fuzzed)
- [x] City combobox AR/FR, accent and spelling tolerant, free text
- [x] Live total with delivery fee rules and delivery time text
- [x] ALTCHA PoW in background, honeypot, render-time token, rate limits, optional Turnstile
- [x] Duplicate guard with "send anyway"; 25 parallel submits give distinct references
- [x] Draft in localStorage 7 days, restored on reload, cleared on success
- [x] One auto retry, then recovery box with Retry and Send by WhatsApp
- [x] Confirmation page: signed token, drawn check, reference ticket + copy, summary, WhatsApp confirm, phone
- [x] Attribution (utm_*, fbclid, first touch, 30 days) stored on the order
- [x] Meta Pixel: PageView, Lead once per order with value/currency/eventID, consent bar option
- [x] Email + webhook notifications, non-blocking

## Admin
- [x] Login, failures throttled and audited, forced password change (12+), reset-password CLI
- [x] **NEW** TOTP 2FA + recovery codes, recent-auth for sensitive settings, absolute session cap
- [x] Shell: bottom tab bar + More sheet on phones/tablets, sidebar from 1024 px; AR/FR per account
- [x] Dashboard: money card, counters as links, setup warnings, 30-day chart, latest orders
- [x] Live new orders: 10 s polling, red cards (slim bar on phones), badge, tab title count, chime with gesture unlock, bell toggle, closed per session
- [x] Orders list: search (name, phone any format, reference), filters (status, paid/unpaid/unopened, offer, city, dates), sort, 10 to 50 per page remembered, range "11 to 20 of 63", cards below 1280 px, table from 1280 px
- [x] Bulk: status, paid, delete; Undo after delete; "mark all as seen"
- [x] CSV export (BOM, injection-safe, attribution)
- [x] Order page: call, WhatsApp template in customer language, edit quantities + delivery fee, status + timeline, note, paid, print slip, delete (owner), traffic source
- [x] Offers and prices: price per litre, other-quantity limits, CRUD, auto/fixed price, visible/hidden, order
- [x] Reviews moderation: tabs, search, bulk, refuse reason, pin, edit (original kept), replies AR/FR, delete (owner), pending badge
- [x] Certificates: ONSSA + others, scan upload, public flag (owner)
- [x] Settings: WhatsApp (test link), order phone, name + address AR/FR, delivery fee rules + preview, templates AR/FR, notifications, section toggles, marketing (pixel)
- [x] **NEW (open in reference)** Admins page with owner/staff roles
- [x] **NEW (open in reference)** Audit log viewer
- [x] **NEW (open in reference)** FAQ CRUD + reorder
- [x] **NEW (open in reference)** Content blocks + gallery + policy editing

## Quality gates
- [x] Mobile audit: 0 findings, all public + admin pages, AR + FR, 320 to 1024 px
- [ ] Lighthouse mobile: Perf >= 95, A11y / BP / SEO 100 (configured in CI, `.github/lighthouserc.json`; not run yet: no repository/CI run so far)
- [x] Budgets test passes (JS, CSS, HTML, fonts, hero image)
- [x] SECURITY.md checklist complete (with the accepted exceptions listed there, pending the owner's acceptance)

## Still open
- [ ] Two-browser session with the reference app running on the same data (see the note at the top)
- [ ] Optional hero video (only if the owner provides a video)
- [ ] Lighthouse run in CI
