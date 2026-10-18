package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 120, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// withOrientation inserts an APP1 Exif segment with the orientation tag.
func withOrientation(jpg []byte, o uint16) []byte {
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08")
	ifd := make([]byte, 2+12+4)
	binary.BigEndian.PutUint16(ifd[0:], 1)
	binary.BigEndian.PutUint16(ifd[2:], 0x0112)
	binary.BigEndian.PutUint16(ifd[4:], 3)
	binary.BigEndian.PutUint32(ifd[6:], 1)
	binary.BigEndian.PutUint16(ifd[10:], o)
	payload := append([]byte("Exif\x00\x00"), append(tiff, ifd...)...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2)) //nolint:gosec // G115: a few dozen bytes
	seg = append(seg, payload...)
	return append(append([]byte{0xFF, 0xD8}, seg...), jpg[2:]...)
}

func TestSniffAndLimits(t *testing.T) {
	jpg := testJPEG(t, 40, 20)
	if Sniff(jpg) != "jpeg" || Sniff([]byte("GIF89a......")) != "" || Sniff([]byte("<svg")) != "" {
		t.Fatal("sniff")
	}
	if _, err := Decode([]byte("<?php echo 1; ?>"), 1<<20); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("php: %v", err)
	}
	if _, err := Decode(jpg, 10); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("size cap: %v", err)
	}
	// A PNG header that claims 7000 x 7000 pixels is refused before decoding.
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewGray(image.Rect(0, 0, 1, 1)))
	bomb := b.Bytes()
	binary.BigEndian.PutUint32(bomb[16:], 7000)
	binary.BigEndian.PutUint32(bomb[20:], 7000)
	binary.BigEndian.PutUint32(bomb[29:], crc32.ChecksumIEEE(bomb[12:29]))
	if _, err := Decode(bomb, 1<<20); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("bomb: %v", err)
	}
	if _, err := Decode(jpg[:len(jpg)/2], 1<<20); !errors.Is(err, ErrInvalid) {
		t.Fatalf("truncated: %v", err)
	}
}

func TestOrientation(t *testing.T) {
	img, err := Decode(withOrientation(testJPEG(t, 40, 20), 6), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 20 || b.Dy() != 40 {
		t.Fatalf("rotated size %v", b)
	}
	if exifOrientation([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0xFF, 0xFF}) != 1 {
		t.Fatal("bad segment length must be ignored")
	}
}

func TestTargetWidths(t *testing.T) {
	for w, want := range map[int][]int{300: {300}, 480: {480}, 1200: {480, 960, 1200}, 4000: {480, 960, 1600}} {
		got := targetWidths(w)
		if len(got) != len(want) {
			t.Fatalf("%d: %v", w, got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%d: %v", w, got)
			}
		}
	}
}

func TestProcessAndServe(t *testing.T) {
	dir := t.TempDir()
	p, err := New(dir, 5<<20)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	res, err := p.Process(context.Background(), withOrientation(testJPEG(t, 1000, 600), 1))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("processed in %s", time.Since(start))
	if !idRe.MatchString(res.ID) || res.Width != 1000 || len(res.Widths) != 3 {
		t.Fatalf("result %+v", res)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(files) != 9 {
		t.Fatalf("%d files: %v", len(files), files)
	}
	// No EXIF survives in the JPEG variant.
	jpg, _ := os.ReadFile(filepath.Join(dir, res.ID+"-480.jpg")) //nolint:gosec // G304: test temp dir
	if bytes.Contains(jpg, []byte("Exif")) {
		t.Fatal("EXIF kept")
	}

	public := map[string]bool{res.ID: true}
	admin := false
	h := p.Handler(func(_ context.Context, id string) (bool, error) { return public[id], nil }, func(*http.Request) bool { return admin })
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
		return rec
	}
	rec := get("/uploads/" + res.ID + "-960.avif")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/avif" || rec.Header().Get("X-Content-Type-Options") != "nosniff" ||
		rec.Header().Get("Content-Security-Policy") != "default-src 'none'" {
		t.Fatalf("serve: %d %v", rec.Code, rec.Header())
	}
	for _, bad := range []string{"/uploads/../go.mod", "/uploads/" + res.ID + "-960.png", "/uploads/" + res.ID + ".webp", "/uploads/ABC-960.webp"} {
		if rec := get(bad); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", bad, rec.Code)
		}
	}
	public[res.ID] = false
	if rec := get("/uploads/" + res.ID + "-960.webp"); rec.Code != http.StatusNotFound {
		t.Fatalf("private image served: %d", rec.Code)
	}
	admin = true
	if rec := get("/uploads/" + res.ID + "-960.webp"); rec.Code != 200 || rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("admin view: %d %v", rec.Code, rec.Header())
	}
	p.Remove(res.ID)
	if files, _ := filepath.Glob(filepath.Join(dir, "*")); len(files) != 0 {
		t.Fatalf("not removed: %v", files)
	}
}

func FuzzDecodeUpload(f *testing.F) {
	f.Add([]byte("\xFF\xD8\xFF\xE1\x00\x10Exif\x00\x00MM\x00\x2a\x00\x00\x00\x08"))
	f.Add([]byte("\x89PNG\r\n\x1a\n"))
	f.Add([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "))
	f.Fuzz(func(t *testing.T, data []byte) {
		img, err := Decode(data, 1<<20)
		if err == nil {
			b := img.Bounds()
			if b.Dx() > MaxSide || b.Dy() > MaxSide {
				t.Fatalf("over the cap: %v", b)
			}
		}
		_ = exifOrientation(data)
	})
}

// A JPEG with a script appended (polyglot) comes out as clean re-encoded images.
func TestPolyglotUploadIsReencoded(t *testing.T) {
	dir := t.TempDir()
	p, err := New(dir, 5<<20)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("<?php system($_GET['c']); ?><script>alert(1)</script>")
	res, err := p.Process(context.Background(), append(testJPEG(t, 600, 400), payload...))
	if err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, res.ID+"-*"))
	for _, f := range files {
		data, _ := os.ReadFile(f) //nolint:gosec // G304: test temp dir
		if bytes.Contains(data, []byte("<?php")) || bytes.Contains(data, []byte("<script")) {
			t.Fatalf("%s kept the appended payload", f)
		}
	}
}
