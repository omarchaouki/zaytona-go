package view

import (
	"strings"
	"testing"
)

func TestSanitizeRichText(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Line one\nline two\n\n2 < 3 & 4 > 1", "<p>Line one<br>line two</p><p>2 &lt; 3 &amp; 4 &gt; 1</p>"},
		{`<p onclick="x()">Hi <script>alert(1)</script><a href="javascript:alert(1)">x</a></p>`, "<p>Hi x</p>"},
		{`<h2>T</h2><img src=x onerror=alert(1)><style>body{}</style>`, "<h2>T</h2>"},
	} {
		if got := sanitizeRichText(tc.in); got != tc.want {
			t.Errorf("%q:\n got %q\nwant %q", tc.in, got, tc.want)
		}
	}
	got := sanitizeRichText(`<p><a href="https://example.com">site</a></p>`)
	if !strings.Contains(got, `rel="nofollow noreferrer noopener"`) && !strings.Contains(got, "noreferrer") {
		t.Errorf("external link without noreferrer: %s", got)
	}
}
