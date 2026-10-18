-- name: CreateSession :exec
insert into sessions (id, admin_id, csrf_token, mfa_pending, idle_expires_at, absolute_expires_at, ip_hash, user_agent)
values ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: GetSessionWithAdmin :one
select sqlc.embed(s), sqlc.embed(a)
from sessions s
join admins a on a.id = s.admin_id
where s.id = $1
  and s.idle_expires_at > now()
  and s.absolute_expires_at > now()
  and a.disabled_at is null;

-- name: TouchSession :exec
-- Sliding idle expiry, never past the absolute cap.
update sessions
set last_seen_at = now(), idle_expires_at = least(@idle_expires_at::timestamptz, absolute_expires_at)
where id = $1;

-- name: CompleteSessionMFA :exec
update sessions set mfa_pending = false, recent_auth_at = now() where id = $1;

-- name: MarkSessionRecentAuth :exec
update sessions set recent_auth_at = now() where id = $1;

-- name: DeleteSession :exec
delete from sessions where id = $1;

-- name: DeleteAdminSessions :exec
delete from sessions where admin_id = $1;

-- name: DeleteOtherAdminSessions :exec
delete from sessions where admin_id = $1 and id <> $2;

-- name: DeleteExpiredSessions :execrows
delete from sessions where idle_expires_at <= now() or absolute_expires_at <= now();
