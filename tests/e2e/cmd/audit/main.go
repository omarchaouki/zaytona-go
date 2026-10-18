// Command audit is the responsive audit (task audit:mobile): for phone and
// tablet sizes, in Arabic and French, it loads every public page in headless
// Chrome and lists elements sticking out of the screen, content spilling out
// of its box, text under 12px, tap targets under 44px, cramped fields and
// overlapping fixed controls. Exits non-zero on any finding.
//
//	BASE_URL=http://localhost:3000 go run ./tests/e2e/cmd/audit
//
// AUDIT_URLS adds space-separated absolute URLs. AUDIT_ORDER=1 places a real
// order through the form (use a throwaway database) and audits its
// confirmation page in both languages.
// AUDIT_ADMIN_EMAIL and AUDIT_ADMIN_PASSWORD (a test admin without 2FA on a
// throwaway database) sign in and audit the admin pages in both languages,
// with the bulk bar, the "More" sheet and the delete dialog open.
package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

//go:embed inspect.js
var inspectJS string

type finding struct {
	Kind    string `json:"kind"`
	Section string `json:"section"`
	What    string `json:"what"`
}

var sizes = [][2]int64{
	{320, 640},
	{360, 740},
	{390, 844},
	{412, 915},
	{740, 360},  // phone in landscape
	{768, 1024}, // tablet portrait
	{1024, 768}, // tablet landscape
}

var pages = []string{"", "/privacy", "/terms", "/reviews/new"}

// formStates move the order form to its later steps (landing page only), so
// every step is audited, not just the first one.
var formStates = []struct{ name, js string }{
	{"step 2", `(() => {
	  const f = document.querySelector('[data-order-form]'); if (!f) return 0
	  f.querySelector('input[name=offer]:not(:disabled)').checked = true
	  f.querySelector('[data-submit]').click(); return 1 })()`},
	{"step 3", `(() => {
	  const f = document.querySelector('[data-order-form]'); if (!f) return 0
	  f.elements.name.value = 'Fatima Zahra El Idrissi'; f.elements.phone.value = '06 12 34 56 78'
	  f.elements.city.value = 'El Kelaa des Sraghna'; f.elements.address.value = 'Derb Lalla 12, quartier Al Qods'
	  f.elements.note.value = 'Appeler apres 18h'
	  f.querySelector('[data-submit]').click(); return 1 })()`},
	{"review dialog", `(() => { const d = document.getElementById('review-dialog'); if (d) { d.showModal(); d.scrollTop = d.scrollHeight } return 1 })()`},
}

// faults are injected by -selftest to prove the checks fire (CSSOM, not style attributes).
const faults = `(() => {
  const main = document.querySelector('main')
  const tiny = document.createElement('p'); tiny.textContent = 'tiny text'; tiny.style.fontSize = '8px'
  const link = document.createElement('a'); link.href = '#x'; link.textContent = 'x'; link.style.display = 'inline-block'
  const wide = document.createElement('div'); wide.textContent = 'wide'; wide.style.width = '200vw'
  main.prepend(tiny, link, wide)
  return 0
})()`

var (
	selftest         = flag.Bool("selftest", false, "inject known faults and require the audit to report them")
	confirmationOnly = flag.Bool("confirmation-only", false, "skip the public pages (with AUDIT_ORDER=1: only the confirmation page)")
	adminOnly        = flag.Bool("admin-only", false, "only the admin pages (needs AUDIT_ADMIN_EMAIL and AUDIT_ADMIN_PASSWORD)")
	onlyPage         = flag.String("page", "", "only this public page: landing, /privacy, /terms or /reviews/new")
)

const openDetails = `(() => { document.querySelectorAll('details').forEach(d => d.open = true); return 0 })()`

