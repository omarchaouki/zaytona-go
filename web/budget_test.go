package web_test

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// PROMPT.md section 9.3: size budgets on the built assets (task assets/build
// first; skipped on a fresh clone without web/static/dist).
func TestAssetBudgets(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("static", "dist", "manifest.json"))
	if err != nil {
		t.Skip("assets not built (run task assets)")
	}
	var manifest map[string]string
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	for name, limit := range map[string]int{
		"js/landing.js": 25 << 10, // landing initial JS
		"css/app.css":   30 << 10, // landing CSS
		"js/admin.js":   25 << 10,
	} {
		data, err := os.ReadFile(filepath.Join("static", "dist", filepath.FromSlash(manifest[name])))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var b bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&b, gzip.BestCompression)
		_, _ = zw.Write(data)
		_ = zw.Close()
		t.Logf("%s: %d bytes gzip (budget %d)", name, b.Len(), limit)
		if b.Len() > limit {
			t.Errorf("%s is %d bytes gzip, budget %d", name, b.Len(), limit)
		}
	}
}

// Hero (LCP) image: at most 120 KB at 960 px AVIF.
func TestHeroImageBudget(t *testing.T) {
	st, err := os.Stat(filepath.Join("static", "images", "olives-golden-hour-960.avif"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() > 120<<10 {
		t.Fatalf("hero 960 AVIF is %d bytes, budget 120 KB", st.Size())
	}
}
