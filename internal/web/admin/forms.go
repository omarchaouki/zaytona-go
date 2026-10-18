package admin

import (
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/omarchaouki/zaytona-go/internal/auth"
	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/domain"
	"github.com/omarchaouki/zaytona-go/internal/i18n"
	"github.com/omarchaouki/zaytona-go/internal/media"
	"github.com/omarchaouki/zaytona-go/internal/view"
)

// form reads and validates one admin form. Every field read records its
// error (an i18n key) so the page can be shown again with the values and
// the messages next to the fields.
type form struct {
	r      *http.Request
	errors map[string]i18n.Key
}

func newForm(r *http.Request) *form { return &form{r: r, errors: map[string]i18n.Key{}} }

func (f *form) fail(field string, k i18n.Key) {
	if _, ok := f.errors[field]; !ok {
		f.errors[field] = k
	}
}

func (f *form) ok() bool { return len(f.errors) == 0 }

// text is a cleaned single-line (or multiline) value of at most maxLen characters.
func (f *form) text(name string, maxLen int, required, multiline bool) string {
	s, valid := domain.CleanText(f.r.PostFormValue(name), multiline)
	switch {
	case !valid:
		f.fail(name, i18n.AdminCommonError)
	case required && s == "":
		f.fail(name, i18n.AdminCommonRequired)
	case utf8.RuneCountInString(s) > maxLen:
		f.fail(name, i18n.AdminCommonTooLong)
	}
	return s
}

func (f *form) check(name string) bool { return f.r.PostFormValue(name) != "" }

// decimal parses "12,5" or "12.5"; empty gives nil unless required.
func (f *form) decimal(name string, minV, maxV int64, required bool) *decimal.Decimal {
	raw := strings.TrimSpace(strings.ReplaceAll(f.r.PostFormValue(name), ",", "."))
	if raw == "" {
		if required {
			f.fail(name, i18n.AdminCommonRequired)
		}
		return nil
	}
	v, err := decimal.NewFromString(raw)
	if err != nil || v.LessThan(decimal.NewFromInt(minV)) || v.GreaterThan(decimal.NewFromInt(maxV)) || v.Exponent() < -2 {
		f.fail(name, i18n.AdminCommonNumberInvalid)
		return nil
	}
	return &v
}

func (f *form) integer(name string, minV, maxV int, def int) int {
	raw := strings.TrimSpace(f.r.PostFormValue(name))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < minV || n > maxV {
		f.fail(name, i18n.AdminCommonNumberInvalid)
		return def
	}
	return n
}

// date parses YYYY-MM-DD (empty: nil).
func (f *form) date(name string) *time.Time {
	raw := strings.TrimSpace(f.r.PostFormValue(name))
	if raw == "" {
		return nil
	}
	t, err := time.Parse(time.DateOnly, raw)
	if err != nil {
		f.fail(name, i18n.AdminCommonError)
		return nil
	}
	return &t
}

// httpsURL accepts an https link without credentials (empty allowed). prefix,
// when set, must start the link (e.g. the Google Maps embed address).
func (f *form) httpsURL(name, prefix string) string {
	raw := strings.TrimSpace(f.r.PostFormValue(name))
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || len(raw) > 500 || (prefix != "" && !strings.HasPrefix(raw, prefix)) {
		f.fail(name, i18n.AdminCommonUrlInvalid)
		return raw
	}
	return u.String()
}

// email accepts a bare address (empty allowed unless required).
func (f *form) email(name string, required bool) string {
	raw := strings.TrimSpace(f.r.PostFormValue(name))
	if raw == "" {
		if required {
			f.fail(name, i18n.AdminCommonRequired)
		}
		return ""
	}
	a, err := mail.ParseAddress(raw)
	if err != nil || a.Name != "" || a.Address != raw || len(raw) > 254 {
		f.fail(name, i18n.AdminCommonEmailInvalid)
		return raw
	}
	return strings.ToLower(a.Address)
}

// messages turns the field errors into translated text for the view.
func (f *form) messages(p view.AdminPage) map[string]string {
	out := map[string]string{}
	for k, v := range f.errors {
		out[k] = p.T(v)
	}
	return out
}

// values are the posted fields, to show the form again after an error.
func (f *form) values() map[string]string {
	out := map[string]string{}
	for k, v := range f.r.PostForm {
		if k != "csrf" && len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}

// upload processes the optional photo field into a media row. It returns nil
// when no file was sent. The write deadline is extended: encoding takes seconds.
func (h *Handlers) upload(r *http.Request, w http.ResponseWriter, cur *auth.Current, field string, kind dbq.MediaKind) (*dbq.Medium, i18n.Key, error) {
	file, _, err := r.FormFile(field)
	if errors.Is(err, http.ErrMissingFile) {
		return nil, "", nil
	}
	if err != nil {
		return nil, i18n.AdminCommonPhotoInvalid, nil
	}
	defer func() { _ = file.Close() }()
	data, err := media.ReadLimited(file, h.Media.MaxBytes)
	if err != nil {
		return nil, i18n.AdminCommonPhotoInvalid, nil
	}
	if len(data) == 0 {
		return nil, "", nil
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(3 * time.Minute))
	res, err := h.Media.Process(r.Context(), data)
	switch {
	case errors.Is(err, media.ErrBusy):
		return nil, i18n.AdminCommonPhotoBusy, nil
	case errors.Is(err, media.ErrUnsupported), errors.Is(err, media.ErrTooLarge), errors.Is(err, media.ErrInvalid):
		return nil, i18n.AdminCommonPhotoInvalid, nil
	case err != nil:
		return nil, "", err
	}
	widths := make([]int32, len(res.Widths))
	for i, x := range res.Widths {
		widths[i] = int32(x) //nolint:gosec // G115: widths are at most 1600
	}
	m, err := h.Store.Q.CreateMedia(r.Context(), dbq.CreateMediaParams{
		ID: res.ID, Kind: kind, Width: int32(res.Width), Height: int32(res.Height), //nolint:gosec // G115: sides capped at media.MaxSide
		Widths: widths, CreatedBy: uuidPtr(cur),
	})
	if err != nil {
		h.Media.Remove(res.ID)
		return nil, "", err
	}
	return &m, "", nil
}

// dropMedia deletes an image nothing points to any more (row and files).
func (h *Handlers) dropMedia(r *http.Request, id *string) {
	if id == nil || *id == "" {
		return
	}
	inUse, err := h.Store.Q.MediaInUse(r.Context(), *id)
	if err != nil || inUse == nil || *inUse {
		return
	}
	if err := h.Store.Q.DeleteMedia(r.Context(), *id); err != nil {
		h.Logger.ErrorContext(r.Context(), "delete media", "err", err)
		return
	}
	h.Media.Remove(*id)
}

func (h *Handlers) uploadMB() string { return strconv.FormatInt(h.Media.MaxBytes>>20, 10) }

func uuidPtr(cur *auth.Current) uuid.NullUUID { return uuid.NullUUID{UUID: cur.Admin.ID, Valid: true} }
