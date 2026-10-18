package web_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	altcha "github.com/altcha-org/altcha-lib-go/v2"

	"github.com/omarchaouki/zaytona-go/internal/config"
	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/db/dbtest"
	"github.com/omarchaouki/zaytona-go/internal/notify"
	"github.com/omarchaouki/zaytona-go/internal/security"
	"github.com/omarchaouki/zaytona-go/internal/seed"
	"github.com/omarchaouki/zaytona-go/internal/store"
)

type fakeNotifier struct {
	mu     sync.Mutex
	orders []notify.Order
}

func (f *fakeNotifier) Enqueue(o notify.Order) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.orders = append(f.orders, o)
}

type orderEnv struct {
	t      *testing.T
	h      http.Handler
	s      *store.Store
	notes  *fakeNotifier
	tokens *security.FormTokens
	ip     string
}

func newOrderEnv(t *testing.T) *orderEnv {
	t.Helper()
	s := store.New(dbtest.NewDatabase(t, pgURL))
	if err := seed.Run(context.Background(), s, seed.Options{}); err != nil {
		t.Fatal(err)
	}
	n := &fakeNotifier{}
	h, _ := newServerWith(t, s, n)
	return &orderEnv{t: t, h: h, s: s, notes: n, tokens: security.NewFormTokens(testConfig(t).DeriveKey(config.KeyFormToken)), ip: "203.0.113.7:1234"}
}

// pow plays the browser: fetches a challenge and solves it.
func (e *orderEnv) pow() string {
	e.t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(e.t.Context(), http.MethodGet, "/api/pow", nil)
	req.RemoteAddr = e.ip
	e.h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
		e.t.Fatalf("pow: %d %v", rec.Code, rec.Header())
	}
	var ch altcha.Challenge
	if err := json.Unmarshal(rec.Body.Bytes(), &ch); err != nil {
		e.t.Fatal(err)
	}
	sol, err := altcha.SolveChallenge(altcha.SolveChallengeOptions{Challenge: ch, DeriveKey: altcha.DeriveKeySHA()})
	if err != nil || sol == nil {
		e.t.Fatal(err)
	}
	raw, _ := json.Marshal(altcha.Payload{Challenge: ch, Solution: *sol})
	return base64.StdEncoding.EncodeToString(raw)
}

func (e *orderEnv) form(overrides map[string]string) url.Values {
	v := url.Values{
		"offer":      {"huile-olive-30l"},
		"name":       {"Fatima Zahra"},
		"phone":      {"06 12 34 56 78"},
		"city":       {"Marrakech"},
		"address":    {"Derb 12"},
		"form_token": {e.tokens.IssueAt(time.Now().Add(-10 * time.Second))},
		"altcha":     {e.pow()},
	}
	for k, val := range overrides {
		if val == "<delete>" {
			v.Del(k)
		} else {
			v.Set(k, val)
		}
	}
	return v
}