// adminStates open the parts of admin pages that are hidden at first.
var adminStates = map[string][]struct{ name, js string }{
	"/admin/orders": {
		{"bulk bar", `(() => { const b = document.querySelector('[data-select-row]'); if (b) { b.click() } return 0 })()`},
		{"delete dialog", `(() => { const d = document.getElementById('confirm-delete'); if (d) d.showModal(); return 0 })()`},
	},
	"/admin/offers":       {{"editors open", openDetails}},
	"/admin/certificates": {{"editors open", openDetails}},
	"/admin/faq":          {{"editors open", openDetails}},
	"/admin/admins":       {{"add form open", openDetails}},
	"/admin/reviews":      {{"bulk bar and editors", `(() => { const b = document.querySelector('[data-select-row]'); if (b) b.click(); document.querySelectorAll('details').forEach(d => d.open = true); return 0 })()`}},
	"/admin": {
		{"more sheet", `(() => { const d = document.getElementById('admin-more'); if (d && innerWidth < 1024) d.showModal(); return 0 })()`},
		{"warnings", `(() => { document.querySelectorAll('details').forEach(d => d.open = true); return 0 })()`},
	},
}

func main() {
	flag.Parse()
	base := strings.TrimRight(os.Getenv("BASE_URL"), "/")
	if base == "" {
		base = "http://localhost:3000"
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.Flag("hide-scrollbars", true))
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancelAlloc()
	browser, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()
	if err := chromedp.Do(browser); err != nil {
		fail(fmt.Errorf("start Chrome: %w", err))
	}

	if *selftest {
		found, err := audit(browser, base+"/fr", 390, 844)
		if err != nil {
			fail(err)
		}
		kinds := map[string]bool{}
		for _, f := range found {
			kinds[f.Kind] = true
		}
		for _, k := range []string{"tiny-text", "small-target", "overflow"} {
			if !kinds[k] {
				fail(fmt.Errorf("selftest: injected %s not reported (got %v)", k, kinds))
			}
		}
		fmt.Println("selftest ok: injected faults reported", kinds)
		return
	}

	total := 0
	for _, locale := range []string{"ar", "fr"} {
		if *confirmationOnly || *adminOnly {
			break
		}
		for _, sz := range sizes {
			for _, path := range pages {
				if *onlyPage != "" && (*onlyPage != "landing" || path != "") && *onlyPage != path {
					continue
				}
				url := base + "/" + locale + path
				found, err := audit(browser, url, sz[0], sz[1])
				if err != nil {
					fail(fmt.Errorf("%s: %w", url, err))
				}
				if path == "" {
					var states []string
					for _, st := range formStates {
						states = append(states, st.js)
						more, err := audit(browser, url, sz[0], sz[1], states...)
						if err != nil {
							fail(fmt.Errorf("%s %s: %w", url, st.name, err))
						}
						for i := range more {
							more[i].Section = "order " + st.name + " :: " + more[i].Section
						}
						found = append(found, more...)
					}
				}
				tag := fmt.Sprintf("/%s%s %dx%d", locale, path, sz[0], sz[1])
				if len(found) > 0 || path == "" {
					fmt.Printf("\n=== %s: %d finding(s)\n", tag, len(found))
				}
				for i, f := range found {
					if i == 40 {
						fmt.Printf("  ... %d more\n", len(found)-40)
						break
					}
					fmt.Printf("  [%s] %s :: %s\n", f.Kind, f.Section, f.What)
				}
				total += len(found)
			}
		}
	}

	extra := strings.Fields(os.Getenv("AUDIT_URLS"))
	if os.Getenv("AUDIT_ORDER") == "1" {
		confirm, err := placeOrder(browser, base)
		if err != nil {
			fail(fmt.Errorf("placing an order for the confirmation page: %w", err))
		}
		extra = append(extra, confirm, strings.Replace(confirm, "/fr/order/", "/ar/order/", 1))
	}
	for _, u := range extra {
		// The token stays out of the output.
		label, _, _ := strings.Cut(u, "?")
		for _, sz := range sizes {
			found, err := audit(browser, u, sz[0], sz[1])
			if err != nil {
				fail(fmt.Errorf("%s: %w", label, err))
			}
			fmt.Printf("\n=== %s %dx%d: %d finding(s)\n", label, sz[0], sz[1], len(found))
			for _, f := range found {
				fmt.Printf("  [%s] %s :: %s\n", f.Kind, f.Section, f.What)
			}
			total += len(found)
		}
	}
	if email, pw := os.Getenv("AUDIT_ADMIN_EMAIL"), os.Getenv("AUDIT_ADMIN_PASSWORD"); email != "" && pw != "" {
		n, err := auditAdmin(browser, base, email, pw)
		if err != nil {
			fail(fmt.Errorf("admin: %w", err))
		}
		total += n
	} else if *adminOnly {
		fail(fmt.Errorf("-admin-only needs AUDIT_ADMIN_EMAIL and AUDIT_ADMIN_PASSWORD"))
	}
	fmt.Printf("\nTotal: %d\n", total)
	if total > 0 {
		os.Exit(1)
	}
}

