// Command fetchtools downloads the pinned Tailwind CSS standalone CLI into
// tools/bin and verifies its SHA-256. A mismatch deletes the file and fails.
// Hashes come from the release's sha256sums.txt (checked by hand once).
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// TailwindVersion is the pinned Tailwind CSS release (same as the reference app).
const TailwindVersion = "v4.3.3"

var tailwindSHA256 = map[string]string{
	"windows/amd64": "e0e260ce048014e9268f6237ff18f8ccf02cef521cbd0ae04e82c2cdf7aa3955",
	"linux/amd64":   "dc61b3ac6b8c9ca874c0cc4c57b2409791a64c5540404ca5f5367360babc313a",
	"linux/arm64":   "55fd0b241214eff3de1e8ee4f22796662f2d2e7a49bcfca7477cfd0bac398195",
	"darwin/arm64":  "cdf646702987a743464dff4d9c60fd4480d1c1e73dd819a9a67f1078815dce9d",
	"darwin/amd64":  "7922e0953f2110c05976e3bf58f14e643d90427575e766b7d433f5f80cbee7e1",
}

var assetName = map[string]string{
	"windows/amd64": "tailwindcss-windows-x64.exe",
	"linux/amd64":   "tailwindcss-linux-x64",
	"linux/arm64":   "tailwindcss-linux-arm64",
	"darwin/arm64":  "tailwindcss-macos-arm64",
	"darwin/amd64":  "tailwindcss-macos-x64",
}

// TailwindPath is where the binary lives (shared with tools/assetbuild).
func TailwindPath() string {
	name := "tailwindcss"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join("tools", "bin", name)
}

func main() {
	platform := runtime.GOOS + "/" + runtime.GOARCH
	want, ok := tailwindSHA256[platform]
	if !ok {
		fail(fmt.Errorf("no pinned Tailwind binary for %s", platform))
	}
	dest := TailwindPath()
	if sum, err := fileSHA256(dest); err == nil && sum == want {
		fmt.Println("tailwindcss", TailwindVersion, "ok")
		return
	}
	url := "https://github.com/tailwindlabs/tailwindcss/releases/download/" + TailwindVersion + "/" + assetName[platform]
	fmt.Println("downloading", url)
	if err := download(url, dest); err != nil {
		fail(err)
	}
	sum, err := fileSHA256(dest)
	if err != nil {
		fail(err)
	}
	if sum != want {
		_ = os.Remove(dest)
		fail(fmt.Errorf("checksum mismatch for %s: got %s want %s (file deleted)", dest, sum, want))
	}
	fmt.Println("tailwindcss", TailwindVersion, "installed and verified")
}

func download(url, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o750) // #nosec G302 G304 -- executable in tools/bin
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 300<<20)); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path) // #nosec G304 -- fixed path
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "fetchtools:", err)
	os.Exit(1)
}
