package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/omarchaouki/zaytona-go/internal/media"
)

// imagesCmd re-encodes the source photos of assets/source (JPEG, PNG or WebP,
// named like "olives-golden-hour.jpg") into web/static/images as AVIF and WebP
// at 480/960/1600 px, and updates internal/view/images.json. Run from the
// repository root, then rebuild the assets.
func imagesCmd(args []string) error {
	src, out, manifest := "assets/source", "web/static/images", "internal/view/images.json"
	if len(args) > 0 {
		src = args[0]
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("source photos: %w (put them in %s)", err, src)
	}
	type entry struct {
		Widths []int `json:"widths"`
		Width  int   `json:"width"`
		Height int   `json:"height"`
	}
	current := map[string]entry{}
	if raw, err := os.ReadFile(manifest); err == nil {
		_ = json.Unmarshal(raw, &current)
	}
	nameRe := regexp.MustCompile(`^[a-z0-9-]{1,60}$`)
	done := 0
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		if e.IsDir() || (ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp") {
			continue
		}
		if !nameRe.MatchString(name) {
			return fmt.Errorf("%s: use lowercase letters, digits and dashes in photo names", e.Name())
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name())) //nolint:gosec // G703: operator-chosen folder of the CLI, names from ReadDir
		if err != nil {
			return err
		}
		img, err := media.Decode(data, 0)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
		widths, err := media.EncodeStatic(img, out, name)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
		b := img.Bounds()
		current[name] = entry{Widths: widths, Width: b.Dx(), Height: b.Dy()}
		fmt.Printf("  %s: %v\n", name, widths)
		done++
	}
	if done == 0 {
		return errors.New("no JPEG, PNG or WebP photo found in " + src)
	}
	names := make([]string, 0, len(current))
	for n := range current {
		names = append(names, n)
	}
	sort.Strings(names)
	ordered := make([]string, 0, len(names))
	for _, n := range names {
		b, _ := json.Marshal(current[n])
		key, _ := json.Marshal(n)
		ordered = append(ordered, "  "+string(key)+": "+string(b))
	}
	if err := os.WriteFile(manifest, []byte("{\n"+strings.Join(ordered, ",\n")+"\n}\n"), 0o644); err != nil { //nolint:gosec // G306: source file, not a secret
		return err
	}
	fmt.Printf("%d photo(s) encoded; %s updated. Run task build (or task assets) next.\n", done, manifest)
	return nil
}
