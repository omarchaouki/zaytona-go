-- New schema for the Go app (fresh database, not adopted from the TypeScript app).
-- Same business concepts as the reference: litre offers, cash-on-delivery orders,
-- reviews, certificates, editable settings and content, admin accounts.
-- Conventions: uuid ids, timestamptz everywhere, money as numeric(10,2),
-- keyed hashes and token hashes as bytea, updated_at maintained by trigger.

-- +goose Up

-- +goose StatementBegin
create function set_updated_at() returns trigger
language plpgsql as $$
begin
  new.updated_at := now();
  return new;
end
$$;
-- +goose StatementEnd

create type locale as enum ('ar', 'fr');
create type admin_role as enum ('owner', 'staff');
create type order_status as enum ('new', 'confirmed', 'shipped', 'delivered', 'cancelled', 'returned');
create type order_event_kind as enum ('created', 'status', 'paid', 'unpaid', 'edited', 'note', 'deleted', 'restored');
create type review_status as enum ('pending', 'approved', 'rejected');
create type stock_status as enum ('in_stock', 'low_stock', 'out_of_stock');
create type certification_type as enum ('onssa', 'organic', 'igp', 'iso', 'other');
create type media_kind as enum ('review_photo', 'certificate_scan', 'gallery', 'offer', 'lab_report');

-- ------------------------------------------------------------------ admins

create table admins (
    id                   uuid primary key default gen_random_uuid(),
    email                text not null check (char_length(email) between 3 and 254),
    name                 text check (char_length(name) <= 120),
    password_hash        text not null,
    role                 admin_role not null default 'staff',
    must_change_password boolean not null default true,
    ui_locale            locale not null default 'ar',
    -- TOTP secret encrypted with AES-256-GCM (key derived from APP_SECRET), key id prefixed.
    totp_secret_enc      bytea,
    totp_enabled_at      timestamptz,
    -- Last accepted TOTP time step: a code is never accepted twice.
    totp_last_step       bigint,
    password_changed_at  timestamptz,
    last_login_at        timestamptz,
    disabled_at          timestamptz,
    created_at           timestamptz not null default now(),
    updated_at           timestamptz not null default now()
);
create unique index admins_email_lower_uq on admins (lower(email));
create trigger admins_updated_at before update on admins
    for each row execute function set_updated_at();

create table admin_recovery_codes (
    id         uuid primary key default gen_random_uuid(),
    admin_id   uuid not null references admins (id) on delete cascade,
    code_hash  bytea not null,
    used_at    timestamptz,
    created_at timestamptz not null default now()
);
create index admin_recovery_codes_admin_idx on admin_recovery_codes (admin_id);
create unique index admin_recovery_codes_hash_uq on admin_recovery_codes (admin_id, code_hash);

create table sessions (
    -- SHA-256 of the session token; the raw token only lives in the cookie.
    id                  bytea primary key check (octet_length(id) = 32),
    admin_id            uuid not null references admins (id) on delete cascade,
    csrf_token          bytea not null check (octet_length(csrf_token) = 32),
    -- true between a correct password and a correct TOTP code.
    mfa_pending         boolean not null default false,
    created_at          timestamptz not null default now(),
    last_seen_at        timestamptz not null default now(),
    idle_expires_at     timestamptz not null,
    absolute_expires_at timestamptz not null,
    -- Last password or TOTP check, for sensitive owner actions.
    recent_auth_at      timestamptz not null default now(),
    ip_hash             bytea,
    user_agent          text check (char_length(user_agent) <= 512)
);
create index sessions_admin_idx on sessions (admin_id);
create index sessions_expiry_idx on sessions (idle_expires_at);

-- ---------------------------------------------------------------- settings

-- One JSON object per settings group (whatsapp, identity, contact, delivery,
-- templates, pricing, marketing, notifications, toggles). Typed in Go.
create table settings (
    key        text primary key check (key ~ '^[a-z_]{1,40}$'),
    value      jsonb not null check (jsonb_typeof(value) = 'object'),
    updated_at timestamptz not null default now(),
    updated_by uuid references admins (id) on delete set null
);
create trigger settings_updated_at before update on settings
    for each row execute function set_updated_at();

