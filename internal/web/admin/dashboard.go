package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/omarchaouki/zaytona-go/internal/auth"
	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/i18n"
	"github.com/omarchaouki/zaytona-go/internal/view"
)

// Dashboard shows money, work to do, setup reminders, 30 days and the latest orders.
func (h *Handlers) Dashboard(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	now := h.Now().In(casablanca)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, casablanca)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, casablanca)
	since := today.AddDate(0, 0, -29)

	c, err := h.Store.Q.DashboardCounters(ctx, dbq.DashboardCountersParams{TodayStart: today, MonthStart: month})
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	perDay, err := h.Store.Q.OrdersPerDay(ctx, since)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	latest, err := h.Store.Q.LatestOrders(ctx, 6)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	clicks, err := h.Store.Q.CountWhatsAppClicksSince(ctx, since)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	site, err := h.Site.Get(ctx)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	demoOffers, err := h.Store.Q.CountActiveDemoOffers(ctx)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	demoReviews, err := h.Store.Q.CountDemoReviews(ctx)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	p := h.page(r, cur, i18n.AdminDashboardTitle, "dashboard")
	d := view.DashboardData{
		Today: c.Today, Pending: c.NewOrders, Delivered: c.Delivered, Total: c.TotalOrders,
		ToDeliver: c.ToDeliver, ToCollect: c.ToCollect,
		PaidCount: c.PaidCount, Paid: c.CollectedAll, PaidMonthCount: c.PaidMonthCount, PaidMonth: c.CollectedMonth,
		UnpaidDeliveredCount: c.DeliveredUnpaidCount, UnpaidDelivered: c.DeliveredUnpaid,
		WhatsAppClicks30: clicks,
	}
	counts := map[string]int64{}
	for _, row := range perDay {
		if row.Day.Valid {
			counts[row.Day.Time.Format(time.DateOnly)] = row.Orders
		}
	}
	for i := range 30 {
		day := since.AddDate(0, 0, i)
		d.Days = append(d.Days, view.ChartDay{Day: day, Count: counts[day.Format(time.DateOnly)]})
	}
	for _, o := range latest {
		d.Latest = append(d.Latest, view.OrderRow{
			ID: o.ID.String(), Reference: o.Reference, Customer: o.CustomerName, City: o.City,
			Status: string(o.Status), Total: o.Total, Seen: o.SeenAt != nil, CreatedAt: o.CreatedAt,
		})
	}
	if cur.IsOwner() && !cur.TOTPEnabled() {
		d.Warnings = append(d.Warnings, p.T(i18n.AdminDashboardWarnNo2FA))
	}
	if site.Settings.WhatsApp.Number == "" {
		d.Warnings = append(d.Warnings, p.T(i18n.AdminDashboardWarnNoWhatsapp))
	}
	if site.Settings.Pricing.IsDemo {
		d.Warnings = append(d.Warnings, p.T(i18n.AdminDashboardWarnDemoPrice))
	}
	if demoOffers > 0 {
		d.Warnings = append(d.Warnings, p.T(i18n.AdminDashboardWarnDemo, "count", strconv.FormatInt(demoOffers, 10)))
	}
	if demoReviews > 0 {
		d.Warnings = append(d.Warnings, p.T(i18n.AdminDashboardWarnDemoReviews, "count", strconv.FormatInt(demoReviews, 10)))
	}
	if site.PublicONSSA() == nil {
		d.Warnings = append(d.Warnings, p.T(i18n.AdminDashboardWarnNoOnssa))
	}
	render(w, r, http.StatusOK, p.Nonce, view.AdminDashboard(p, d))
}

// Notifications is the fragment polled every 10 s by admin.js. skip holds
// the ids closed in this browser session.
func (h *Handlers) Notifications(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	skip := []uuid.UUID{}
	for _, s := range strings.Split(r.URL.Query().Get("skip"), ",") {
		if id, err := uuid.Parse(s); err == nil && len(skip) < 50 {
			skip = append(skip, id)
		}
	}
	count, err := h.Store.Q.CountUnseenOrders(ctx)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	ids, err := h.Store.Q.ListUnseenOrderIDs(ctx)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	open, err := h.Store.Q.ListUnseenOrders(ctx, dbq.ListUnseenOrdersParams{Skip: skip, MaxRows: 50})
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	p := h.page(r, cur, i18n.AdminNotifyLabel, "")
	d := view.NotifyData{Count: count}
	for _, id := range ids {
		d.IDs = append(d.IDs, id.String())
	}
	if len(open) > 0 {
		o := open[0]
		d.Newest = &view.OrderRow{ID: o.ID.String(), Reference: o.Reference, Customer: o.CustomerName, City: o.City, Total: o.Total, CreatedAt: o.CreatedAt}
		for _, x := range open {
			d.Closable = append(d.Closable, x.ID.String())
		}
	}
	render(w, r, http.StatusOK, p.Nonce, view.AdminNotifications(p, d))
}
