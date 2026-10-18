package public

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/domain"
	"github.com/omarchaouki/zaytona-go/internal/httpx"
	"github.com/omarchaouki/zaytona-go/internal/i18n"
	"github.com/omarchaouki/zaytona-go/internal/media"
	"github.com/omarchaouki/zaytona-go/internal/security"
	"github.com/omarchaouki/zaytona-go/internal/store"
	"github.com/omarchaouki/zaytona-go/internal/view"
)

// Reviews page size and the submission limit (SECURITY.md section 10).
const (
	reviewsPageSize    = 6
	reviewLimit        = 3
	reviewLimitWindow  = time.Hour
	maxReviewListPages = 50
)

// reviewQuery reads ?stars= and ?rpage= (landing) or ?page= (fragment).
func reviewQuery(q url.Values, pageKey string) (stars, page int) {
	if n, err := strconv.Atoi(q.Get("stars")); err == nil && n >= 1 && n <= 5 {
		stars = n
	}
	page = 1
	if n, err := strconv.Atoi(q.Get(pageKey)); err == nil && n > 1 && n <= maxReviewListPages {
		page = n
	}
	return stars, page
}

func ratingParam(stars int) *int16 {
	if stars == 0 {
		return nil
	}
	v := int16(stars) //nolint:gosec // G115: 1..5
	return &v
}

// reviewCards loads approved reviews: rows [skip, skip+n), plus whether more exist.
func (h *Handlers) reviewCards(ctx context.Context, l i18n.Locale, stars, skip, n int) ([]view.ReviewCard, bool, error) {
	rows, err := h.Store.Q.ListPublicReviews(ctx, dbq.ListPublicReviewsParams{
		Rating: ratingParam(stars), MaxRows: int32(n + 1), Skip: int32(skip), //nolint:gosec // G115: bounded pages
	})
	if err != nil {
		return nil, false, err
	}
	more := len(rows) > n
	if more {
		rows = rows[:n]
	}
	cards := make([]view.ReviewCard, 0, len(rows))
	for _, r := range rows {
		c := view.ReviewCard{
			ID: r.ID.String(), Name: domain.DisplayName(r.AuthorName), City: deref(r.City), Comment: r.Comment,
			Lang: string(r.Language), Rating: int(r.Rating), Verified: r.Verified, Demo: r.IsDemo,
			Day: domain.FormatDayLong(r.CreatedAt.In(casablanca), string(l)),
		}
		reply := r.ReplyFr
		if l == i18n.AR {
			reply = r.ReplyAr
		}
		if reply == nil {
			reply = r.ReplyAr
			if reply == nil {
				reply = r.ReplyFr
			}
		}
		c.Reply = deref(reply)
		if r.PhotoID != nil && r.PhotoWidth != nil && r.PhotoHeight != nil {
			c.Photo = view.MediaImage(*r.PhotoID, ints(r.PhotoWidths), int(*r.PhotoWidth), int(*r.PhotoHeight))
		}
		cards = append(cards, c)
	}
	return cards, more, nil
}

func reviewsHref(l i18n.Locale, stars, page int) string {
	v := url.Values{}
	if stars > 0 {
		v.Set("stars", strconv.Itoa(stars))
	}
	if page > 1 {
		v.Set("rpage", strconv.Itoa(page))
	}
	href := "/" + string(l)
	if q := v.Encode(); q != "" {
		href += "?" + q
	}
	return href + "#reviews"
}

// reviewsSection builds the landing section: the first `page` pages (no-JS
// "load more" shows them all), the summary of real reviews and the form.
func (h *Handlers) reviewsSection(r *http.Request, l i18n.Locale, d *store.SiteData) (*view.ReviewsData, error) {
	if !d.Settings.Toggles.ShowReviews {
		return nil, nil
	}
	stars, page := reviewQuery(r.URL.Query(), "rpage")
	cards, more, err := h.reviewCards(r.Context(), l, stars, 0, page*reviewsPageSize)
	if err != nil {
		return nil, err
	}
	out := &view.ReviewsData{
		List: view.ReviewList{
			Items: cards, Stars: stars, AllHref: reviewsHref(l, 0, 1), FragmentBase: "/" + string(l) + "/reviews",
		},
		Form: view.ReviewFormData{Token: h.FormTokens.Issue()},
	}
	if more {
		out.List.NextPage = page + 1
		out.List.MoreHref = reviewsHref(l, stars, page+1)
	}
	if s := d.RealReviews; s.Total > 0 {
		avg := s.Average.Round(1)
		avgF, _ := avg.Float64()
		sum := &view.ReviewSummary{
			Average: avgF, AverageText: domain.FormatNumber(avg), CountText: strconv.FormatInt(s.Total, 10),
			AllReal: d.DemoReviews == 0,
		}
		for i, n := range []int64{s.R5, s.R4, s.R3, s.R2, s.R1} {
			st := 5 - i
			href := reviewsHref(l, st, 1)
			if stars == st {
				href = reviewsHref(l, 0, 1)
			}
			sum.Bars = append(sum.Bars, view.ReviewBar{Stars: st, Count: n, Pct: int((n*100 + s.Total/2) / s.Total), Href: href, Active: stars == st})
		}
		out.Summary = sum
	}
	return out, nil
}

