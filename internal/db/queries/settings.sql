-- name: ListSettings :many
select key, value, updated_at from settings order by key;

-- name: GetSetting :one
select key, value, updated_at from settings where key = $1;

-- name: UpsertSetting :exec
insert into settings (key, value, updated_by)
values ($1, $2, sqlc.narg(updated_by))
on conflict (key) do update set value = excluded.value, updated_by = excluded.updated_by;

-- name: InsertSettingIfMissing :exec
insert into settings (key, value) values ($1, $2) on conflict (key) do nothing;

-- name: ListContentBlocks :many
select key, value_ar, value_fr, updated_at from content_blocks order by key;

-- name: GetContentBlock :one
select key, value_ar, value_fr, updated_at from content_blocks where key = $1;

-- name: UpsertContentBlock :exec
insert into content_blocks (key, value_ar, value_fr, updated_by)
values ($1, $2, $3, sqlc.narg(updated_by))
on conflict (key) do update
set value_ar = excluded.value_ar, value_fr = excluded.value_fr, updated_by = excluded.updated_by;

-- name: InsertContentBlockIfMissing :exec
insert into content_blocks (key, value_ar, value_fr) values ($1, $2, $3)
on conflict (key) do nothing;
