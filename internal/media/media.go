// Package media turns uploaded photos into the responsive set the site serves
// (SECURITY.md section 9): JPEG, PNG or WebP only (sniffed, never trusted from
// the client), dimension and pixel caps checked before decoding, EXIF
// orientation applied, resized to 480/960/1600 px and re-encoded to AVIF,
// WebP and JPEG. The original bytes are never stored; re-encoding drops EXIF
// and GPS data. Files live in Dir as <id>-<width>.<ext> under random ids.
package media

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"time"

	"github.com/gen2brain/avif"
	gwebp "github.com/gen2brain/webp"
	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// Widths are the responsive sizes generated (never upscaled).
var Widths = []int{480, 960, 1600}

// Formats are the encoded variants of every width.
var Formats = []string{"avif", "webp", "jpg"}

// Limits checked before decoding (decompression bombs).
const (
	MaxSide   = 6000
	MaxPixels = 24_000_000
)

// Errors returned to the handlers (mapped to one "invalid file" message).
var (
	ErrUnsupported = errors.New("media: not a JPEG, PNG or WebP image")
	ErrTooLarge    = errors.New("media: file or image too large")
	ErrInvalid     = errors.New("media: image cannot be read")
	ErrBusy        = errors.New("media: too many images being processed")
)

// Result describes a processed image.
type Result struct {
	ID     string
	Width  int // intrinsic size after orientation
	Height int
	Widths []int
}

// Processor encodes uploads in a bounded pool.
type Processor struct {
	Dir      string
	MaxBytes int64
	// Timeout per image (all widths and formats).
	Timeout time.Duration
	sem     chan struct{}
	queue   chan struct{}
}

// New prepares dir (created 0750), a pool of one encoder per CPU shared by
// every upload, and a queue of 4 images per CPU; beyond that uploads get ErrBusy.
func New(dir string, maxBytes int64) (*Processor, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	n := max(1, runtime.NumCPU())
	return &Processor{
		Dir: dir, MaxBytes: maxBytes, Timeout: 2 * time.Minute,
		sem: make(chan struct{}, n), queue: make(chan struct{}, n*4),
	}, nil
}

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// NewID is 128 random bits as 26 lowercase base32 characters.
func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return idEncoding.EncodeToString(b)
}

// Sniff returns "jpeg", "png" or "webp" from the magic bytes, or "".
func Sniff(b []byte) string {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "jpeg"
	case len(b) >= 8 && bytes.Equal(b[:8], []byte("\x89PNG\r\n\x1a\n")):
		return "png"
	case len(b) >= 12 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")):
		return "webp"
	}
	return ""
}

// Decode checks the format and size, then decodes with orientation applied.
func Decode(data []byte, maxBytes int64) (image.Image, error) {
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		return nil, ErrTooLarge
	}
	kind := Sniff(data)
	if kind == "" {
		return nil, ErrUnsupported
	}
	var cfg image.Config
	var err error
	switch kind {
	case "jpeg":
		cfg, err = jpeg.DecodeConfig(bytes.NewReader(data))
	case "png":
		cfg, err = png.DecodeConfig(bytes.NewReader(data))
	case "webp":
		cfg, err = webp.DecodeConfig(bytes.NewReader(data))
	}
	if err != nil {
		return nil, ErrInvalid
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxSide || cfg.Height > MaxSide || cfg.Width*cfg.Height > MaxPixels {
		return nil, ErrTooLarge
	}
	var img image.Image
	switch kind {
	case "jpeg":
		img, err = jpeg.Decode(bytes.NewReader(data))
	case "png":
		img, err = png.Decode(bytes.NewReader(data))
	case "webp":
		img, err = webp.Decode(bytes.NewReader(data))
	}
	if err != nil {
		return nil, ErrInvalid
	}
	if kind == "jpeg" {
		img = orient(img, exifOrientation(data))
	}
	return img, nil
}

// targetWidths are the Widths below the image width plus the width itself (capped at the largest).
func targetWidths(w int) []int {
	var out []int
	for _, x := range Widths {
		if x < w {
			out = append(out, x)
		}
	}
	last := min(w, Widths[len(Widths)-1])
	if len(out) == 0 || out[len(out)-1] != last {
		out = append(out, last)
	}
	return out
}

