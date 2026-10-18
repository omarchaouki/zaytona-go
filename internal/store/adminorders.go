package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
)

// ErrForbidden is returned when the actor's role may not do this (the handler
// checks first; destructive methods check again here).
var ErrForbidden = errors.New("store: forbidden")

// ErrEmptyOrder is returned when an edit would remove every item.
var ErrEmptyOrder = errors.New("store: an order keeps at least one item")

// Actor is the admin behind a change, for events and the audit log.
type Actor struct {
	AdminID uuid.UUID
	Role    dbq.AdminRole
	// IPHash is the keyed hash of the client IP (never the clear IP).
	IPHash []byte
}

func (a Actor) owner() bool { return a.Role == dbq.AdminRoleOwner }

// Audit writes one audit_logs row with q (pool or transaction).
func Audit(ctx context.Context, q *dbq.Queries, a Actor, action, entity, entityID string, meta map[string]any) error {
	p := dbq.InsertAuditLogParams{Action: action, IpHash: a.IPHash}
	if a.AdminID != uuid.Nil {
		p.AdminID = uuid.NullUUID{UUID: a.AdminID, Valid: true}
	}
	if entity != "" {
		p.Entity = &entity
	}
	if entityID != "" {
		p.EntityID = &entityID
	}
	if meta != nil {
		b, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		p.Meta = b
	}
	return q.InsertAuditLog(ctx, p)
}

func event(ctx context.Context, q *dbq.Queries, a Actor, orderID uuid.UUID, kind dbq.OrderEventKind, from, to *dbq.OrderStatus, note string) error {
	var n *string
	if note != "" {
		n = &note
	}
	return q.CreateOrderEvent(ctx, dbq.CreateOrderEventParams{
		OrderID: orderID, AdminID: uuid.NullUUID{UUID: a.AdminID, Valid: a.AdminID != uuid.Nil},
		Kind: kind, FromStatus: from, ToStatus: to, Note: n,
	})
}

// SetOrdersStatus moves orders to status (one transaction per order, row
// locked). Orders already in that status are skipped. Returns how many changed.
func (s *Store) SetOrdersStatus(ctx context.Context, a Actor, ids []uuid.UUID, status dbq.OrderStatus, note string) (int, error) {
	changed := 0
	for _, id := range ids {
		err := s.InTx(ctx, func(q *dbq.Queries) error {
			o, err := q.GetOrderForUpdate(ctx, id)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && o.Status == status) {
				return nil
			}
			if err != nil {
				return err
			}
			if _, err := q.UpdateOrderStatus(ctx, dbq.UpdateOrderStatusParams{ID: id, Status: status}); err != nil {
				return err
			}
			from, to := o.Status, status
			if err := event(ctx, q, a, id, dbq.OrderEventKindStatus, &from, &to, note); err != nil {
				return err
			}
			changed++
			return Audit(ctx, q, a, "order.status", "order", id.String(), map[string]any{"from": from, "to": to, "note": note != ""})
		})
		if err != nil {
			return changed, err
		}
	}
	return changed, nil
}

