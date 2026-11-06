-- Two kinds of products: olive oil sold by the litre (as before) and products
-- sold by the jar, weighed in grams (amlou with almonds). A jar has liters = 0
-- (the litre columns keep their type, so every existing query and total stays
-- right: jars add no litres) and its weight in grams. Jars always have a fixed
-- price; only oil offers follow the price per litre.
-- Order lines keep the grams of a jar at order time, like the litres of oil.

-- +goose Up
create type offer_kind as enum ('oil', 'jar');

alter table offers add column kind offer_kind not null default 'oil';
alter table offers add column grams integer check (grams between 1 and 100000);
alter table offers drop constraint offers_liters_check;
alter table offers add constraint offers_size_check check (
    (kind = 'oil' and liters > 0 and grams is null)
    or (kind = 'jar' and liters = 0 and grams is not null)
);
alter table offers add constraint offers_jar_fixed_price check (kind = 'oil' or not auto_price);

alter table order_items drop constraint order_items_liters_check;
alter table order_items add constraint order_items_liters_check check (liters >= 0);
alter table order_items add column grams integer check (grams between 1 and 100000);

-- +goose Down
delete from order_items where liters = 0;
alter table order_items drop column grams;
alter table order_items drop constraint order_items_liters_check;
alter table order_items add constraint order_items_liters_check check (liters > 0);

delete from offers where kind = 'jar';
alter table offers drop constraint offers_jar_fixed_price;
alter table offers drop constraint offers_size_check;
alter table offers add constraint offers_liters_check check (liters > 0);
alter table offers drop column grams;
alter table offers drop column kind;
drop type offer_kind;
