package admin

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/omarchaouki/zaytona-go/internal/auth"
	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/domain"
	"github.com/omarchaouki/zaytona-go/internal/i18n"
	"github.com/omarchaouki/zaytona-go/internal/store"
	"github.com/omarchaouki/zaytona-go/internal/view"
)

// orderRoutes registers the dashboard and order pages.
func (h *Handlers) orderRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin", h.guard(roleAny, 0, h.Dashboard))
	mux.HandleFunc("GET /admin/{$}", h.guard(roleAny, 0, h.Dashboard))
	mux.HandleFunc("GET /admin/notifications", h.guard(roleAny, 0, h.Notifications))
	mux.HandleFunc("GET /admin/orders", h.guard(roleAny, 0, h.Orders))
	mux.HandleFunc("GET /admin/orders.csv", h.guard(roleAny, 0, h.OrdersCSV))
	post(mux, "POST /admin/orders/bulk", h.guard(roleAny, 0, h.OrdersBulk))
	post(mux, "POST /admin/orders/seen-all", h.guard(roleAny, 0, h.MarkAllSeen))
	post(mux, "POST /admin/orders/restore", h.guard(roleOwner, 0, h.OrdersRestore))
	mux.HandleFunc("GET /admin/orders/{id}", h.guard(roleAny, 0, h.Order))
	post(mux, "POST /admin/orders/{id}/seen", h.guard(roleAny, 0, h.OrderSeen))
	post(mux, "POST /admin/orders/{id}/status", h.guard(roleAny, 0, h.OrderStatus))
	post(mux, "POST /admin/orders/{id}/paid", h.guard(roleAny, 0, h.OrderPaid))
	post(mux, "POST /admin/orders/{id}/edit", h.guard(roleAny, 0, h.OrderEdit))
	post(mux, "POST /admin/orders/{id}/note", h.guard(roleAny, 0, h.OrderNote))
	post(mux, "POST /admin/orders/{id}/delete", h.guard(roleOwner, 0, h.OrderDelete))
}

func actor(r *http.Request, h *Handlers, cur *auth.Current) store.Actor {
	return store.Actor{AdminID: cur.Admin.ID, Role: cur.Admin.Role, IPHash: h.ipHash(r)}
}

var sortKeys = []string{"created", "name", "city", "total", "status"}

// parseFilters validates the list query. Unknown or invalid values are dropped.
func parseFilters(q url.Values) view.OrderFilters {
	f := view.OrderFilters{Page: 1, Size: 20}
	if s, ok := domain.CleanText(q.Get("q"), false); ok && utf8.RuneCountInString(s) <= 100 {
		f.Q = s
	}
	if s := q.Get("status"); slices.Contains(view.OrderStatuses, s) {
		f.Status = s
	}
	switch q.Get("paid") {
	case "paid", "unpaid":
		f.Paid = q.Get("paid")
	case "unseen":
		f.Unseen = true
	}
	if id, err := uuid.Parse(q.Get("offer")); err == nil {
		f.Offer = id.String()
	}
	if s, ok := domain.CleanText(q.Get("city"), false); ok && utf8.RuneCountInString(s) <= 80 {
		f.City = s
	}
	for _, d := range []struct {
		key string
		dst *string
	}{{"from", &f.From}, {"to", &f.To}} {
		if _, err := time.Parse(time.DateOnly, q.Get(d.key)); err == nil {
			*d.dst = q.Get(d.key)
		}
	}
	if s := q.Get("sort"); slices.Contains(sortKeys, s) {
		f.Sort = s
		f.Dir = "desc"
		if q.Get("dir") == "asc" {
			f.Dir = "asc"
		}
	}
	if n, err := strconv.Atoi(q.Get("page")); err == nil && n >= 1 && n <= 10000 {
		f.Page = n
	}
	if n, err := strconv.Atoi(q.Get("size")); err == nil && slices.Contains(view.PageSizes, n) {
		f.Size = n
	}
	return f
}

// likeSafe removes LIKE wildcards from user input.
func likeSafe(s string) string { return strings.NewReplacer("%", "", "_", "", "\\", "").Replace(s) }

// orderQuery is the shared filter part of SearchOrders and ExportOrders.
type orderQuery struct {
	Q, Phone, City *string
	Status         *dbq.OrderStatus
	Paid           *bool
	Unseen         bool
	Offer          uuid.NullUUID
	From, To       *time.Time
}

