//go:build e2e

// Package e2e drives a running server in headless Chrome at phone size
// (390x844): BASE_URL=http://localhost:3001 go test -tags e2e ./tests/e2e/...
// Use a throwaway database: these tests place real orders.
package e2e

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

func base() string {
	if u := strings.TrimRight(os.Getenv("BASE_URL"), "/"); u != "" {
		return u
	}
	return "http://localhost:3000"
}

// browser starts one headless Chrome for a test; tab opens pages in it.
func browser(t *testing.T) context.Context {
	t.Helper()
	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), chromedp.DefaultExecAllocatorOptions[:]...)
	t.Cleanup(cancelAlloc)
	ctx, cancel := chromedp.NewContext(alloc)
	t.Cleanup(cancel)
	if err := chromedp.Do(ctx); err != nil {
		t.Fatal(err)
	}
	return ctx
}

func run(t *testing.T, ctx context.Context, steps ...chromedp.Action[chromedp.Void]) {
	t.Helper()
	tctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if err := chromedp.Do(tctx, steps...); err != nil {
		t.Fatal(err)
	}
}

func eval[T any](t *testing.T, ctx context.Context, js string) T {
	t.Helper()
	v, err := chromedp.Run(ctx, chromedp.Evaluate[T](js))
	if err != nil {
		t.Fatalf("%s: %v", js, err)
	}
	return v
}

// waitConfirmation waits for the confirmation page URL. The page navigates
// while we wait, so each check is its own evaluation and errors are retried.
func waitConfirmation(t *testing.T, ctx context.Context) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		path, err := chromedp.Run(ctx, chromedp.Evaluate[string](`location.pathname`))
		if err == nil && strings.Contains(path, "/order/OL-") {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	state, _ := chromedp.Run(ctx, chromedp.Evaluate[string](`JSON.stringify({
		error: document.querySelector('[data-form-error-text]')?.textContent,
		errorHidden: document.querySelector('[data-form-error]')?.hidden,
		duplicate: document.querySelector('[data-duplicate]')?.hidden === false,
		submit: document.querySelector('[data-submit]')?.textContent.trim(),
		altcha: (document.querySelector('[data-altcha]')?.value || '').length,
	})`))
	t.Fatalf("no confirmation page; form state: %s", state)
}

// uniquePhone avoids the 10-minute duplicate guard between runs.
func uniquePhone(prefix string) string {
	return fmt.Sprintf("%s%08d", prefix, time.Now().UnixNano()/1000%100_000_000)
}

// phone emulates a 390x844 phone. Reduced motion turns off smooth scrolling,
// which would move elements between chromedp's scroll and its click.
func phone(ctx context.Context) chromedp.Action[chromedp.Void] {
	return chromedp.Steps(
		chromedp.EmulateViewport(390, 844, chromedp.EmulateScale(2), chromedp.EmulateMobile, chromedp.EmulateTouch),
		chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
			_, err := cdp.Call(ctx, t, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{
				Features: []*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}},
			})
			return err
		}),
	)
}

// tap scrolls an element to the middle of the screen, then clicks it there
// (at the bottom edge the phone's sticky order bar would take the click, as
// it would a thumb).
func tap(sel string) chromedp.Action[chromedp.Void] {
	return chromedp.Steps(
		chromedp.Func(func(ctx context.Context, tg *chromedp.Target) error {
			_, err := chromedp.Evaluate[int]("document.querySelector("+strconv.Quote(sel)+").scrollIntoView({ block: 'center', behavior: 'instant' }); 0")(ctx, tg)
			return err
		}),
		chromedp.Click(chromedp.CSS(sel)),
	)
}

// fillDetails goes from step 1 to the review step.
func fillDetails(t *testing.T, ctx context.Context, name, tel string) {
	t.Helper()
	run(t, ctx,
		tap(`a[data-offer="huile-olive-60l"]`),
		tap(`[data-submit]`),
		chromedp.WaitVisible(chromedp.CSS(`#order-name`)),
		chromedp.SendKeys(chromedp.CSS(`#order-name`), name),
		chromedp.SendKeys(chromedp.CSS(`#order-phone`), tel),
		chromedp.SendKeys(chromedp.CSS(`#order-city`), "Marrakech"),
		tap(`[data-submit]`),
		chromedp.WaitVisible(chromedp.CSS(`[data-review="name"]`)),
	)
}

func TestOrderFlowBothLanguages(t *testing.T) {
	for _, tc := range []struct{ locale, name, phone, thanks string }{
		{"fr", "Client E2E", uniquePhone("06"), "Merci Client"},
		{"ar", "زبون تجربة", uniquePhone("07"), "زبون"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			ctx := browser(t)
			start := time.Now()
			run(t, ctx, phone(ctx), chromedp.Navigate(base()+"/"+tc.locale), chromedp.WaitVisible(chromedp.CSS(`[data-order-form][data-enhanced]`)))
			// Only the first step shows; the offer card picked its option.
			if n := eval[int](t, ctx, `[...document.querySelectorAll('[data-step]')].filter(s => s.offsetParent).length`); n != 1 {
				t.Fatalf("%d steps visible", n)
			}
			fillDetails(t, ctx, tc.name, tc.phone)
			if got := eval[string](t, ctx, `document.querySelector('[data-review="name"]').textContent`); got != tc.name {
				t.Fatalf("review shows %q", got)
			}
			if got := eval[string](t, ctx, `document.querySelector('[data-review-lines]').textContent`); !strings.Contains(got, "60") {
				t.Fatalf("review lines %q", got)
			}
			// The render-time token needs 3 s; people take longer anyway.
			if wait := 3500*time.Millisecond - time.Since(start); wait > 0 {
				time.Sleep(wait)
			}
			run(t, ctx, tap(`[data-submit]`))
			waitConfirmation(t, ctx)
			run(t, ctx, chromedp.WaitVisible(chromedp.CSS(`[data-lead]`)))
			body := eval[string](t, ctx, `document.body.innerText`)
			if !strings.Contains(body, tc.thanks) || !strings.Contains(body, "OL-") {
				t.Fatalf("confirmation page: %.300s", body)
			}
			if eval[bool](t, ctx, `localStorage.getItem('order-draft') !== null`) {
				t.Fatal("draft kept after the order was sent")
			}
		})
	}
}