// ReviewsFragment serves GET /{locale}/reviews?page=&stars= for "load more"
// and the star filters (cards only, or the whole list part with ?list=1).
func (h *Handlers) ReviewsFragment(w http.ResponseWriter, r *http.Request) {
	l, ok := i18n.Parse(r.PathValue("locale"))
	if !ok {
		h.NotFound(w, r)
		return
	}
	d, err := h.Site.Get(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if !d.Settings.Toggles.ShowReviews {
		h.NotFound(w, r)
		return
	}
	stars, page := reviewQuery(r.URL.Query(), "page")
	cards, more, err := h.reviewCards(r.Context(), l, stars, (page-1)*reviewsPageSize, reviewsPageSize)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	p := h.page(r, l, d, "")
	list := view.ReviewList{Items: cards, Stars: stars, AllHref: reviewsHref(l, 0, 1), FragmentBase: "/" + string(l) + "/reviews"}
	if more {
		list.NextPage = page + 1
		list.MoreHref = reviewsHref(l, stars, page+1)
	}
	w.Header().Set("X-Robots-Tag", "noindex")
	if r.URL.Query().Get("list") == "1" {
		render(w, r, http.StatusOK, p.Nonce, view.ReviewListPart(p, list))
		return
	}
	render(w, r, http.StatusOK, p.Nonce, view.ReviewFragment(p, list))
}

func (h *Handlers) reviewPage(r *http.Request, l i18n.Locale, d *store.SiteData) view.Page {
	p := h.page(r, l, d, "/reviews/new")
	p.Title = i18n.T(l, i18n.ReviewsFormTitle) + " | " + p.SiteName()
	p.NoIndex = true
	p.Canonical = ""
	p.Alternates = nil
	p.Script = "js/landing.js"
	return p
}

// ReviewForm serves GET /{locale}/reviews/new: the form on its own page
// (without JS) and the thank-you after a post (?sent=1).
func (h *Handlers) ReviewForm(w http.ResponseWriter, r *http.Request) {
	l, ok := i18n.Parse(r.PathValue("locale"))
	if !ok {
		h.NotFound(w, r)
		return
	}
	d, err := h.Site.Get(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if !d.Settings.Toggles.ShowReviews {
		h.NotFound(w, r)
		return
	}
	p := h.reviewPage(r, l, d)
	f := view.ReviewFormData{Token: h.FormTokens.Issue(), Sent: r.URL.Query().Get("sent") == "1"}
	render(w, r, http.StatusOK, p.Nonce, view.ReviewPage(p, f))
}

// SubmitReview handles POST /{locale}/reviews (multipart with an optional
// photo): validation, honeypot, signed form token, optional Turnstile, ALTCHA
// proof of work, 3 per hour per IP, then a pending review.
func (h *Handlers) SubmitReview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l, ok := i18n.Parse(r.PathValue("locale"))
	if !ok {
		h.NotFound(w, r)
		return
	}
	d, err := h.Site.Get(ctx)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if !d.Settings.Toggles.ShowReviews {
		h.NotFound(w, r)
		return
	}
	// The body is capped (5 MB photo + fields) by the server middleware before this.
	if err := r.ParseMultipartForm(1 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) { //nolint:gosec // G120: bounded by LimitBodyFor
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	if !reviewFieldsOK(r) {
		http.Error(w, "unexpected form field", http.StatusBadRequest)
		return
	}
	in := domain.ReviewInput{
		Name: r.PostFormValue("name"), Rating: r.PostFormValue("rating"), Comment: r.PostFormValue("comment"),
		City: r.PostFormValue("city"), Phone: r.PostFormValue("phone"),
	}
	p := h.reviewPage(r, l, d)
	f := view.ReviewFormData{
		Token:  h.FormTokens.Issue(),
		Values: map[string]string{"name": in.Name, "rating": in.Rating, "comment": in.Comment, "city": in.City, "phone": in.Phone},
	}
	fail := func(status int, formErr i18n.Key, fieldErrs map[string]string) {
		if formErr != "" {
			f.FormError = i18n.T(l, formErr)
		}
		f.Errors = fieldErrs
		render(w, r, status, p.Nonce, view.ReviewPage(p, f))
	}
	v, ferrs := in.Validate()
	if len(ferrs) > 0 {
		msgs := map[string]string{}
		for k, key := range ferrs {
			msgs[k] = i18n.T(l, i18n.Key(key))
		}
		fail(http.StatusUnprocessableEntity, "", msgs)
		return
	}
	if r.PostFormValue("website") != "" || h.FormTokens.Check(r.PostFormValue("form_token")) != security.FormTokenOK {
		fail(http.StatusUnprocessableEntity, i18n.ReviewsErrorsGeneric, nil)
		return
	}
	if h.Turnstile != nil && !h.Turnstile.Verify(ctx, r.PostFormValue("cf-turnstile-response"), httpx.ClientIP(ctx)) {
		fail(http.StatusUnprocessableEntity, i18n.ReviewsErrorsGeneric, nil)
		return
	}
	if pow, err := h.Pow.Check(ctx, r.PostFormValue("altcha")); err != nil || pow != security.PowOK {
		if err != nil {
			h.Logger.ErrorContext(ctx, "pow check", "err", err)
		}
		fail(http.StatusUnprocessableEntity, i18n.ReviewsErrorsGeneric, nil)
		return
	}
	ipHash := h.IPHash.Sum(httpx.ClientIP(ctx))
	allowed, err := h.Limiter.Allow(ctx, "review:ip:"+hex.EncodeToString(ipHash), reviewLimit, reviewLimitWindow)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if !allowed {
		fail(http.StatusTooManyRequests, i18n.ReviewsErrorsRateLimited, nil)
		return
	}

	var photoID *string
	if file, _, err := r.FormFile("photo"); err == nil {
		defer func() { _ = file.Close() }()
		data, rerr := media.ReadLimited(file, domain.ReviewPhotoMaxBytes)
		switch {
		case errors.Is(rerr, media.ErrTooLarge):
			fail(http.StatusUnprocessableEntity, "", map[string]string{"photo": i18n.T(l, i18n.ReviewsErrorsPhotoTooLarge)})
			return
		case rerr != nil:
			fail(http.StatusUnprocessableEntity, "", map[string]string{"photo": i18n.T(l, i18n.ReviewsErrorsPhotoInvalid)})
			return
		case len(data) > 0:
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(3 * time.Minute))
			res, perr := h.Media.Process(ctx, data)
			switch {
			case errors.Is(perr, media.ErrTooLarge):
				fail(http.StatusUnprocessableEntity, "", map[string]string{"photo": i18n.T(l, i18n.ReviewsErrorsPhotoTooLarge)})
				return
			case errors.Is(perr, media.ErrUnsupported), errors.Is(perr, media.ErrInvalid):
				fail(http.StatusUnprocessableEntity, "", map[string]string{"photo": i18n.T(l, i18n.ReviewsErrorsPhotoInvalid)})
				return
			case errors.Is(perr, media.ErrBusy):
				fail(http.StatusServiceUnavailable, i18n.ReviewsErrorsGeneric, nil)
				return
			case perr != nil:
				h.serverError(w, r, perr)
				return
			}
			widths := make([]int32, len(res.Widths))
			for i, x := range res.Widths {
				widths[i] = int32(x) //nolint:gosec // G115: at most 1600
			}
			m, merr := h.Store.Q.CreateMedia(ctx, dbq.CreateMediaParams{
				ID: res.ID, Kind: dbq.MediaKindReviewPhoto, Width: int32(res.Width), Height: int32(res.Height), Widths: widths, //nolint:gosec // G115: capped sides
			})
			if merr != nil {
				h.Media.Remove(res.ID)
				h.serverError(w, r, merr)
				return
			}
			photoID = &m.ID
		}
	}

	var phoneHash []byte
	if v.PhoneE164 != "" {
		phoneHash = h.PhoneHash.Sum(v.PhoneE164)
	}
	rev, err := h.Store.SubmitReview(ctx, store.NewReview{Review: v, PhoneHash: phoneHash, PhotoID: photoID, IPHash: ipHash})
	if err != nil {
		if photoID != nil {
			h.Media.Remove(*photoID)
		}
		h.serverError(w, r, err)
		return
	}
	h.Logger.InfoContext(ctx, "review received", "id", rev.ID.String(), "rating", v.Rating, "photo", photoID != nil, "verified", rev.OrderID.Valid)
	http.Redirect(w, r, "/"+string(l)+"/reviews/new?sent=1", http.StatusSeeOther) //nolint:gosec // G710: l is a parsed locale
}

// reviewFieldsOK accepts only the review form's fields and one photo.
func reviewFieldsOK(r *http.Request) bool {
	for name, values := range r.PostForm {
		switch name {
		case "name", "rating", "comment", "city", "phone", "website", "form_token", "altcha", "cf-turnstile-response":
		case "photo":
			// No photo chosen: the browser sends an empty part (filename=""),
			// which Go reads as an empty value. Anything else is refused.
			for _, v := range values {
				if v != "" {
					return false
				}
			}
		default:
			return false
		}
	}
	if r.MultipartForm != nil {
		for name, files := range r.MultipartForm.File {
			if name != "photo" || len(files) > 1 {
				return false
			}
		}
	}
	return true
}
