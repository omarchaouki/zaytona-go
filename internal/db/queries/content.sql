-- name: ListActiveFaqs :many
select * from faqs where is_active order by sort_order, created_at;

-- name: ListAllFaqs :many
select * from faqs order by sort_order, created_at;

-- name: CountFaqs :one
select count(*) from faqs;

-- name: CreateFaq :one
insert into faqs (question_ar, question_fr, answer_ar, answer_fr, sort_order, is_active, is_demo)
values ($1, $2, $3, $4, $5, $6, $7)
returning *;

-- name: UpdateFaq :exec
update faqs
set question_ar = $2, question_fr = $3, answer_ar = $4, answer_fr = $5, is_active = $6, is_demo = false
where id = $1;

-- name: SetFaqSortOrder :exec
update faqs set sort_order = $2 where id = $1;

-- name: DeleteFaq :exec
delete from faqs where id = $1;

-- name: ListPublicCertifications :many
select * from certifications where is_public order by sort_order, created_at;

-- name: ListAllCertifications :many
select * from certifications order by sort_order, created_at;

-- name: GetCertification :one
select * from certifications where id = $1;

-- name: CreateCertification :one
insert into certifications (type, label_ar, label_fr, number, issued_at, expires_at, scan_id, is_public, sort_order)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
returning *;

-- name: UpdateCertification :exec
update certifications
set type = $2, label_ar = $3, label_fr = $4, number = $5, issued_at = $6, expires_at = $7,
    scan_id = $8, is_public = $9, sort_order = $10
where id = $1;

-- name: DeleteCertification :exec
delete from certifications where id = $1;

-- name: ListPublicLabReports :many
select * from lab_reports where is_public order by analysed_at desc nulls last;

-- name: ListGalleryImages :many
select g.*, m.width, m.height, m.widths
from gallery_images g join media m on m.id = g.media_id
order by g.sort_order, g.created_at;

-- name: CreateMedia :one
insert into media (id, kind, width, height, widths, created_by)
values ($1, $2, $3, $4, $5, $6)
returning *;

-- name: GetMedia :one
select * from media where id = $1;

-- name: IsMediaPublic :one
-- Certificate scans, lab reports and review photos are public only while their owner row is.
select (case
  when m.kind = 'certificate_scan' then exists (select 1 from certifications c where c.scan_id = m.id and c.is_public)
  when m.kind = 'lab_report' then exists (select 1 from lab_reports l where l.file_id = m.id and l.is_public)
  when m.kind = 'review_photo' then exists (select 1 from reviews r where r.photo_id = m.id and r.status = 'approved')
  else true
end)::boolean as public
from media m where m.id = $1;

-- name: ListPublicCertificationsWithScan :many
select c.*, m.width as scan_width, m.height as scan_height, m.widths as scan_widths
from certifications c
left join media m on m.id = c.scan_id
where c.is_public
order by c.sort_order, c.created_at;

-- name: CountPublicLabReports :one
select count(*) from lab_reports where is_public;

-- name: ListAllCertificationsWithScan :many
select c.*, m.width as scan_width, m.height as scan_height, m.widths as scan_widths
from certifications c
left join media m on m.id = c.scan_id
order by c.sort_order, c.created_at;

-- name: DeleteMedia :exec
-- Only images no row points to any more (the caller checked).
delete from media where id = $1;

-- name: MediaInUse :one
select exists (select 1 from offers where image_id = @id::text)
    or exists (select 1 from certifications where scan_id = @id::text)
    or exists (select 1 from gallery_images where media_id = @id::text)
    or exists (select 1 from lab_reports where file_id = @id::text)
    or exists (select 1 from reviews where photo_id = @id::text);

-- name: GetFaq :one
select * from faqs where id = $1;

-- name: CreateGalleryImage :one
insert into gallery_images (media_id, caption_ar, caption_fr, sort_order)
values ($1, $2, $3, coalesce((select max(sort_order) + 1 from gallery_images), 0))
returning *;

-- name: GetGalleryImage :one
select * from gallery_images where id = $1;

-- name: UpdateGalleryCaption :exec
update gallery_images set caption_ar = $2, caption_fr = $3 where id = $1;

-- name: SetGallerySortOrder :exec
update gallery_images set sort_order = $2 where id = $1;

-- name: DeleteGalleryImage :exec
delete from gallery_images where id = $1;
