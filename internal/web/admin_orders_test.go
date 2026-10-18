package web_test

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/domain"
	"github.com/omarchaouki/zaytona-go/internal/store"
)

// placeOrder creates an order like the public form does (offer 30 L, qty).
func (e *adminEnv) placeOrder(name, phone, city string, qty int) dbq.Order {
	e.t.Helper()
	ctx := context.Background()
	offer, err := e.s.Q.GetOfferBySlug(ctx, "huile-olive-30l")
	if err != nil {
		e.t.Fatal(err)
	}
	lines := []domain.OrderLine{{
		OfferID: offer.ID.String(), NameAr: offer.NameAr, NameFr: offer.NameFr,
		Liters: offer.Liters, UnitPrice: offer.PriceMad, Quantity: qty,
	}}
	res, err := e.s.PlaceOrder(ctx, store.NewOrder{
		Order:            domain.ValidOrder{Choice: offer.Slug, Name: name, PhoneE164: phone, City: city, Note: "=HYPERLINK(\"x\")"},
		Lines:            lines,
		Totals:           domain.ComputeTotals(lines, domain.DeliverySettings{Mode: domain.DeliveryConfirm}),
		Locale:           dbq.LocaleFr,
		Attribution:      map[string]string{"utm_source": "facebook", "utm_campaign": "spring"},
		ConfirmDuplicate: true,
	})
	if err != nil || res.Created == nil {
		e.t.Fatalf("place order: %v %+v", err, res)
	}
	return *res.Created
}

func (e *adminEnv) order(id fmt.Stringer) dbq.Order {
	e.t.Helper()
	var o dbq.Order
	row := e.s.Pool.QueryRow(context.Background(), "select status, paid_at is not null, seen_at is not null, deleted_at is not null, total::text from orders where id = $1", id.String())
	var paid, seen, deleted bool
	var total string
	if err := row.Scan(&o.Status, &paid, &seen, &deleted, &total); err != nil {
		e.t.Fatal(err)
	}
	o.Total = decimal.RequireFromString(total)
	if paid {
		o.PaidBy.Valid = true
	}
	if seen {
		o.SeenAt = &o.CreatedAt
	}
	if deleted {
		o.DeletedAt = &o.CreatedAt
	}
	return o
}

func TestAdminDashboardAndList(t *testing.T) {
	e := newAdminEnv(t)
	a := e.placeOrder("Fatima Zahra", "+212612000001", "Rabat", 1)
	b := e.placeOrder("Youssef Alami", "+212612000002", "Casablanca", 2)
	e.placeOrder("Karim Bennani", "+212612000003", "Marrakech", 1)

	br := e.browser("198.51.100.20:1000")
	br.signIn(ownerEmail, ownerPassword)
	home := br.get("/admin").Body.String()
	for _, want := range []string{"Fatima Zahra", a.Reference, `<svg viewBox="0 0 300 100"`, "sur 30 jours", "vérification en deux étapes"} {
		if !strings.Contains(home, want) {
			t.Errorf("dashboard misses %q", want)
		}
	}
	if strings.Contains(home, "style=") {
		t.Error("dashboard has a style attribute")
	}

	// Search by name, by phone in local format, by reference; filters; sorting.
	cases := map[string][]string{
		"/admin/orders?q=youssef":                         {b.Reference},
		"/admin/orders?q=0612000003":                      {"Karim Bennani"},
		"/admin/orders?q=" + url.QueryEscape(a.Reference): {"Fatima Zahra"},
		"/admin/orders?city=casa":                         {"Youssef Alami"},
		"/admin/orders?paid=unseen":                       {"Fatima Zahra", "Youssef Alami", "Karim Bennani"},
	}
	for path, wants := range cases {
		body := br.get(path).Body.String()
		for _, w := range wants {
			if !strings.Contains(body, w) {
				t.Errorf("%s misses %q", path, w)
			}
		}
	}
	if body := br.get("/admin/orders?q=youssef").Body.String(); strings.Contains(body, "Fatima Zahra") {
		t.Error("search does not filter")
	}
	if body := br.get("/admin/orders?q=%25").Body.String(); !strings.Contains(body, "Fatima") {
		t.Error("a lone LIKE wildcard should be ignored, not match nothing or error")
	}
	sorted := br.get("/admin/orders?sort=total&dir=desc").Body.String()
	if strings.Index(sorted, b.Reference) > strings.Index(sorted, a.Reference) {
		t.Error("sort by total desc")
	}
	if rec := br.get("/admin/orders?sort=drop%20table&status=evil&page=-3&size=1000"); rec.Code != 200 {
		t.Fatalf("bad params: %d", rec.Code)
	}
	page := br.get("/admin/orders?size=10&page=1").Body.String()
	if !strings.Contains(page, `value="10" selected`) {
		t.Error("page size not kept")
	}

	// GET never changes anything: viewing the list and an order keeps them unseen.
	br.get("/admin/orders/" + a.ID.String())
	if e.order(a.ID).SeenAt != nil {
		t.Fatal("GET marked the order as seen")
	}
	if rec := br.post("/admin/orders/"+a.ID.String()+"/seen", nil); rec.Code != http.StatusSeeOther || e.order(a.ID).SeenAt == nil {
		t.Fatalf("seen: %d", rec.Code)
	}
	// Notifications fragment: count and the newest order not closed here.
	frag := br.get("/admin/notifications?skip=" + b.ID.String()).Body.String()
	if !strings.Contains(frag, `data-count="2"`) || strings.Contains(frag, "Youssef") || !strings.Contains(frag, "Karim") {
		t.Errorf("notifications fragment: %s", frag)
	}
	if rec := br.post("/admin/orders/seen-all", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("seen all: %d", rec.Code)
	}
	if frag := br.get("/admin/notifications").Body.String(); !strings.Contains(frag, `data-count="0"`) {
		t.Errorf("after seen-all: %s", frag)
	}
}