-- Editable page texts (hero, story, policies, privacy and terms pages), one JSON object per language.
create table content_blocks (
    key        text primary key check (key ~ '^[a-z_]{1,40}$'),
    value_ar   jsonb not null check (jsonb_typeof(value_ar) = 'object'),
    value_fr   jsonb not null check (jsonb_typeof(value_fr) = 'object'),
    updated_at timestamptz not null default now(),
    updated_by uuid references admins (id) on delete set null
);
create trigger content_blocks_updated_at before update on content_blocks
    for each row execute function set_updated_at();

-- ------------------------------------------------------------------- media

-- Uploaded images, re-encoded (EXIF stripped). Files live under UPLOAD_DIR as
-- <id>-<width>.<ext>; the original upload is never stored.
create table media (
    id         text primary key check (id ~ '^[a-z0-9]{26}$'),
    kind       media_kind not null,
    width      integer not null check (width > 0),
    height     integer not null check (height > 0),
    -- Widths generated for each format, e.g. {480,960,1600}.
    widths     integer[] not null default '{}',
    created_by uuid references admins (id) on delete set null,
    created_at timestamptz not null default now()
);
create index media_kind_idx on media (kind, created_at);

-- ------------------------------------------------------------------ offers

-- Olive oil sold by volume. Automatic offers keep price_mad = liters x price per
-- litre (recomputed when the price per litre or the offer is saved), so the page,
-- the form and the server read one stored price.
create table offers (
    id           uuid primary key default gen_random_uuid(),
    slug         text not null unique check (slug ~ '^[a-z0-9-]{1,64}$'),
    name_ar      text not null check (char_length(name_ar) between 1 and 120),
    name_fr      text not null check (char_length(name_fr) between 1 and 120),
    tagline_ar   text check (char_length(tagline_ar) <= 240),
    tagline_fr   text check (char_length(tagline_fr) <= 240),
    liters       numeric(7, 2) not null check (liters > 0),
    auto_price   boolean not null default true,
    price_mad    numeric(10, 2) not null check (price_mad >= 0),
    image_id     text references media (id) on delete set null,
    stock_status stock_status not null default 'in_stock',
    is_active    boolean not null default true,
    sort_order   integer not null default 0,
    -- Seed rows: the admin sees what still needs replacing.
    is_demo      boolean not null default false,
    created_at   timestamptz not null default now(),
    updated_at   timestamptz not null default now()
);
create index offers_active_sort_idx on offers (is_active, sort_order);
create trigger offers_updated_at before update on offers
    for each row execute function set_updated_at();

-- ------------------------------------------------------------------ orders

-- Feeds the reference OL-YYYY-010001. nextval() never hands the same value to two
-- transactions; gaps after a rollback are normal.
create sequence order_number_seq start with 10001;

create table orders (
    id            uuid primary key default gen_random_uuid(),
    number        bigint not null unique default nextval('order_number_seq'),
    reference     text not null unique check (reference ~ '^OL-[0-9]{4}-[0-9]{6,}$'),
    customer_name text not null check (char_length(customer_name) between 2 and 120),
    phone_e164    text not null check (phone_e164 ~ '^\+212[5-7][0-9]{8}$'),
    city          text not null check (char_length(city) between 2 and 80),
    address       text check (char_length(address) <= 300),
    note          text check (char_length(note) <= 500),
    internal_note text check (char_length(internal_note) <= 2000),
    status        order_status not null default 'new',
    subtotal      numeric(10, 2) not null check (subtotal >= 0),
    -- null: delivery fee confirmed by phone.
    delivery_fee  numeric(10, 2) check (delivery_fee >= 0),
    total         numeric(10, 2) not null check (total >= 0),
    locale        locale not null,
    source        text not null default 'landing' check (char_length(source) <= 40),
    -- First-touch utm_* and fbclid, for the ads.
    attribution   jsonb check (attribution is null or jsonb_typeof(attribution) = 'object'),
    ip_hash       bytea,
    -- First time an admin opened the order; null = shown in red as new.
    seen_at       timestamptz,
    -- Cash received on delivery, set from the admin.
    paid_at       timestamptz,
    paid_by       uuid references admins (id) on delete set null,
    created_at    timestamptz not null default now(),
    updated_at    timestamptz not null default now(),
    deleted_at    timestamptz
);
alter sequence order_number_seq owned by orders.number;
create index orders_created_idx on orders (created_at desc) where deleted_at is null;
create index orders_status_idx on orders (status, created_at desc) where deleted_at is null;
create index orders_phone_idx on orders (phone_e164, created_at desc);
create index orders_city_idx on orders (lower(city));
create index orders_unseen_idx on orders (created_at desc) where seen_at is null and deleted_at is null;
create index orders_paid_idx on orders (paid_at) where paid_at is not null;
create trigger orders_updated_at before update on orders
    for each row execute function set_updated_at();