func audit(browser context.Context, url string, w, h int64, states ...string) ([]finding, error) {
	tab, cancel := chromedp.NewContext(browser)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(tab, 60*time.Second)
	defer cancelTimeout()
	viewport := []chromedp.EmulateViewportOption{chromedp.EmulateScale(2), chromedp.EmulateTouch}
	if w < 1024 {
		viewport = append(viewport, chromedp.EmulateMobile)
	}
	err := chromedp.Do(ctx,
		chromedp.EmulateViewport(w, h, viewport...),
		chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
			_, err := cdp.Call(ctx, t, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{
				Features: []*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}},
			})
			return err
		}),
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
		chromedp.Sleep(700*time.Millisecond),
	)
	if err != nil {
		return nil, err
	}
	// Reveal everything (as scrolling would) so hidden-until-revealed content is measured.
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`document.querySelectorAll('[data-reveal]').forEach(e => e.setAttribute('data-revealed','')); 0`)); err != nil {
		return nil, err
	}
	if *selftest {
		if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](faults)); err != nil {
			return nil, err
		}
	}
	for _, js := range states {
		if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](js)); err != nil {
			return nil, err
		}
		if err := chromedp.Do(ctx, chromedp.Sleep(300*time.Millisecond)); err != nil {
			return nil, err
		}
	}
	if err := chromedp.Do(ctx, chromedp.Sleep(200*time.Millisecond)); err != nil {
		return nil, err
	}
	raw, err := chromedp.Run(ctx, chromedp.Evaluate[[]finding](inspectJS))
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []finding
	for _, f := range raw {
		if k := f.Kind + f.What; !seen[k] {
			seen[k] = true
			out = append(out, f)
		}
	}
	return out, nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "audit:", err)
	os.Exit(2)
}