func buildQuery(f view.OrderFilters) orderQuery {
	var oq orderQuery
	if q := likeSafe(f.Q); q != "" {
		oq.Q = &q
		if e164, ok := domain.NormalizeMoroccanPhone(q); ok {
			oq.Phone = &e164
		} else if digits := strings.TrimLeft(onlyDigits(q), "0"); len(digits) >= 4 {
			oq.Phone = &digits
		}
	}
	if f.Status != "" {
		s := dbq.OrderStatus(f.Status)
		oq.Status = &s
	}
	if f.Paid != "" {
		b := f.Paid == "paid"
		oq.Paid = &b
	}
	oq.Unseen = f.Unseen
	if id, err := uuid.Parse(f.Offer); err == nil {
		oq.Offer = uuid.NullUUID{UUID: id, Valid: true}
	}
	if c := likeSafe(f.City); c != "" {
		oq.City = &c
	}
	if d, err := time.ParseInLocation(time.DateOnly, f.From, casablanca); err == nil {
		oq.From = &d
	}
	if d, err := time.ParseInLocation(time.DateOnly, f.To, casablanca); err == nil {
		next := d.AddDate(0, 0, 1)
		oq.To = &next
	}
	return oq
}

func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func sortKey(f view.OrderFilters) string {
	if f.Sort == "" || (f.Sort == "created" && f.Dir == "desc") {
		return "created_desc"
	}
	return f.Sort + "_" + f.Dir
}

// Orders renders the list.
func (h *Handlers) Orders(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	f := parseFilters(r.URL.Query())
	oq := buildQuery(f)
	rows, err := h.Store.Q.SearchOrders(ctx, dbq.SearchOrdersParams{
		Q: oq.Q, Phone: oq.Phone, Status: oq.Status, Paid: oq.Paid, UnseenOnly: oq.Unseen,
		City: oq.City, OfferID: oq.Offer, FromDate: oq.From, ToDate: oq.To,
		// Size is one of PageSizes and Page is at most 10000 (parseFilters).
		SortKey: sortKey(f), PageSize: int32(f.Size), PageOffset: int32((f.Page - 1) * f.Size), //nolint:gosec // G115: bounded above
	})
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	p := h.page(r, cur, i18n.AdminOrdersTitle, "orders")
	d := view.OrdersData{Filters: f, Page: f.Page, PageSize: f.Size, Unseen: p.Unseen}
	for _, o := range rows {
		d.Total = o.TotalCount
		d.Rows = append(d.Rows, view.OrderRow{
			ID: o.ID.String(), Reference: o.Reference, Customer: o.CustomerName, Phone: o.PhoneE164,
			City: o.City, Status: string(o.Status), Total: o.Total, Items: o.ItemCount,
			Seen: o.SeenAt != nil, Paid: o.PaidAt != nil, CreatedAt: o.CreatedAt,
		})
	}
	if len(rows) == 0 && f.Page > 1 {
		// Past the last page (after deletes): count without paging.
		if c, err := h.Store.Q.SearchOrders(ctx, dbq.SearchOrdersParams{
			Q: oq.Q, Phone: oq.Phone, Status: oq.Status, Paid: oq.Paid, UnseenOnly: oq.Unseen,
			City: oq.City, OfferID: oq.Offer, FromDate: oq.From, ToDate: oq.To, SortKey: "created_desc", PageSize: 1,
		}); err == nil && len(c) > 0 {
			d.Total = c[0].TotalCount
		}
	}
	offers, err := h.Store.Q.ListOffersForFilter(ctx)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	for _, o := range offers {
		label := o.NameFr
		if p.Locale == i18n.AR {
			label = o.NameAr
		}
		d.Offers = append(d.Offers, view.Option{Value: o.ID.String(), Label: label})
	}
	q := r.URL.Query()
	switch q.Get("done") {
	case "deleted":
		for _, s := range strings.Split(q.Get("undo"), ",") {
			if id, err := uuid.Parse(s); err == nil && len(d.Undo) < 50 {
				d.Undo = append(d.Undo, id.String())
			}
		}
		d.UndoReturn = d.Filters.Values().Encode()
		if d.UndoReturn != "" {
			d.UndoReturn = "/admin/orders?" + d.UndoReturn
		} else {
			d.UndoReturn = "/admin/orders"
		}
		if !p.Owner || len(d.Undo) == 0 {
			p.Flash = p.T(i18n.AdminOrdersDeleted, "count", q.Get("n"))
		}
	case "empty":
		p.Flash, p.FlashError = p.T(i18n.AdminOrdersEmpty), true
	}
	render(w, r, http.StatusOK, p.Nonce, view.AdminOrders(p, d))
}

