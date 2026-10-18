package store

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/domain"
)

// ContentBlock is one content_blocks row: a JSON object per language.
type ContentBlock struct {
	Ar, Fr json.RawMessage
}

// SiteData is everything the public pages read. It is cached in memory and
// rebuilt after Invalidate (admin saves) or when the TTL runs out.
type SiteData struct {
	Settings       domain.Settings
	Content        map[string]ContentBlock
	Offers         []dbq.ListActiveOffersWithImageRow
	Certifications []dbq.ListPublicCertificationsWithScanRow
	FAQs           []dbq.Faq
	Gallery        []dbq.ListGalleryImagesRow
	// RealReviews counts approved, non-example reviews only (hero, reasons, JSON-LD).
	RealReviews dbq.ReviewSummaryRow
	DemoReviews int64 // approved example reviews still listed
	// TopReviews are up to 5 real published reviews for structured data.
	TopReviews      []dbq.ListStructuredDataReviewsRow
	DeliveredOrders int64 // only counted while the "counters" toggle is on
	LabReports      int64
	LoadedAt        time.Time
}

// ContentFor decodes a content block in one language into v. Missing or
// malformed blocks leave v untouched (the page falls back to translations).
func (d *SiteData) ContentFor(key string, ar bool, v any) {
	b, ok := d.Content[key]
	if !ok {
		return
	}
	raw := b.Fr
	if ar {
		raw = b.Ar
	}
	_ = json.Unmarshal(raw, v)
}

// PublicONSSA is the public ONSSA authorisation with a number, if any.
func (d *SiteData) PublicONSSA() *dbq.ListPublicCertificationsWithScanRow {
	for i := range d.Certifications {
		c := &d.Certifications[i]
		if c.Type == dbq.CertificationTypeOnssa && c.Number != "" {
			return c
		}
	}
	return nil
}

// SiteCache keeps SiteData in memory. Admin saves call Invalidate synchronously,
// so a changed WhatsApp number is never served stale; the TTL is a safety net.
type SiteCache struct {
	store *Store
	ttl   time.Duration

	mu   sync.Mutex
	data *SiteData
}

// NewSiteCache creates a cache with the given TTL.
func NewSiteCache(s *Store, ttl time.Duration) *SiteCache {
	return &SiteCache{store: s, ttl: ttl}
}

// Get returns cached data, reloading it when missing or expired.
func (c *SiteCache) Get(ctx context.Context) (*SiteData, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.data != nil && time.Since(c.data.LoadedAt) < c.ttl {
		return c.data, nil
	}
	d, err := c.store.loadSiteData(ctx)
	if err != nil {
		if c.data != nil {
			// Serve the last good copy rather than failing the page.
			return c.data, nil
		}
		return nil, err
	}
	c.data = d
	return d, nil
}

// Invalidate drops the cached copy; the next Get reloads.
func (c *SiteCache) Invalidate() {
	c.mu.Lock()
	c.data = nil
	c.mu.Unlock()
}

func (s *Store) loadSiteData(ctx context.Context) (*SiteData, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	q := s.Q

	rows, err := q.ListSettings(ctx)
	if err != nil {
		return nil, err
	}
	raw := make(map[string][]byte, len(rows))
	for _, r := range rows {
		raw[r.Key] = r.Value
	}
	d := &SiteData{Settings: domain.SettingsFromRows(raw), Content: map[string]ContentBlock{}, LoadedAt: time.Now()}

	blocks, err := q.ListContentBlocks(ctx)
	if err != nil {
		return nil, err
	}
	for _, b := range blocks {
		d.Content[b.Key] = ContentBlock{Ar: b.ValueAr, Fr: b.ValueFr}
	}
	if d.Offers, err = q.ListActiveOffersWithImage(ctx); err != nil {
		return nil, err
	}
	if d.Certifications, err = q.ListPublicCertificationsWithScan(ctx); err != nil {
		return nil, err
	}
	if d.FAQs, err = q.ListActiveFaqs(ctx); err != nil {
		return nil, err
	}
	if d.Settings.Toggles.ShowGallery {
		if d.Gallery, err = q.ListGalleryImages(ctx); err != nil {
			return nil, err
		}
	}
	if d.RealReviews, err = q.ReviewSummary(ctx); err != nil {
		return nil, err
	}
	if d.DemoReviews, err = q.CountApprovedDemoReviews(ctx); err != nil {
		return nil, err
	}
	if d.TopReviews, err = q.ListStructuredDataReviews(ctx); err != nil {
		return nil, err
	}
	if d.Settings.Toggles.ShowCounters {
		if d.DeliveredOrders, err = q.CountDeliveredOrders(ctx); err != nil {
			return nil, err
		}
	}
	if d.LabReports, err = q.CountPublicLabReports(ctx); err != nil {
		return nil, err
	}
	return d, nil
}