// SetOrdersPaid records the cash as received (or cancels it). Returns how many changed.
func (s *Store) SetOrdersPaid(ctx context.Context, a Actor, ids []uuid.UUID, paid bool) (int, error) {
	changed := 0
	err := s.InTx(ctx, func(q *dbq.Queries) error {
		for _, id := range ids {
			var n int64
			var err error
			if paid {
				n, err = q.MarkOrderPaid(ctx, dbq.MarkOrderPaidParams{ID: id, AdminID: uuid.NullUUID{UUID: a.AdminID, Valid: true}})
			} else {
				n, err = q.MarkOrderUnpaid(ctx, id)
			}
			if err != nil {
				return err
			}
			if n == 0 {
				continue
			}
			kind, action := dbq.OrderEventKindPaid, "order.paid"
			if !paid {
				kind, action = dbq.OrderEventKindUnpaid, "order.unpaid"
			}
			if err := event(ctx, q, a, id, kind, nil, nil, ""); err != nil {
				return err
			}
			if err := Audit(ctx, q, a, action, "order", id.String(), nil); err != nil {
				return err
			}
			changed++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return changed, nil
}

// OrderEdit changes quantities (0 removes a line) and the delivery fee
// (nil: to be confirmed by phone). Totals are recomputed from the stored unit prices.
type OrderEdit struct {
	Quantities  map[uuid.UUID]int32
	DeliveryFee *decimal.Decimal
}

// EditOrder applies e in one transaction.
func (s *Store) EditOrder(ctx context.Context, a Actor, id uuid.UUID, e OrderEdit) error {
	return s.InTx(ctx, func(q *dbq.Queries) error {
		o, err := q.GetOrderForUpdate(ctx, id)
		if err != nil {
			return NotFound(err)
		}
		items, err := q.ListOrderItems(ctx, id)
		if err != nil {
			return err
		}
		remaining := 0
		for _, it := range items {
			qty, ok := e.Quantities[it.ID]
			if !ok {
				qty = it.Quantity
			}
			if qty > 0 {
				remaining++
			}
		}
		if remaining == 0 {
			return ErrEmptyOrder
		}
		subtotal := decimal.Zero
		changedLines := 0
		for _, it := range items {
			qty, ok := e.Quantities[it.ID]
			if !ok || qty == it.Quantity {
				subtotal = subtotal.Add(it.UnitPrice.Mul(decimal.NewFromInt32(it.Quantity)))
				continue
			}
			changedLines++
			if qty <= 0 {
				if err := q.DeleteOrderItem(ctx, dbq.DeleteOrderItemParams{ID: it.ID, OrderID: id}); err != nil {
					return err
				}
				continue
			}
			qty = min(qty, 999)
			if err := q.UpdateOrderItemQuantity(ctx, dbq.UpdateOrderItemQuantityParams{ID: it.ID, OrderID: id, Quantity: qty}); err != nil {
				return err
			}
			subtotal = subtotal.Add(it.UnitPrice.Mul(decimal.NewFromInt32(qty)))
		}
		total := subtotal
		var fee decimal.NullDecimal
		if e.DeliveryFee != nil {
			total = total.Add(*e.DeliveryFee)
			fee = decimal.NullDecimal{Decimal: *e.DeliveryFee, Valid: true}
		}
		if err := q.UpdateOrderTotals(ctx, dbq.UpdateOrderTotalsParams{ID: id, Subtotal: subtotal, DeliveryFee: fee, Total: total}); err != nil {
			return err
		}
		if err := event(ctx, q, a, id, dbq.OrderEventKindEdited, nil, nil, ""); err != nil {
			return err
		}
		feeText := "tbc"
		if e.DeliveryFee != nil {
			feeText = e.DeliveryFee.StringFixed(2)
		}
		return Audit(ctx, q, a, "order.edit", "order", id.String(), map[string]any{
			"lines_changed": changedLines, "delivery_fee": feeText,
			"total_before": o.Total.StringFixed(2), "total_after": total.StringFixed(2),
		})
	})
}

// SetOrderNote saves the internal note ("" clears it).
func (s *Store) SetOrderNote(ctx context.Context, a Actor, id uuid.UUID, note string) error {
	return s.InTx(ctx, func(q *dbq.Queries) error {
		if _, err := q.GetOrderForUpdate(ctx, id); err != nil {
			return NotFound(err)
		}
		var n *string
		if note != "" {
			n = &note
		}
		if err := q.UpdateOrderInternalNote(ctx, dbq.UpdateOrderInternalNoteParams{ID: id, InternalNote: n}); err != nil {
			return err
		}
		if err := event(ctx, q, a, id, dbq.OrderEventKindNote, nil, nil, ""); err != nil {
			return err
		}
		return Audit(ctx, q, a, "order.note", "order", id.String(), map[string]any{"length": len([]rune(note))})
	})
}

// SoftDeleteOrders hides orders from lists, counts and exports (owner only).
// The rows stay for the audit trail and can be restored.
func (s *Store) SoftDeleteOrders(ctx context.Context, a Actor, ids []uuid.UUID) (int, error) {
	if !a.owner() {
		return 0, ErrForbidden
	}
	return s.toggleDeleted(ctx, a, ids, true)
}

// RestoreOrders undoes SoftDeleteOrders (owner only).
func (s *Store) RestoreOrders(ctx context.Context, a Actor, ids []uuid.UUID) (int, error) {
	if !a.owner() {
		return 0, ErrForbidden
	}
	return s.toggleDeleted(ctx, a, ids, false)
}

func (s *Store) toggleDeleted(ctx context.Context, a Actor, ids []uuid.UUID, deleted bool) (int, error) {
	changed := 0
	err := s.InTx(ctx, func(q *dbq.Queries) error {
		for _, id := range ids {
			var n int64
			var err error
			kind, action := dbq.OrderEventKindDeleted, "order.delete"
			if deleted {
				n, err = q.SoftDeleteOrder(ctx, id)
			} else {
				n, err = q.RestoreOrder(ctx, id)
				kind, action = dbq.OrderEventKindRestored, "order.restore"
			}
			if err != nil {
				return err
			}
			if n == 0 {
				continue
			}
			if err := event(ctx, q, a, id, kind, nil, nil, ""); err != nil {
				return err
			}
			if err := Audit(ctx, q, a, action, "order", id.String(), nil); err != nil {
				return err
			}
			changed++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return changed, nil
}
