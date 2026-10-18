package view

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

// images.json lists the responsive photo set in web/static/images (copied from
// the reference build): name -> widths encoded as AVIF and WebP, intrinsic size.
//
//go:embed images.json
var imagesJSON []byte

type imageEntry struct {
	Widths []int `json:"widths"`
	Width  int   `json:"width"`
	Height int   `json:"height"`
}

var staticImages = func() map[string]imageEntry {
	m := map[string]imageEntry{}
	if err := json.Unmarshal(imagesJSON, &m); err != nil {
		panic(fmt.Errorf("images.json: %w", err))
	}
	return m
}()

// Image is a responsive image ready for <picture>.
type Image struct {
	AVIFSrcSet string
	WebPSrcSet string
	Fallback   string
	Width      int
	Height     int
}

// Valid reports whether the image resolved to something.
func (i Image) Valid() bool { return i.Fallback != "" }

// StaticImage resolves a photo of web/static/images through the asset manifest
// (hashed URLs). Unknown names give an invalid Image.
func StaticImage(asset func(string) string, name string) Image {
	e, ok := staticImages[name]
	if !ok || len(e.Widths) == 0 {
		return Image{}
	}
	set := func(ext string) string {
		parts := make([]string, len(e.Widths))
		for i, w := range e.Widths {
			parts[i] = fmt.Sprintf("%s %dw", asset(fmt.Sprintf("images/%s-%d.%s", name, w, ext)), w)
		}
		return strings.Join(parts, ", ")
	}
	last := e.Widths[len(e.Widths)-1]
	return Image{
		AVIFSrcSet: set("avif"),
		WebPSrcSet: set("webp"),
		Fallback:   asset(fmt.Sprintf("images/%s-%d.webp", name, last)),
		Width:      e.Width,
		Height:     e.Height,
	}
}

// MediaImage resolves an uploaded image (media table): /uploads/<id>-<w>.<ext>.
func MediaImage(id string, widths []int, width, height int) Image {
	if id == "" || len(widths) == 0 {
		return Image{}
	}
	set := func(ext string) string {
		parts := make([]string, len(widths))
		for i, w := range widths {
			parts[i] = fmt.Sprintf("/uploads/%s-%d.%s %dw", id, w, ext, w)
		}
		return strings.Join(parts, ", ")
	}
	return Image{
		AVIFSrcSet: set("avif"),
		WebPSrcSet: set("webp"),
		Fallback:   fmt.Sprintf("/uploads/%s-%d.jpg", id, widths[len(widths)-1]),
		Width:      width,
		Height:     height,
	}
}
