// Command assetbuild builds web/static into web/static/dist:
// Tailwind CSS (standalone CLI from tools/fetchtools), JS bundles (esbuild Go
// API), fonts/images/vendor copies, content-hashed names, .gz precompression
// and manifest.json (logical name -> hashed name). -watch rebuilds on change.
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/evanw/esbuild/pkg/api"
)

const (
	srcDir  = "web/static"
	distDir = "web/static/dist"
)

// Copied as they are (then hashed). Licenses keep their names.
var copyDirs = []string{"fonts", "images", "vendor", "videos"}

var compressible = map[string]bool{".css": true, ".js": true, ".svg": true, ".json": true, ".txt": true}

func main() {
	watch := flag.Bool("watch", false, "rebuild when sources change")
	flag.Parse()

	if err := build(); err != nil {
		fail(err)
	}
	if !*watch {
		return
	}
	fmt.Println("assetbuild: watching web/static and internal/view")
	last := snapshot()
	for {
		time.Sleep(700 * time.Millisecond)
		now := snapshot()
		if now == last {
			continue
		}
		last = now
		start := time.Now()
		if err := build(); err != nil {
			fmt.Fprintln(os.Stderr, "assetbuild:", err)
			continue
		}
		fmt.Printf("assetbuild: rebuilt in %s\n", time.Since(start).Round(time.Millisecond))
	}
}

func build() error {
	if err := cleanDist(); err != nil {
		return err
	}
	manifest := map[string]string{}

	// 1. Static files: fonts, images, vendor, favicon.
	for _, dir := range copyDirs {
		root := filepath.Join(srcDir, dir)
		if _, err := os.Stat(root); err != nil {
			continue
		}
		err := filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() {
				return err
			}
			r, err := filepath.Rel(filepath.FromSlash(srcDir), p)
			if err != nil {
				return err
			}
			rel := filepath.ToSlash(r)
			data, err := os.ReadFile(p) // #nosec G304 G122 -- build tool walking our own source tree
			if err != nil {
				return err
			}
			if strings.Contains(rel, "/LICENSES/") || strings.HasSuffix(rel, "MANIFEST.txt") {
				return writeDist(rel, data) // documentation, not hashed
			}
			return emit(manifest, rel, data)
		})
		if err != nil {
			return err
		}
	}
	if data, err := os.ReadFile(filepath.Join(srcDir, "favicon.svg")); err == nil {
		if err := emit(manifest, "favicon.svg", data); err != nil {
			return err
		}
	}

	// 2. CSS through Tailwind, then font URLs rewritten to their hashed names.
	css, err := tailwind()
	if err != nil {
		return err
	}
	css = rewriteURLs(css, manifest)
	if err := emit(manifest, "css/app.css", css); err != nil {
		return err
	}

	// 3. JS entry points (web/static/js/*.js), bundled and minified.
	entries, _ := filepath.Glob(filepath.Join(srcDir, "js", "*.js"))
	if len(entries) > 0 {
		res := api.Build(api.BuildOptions{
			EntryPoints:       entries,
			Bundle:            true,
			MinifyWhitespace:  true,
			MinifyIdentifiers: true,
			MinifySyntax:      true,
			Format:            api.FormatESModule,
			Target:            api.ES2020,
			Outdir:            "out",
			Write:             false,
			LegalComments:     api.LegalCommentsEndOfFile,
			Charset:           api.CharsetUTF8,
		})
		if len(res.Errors) > 0 {
			msgs := api.FormatMessages(res.Errors, api.FormatMessagesOptions{Kind: api.ErrorMessage})
			return fmt.Errorf("esbuild:\n%s", strings.Join(msgs, "\n"))
		}
		for _, f := range res.OutputFiles {
			if err := emit(manifest, "js/"+filepath.Base(f.Path), f.Contents); err != nil {
				return err
			}
		}
	}

	raw, err := json.MarshalIndent(sorted(manifest), "", "  ")
	if err != nil {
		return err
	}
	return writeDist("manifest.json", raw)
}

// emit writes name.<hash>.ext (+ .gz) and records it in the manifest.
func emit(manifest map[string]string, logical string, data []byte) error {
	sum := sha256.Sum256(data)
	ext := path.Ext(logical)
	hashed := strings.TrimSuffix(logical, ext) + "." + hex.EncodeToString(sum[:5]) + ext
	manifest[logical] = hashed
	if err := writeDist(hashed, data); err != nil {
		return err
	}
	if compressible[ext] && len(data) > 512 {
		var buf bytes.Buffer
		zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		if err != nil {
			return err
		}
		if _, err := zw.Write(data); err != nil {
			return err
		}
		if err := zw.Close(); err != nil {
			return err
		}
		if buf.Len() < len(data) {
			return writeDist(hashed+".gz", buf.Bytes())
		}
	}
	return nil
}

func writeDist(rel string, data []byte) error {
	p := filepath.Join(distDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil { // #nosec G703 -- names come from our manifest
		return err
	}
	return os.WriteFile(p, data, 0o600) // #nosec G703 -- names come from our manifest
}

func cleanDist() error {
	entries, err := os.ReadDir(distDir)
	if err != nil {
		return os.MkdirAll(distDir, 0o750)
	}
	for _, e := range entries {
		if e.Name() == ".gitkeep" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(distDir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func tailwind() ([]byte, error) {
	bin := filepath.Join("tools", "bin", "tailwindcss")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if _, err := os.Stat(bin); err != nil {
		return nil, fmt.Errorf("%s missing: run `go run ./tools/fetchtools` (task tools)", bin)
	}
	tmp, err := os.CreateTemp("", "zaytona-*.css")
	if err != nil {
		return nil, err
	}
	out := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(out) }()
	cmd := exec.CommandContext(context.Background(), bin, "-i", filepath.Join(srcDir, "css", "app.css"), "-o", out, "--minify") // #nosec G204 -- pinned, checksum-verified binary
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("tailwind: %w\n%s", err, stderr.String())
	}
	return os.ReadFile(out) // #nosec G304 -- our temp file
}

var urlRef = regexp.MustCompile(`url\(\s*["']?/static/([^"')]+)["']?\s*\)`)

func rewriteURLs(css []byte, manifest map[string]string) []byte {
	return urlRef.ReplaceAllFunc(css, func(m []byte) []byte {
		name := string(urlRef.FindSubmatch(m)[1])
		if h, ok := manifest[name]; ok {
			return []byte(`url("/static/` + h + `")`)
		}
		fmt.Fprintf(os.Stderr, "assetbuild: warning: %s referenced in CSS but not found\n", name)
		return m
	})
}

func sorted(m map[string]string) map[string]string { return m } // encoding/json sorts map keys

// snapshot is a cheap fingerprint (path + size + mtime) of the watched sources.
func snapshot() string {
	var parts []string
	for _, root := range []string{srcDir, "internal/view"} {
		_ = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return nil //nolint:nilerr // a vanished file is just a change
			}
			if e.IsDir() && filepath.ToSlash(p) == distDir {
				return filepath.SkipDir
			}
			if e.IsDir() || strings.HasSuffix(p, "_templ.go") {
				return nil
			}
			if info, err := e.Info(); err == nil {
				parts = append(parts, fmt.Sprintf("%s:%d:%d", p, info.Size(), info.ModTime().UnixNano()))
			}
			return nil
		})
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "assetbuild:", err)
	os.Exit(1)
}
