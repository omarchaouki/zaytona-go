package admin

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/omarchaouki/zaytona-go/internal/auth"
	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/domain"
	"github.com/omarchaouki/zaytona-go/internal/i18n"
	"github.com/omarchaouki/zaytona-go/internal/store"
	"github.com/omarchaouki/zaytona-go/internal/view"
)

// reviewsRoutes: moderation is open to staff (as in the reference); deleting
// is owner-only. Every action is audited and refreshes the site cache.
func (h *Handlers) reviewsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/reviews", h.guard(roleAny, 0, h.Reviews))
	post(mux, "POST /admin/reviews/bulk", h.guard(roleAny, 0, h.ReviewsBulk))
	post(mux, "POST /admin/reviews/{id}/delete", h.guard(roleOwner, 0, h.ReviewDelete))
	post(mux, "POST /admin/reviews/{id}/{action}", h.guard(roleAny, 0, h.ReviewAction))
}

const reviewsPageSize = 20

var reviewStatuses = []string{"pending", "approved", "rejected", "all"}

// Reviews lists reviews by status (pending first), with search.
func (h *Handlers) Reviews(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	q := r.URL.Query()
	d := view.AdminReviewsData{Status: "pending", Page: 1, Counts: map[string]int64{}}
	if s := q.Get("status"); slices.Contains(reviewStatuses, s) {
		d.Status = s
	}
	if s, ok := domain.CleanText(q.Get("q"), false); ok && utf8.RuneCountInString(s) <= 100 {
		d.Q = s
	}
	if n, err := strconv.Atoi(q.Get("page")); err == nil && n > 1 && n <= 1000 {
		d.Page = n
	}
	params := dbq.AdminListReviewsParams{MaxRows: reviewsPageSize, Skip: int32((d.Page - 1) * reviewsPageSize)} //nolint:gosec // G115: page capped at 1000
	if d.Status != "all" {
		st := dbq.ReviewStatus(d.Status)
		params.Status = &st
	}
	if s := likeSafe(d.Q); s != "" {
		params.Q = &s
	}
	rows, err := h.Store.Q.AdminListReviews(ctx, params)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	counts, err := h.Store.Q.ReviewStatusCounts(ctx)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	for _, c := range counts {
		d.Counts[string(c.Status)] = c.N
	}
	var total int64
	for _, x := range rows {
		total = x.TotalCount
		row := view.AdminReviewRow{
			ID: x.ID.String(), Name: x.AuthorName, City: deref(x.City), Comment: x.Comment, Original: deref(x.OriginalComment),
			ReplyAr: deref(x.ReplyAr), ReplyFr: deref(x.ReplyFr), Reason: deref(x.RejectionReason), Status: string(x.Status),
			OrderReference: deref(x.OrderReference), Rating: int(x.Rating), Featured: x.IsFeatured, Verified: x.Verified,
			Demo: x.IsDemo, CreatedAt: x.CreatedAt,
		}
		if x.PhotoID != nil && x.PhotoWidth != nil && x.PhotoHeight != nil {
			row.Photo = view.MediaImage(*x.PhotoID, ints32(x.PhotoWidths), int(*x.PhotoWidth), int(*x.PhotoHeight))
		}
		d.Rows = append(d.Rows, row)
	}
	pageURL := func(n int) string {
		v := url.Values{"status": {d.Status}}
		if d.Q != "" {
			v.Set("q", d.Q)
		}
		if n > 1 {
			v.Set("page", strconv.Itoa(n))
		}
		return "/admin/reviews?" + v.Encode()
	}
	d.Return = pageURL(d.Page)
	if d.Page > 1 {
		d.PrevURL = pageURL(d.Page - 1)
	}
	if int64(d.Page*reviewsPageSize) < total {
		d.NextURL = pageURL(d.Page + 1)
	}
	p := h.page(r, cur, i18n.AdminReviewsTitle, "reviews")
	if q.Get("done") == "reviews" {
		p.Flash = p.T(i18n.AdminReviewsDone, "count", q.Get("n"))
	}
	render(w, r, http.StatusOK, p.Nonce, view.AdminReviews(p, d))
}

