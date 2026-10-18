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