create table order_items (
    id          uuid primary key default gen_random_uuid(),
    order_id    uuid not null references orders (id) on delete cascade,
    -- null for an "other quantity" line or a deleted offer.
    offer_id    uuid references offers (id) on delete set null,
    name_ar     text not null,
    name_fr     text not null,
    -- Litres per unit at order time; custom quantities are one unit of N litres.
    liters      numeric(7, 2) not null check (liters > 0),
    unit_price  numeric(10, 2) not null check (unit_price >= 0),
    quantity    integer not null check (quantity between 1 and 999),
    position    smallint not null default 0
);
create index order_items_order_idx on order_items (order_id, position);
create index order_items_offer_idx on order_items (offer_id);

create table order_events (
    id          uuid primary key default gen_random_uuid(),
    order_id    uuid not null references orders (id) on delete cascade,
    admin_id    uuid references admins (id) on delete set null,
    kind        order_event_kind not null,
    from_status order_status,
    to_status   order_status,
    note        text check (char_length(note) <= 2000),
    created_at  timestamptz not null default now()
);
create index order_events_order_idx on order_events (order_id, created_at);

-- ----------------------------------------------------------------- reviews

create table reviews (
    id               uuid primary key default gen_random_uuid(),
    author_name      text not null check (char_length(author_name) between 2 and 60),
    city             text check (char_length(city) <= 80),
    rating           smallint not null check (rating between 1 and 5),
    comment          text not null check (char_length(comment) between 10 and 600),
    -- Language the customer wrote in.
    language         locale not null default 'ar',
    -- Keyed hash of the E.164 phone; never displayed, matched to a delivered order.
    phone_hash       bytea,
    order_id         uuid references orders (id) on delete set null,
    status           review_status not null default 'pending',
    is_featured      boolean not null default false,
    reply_ar         text check (char_length(reply_ar) <= 1000),
    reply_fr         text check (char_length(reply_fr) <= 1000),
    rejection_reason text check (char_length(rejection_reason) <= 500),
    -- Customer's text, kept when an admin fixes typos.
    original_comment text,
    photo_id         text references media (id) on delete set null,
    ip_hash          bytea,
    -- Example review: badge on the card, excluded from aggregates and structured data.
    is_demo          boolean not null default false,
    created_at       timestamptz not null default now(),
    moderated_at     timestamptz,
    moderated_by     uuid references admins (id) on delete set null
);
create index reviews_public_idx on reviews (status, is_featured desc, created_at desc);
create index reviews_phone_idx on reviews (phone_hash) where phone_hash is not null;

-- ----------------------------------------------------------------- content

create table faqs (
    id          uuid primary key default gen_random_uuid(),
    question_ar text not null check (char_length(question_ar) between 1 and 300),
    question_fr text not null check (char_length(question_fr) between 1 and 300),
    answer_ar   text not null check (char_length(answer_ar) between 1 and 3000),
    answer_fr   text not null check (char_length(answer_fr) between 1 and 3000),
    sort_order  integer not null default 0,
    is_active   boolean not null default true,
    is_demo     boolean not null default false,
    created_at  timestamptz not null default now(),
    updated_at  timestamptz not null default now()
);
create index faqs_active_sort_idx on faqs (is_active, sort_order);
create trigger faqs_updated_at before update on faqs
    for each row execute function set_updated_at();

