-- name: LockPhone :exec
-- Serializes order creation per phone for the duplicate guard (transaction scoped).
select pg_advisory_xact_lock(hashtextextended(@phone_e164::text, 0));

-- name: RecentDuplicateOrder :one
-- Duplicate guard: the latest order from this phone since @since, for the same
-- offer (or any order when offer_id is null, e.g. a custom quantity).
select o.reference, o.created_at
from orders o
where o.phone_e164 = @phone_e164::text
  and o.deleted_at is null
  and o.created_at > @since::timestamptz
  and (sqlc.narg(offer_id)::uuid is null
       or exists (select 1 from order_items i where i.order_id = o.id and i.offer_id = sqlc.narg(offer_id)::uuid))
order by o.created_at desc
limit 1;

-- name: CreateOrder :one
with n as (select nextval('order_number_seq') as number)
insert into orders (number, reference, customer_name, phone_e164, city, address, note,
                    subtotal, delivery_fee, total, locale, source, attribution, ip_hash)
select n.number,
       'OL-' || to_char(now() at time zone 'Africa/Casablanca', 'YYYY') || '-' || lpad(n.number::text, 6, '0'),
       @customer_name::text, @phone_e164::text, @city::text, sqlc.narg(address)::text, sqlc.narg(note)::text,
       @subtotal::numeric, sqlc.narg(delivery_fee)::numeric, @total::numeric, @locale::locale,
       @source::text, sqlc.narg(attribution)::jsonb, sqlc.narg(ip_hash)::bytea
from n
returning *;

-- name: CreateOrderItem :exec
insert into order_items (order_id, offer_id, name_ar, name_fr, liters, unit_price, quantity, position)
values ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: CreateOrderEvent :exec
insert into order_events (order_id, admin_id, kind, from_status, to_status, note)
values ($1, $2, $3, $4, $5, $6);

-- name: GetOrder :one
select * from orders where id = $1 and deleted_at is null;

-- name: GetOrderIncludingDeleted :one
select * from orders where id = $1;

-- name: GetOrderByReference :one
select * from orders where reference = $1 and deleted_at is null;

-- name: ListOrderItems :many
select * from order_items where order_id = $1 order by position;

-- name: ListOrderEvents :many
select * from order_events where order_id = $1 order by created_at;

-- name: ListOrderEventsWithAdmin :many
select e.id, e.kind, e.from_status, e.to_status, e.note, e.created_at, a.email as admin_email
from order_events e left join admins a on a.id = e.admin_id
where e.order_id = $1 order by e.created_at desc;

-- name: SearchOrders :many
-- Fixed SQL with nullable filters. sort_key comes from a whitelist in Go and is
-- mapped to fixed CASE branches here, never concatenated. q and city arrive
-- with LIKE wildcards already stripped.
select o.id, o.reference, o.customer_name, o.phone_e164, o.city, o.total, o.status,
       o.locale, o.seen_at, o.paid_at, o.created_at,
       (select coalesce(sum(i.quantity), 0) from order_items i where i.order_id = o.id)::int as item_count,
       count(*) over () as total_count
from orders o
where o.deleted_at is null
  and (sqlc.narg(q)::text is null
       or o.reference ilike '%' || sqlc.narg(q)::text || '%'
       or o.customer_name ilike '%' || sqlc.narg(q)::text || '%'
       or (sqlc.narg(phone)::text is not null and o.phone_e164 like '%' || sqlc.narg(phone)::text || '%'))
  and (sqlc.narg(status)::order_status is null or o.status = sqlc.narg(status)::order_status)
  and (sqlc.narg(paid)::boolean is null or (o.paid_at is not null) = sqlc.narg(paid)::boolean)
  and (not @unseen_only::boolean or o.seen_at is null)
  and (sqlc.narg(city)::text is null or o.city ilike '%' || sqlc.narg(city)::text || '%')
  and (sqlc.narg(offer_id)::uuid is null
       or exists (select 1 from order_items i where i.order_id = o.id and i.offer_id = sqlc.narg(offer_id)::uuid))
  and (sqlc.narg(from_date)::timestamptz is null or o.created_at >= sqlc.narg(from_date)::timestamptz)
  and (sqlc.narg(to_date)::timestamptz is null or o.created_at < sqlc.narg(to_date)::timestamptz)
order by
  case when @sort_key::text = 'created_asc' then o.created_at end asc,
  case when @sort_key::text = 'total_desc' then o.total end desc,
  case when @sort_key::text = 'total_asc' then o.total end asc,
  case when @sort_key::text = 'name_asc' then o.customer_name end asc,
  case when @sort_key::text = 'name_desc' then o.customer_name end desc,
  case when @sort_key::text = 'city_asc' then o.city end asc,
  case when @sort_key::text = 'city_desc' then o.city end desc,
  case when @sort_key::text = 'status_asc' then o.status end asc,
  case when @sort_key::text = 'status_desc' then o.status end desc,
  o.created_at desc, o.number desc
limit @page_size::int offset @page_offset::int;

