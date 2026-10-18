package view

import (
	"context"
	"html"
	"io"
	"regexp"
	"strings"

	"github.com/a-h/templ"
	"github.com/microcosm-cc/bluemonday"
)

// This is the only file allowed to call templ.Raw (lint rule + guard test):
// everything passed to it comes out of sanitizeRichText.

var richTextPolicy = func() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowElements("p", "br", "strong", "em", "b", "i", "ul", "ol", "li", "h2", "h3", "blockquote")
	p.AllowAttrs("href").OnElements("a")
	p.AllowURLSchemes("http", "https", "mailto", "tel")
	p.RequireParseableURLs(true)
	p.RequireNoReferrerOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)
	return p
}()

var (
	looksLikeHTML = regexp.MustCompile(`(?i)<[a-z!/]`)
	blankLines    = regexp.MustCompile(`\n\s*\n`)
)

// sanitizeRichText turns admin-authored text into safe HTML (same rules as the
// reference): plain text becomes paragraphs (blank-line separated, single
// newlines as <br>); HTML is filtered to a small allow-list.
func sanitizeRichText(input string) string {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	if !looksLikeHTML.MatchString(input) {
		var b strings.Builder
		for _, block := range blankLines.Split(strings.TrimSpace(input), -1) {
			block = strings.TrimSpace(block)
			if block == "" {
				continue
			}
			b.WriteString("<p>")
			b.WriteString(strings.ReplaceAll(html.EscapeString(block), "\n", "<br>"))
			b.WriteString("</p>")
		}
		return b.String()
	}
	return richTextPolicy.Sanitize(input)
}

// RichText renders admin-authored rich text after sanitizing it.
func RichText(input string) templ.Component {
	return templ.Raw(sanitizeRichText(input))
}

// JSONLD renders a JSON-LD script with the request nonce. payload must come from
// encoding/json with <, >, & and U+2028/U+2029 escaped (see public.jsonLD).
func JSONLD(nonce, payload string) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, `<script type="application/ld+json" nonce="`+html.EscapeString(nonce)+`">`+payload+`</script>`)
		return err
	})
}

// CleanRichText filters admin HTML through the same allow-list before it is
// stored (it is filtered again on render). Plain text is kept as typed.
func CleanRichText(input string) string {
	if !looksLikeHTML.MatchString(input) {
		return input
	}
	return richTextPolicy.Sanitize(input)
}
