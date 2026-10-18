-- name: ListActiveOffers :many
select * from offers where is_active order by sort_order, liters;

-- name: ListAllOffers :many
select * from offers order by sort_order, liters;

-- name: GetOffer :one
select * from offers where id = $1;

-- name: GetOfferBySlug :one
select * from offers where slug = $1;

-- name: GetActiveOffersByIDs :many
select * from offers where id = any(@ids::uuid[]) and is_active;

-- name: CountOffers :one
select count(*) from offers;

-- name: CreateOffer :one
insert into offers (slug, name_ar, name_fr, tagline_ar, tagline_fr, liters, auto_price, price_mad,
                    image_id, stock_status, is_active, sort_order, is_demo)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
returning *;

-- name: UpdateOffer :one
update offers
set slug = $2, name_ar = $3, name_fr = $4, tagline_ar = $5, tagline_fr = $6, liters = $7,
    auto_price = $8, price_mad = $9, image_id = $10, stock_status = $11, is_active = $12,
    sort_order = $13, is_demo = false
where id = $1
returning *;

-- name: RepriceAutoOffers :execrows
update offers set price_mad = round(liters * @price_per_liter::numeric, 2)
where auto_price;

-- name: DeleteOffer :exec
delete from offers where id = $1;

-- name: ListActiveOffersWithImage :many
select o.*, m.width as image_width, m.height as image_height, m.widths as image_widths
from offers o
left join media m on m.id = o.image_id
where o.is_active
order by o.sort_order, o.liters;

-- name: CountActiveDemoOffers :one
select count(*) from offers where is_demo and is_active;

-- name: ListOffersForFilter :many
select id, name_ar, name_fr from offers order by sort_order, created_at;

-- name: ListAllOffersWithImage :many
select o.*, m.width as image_width, m.height as image_height, m.widths as image_widths
from offers o
left join media m on m.id = o.image_id
order by o.sort_order, o.liters;
