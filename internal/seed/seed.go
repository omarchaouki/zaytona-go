// Package seed fills a fresh database with the starting content. Idempotent:
// it only fills what is missing and never overwrites the owner's edits. Every
// placeholder row is flagged (is_demo, pricing.isDemo, policies reviewed=false)
// so the admin lists exactly what the client must check or replace.
package seed

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omarchaouki/zaytona-go/internal/auth"
	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/domain"
	"github.com/omarchaouki/zaytona-go/internal/store"
)

// data.json is exported from the reference seed (client-approved copy, demo rows).
//
//go:embed data.json
var rawData []byte

type policy struct {
	Title    string `json:"title"`
	HTML     string `json:"html"`
	Reviewed bool   `json:"reviewed"`
}

type data struct {
	DemoPricePerLiter int `json:"demoPricePerLiter"`
	Offers            []struct {
		Slug      string `json:"slug"`
		Liters    int    `json:"liters"`
		NameAr    string `json:"nameAr"`
		NameFr    string `json:"nameFr"`
		TaglineAr string `json:"taglineAr"`
		TaglineFr string `json:"taglineFr"`
	} `json:"offers"`
	Reviews []struct {
		AuthorName string `json:"authorName"`
		City       string `json:"city"`
		Rating     int16  `json:"rating"`
		Comment    string `json:"comment"`
	} `json:"reviews"`
	FAQs []struct {
		QAr string `json:"qAr"`
		AAr string `json:"aAr"`
		QFr string `json:"qFr"`
		AFr string `json:"aFr"`
	} `json:"faqs"`
	Privacy map[string]policy `json:"privacy"`
	Terms   map[string]policy `json:"terms"`
}

// Options control the seed.
type Options struct {
	AdminEmail    string
	AdminPassword string
	// Development only: preview WhatsApp buttons.
	WhatsAppNumber string
	// Example reviews (flagged is_demo).
	DemoReviews bool
	Log         func(string)
}

