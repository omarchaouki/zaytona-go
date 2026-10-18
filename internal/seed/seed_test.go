package seed_test

import (
	"context"
	"os"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/omarchaouki/zaytona-go/internal/auth"
	"github.com/omarchaouki/zaytona-go/internal/db/dbtest"
	"github.com/omarchaouki/zaytona-go/internal/domain"
	"github.com/omarchaouki/zaytona-go/internal/seed"
	"github.com/omarchaouki/zaytona-go/internal/store"
)

var pgURL string

func TestMain(m *testing.M) { os.Exit(dbtest.RunMain(m, &pgURL)) }

func TestSeedIsIdempotentAndFlagsDemoRows(t *testing.T) {
	s := store.New(dbtest.NewDatabase(t, pgURL))
	ctx := context.Background()
	opts := seed.Options{
		AdminEmail:    "owner@example.com",
		AdminPassword: "a-long-test-password",
		DemoReviews:   true,
	}
	for range 2 {
		if err := seed.Run(ctx, s, opts); err != nil {
			t.Fatal(err)
		}
	}

	offers, err := s.Q.ListAllOffers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(offers) != 3 {
		t.Fatalf("%d offers", len(offers))
	}
	for _, o := range offers {
		if !o.IsDemo || !o.AutoPrice {
			t.Errorf("offer %s not flagged demo/auto", o.Slug)
		}
		if want := o.Liters.Mul(domainPrice(t, s)); !o.PriceMad.Equal(want) {
			t.Errorf("offer %s price %s, want %s", o.Slug, o.PriceMad, want)
		}
	}

	if n, _ := s.Q.CountDemoReviews(ctx); n != 4 {
		t.Fatalf("%d demo reviews", n)
	}
	sum, err := s.Q.ReviewSummary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Total != 0 {
		t.Fatalf("demo reviews counted in summary: %d", sum.Total)
	}
	if n, _ := s.Q.CountFaqs(ctx); n != 10 {
		t.Fatalf("%d faqs", n)
	}

	a, err := s.Q.GetAdminByEmail(ctx, "OWNER@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if a.Role != "owner" || !a.MustChangePassword {
		t.Fatalf("owner row: role=%s mustChange=%v", a.Role, a.MustChangePassword)
	}
	if ok, _ := auth.VerifyPassword(a.PasswordHash, "a-long-test-password"); !ok {
		t.Fatal("owner password does not verify")
	}
	admins, _ := s.Q.ListAdmins(ctx)
	if len(admins) != 1 {
		t.Fatalf("%d admins after two seeds", len(admins))
	}
}

func domainPrice(t *testing.T, s *store.Store) decimal.Decimal {
	t.Helper()
	row, err := s.Q.GetSetting(context.Background(), "pricing")
	if err != nil {
		t.Fatal(err)
	}
	p := domain.SettingsFromRows(map[string][]byte{"pricing": row.Value}).Pricing
	if !p.PricePerLiter.Valid || !p.IsDemo {
		t.Fatalf("pricing not seeded as demo: %+v", p)
	}
	return p.PricePerLiter.Decimal
}
