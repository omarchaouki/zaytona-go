package db_test

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omarchaouki/zaytona-go/internal/db"
	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/db/dbtest"
	"github.com/omarchaouki/zaytona-go/internal/store"
)

var pgURL string

func TestMain(m *testing.M) { os.Exit(dbtest.RunMain(m, &pgURL)) }

func TestMigrateUpDownUp(t *testing.T) {
	pool := dbtest.NewDatabase(t, pgURL)
	ctx := context.Background()
	if err := db.MigrateDownAll(ctx, pool); err != nil {
		t.Fatalf("down: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, "select count(*) from information_schema.tables where table_schema = 'public' and table_name <> 'goose_db_version'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d tables left after down", n)
	}
	v, err := db.Migrate(ctx, pool)
	if err != nil {
		t.Fatalf("up again: %v", err)
	}
	if v < 1 {
		t.Fatalf("version %d", v)
	}
}

// On a Supabase-like database (roles anon and authenticated, which the Data
// API uses, with default access to new tables), migration 00002 takes every
// right away from those roles and turns row-level security on; the app, owner
// of the tables, still reads and writes.
func TestDataAPIRolesLockedOut(t *testing.T) {
	pool := dbtest.NewDatabase(t, pgURL)
	ctx := context.Background()
	for _, stmt := range []string{
		`do $$ begin
			if not exists (select 1 from pg_roles where rolname = 'anon') then create role anon nologin; end if;
			if not exists (select 1 from pg_roles where rolname = 'authenticated') then create role authenticated nologin; end if;
		end $$`,
		`alter default privileges in schema public grant all on tables to anon, authenticated`,
		`alter default privileges in schema public grant all on sequences to anon, authenticated`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.MigrateDownAll(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var open []string
	rows, err := pool.Query(ctx, `select c.relname from pg_class c join pg_namespace n on n.oid = c.relnamespace
		where n.nspname = 'public' and c.relkind = 'r'
		and (not c.relrowsecurity or has_table_privilege('anon', c.oid, 'select') or has_table_privilege('authenticated', c.oid, 'insert'))`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		open = append(open, name)
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	if len(open) > 0 {
		t.Fatalf("tables open to the Data API roles: %v", open)
	}
	// The app (owner) is not limited by row-level security.
	var n int
	if err := pool.QueryRow(ctx, "select count(*) from offers").Scan(&n); err != nil {
		t.Fatalf("owner read: %v", err)
	}
}

// Every application connection carries the 5 s statement timeout (set after
// connecting, so it also holds behind a session pooler).
func TestStatementTimeoutOnEveryConnection(t *testing.T) {
	pool := dbtest.NewDatabase(t, pgURL)
	ctx := context.Background()
	for range 3 {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var v string
		err = conn.QueryRow(ctx, "show statement_timeout").Scan(&v)
		conn.Release()
		if err != nil {
			t.Fatal(err)
		}
		if v != "5s" {
			t.Fatalf("statement_timeout = %q", v)
		}
	}
}

func TestOrderReferencesAreUniqueUnderConcurrency(t *testing.T) {
	pool := dbtest.NewDatabase(t, pgURL)
	s := store.New(pool)
	ctx := context.Background()

	const n = 25
	refs := make(chan string, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			o, err := s.Q.CreateOrder(ctx, dbq.CreateOrderParams{
				CustomerName: "Test Client",
				PhoneE164:    "+212612345678",
				City:         "Marrakech",
				Subtotal:     decimal.RequireFromString("2700"),
				Total:        decimal.RequireFromString("2700"),
				Locale:       dbq.LocaleAr,
				Source:       "landing",
			})
			if err != nil {
				t.Error(err)
				return
			}
			refs <- o.Reference
		})
	}
	wg.Wait()
	close(refs)
	seen := map[string]bool{}
	for r := range refs {
		if seen[r] {
			t.Fatalf("duplicate reference %s", r)
		}
		seen[r] = true
	}
	if len(seen) != n {
		t.Fatalf("got %d references, want %d", len(seen), n)
	}
}

func TestFirstOrderReferenceStartsAt10001(t *testing.T) {
	pool := dbtest.NewDatabase(t, pgURL)
	q := dbq.New(pool)
	o, err := q.CreateOrder(context.Background(), dbq.CreateOrderParams{
		CustomerName: "Test Client",
		PhoneE164:    "+212612345678",
		City:         "Rabat",
		Subtotal:     decimal.Zero,
		Total:        decimal.Zero,
		Locale:       dbq.LocaleFr,
		Source:       "landing",
	})
	if err != nil {
		t.Fatal(err)
	}
	if o.Number != 10001 {
		t.Fatalf("number %d", o.Number)
	}
	if want := "-010001"; o.Reference[len(o.Reference)-len(want):] != want {
		t.Fatalf("reference %s", o.Reference)
	}
}

func TestConstraintsRejectBadRows(t *testing.T) {
	pool := dbtest.NewDatabase(t, pgURL)
	q := dbq.New(pool)
	ctx := context.Background()
	_, err := q.CreateOrder(ctx, dbq.CreateOrderParams{
		CustomerName: "Test Client",
		PhoneE164:    "0612345678", // not E.164
		City:         "Rabat",
		Subtotal:     decimal.Zero,
		Total:        decimal.Zero,
		Locale:       dbq.LocaleFr,
		Source:       "landing",
	})
	if err == nil {
		t.Fatal("non-E.164 phone accepted")
	}
	_, err = q.CreateReview(ctx, dbq.CreateReviewParams{
		AuthorName: "Ali",
		Rating:     6,
		Comment:    "too high rating here",
		Language:   dbq.LocaleAr,
		Status:     dbq.ReviewStatusPending,
	})
	if err == nil {
		t.Fatal("rating 6 accepted")
	}
}

func TestUpdatedAtTrigger(t *testing.T) {
	pool := dbtest.NewDatabase(t, pgURL)
	ctx := context.Background()
	q := dbq.New(pool)
	if err := q.UpsertSetting(ctx, dbq.UpsertSettingParams{Key: "toggles", Value: []byte(`{"showReviews":true}`)}); err != nil {
		t.Fatal(err)
	}
	before, err := q.GetSetting(ctx, "toggles")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "select pg_sleep(0.01)"); err != nil {
		t.Fatal(err)
	}
	if err := q.UpsertSetting(ctx, dbq.UpsertSettingParams{Key: "toggles", Value: []byte(`{"showReviews":false}`)}); err != nil {
		t.Fatal(err)
	}
	after, err := q.GetSetting(ctx, "toggles")
	if err != nil {
		t.Fatal(err)
	}
	if !after.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("updated_at not bumped: %v -> %v", before.UpdatedAt, after.UpdatedAt)
	}
}

func TestPowSignatureSingleUse(t *testing.T) {
	pool := dbtest.NewDatabase(t, pgURL)
	q := dbq.New(pool)
	ctx := context.Background()
	sig := make([]byte, 32)
	sig[0] = 1
	exp := time.Now().Add(time.Hour)
	n, err := q.UsePowSignature(ctx, dbq.UsePowSignatureParams{SignatureHash: sig, ExpiresAt: exp})
	if err != nil || n != 1 {
		t.Fatalf("first use: n=%d err=%v", n, err)
	}
	n, err = q.UsePowSignature(ctx, dbq.UsePowSignatureParams{SignatureHash: sig, ExpiresAt: exp})
	if err != nil || n != 0 {
		t.Fatalf("replay accepted: n=%d err=%v", n, err)
	}
}
