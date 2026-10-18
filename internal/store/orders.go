package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/domain"
)

// DuplicateWindow is the duplicate guard window: the same phone ordering the
// same offer within it gets a warning instead of a second order (unless it confirms).
const DuplicateWindow = 10 * time.Minute

// NewOrder is a validated, priced order ready to be stored.
type NewOrder struct {
	Order       domain.ValidOrder
	Lines       []domain.OrderLine
	Totals      domain.Totals
	Locale      dbq.Locale
	Attribution map[string]string
	IPHash      []byte
	// DuplicateOfferID scopes the duplicate guard ("" = any order from this phone).
	DuplicateOfferID string
	// ConfirmDuplicate: the customer saw the warning and sends anyway.
	ConfirmDuplicate bool
}

// PlaceResult is the outcome of PlaceOrder: Created or a Duplicate warning.
type PlaceResult struct {
	Created   *dbq.Order
	Duplicate *DuplicateInfo
}

// DuplicateInfo describes the earlier order the customer probably repeated.
type DuplicateInfo struct {
	Reference  string
	MinutesAgo int
}

// PlaceOrder stores an order in one transaction. Submissions from the same
// phone are serialized by an advisory lock, so two taps on "Send" (or two tabs)
// arriving together cannot both pass the duplicate check.
func (s *Store) PlaceOrder(ctx context.Context, o NewOrder) (PlaceResult, error) {
	if len(o.Lines) == 0 {
		return PlaceResult{}, errors.New("store: order without lines")
	}
	var attribution []byte
	if len(o.Attribution) > 0 {
		b, err := json.Marshal(o.Attribution)
		if err != nil {
			return PlaceResult{}, err
		}
		attribution = b
	}
	var res PlaceResult
	err := s.InTx(ctx, func(q *dbq.Queries) error {
		if err := q.LockPhone(ctx, o.Order.PhoneE164); err != nil {
			return fmt.Errorf("lock: %w", err)
		}
		if !o.ConfirmDuplicate {
			params := dbq.RecentDuplicateOrderParams{PhoneE164: o.Order.PhoneE164, Since: time.Now().Add(-DuplicateWindow)}
			if o.DuplicateOfferID != "" {
				id, err := uuid.Parse(o.DuplicateOfferID)
				if err != nil {
					return err
				}
				params.OfferID = uuid.NullUUID{UUID: id, Valid: true}
			}
			recent, err := q.RecentDuplicateOrder(ctx, params)
			switch {
			case err == nil:
				res.Duplicate = &DuplicateInfo{
					Reference:  recent.Reference,
					MinutesAgo: max(1, int(time.Since(recent.CreatedAt).Round(time.Minute)/time.Minute)),
				}
				return nil
			case !errors.Is(err, pgx.ErrNoRows):
				return fmt.Errorf("duplicate check: %w", err)
			}
		}

		order, err := q.CreateOrder(ctx, dbq.CreateOrderParams{
			CustomerName: o.Order.Name,
			PhoneE164:    o.Order.PhoneE164,
			City:         o.Order.City,
			Address:      optional(o.Order.Address),
			Note:         optional(o.Order.Note),
			Subtotal:     o.Totals.Subtotal,
			DeliveryFee:  o.Totals.DeliveryFee,
			Total:        o.Totals.Total,
			Locale:       o.Locale,
			Source:       "landing",
			Attribution:  attribution,
			IpHash:       o.IPHash,
		})
		if err != nil {
			return fmt.Errorf("create order: %w", err)
		}
		for i, l := range o.Lines {
			item := dbq.CreateOrderItemParams{
				OrderID:   order.ID,
				NameAr:    l.NameAr,
				NameFr:    l.NameFr,
				Liters:    l.Liters,
				UnitPrice: l.UnitPrice,
				Quantity:  int32(l.Quantity), // #nosec G115 -- 1..999, validated
				Position:  int16(i),          // #nosec G115 -- one or two lines
			}
			if l.OfferID != "" {
				id, err := uuid.Parse(l.OfferID)
				if err != nil {
					return err
				}
				item.OfferID = uuid.NullUUID{UUID: id, Valid: true}
			}
			if err := q.CreateOrderItem(ctx, item); err != nil {
				return fmt.Errorf("create item: %w", err)
			}
		}
		status := dbq.OrderStatusNew
		note := "placed on website"
		if err := q.CreateOrderEvent(ctx, dbq.CreateOrderEventParams{
			OrderID: order.ID, Kind: dbq.OrderEventKindCreated, ToStatus: &status, Note: &note,
		}); err != nil {
			return fmt.Errorf("create event: %w", err)
		}
		res.Created = &order
		return nil
	})
	return res, err
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
