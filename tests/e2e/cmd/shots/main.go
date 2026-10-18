// Command shots saves one screenshot per landing section (headless Chrome,
// reduced motion) into tests/e2e/screenshots for visual review.
//
//	BASE_URL=http://localhost:3000 go run ./tests/e2e/cmd/shots [-width 390] [-locale ar]
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

func main() {
	width := flag.Int64("width", 390, "viewport width")
	locale := flag.String("locale", "", "ar or fr (default both)")
	path := flag.String("path", "", "path after /{locale}")
	flag.Parse()
	base := strings.TrimRight(os.Getenv("BASE_URL"), "/")
	if base == "" {
		base = "http://localhost:3000"
	}
	locales := []string{"ar", "fr"}
	if *locale != "" {
		locales = []string{*locale}
	}
	out := filepath.Join("tests", "e2e", "screenshots")
	if err := os.MkdirAll(out, 0o750); err != nil {
		fail(err)
	}

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), chromedp.DefaultExecAllocatorOptions[:]...)
	defer cancelAlloc()
	browser, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	if err := chromedp.Do(browser); err != nil {
		fail(err)
	}
	for _, l := range locales {
		ctx, cancelTab := context.WithTimeout(browser, 2*time.Minute)
		opts := []chromedp.EmulateViewportOption{chromedp.EmulateScale(1)}
		if *width < 1024 {
			opts = append(opts, chromedp.EmulateMobile, chromedp.EmulateTouch)
		}
		err := chromedp.Do(ctx,
			chromedp.EmulateViewport(*width, 900, opts...),
			chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
				_, err := cdp.Call(ctx, t, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{
					Features: []*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}},
				})
				return err
			}),
			chromedp.Navigate(base+"/"+l+*path),
			chromedp.WaitReady("body"),
		)
		if err == nil {
			_, err = chromedp.Run(ctx, chromedp.Evaluate[any](`document.querySelectorAll('[data-reveal]').forEach(e => e.setAttribute('data-revealed',''));
				document.querySelectorAll('img[loading=lazy]').forEach(i => i.loading = 'eager'); 0`))
		}
		if err == nil {
			err = chromedp.Do(ctx, chromedp.Sleep(2500*time.Millisecond))
		}
		var n int
		if err == nil {
			n, err = chromedp.Run(ctx, chromedp.Evaluate[int](`document.querySelectorAll('main > *').length`))
		}
		for i := 1; err == nil && i <= n+1; i++ {
			sel := fmt.Sprintf("main > :nth-child(%d)", i)
			if i == n+1 {
				sel = "body > footer"
			}
			var png []byte
			png, err = chromedp.Run(ctx, chromedp.Screenshot(sel))
			if err == nil {
				name := filepath.Join(out, fmt.Sprintf("%s%s-%d-%02d.png", l, strings.ReplaceAll(*path, "/", "_"), *width, i))
				err = os.WriteFile(name, png, 0o600)
				fmt.Println(name)
			}
		}
		cancelTab()
		if err != nil {
			fail(err)
		}
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "shots:", err)
	os.Exit(1)
}
