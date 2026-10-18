# DATA_MIGRATION.md: from the TypeScript app to the Go app

## Decision (owner, 2026-10-15)

The Go app runs on a **new PostgreSQL database with its own schema**. It does not adopt the TypeScript app's (Drizzle) database. PROMPT.md section 4 items on Drizzle adoption, schema equivalence and reference hash test vectors were dropped for that reason (recorded in `PLAN.md`, "Decisions").

Consequences:

| Data in the old app | In the Go app |
| --- | --- |
| Settings (WhatsApp number, phone, name, address, delivery rules, templates, pixel) | Re-entered by the owner in Admin → Paramètres (10 minutes) |
| Offers and price per litre | Re-entered in Admin → Offres et prix |
| ONSSA number and scan, other certificates | Re-entered in Admin → Certificats (upload the scan again) |
| FAQ, page texts, privacy and terms pages | Seeded drafts; edit in Admin → Questions fréquentes and Contenus |
| Gallery photos | Uploaded again in Admin → Contenus → Galerie |
| Admin accounts and passwords | Not carried over: the owner is created by `zaytona seed`, staff accounts from Admin → Administrateurs |
| Orders | Not carried over. Export them from the old admin (CSV) before the switch and keep the file |
| Customer reviews | Not carried over (the old site had none published at the time of the decision). Example reviews stay flagged until real ones are approved |
| Verified-purchase badges | Start fresh: they need delivered orders in the new database |

Nothing is read from the old database, so no data can be corrupted by the switch.

## Cutover checklist

1. Old app: export the orders CSV from its admin and keep it with the backups.
2. Server: create the new database, set `/etc/zaytona/zaytona.env` (`APP_ENV=production`, `SITE_URL`, `DATABASE_URL`, a new random `APP_SECRET`, `ADMIN_EMAIL`, `ADMIN_PASSWORD`), run `zaytona migrate` then `zaytona seed`.
3. Start the service, open `/admin/login`, choose the owner password, turn on two-step login (Admin → Sécurité).
4. Enter the content listed above (CLIENT_HANDOVER.md has the list), until the dashboard shows no setup warning.
5. Place one test order from a phone, mark it delivered and paid, then delete it (owner) so the counters start clean.
6. Switch the domain (Caddy) to the Go app. Keep the old app and its database read-only for a few weeks, then archive them with the CSV export.
7. Remove `ADMIN_PASSWORD` from the environment file once the owner has logged in.

## Rehearsal

Rehearse steps 2 to 5 on a scratch database first (the same commands work against `task db:local`). The browser scenarios (`task e2e`) and the mobile audit (`task audit:mobile`) can run against that rehearsal server; they create test orders, so never point them at production.
