-- name: CreateReview :one
insert into reviews (author_name, city, rating, comment, language, phone_hash, order_id, photo_id, ip_hash, is_demo, status)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
returning *;

-- name: CreateDemoReview :exec
-- Example review for the seed: approved, flagged is_demo, backdated.
insert into reviews (author_name, city, rating, comment, language, status, is_demo, moderated_at, created_at)
values ($1, $2, $3, $4, 'ar', 'approved', true, now(), $5);

-- name: ListPublicReviews :many
-- Approved reviews, pinned first then newest; verified = linked to a delivered order.
select r.id, r.author_name, r.city, r.rating, r.comment, r.language, r.reply_ar, r.reply_fr,
       r.is_demo, r.created_at, r.photo_id,
       m.width as photo_width, m.height as photo_height, m.widths as photo_widths,
       coalesce(o.status = 'delivered', false)::boolean as verified
from reviews r
left join media m on m.id = r.photo_id
left join orders o on o.id = r.order_id
where r.status = 'approved'
  and (sqlc.narg(rating)::smallint is null or r.rating = sqlc.narg(rating)::smallint)
order by r.is_featured desc, r.created_at desc
limit @max_rows::int offset @skip::int;

-- name: ReviewSummary :one
-- Real reviews only: example reviews never count.
select count(*) as total,
       coalesce(avg(rating), 0)::numeric as average,
       count(*) filter (where rating = 5) as r5,
       count(*) filter (where rating = 4) as r4,
       count(*) filter (where rating = 3) as r3,
       count(*) filter (where rating = 2) as r2,
       count(*) filter (where rating = 1) as r1
from reviews where status = 'approved' and not is_demo;

-- name: CountDemoReviews :one
select count(*) from reviews where is_demo;

-- name: CountPendingReviews :one
select count(*) from reviews where status = 'pending';

-- name: AdminListReviews :many
select r.*, m.width as photo_width, m.height as photo_height, m.widths as photo_widths,
       o.reference as order_reference, coalesce(o.status = 'delivered', false)::boolean as verified,
       a.email as moderator_email, count(*) over () as total_count
from reviews r
left join media m on m.id = r.photo_id
left join orders o on o.id = r.order_id
left join admins a on a.id = r.moderated_by
where (sqlc.narg(status)::review_status is null or r.status = sqlc.narg(status)::review_status)
  and (sqlc.narg(q)::text is null
       or r.author_name ilike '%' || sqlc.narg(q)::text || '%'
       or r.comment ilike '%' || sqlc.narg(q)::text || '%')
order by r.created_at desc
limit @max_rows::int offset @skip::int;

-- name: ReviewStatusCounts :many
select status, count(*) as n from reviews group by status;

-- name: GetReview :one
select * from reviews where id = $1;

-- name: ModerateReviews :many
update reviews
set status = @status::review_status, rejection_reason = sqlc.narg(reason)::text,
    moderated_at = now(), moderated_by = @admin_id::uuid
where id = any(@ids::uuid[]) and status <> @status::review_status
returning id;

-- name: SetReviewFeatured :exec
update reviews set is_featured = $2 where id = $1;

-- name: EditReviewComment :exec
update reviews
set original_comment = coalesce(original_comment, comment), comment = $2
where id = $1;

-- name: SetReviewReplies :exec
update reviews set reply_ar = $2, reply_fr = $3 where id = $1;

-- name: DeleteReview :one
delete from reviews where id = $1 returning photo_id;

-- name: CountApprovedDemoReviews :one
select count(*) from reviews where is_demo and status = 'approved';

-- name: ListStructuredDataReviews :many
-- Real published reviews for JSON-LD (never example reviews).
select author_name, rating, comment, language, created_at from reviews
where status = 'approved' and not is_demo
order by is_featured desc, created_at desc
limit 5;
