package web_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/omarchaouki/zaytona-go/internal/config"
	"github.com/omarchaouki/zaytona-go/internal/security"
)

type reviewEnv struct {
	*adminEnv
	ord *orderEnv
}

func newReviewEnv(t *testing.T) *reviewEnv {
	t.Helper()
	e := newAdminEnv(t)
	ord := &orderEnv{t: t, h: e.h, s: e.s, tokens: security.NewFormTokens(testConfig(t).DeriveKey(config.KeyFormToken)), ip: "203.0.113.50:1000"}
	return &reviewEnv{adminEnv: e, ord: ord}
}

// submit posts the public review form (multipart) like the page script does.
func (e *reviewEnv) submit(ip string, fields map[string]string, photo []byte) *httptest.ResponseRecorder {
	e.t.Helper()
	all := map[string]string{
		"name": "Fatima Zahra Bennani", "rating": "5", "comment": "Huile excellente, goût fruité, livraison rapide.",
		"city": "Rabat", "form_token": e.ord.tokens.IssueAt(time.Now().Add(-10 * time.Second)), "altcha": e.ord.pow(),
	}
	for k, v := range fields {
		if v == "<delete>" {
			delete(all, k)
		} else {
			all[k] = v
		}
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range all {
		_ = mw.WriteField(k, v)
	}
	if photo != nil {
		w, _ := mw.CreateFormFile("photo", "olive.jpg")
		_, _ = w.Write(photo)
	} else {
		// No photo chosen: browsers still send the part, with filename="".
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", `form-data; name="photo"; filename=""`)
		h.Set("Content-Type", "application/octet-stream")
		_, _ = mw.CreatePart(h)
	}
	_ = mw.Close()
	req := httptest.NewRequestWithContext(e.t.Context(), http.MethodPost, "/fr/reviews", &body)
	req.RemoteAddr = ip
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func TestReviewSubmissionAndModeration(t *testing.T) {
	e := newReviewEnv(t)
	// A delivered order for the phone used in the review: "verified purchase".
	o := e.placeOrder("Fatima Zahra Bennani", "+212612000077", "Rabat", 1)
	if _, err := e.s.Pool.Exec(context.Background(), "update orders set status = 'delivered' where id = $1", o.ID); err != nil {
		t.Fatal(err)
	}

	if rec := get(t, e.h, "/fr/reviews/new"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `name="rating"`) {
		t.Fatalf("form page: %d", rec.Code)
	}
	rec := e.submit("203.0.113.51:1000", map[string]string{"phone": "06 12 00 00 77"}, testPhoto(t))
	if rec.Code != http.StatusSeeOther || location(rec) != "/fr/reviews/new?sent=1" {
		t.Fatalf("submit: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(get(t, e.h, "/fr/reviews/new?sent=1").Body.String(), "Merci") {
		t.Fatal("thank-you page")
	}
	id := e.scalar("select id::text from reviews where author_name = 'Fatima Zahra Bennani'")
	if e.scalar("select status::text from reviews where id = $1", id) != "pending" || e.scalar("select order_id::text from reviews where id = $1", id) != o.ID.String() {
		t.Fatal("pending review not linked to the delivered order")
	}
	var raw []byte
	if err := e.s.Pool.QueryRow(context.Background(), "select row_to_json(r)::text::bytea from reviews r where id = $1", id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("612000077")) {
		t.Fatal("phone stored in clear")
	}
	photo := e.scalar("select photo_id from reviews where id = $1", id)
	photoURL := "/uploads/" + photo + "-480.webp"
	if rec := get(t, e.h, photoURL); rec.Code != http.StatusNotFound {
		t.Fatalf("photo of a pending review is public: %d", rec.Code)
	}
	if strings.Contains(get(t, e.h, "/fr").Body.String(), "goût fruité") {
		t.Fatal("pending review on the site")
	}

	// Staff moderates: publish, pin, reply, fix the text (original kept).
	s := e.browser("198.51.100.60:1000")
	s.signIn(staffEmail, staffPassword)
	page := s.get("/admin/reviews").Body.String()
	if !strings.Contains(page, "goût fruité") || !strings.Contains(page, "Achat vérifié · commande "+o.Reference) {
		t.Fatal("moderation list")
	}
	if strings.Contains(s.get("/admin").Body.String(), `href="/admin/reviews?status=pending"`) == false {
		t.Error("dashboard misses the pending reviews counter")
	}
	if rec := s.post("/admin/reviews/"+id+"/approve", url.Values{"return": {"/admin/reviews?status=pending"}}); rec.Code != http.StatusSeeOther || !strings.Contains(location(rec), "done=reviews") {
		t.Fatalf("approve: %d %s", rec.Code, location(rec))
	}
	s.post("/admin/reviews/"+id+"/pin", nil)
	if rec := s.post("/admin/reviews/"+id+"/edit", url.Values{"comment": {"Huile excellente, goût fruité, livraison très rapide."}, "replyFr": {"Merci Fatima !"}, "replyAr": {""}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("edit: %d", rec.Code)
	}
	if e.scalar("select original_comment from reviews where id = $1", id) != "Huile excellente, goût fruité, livraison rapide." {
		t.Fatal("original comment not kept")
	}
	if rec := s.post("/admin/reviews/"+id+"/delete", url.Values{"confirm": {"1"}}); rec.Code != http.StatusForbidden {
		t.Fatalf("staff delete: %d", rec.Code)
	}

	landing := get(t, e.h, "/fr").Body.String()
	for _, want := range []string{"Fatima Zahra B.", "livraison très rapide", "Achat vérifié", "Merci Fatima !", photoURL, `"@type":"Review"`} {
		if !strings.Contains(landing, want) {
			t.Errorf("landing misses %q", want)
		}
	}
	if strings.Contains(landing, "Bennani") {
		t.Error("full surname published")
	}
	if rec := get(t, e.h, photoURL); rec.Code != 200 {
		t.Fatalf("published photo: %d", rec.Code)
	}

	// Refuse with an internal reason: gone from the site, reason never public.
	if rec := s.post("/admin/reviews/"+id+"/reject", url.Values{"reason": {"Test interne"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("reject: %d", rec.Code)
	}
	landing = get(t, e.h, "/fr").Body.String()
	if strings.Contains(landing, "livraison très rapide") || strings.Contains(landing, "Test interne") {
		t.Fatal("refused review or reason on the site")
	}

	// The owner deletes it for good: row, photo files and media row.
	b := e.browser("198.51.100.61:1000")
	b.signIn(ownerEmail, ownerPassword)
	b.get("/admin/reviews?status=rejected")
	if rec := b.post("/admin/reviews/"+id+"/delete", nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), `name="confirm" value="1"`) {
		t.Fatalf("confirm page: %d", rec.Code)
	}
	if rec := b.post("/admin/reviews/"+id+"/delete", url.Values{"confirm": {"1"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("delete: %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(uploadDir, photo+"-480.webp")); !os.IsNotExist(err) {
		t.Fatal("photo files kept")
	}
	for _, a := range []string{"review.approve", "review.pin", "review.edit", "review.reject", "review.delete"} {
		if e.auditCount(a) == 0 {
			t.Errorf("%s not audited", a)
		}
	}
}

func TestReviewAntiSpamAndValidation(t *testing.T) {
	e := newReviewEnv(t)
	ip := "203.0.113.52:1000"
	rec := e.submit(ip, map[string]string{"rating": "0", "comment": "court", "name": "Karim"}, nil)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Choisissez une note.") || !strings.Contains(rec.Body.String(), "Écrivez au moins 10 caractères.") || !strings.Contains(rec.Body.String(), `value="Karim"`) {
		t.Fatalf("validation: %d", rec.Code)
	}
	for name, f := range map[string]map[string]string{
		"honeypot":   {"website": "http://spam.example"},
		"no pow":     {"altcha": ""},
		"fast token": {"form_token": e.ord.tokens.IssueAt(time.Now())},
	} {
		if rec := e.submit(ip, f, nil); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s accepted: %d", name, rec.Code)
		}
	}
	if rec := e.submit(ip, nil, []byte("GIF89a not a photo at all")); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "pas une image valide") {
		t.Fatalf("bad photo: %d", rec.Code)
	}
	// 3 per hour per IP (the bad photo above counted as one).
	for i := range 2 {
		if rec := e.submit(ip, nil, nil); rec.Code != http.StatusSeeOther {
			t.Fatalf("review %d: %d", i, rec.Code)
		}
	}
	if rec := e.submit(ip, nil, nil); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("4th review: %d", rec.Code)
	}
	if e.scalar("select count(*)::text from reviews where status = 'pending'") != "2" {
		t.Fatal("pending count")
	}
}

func TestReviewsSectionFiltersAndToggle(t *testing.T) {
	h, s := newServer(t) // seeded with example reviews
	landing := get(t, h, "/fr").Body.String()
	if !strings.Contains(landing, `id="reviews"`) || !strings.Contains(landing, "<span data-demo") {
		t.Fatal("section with example reviews")
	}
	if strings.Contains(landing, `"@type":"Review"`) || strings.Contains(landing, "Basé sur") {
		t.Fatal("example reviews counted in the summary or structured data")
	}
	// Real approved reviews: 2 five-star, 1 three-star.
	for i, rating := range []int{5, 5, 3} {
		if _, err := s.Pool.Exec(context.Background(), `insert into reviews (author_name, rating, comment, status, created_at)
			values ($1, $2, 'Avis réel numéro un deux trois', 'approved', now() - make_interval(mins => $3))`, "Client "+string(rune('A'+i)), rating, i); err != nil {
			t.Fatal(err)
		}
	}
	h, _ = newServerFor(t, s) // fresh site cache
	landing = get(t, h, "/fr").Body.String()
	if !strings.Contains(landing, "Basé sur 3 avis") || !strings.Contains(landing, `href="/fr?stars=5#reviews"`) {
		t.Fatal("summary and filter links")
	}
	if strings.Contains(landing, "Avis de vrais clients") {
		t.Fatal("'real customers' note while example reviews are listed")
	}
	only3 := get(t, h, "/fr?stars=3").Body.String()
	only3 = only3[strings.Index(only3, "data-review-list"):] // the list (JSON-LD names every real review)
	if !strings.Contains(only3, "Client C") || strings.Contains(only3, "Client A") {
		t.Fatal("star filter")
	}
	frag := get(t, h, "/fr/reviews?stars=5&page=1")
	if frag.Code != 200 || !strings.Contains(frag.Body.String(), "Client A") || strings.Contains(frag.Body.String(), "<html") {
		t.Fatalf("fragment: %d", frag.Code)
	}
	if rec := get(t, h, "/fr/reviews?list=1"); !strings.Contains(rec.Body.String(), "data-review-list") {
		t.Fatal("list fragment")
	}

	// Section hidden by the owner: no section, no form, no submissions.
	if _, err := s.Pool.Exec(context.Background(), `update settings set value = jsonb_set(value, '{showReviews}', 'false') where key = 'toggles'`); err != nil {
		t.Fatal(err)
	}
	h, _ = newServerFor(t, s)
	if strings.Contains(get(t, h, "/fr").Body.String(), `id="reviews"`) {
		t.Fatal("section shown while hidden")
	}
	for _, p := range []string{"/fr/reviews", "/fr/reviews/new"} {
		if rec := get(t, h, p); rec.Code != http.StatusNotFound {
			t.Errorf("%s while hidden: %d", p, rec.Code)
		}
	}
}
