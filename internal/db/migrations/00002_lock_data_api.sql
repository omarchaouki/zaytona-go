-- Hosted Postgres providers that publish the database over an HTTP API
-- (Supabase's Data API, through the roles anon and authenticated) would let
-- anyone holding the public API key read and change these tables: orders hold
-- names, phones and addresses. The app never uses that API; it connects as the
-- owner of the tables, which row-level security does not restrict.
--
-- Where those roles exist: row-level security on every table (no policy, so
-- nothing is visible through them) and every grant to them removed, now and
-- for tables created later. On a plain PostgreSQL server (no such roles) this
-- migration changes nothing, so a separate non-owner app role keeps working.

-- +goose Up
-- +goose StatementBegin
DO $$
DECLARE
	api_role text;
	t record;
	locked boolean := false;
BEGIN
	FOREACH api_role IN ARRAY ARRAY['anon', 'authenticated'] LOOP
		IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = api_role) THEN
			locked := true;
			EXECUTE format('REVOKE ALL ON ALL TABLES IN SCHEMA public FROM %I', api_role);
			EXECUTE format('REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM %I', api_role);
			EXECUTE format('REVOKE ALL ON ALL FUNCTIONS IN SCHEMA public FROM %I', api_role);
			EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON TABLES FROM %I', api_role);
			EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON SEQUENCES FROM %I', api_role);
			EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON FUNCTIONS FROM %I', api_role);
		END IF;
	END LOOP;
	IF locked THEN
		FOR t IN SELECT tablename FROM pg_tables WHERE schemaname = 'public' LOOP
			EXECUTE format('ALTER TABLE public.%I ENABLE ROW LEVEL SECURITY', t.tablename);
		END LOOP;
	END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- Row-level security off again (the grants are not given back: the app never
-- needs them).
-- +goose StatementBegin
DO $$
DECLARE
	t record;
BEGIN
	IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname IN ('anon', 'authenticated')) THEN
		FOR t IN SELECT tablename FROM pg_tables WHERE schemaname = 'public' LOOP
			EXECUTE format('ALTER TABLE public.%I DISABLE ROW LEVEL SECURITY', t.tablename);
		END LOOP;
	END IF;
END
$$;
-- +goose StatementEnd