func TestAdminOrderActions(t *testing.T) {
	e := newAdminEnv(t)
	o := e.placeOrder("Fatima Zahra", "+212612000001", "Rabat", 2)
	br := e.browser("198.51.100.21:1000")
	br.signIn(staffEmail, staffPassword)
	path := "/admin/orders/" + o.ID.String()
	detail := br.get(path).Body.String()
	for _, want := range []string{"Fatima Zahra", "facebook · spring", "https://wa.me/212612000001?text=", "print-only"} {
		if !strings.Contains(detail, want) {
			t.Errorf("order page misses %q", want)
		}
	}

	// Status with a note, then the timeline shows both badges and the note.
	if rec := br.post(path+"/status", url.Values{"status": {"confirmed"}, "note": {"Appelé, livraison jeudi"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("status: %d", rec.Code)
	}
	if e.order(o.ID).Status != dbq.OrderStatusConfirmed {
		t.Fatal("status not saved")
	}
	if body := br.get(path).Body.String(); !strings.Contains(body, "Appelé, livraison jeudi") || !strings.Contains(body, staffEmail) {
		t.Error("timeline misses the change")
	}
	if rec := br.post(path+"/status", url.Values{"status": {"lost"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown status: %d", rec.Code)
	}

	// Paid toggle.
	br.post(path+"/paid", url.Values{"paid": {"1"}})
	if !e.order(o.ID).PaidBy.Valid {
		t.Fatal("not paid")
	}
	br.post(path+"/paid", url.Values{"paid": {"0"}})
	if e.order(o.ID).PaidBy.Valid {
		t.Fatal("still paid")
	}

	// Edit: quantity 2 -> 1 and a 30 MAD delivery fee; totals recomputed from the stored price.
	items, _ := e.s.Q.ListOrderItems(context.Background(), o.ID)
	unit := items[0].UnitPrice
	if rec := br.post(path+"/edit", url.Values{"q_" + items[0].ID.String(): {"1"}, "fee": {"30"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("edit: %d %s", rec.Code, rec.Body.String())
	}
	if got := e.order(o.ID).Total; !got.Equal(unit.Add(decimal.NewFromInt(30))) {
		t.Fatalf("total %s, want %s + 30", got, unit)
	}
	if rec := br.post(path+"/edit", url.Values{"q_" + items[0].ID.String(): {"0"}}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("removing every item: %d", rec.Code)
	}
	if rec := br.post(path+"/edit", url.Values{"fee": {"-5"}}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("negative fee: %d", rec.Code)
	}

	// Internal note.
	if rec := br.post(path+"/note", url.Values{"note": {"Client fidèle"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("note: %d", rec.Code)
	}
	if body := br.get(path).Body.String(); !strings.Contains(body, "Client fidèle") {
		t.Error("note not shown")
	}

	// Staff cannot delete or restore (handler), nor through the bulk form.
	if rec := br.post(path+"/delete", url.Values{"confirm": {"1"}}); rec.Code != http.StatusForbidden {
		t.Fatalf("staff delete: %d", rec.Code)
	}
	if rec := br.post("/admin/orders/bulk", url.Values{"action": {"delete"}, "confirm": {"1"}, "ids": {o.ID.String()}}); rec.Code != http.StatusForbidden {
		t.Fatalf("staff bulk delete: %d", rec.Code)
	}
	if rec := br.post("/admin/orders/restore", url.Values{"ids": {o.ID.String()}}); rec.Code != http.StatusForbidden {
		t.Fatalf("staff restore: %d", rec.Code)
	}
	if e.order(o.ID).DeletedAt != nil {
		t.Fatal("staff deleted an order")
	}
	// The store re-checks the role too.
	if _, err := e.s.SoftDeleteOrders(context.Background(), store.Actor{AdminID: e.staff.ID, Role: dbq.AdminRoleStaff}, nil); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("store delete as staff: %v", err)
	}
	if strings.Contains(br.get(path).Body.String(), "/delete") {
		t.Error("delete button shown to staff")
	}
	for _, action := range []string{"order.status", "order.paid", "order.unpaid", "order.edit", "order.note"} {
		if e.auditCount(action) == 0 {
			t.Errorf("%s not audited", action)
		}
	}
}

func TestAdminBulkDeleteUndoAndCSV(t *testing.T) {
	e := newAdminEnv(t)
	a := e.placeOrder("Fatima Zahra", "+212612000001", "Rabat", 1)
	b := e.placeOrder("Youssef Alami", "+212612000002", "Casablanca", 1)
	br := e.browser("198.51.100.22:1000")
	br.signIn(ownerEmail, ownerPassword)
	ids := url.Values{"ids": {a.ID.String(), b.ID.String()}, "return": {"/admin/orders?status=new"}}

	// Bulk status and paid.
	form := url.Values{"action": {"status"}, "status": {"shipped"}}
	for k, v := range ids {
		form[k] = v
	}
	rec := br.post("/admin/orders/bulk", form)
	if rec.Code != http.StatusSeeOther || !strings.Contains(location(rec), "done=applied") || !strings.Contains(location(rec), "n=2") || !strings.Contains(location(rec), "status=new") {
		t.Fatalf("bulk status: %d %s", rec.Code, location(rec))
	}
	if e.order(a.ID).Status != dbq.OrderStatusShipped {
		t.Fatal("bulk status not applied")
	}

	// Delete without confirmation shows the confirmation page (no JS).
	del := url.Values{"action": {"delete"}, "confirm": {"0"}}
	for k, v := range ids {
		del[k] = v
	}
	rec = br.post("/admin/orders/bulk", del)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `name="confirm" value="1"`) || e.order(a.ID).DeletedAt != nil {
		t.Fatalf("confirmation page: %d", rec.Code)
	}
	del.Set("confirm", "1")
	rec = br.post("/admin/orders/bulk", del)
	if rec.Code != http.StatusSeeOther || !strings.Contains(location(rec), "done=deleted") {
		t.Fatalf("delete: %d %s", rec.Code, location(rec))
	}
	if e.order(a.ID).DeletedAt == nil || e.order(b.ID).DeletedAt == nil {
		t.Fatal("not deleted")
	}
	list := br.get(location(rec)).Body.String()
	if strings.Contains(list, "Fatima Zahra") || !strings.Contains(list, `action="/admin/orders/restore"`) {
		t.Fatal("deleted orders still listed or no undo")
	}
	if rec := br.get("/admin/orders/" + a.ID.String()); rec.Code != http.StatusNotFound {
		t.Fatalf("deleted order page: %d", rec.Code)
	}
	if rec := br.post("/admin/orders/restore", url.Values{"ids": {a.ID.String() + "," + b.ID.String()}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("restore: %d", rec.Code)
	}
	if e.order(a.ID).DeletedAt != nil {
		t.Fatal("not restored")
	}
	if e.auditCount("order.delete") != 2 || e.auditCount("order.restore") != 2 {
		t.Fatal("delete/restore not audited")
	}

	// CSV: BOM, header, formula-safe cells, attribution, audited.
	rec = br.get("/admin/orders.csv?q=fatima")
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("csv: %d %v", rec.Code, rec.Header())
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "\xEF\xBB\xBF") {
		t.Fatal("no BOM")
	}
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, "\xEF\xBB\xBF")))
	rows, err := r.ReadAll()
	if err != nil || len(rows) != 2 {
		t.Fatalf("csv rows: %v %d", err, len(rows))
	}
	row := map[string]string{}
	for i, h := range rows[0] {
		row[h] = rows[1][i]
	}
	if row["customer_name"] != "Fatima Zahra" || row["phone"] != "'+212612000001" || !strings.HasPrefix(row["note"], "'=") || row["utm_source"] != "facebook" {
		t.Fatalf("csv row: %v", row)
	}
	if e.auditCount("order.export") != 1 {
		t.Fatal("export not audited")
	}
}
