-- name: CreateAdmin :one
insert into admins (email, name, password_hash, role, must_change_password, ui_locale)
values ($1, $2, $3, $4, $5, $6)
returning *;

-- name: GetAdminByID :one
select * from admins where id = $1;

-- name: GetAdminByEmail :one
select * from admins where lower(email) = lower(@email::text);

-- name: ListAdmins :many
select * from admins order by created_at;

-- name: CountActiveOwners :one
select count(*) from admins where role = 'owner' and disabled_at is null;

-- name: UpdateAdminPassword :exec
update admins
set password_hash = $2, must_change_password = $3, password_changed_at = now()
where id = $1;

-- name: UpdateAdminLastLogin :exec
update admins set last_login_at = now() where id = $1;

-- name: UpdateAdminRole :exec
update admins set role = $2 where id = $1;

-- name: UpdateAdminLocale :exec
update admins set ui_locale = $2 where id = $1;

-- name: SetAdminDisabled :exec
update admins set disabled_at = sqlc.narg(disabled_at) where id = $1;

-- name: SetAdminTOTP :exec
update admins
set totp_secret_enc = sqlc.narg(secret_enc), totp_enabled_at = sqlc.narg(enabled_at), totp_last_step = null
where id = $1;

-- name: AdvanceTOTPStep :execrows
-- Accepts a TOTP step only if it is newer than the last one used.
update admins set totp_last_step = @step::bigint
where id = $1 and (totp_last_step is null or totp_last_step < @step::bigint);

-- name: DeleteRecoveryCodes :exec
delete from admin_recovery_codes where admin_id = $1;

-- name: InsertRecoveryCode :exec
insert into admin_recovery_codes (admin_id, code_hash) values ($1, $2);

-- name: UseRecoveryCode :execrows
update admin_recovery_codes set used_at = now()
where admin_id = $1 and code_hash = $2 and used_at is null;

-- name: CountUnusedRecoveryCodes :one
select count(*) from admin_recovery_codes where admin_id = $1 and used_at is null;

-- name: ListAdminsOverview :many
select a.id, a.email, a.name, a.role, a.must_change_password, a.totp_enabled_at, a.last_login_at,
       a.disabled_at, a.created_at
from admins a
order by a.disabled_at nulls first, a.role, a.created_at;

-- name: ListAuditActions :many
select distinct action from audit_logs order by action;
