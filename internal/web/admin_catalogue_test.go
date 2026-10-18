package web_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func testPhoto(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 640, 400))
	for y := range 400 {
		for x := range 640 {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 90, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// postMultipart sends a form with one file (like a browser upload).
func (b *browser) postMultipart(path string, fields map[string]string, fileField string, file []byte) *httptest.ResponseRecorder {
	b.e.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if _, ok := fields["csrf"]; !ok {
		_ = mw.WriteField("csrf", b.csrf)
	}
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	if file != nil {
		w, _ := mw.CreateFormFile(fileField, "photo.jpg")
		_, _ = w.Write(file)
	}
	_ = mw.Close()
	req := httptest.NewRequestWithContext(b.e.t.Context(), http.MethodPost, path, &body)
	req.RemoteAddr = b.ip
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if b.cookie != nil {
		req.AddCookie(b.cookie)
	}
	rec := httptest.NewRecorder()
	b.e.h.ServeHTTP(rec, req)
	if m := csrfRe.FindStringSubmatch(rec.Body.String()); m != nil {
		b.csrf = m[1]
	}
	return rec
}

func (e *adminEnv) scalar(query string, args ...any) string {
	e.t.Helper()
	var s *string
	if err := e.s.Pool.QueryRow(context.Background(), query, args...).Scan(&s); err != nil {
		e.t.Fatal(err)
	}
	if s == nil {
		return ""
	}
	return *s
}

func TestAdminOffers(t *testing.T) {
	e := newAdminEnv(t)
	b := e.browser("198.51.100.30:1000")
	b.signIn(ownerEmail, ownerPassword)
	b.get("/admin/offers")

	// Price per litre: automatic offers are re-priced, the demo flag goes.
	rec := b.post("/admin/offers/pricing", url.Values{"pricePerLiter": {"95,5"}, "customEnabled": {"1"}, "customMin": {"5"}, "customMax": {"200"}})
	if rec.Code != http.StatusSeeOther || !strings.Contains(location(rec), "done=pricing") {
		t.Fatalf("pricing: %d %s", rec.Code, rec.Body.String())
	}
	if got := e.scalar("select price_mad::text from offers where slug = 'huile-olive-30l'"); got != "2865.00" {
		t.Fatalf("30 L repriced to %s", got)
	}
	if e.scalar("select (value->>'isDemo') from settings where key = 'pricing'") != "false" {
		t.Fatal("demo flag kept")
	}
	rec = b.post("/admin/offers/pricing", url.Values{"pricePerLiter": {"95"}, "customMin": {"50"}, "customMax": {"10"}})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `value="50"`) {
		t.Fatalf("min > max: %d", rec.Code)
	}

	// New offer with a photo: re-encoded, served from /uploads, shown on the site.
	b.get("/admin/offers")
	rec = b.postMultipart("/admin/offers", map[string]string{"liters": "10", "sortOrder": "5", "nameAr": "زيت 10", "nameFr": "Huile 10", "mode": "auto", "isActive": "1"}, "photo", testPhoto(t))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	if e.scalar("select price_mad::text from offers where name_fr = 'Huile 10'") != "955.00" {
		t.Fatal("auto price")
	}
	imageID := e.scalar("select image_id from offers where name_fr = 'Huile 10'")
	if imageID == "" {
		t.Fatal("no image")
	}
	for _, w := range []string{"480", "640"} {
		for _, ext := range []string{"avif", "webp", "jpg"} {
			if _, err := os.Stat(filepath.Join(uploadDir, imageID+"-"+w+"."+ext)); err != nil {
				t.Fatalf("variant %s.%s missing", w, ext)
			}
		}
	}
	if rec := get(t, e.h, "/uploads/"+imageID+"-480.webp"); rec.Code != 200 || rec.Header().Get("Content-Type") != "image/webp" {
		t.Fatalf("public photo: %d", rec.Code)
	}
	landing := get(t, e.h, "/fr").Body.String()
	if !strings.Contains(landing, "/uploads/"+imageID) || !strings.Contains(landing, "Huile 10") {
		t.Fatal("offer photo not on the site")
	}

	// A file that is not an image is refused, values kept.
	rec = b.postMultipart("/admin/offers", map[string]string{"liters": "20", "nameAr": "زيت", "nameFr": "Huile 20", "mode": "auto"}, "photo", []byte("<?php system($_GET['x']); ?>"))
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Fichier invalide") || !strings.Contains(rec.Body.String(), `value="Huile 20"`) {
		t.Fatalf("bad upload: %d", rec.Code)
	}

	// Edit: fixed price, remove the photo (files deleted).
	id := e.scalar("select id::text from offers where name_fr = 'Huile 10'")
	rec = b.postMultipart("/admin/offers/"+id, map[string]string{"liters": "10", "nameAr": "زيت 10", "nameFr": "Huile 10", "mode": "fixed", "price": "999", "removePhoto": "1"}, "", nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	if e.scalar("select price_mad::text from offers where id = $1", id) != "999.00" || e.scalar("select is_active::text from offers where id = $1", id) != "false" {
		t.Fatal("fixed price / hidden not saved")
	}
	if _, err := os.Stat(filepath.Join(uploadDir, imageID+"-480.jpg")); !os.IsNotExist(err) {
		t.Fatal("removed photo still on disk")
	}

	// Delete asks first without JS.
	rec = b.post("/admin/offers/"+id+"/delete", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `name="confirm" value="1"`) {
		t.Fatalf("confirm page: %d", rec.Code)
	}
	if rec := b.post("/admin/offers/"+id+"/delete", url.Values{"confirm": {"1"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("delete: %d", rec.Code)
	}
	if e.scalar("select count(*)::text from offers where id = $1", id) != "0" {
		t.Fatal("not deleted")
	}
	for _, a := range []string{"settings.pricing", "offer.create", "offer.update", "offer.delete"} {
		if e.auditCount(a) == 0 {
			t.Errorf("%s not audited", a)
		}
	}

	// Staff: read-only page, every change refused.
	s := e.browser("198.51.100.31:1000")
	s.signIn(staffEmail, staffPassword)
	page := s.get("/admin/offers").Body.String()
	if !strings.Contains(page, "<fieldset disabled") || strings.Contains(page, `action="/admin/offers"`) {
		t.Fatal("staff sees an editable page")
	}
	if rec := s.post("/admin/offers/pricing", url.Values{"pricePerLiter": {"1"}, "customMin": {"1"}, "customMax": {"2"}}); rec.Code != http.StatusForbidden {
		t.Fatalf("staff pricing: %d", rec.Code)
	}
}

func TestAdminSettings(t *testing.T) {
	e := newAdminEnv(t)
	b := e.browser("198.51.100.32:1000")
	b.signIn(ownerEmail, ownerPassword)
	b.get("/admin/settings")
	contact := url.Values{"whatsapp": {"06 12 34 56 78"}, "phone": {"0524 11 22 33"}, "email": {"contact@huilerie.example"}, "mapEmbed": {"https://www.google.com/maps/embed?pb=1"}}

	// Changing the numbers needs a password check within 10 minutes.
	if _, err := e.s.Pool.Exec(context.Background(), "update sessions set recent_auth_at = now() - interval '11 minutes'"); err != nil {
		t.Fatal(err)
	}
	rec := b.post("/admin/settings/contact", contact)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `action="/admin/security/reauth"`) {
		t.Fatalf("contact without recent auth: %d", rec.Code)
	}
	rec = b.post("/admin/security/reauth", url.Values{"password": {ownerPassword}, "next": {"/admin/settings#contact"}})
	if location(rec) != "/admin/settings#contact" {
		t.Fatalf("reauth next: %s", location(rec))
	}
	if rec := b.post("/admin/settings/contact", contact); rec.Code != http.StatusSeeOther {
		t.Fatalf("contact: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(get(t, e.h, "/fr").Body.String(), "wa.me/212612345678") {
		t.Fatal("new WhatsApp number not on the site")
	}
	meta := e.scalar("select meta::text from audit_logs where action = 'settings.whatsapp'")
	if meta == "" || strings.Contains(meta, "612345678") {
		t.Fatalf("whatsapp audit not masked: %s", meta)
	}

	bad := map[string]url.Values{
		"contact":       {"whatsapp": {"123"}, "mapUrl": {"javascript:alert(1)"}},
		"notifications": {"webhook": {"https://user:secret@hooks.example/x"}, "email": {"not an email"}},
		"marketing":     {"pixelEnabled": {"1"}, "pixelId": {""}},
		"delivery":      {"mode": {"free_above"}, "fee": {"30"}},
		"identity":      {"nameAr": {""}, "nameFr": {"X"}},
		"templates":     {"orderAr": {""}},
	}
	for group, v := range bad {
		if rec := b.post("/admin/settings/"+group, v); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s accepted bad input: %d", group, rec.Code)
		}
	}
	if rec := b.post("/admin/settings/delivery", url.Values{"mode": {"free_above"}, "fee": {"30"}, "freeAbove": {"500"}, "timeFr": {"24 à 72 h"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("delivery: %d", rec.Code)
	}
	if !strings.Contains(get(t, e.h, "/fr").Body.String(), "offerte dès 500 MAD") {
		t.Fatal("delivery rule not on the site")
	}
	if rec := b.post("/admin/settings/marketing", url.Values{"pixelEnabled": {"1"}, "pixelId": {"123456789012345"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("marketing: %d", rec.Code)
	}
	if !strings.Contains(get(t, e.h, "/fr").Body.String(), `data-pixel-id="123456789012345"`) {
		t.Fatal("pixel not on")
	}
	if rec := b.post("/admin/settings/nope", url.Values{}); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown group: %d", rec.Code)
	}

	s := e.browser("198.51.100.33:1000")
	s.signIn(staffEmail, staffPassword)
	if rec := s.get("/admin/settings"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "<fieldset disabled") {
		t.Fatal("staff settings page")
	}
	if rec := s.post("/admin/settings/toggles", url.Values{}); rec.Code != http.StatusForbidden {
		t.Fatalf("staff settings change: %d", rec.Code)
	}
}

func TestAdminCertificateScanVisibility(t *testing.T) {
	e := newAdminEnv(t)
	b := e.browser("198.51.100.34:1000")
	b.signIn(ownerEmail, ownerPassword)
	b.get("/admin/certificates")
	rec := b.postMultipart("/admin/certificates", map[string]string{"type": "onssa", "number": "ONSSA-123", "issued": "2026-01-10"}, "scan", testPhoto(t))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	scan := e.scalar("select scan_id from certifications where number = 'ONSSA-123'")
	path := "/uploads/" + scan + "-480.webp"
	if rec := get(t, e.h, path); rec.Code != http.StatusNotFound {
		t.Fatalf("private scan served to the public: %d", rec.Code)
	}
	if rec := b.get(path); rec.Code != 200 || rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("admin view of a private scan: %d %v", rec.Code, rec.Header())
	}
	id := e.scalar("select id::text from certifications where number = 'ONSSA-123'")
	b.get("/admin/certificates")
	if rec := b.postMultipart("/admin/certificates/"+id, map[string]string{"type": "onssa", "number": "ONSSA-123", "isPublic": "1"}, "", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("publish: %d", rec.Code)
	}
	if rec := get(t, e.h, path); rec.Code != 200 {
		t.Fatalf("public scan: %d", rec.Code)
	}
	if !strings.Contains(get(t, e.h, "/fr").Body.String(), "ONSSA-123") {
		t.Fatal("ONSSA number not on the site")
	}
	if rec := b.post("/admin/certificates/"+id+"/delete", url.Values{"confirm": {"1"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("delete: %d", rec.Code)
	}
	if rec := get(t, e.h, path); rec.Code != http.StatusNotFound {
		t.Fatal("scan still served after delete")
	}
}

func TestAdminFAQContentAndGallery(t *testing.T) {
	e := newAdminEnv(t)
	b := e.browser("198.51.100.35:1000")
	b.signIn(ownerEmail, ownerPassword)
	b.get("/admin/faq")
	rec := b.post("/admin/faq", url.Values{"questionAr": {"سؤال؟"}, "questionFr": {"Question test ?"}, "answerAr": {"جواب"}, "answerFr": {"Réponse"}, "isActive": {"1"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("faq create: %d", rec.Code)
	}
	id := e.scalar("select id::text from faqs where question_fr = 'Question test ?'")
	before := e.scalar("select sort_order::text from faqs where id = $1", id)
	b.post("/admin/faq/"+id+"/move", url.Values{"dir": {"up"}})
	if after := e.scalar("select sort_order::text from faqs where id = $1", id); len(after) > len(before) || (len(after) == len(before) && after >= before) {
		t.Fatalf("not moved up: %s -> %s", before, after)
	}
	if !strings.Contains(get(t, e.h, "/fr").Body.String(), "Question test ?") {
		t.Fatal("question not on the site")
	}

	b.get("/admin/content")
	if rec := b.post("/admin/content/hero", url.Values{"title_fr": {"Notre *huile* du jour"}, "title_ar": {"زيتنا"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("hero: %d", rec.Code)
	}
	if !strings.Contains(get(t, e.h, "/fr").Body.String(), "du jour") {
		t.Fatal("hero title not on the site")
	}
	rec = b.post("/admin/content/page_privacy", url.Values{
		"title_ar": {"الخصوصية"}, "title_fr": {"Confidentialité"},
		"html_ar": {"<p>نص</p>"}, "html_fr": {`<p onclick="x()">Texte</p><script>alert(1)</script>`},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("privacy: %d", rec.Code)
	}
	if stored := e.scalar("select value_fr->>'html' from content_blocks where key = 'page_privacy'"); strings.Contains(stored, "script") || strings.Contains(stored, "onclick") {
		t.Fatalf("unsanitized HTML stored: %s", stored)
	}
	if rec := b.post("/admin/content/nope", url.Values{}); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown block: %d", rec.Code)
	}

	rec = b.postMultipart("/admin/gallery", map[string]string{"captionFr": "Récolte"}, "photo", testPhoto(t))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("gallery add: %d %s", rec.Code, rec.Body.String())
	}
	gid := e.scalar("select id::text from gallery_images where caption_fr = 'Récolte'")
	if rec := b.post("/admin/gallery/"+gid+"/delete", url.Values{"confirm": {"1"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("gallery delete: %d", rec.Code)
	}
	if e.scalar("select count(*)::text from media where kind = 'gallery'") != "0" {
		t.Fatal("gallery media row kept")
	}
}

var tempPwRe = regexp.MustCompile(`<code dir="ltr"[^>]*>([a-z2-9-]{23})</code>`)

func TestAdminAdminsAndAudit(t *testing.T) {
	e := newAdminEnv(t)
	b := e.browser("198.51.100.36:1000")
	b.signIn(ownerEmail, ownerPassword)
	b.get("/admin/admins")

	rec := b.post("/admin/admins", url.Values{"email": {"nouveau@huilerie.example"}, "role": {"staff"}})
	m := tempPwRe.FindStringSubmatch(rec.Body.String())
	if rec.Code != 200 || m == nil {
		t.Fatalf("add: %d", rec.Code)
	}
	if rec := b.post("/admin/admins", url.Values{"email": {"nouveau@huilerie.example"}, "role": {"staff"}}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("duplicate email: %d", rec.Code)
	}
	n := e.browser("198.51.100.37:1000")
	if rec := n.login("nouveau@huilerie.example", m[1]); location(rec) != "/admin/password" {
		t.Fatalf("temp password login: %s", location(rec))
	}

	newID := e.scalar("select id::text from admins where email = 'nouveau@huilerie.example'")
	if rec := b.post("/admin/admins/"+e.owner.ID.String()+"/disable", url.Values{"confirm": {"1"}}); rec.Code != http.StatusConflict {
		t.Fatalf("self disable: %d", rec.Code)
	}
	if rec := b.post("/admin/admins/"+e.owner.ID.String()+"/role", url.Values{"role": {"staff"}}); rec.Code != http.StatusConflict {
		t.Fatalf("self demote: %d", rec.Code)
	}
	if rec := b.post("/admin/admins/"+newID+"/disable", url.Values{"confirm": {"1"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("disable: %d", rec.Code)
	}
	if rec := n.get("/admin/password"); rec.Code != http.StatusSeeOther {
		t.Fatal("disabled admin keeps a session")
	}
	if rec := n.login("nouveau@huilerie.example", m[1]); rec.Code != http.StatusUnauthorized {
		t.Fatalf("disabled login: %d", rec.Code)
	}

	// Promote, then the store refuses to leave no active owner.
	b.post("/admin/admins/"+newID+"/enable", nil)
	b.post("/admin/admins/"+newID+"/role", url.Values{"role": {"owner"}})
	if e.scalar("select role::text from admins where id = $1", newID) != "owner" {
		t.Fatal("role not changed")
	}
	if rec := b.post("/admin/admins/"+newID+"/reset", nil); rec.Code != 200 || tempPwRe.FindStringSubmatch(rec.Body.String()) == nil {
		t.Fatalf("reset: %d", rec.Code)
	}

	// Without a recent password check nothing changes.
	if _, err := e.s.Pool.Exec(context.Background(), "update sessions set recent_auth_at = now() - interval '11 minutes'"); err != nil {
		t.Fatal(err)
	}
	if rec := b.post("/admin/admins/"+newID+"/role", url.Values{"role": {"staff"}}); location(rec) != "/admin/admins" {
		t.Fatalf("stale auth: %d %s", rec.Code, location(rec))
	}
	if e.scalar("select role::text from admins where id = $1", newID) != "owner" {
		t.Fatal("changed without recent auth")
	}

	// Staff never reaches these pages.
	s := e.browser("198.51.100.38:1000")
	s.signIn(staffEmail, staffPassword)
	for _, p := range []string{"/admin/admins", "/admin/audit"} {
		if rec := s.get(p); rec.Code != http.StatusForbidden {
			t.Errorf("staff %s: %d", p, rec.Code)
		}
	}
	if strings.Contains(s.get("/admin").Body.String(), `href="/admin/audit"`) {
		t.Error("staff sees the audit link")
	}

	audit := b.get("/admin/audit?action=admin.create").Body.String()
	if !strings.Contains(audit, "admin.create") || strings.Contains(audit, "admin.disable</span>") {
		t.Fatal("audit filter")
	}
	for _, a := range []string{"admin.create", "admin.disable", "admin.enable", "admin.role", "admin.password_reset"} {
		if e.auditCount(a) == 0 {
			t.Errorf("%s not audited", a)
		}
	}
}