-- name: ExportOrders :many
-- Every matching order for the CSV export (same filters as SearchOrders, newest first).
select o.*
from orders o
where o.deleted_at is null
  and (sqlc.narg(q)::text is null
       or o.reference ilike '%' || sqlc.narg(q)::text || '%'
       or o.customer_name ilike '%' || sqlc.narg(q)::text || '%'
       or (sqlc.narg(phone)::text is not null and o.phone_e164 like '%' || sqlc.narg(phone)::text || '%'))
  and (sqlc.narg(status)::order_status is null or o.status = sqlc.narg(status)::order_status)
  and (sqlc.narg(paid)::boolean is null or (o.paid_at is not null) = sqlc.narg(paid)::boolean)
  and (not @unseen_only::boolean or o.seen_at is null)
  and (sqlc.narg(city)::text is null or o.city ilike '%' || sqlc.narg(city)::text || '%')
  and (sqlc.narg(offer_id)::uuid is null
       or exists (select 1 from order_items i where i.order_id = o.id and i.offer_id = sqlc.narg(offer_id)::uuid))
  and (sqlc.narg(from_date)::timestamptz is null or o.created_at >= sqlc.narg(from_date)::timestamptz)
  and (sqlc.narg(to_date)::timestamptz is null or o.created_at < sqlc.narg(to_date)::timestamptz)
order by o.created_at desc, o.number desc
limit 20000;

-- name: ListItemsForOrders :many
select * from order_items where order_id = any(@order_ids::uuid[]) order by order_id, position;

-- name: GetOrderForUpdate :one
select * from orders where id = $1 and deleted_at is null for update;

-- name: UpdateOrderStatus :one
update orders set status = $2 where id = $1 and deleted_at is null returning *;

-- name: MarkOrderPaid :execrows
update orders set paid_at = now(), paid_by = @admin_id
where id = @id and deleted_at is null and paid_at is null;

-- name: MarkOrderUnpaid :execrows
update orders set paid_at = null, paid_by = null
where id = $1 and deleted_at is null and paid_at is not null;

-- name: MarkOrderSeen :exec
update orders set seen_at = now() where id = $1 and seen_at is null;

-- name: MarkAllOrdersSeen :execrows
update orders set seen_at = now() where seen_at is null and deleted_at is null;

-- name: UpdateOrderInternalNote :exec
update orders set internal_note = $2 where id = $1 and deleted_at is null;

-- name: UpdateOrderTotals :exec
update orders set subtotal = $2, delivery_fee = $3, total = $4 where id = $1 and deleted_at is null;

-- name: UpdateOrderItemQuantity :exec
update order_items set quantity = $3 where id = $1 and order_id = $2;

-- name: DeleteOrderItem :exec
delete from order_items where id = $1 and order_id = $2;

-- name: SoftDeleteOrder :execrows
update orders set deleted_at = now() where id = $1 and deleted_at is null;

-- name: RestoreOrder :execrows
update orders set deleted_at = null where id = $1 and deleted_at is not null;

-- name: CountUnseenOrders :one
select count(*) from orders where seen_at is null and deleted_at is null;

-- name: ListUnseenOrders :many
-- Newest unseen orders for the live notifications, without the ones closed in this browser.
select id, reference, customer_name, city, total, created_at from orders
where seen_at is null and deleted_at is null and not (id = any(@skip::uuid[]))
order by created_at desc limit @max_rows::int;

-- name: ListUnseenOrderIDs :many
select id from orders where seen_at is null and deleted_at is null
order by created_at desc limit 50;

-- name: LatestOrders :many
select id, reference, customer_name, city, total, status, seen_at, created_at
from orders where deleted_at is null
order by created_at desc limit $1;

-- name: DashboardCounters :one
select
  count(*) filter (where created_at >= @today_start::timestamptz) as today,
  count(*) filter (where status = 'new') as new_orders,
  count(*) filter (where status in ('confirmed', 'shipped')) as to_deliver,
  coalesce(sum(total) filter (where status in ('confirmed', 'shipped')), 0)::numeric as to_collect,
  count(*) filter (where status = 'delivered') as delivered,
  count(*) as total_orders,
  count(*) filter (where paid_at is not null) as paid_count,
  coalesce(sum(total) filter (where paid_at is not null), 0)::numeric as collected_all,
  count(*) filter (where paid_at >= @month_start::timestamptz) as paid_month_count,
  coalesce(sum(total) filter (where paid_at >= @month_start::timestamptz), 0)::numeric as collected_month,
  count(*) filter (where status = 'delivered' and paid_at is null) as delivered_unpaid_count,
  coalesce(sum(total) filter (where status = 'delivered' and paid_at is null), 0)::numeric as delivered_unpaid
from orders where deleted_at is null;

-- name: OrdersPerDay :many
select (created_at at time zone 'Africa/Casablanca')::date as day, count(*) as orders
from orders
where deleted_at is null and created_at >= @since::timestamptz
group by day order by day;

-- name: LatestDeliveredOrderForPhone :one
select id from orders
where phone_e164 = $1 and status = 'delivered' and deleted_at is null
order by created_at desc limit 1;

-- name: ListDistinctCities :many
select city, count(*) as n from orders where deleted_at is null
group by city order by n desc limit 200;

-- name: CountDeliveredOrders :one
select count(*) from orders where status = 'delivered' and deleted_at is null;

-- name: GetOrderForConfirmation :one
select id, reference, customer_name, city, subtotal, delivery_fee, total, status, created_at
from orders where reference = $1 and deleted_at is null;