func (e *orderEnv) post(locale string, v url.Values, json bool) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequestWithContext(e.t.Context(), http.MethodPost, "/"+locale+"/order", strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = e.ip
	if json {
		req.Header.Set("Accept", "application/json")
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

type outcome struct {
	Status    string            `json:"status"`
	Redirect  string            `json:"redirect"`
	Errors    map[string]string `json:"errors"`
	FormError string            `json:"formError"`
	Duplicate string            `json:"duplicate"`
	Step      int               `json:"step"`
}

func (e *orderEnv) postJSON(locale string, v url.Values) outcome {
	e.t.Helper()
	rec := e.post(locale, v, true)
	var o outcome
	if err := json.Unmarshal(rec.Body.Bytes(), &o); err != nil {
		e.t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	return o
}

func TestOrderFullPageFlowAndConfirmation(t *testing.T) {
	e := newOrderEnv(t)
	rec := e.post("fr", e.form(nil), false)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("plain post: %d %s", rec.Code, rec.Body.String()[:min(400, rec.Body.Len())])
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/fr/order/OL-") || !strings.Contains(loc, "?t=") {
		t.Fatalf("redirect %q", loc)
	}
	page := get(t, e.h, loc)
	body := page.Body.String()
	if page.Code != 200 || !strings.Contains(body, "Merci Fatima") || !strings.Contains(body, "2.700 MAD") {
		t.Fatalf("confirmation: %d", page.Code)
	}
	for k, v := range map[string]string{"Cache-Control": "no-store", "Referrer-Policy": "no-referrer", "X-Robots-Tag": "noindex, nofollow"} {
		if page.Header().Get(k) != v {
			t.Errorf("%s = %q", k, page.Header().Get(k))
		}
	}
	if !strings.Contains(body, `content="no-referrer"`) || !strings.Contains(body, `data-lead="OL-`) {
		t.Error("confirmation meta or lead data missing")
	}

	// Sequential references: the page needs the token.
	ref := strings.TrimPrefix(strings.Split(loc, "?")[0], "/fr/order/")
	for _, bad := range []string{"/fr/order/" + ref, "/fr/order/" + ref + "?t=AAAAAAAAAAAAAAAAAAAAAA", "/fr/order/OL-2026-099999?t=" + strings.Split(loc, "?t=")[1]} {
		if r := get(t, e.h, bad); r.Code != http.StatusNotFound || strings.Contains(r.Body.String(), "Fatima") {
			t.Errorf("%s: %d", bad, r.Code)
		}
	}

	// The order is stored with prices from the DB, and the notifier got it.
	o, err := e.s.Q.GetOrderByReference(t.Context(), ref)
	if err != nil || o.PhoneE164 != "+212612345678" || o.Total.String() != "2700" || o.Locale != dbq.LocaleFr {
		t.Fatalf("stored order: %+v %v", o, err)
	}
	if len(e.notes.orders) != 1 || e.notes.orders[0].Reference != ref {
		t.Fatalf("notifications: %+v", e.notes.orders)
	}
}

func TestOrderRejectsBots(t *testing.T) {
	e := newOrderEnv(t)
	for name, over := range map[string]map[string]string{
		"honeypot":       {"website": "http://spam"},
		"too fast":       {"form_token": e.tokens.IssueAt(time.Now())},
		"forged token":   {"form_token": "123.abc"},
		"no pow":         {"altcha": "<delete>"},
		"garbage pow":    {"altcha": "bm90IGpzb24="},
		"expired render": {"form_token": e.tokens.IssueAt(time.Now().Add(-25 * time.Hour))},
	} {
		if o := e.postJSON("fr", e.form(over)); o.Status != "rejected" {
			t.Errorf("%s: %+v", name, o)
		}
	}
	// A proof of work is accepted once.
	v := e.form(nil)
	if o := e.postJSON("fr", v); o.Status != "created" {
		t.Fatalf("first: %+v", o)
	}
	v.Set("phone", "0712345678")
	if o := e.postJSON("fr", v); o.Status != "rejected" {
		t.Fatalf("replayed proof of work accepted: %+v", o)
	}
	if n, _ := e.s.Q.CountUnseenOrders(t.Context()); n != 1 {
		t.Fatalf("%d orders stored, want 1", n)
	}
}

func TestOrderValidationAndReRender(t *testing.T) {
	e := newOrderEnv(t)
	o := e.postJSON("ar", e.form(map[string]string{"phone": "123", "city": "", "name": "A"}))
	if o.Status != "invalid" || o.Step != 1 || o.Errors["phone"] == "" || o.Errors["city"] == "" || o.Errors["name"] == "" {
		t.Fatalf("%+v", o)
	}
	o = e.postJSON("fr", e.form(map[string]string{"offer": "custom", "liters": "2"}))
	if o.Status != "invalid" || o.Step != 0 || !strings.Contains(o.Errors["customLiters"], "limites") {
		t.Fatalf("custom litres below min: %+v", o)
	}
	// Without JS: the landing page again, values kept, errors shown, 422.
	rec := e.post("fr", e.form(map[string]string{"phone": "123"}), false)
	body := rec.Body.String()
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(body, "Numéro invalide") ||
		!strings.Contains(body, `value="Fatima Zahra"`) || !strings.Contains(body, `aria-invalid`) {
		t.Fatalf("re-render: %d", rec.Code)
	}
}

func TestOrderCustomLitresPricedFromSettings(t *testing.T) {
	e := newOrderEnv(t)
	o := e.postJSON("ar", e.form(map[string]string{"offer": "custom", "liters": "45"}))
	if o.Status != "created" {
		t.Fatalf("%+v", o)
	}
	ref := strings.Split(strings.TrimPrefix(o.Redirect, "/ar/order/"), "?")[0]
	order, _ := e.s.Q.GetOrderByReference(t.Context(), ref)
	items, _ := e.s.Q.ListOrderItems(t.Context(), order.ID)
	if order.Total.String() != "4050" || len(items) != 1 || items[0].OfferID.Valid || items[0].Liters.String() != "45" {
		t.Fatalf("custom order: %+v %+v", order, items)
	}
}

func TestOrderDuplicateGuardAndUnavailable(t *testing.T) {
	e := newOrderEnv(t)
	if o := e.postJSON("fr", e.form(nil)); o.Status != "created" {
		t.Fatalf("first: %+v", o)
	}
	o := e.postJSON("fr", e.form(nil))
	if o.Status != "duplicate" || !strings.Contains(o.Duplicate, "OL-") {
		t.Fatalf("duplicate: %+v", o)
	}
	if o := e.postJSON("fr", e.form(map[string]string{"confirm_duplicate": "1"})); o.Status != "created" {
		t.Fatalf("send anyway: %+v", o)
	}
	if _, err := e.s.Pool.Exec(t.Context(), "update offers set stock_status = 'out_of_stock' where slug = 'huile-olive-60l'"); err != nil {
		t.Fatal(err)
	}
	if o := e.postJSON("fr", e.form(map[string]string{"offer": "huile-olive-60l", "phone": "0711111111"})); o.Status != "unavailable" || o.Step != 0 {
		t.Fatalf("out of stock: %+v", o)
	}
	if o := e.postJSON("fr", e.form(map[string]string{"offer": "no-such-offer", "phone": "0722222222"})); o.Status != "unavailable" {
		t.Fatalf("unknown offer: %+v", o)
	}
}

func TestOrderRateLimitPerIP(t *testing.T) {
	e := newOrderEnv(t)
	phones := []string{"0610000001", "0610000002", "0610000003", "0610000004", "0610000005", "0610000006"}
	var last outcome
	for _, p := range phones {
		last = e.postJSON("fr", e.form(map[string]string{"phone": p}))
	}
	if last.Status != "rate_limited" {
		t.Fatalf("6th order from one IP: %+v", last)
	}
}

func TestMetaPixelOnlyWhenEnabled(t *testing.T) {
	e := newOrderEnv(t)
	off := get(t, e.h, "/fr")
	if strings.Contains(off.Header().Get("Content-Security-Policy"), "facebook") || strings.Contains(off.Body.String(), "data-pixel-id") {
		t.Fatal("pixel origins or data present while the pixel is off")
	}
	if err := e.s.Q.UpsertSetting(t.Context(), dbq.UpsertSettingParams{Key: "marketing", Value: []byte(`{"pixelEnabled":true,"pixelId":"123456789012345","requireConsent":true}`)}); err != nil {
		t.Fatal(err)
	}
	h, _ := newServerWith(t, e.s, nil)
	on := get(t, h, "/fr")
	csp := on.Header().Get("Content-Security-Policy")
	for _, want := range []string{"script-src 'self' 'nonce-", "https://connect.facebook.net", "img-src 'self' data: https://www.facebook.com"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP missing %q: %s", want, csp)
		}
	}
	body := on.Body.String()
	if !strings.Contains(body, `data-pixel-id="123456789012345"`) || !strings.Contains(body, "data-pixel-consent") || !strings.Contains(body, "data-consent") {
		t.Error("pixel data or consent bar missing")
	}
	if !strings.Contains(get(t, h, "/fr/privacy").Body.String(), "Mesure des publicités (Meta)") {
		t.Error("privacy page lacks the pixel paragraph while the pixel is on")
	}
}

func TestOfferPreselectedWithoutJS(t *testing.T) {
	e := newOrderEnv(t)
	body := get(t, e.h, "/fr?offer=huile-olive-90l").Body.String()
	if !strings.Contains(body, `value="huile-olive-90l" checked`) || !strings.Contains(body, "8.100 MAD") {
		t.Fatal("offer from ?offer= not preselected with its total")
	}
}