// parseIDs reads "ids" (repeated or comma-separated), at most 200.
func parseIDs(r *http.Request) []uuid.UUID {
	var out []uuid.UUID
	seen := map[uuid.UUID]bool{}
	for _, v := range r.PostForm["ids"] {
		for _, s := range strings.Split(v, ",") {
			if id, err := uuid.Parse(strings.TrimSpace(s)); err == nil && !seen[id] && len(out) < 200 {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// backTo builds the redirect after an action on the list: the list the admin
// was on (validated), with the result to show.
func backTo(ret string, flash url.Values) string {
	ret = safeNext(ret)
	if ret == "" || !strings.HasPrefix(ret, "/admin/orders") {
		ret = "/admin/orders"
	}
	u, _ := url.Parse(ret)
	q := u.Query()
	for _, k := range []string{"done", "n", "undo"} {
		q.Del(k)
	}
	for k, v := range flash {
		q[k] = v
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// redirectBack sends the admin back to the list they were on (see backTo).
func redirectBack(w http.ResponseWriter, r *http.Request, ret string, flash url.Values) {
	http.Redirect(w, r, backTo(ret, flash), http.StatusSeeOther) //nolint:gosec // G710: backTo keeps only /admin/orders paths (safeNext)
}

// OrdersBulk applies an action to the checked orders.
func (h *Handlers) OrdersBulk(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	ids := parseIDs(r)
	ret := r.PostFormValue("return")
	if len(ids) == 0 {
		redirectBack(w, r, ret, url.Values{"done": {"empty"}})
		return
	}
	a := actor(r, h, cur)
	var (
		n    int
		err  error
		done string
	)
	switch r.PostFormValue("action") {
	case "status":
		s := r.PostFormValue("status")
		if !slices.Contains(view.OrderStatuses, s) {
			http.Error(w, "bad status", http.StatusBadRequest)
			return
		}
		n, err = h.Store.SetOrdersStatus(ctx, a, ids, dbq.OrderStatus(s), "")
		done = "applied"
	case "paid", "unpaid":
		paid := r.PostFormValue("action") == "paid"
		n, err = h.Store.SetOrdersPaid(ctx, a, ids, paid)
		done = r.PostFormValue("action")
	case "delete":
		if !cur.IsOwner() {
			h.message(w, r, cur, http.StatusForbidden, i18n.AdminCommonForbidden, "")
			return
		}
		h.deleteOrders(w, r, cur, ids, ret)
		return
	default:
		http.Error(w, "bad action", http.StatusBadRequest)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	redirectBack(w, r, ret, url.Values{"done": {done}, "n": {strconv.Itoa(n)}})
}

// deleteOrders asks first (confirm=1 comes from the dialog or the confirmation page).
func (h *Handlers) deleteOrders(w http.ResponseWriter, r *http.Request, cur *auth.Current, ids []uuid.UUID, ret string) {
	if r.PostFormValue("confirm") != "1" {
		p := h.page(r, cur, i18n.AdminOrdersDelete, "orders")
		strs := make([]string, len(ids))
		for i, id := range ids {
			strs[i] = id.String()
		}
		render(w, r, http.StatusOK, p.Nonce, view.AdminConfirmDelete(p, strs, backTo(ret, nil)))
		return
	}
	n, err := h.Store.SoftDeleteOrders(r.Context(), actor(r, h, cur), ids)
	if errors.Is(err, store.ErrForbidden) {
		h.message(w, r, cur, http.StatusForbidden, i18n.AdminCommonForbidden, "")
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	undo := make([]string, len(ids))
	for i, id := range ids {
		undo[i] = id.String()
	}
	redirectBack(w, r, ret, url.Values{"done": {"deleted"}, "n": {strconv.Itoa(n)}, "undo": {strings.Join(undo, ",")}})
}

// OrdersRestore undoes a delete (owner).
func (h *Handlers) OrdersRestore(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	n, err := h.Store.RestoreOrders(r.Context(), actor(r, h, cur), parseIDs(r))
	if errors.Is(err, store.ErrForbidden) {
		h.message(w, r, cur, http.StatusForbidden, i18n.AdminCommonForbidden, "")
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	redirectBack(w, r, r.PostFormValue("return"), url.Values{"done": {"restored"}, "n": {strconv.Itoa(n)}})
}

// MarkAllSeen turns every red "new" order off (fetch: 204, form: back to the list).
func (h *Handlers) MarkAllSeen(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	n, err := h.Store.Q.MarkAllOrdersSeen(ctx)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if n > 0 {
		h.audit(ctx, r, &cur.Admin.ID, "order.seen_all", "order", "", map[string]any{"count": n})
	}
	if r.Header.Get("X-Requested-With") == "fetch" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	redirectBack(w, r, r.PostFormValue("return"), nil)
}

var csvHeaders = []string{
	"reference", "created_at", "status", "customer_name", "phone", "city", "address", "note",
	"items", "subtotal", "delivery_fee", "total", "language", "paid_at",
	"utm_source", "utm_medium", "utm_campaign", "utm_content", "utm_term", "fbclid",
}

// OrdersCSV exports the filtered orders (UTF-8 BOM for Excel, formula-safe, audited).
func (h *Handlers) OrdersCSV(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	f := parseFilters(r.URL.Query())
	oq := buildQuery(f)
	rows, err := h.Store.Q.ExportOrders(ctx, dbq.ExportOrdersParams{
		Q: oq.Q, Phone: oq.Phone, Status: oq.Status, Paid: oq.Paid, UnseenOnly: oq.Unseen,
		City: oq.City, OfferID: oq.Offer, FromDate: oq.From, ToDate: oq.To,
	})
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	ids := make([]uuid.UUID, len(rows))
	for i, o := range rows {
		ids[i] = o.ID
	}
	items, err := h.Store.Q.ListItemsForOrders(ctx, ids)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	byOrder := map[uuid.UUID][]string{}
	for _, it := range items {
		name := it.NameFr
		if i18n.Locale(cur.Admin.UiLocale) == i18n.AR {
			name = it.NameAr
		}
		byOrder[it.OrderID] = append(byOrder[it.OrderID], strconv.Itoa(int(it.Quantity))+" x "+name)
	}
	var buf bytes.Buffer
	buf.WriteString("\uFEFF") // BOM so Excel opens Arabic text as UTF-8
	cw := csv.NewWriter(&buf)
	cw.UseCRLF = true
	_ = cw.Write(csvHeaders)
	for _, o := range rows {
		var attr map[string]string
		_ = json.Unmarshal(o.Attribution, &attr)
		fee := ""
		if o.DeliveryFee.Valid {
			fee = o.DeliveryFee.Decimal.StringFixed(2)
		}
		paid := ""
		if o.PaidAt != nil {
			paid = o.PaidAt.UTC().Format(time.RFC3339)
		}
		rec := []string{
			o.Reference, o.CreatedAt.UTC().Format(time.RFC3339), string(o.Status), o.CustomerName, o.PhoneE164,
			o.City, deref(o.Address), deref(o.Note), strings.Join(byOrder[o.ID], " | "),
			o.Subtotal.StringFixed(2), fee, o.Total.StringFixed(2), string(o.Locale), paid,
			attr["utm_source"], attr["utm_medium"], attr["utm_campaign"], attr["utm_content"], attr["utm_term"], attr["fbclid"],
		}
		for i := range rec {
			rec[i] = domain.CSVCell(rec[i])
		}
		_ = cw.Write(rec)
	}
	cw.Flush()
	h.audit(ctx, r, &cur.Admin.ID, "order.export", "order", "", map[string]any{"rows": len(rows), "filters": filterKeys(f)})
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="orders-`+h.Now().In(casablanca).Format("2006-01-02")+`.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(buf.Bytes())
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// filterKeys names the filters used (values could hold a phone or a name).
func filterKeys(f view.OrderFilters) []string {
	keys := []string{}
	for k := range f.Values() {
		if k != "page" && k != "size" && k != "sort" && k != "dir" {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
}
