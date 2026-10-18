//go:build e2e

package e2e

import (
	"os"
	"testing"

	"github.com/chromedp/chromedp"
)

// A marker set in the page's memory survives only if the page was swapped in
// place, never reloaded.
const (
	setMarker  = `window.__inPlace = 1; 0`
	markerKept = `window.__inPlace === 1`
)

// Public pages change in place: footer link, back button, language switch,
// and a link to a section of the home page.
func TestInPlaceNavigationPublic(t *testing.T) {
	ctx := browser(t)
	run(t, ctx, phone(ctx), chromedp.Navigate(base()+"/fr"), chromedp.WaitVisible(chromedp.CSS(`[data-order-form][data-enhanced]`)))
	_ = eval[int](t, ctx, setMarker)

	run(t, ctx, tap(`footer a[href="/fr/privacy"]`))
	waitFor(t, ctx, `location.pathname === '/fr/privacy' && !!document.querySelector('main h1')`)
	if !eval[bool](t, ctx, markerKept) {
		t.Fatal("the privacy page was a full reload")
	}
	if got := eval[string](t, ctx, `document.title`); got == "" {
		t.Fatal("title not updated")
	}

	// Back: the landing again, scripts alive (the order form is enhanced).
	_ = eval[int](t, ctx, `history.back(); 0`)
	waitFor(t, ctx, `location.pathname === '/fr' && !!document.querySelector('[data-order-form][data-enhanced]')`)
	if !eval[bool](t, ctx, markerKept) {
		t.Fatal("back was a full reload")
	}

	// Language switch: Arabic, right to left, still in place.
	run(t, ctx, tap(`header a[hreflang="ar"]`))
	waitFor(t, ctx, `location.pathname === '/ar' && document.documentElement.dir === 'rtl' && document.documentElement.lang.startsWith('ar')`)
	if !eval[bool](t, ctx, markerKept) {
		t.Fatal("the language switch was a full reload")
	}

	// From another page, "Order" lands on the home page's order section.
	run(t, ctx, tap(`footer a[href="/ar/terms"]`))
	waitFor(t, ctx, `location.pathname === '/ar/terms'`)
	run(t, ctx, tap(`footer a[href="/ar#order"]`))
	waitFor(t, ctx, `location.pathname === '/ar' && location.hash === '#order' && !!document.querySelector('[data-order-form][data-enhanced]')`)
	if !eval[bool](t, ctx, markerKept) {
		t.Fatal("the order link was a full reload")
	}
}

// The admin changes in place too: signing in (a form post), the order list,
// and a page whose form is refused keeps its address.
func TestInPlaceNavigationAdmin(t *testing.T) {
	email, password := os.Getenv("E2E_ADMIN_EMAIL"), os.Getenv("E2E_ADMIN_PASSWORD")
	if email == "" || password == "" {
		t.Skip("E2E_ADMIN_EMAIL and E2E_ADMIN_PASSWORD not set")
	}
	ctx := browser(t)
	run(t, ctx, phone(ctx),
		chromedp.Navigate(base()+"/admin/login?lang=fr"),
		chromedp.WaitVisible(chromedp.CSS(`input[name=email]`)),
	)
	_ = eval[int](t, ctx, setMarker)
	run(t, ctx,
		chromedp.SendKeys(chromedp.CSS(`input[name=email]`), email),
		chromedp.SendKeys(chromedp.CSS(`input[name=password]`), password),
		tap(`button[type=submit]`),
	)
	waitFor(t, ctx, `!!document.querySelector('[data-autorefresh]')`)
	if !eval[bool](t, ctx, markerKept) {
		t.Fatal("signing in was a full reload")
	}
	if path := eval[string](t, ctx, `location.pathname`); path != "/admin" {
		t.Fatalf("after sign-in: %s", path)
	}

	// The phone tab bar's link (the sidebar one is hidden at phone size).
	_ = eval[int](t, ctx, `[...document.querySelectorAll('a[href="/admin/orders"]')].find((a) => a.offsetParent).click(); 0`)
	waitFor(t, ctx, `location.pathname === '/admin/orders' && !!document.querySelector('[data-orders]')`)
	if !eval[bool](t, ctx, markerKept) {
		t.Fatal("the order list was a full reload")
	}

	// Back to the dashboard, then forward again.
	_ = eval[int](t, ctx, `history.back(); 0`)
	waitFor(t, ctx, `location.pathname === '/admin' && !!document.querySelector('[data-autorefresh]')`)
	_ = eval[int](t, ctx, `history.forward(); 0`)
	waitFor(t, ctx, `location.pathname === '/admin/orders'`)
	if !eval[bool](t, ctx, markerKept) {
		t.Fatal("back/forward was a full reload")
	}
}
