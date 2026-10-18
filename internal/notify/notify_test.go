package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omarchaouki/zaytona-go/internal/domain"
)

func TestSSRFBlocklist(t *testing.T) {
	w := NewWebhook([]byte("k"), false)
	for _, ip := range []string{
		"127.0.0.1", "::1", "169.254.169.254", "10.1.2.3", "172.16.0.1", "192.168.1.1",
		"100.64.0.1", "0.0.0.0", "fd00::1", "fe80::1", "::ffff:127.0.0.1", "::ffff:10.0.0.1", "224.0.0.1",
	} {
		if w.allowed(netip.MustParseAddr(ip)) {
			t.Errorf("%s allowed", ip)
		}
	}
	for _, ip := range []string{"93.184.216.34", "2606:2800:220:1:248:1893:25c8:1946"} {
		if !w.allowed(netip.MustParseAddr(ip)) {
			t.Errorf("%s blocked", ip)
		}
	}
	if !NewWebhook(nil, true).allowed(netip.MustParseAddr("127.0.0.1")) {
		t.Error("development should allow a local receiver")
	}
}

func TestValidateURL(t *testing.T) {
	prod := NewWebhook(nil, false)
	for _, bad := range []string{"http://example.com/h", "https://user:pw@example.com/h", "ftp://example.com", "not a url", ""} {
		if prod.ValidateURL(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if prod.ValidateURL("https://hooks.example.com/orders") != nil {
		t.Error("https refused")
	}
}

func TestWebhookRefusesLoopbackInProduction(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	defer srv.Close()
	w := NewWebhook([]byte("k"), false)
	err := w.Post(context.Background(), srv.URL, Order{Reference: "OL-2026-010001"})
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("loopback receiver reached in production: %v", err)
	}
}

func TestWebhookSignsAndDoesNotFollowRedirects(t *testing.T) {
	var got struct {
		sig  string
		body []byte
	}
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
			return
		}
		mu.Lock()
		got.sig = r.Header.Get("X-Zaytona-Signature")
		got.body, _ = io.ReadAll(r.Body)
		mu.Unlock()
		w.WriteHeader(204)
	}))
	defer srv.Close()
	w := NewWebhook([]byte("secret"), true)
	o := Order{Reference: "OL-2026-010001", Total: decimal.RequireFromString("2700")}
	if err := w.Post(context.Background(), srv.URL+"/hook", o); err != nil {
		t.Fatal(err)
	}
	if got.sig != "sha256="+Sign([]byte("secret"), got.body) {
		t.Fatalf("signature %q does not match body", got.sig)
	}
	var payload map[string]any
	if json.Unmarshal(got.body, &payload) != nil || payload["event"] != "order.created" {
		t.Fatalf("payload %s", got.body)
	}
	if err := w.Post(context.Background(), srv.URL+"/redirect", o); err == nil || !strings.Contains(err.Error(), "302") {
		t.Fatalf("redirect followed or not reported: %v", err)
	}
}

func TestNotifierUsesSettingsAndNeverBlocks(t *testing.T) {
	var hits int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.WriteHeader(204)
	}))
	defer srv.Close()
	n := New(Config{WebhookSecret: []byte("k")}, func(context.Context) domain.NotificationSettings {
		return domain.NotificationSettings{WebhookURL: srv.URL}
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	n.webhook = NewWebhook([]byte("k"), true)
	for range 3 {
		n.Enqueue(Order{Reference: "OL-2026-010001"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	n.Close(ctx)
	if hits != 3 {
		t.Fatalf("%d webhook calls, want 3", hits)
	}
}

func TestEmailText(t *testing.T) {
	txt := Text(Order{
		Reference: "OL-2026-010001", Name: "Ali", PhoneE164: "+212612345678", City: "Rabat",
		Items: []Item{{Name: "Huile 30 L", Quantity: 1}}, Subtotal: decimal.RequireFromString("2700"), Total: decimal.RequireFromString("2700"),
	})
	for _, want := range []string{"OL-2026-010001", "06 12 34 56 78", "1 × Huile 30 L", "2.700 MAD", "à confirmer"} {
		if !strings.Contains(txt, want) {
			t.Errorf("email text missing %q:\n%s", want, txt)
		}
	}
}
