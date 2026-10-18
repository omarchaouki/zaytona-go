package web_test

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// SECURITY.md sections 4 and 7: admin forms accept only their declared fields
// and at most one file in the declared file field.
func TestAdminRejectsUnknownFormFields(t *testing.T) {
	e := newAdminEnv(t)
	o := e.placeOrder("Fatima Zahra", "+212612000001", "Rabat", 1)
	b := e.browser("198.51.100.70:1000")
	b.signIn(ownerEmail, ownerPassword)
	path := "/admin/orders/" + o.ID.String()
	b.get(path)
	if rec := b.post(path+"/status", url.Values{"status": {"confirmed"}, "admin_id": {e.staff.ID.String()}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", rec.Code)
	}
	if rec := b.post(path+"/status", url.Values{"status": {"confirmed"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("known fields: %d", rec.Code)
	}
	b.get("/admin/faq")
	if rec := b.postMultipart("/admin/faq", map[string]string{"questionAr": "س", "questionFr": "Q", "answerAr": "ج", "answerFr": "R"}, "photo", testPhoto(t)); rec.Code != http.StatusBadRequest {
		t.Fatalf("file on a form without uploads: %d", rec.Code)
	}

	// Two files in one field.
	b.get("/admin/gallery")
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("csrf", b.csrf)
	for range 2 {
		w, _ := mw.CreateFormFile("photo", "a.jpg")
		_, _ = w.Write(testPhoto(t))
	}
	_ = mw.Close()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/admin/gallery", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.RemoteAddr = b.ip
	req.AddCookie(b.cookie)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("two files: %d", rec.Code)
	}

	// A file input left empty (what a browser sends when no file is chosen)
	// is not an unknown field; a text value in it is.
	for _, tc := range []struct {
		value string
		bad   bool
	}{{"", false}, {"not a file", true}} {
		b.get("/admin/gallery")
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		_ = mw.WriteField("csrf", b.csrf)
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", `form-data; name="photo"; filename=""`)
		if tc.value != "" {
			h.Set("Content-Disposition", `form-data; name="photo"`)
		}
		w, _ := mw.CreatePart(h)
		_, _ = w.Write([]byte(tc.value))
		_ = mw.Close()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/admin/gallery", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.RemoteAddr = b.ip
		req.AddCookie(b.cookie)
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		if got := rec.Code == http.StatusBadRequest; got != tc.bad {
			t.Fatalf("photo part %q: %d", tc.value, rec.Code)
		}
	}
}

// Unknown emails cost one argon2 verification too: on average the two
// failures take about the same time (generous tolerance for CI noise).
func TestLoginTimingUnknownVsKnown(t *testing.T) {
	e := newAdminEnv(t)
	measure := func(email string, n int) time.Duration {
		var total time.Duration
		for i := range n {
			b := e.browser(fmt.Sprintf("198.51.100.%d:1000", 100+i+len(email)%50))
			start := time.Now()
			if rec := b.login(email, "definitely not the password"); rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s: %d", email, rec.Code)
			}
			total += time.Since(start)
		}
		return total / time.Duration(n)
	}
	measure(ownerEmail, 1) // warm up
	known := measure(staffEmail, 4)
	unknown := measure("nobody-here@huilerie.example", 4)
	lo, hi := min(known, unknown), max(known, unknown)
	if hi > lo*3 {
		t.Fatalf("known %s vs unknown %s: unknown emails are distinguishable", known, unknown)
	}
}

func TestSessionIdleAndAbsoluteExpiry(t *testing.T) {
	e := newAdminEnv(t)
	ctx := context.Background()
	for _, tc := range []struct{ name, set string }{
		{"idle", "idle_expires_at = now() - interval '1 second'"},
		{"absolute", "absolute_expires_at = now() - interval '1 second'"},
	} {
		b := e.browser("198.51.100.80:1000")
		b.signIn(staffEmail, staffPassword)
		if _, err := e.s.Pool.Exec(ctx, "update sessions set "+tc.set); err != nil {
			t.Fatal(err)
		}
		if rec := b.get("/admin"); rec.Code != http.StatusSeeOther || location(rec) != "/admin/login" {
			t.Fatalf("%s expiry: %d %s", tc.name, rec.Code, location(rec))
		}
	}
	// Sliding never goes past the absolute cap.
	b := e.browser("198.51.100.81:1000")
	b.signIn(staffEmail, staffPassword)
	if _, err := e.s.Pool.Exec(ctx, "update sessions set last_seen_at = now() - interval '1 hour', absolute_expires_at = now() + interval '1 minute'"); err != nil {
		t.Fatal(err)
	}
	b.get("/admin")
	var over bool
	if err := e.s.Pool.QueryRow(ctx, "select idle_expires_at > absolute_expires_at from sessions order by created_at desc limit 1").Scan(&over); err != nil || over {
		t.Fatalf("idle expiry past the absolute cap (%v)", err)
	}
}

// snapshot fingerprints the business tables (sessions, rate limits and audit
// rows are not business data: reading a page may touch them).
func (e *adminEnv) snapshot() string {
	e.t.Helper()
	var out strings.Builder
	for _, table := range []string{"orders", "order_items", "order_events", "reviews", "settings", "content_blocks", "offers", "faqs", "certifications", "gallery_images", "media", "admins", "admin_recovery_codes"} {
		var h *string
		if err := e.s.Pool.QueryRow(context.Background(), "select md5(string_agg(t::text, '|' order by t::text)) from "+table+" t").Scan(&h); err != nil {
			e.t.Fatal(err)
		}
		if h != nil {
			out.WriteString(table + "=" + *h + ";")
		}
	}
	return out.String()
}

// SECURITY.md section 5: no state change on GET, ever.
func TestGETNeverChangesData(t *testing.T) {
	e := newAdminEnv(t)
	o := e.placeOrder("Fatima Zahra", "+212612000001", "Rabat", 1)
	b := e.browser("198.51.100.90:1000")
	b.signIn(ownerEmail, ownerPassword)
	before := e.snapshot()
	for _, p := range []string{
		"/admin", "/admin/orders", "/admin/orders?paid=unseen&sort=total&dir=asc", "/admin/orders/" + o.ID.String(),
		"/admin/orders/" + o.ID.String() + "?edit=1", "/admin/orders.csv", "/admin/notifications", "/admin/offers",
		"/admin/settings", "/admin/certificates", "/admin/content", "/admin/faq", "/admin/admins", "/admin/audit",
		"/admin/reviews?status=all", "/admin/security", "/admin/password", "/admin/more", "/admin/2fa",
		"/admin/orders/" + o.ID.String() + "/status", "/admin/logout",
		"/fr", "/ar?stars=5", "/fr/reviews", "/fr/reviews/new", "/fr/privacy", "/sitemap.xml", "/robots.txt",
		"/fr/order/" + o.Reference, "/api/pow",
	} {
		b.get(p)
	}
	if after := e.snapshot(); after != before {
		t.Fatal("a GET request changed business data")
	}
}

// IDOR: ids that do not exist answer 404, never another row.
func TestUnknownIDsAre404(t *testing.T) {
	e := newAdminEnv(t)
	b := e.browser("198.51.100.95:1000")
	b.signIn(ownerEmail, ownerPassword)
	missing := uuid.NewString()
	if rec := b.get("/admin/orders/" + missing); rec.Code != http.StatusNotFound {
		t.Errorf("order page: %d", rec.Code)
	}
	b.get("/admin")
	posts := map[string]url.Values{
		"/admin/orders/" + missing + "/status":       {"status": {"confirmed"}},
		"/admin/orders/" + missing + "/paid":         {"paid": {"1"}},
		"/admin/orders/" + missing + "/seen":         nil,
		"/admin/orders/" + missing + "/note":         {"note": {"x"}},
		"/admin/offers/" + missing:                   {"liters": {"1"}},
		"/admin/offers/" + missing + "/delete":       {"confirm": {"1"}},
		"/admin/certificates/" + missing + "/delete": {"confirm": {"1"}},
		"/admin/faq/" + missing + "/delete":          {"confirm": {"1"}},
		"/admin/gallery/" + missing + "/delete":      {"confirm": {"1"}},
		"/admin/admins/" + missing + "/role":         {"role": {"staff"}},
		"/admin/reviews/" + missing + "/pin":         nil,
		"/admin/orders/not-a-uuid/status":            {"status": {"confirmed"}},
	}
	for p, v := range posts {
		if rec := b.post(p, v); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
	// Confirmation pages need their signed token.
	o := e.placeOrder("Karim", "+212612000002", "Rabat", 1)
	for _, p := range []string{"/fr/order/" + o.Reference, "/fr/order/" + o.Reference + "?t=forged", "/fr/order/OL-2026-999999?t=x"} {
		if rec := get(t, e.h, p); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
}