// Process decodes, resizes and writes every variant. Encodes run in parallel
// through the shared pool (one slot per CPU across all uploads). On error
// nothing stays on disk.
func (p *Processor) Process(ctx context.Context, data []byte) (Result, error) {
	select {
	case p.queue <- struct{}{}:
		defer func() { <-p.queue }()
	default:
		return Result{}, ErrBusy
	}
	img, err := Decode(data, p.MaxBytes)
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()

	b := img.Bounds()
	res := Result{ID: NewID(), Width: b.Dx(), Height: b.Dy(), Widths: targetWidths(b.Dx())}
	type job struct {
		img  *image.NRGBA
		path string
		ext  string
	}
	var jobs []job
	for _, w := range res.Widths {
		h := max(1, (b.Dy()*w+b.Dx()/2)/b.Dx())
		dst := image.NewNRGBA(image.Rect(0, 0, w, h))
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
		for _, ext := range Formats {
			jobs = append(jobs, job{dst, filepath.Join(p.Dir, fmt.Sprintf("%s-%d.%s", res.ID, w, ext)), ext})
		}
	}
	errs := make(chan error, len(jobs))
	for _, j := range jobs {
		go func() {
			select {
			case p.sem <- struct{}{}:
				defer func() { <-p.sem }()
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			}
			if err := ctx.Err(); err != nil {
				errs <- err
				return
			}
			errs <- writeVariant(j.path, j.img, j.ext)
		}()
	}
	var first error
	for range jobs {
		if err := <-errs; err != nil && first == nil {
			first = err
			cancel()
		}
	}
	if first != nil {
		for _, j := range jobs {
			_ = os.Remove(j.path)
			_ = os.Remove(j.path + ".tmp")
		}
		return Result{}, first
	}
	return res, nil
}

func writeVariant(path string, img *image.NRGBA, ext string) error {
	var buf bytes.Buffer
	var err error
	switch ext {
	case "avif":
		err = avif.Encode(&buf, img, avif.Options{Quality: 55, Speed: 10})
	case "webp":
		err = gwebp.Encode(&buf, img, gwebp.Options{Quality: 78, Method: 4})
	case "jpg":
		err = jpeg.Encode(&buf, flatten(img), &jpeg.Options{Quality: 82})
	}
	if err != nil {
		return fmt.Errorf("encode %s: %w", ext, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// flatten puts transparent pixels on white (JPEG has no alpha).
func flatten(img *image.NRGBA) image.Image {
	opaque := true
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0xFF {
			opaque = false
			break
		}
	}
	if opaque {
		return img
	}
	out := image.NewRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Over)
	return out
}

// Remove deletes every variant of id (best effort).
func (p *Processor) Remove(id string) {
	if !idRe.MatchString(id) {
		return
	}
	matches, _ := filepath.Glob(filepath.Join(p.Dir, id+"-*"))
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

var (
	idRe   = regexp.MustCompile(`^[a-z2-7]{26}$`)
	fileRe = regexp.MustCompile(`^([a-z2-7]{26})-([0-9]{1,4})\.(avif|webp|jpg)$`)
)

// ParseName validates "<id>-<width>.<ext>" and returns the id and extension.
func ParseName(name string) (id, ext string, ok bool) {
	m := fileRe.FindStringSubmatch(name)
	if m == nil {
		return "", "", false
	}
	return m[1], m[3], true
}

// ReadLimited reads at most max+1 bytes (so a too large file is detected).
func ReadLimited(r io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, ErrTooLarge
	}
	return data, nil
}

// Exists reports whether the file of a validated name exists.
func (p *Processor) Exists(name string) bool {
	if _, _, ok := ParseName(name); !ok {
		return false
	}
	_, err := os.Stat(filepath.Join(p.Dir, name))
	return err == nil
}

// Warm compiles the WASM encoders once (a few seconds), so the first upload
// is not slower than the others. Call it in a goroutine at startup.
func Warm() {
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	_ = avif.Encode(io.Discard, img, avif.Options{Quality: 55, Speed: 10})
	_ = gwebp.Encode(io.Discard, img, gwebp.Options{Quality: 78})
}

// EncodeStatic writes name-<width>.avif and .webp for every target width of
// img into dir (the site's static photo set) and returns the widths written.
func EncodeStatic(img image.Image, dir, name string) ([]int, error) {
	b := img.Bounds()
	widths := targetWidths(b.Dx())
	for _, w := range widths {
		h := max(1, (b.Dy()*w+b.Dx()/2)/b.Dx())
		dst := image.NewNRGBA(image.Rect(0, 0, w, h))
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
		for _, ext := range []string{"avif", "webp"} {
			if err := writeVariant(filepath.Join(dir, fmt.Sprintf("%s-%d.%s", name, w, ext)), dst, ext); err != nil {
				return nil, err
			}
		}
	}
	return widths, nil
}
