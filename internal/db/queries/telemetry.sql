-- name: InsertWhatsAppClick :exec
insert into whatsapp_clicks (placement, locale) values ($1, $2);

-- name: CountWhatsAppClicksSince :one
select count(*) from whatsapp_clicks where created_at >= $1;

-- name: InsertAuditLog :exec
insert into audit_logs (admin_id, action, entity, entity_id, meta, ip_hash)
values ($1, $2, $3, $4, $5, $6);

-- name: ListAuditLogs :many
select l.*, a.email as admin_email, count(*) over () as total_count
from audit_logs l
left join admins a on a.id = l.admin_id
where (sqlc.narg(admin_id)::uuid is null or l.admin_id = sqlc.narg(admin_id)::uuid)
  and (sqlc.narg(action)::text is null or l.action = sqlc.narg(action)::text)
  and (sqlc.narg(from_date)::timestamptz is null or l.created_at >= sqlc.narg(from_date)::timestamptz)
  and (sqlc.narg(to_date)::timestamptz is null or l.created_at < sqlc.narg(to_date)::timestamptz)
order by l.created_at desc
limit $1 offset $2;

-- name: HitRateLimit :one
-- Increments the fixed-window counter and returns the new count.
insert into rate_limits (key, window_start, count) values ($1, $2, 1)
on conflict (key, window_start) do update set count = rate_limits.count + 1
returning count;

-- name: PeekRateLimit :one
select coalesce((select count from rate_limits where key = $1 and window_start = $2), 0)::int as count;

-- name: DeleteOldRateLimits :execrows
delete from rate_limits where window_start < $1;

-- name: UsePowSignature :execrows
-- 1 row: first use. 0 rows: replay.
insert into pow_used (signature_hash, expires_at) values ($1, $2)
on conflict (signature_hash) do nothing;

-- name: DeleteExpiredPow :execrows
delete from pow_used where expires_at < now();