// Run seeds the database in one transaction.
func Run(ctx context.Context, s *store.Store, opts Options) error {
	var d data
	if err := json.Unmarshal(rawData, &d); err != nil {
		return fmt.Errorf("seed data: %w", err)
	}
	log := opts.Log
	if log == nil {
		log = func(string) {}
	}

	defaults := domain.DefaultSettings()
	defaults.Pricing.PricePerLiter = decimal.NewNullDecimal(decimal.NewFromInt(int64(d.DemoPricePerLiter)))
	defaults.Pricing.IsDemo = true
	if opts.WhatsAppNumber != "" {
		n, ok := domain.NormalizeWhatsAppNumber(opts.WhatsAppNumber)
		if !ok {
			return errors.New("SEED_WHATSAPP_NUMBER is not a valid phone number")
		}
		defaults.WhatsApp.Number = n
	}

	var passwordHash string
	if opts.AdminEmail != "" && opts.AdminPassword != "" {
		h, err := auth.HashPassword(opts.AdminPassword)
		if err != nil {
			return err
		}
		passwordHash = h
	}

	return s.InTx(ctx, func(q *dbq.Queries) error {
		// Settings: defaults for groups that do not exist yet.
		existing, err := q.ListSettings(ctx)
		if err != nil {
			return err
		}
		have := map[string]bool{}
		for _, r := range existing {
			have[r.Key] = true
		}
		for _, key := range domain.SettingsKeys {
			if have[key] {
				continue
			}
			value, err := json.Marshal(defaults.SettingsGroup(key))
			if err != nil {
				return err
			}
			if err := q.InsertSettingIfMissing(ctx, dbq.InsertSettingIfMissingParams{Key: key, Value: value}); err != nil {
				return fmt.Errorf("settings.%s: %w", key, err)
			}
			log("settings." + key + ": defaults written")
		}

		// Demo offers (by slug), priced from the stored price per litre.
		row, err := q.GetSetting(ctx, "pricing")
		if err != nil {
			return err
		}
		pricing := domain.SettingsFromRows(map[string][]byte{"pricing": row.Value}).Pricing
		ppl := decimal.NewFromInt(int64(d.DemoPricePerLiter))
		if pricing.PricePerLiter.Valid {
			ppl = pricing.PricePerLiter.Decimal
		}
		for i, o := range d.Offers {
			if _, err := q.GetOfferBySlug(ctx, o.Slug); err == nil {
				continue
			} else if !errors.Is(store.NotFound(err), store.ErrNotFound) {
				return err
			}
			liters := decimal.NewFromInt(int64(o.Liters))
			_, err := q.CreateOffer(ctx, dbq.CreateOfferParams{
				Slug:        o.Slug,
				NameAr:      o.NameAr,
				NameFr:      o.NameFr,
				TaglineAr:   ptr(o.TaglineAr),
				TaglineFr:   ptr(o.TaglineFr),
				Liters:      liters,
				AutoPrice:   true,
				PriceMad:    liters.Mul(ppl).Round(2),
				StockStatus: dbq.StockStatusInStock,
				IsActive:    true,
				SortOrder:   int32(i), // #nosec G115 -- a handful of seed rows
				IsDemo:      true,
			})
			if err != nil {
				return fmt.Errorf("offer %s: %w", o.Slug, err)
			}
			log("offer " + o.Slug + ": demo row created")
		}

		// Example reviews, once.
		if opts.DemoReviews {
			n, err := q.CountDemoReviews(ctx)
			if err != nil {
				return err
			}
			if n == 0 {
				now := time.Now()
				for i, r := range d.Reviews {
					err := q.CreateDemoReview(ctx, dbq.CreateDemoReviewParams{
						AuthorName: r.AuthorName,
						City:       ptr(r.City),
						Rating:     r.Rating,
						Comment:    r.Comment,
						CreatedAt:  now.Add(-time.Duration(i+1) * 24 * time.Hour),
					})
					if err != nil {
						return fmt.Errorf("demo review: %w", err)
					}
				}
				log(fmt.Sprintf("reviews: %d example reviews created", len(d.Reviews)))
			}
		}

		// Demo FAQ, only on an empty table.
		n, err := q.CountFaqs(ctx)
		if err != nil {
			return err
		}
		if n == 0 {
			for i, f := range d.FAQs {
				_, err := q.CreateFaq(ctx, dbq.CreateFaqParams{
					QuestionAr: f.QAr, QuestionFr: f.QFr, AnswerAr: f.AAr, AnswerFr: f.AFr,
					SortOrder: int32(i), // #nosec G115 -- a handful of seed rows
					IsActive:  true,
					IsDemo:    true,
				})
				if err != nil {
					return fmt.Errorf("faq: %w", err)
				}
			}
			log(fmt.Sprintf("faq: %d demo questions created", len(d.FAQs)))
		}

		// Policy page drafts (need legal review).
		for key, p := range map[string]map[string]policy{"page_privacy": d.Privacy, "page_terms": d.Terms} {
			ar, err := json.Marshal(p["ar"])
			if err != nil {
				return err
			}
			fr, err := json.Marshal(p["fr"])
			if err != nil {
				return err
			}
			if _, err := q.GetContentBlock(ctx, key); err == nil {
				continue
			}
			if err := q.InsertContentBlockIfMissing(ctx, dbq.InsertContentBlockIfMissingParams{Key: key, ValueAr: ar, ValueFr: fr}); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
			log(key + ": draft written (needs legal review)")
		}

		// Owner account.
		if passwordHash == "" {
			log("admin: ADMIN_EMAIL / ADMIN_PASSWORD not set, no owner created")
			return nil
		}
		email := strings.TrimSpace(opts.AdminEmail)
		if _, err := q.GetAdminByEmail(ctx, email); err == nil {
			return nil
		} else if !errors.Is(store.NotFound(err), store.ErrNotFound) {
			return err
		}
		if _, err := q.CreateAdmin(ctx, dbq.CreateAdminParams{
			Email:              email,
			PasswordHash:       passwordHash,
			Role:               dbq.AdminRoleOwner,
			MustChangePassword: true,
			UiLocale:           dbq.LocaleAr,
		}); err != nil {
			return fmt.Errorf("owner: %w", err)
		}
		log("admin " + email + ": owner created (password change required at first login)")
		return nil
	})
}

func ptr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