// reviewsBack returns to the list the admin was on, with the result.
func reviewsBack(w http.ResponseWriter, r *http.Request, n int) {
	ret := safeNext(r.PostFormValue("return"))
	if !strings.HasPrefix(ret, "/admin/reviews") {
		ret = "/admin/reviews"
	}
	u, _ := url.Parse(ret)
	q := u.Query()
	q.Set("done", "reviews")
	q.Set("n", strconv.Itoa(n))
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusSeeOther) //nolint:gosec // G710: same-site /admin/reviews path only
}

// ReviewsBulk publishes or refuses the checked reviews.
func (h *Handlers) ReviewsBulk(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ids := parseIDs(r)
	action := r.PostFormValue("action")
	if action != "approve" && action != "reject" {
		http.Error(w, "bad action", http.StatusBadRequest)
		return
	}
	n := 0
	if len(ids) > 0 {
		var err error
		n, err = h.Store.ModerateReviews(r.Context(), actor(r, h, cur), ids, action == "approve", "")
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		h.Site.Invalidate()
	}
	reviewsBack(w, r, n)
}

// ReviewAction approves, refuses (with an internal reason), pins, unpins or edits one review.
func (h *Handlers) ReviewAction(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a := actor(r, h, cur)
	n := 1
	switch r.PathValue("action") {
	case "approve", "reject":
		reason := ""
		if r.PathValue("action") == "reject" {
			s, ok := domain.CleanText(r.PostFormValue("reason"), false)
			if !ok || utf8.RuneCountInString(s) > 300 {
				http.Error(w, "bad reason", http.StatusBadRequest)
				return
			}
			reason = s
		}
		n, err = h.Store.ModerateReviews(ctx, a, []uuid.UUID{id}, r.PathValue("action") == "approve", reason)
	case "pin", "unpin":
		err = h.Store.SetReviewFeatured(ctx, a, id, r.PathValue("action") == "pin")
	case "edit":
		comment, ok1 := domain.CleanText(r.PostFormValue("comment"), true)
		replyAr, ok2 := domain.CleanText(r.PostFormValue("replyAr"), true)
		replyFr, ok3 := domain.CleanText(r.PostFormValue("replyFr"), true)
		cl := utf8.RuneCountInString(comment)
		if !ok1 || !ok2 || !ok3 || cl < domain.ReviewCommentMin || cl > domain.ReviewCommentMax ||
			utf8.RuneCountInString(replyAr) > 1000 || utf8.RuneCountInString(replyFr) > 1000 {
			http.Error(w, "bad input", http.StatusBadRequest)
			return
		}
		err = h.Store.EditReview(ctx, a, id, comment, replyAr, replyFr)
	default:
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.Site.Invalidate()
	reviewsBack(w, r, n)
}

// ReviewDelete removes a review and its photo for good (owner, confirmed).
func (h *Handlers) ReviewDelete(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if r.PostFormValue("confirm") != "1" {
		p := h.page(r, cur, i18n.AdminCommonDeleteTitle, "reviews")
		back := safeNext(r.PostFormValue("return"))
		if back == "" {
			back = "/admin/reviews"
		}
		render(w, r, http.StatusOK, p.Nonce, view.AdminConfirm(p, r.URL.Path, p.T(i18n.AdminCommonDeleteTitle), p.T(i18n.AdminReviewsDeleteBody), back, map[string]string{"return": back}))
		return
	}
	photos, err := h.Store.DeleteReviews(r.Context(), actor(r, h, cur), []uuid.UUID{id})
	if errors.Is(err, store.ErrForbidden) {
		h.message(w, r, cur, http.StatusForbidden, i18n.AdminCommonForbidden, "")
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	for _, ph := range photos {
		h.dropMedia(r, &ph)
	}
	h.Site.Invalidate()
	reviewsBack(w, r, 1)
}
