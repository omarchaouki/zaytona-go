//go:build e2e

package e2e

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestAdminFlow signs in with a test admin (E2E_ADMIN_EMAIL, E2E_ADMIN_PASSWORD:
// an owner without 2FA and without a pending password change, on the
// throwaway database), then works through the order pages like the owner on a phone.
func TestAdminFlow(t *testing.T) {
	email, password := os.Getenv("E2E_ADMIN_EMAIL"), os.Getenv("E2E_ADMIN_PASSWORD")
	if email == "" || password == "" {
		t.Skip("E2E_ADMIN_EMAIL and E2E_ADMIN_PASSWORD not set")
	}
	ctx := browser(t)

	// A fresh order first, so there is an unseen one.
	run(t, ctx, phone(ctx))
	placeTestOrder(t, ctx)

	run(t, ctx,
		chromedp.Navigate(base()+"/admin/login?lang=fr"),
		chromedp.WaitVisible(chromedp.CSS(`input[name=email]`)),
		chromedp.SendKeys(chromedp.CSS(`input[name=email]`), email),
		chromedp.SendKeys(chromedp.CSS(`input[name=password]`), password),
		chromedp.Click(chromedp.CSS(`button[type=submit]`)),
		chromedp.WaitVisible(chromedp.CSS(`[data-autorefresh]`)),
	)

	// The poll puts "(n)" in the tab title and fills the notification.
	waitFor(t, ctx, `/^\(\d+\)/.test(document.title) && !!document.querySelector('[data-notify-root], [data-notifications] > *')`)

	// Orders list: checking a row shows the bulk bar with the count.
	run(t, ctx, chromedp.Navigate(base()+"/admin/orders?paid=unseen"), chromedp.WaitVisible(chromedp.CSS(`[data-orders]`)))
	if !eval[bool](t, ctx, `document.querySelector('[data-bulk]').hidden`) {
		t.Fatal("bulk bar visible with nothing checked")
	}
	run(t, ctx, chromedp.Click(chromedp.CSS(`ul[data-orders] [data-select-row]`)))
	if eval[bool](t, ctx, `document.querySelector('[data-bulk]').hidden`) || !strings.Contains(eval[string](t, ctx, `document.querySelector('[data-bulk-count]').textContent`), "1") {
		t.Fatal("bulk bar not shown for one checked row")
	}

	// Open the newest unseen order: it is marked seen by a POST, and the
	// status change goes through the plain form.
	href := eval[string](t, ctx, `document.querySelector('ul[data-orders] a[href^="/admin/orders/"]').pathname`)
	before := eval[int](t, ctx, `Number(document.querySelector('[data-unseen-badge]').textContent)`)
	run(t, ctx, chromedp.Navigate(base()+href), chromedp.WaitVisible(chromedp.CSS(`input[name=status][value=confirmed]`)))
	run(t, ctx,
		chromedp.Click(chromedp.CSS(`input[name=status][value=confirmed]`)),
		chromedp.Click(chromedp.CSS(`form[action$="/status"] button[type=submit]`)),
		chromedp.WaitVisible(chromedp.CSS(`[role=status]`)),
	)
	if !eval[bool](t, ctx, `document.querySelector('input[name=status][value=confirmed]').checked`) {
		t.Fatal("status not saved")
	}
	after := eval[int](t, ctx, `Number(document.querySelector('[data-unseen-badge]').textContent)`)
	if after != before-1 {
		t.Fatalf("unseen count %d -> %d after opening the order", before, after)
	}

	// Delete asks first; Cancel keeps the order.
	if eval[bool](t, ctx, `!!document.querySelector('[data-confirm="confirm-delete"]')`) {
		run(t, ctx, chromedp.Click(chromedp.CSS(`button[data-confirm="confirm-delete"]`)))
		if !eval[bool](t, ctx, `document.getElementById('confirm-delete').open`) {
			t.Fatal("delete dialog not open")
		}
		run(t, ctx, chromedp.Click(chromedp.CSS(`#confirm-delete form[method=dialog] button`)))
		if !strings.HasPrefix(eval[string](t, ctx, `location.pathname`), href) {
			t.Fatal("cancel left the page")
		}
	}
}

// placeTestOrder sends the order form in French and waits for the confirmation page.
func placeTestOrder(t *testing.T, ctx context.Context) {
	t.Helper()
	run(t, ctx,
		chromedp.Navigate(base()+"/fr"),
		chromedp.WaitVisible(chromedp.CSS(`[data-order-form][data-enhanced]`)),
		tap(`a[data-offer="huile-olive-30l"]`),
		tap(`[data-submit]`),
		chromedp.WaitVisible(chromedp.CSS(`#order-name`)),
		chromedp.SendKeys(chromedp.CSS(`#order-name`), "Admin E2E"),
		chromedp.SendKeys(chromedp.CSS(`#order-phone`), uniquePhone("06")),
		chromedp.SendKeys(chromedp.CSS(`#order-city`), "Rabat"),
		tap(`[data-submit]`),
		chromedp.WaitVisible(chromedp.CSS(`[data-review="name"]`)),
		chromedp.Sleep(3200*time.Millisecond), // the render-time token needs 3 s
		tap(`[data-submit]`),
	)
	waitConfirmation(t, ctx)
}

// waitFor polls a JS condition for up to 30 s (pages may navigate meanwhile).
func waitFor(t *testing.T, ctx context.Context, js string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](js))
		if err == nil && ok {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", js)
}

// TestAdminDeliveryPreview: the sentence the customer reads follows the
// delivery fields while typing (nothing is saved).
func TestAdminDeliveryPreview(t *testing.T) {
	email, password := os.Getenv("E2E_ADMIN_EMAIL"), os.Getenv("E2E_ADMIN_PASSWORD")
	if email == "" || password == "" {
		t.Skip("E2E_ADMIN_EMAIL and E2E_ADMIN_PASSWORD not set")
	}
	ctx := browser(t)
	run(t, ctx, phone(ctx),
		chromedp.Navigate(base()+"/admin/login?lang=fr"),
		chromedp.WaitVisible(chromedp.CSS(`input[name=email]`)),
		chromedp.SendKeys(chromedp.CSS(`input[name=email]`), email),
		chromedp.SendKeys(chromedp.CSS(`input[name=password]`), password),
		chromedp.Click(chromedp.CSS(`button[type=submit]`)),
		chromedp.WaitVisible(chromedp.CSS(`[data-autorefresh]`)),
		chromedp.Navigate(base()+"/admin/settings#delivery"),
		chromedp.WaitVisible(chromedp.CSS(`[data-delivery-preview]`)),
		chromedp.Click(chromedp.CSS(`#delivery input[value=free_above]`)),
	)
	eval[any](t, ctx, `(() => { const f = document.querySelector('[data-delivery-preview]').closest('form'); f.elements.fee.value = ''; f.elements.freeAbove.value = ''; return 0 })()`)
	run(t, ctx,
		chromedp.SendKeys(chromedp.CSS(`#delivery-fee`), "25"),
		chromedp.SendKeys(chromedp.CSS(`#delivery-freeAbove`), "600"),
	)
	waitFor(t, ctx, `document.querySelector('[data-preview-fr]').textContent.includes('600 MAD') && document.querySelector('[data-preview-ar]').textContent.includes('600')`)
}
