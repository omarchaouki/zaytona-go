package domain

import "testing"

func TestCSVCell(t *testing.T) {
	for in, want := range map[string]string{
		"=1+1": "'=1+1", "+212612345678": "'+212612345678", "-5": "'-5", "@x": "'@x",
		"\tx": "'\tx", "\rx": "'\rx", "Rabat": "Rabat", "": "",
	} {
		if got := CSVCell(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func FuzzCSVEscape(f *testing.F) {
	for _, s := range []string{"=cmd", "+1", "-1", "@a", "\tx", "plain", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out := CSVCell(s)
		if out != "" && (out[0] == '=' || out[0] == '+' || out[0] == '-' || out[0] == '@' || out[0] == '\t' || out[0] == '\r') {
			t.Fatalf("formula start kept: %q", out)
		}
	})
}
