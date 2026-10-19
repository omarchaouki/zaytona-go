# Deploying on Ubuntu with nginx, HTTPS and Supabase

Target: Ubuntu 24.04 LTS, one VPS, the Go app as a systemd service behind nginx, Let's Encrypt certificates, the database on Supabase (managed PostgreSQL). Uploaded photos stay on the server's disk.

Replace `example.ma` with your domain and `you@example.ma` with your e-mail in every command. Commands start with `sudo` when they need root.

## 0. Before you start

- The domain's DNS records point to the server: an `A` record (IPv4) for `example.ma` and `www.example.ma`, plus `AAAA` (IPv6) if the server has one.
- A Supabase project, created in a region near Morocco (for example `eu-west-3`, Paris), with a database password you keep safe.

## 1. Supabase

In the Supabase dashboard of the project:

1. **Connection string.** Click **Connect**. Two choices work with this app:
   - **Direct connection** (`db.<ref>.supabase.co:5432`): best, but IPv6 only unless you buy the IPv4 add-on. Use it if the server has IPv6 (`curl -6 -s https://ifconfig.co` prints an address).
   - **Session pooler** (`aws-0-<region>.pooler.supabase.com:5432`, user `postgres.<ref>`): works over IPv4.
   - Never the **transaction pooler** (port `6543`): the app refuses it at start, because it breaks prepared statements.
2. **TLS certificate.** Project Settings → Database → SSL Configuration: turn on **Enforce SSL** and **Download certificate**. You will copy it to `/etc/zaytona/supabase-ca.crt` in step 4.
3. **Data API.** The app never uses Supabase's HTTP API. Turn it off (Project Settings → Data API), or at least remove `public` from the exposed schemas. Migration `00002_lock_data_api.sql` also removes every right of the `anon` and `authenticated` roles and turns on row-level security on every table, so customer data cannot be read through the API even if it stays on.
4. **Backups.** The free plan has no automatic backups; step 9 sets up a nightly `pg_dump` on the server.

The `DATABASE_URL` you will write in step 4 (URL-encode the password if it has symbols such as `@`, `:` or `/`):

```text
# Direct connection (IPv6)
postgres://postgres:PASSWORD@db.REF.supabase.co:5432/postgres?sslmode=verify-full&sslrootcert=/etc/zaytona/supabase-ca.crt

# Session pooler (IPv4)
postgres://postgres.REF:PASSWORD@aws-0-REGION.pooler.supabase.com:5432/postgres?sslmode=verify-full&sslrootcert=/etc/zaytona/supabase-ca.crt
```

If `verify-full` fails with a certificate error on the pooler, use `sslmode=require` (still encrypted, without checking the certificate). In production the app refuses a remote database without `sslmode=require`, `verify-ca` or `verify-full`.

Keep `DATABASE_POOL_MAX=5`: Supabase plans limit connections, and the app needs few.

## 2. Server basics

```bash
sudo apt update && sudo apt -y full-upgrade
```

```bash
sudo apt install -y nginx certbot git curl ufw unattended-upgrades
```

Firewall: SSH and the web only (the app itself listens on 127.0.0.1).

```bash
sudo ufw allow OpenSSH
```

```bash
sudo ufw allow 'Nginx Full'
```

```bash
sudo ufw --force enable
```

PostgreSQL client tools for backups, same major version as Supabase or newer (new Supabase projects run PostgreSQL 17):

```bash
sudo apt install -y postgresql-common && sudo /usr/share/postgresql-common/pgdg/apt.postgresql.org.sh -y
```

```bash
sudo apt install -y postgresql-client-17
```

## 3. Build the app

Go (the version in `go.mod`), then Task:

```bash
curl -LO https://go.dev/dl/go1.27.2.linux-amd64.tar.gz && sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.27.2.linux-amd64.tar.gz && rm go1.27.2.linux-amd64.tar.gz
```

```bash
echo 'export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin' >> ~/.bashrc && . ~/.bashrc && go version
```

```bash
go install github.com/go-task/task/v3/cmd/task@latest
```

