package security

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Grep tests from SECURITY.md section 14: patterns that must never appear.
var (
	templRaw   = regexp.MustCompile(`templ\.Raw\(`)
	styleAttr  = regexp.MustCompile(`\sstyle=`)
	inlineOn   = regexp.MustCompile(`\son[a-z]+=`)
	hxOn       = regexp.MustCompile(`hx-on`)
	builtQuery = regexp.MustCompile(`\.(Exec|Query|QueryRow)\([^)]*(fmt\.Sprintf|"\s*\+|\+\s*")`)
)

func TestForbiddenPatterns(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(strings.TrimPrefix(p, root+string(filepath.Separator)))
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".data", "node_modules", "dist", "bin", "uploads":
				return filepath.SkipDir
			}
			return nil
		}
		isTempl := strings.HasSuffix(p, ".templ")
		isGo := strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_templ.go") && !strings.HasSuffix(p, "_test.go")
		if !isTempl && !isGo {
			return nil
		}
		src, err := os.ReadFile(p) // #nosec G304 G122 -- test walking our own repository
		if err != nil {
			return err
		}
		s := string(src)
		if templRaw.MatchString(s) && rel != "internal/view/safehtml.go" {
			t.Errorf("%s: templ.Raw outside internal/view/safehtml.go", rel)
		}
		if isTempl {
			for _, re := range []*regexp.Regexp{styleAttr, inlineOn, hxOn} {
				if re.MatchString(s) {
					t.Errorf("%s: forbidden attribute %q (CSP: no inline styles or handlers)", rel, re.FindString(s))
				}
			}
		}
		if isGo && builtQuery.MatchString(s) && !strings.HasPrefix(rel, "internal/db/dbtest/") {
			t.Errorf("%s: SQL built with string concatenation; use sqlc queries", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