create table certifications (
    id         uuid primary key default gen_random_uuid(),
    type       certification_type not null,
    label_ar   text check (char_length(label_ar) <= 120),
    label_fr   text check (char_length(label_fr) <= 120),
    number     text not null check (char_length(number) between 1 and 80),
    issued_at  date,
    expires_at date,
    scan_id    text references media (id) on delete set null,
    is_public  boolean not null default false,
    sort_order integer not null default 0,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    check (expires_at is null or issued_at is null or expires_at >= issued_at)
);
create index certifications_public_idx on certifications (is_public, sort_order);
create trigger certifications_updated_at before update on certifications
    for each row execute function set_updated_at();

-- Lab analyses (acidity, peroxide...) shown in "why us" only when real data exists.
create table lab_reports (
    id          uuid primary key default gen_random_uuid(),
    lab_name    text check (char_length(lab_name) <= 120),
    analysed_at date,
    file_id     text references media (id) on delete set null,
    -- Measured values, e.g. {"acidity_pct": "0.4", "peroxide_meq": "8"}.
    results     jsonb not null default '{}' check (jsonb_typeof(results) = 'object'),
    is_public   boolean not null default false,
    created_at  timestamptz not null default now()
);

create table gallery_images (
    id         uuid primary key default gen_random_uuid(),
    media_id   text not null references media (id) on delete cascade,
    caption_ar text check (char_length(caption_ar) <= 200),
    caption_fr text check (char_length(caption_fr) <= 200),
    sort_order integer not null default 0,
    created_at timestamptz not null default now()
);
create index gallery_images_sort_idx on gallery_images (sort_order);

-- --------------------------------------------------------------- telemetry

create table whatsapp_clicks (
    id         bigint generated always as identity primary key,
    placement  text not null check (placement ~ '^[a-z_]{1,32}$'),
    locale     locale not null,
    created_at timestamptz not null default now()
);
create index whatsapp_clicks_created_idx on whatsapp_clicks (created_at);

create table audit_logs (
    id         bigint generated always as identity primary key,
    admin_id   uuid references admins (id) on delete set null,
    action     text not null check (action ~ '^[a-z_.]{1,64}$'),
    entity     text check (char_length(entity) <= 40),
    entity_id  text check (char_length(entity_id) <= 64),
    meta       jsonb check (meta is null or jsonb_typeof(meta) = 'object'),
    -- Keyed hash, never the clear IP.
    ip_hash    bytea,
    created_at timestamptz not null default now()
);
create index audit_logs_created_idx on audit_logs (created_at desc);
create index audit_logs_admin_idx on audit_logs (admin_id, created_at desc);
create index audit_logs_entity_idx on audit_logs (entity, entity_id);
create index audit_logs_action_idx on audit_logs (action, created_at desc);

-- ------------------------------------------------------------- anti-abuse

-- Fixed-window counters, keyed by e.g. order:ip:<hash>. Survive restarts.
create table rate_limits (
    key          text not null check (char_length(key) <= 200),
    window_start timestamptz not null,
    count        integer not null default 0,
    primary key (key, window_start)
);
create index rate_limits_window_idx on rate_limits (window_start);

-- ALTCHA solutions accepted once: SHA-256 of the challenge signature.
create table pow_used (
    signature_hash bytea primary key check (octet_length(signature_hash) = 32),
    expires_at     timestamptz not null
);
create index pow_used_expires_idx on pow_used (expires_at);

-- +goose Down
drop table if exists pow_used;
drop table if exists rate_limits;
drop table if exists audit_logs;
drop table if exists whatsapp_clicks;
drop table if exists gallery_images;
drop table if exists lab_reports;
drop table if exists certifications;
drop table if exists faqs;
drop table if exists reviews;
drop table if exists order_events;
drop table if exists order_items;
drop table if exists orders;
drop sequence if exists order_number_seq;
drop table if exists offers;
drop table if exists media;
drop table if exists content_blocks;
drop table if exists settings;
drop table if exists sessions;
drop table if exists admin_recovery_codes;
drop table if exists admins;
drop type if exists media_kind;
drop type if exists certification_type;
drop type if exists stock_status;
drop type if exists review_status;
drop type if exists order_event_kind;
drop type if exists order_status;
drop type if exists admin_role;
drop type if exists locale;
drop function if exists set_updated_at();