// The litre gauge prices a quantity live from the price per litre, and its
// button fills the order form in place (no reload) with "other quantity".
func TestLitreGaugeFillsOrderForm(t *testing.T) {
	ctx := browser(t)
	run(t, ctx, phone(ctx), chromedp.Navigate(base()+"/fr"), chromedp.WaitVisible(chromedp.CSS(`[data-order-form][data-enhanced]`)))
	_ = eval[any](t, ctx, `(() => { const n = document.querySelector('#gauge-liters'); n.value = '40'; n.dispatchEvent(new Event('input', { bubbles: true })); return 0 })()`)
	if got := eval[string](t, ctx, `document.querySelector('[data-gauge-total]').textContent.trim()`); got != "3.600 MAD" {
		t.Fatalf("gauge total for 40 L: %q", got)
	}
	run(t, ctx, tap(`[data-gauge-submit]`))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('[data-order-form] input[name=offer][value=custom]')?.checked === true && document.querySelector('#order-liters')?.value === '40'`)); err != nil {
		t.Fatalf("order form not filled from the gauge: %v", err)
	}
	if got := eval[string](t, ctx, `document.querySelector('[data-order-form] [data-total]').textContent`); !strings.Contains(got, "3.600") {
		t.Fatalf("order total: %q", got)
	}
	if path := eval[string](t, ctx, `location.pathname + location.search`); path != "/fr" {
		t.Fatalf("the gauge navigated: %q", path)
	}
}

func TestOrderDraftRestoredAfterReload(t *testing.T) {
	ctx := browser(t)
	run(t, ctx, phone(ctx), chromedp.Navigate(base()+"/fr"), chromedp.WaitVisible(chromedp.CSS(`[data-order-form][data-enhanced]`)),
		tap(`a[data-offer="huile-olive-30l"]`),
		tap(`[data-submit]`),
		chromedp.WaitVisible(chromedp.CSS(`#order-name`)),
		chromedp.SendKeys(chromedp.CSS(`#order-name`), "Brouillon Gardé"),
	)
	// Typing is asynchronous: wait for the whole value, then for the draft save.
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](`document.querySelector('#order-name').value === 'Brouillon Gardé'`)); err != nil {
		t.Fatal(err)
	}
	run(t, ctx,
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Reload(),
		chromedp.WaitVisible(chromedp.CSS(`[data-order-form][data-enhanced]`)),
	)
	if got := eval[string](t, ctx, `document.querySelector('#order-name').value`); got != "Brouillon Gardé" {
		t.Fatalf("name after reload: %q", got)
	}
	if !eval[bool](t, ctx, `document.querySelector('[data-step="1"]').hasAttribute('data-current')`) {
		t.Fatal("step not restored")
	}
	if eval[string](t, ctx, `document.querySelector('input[name=offer]:checked')?.value || ''`) != "huile-olive-30l" {
		t.Fatal("offer not restored")
	}
	_ = eval[any](t, ctx, `localStorage.clear(); 0`)
}

func TestOrderNetworkFailureShowsRecovery(t *testing.T) {
	ctx := browser(t)
	start := time.Now()
	run(t, ctx, phone(ctx), chromedp.Navigate(base()+"/fr"), chromedp.WaitVisible(chromedp.CSS(`[data-order-form][data-enhanced]`)))
	fillDetails(t, ctx, "Client Hors Ligne", uniquePhone("06"))
	if wait := 3500*time.Millisecond - time.Since(start); wait > 0 {
		time.Sleep(wait)
	}
	// The connection drops: order posts fail (twice, the automatic retry too).
	_ = eval[any](t, ctx, `window.__fetch = window.fetch; window.fetch = (u, o) => (o && o.method === 'POST') ? Promise.reject(new TypeError('offline')) : window.__fetch(u, o); 0`)
	run(t, ctx, tap(`[data-submit]`), chromedp.WaitVisible(chromedp.CSS(`[data-recovery]`)))
	if got := eval[string](t, ctx, `document.querySelector('[data-form-error-text]').textContent`); !strings.Contains(got, "Connexion impossible") {
		t.Fatalf("error text %q", got)
	}
	if got := eval[string](t, ctx, `document.querySelector('#order-name').value`); got != "Client Hors Ligne" {
		t.Fatalf("details lost: %q", got)
	}
	// Back online: Retry sends the order.
	_ = eval[any](t, ctx, `window.fetch = window.__fetch; 0`)
	run(t, ctx, chromedp.Click(chromedp.CSS(`[data-recovery] button[type=submit]`)))
	waitConfirmation(t, ctx)
}
