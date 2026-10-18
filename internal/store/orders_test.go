package store_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/db/dbtest"
	"github.com/omarchaouki/zaytona-go/internal/domain"
	"github.com/omarchaouki/zaytona-go/internal/seed"
	"github.com/omarchaouki/zaytona-go/internal/store"
)

var pgURL string

func TestMain(m *testing.M) { os.Exit(dbtest.RunMain(m, &pgURL)) }

func newOrder(ctx context.Context, t *testing.T, s *store.Store, phone string) store.NewOrder {
	t.Helper()
	offer, err := s.Q.GetOfferBySlug(ctx, "huile-olive-30l")
	if err != nil {
		t.Fatal(err)
	}
	lines := []domain.OrderLine{{
		OfferID: offer.ID.String(), NameAr: offer.NameAr, NameFr: offer.NameFr,
		Liters: offer.Liters, UnitPrice: offer.PriceMad, Quantity: 1,
	}}
	return store.NewOrder{
		Order:            domain.ValidOrder{Choice: offer.Slug, Name: "Test Client", PhoneE164: phone, City: "Rabat"},
		Lines:            lines,
		Totals:           domain.ComputeTotals(lines, domain.DeliverySettings{Mode: domain.DeliveryConfirm}),
		Locale:           dbq.LocaleFr,
		Attribution:      map[string]string{"utm_source": "facebook"},
		DuplicateOfferID: offer.ID.String(),
	}
}

func seeded(t *testing.T) *store.Store {
	t.Helper()
	s := store.New(dbtest.NewDatabase(t, pgURL))
	if err := seed.Run(context.Background(), s, seed.Options{}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestParallelSubmitsFromOnePhoneCreateOneOrder(t *testing.T) {
	s := seeded(t)
	ctx := context.Background()
	var created, duplicates int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range 25 {
		wg.Go(func() {
			res, err := s.PlaceOrder(ctx, newOrder(ctx, t, s, "+212612345678"))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil:
				t.Error(err)
			case res.Created != nil:
				created++
			case res.Duplicate != nil:
				duplicates++
			}
		})
	}
	wg.Wait()
	if created != 1 || duplicates != 24 {
		t.Fatalf("created %d, duplicates %d; want 1 and 24", created, duplicates)
	}

	// The customer confirms it is a new order: it goes through.
	o := newOrder(ctx, t, s, "+212612345678")
	o.ConfirmDuplicate = true
	res, err := s.PlaceOrder(ctx, o)
	if err != nil || res.Created == nil {
		t.Fatalf("confirmed duplicate: %+v %v", res, err)
	}
}

func TestParallelSubmitsFromManyPhonesGetDistinctReferences(t *testing.T) {
	s := seeded(t)
	ctx := context.Background()
	refs := make(chan string, 25)
	var wg sync.WaitGroup
	for i := range 25 {
		wg.Go(func() {
			phone := fmt.Sprintf("+21261234%02d00", i)
			res, err := s.PlaceOrder(ctx, newOrder(ctx, t, s, phone))
			if err != nil || res.Created == nil {
				t.Errorf("%s: %+v %v", phone, res, err)
				return
			}
			refs <- res.Created.Reference
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
	if len(seen) != 25 {
		t.Fatalf("%d orders", len(seen))
	}
}

func TestPlacedOrderIsComplete(t *testing.T) {
	s := seeded(t)
	ctx := context.Background()
	res, err := s.PlaceOrder(ctx, newOrder(ctx, t, s, "+212712345678"))
	if err != nil || res.Created == nil {
		t.Fatal(res, err)
	}
	o := res.Created
	if !o.Total.Equal(decimal.RequireFromString("2700")) || o.DeliveryFee.Valid || o.SeenAt != nil {
		t.Fatalf("order: %+v", o)
	}
	items, _ := s.Q.ListOrderItems(ctx, o.ID)
	events, _ := s.Q.ListOrderEvents(ctx, o.ID)
	if len(items) != 1 || len(events) != 1 || events[0].Kind != dbq.OrderEventKindCreated {
		t.Fatalf("items %d events %+v", len(items), events)
	}
	if string(o.Attribution) != `{"utm_source": "facebook"}` && string(o.Attribution) != `{"utm_source":"facebook"}` {
		t.Fatalf("attribution %s", o.Attribution)
	}
}

func TestCleanupRetention(t *testing.T) {
	s := seeded(t)
	ctx := context.Background()
	for _, q := range []string{
		`insert into rate_limits (key, window_start, count) values ('old', now() - interval '3 days', 1), ('new', now(), 1)`,
		`insert into pow_used (signature_hash, expires_at) values (sha256('a'), now() - interval '1 hour'), (sha256('b'), now() + interval '1 hour')`,
	} {
		if _, err := s.Pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	r, err := s.Cleanup(ctx, time.Now())
	if err != nil || r.RateLimits != 1 || r.Pow != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	var n int
	_ = s.Pool.QueryRow(ctx, "select count(*) from rate_limits").Scan(&n)
	if n != 1 {
		t.Fatalf("%d rate limit rows left", n)
	}
}