The code, the pinned Tailwind CLI, and the release binary (static, assets embedded) in `bin/zaytona`:

```bash
git clone https://github.com/omarchaouki/zaytona-go.git ~/zaytona-go && cd ~/zaytona-go
```

```bash
task tools && task build
```

## 4. Install

A system user without login, the binary, the folders:

```bash
sudo useradd --system --home /var/lib/zaytona --shell /usr/sbin/nologin zaytona
```

```bash
sudo install -d -o root -g root -m 0755 /opt/zaytona && sudo install -o root -g root -m 0755 ~/zaytona-go/bin/zaytona /opt/zaytona/zaytona
```

```bash
sudo install -d -o zaytona -g zaytona -m 0700 /var/lib/zaytona /var/lib/zaytona/uploads && sudo install -d -o root -g root -m 0755 /etc/zaytona
```

The Supabase certificate from step 1 (copy it to the server first, for example with `scp prod-ca-2021.crt you@server:`):

```bash
sudo install -o root -g root -m 0644 ~/prod-ca-2021.crt /etc/zaytona/supabase-ca.crt
```

The environment file (readable by root only; systemd reads it for the service). Generate the secret once and keep it: changing it signs everyone out and invalidates two-step login.

```bash
openssl rand -hex 32
```

```bash
sudo nano /etc/zaytona/zaytona.env
```

```text
APP_ENV=production
SITE_URL=https://example.ma
DATABASE_URL='postgres://postgres.REF:PASSWORD@aws-0-REGION.pooler.supabase.com:5432/postgres?sslmode=verify-full&sslrootcert=/etc/zaytona/supabase-ca.crt'
DATABASE_POOL_MAX=5
APP_SECRET=the-64-hex-characters-from-openssl
ADMIN_EMAIL=you@example.ma
ADMIN_PASSWORD=a-temporary-password-of-12-or-more-characters
```

Optional lines: `SMTP_URL` and `NOTIFY_EMAIL_FROM` (new-order e-mails), `ORDER_WEBHOOK_URL`, `TURNSTILE_SITE_KEY` and `TURNSTILE_SECRET_KEY`.

Put any value that contains `&`, `$`, spaces or quotes (the database URL always has `&`) in **single quotes**, as above: systemd and the shell (used by `migrate`, `seed` and the backup) then read the same value.

```bash
sudo chmod 600 /etc/zaytona/zaytona.env
```

## 5. Database schema and first content

Both read the environment file. `migrate` creates the tables (and locks the Data API); `seed` adds the starting content and the owner account from `ADMIN_EMAIL`/`ADMIN_PASSWORD`.

```bash
sudo sh -c 'set -a; . /etc/zaytona/zaytona.env; set +a; /opt/zaytona/zaytona migrate'
```

```bash
sudo sh -c 'set -a; . /etc/zaytona/zaytona.env; set +a; /opt/zaytona/zaytona seed'
```

## 6. Run it

```bash
sudo cp ~/zaytona-go/deploy/zaytona.service /etc/systemd/system/zaytona.service && sudo systemctl daemon-reload && sudo systemctl enable --now zaytona
```

```bash
sudo systemctl status zaytona --no-pager && curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:3000/healthz
```

The last command prints `200`. Logs: `sudo journalctl -u zaytona -f`.

## 7. nginx and HTTPS

First a plain HTTP site that answers the Let's Encrypt challenge:

```bash
sudo mkdir -p /var/www/certbot && sudo rm -f /etc/nginx/sites-enabled/default
```

```bash
printf 'server {\n  listen 80;\n  listen [::]:80;\n  server_name example.ma www.example.ma;\n  location ^~ /.well-known/acme-challenge/ { root /var/www/certbot; }\n  location / { return 404; }\n}\n' | sudo tee /etc/nginx/sites-available/zaytona >/dev/null
```

```bash
sudo ln -sf /etc/nginx/sites-available/zaytona /etc/nginx/sites-enabled/zaytona && sudo nginx -t && sudo systemctl reload nginx
```

The certificate for both names:

