package view

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omarchaouki/zaytona-go/internal/domain"
	"github.com/omarchaouki/zaytona-go/internal/i18n"
)

// OrderStatuses lists the statuses in workflow order.
var OrderStatuses = []string{"new", "confirmed", "shipped", "delivered", "cancelled", "returned"}

var statusKeys = map[string]i18n.Key{
	"new":       i18n.AdminStatusNew,
	"confirmed": i18n.AdminStatusConfirmed,
	"shipped":   i18n.AdminStatusShipped,
	"delivered": i18n.AdminStatusDelivered,
	"cancelled": i18n.AdminStatusCancelled,
	"returned":  i18n.AdminStatusReturned,
}

var statusStyle = map[string]string{
	"new":       "bg-oil text-olive-950",
	"confirmed": "bg-olive-600 text-limewash",
	"shipped":   "bg-sky-700 text-white",
	"delivered": "bg-olive-950 text-olive-200",
	"cancelled": "bg-sand-deep text-ink-soft line-through decoration-1",
	"returned":  "bg-danger/90 text-white",
}

func (p AdminPage) statusLabel(s string) string { return p.T(statusKeys[s]) }

func (p AdminPage) mad(v decimal.Decimal) string { return domain.FormatMAD(v, string(p.Locale)) }

func (p AdminPage) num(n int64) string { return domain.FormatNumber(decimal.NewFromInt(n)) }

var casablanca = func() *time.Location {
	loc, err := time.LoadLocation("Africa/Casablanca")
	if err != nil {
		return time.UTC
	}
	return loc
}()

// shortMonths in Western digits for both languages (CLDR abbreviations).
var shortMonths = map[i18n.Locale][12]string{
	i18n.AR: {"يناير", "فبراير", "مارس", "أبريل", "ماي", "يونيو", "يوليوز", "غشت", "شتنبر", "أكتوبر", "نونبر", "دجنبر"},
	i18n.FR: {"janv.", "févr.", "mars", "avr.", "mai", "juin", "juil.", "août", "sept.", "oct.", "nov.", "déc."},
}

// dateTime is "16 oct. 14:05" in Morocco time.
func (p AdminPage) dateTime(t time.Time) string {
	t = t.In(casablanca)
	return strconv.Itoa(t.Day()) + " " + shortMonths[p.Locale][t.Month()-1] + " " + t.Format("15:04")
}

// dateLong is "16 oct. 2026, 14:05" in Morocco time.
func (p AdminPage) dateLong(t time.Time) string {
	t = t.In(casablanca)
	return strconv.Itoa(t.Day()) + " " + shortMonths[p.Locale][t.Month()-1] + " " + strconv.Itoa(t.Year()) + ", " + t.Format("15:04")
}

// dateShort is "16/10 14:05" (orders list).
func dateShort(t time.Time) string { return t.In(casablanca).Format("02/01 15:04") }

// DashboardData fills the dashboard.
type DashboardData struct {
	Today, Pending, Delivered, Total int64
	ToDeliver                        int64
	ToCollect                        decimal.Decimal
	PaidCount, PaidMonthCount        int64
	Paid, PaidMonth                  decimal.Decimal
	UnpaidDeliveredCount             int64
	UnpaidDelivered                  decimal.Decimal
	WhatsAppClicks30                 int64
	Days                             []ChartDay
	Latest                           []OrderRow
	Warnings                         []string
}

// ChartDay is one bar of the 30-day chart.
type ChartDay struct {
	Day   time.Time
	Count int64
}

// OrderRow is one order in a list.
type OrderRow struct {
	ID, Reference, Customer, Phone, City, Status string
	Total                                        decimal.Decimal
	Items                                        int32
	Seen, Paid                                   bool
	CreatedAt                                    time.Time
}

// chartBar is a bar in SVG units (viewBox 300 x 100).
type chartBar struct {
	X, Y, W, H string
	Label      string
	Empty      bool
}

func (p AdminPage) chartBars(days []ChartDay) []chartBar {
	var maxN int64 = 1
	for _, d := range days {
		maxN = max(maxN, d.Count)
	}
	n := len(days)
	if n == 0 {
		return nil
	}
	slot := 300.0 / float64(n)
	gap := 1.5
	out := make([]chartBar, 0, n)
	for i, d := range days {
		h := 4.0
		if d.Count > 0 {
			h = max(4, float64(d.Count)/float64(maxN)*100)
		}
		f := func(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }
		out = append(out, chartBar{
			X: f(float64(i)*slot + gap/2), Y: f(100 - h), W: f(slot - gap), H: f(h),
			Label: p.T(i18n.AdminDashboardChartLabel, "count", strconv.FormatInt(d.Count, 10), "day", d.Day.Format("02/01")),
			Empty: d.Count == 0,
		})
	}
	return out
}

func chartTotal(days []ChartDay) int64 {
	var t int64
	for _, d := range days {
		t += d.Count
	}
	return t
}

// OrdersData fills the orders list.
type OrdersData struct {
	Filters    OrderFilters
	Rows       []OrderRow
	Total      int64
	Page       int
	PageSize   int
	Offers     []Option
	Unseen     int64
	Undo       []string // ids of the orders just deleted (Undo button)
	UndoReturn string
}

// Option is a <select> choice.
type Option struct{ Value, Label string }

// PageSizes are the "per page" choices.
var PageSizes = []int{10, 20, 30, 40, 50}

