package web_test

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVendoredFileHashes recomputes the SHA-256 of every file listed in
// static/vendor/MANIFEST.txt, and checks that every font is listed.
func TestVendoredFileHashes(t *testing.T) {
	f, err := os.Open(filepath.Join("static", "vendor", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	listed := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, " | ")
		if len(parts) != 4 {
			t.Fatalf("bad line %q", line)
		}
		path, want := parts[0], parts[3]
		data, err := os.ReadFile(filepath.Join("static", filepath.FromSlash(path)))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("%s: sha256 %s, manifest says %s", path, got, want)
		}
		listed[path] = true
	}
	fonts, _ := filepath.Glob(filepath.Join("static", "fonts", "*.woff2"))
	for _, p := range fonts {
		if rel := "fonts/" + filepath.Base(p); !listed[rel] {
			t.Errorf("%s is not in MANIFEST.txt", rel)
		}
	}
}