```bash
sudo certbot certonly --webroot -w /var/www/certbot -d example.ma -d www.example.ma --email you@example.ma --agree-tos --no-eff-email
```

Then the real site (HTTPS, `www` and `http` redirected to `https://example.ma`, the app behind it):

```bash
sudo cp ~/zaytona-go/deploy/nginx/zaytona-tls.conf /etc/nginx/snippets/zaytona-tls.conf
```

```bash
sed 's/example\.ma/YOUR-DOMAIN/g' ~/zaytona-go/deploy/nginx/zaytona.conf | sudo tee /etc/nginx/sites-available/zaytona >/dev/null
```

```bash
sudo nginx -t && sudo systemctl reload nginx
```

Renewal runs by itself (certbot's systemd timer). Reload nginx after each renewal, then check that renewal works:

```bash
printf '#!/bin/sh\nsystemctl reload nginx\n' | sudo tee /etc/letsencrypt/renewal-hooks/deploy/reload-nginx >/dev/null && sudo chmod 755 /etc/letsencrypt/renewal-hooks/deploy/reload-nginx
```

```bash
sudo certbot renew --dry-run
```

Check from your computer: `https://example.ma` opens, `http://example.ma` and `https://www.example.ma` redirect to it. The SSL Labs test (ssllabs.com/ssltest) should give A or A+.

## 8. First login

Open `https://example.ma/admin/login`, sign in with `ADMIN_EMAIL` and the temporary password, choose your own password, then turn on two-step login (Admin → Sécurité). Then remove `ADMIN_PASSWORD` from the environment file and restart:

```bash
sudo sed -i '/^ADMIN_PASSWORD=/d' /etc/zaytona/zaytona.env && sudo systemctl restart zaytona
```

Fill in the settings (WhatsApp number, phone, prices, ONSSA number...) as listed in `CLIENT_HANDOVER.md`.

## 9. Backups

Database and photos every night at 03:15, kept 14 days in `/var/backups/zaytona`. Copy that folder off the server too (another machine, cloud storage).

```bash
sudo install -o root -g root -m 0750 ~/zaytona-go/deploy/backup.sh /usr/local/sbin/zaytona-backup
```

```bash
echo '15 3 * * * root /usr/local/sbin/zaytona-backup' | sudo tee /etc/cron.d/zaytona-backup >/dev/null
```

```bash
sudo /usr/local/sbin/zaytona-backup && sudo ls -l /var/backups/zaytona
```

Restore a dump into an empty database:

```bash
pg_restore --clean --if-exists --no-owner --dbname="$DATABASE_URL" /var/backups/zaytona/db-YYYY-MM-DD.dump
```

## 10. Updating

```bash
cd ~/zaytona-go && git pull && task build
```

```bash
sudo install -o root -g root -m 0755 ~/zaytona-go/bin/zaytona /opt/zaytona/zaytona
```

```bash
sudo sh -c 'set -a; . /etc/zaytona/zaytona.env; set +a; /opt/zaytona/zaytona migrate' && sudo systemctl restart zaytona
```

## Troubleshooting

| Symptom | Cause and fix |
| --- | --- |
| `config: DATABASE_URL: a remote database needs TLS` | Add `?sslmode=verify-full&sslrootcert=/etc/zaytona/supabase-ca.crt` (or `sslmode=require`) to `DATABASE_URL`. |
| `transaction pooler (port 6543) is not supported` | Use the session pooler (same host, port `5432`) or the direct connection. |
| `connect to database: ... network is unreachable` with `db.<ref>.supabase.co` | The direct connection is IPv6 only: use the session pooler, or give the server IPv6. |
| `x509: certificate signed by unknown authority` | Wrong or missing certificate file: download it again (step 1) or use `sslmode=require`. |
| `too many connections` | Lower `DATABASE_POOL_MAX`, or use the session pooler. |
| nginx `502 Bad Gateway` | The app is not running: `sudo systemctl status zaytona` and `sudo journalctl -u zaytona -n 50`. |
| Uploads refused with `413` | A photo over 5 MB: `client_max_body_size` in the nginx site and `MAX_UPLOAD_MB` go together. |