// OrderFilters are the list query parameters (validated by the handler).
type OrderFilters struct {
	Q, Status, Paid, Offer, City, From, To, Sort, Dir string
	Unseen                                            bool
	Page, Size                                        int
}

// Values encodes the filters (page and size included when set).
func (f OrderFilters) Values() url.Values {
	v := url.Values{}
	set := func(k, s string) {
		if s != "" {
			v.Set(k, s)
		}
	}
	set("q", f.Q)
	set("status", f.Status)
	if f.Unseen {
		v.Set("paid", "unseen")
	} else {
		set("paid", f.Paid)
	}
	set("offer", f.Offer)
	set("city", f.City)
	set("from", f.From)
	set("to", f.To)
	set("sort", f.Sort)
	set("dir", f.Dir)
	if f.Page > 1 {
		v.Set("page", strconv.Itoa(f.Page))
	}
	if f.Size != 0 && f.Size != 20 {
		v.Set("size", strconv.Itoa(f.Size))
	}
	return v
}

// ActiveCount is the number of filters set (badge on the filters toggle).
func (f OrderFilters) ActiveCount() int {
	n := 0
	for _, s := range []string{f.Status, f.Paid, f.Offer, f.City, f.From, f.To} {
		if s != "" {
			n++
		}
	}
	if f.Unseen {
		n++
	}
	return n
}

func (d OrdersData) href(edit func(*OrderFilters)) string {
	f := d.Filters
	edit(&f)
	q := f.Values().Encode()
	if q == "" {
		return "/admin/orders"
	}
	return "/admin/orders?" + q
}

func (d OrdersData) pageHref(page int) string {
	return d.href(func(f *OrderFilters) { f.Page = page })
}

// sortHref toggles the direction on the active column, newest/largest first otherwise.
func (d OrdersData) sortHref(key string) string {
	return d.href(func(f *OrderFilters) {
		dir := "desc"
		if d.sortKey() == key && d.sortDir() == "desc" {
			dir = "asc"
		}
		f.Sort, f.Dir, f.Page = key, dir, 0
	})
}

func (d OrdersData) sortKey() string {
	if d.Filters.Sort == "" {
		return "created"
	}
	return d.Filters.Sort
}

func (d OrdersData) sortDir() string {
	if d.Filters.Dir == "" {
		return "desc"
	}
	return d.Filters.Dir
}

func (d OrdersData) pages() int {
	if d.PageSize == 0 {
		return 1
	}
	return max(1, int((d.Total+int64(d.PageSize)-1)/int64(d.PageSize)))
}

func (d OrdersData) rangeText() string {
	if d.Total == 0 {
		return "0"
	}
	from := int64((d.Page-1)*d.PageSize + 1)
	to := min(int64(d.Page*d.PageSize), d.Total)
	return strconv.FormatInt(min(from, d.Total), 10) + "-" + strconv.FormatInt(to, 10)
}

func (d OrdersData) csvHref() string {
	f := d.Filters
	f.Page, f.Size, f.Sort, f.Dir = 0, 0, "", ""
	q := f.Values().Encode()
	if q == "" {
		return "/admin/orders.csv"
	}
	return "/admin/orders.csv?" + q
}

// returnPath is the list URL to come back to after an action.
func (d OrdersData) returnPath() string { return d.href(func(*OrderFilters) {}) }

func joinIDs(ids []string) string { return strings.Join(ids, ",") }

// OrderDetail fills the order page.
type OrderDetail struct {
	ID, Reference, Customer, Phone, City, Address, Note, InternalNote, Status string
	Locale, Source, SiteName                                                  string
	Subtotal, Total                                                           decimal.Decimal
	DeliveryFee                                                               *decimal.Decimal
	PaidAt                                                                    *time.Time
	Seen                                                                      bool
	CreatedAt                                                                 time.Time
	Items                                                                     []OrderItemRow
	Events                                                                    []OrderEventRow
	WhatsAppHref                                                              string
	Editing                                                                   bool
	EditError                                                                 string
}

// OrderItemRow is one line of an order.
type OrderItemRow struct {
	ID, Name  string
	UnitPrice decimal.Decimal
	Quantity  int32
}

func (i OrderItemRow) lineTotal() decimal.Decimal {
	return i.UnitPrice.Mul(decimal.NewFromInt32(i.Quantity))
}

// OrderEventRow is one timeline entry.
type OrderEventRow struct {
	Kind, From, To, Note, By string
	At                       time.Time
}

func (p AdminPage) eventText(e OrderEventRow) string {
	switch e.Kind {
	case "created":
		return p.T(i18n.AdminEventPlaced)
	case "edited":
		return p.T(i18n.AdminEventEdited)
	case "paid":
		return p.T(i18n.AdminEventPaid)
	case "unpaid":
		return p.T(i18n.AdminEventUnpaid)
	case "note":
		return p.T(i18n.AdminEventNote)
	case "deleted":
		return p.T(i18n.AdminEventDeleted)
	case "restored":
		return p.T(i18n.AdminEventRestored)
	}
	return e.Note // status change: the admin's note, if any
}

func (p AdminPage) localeName(l string) string {
	if l == "ar" {
		return i18n.AR.Meta().Label
	}
	return i18n.FR.Meta().Label
}

func feeValue(f *decimal.Decimal) string {
	if f == nil {
		return ""
	}
	return f.StringFixed(2)
}

func qtyString(n int32) string { return strconv.FormatInt(int64(n), 10) }
