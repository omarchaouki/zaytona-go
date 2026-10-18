package public

import (
	"strings"
	"testing"
)

func TestJSONLDCannotCloseTheScript(t *testing.T) {
	// Line and paragraph separators (U+2028, U+2029) end a JS string in old parsers.
	lineSep, paraSep := string(rune(0x2028)), string(rune(0x2029))
	out, err := marshalJSONLD(map[string]any{"name": "</script><script>alert(1)</script> & <!-- " + lineSep + paraSep})
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"<", ">", "&", lineSep, paraSep} {
		if strings.Contains(out, bad) {
			t.Errorf("unescaped %q in %s", bad, out)
		}
	}
	if !strings.Contains(out, "u003c/script") {
		t.Errorf("expected unicode escapes: %s", out)
	}
}

func TestSplitParagraphs(t *testing.T) {
	if s := splitParagraphs("a\n\n\nb\r\n\r\nc"); len(s) != 3 {
		t.Fatalf("paragraphs: %q", s)
	}
}
