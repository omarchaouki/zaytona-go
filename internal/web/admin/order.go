package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/omarchaouki/zaytona-go/internal/auth"
	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/domain"
	"github.com/omarchaouki/zaytona-go/internal/i18n"
	"github.com/omarchaouki/zaytona-go/internal/store"
	"github.com/omarchaouki/zaytona-go/internal/view"
)

func orderID(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	return id, err == nil
}

// describeAttribution is "facebook · spring-sale · video-a" for the admin.
func describeAttribution(raw []byte) string {
	var a map[string]string
	if len(raw) == 0 || json.Unmarshal(raw, &a) != nil {
		return ""
	}
	var parts []string
	for _, k := range []string{"utm_source", "utm_medium", "utm_campaign", "utm_content"} {
		if a[k] != "" {
			parts = append(parts, a[k])
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, " · ")
	}
	if a["fbclid"] != "" {
		return "facebook / instagram (fbclid)"
	}
	return ""
}

// Order renders the order page (?edit=1 opens the items form).
func (h *Handlers) Order(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	h.renderOrder(w, r, cur, http.StatusOK, r.URL.Query().Get("edit") == "1", "")
}

func (h *Handlers) renderOrder(w http.ResponseWriter, r *http.Request, cur *auth.Current, status int, editing bool, editErr string) {
	ctx := r.Context()
	id, ok := orderID(r)
	if !ok {
		h.orderNotFound(w, r, cur)
		return
	}
	o, err := h.Store.Q.GetOrder(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		h.orderNotFound(w, r, cur)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	items, err := h.Store.Q.ListOrderItems(ctx, id)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	events, err := h.Store.Q.ListOrderEventsWithAdmin(ctx, id)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	site, err := h.Site.Get(ctx)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	p := h.page(r, cur, i18n.AdminNavOrders, "orders")
	p.Title = o.Reference + " | " + p.T(i18n.AdminNavBrand)
	ar := o.Locale == dbq.LocaleAr
	tpl, siteName := site.Settings.Templates.CustomerFr, site.Settings.Identity.NameFr
	if ar {
		tpl, siteName = site.Settings.Templates.CustomerAr, site.Settings.Identity.NameAr
	}
	first, _, _ := strings.Cut(strings.TrimSpace(o.CustomerName), " ")
	d := view.OrderDetail{
		ID: o.ID.String(), Reference: o.Reference, Customer: o.CustomerName, Phone: o.PhoneE164,
		City: o.City, Address: deref(o.Address), Note: deref(o.Note), InternalNote: deref(o.InternalNote),
		Status: string(o.Status), Locale: string(o.Locale), Source: describeAttribution(o.Attribution),
		SiteName: siteName, Subtotal: o.Subtotal, Total: o.Total, PaidAt: o.PaidAt, Seen: o.SeenAt != nil,
		CreatedAt: o.CreatedAt, Editing: editing, EditError: editErr,
		WhatsAppHref: domain.WhatsAppLink(strings.TrimPrefix(o.PhoneE164, "+"), domain.FillTemplate(tpl, map[string]string{
			"name": first, "reference": o.Reference,
		})),
	}
	if o.DeliveryFee.Valid {
		fee := o.DeliveryFee.Decimal
		d.DeliveryFee = &fee
	}
	for _, it := range items {
		name := it.NameFr
		if p.Locale == i18n.AR {
			name = it.NameAr
		}
		d.Items = append(d.Items, view.OrderItemRow{ID: it.ID.String(), Name: name, UnitPrice: it.UnitPrice, Quantity: it.Quantity})
	}
	for _, e := range events {
		row := view.OrderEventRow{Kind: string(e.Kind), Note: deref(e.Note), At: e.CreatedAt, By: deref(e.AdminEmail)}
		if e.FromStatus != nil {
			row.From = string(*e.FromStatus)
		}
		if e.ToStatus != nil {
			row.To = string(*e.ToStatus)
		}
		d.Events = append(d.Events, row)
	}
	render(w, r, status, p.Nonce, view.AdminOrder(p, d))
}

func (h *Handlers) orderNotFound(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	p := h.page(r, cur, i18n.AdminOrderNotFound, "orders")
	p.Title = p.T(i18n.AdminOrderNotFound)
	render(w, r, http.StatusNotFound, p.Nonce, view.AdminMessage(p, p.T(i18n.AdminOrderNotFound), "/admin/orders", p.T(i18n.AdminOrderBack)))
}

// orderExists answers 404 for an unknown or deleted order id.
func (h *Handlers) orderExists(w http.ResponseWriter, r *http.Request, cur *auth.Current, id uuid.UUID) bool {
	_, err := h.Store.Q.GetOrder(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		h.orderNotFound(w, r, cur)
		return false
	}
	if err != nil {
		h.serverError(w, r, err)
		return false
	}
	return true
}

// OrderSeen marks an order as opened (posted by admin.js when the page shows).
func (h *Handlers) OrderSeen(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	id, ok := orderID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !h.orderExists(w, r, cur, id) {
		return
	}
	h.markSeen(r, cur, id)
	if r.Header.Get("X-Requested-With") == "fetch" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/admin/orders/"+id.String(), http.StatusSeeOther) //nolint:gosec // G710: fixed path + parsed UUID
}

// markSeen turns the order's red "new" state off (any action on it does too).
func (h *Handlers) markSeen(r *http.Request, cur *auth.Current, id uuid.UUID) {
	ctx := r.Context()
	o, err := h.Store.Q.GetOrder(ctx, id)
	if err != nil || o.SeenAt != nil {
		return
	}
	if err := h.Store.Q.MarkOrderSeen(ctx, id); err != nil {
		h.Logger.ErrorContext(ctx, "mark seen", "err", err)
		return
	}
	h.audit(ctx, r, &cur.Admin.ID, "order.seen", "order", id.String(), nil)
}

func (h *Handlers) afterOrderAction(w http.ResponseWriter, r *http.Request, id uuid.UUID, done string) {
	target := "/admin/orders/" + id.String()
	if done != "" {
		target += "?done=" + done
	}
	http.Redirect(w, r, target, http.StatusSeeOther) //nolint:gosec // G710: fixed path + parsed UUID + fixed flash key
}

// OrderStatus changes the status, with an optional note on the timeline.
func (h *Handlers) OrderStatus(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	id, ok := orderID(r)
	status := r.PostFormValue("status")
	if !ok {
		h.orderNotFound(w, r, cur)
		return
	}
	if !slices.Contains(view.OrderStatuses, status) {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	note, valid := domain.CleanText(r.PostFormValue("note"), false)
	if !valid || utf8.RuneCountInString(note) > 500 {
		http.Error(w, "bad note", http.StatusBadRequest)
		return
	}
	if !h.orderExists(w, r, cur, id) {
		return
	}
	h.markSeen(r, cur, id)
	if _, err := h.Store.SetOrdersStatus(r.Context(), actor(r, h, cur), []uuid.UUID{id}, dbq.OrderStatus(status), note); err != nil {
		h.serverError(w, r, err)
		return
	}
	h.afterOrderAction(w, r, id, "saved")
}

// OrderPaid records the cash as received (paid=1) or cancels it (paid=0).
func (h *Handlers) OrderPaid(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	id, ok := orderID(r)
	paid := r.PostFormValue("paid")
	if !ok {
		h.orderNotFound(w, r, cur)
		return
	}
	if paid != "0" && paid != "1" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !h.orderExists(w, r, cur, id) {
		return
	}
	h.markSeen(r, cur, id)
	if _, err := h.Store.SetOrdersPaid(r.Context(), actor(r, h, cur), []uuid.UUID{id}, paid == "1"); err != nil {
		h.serverError(w, r, err)
		return
	}
	h.afterOrderAction(w, r, id, "saved")
}

// OrderEdit saves quantities (q_<item id>) and the delivery fee ("" = to confirm).
func (h *Handlers) OrderEdit(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	id, ok := orderID(r)
	if !ok {
		h.orderNotFound(w, r, cur)
		return
	}
	p := h.page(r, cur, i18n.AdminNavOrders, "orders")
	edit := store.OrderEdit{Quantities: map[uuid.UUID]int32{}}
	for key, vals := range r.PostForm {
		raw, found := strings.CutPrefix(key, "q_")
		if !found || len(vals) != 1 {
			continue
		}
		itemID, err := uuid.Parse(raw)
		n, err2 := strconv.Atoi(strings.TrimSpace(vals[0]))
		if err != nil || err2 != nil || n < 0 || n > 999 {
			h.renderOrder(w, r, cur, http.StatusUnprocessableEntity, true, p.T(i18n.AdminOrderError))
			return
		}
		edit.Quantities[itemID] = int32(n) //nolint:gosec // G109: n checked to be 0..999 above
	}
	if fee := strings.TrimSpace(strings.ReplaceAll(r.PostFormValue("fee"), ",", ".")); fee != "" {
		v, err := decimal.NewFromString(fee)
		if err != nil || v.IsNegative() || v.GreaterThan(decimal.NewFromInt(100000)) || v.Exponent() < -2 {
			h.renderOrder(w, r, cur, http.StatusUnprocessableEntity, true, p.T(i18n.AdminOrderError))
			return
		}
		edit.DeliveryFee = &v
	}
	err := h.Store.EditOrder(ctx, actor(r, h, cur), id, edit)
	switch {
	case errors.Is(err, store.ErrEmptyOrder):
		h.renderOrder(w, r, cur, http.StatusUnprocessableEntity, true, p.T(i18n.AdminOrderEmptyError))
		return
	case errors.Is(err, store.ErrNotFound):
		h.orderNotFound(w, r, cur)
		return
	case err != nil:
		h.serverError(w, r, err)
		return
	}
	h.markSeen(r, cur, id)
	h.afterOrderAction(w, r, id, "saved")
}

// OrderNote saves the internal note (never shown to the customer).
func (h *Handlers) OrderNote(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	id, ok := orderID(r)
	note, valid := domain.CleanText(r.PostFormValue("note"), true)
	if !ok || !valid || utf8.RuneCountInString(note) > 2000 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	err := h.Store.SetOrderNote(r.Context(), actor(r, h, cur), id, note)
	if errors.Is(err, store.ErrNotFound) {
		h.orderNotFound(w, r, cur)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.markSeen(r, cur, id)
	h.afterOrderAction(w, r, id, "saved")
}

// OrderDelete soft-deletes one order (owner; asks first without JS).
func (h *Handlers) OrderDelete(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	id, ok := orderID(r)
	if !ok {
		h.orderNotFound(w, r, cur)
		return
	}
	h.deleteOrders(w, r, cur, []uuid.UUID{id}, r.PostFormValue("return"))
}
