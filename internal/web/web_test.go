package web_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/omarchaouki/zaytona-go/internal/assets"
	"github.com/omarchaouki/zaytona-go/internal/config"
	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/db/dbtest"
	"github.com/omarchaouki/zaytona-go/internal/seed"
	"github.com/omarchaouki/zaytona-go/internal/store"
	"github.com/omarchaouki/zaytona-go/internal/web"
	"github.com/omarchaouki/zaytona-go/internal/web/public"
)

var (
	pgURL     string
	uploadDir string
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "zaytona-uploads-")
	if err != nil {
		panic(err)
	}
	uploadDir = dir
	code := dbtest.RunMain(m, &pgURL)
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func newServer(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	s := store.New(dbtest.NewDatabase(t, pgURL))
	if err := seed.Run(context.Background(), s, seed.Options{DemoReviews: true}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.FromLookup(func(k string) (string, bool) {
		v, ok := map[string]string{
			"APP_ENV":      "development",
			"DATABASE_URL": "postgres://localhost/db",
			"APP_SECRET":   config.ExampleSecret,
			"SITE_URL":     "https://huilerie.example",
			"UPLOAD_DIR":   uploadDir,
		}[k]
		return v, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	a, err := assets.Load(fstest.MapFS{
		"manifest.json":                {Data: []byte(`{"css/app.css":"css/app.0123456789.css"}`)},
		"css/app.0123456789.css":       {Data: []byte("body{}")},
		"css/app.0123456789.css.gz":    {Data: []byte("not really gzip")},
		"fonts/LICENSES/Inter-OFL.txt": {Data: []byte("OFL")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return web.NewHandler(web.Deps{
		Config: cfg,
		Store:  s,
		Site:   store.NewSiteCache(s, time.Minute),
		Assets: a,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}), s
}

// newServerFor builds a handler on an existing (already seeded) store.
func newServerFor(t *testing.T, s *store.Store) (http.Handler, *store.Store) {
	return newServerWith(t, s, nil)
}

// testConfig is the development config shared by the handler tests.
func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.FromLookup(func(k string) (string, bool) {
		v, ok := map[string]string{
			"APP_ENV":      "development",
			"DATABASE_URL": "postgres://localhost/db",
			"APP_SECRET":   config.ExampleSecret,
			"SITE_URL":     "https://huilerie.example",
			"UPLOAD_DIR":   uploadDir,
		}[k]
		return v, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// newServerWith also takes the order notifier (nil: none).
func newServerWith(t *testing.T, s *store.Store, n public.OrderNotifier) (http.Handler, *store.Store) {
	t.Helper()
	cfg, err := config.FromLookup(func(k string) (string, bool) {
		v, ok := map[string]string{
			"APP_ENV":      "development",
			"DATABASE_URL": "postgres://localhost/db",
			"APP_SECRET":   config.ExampleSecret,
			"SITE_URL":     "https://huilerie.example",
			"UPLOAD_DIR":   uploadDir,
		}[k]
		return v, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	a, err := assets.Load(fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	return web.NewHandler(web.Deps{
		Config:   cfg,
		Store:    s,
		Site:     store.NewSiteCache(s, time.Minute),
		Assets:   a,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Notifier: n,
	}), s
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
	return rec
}

func TestRoutes(t *testing.T) {
	h, _ := newServer(t)

	if rec := get(t, h, "/"); rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/ar" {
		t.Fatalf("/ -> %d %s", rec.Code, rec.Header().Get("Location"))
	}
	for _, p := range []string{"/en", "/ar/nope", "/admin.php"} {
		if rec := get(t, h, p); rec.Code != http.StatusNotFound {
			t.Errorf("%s -> %d", p, rec.Code)
		}
	}
	if rec := get(t, h, "/robots.txt"); !strings.Contains(rec.Body.String(), "Disallow: /admin") {
		t.Errorf("robots: %q", rec.Body.String())
	}
	if rec := get(t, h, "/static/css/app.0123456789.css"); rec.Code != 200 || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("hashed css: %d %v", rec.Code, rec.Header())
	}
	for _, p := range []string{"/static/manifest.json", "/static/../go.mod", "/static/css/app.0123456789.css.gz"} {
		// Traversal is cleaned by the mux (redirect to a path that 404s); never a 200.
		if rec := get(t, h, p); rec.Code == http.StatusOK {
			t.Errorf("%s should not be served: %d", p, rec.Code)
		}
	}
}

var (
	nonceAttr  = regexp.MustCompile(`<script nonce="([^"]+)"`)
	scriptTag  = regexp.MustCompile(`<script\b[^>]*>`)
	styleAttr  = regexp.MustCompile(`\sstyle=`)
	inlineHand = regexp.MustCompile(`\son[a-z]+=`)
)

func TestLandingPages(t *testing.T) {
	h, _ := newServer(t)
	for _, tc := range []struct{ locale, lang, dir, price string }{
		{"ar", `lang="ar-MA"`, `dir="rtl"`, "2.700 درهم"},
		{"fr", `lang="fr-MA"`, `dir="ltr"`, "2.700 MAD"},
	} {
		rec := get(t, h, "/"+tc.locale)
		body := rec.Body.String()
		if rec.Code != 200 {
			t.Fatalf("%s: %d", tc.locale, rec.Code)
		}
		for _, want := range []string{
			tc.lang, tc.dir, tc.price,
			`hreflang="ar" href="https://huilerie.example/ar"`,
			`hreflang="fr" href="https://huilerie.example/fr"`,
			`hreflang="x-default" href="https://huilerie.example/ar"`,
			`rel="canonical" href="https://huilerie.example/` + tc.locale + `"`,
			`/static/css/app.0123456789.css`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing %q", tc.locale, want)
			}
		}

		m := nonceAttr.FindStringSubmatch(body)
		if m == nil || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "'nonce-"+m[1]+"'") {
			t.Errorf("%s: script nonce does not match the CSP", tc.locale)
		}
		for _, tag := range scriptTag.FindAllString(body, -1) {
			if !strings.Contains(tag, "nonce=") {
				t.Errorf("%s: script without nonce: %s", tc.locale, tag)
			}
		}
		if styleAttr.MatchString(body) || inlineHand.MatchString(body) {
			t.Errorf("%s: inline style or event handler in HTML", tc.locale)
		}
		// No WhatsApp number seeded: every WhatsApp button is hidden.
		if strings.Contains(body, "wa.me") {
			t.Errorf("%s: WhatsApp link shown without a number", tc.locale)
		}
	}
}

// The litre gauge works without JS: a GET to ?offer=custom&liters=<n>#order
// preselects "other quantity" with those litres; anything but digits is ignored.
func TestGaugePrefillsCustomLiters(t *testing.T) {
	h, _ := newServer(t)
	body := get(t, h, "/fr?offer=custom&liters=40").Body.String()
	if !regexp.MustCompile(`value="custom" checked`).MatchString(body) {
		t.Error("custom quantity not preselected")
	}
	if !strings.Contains(body, `id="order-liters" name="liters" type="text" inputmode="numeric" pattern="[0-9]*" maxlength="3" autocomplete="off" enterkeyhint="next" dir="ltr" value="40"`) {
		t.Error("litres not prefilled")
	}
	if !strings.Contains(body, "3.600 MAD") {
		t.Error("server total for 40 L at 90 MAD missing")
	}
	body = get(t, h, "/fr?offer=custom&liters=4x0").Body.String()
	if strings.Contains(body, `enterkeyhint="next" dir="ltr" value="4x0"`) {
		t.Error("non-digit litres echoed into the form")
	}
}

func TestLanguageSwitchKeepsQuery(t *testing.T) {
	h, _ := newServer(t)
	body := get(t, h, "/fr?utm_source=fb").Body.String()
	if !strings.Contains(body, `href="/ar?utm_source=fb"`) {
		t.Fatal("switcher lost the query string")
	}
}

func TestLandingSectionsAndJSONLD(t *testing.T) {
	h, _ := newServer(t)
	body := get(t, h, "/fr").Body.String()
	for _, id := range []string{
		`id="hero-title"`, `id="products-title"`, `id="benefits-title"`, `id="reasons-title"`,
		`id="onssa"`, `id="why-title"`, `id="how-title"`, `id="story-title"`, `id="order"`,
		`id="faq-title"`, `id="final-cta-title"`, `<footer`,
		"90 MAD le litre", "8.100 MAD", `data-offer="huile-olive-30l"`, `name="offer" value="custom"`,
		`id="faq"`, `data-gauge`, `/fr/privacy`, `/fr/terms`, `rel="preload" as="image"`,
	} {
		if !strings.Contains(body, id) {
			t.Errorf("landing missing %q", id)
		}
	}
	// Structured data from real data only: demo offers and example reviews never appear.
	if !strings.Contains(body, `"@type":"LocalBusiness"`) || !strings.Contains(body, `"@type":"FAQPage"`) {
		t.Error("LocalBusiness or FAQPage JSON-LD missing")
	}
	for _, bad := range []string{`"@type":"Product"`, `"aggregateRating"`} {
		if strings.Contains(body, bad) {
			t.Errorf("demo data leaked into JSON-LD: %s", bad)
		}
	}
	// Example reviews are not "real": no hero rating snippet.
	if strings.Contains(body, `href="#reviews"`) {
		t.Error("rating snippet shown from example reviews")
	}
}

func TestWhatsAppNumberDrivesEveryButton(t *testing.T) {
	_, s := newServer(t)
	if err := s.Q.UpsertSetting(t.Context(), dbq.UpsertSettingParams{Key: "whatsapp", Value: []byte(`{"number":"212612345678"}`)}); err != nil {
		t.Fatal(err)
	}
	h, _ := newServerFor(t, s)
	body := get(t, h, "/ar").Body.String()
	for _, placement := range []string{"hero", "floating", "mobile_bar", "products_help", "faq_more", "final_cta", "footer", "order_section", "why_us"} {
		if !strings.Contains(body, `data-wa-placement="`+placement+`"`) {
			t.Errorf("WhatsApp button %s missing", placement)
		}
	}
	if !strings.Contains(body, "https://wa.me/212612345678?text=") {
		t.Error("wa.me link not built from the setting")
	}
}

func TestPoliciesAndSitemap(t *testing.T) {
	h, _ := newServer(t)
	body := get(t, h, "/fr/privacy").Body.String()
	if !strings.Contains(body, "Politique de confidentialité") || !strings.Contains(body, "<h2>") {
		t.Error("privacy draft not rendered")
	}
	if rec := get(t, h, "/ar/terms"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "شروط البيع") {
		t.Errorf("terms: %d", rec.Code)
	}
	sm := get(t, h, "/sitemap.xml")
	if sm.Code != 200 || strings.Count(sm.Body.String(), "<url>") != 6 || !strings.Contains(sm.Body.String(), "https://huilerie.example/fr/privacy") {
		t.Errorf("sitemap: %s", sm.Body.String())
	}
	if !strings.Contains(get(t, h, "/robots.txt").Body.String(), "Sitemap: https://huilerie.example/sitemap.xml") {
		t.Error("robots.txt without sitemap")
	}
}

func TestWAClickCountsAndRateLimits(t *testing.T) {
	h, s := newServer(t)
	post := func(body string) int {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/wa-click", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if c := post(`{"placement":"evil","locale":"ar"}`); c != http.StatusBadRequest {
		t.Fatalf("unknown placement: %d", c)
	}
	if c := post(`{"placement":"hero","locale":"en"}`); c != http.StatusBadRequest {
		t.Fatalf("unknown locale: %d", c)
	}
	if c := post(strings.Repeat("x", 4096)); c == http.StatusNoContent {
		t.Fatal("oversized body accepted")
	}
	for range 35 {
		if c := post(`{"placement":"hero","locale":"fr"}`); c != http.StatusNoContent {
			t.Fatalf("valid click: %d", c)
		}
	}
	n, err := s.Q.CountWhatsAppClicksSince(t.Context(), time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 30 {
		t.Fatalf("%d clicks stored, want 30 (rate limit 30/min per IP)", n)
	}
}

// PROMPT.md section 9.3: landing HTML <= 60 KB gzip, at most 2 preloaded fonts.
func TestLandingHTMLBudget(t *testing.T) {
	h, _ := newServer(t)
	for _, l := range []string{"ar", "fr"} {
		body := get(t, h, "/"+l).Body.Bytes()
		var b bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&b, gzip.BestCompression)
		_, _ = zw.Write(body)
		_ = zw.Close()
		t.Logf("/%s: %d bytes, %d gzip", l, len(body), b.Len())
		if b.Len() > 60<<10 {
			t.Errorf("/%s HTML is %d bytes gzip, budget 60 KB", l, b.Len())
		}
		if n := strings.Count(string(body), `as="font"`); n > 2 {
			t.Errorf("/%s preloads %d fonts, budget 2", l, n)
		}
	}
}