// placeOrder fills and sends the order form like a visitor and returns the
// confirmation URL (signed token included; it never leaves this process).
func placeOrder(browser context.Context, base string) (string, error) {
	tab, cancel := chromedp.NewContext(browser)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(tab, 90*time.Second)
	defer cancelTimeout()
	if err := chromedp.Do(ctx, chromedp.Navigate(base+"/fr"), chromedp.WaitVisible(chromedp.CSS(`[data-order-form][data-enhanced]`))); err != nil {
		return "", err
	}
	phone := fmt.Sprintf("06%08d", time.Now().UnixNano()/1000%100_000_000)
	steps := []string{
		`(() => { const f = document.querySelector('[data-order-form]'); f.querySelector('input[name=offer]:not(:disabled)').checked = true; f.querySelector('[data-submit]').click(); return 0 })()`,
		`(() => { const f = document.querySelector('[data-order-form]'); f.elements.name.value = 'Audit Client'; f.elements.phone.value = '` + phone + `'; f.elements.city.value = 'Rabat'; f.querySelector('[data-submit]').click(); return 0 })()`,
	}
	for _, js := range steps {
		if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](js)); err != nil {
			return "", err
		}
	}
	time.Sleep(3500 * time.Millisecond) // the render-time token needs 3 s
	// Already on the confirmation page (the last step can send by itself): done.
	where, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => { if (location.pathname.includes('/order/OL-')) return ''; const b = document.querySelector('[data-submit]'); if (!b) return location.pathname + ' ' + document.title; b.click(); return '' })()`))
	if err != nil {
		return "", err
	}
	if where != "" {
		return "", fmt.Errorf("no submit button on %s", where)
	}
	for range 120 {
		href, err := chromedp.Run(ctx, chromedp.Evaluate[string](`location.href`))
		if err == nil && strings.Contains(href, "/order/OL-") {
			return href, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return "", fmt.Errorf("no confirmation page after sending")
}

// auditAdmin signs in once (tabs share the browser's cookies) and audits the
// admin pages in the admin's language, then switches language and repeats.
func auditAdmin(browser context.Context, base, email, password string) (int, error) {
	total := 0
	report := func(tag string, found []finding) {
		fmt.Printf("\n=== %s: %d finding(s)\n", tag, len(found))
		for _, f := range found {
			fmt.Printf("  [%s] %s :: %s\n", f.Kind, f.Section, f.What)
		}
		total += len(found)
	}
	for _, lang := range []string{"ar", "fr"} {
		for _, sz := range sizes {
			found, err := audit(browser, base+"/admin/login?lang="+lang, sz[0], sz[1])
			if err != nil {
				return total, err
			}
			report(fmt.Sprintf("/admin/login?lang=%s %dx%d", lang, sz[0], sz[1]), found)
		}
	}
	tab, cancel := chromedp.NewContext(browser)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(tab, 45*time.Minute)
	defer cancelTimeout()
	if err := chromedp.Do(ctx,
		chromedp.Navigate(base+"/admin/login"),
		chromedp.WaitReady("body"),
		chromedp.SendKeys(chromedp.CSS(`input[name=email]`), email),
		chromedp.SendKeys(chromedp.CSS(`input[name=password]`), password),
		chromedp.Click(chromedp.CSS(`button[type=submit]`)),
		chromedp.Sleep(2*time.Second),
	); err != nil {
		return total, err
	}
	where, err := chromedp.Run(ctx, chromedp.Evaluate[string](`location.pathname`))
	if err != nil {
		return total, err
	}
	if where != "/admin" {
		return total, fmt.Errorf("sign-in ended on %s (wrong credentials, 2FA on, or a password change pending)", where)
	}
	order, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(document.querySelector('a[href^="/admin/orders/"]') || {}).pathname || ''`))
	if err != nil {
		return total, err
	}
	paths := []string{
		"/admin", "/admin/orders", "/admin/security", "/admin/password", "/admin/more",
		"/admin/offers", "/admin/settings", "/admin/certificates", "/admin/content", "/admin/faq",
		"/admin/admins", "/admin/audit", "/admin/reviews", "/admin/reviews?status=all",
	}
	if order != "" {
		paths = append(paths, order, order+"?edit=1")
	}
	for range 2 {
		for _, path := range paths {
			for _, sz := range sizes {
				found, err := audit(browser, base+path, sz[0], sz[1])
				if err != nil {
					return total, err
				}
				var states []string
				for _, st := range adminStates[path] {
					states = append(states, st.js)
					more, err := audit(browser, base+path, sz[0], sz[1], states...)
					if err != nil {
						return total, err
					}
					for i := range more {
						more[i].Section = st.name + " :: " + more[i].Section
					}
					found = append(found, more...)
				}
				lang, _ := chromedp.Run(ctx, chromedp.Evaluate[string](`document.documentElement.lang`))
				report(fmt.Sprintf("%s %dx%d (%s)", path, sz[0], sz[1], lang), found)
			}
		}
		// Switch the admin language (a POST form, like a click on the button).
		if err := chromedp.Do(ctx, chromedp.Navigate(base+"/admin"), chromedp.WaitReady("body")); err != nil {
			return total, err
		}
		if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`document.querySelector('form[action="/admin/locale"] button').click(); 0`)); err != nil {
			return total, err
		}
		if err := chromedp.Do(ctx, chromedp.Sleep(1500*time.Millisecond)); err != nil {
			return total, err
		}
	}
	return total, nil
}
