package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/omarchaouki/zaytona-go/internal/auth"
	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/db/dbtest"
	"github.com/omarchaouki/zaytona-go/internal/seed"
	"github.com/omarchaouki/zaytona-go/internal/store"
)

const (
	ownerEmail    = "owner@huilerie.example"
	ownerPassword = "olive harvest at dawn 2026"
	staffEmail    = "staff@huilerie.example"
	staffPassword = "pressing the green olives"
)

type adminEnv struct {
	t     *testing.T
	h     http.Handler
	s     *store.Store
	owner dbq.Admin
	staff dbq.Admin
}

func newAdminEnv(t *testing.T) *adminEnv {
	t.Helper()
	s := store.New(dbtest.NewDatabase(t, pgURL))
	if err := seed.Run(context.Background(), s, seed.Options{}); err != nil {
		t.Fatal(err)
	}
	h, _ := newServerWith(t, s, nil)
	e := &adminEnv{t: t, h: h, s: s}
	e.owner = e.createAdmin(ownerEmail, ownerPassword, dbq.AdminRoleOwner, false)
	e.staff = e.createAdmin(staffEmail, staffPassword, dbq.AdminRoleStaff, false)
	return e
}

func (e *adminEnv) createAdmin(email, password string, role dbq.AdminRole, mustChange bool) dbq.Admin {
	e.t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		e.t.Fatal(err)
	}
	a, err := e.s.Q.CreateAdmin(context.Background(), dbq.CreateAdminParams{
		Email: email, PasswordHash: hash, Role: role, MustChangePassword: mustChange, UiLocale: dbq.LocaleFr,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return a
}

// browser keeps the session cookie and the last CSRF token like a real one.
type browser struct {
	e      *adminEnv
	ip     string
	cookie *http.Cookie
	csrf   string
}

func (e *adminEnv) browser(ip string) *browser { return &browser{e: e, ip: ip} }

var csrfRe = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

func (b *browser) do(method, path string, form url.Values) *httptest.ResponseRecorder {
	b.e.t.Helper()
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequestWithContext(b.e.t.Context(), method, path, body)
	req.RemoteAddr = b.ip
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if b.cookie != nil {
		req.AddCookie(b.cookie)
	}
	rec := httptest.NewRecorder()
	b.e.h.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Name == "__Host-zaytona_session" || c.Name == "zaytona_session" {
			if c.MaxAge < 0 {
				b.cookie = nil
			} else {
				b.cookie = c
			}
		}
	}
	if m := csrfRe.FindStringSubmatch(rec.Body.String()); m != nil {
		b.csrf = m[1]
	}
	return rec
}

func (b *browser) get(path string) *httptest.ResponseRecorder { return b.do(http.MethodGet, path, nil) }

// post sends a form with the current CSRF token.
func (b *browser) post(path string, form url.Values) *httptest.ResponseRecorder {
	if form == nil {
		form = url.Values{}
	}
	if form.Get("csrf") == "" && b.csrf != "" {
		form.Set("csrf", b.csrf)
	}
	return b.do(http.MethodPost, path, form)
}

func (b *browser) login(email, password string) *httptest.ResponseRecorder {
	return b.do(http.MethodPost, "/admin/login", url.Values{"email": {email}, "password": {password}})
}

// signIn logs in and loads the dashboard (for the CSRF token).
func (b *browser) signIn(email, password string) {
	b.e.t.Helper()
	if rec := b.login(email, password); rec.Code != http.StatusSeeOther {
		b.e.t.Fatalf("login %s: %d %s", email, rec.Code, rec.Body.String())
	}
	if rec := b.get("/admin"); rec.Code != http.StatusOK {
		b.e.t.Fatalf("dashboard: %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

func location(rec *httptest.ResponseRecorder) string { return rec.Header().Get("Location") }

func (e *adminEnv) auditCount(action string) int {
	e.t.Helper()
	var n int
	if err := e.s.Pool.QueryRow(context.Background(), "select count(*) from audit_logs where action = $1", action).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func TestAdminRequiresSession(t *testing.T) {
	e := newAdminEnv(t)
	b := e.browser("198.51.100.1:1000")
	if rec := b.get("/admin"); rec.Code != http.StatusSeeOther || location(rec) != "/admin/login" {
		t.Fatalf("/admin: %d %s", rec.Code, location(rec))
	}
	rec := b.get("/admin/orders?status=new")
	if rec.Code != http.StatusSeeOther || location(rec) != "/admin/login?next="+url.QueryEscape("/admin/orders?status=new") {
		t.Fatalf("/admin/orders: %d %s", rec.Code, location(rec))
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("cache-control %q", cc)
	}
	if xr := rec.Header().Get("X-Robots-Tag"); !strings.Contains(xr, "noindex") {
		t.Errorf("x-robots-tag %q", xr)
	}
	// POSTs without a session never act.
	if rec := b.do(http.MethodPost, "/admin/logout", url.Values{}); rec.Code != http.StatusSeeOther || location(rec) != "/admin/login" {
		t.Fatalf("logout without session: %d %s", rec.Code, location(rec))
	}
	page := b.get("/admin/login?lang=ar")
	if page.Code != 200 || !strings.Contains(page.Body.String(), `dir="rtl"`) || !strings.Contains(page.Body.String(), `name="email"`) {
		t.Fatalf("login page: %d", page.Code)
	}
}

func TestAdminLoginAndLogout(t *testing.T) {
	e := newAdminEnv(t)
	b := e.browser("198.51.100.2:1000")

	wrong := b.login(ownerEmail, "not the password at all")
	unknown := b.login("nobody@huilerie.example", "not the password at all")
	if wrong.Code != http.StatusUnauthorized || unknown.Code != http.StatusUnauthorized {
		t.Fatalf("failures: %d %d", wrong.Code, unknown.Code)
	}
	// Same generic message (login page defaults to Arabic) for both.
	for _, rec := range []*httptest.ResponseRecorder{wrong, unknown} {
		if !strings.Contains(rec.Body.String(), "البريد الإلكتروني أو كلمة المرور غير صحيحة.") {
			t.Fatalf("generic error missing: %s", rec.Body.String())
		}
	}
	if b.cookie != nil {
		t.Fatal("failed login set a session cookie")
	}
	if n := e.auditCount("auth.login_failed"); n != 2 {
		t.Fatalf("login_failed audits = %d", n)
	}

	// Open redirect attempts fall back to /admin.
	rec := b.do(http.MethodPost, "/admin/login", url.Values{"email": {"OWNER@huilerie.example "}, "password": {ownerPassword}, "next": {"//evil.example/admin"}})
	if rec.Code != http.StatusSeeOther || location(rec) != "/admin" {
		t.Fatalf("login: %d %s", rec.Code, location(rec))
	}
	c := b.cookie
	if c == nil || c.Name != "__Host-zaytona_session" || !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
		t.Fatalf("cookie %+v", c)
	}
	if e.auditCount("auth.login") != 1 {
		t.Fatal("login not audited")
	}
	home := b.get("/admin")
	if home.Code != 200 || !strings.Contains(home.Body.String(), ownerEmail) {
		t.Fatalf("dashboard %d", home.Code)
	}
	// Logged in: the login page goes to the dashboard.
	if rec := b.get("/admin/login"); rec.Code != http.StatusSeeOther || location(rec) != "/admin" {
		t.Fatalf("login page when signed in: %d", rec.Code)
	}

	// CSRF: a missing or wrong token is refused and the session stays.
	if rec := b.do(http.MethodPost, "/admin/logout", url.Values{"csrf": {"x"}}); rec.Code != http.StatusForbidden {
		t.Fatalf("logout with a bad token: %d", rec.Code)
	}
	if rec := b.do(http.MethodPost, "/admin/logout", url.Values{}); rec.Code != http.StatusForbidden {
		t.Fatalf("logout without token: %d", rec.Code)
	}
	if rec := b.post("/admin/logout", nil); rec.Code != http.StatusSeeOther || !strings.HasPrefix(location(rec), "/admin/login") {
		t.Fatalf("logout: %d %s", rec.Code, location(rec))
	}
	// The old cookie no longer works.
	b.cookie = c
	if rec := b.get("/admin"); rec.Code != http.StatusSeeOther {
		t.Fatalf("after logout: %d", rec.Code)
	}
}

func TestAdminLoginThrottle(t *testing.T) {
	e := newAdminEnv(t)
	b := e.browser("198.51.100.3:1000")
	for i := range 5 {
		if rec := b.login(ownerEmail, "wrong password number"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i, rec.Code)
		}
	}
	// The right password no longer helps for this email, from any IP.
	other := e.browser("198.51.100.4:1000")
	if rec := other.login(ownerEmail, ownerPassword); rec.Code != http.StatusTooManyRequests || other.cookie != nil {
		t.Fatalf("after 5 failures: %d", rec.Code)
	}
	// Another account from a fresh IP still works.
	if rec := other.login(staffEmail, staffPassword); rec.Code != http.StatusSeeOther {
		t.Fatalf("staff login: %d", rec.Code)
	}
	// 10 failures per IP block that IP for every account.
	ip := e.browser("198.51.100.5:1000")
	for i := range 10 {
		ip.login("someone"+string(rune('a'+i))+"@huilerie.example", "wrong password number")
	}
	if rec := ip.login(staffEmail, staffPassword); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("after 10 failures from one IP: %d", rec.Code)
	}
}

func TestAdminForcedPasswordChange(t *testing.T) {
	e := newAdminEnv(t)
	first := e.createAdmin("newcomer@huilerie.example", "temporary password 1", dbq.AdminRoleStaff, true)
	other := e.browser("198.51.100.6:1000")
	if rec := other.login(first.Email, "temporary password 1"); location(rec) != "/admin/password" {
		t.Fatalf("other device: %s", location(rec))
	}
	b := e.browser("198.51.100.7:1000")
	if rec := b.login(first.Email, "temporary password 1"); rec.Code != http.StatusSeeOther || location(rec) != "/admin/password" {
		t.Fatalf("login: %d %s", rec.Code, location(rec))
	}
	for _, p := range []string{"/admin", "/admin/orders", "/admin/security"} {
		if rec := b.get(p); rec.Code != http.StatusSeeOther || location(rec) != "/admin/password" {
			t.Fatalf("%s: %d %s", p, rec.Code, location(rec))
		}
	}
	page := b.get("/admin/password")
	if page.Code != 200 || strings.Contains(page.Body.String(), `href="/admin/orders"`) {
		t.Fatalf("forced page should hide the navigation: %d", page.Code)
	}
	old := b.cookie
	cases := []struct{ current, next, confirm, want string }{
		{"wrong current pw", "a brand new passphrase", "a brand new passphrase", "Mot de passe actuel incorrect."},
		{"temporary password 1", "short", "short", "Au moins 12 caractères."},
		{"temporary password 1", "Password1234", "Password1234", "trop courant"},
		{"temporary password 1", "newcomer password 1", "newcomer password 1", "adresse e-mail"},
		{"temporary password 1", "temporary password 1", "temporary password 1", "différent de l’actuel"},
		{"temporary password 1", "a brand new passphrase", "a brand new passphrasf", "ne correspondent pas"},
	}
	for _, c := range cases {
		rec := b.post("/admin/password", url.Values{"current": {c.current}, "next": {c.next}, "confirm": {c.confirm}})
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), c.want) {
			t.Fatalf("%q: %d, want %q", c.next, rec.Code, c.want)
		}
	}
	a, _ := e.s.Q.GetAdminByID(context.Background(), first.ID)
	if !a.MustChangePassword {
		t.Fatal("a rejected password was saved")
	}
	rec := b.post("/admin/password", url.Values{"current": {"temporary password 1"}, "next": {"a brand new passphrase"}, "confirm": {"a brand new passphrase"}})
	if rec.Code != http.StatusSeeOther || location(rec) != "/admin" {
		t.Fatalf("change: %d %s %s", rec.Code, location(rec), rec.Body.String())
	}
	if b.cookie == nil || b.cookie.Value == old.Value {
		t.Fatal("session not rotated")
	}
	if rec := b.get("/admin"); rec.Code != 200 {
		t.Fatalf("dashboard after change: %d", rec.Code)
	}
	if rec := other.get("/admin"); rec.Code != http.StatusSeeOther || location(rec) != "/admin/login" {
		t.Fatalf("other session still valid: %d %s", rec.Code, location(rec))
	}
	b.cookie = old
	if rec := b.get("/admin"); rec.Code != http.StatusSeeOther {
		t.Fatal("old cookie still valid after password change")
	}
	if e.auditCount("auth.password_changed") != 1 {
		t.Fatal("password change not audited")
	}
}

var (
	secretRe   = regexp.MustCompile(`<code dir="ltr"[^>]*>([A-Z2-7]+)</code>`)
	recoveryRe = regexp.MustCompile(`<li class="rounded-lg bg-white px-3 py-2 text-center">([a-z2-9]{4}-[a-z2-9]{4})</li>`)
)

func TestAdminTwoFactor(t *testing.T) {
	e := newAdminEnv(t)
	b := e.browser("198.51.100.8:1000")
	b.signIn(ownerEmail, ownerPassword)

	// Fresh login counts as a recent authentication: setup starts directly.
	if rec := b.post("/admin/security/totp/start", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("start: %d", rec.Code)
	}
	page := b.get("/admin/security")
	m := secretRe.FindStringSubmatch(page.Body.String())
	if m == nil || !strings.Contains(page.Body.String(), `src="data:image/png;base64,`) {
		t.Fatalf("enrollment not shown: %s", page.Body.String())
	}
	secret := m[1]
	if rec := b.post("/admin/security/totp/enable", url.Values{"code": {"000000"}}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad code: %d", rec.Code)
	}
	code, _ := totp.GenerateCode(secret, time.Now())
	rec := b.post("/admin/security/totp/enable", url.Values{"code": {code}})
	codes := recoveryRe.FindAllStringSubmatch(rec.Body.String(), -1)
	if rec.Code != 200 || len(codes) != 10 {
		t.Fatalf("enable: %d, %d codes", rec.Code, len(codes))
	}
	a, _ := e.s.Q.GetAdminByID(context.Background(), e.owner.ID)
	if a.TotpEnabledAt == nil || strings.Contains(string(a.TotpSecretEnc), secret) {
		t.Fatal("2FA not enabled or secret stored in clear")
	}
	if rec := b.get("/admin"); rec.Code != 200 {
		t.Fatalf("rotated session lost: %d", rec.Code)
	}
	b.post("/admin/logout", nil)

	// Password alone now leads to the code step only.
	if rec := b.login(ownerEmail, ownerPassword); location(rec) != "/admin/2fa" {
		t.Fatalf("login with 2FA: %s", location(rec))
	}
	for _, p := range []string{"/admin", "/admin/orders", "/admin/password"} {
		if rec := b.get(p); location(rec) != "/admin/2fa" {
			t.Fatalf("%s while code pending: %d %s", p, rec.Code, location(rec))
		}
	}
	b.get("/admin/2fa")
	if rec := b.post("/admin/2fa", url.Values{"code": {"123456"}}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong code: %d", rec.Code)
	}
	// The code used at setup cannot be replayed in the same time step.
	if rec := b.post("/admin/2fa", url.Values{"code": {code}}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("replayed code: %d", rec.Code)
	}
	pending := b.cookie
	recovery := codes[0][1]
	if rec := b.post("/admin/2fa", url.Values{"code": {strings.ToUpper(recovery)}}); rec.Code != http.StatusSeeOther || location(rec) != "/admin" {
		t.Fatalf("recovery code: %d %s", rec.Code, location(rec))
	}
	if b.cookie.Value == pending.Value {
		t.Fatal("session not rotated after the second factor")
	}
	if rec := b.get("/admin"); rec.Code != 200 {
		t.Fatalf("after 2FA: %d", rec.Code)
	}
	if e.auditCount("auth.recovery_code_used") != 1 {
		t.Fatal("recovery code use not audited")
	}
	b.post("/admin/logout", nil)

	// A recovery code works once; a fresh TOTP code (next step) works.
	b.login(ownerEmail, ownerPassword)
	b.get("/admin/2fa")
	if rec := b.post("/admin/2fa", url.Values{"code": {recovery}}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("recovery code reused: %d", rec.Code)
	}
	next, _ := totp.GenerateCode(secret, time.Now().Add(30*time.Second))
	if rec := b.post("/admin/2fa", url.Values{"code": {next}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("next-step code: %d", rec.Code)
	}

	// Disable: needs a recent authentication, which the code step gave.
	b.get("/admin/security")
	if rec := b.post("/admin/security/totp/disable", nil); rec.Code != http.StatusSeeOther || location(rec) != "/admin/security?done=totp-off" {
		t.Fatalf("disable: %d %s", rec.Code, location(rec))
	}
	a, _ = e.s.Q.GetAdminByID(context.Background(), e.owner.ID)
	if a.TotpEnabledAt != nil || len(a.TotpSecretEnc) != 0 {
		t.Fatal("2FA still on")
	}
	if e.auditCount("auth.totp_enabled") != 1 || e.auditCount("auth.totp_disabled") != 1 {
		t.Fatal("2FA changes not audited")
	}
}

func TestAdminTwoFactorNeedsRecentAuth(t *testing.T) {
	e := newAdminEnv(t)
	b := e.browser("198.51.100.9:1000")
	b.signIn(ownerEmail, ownerPassword)
	if _, err := e.s.Pool.Exec(context.Background(), "update sessions set recent_auth_at = now() - interval '11 minutes'"); err != nil {
		t.Fatal(err)
	}
	page := b.get("/admin/security")
	if !strings.Contains(page.Body.String(), `action="/admin/security/reauth"`) {
		t.Fatal("no password prompt")
	}
	if rec := b.post("/admin/security/totp/start", nil); location(rec) != "/admin/security" {
		t.Fatalf("start without recent auth: %s", location(rec))
	}
	a, _ := e.s.Q.GetAdminByID(context.Background(), e.owner.ID)
	if len(a.TotpSecretEnc) != 0 {
		t.Fatal("setup started without a recent password")
	}
	if rec := b.post("/admin/security/reauth", url.Values{"password": {"wrong one here"}}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("reauth wrong: %d", rec.Code)
	}
	if rec := b.post("/admin/security/reauth", url.Values{"password": {ownerPassword}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("reauth: %d", rec.Code)
	}
	if !strings.Contains(b.get("/admin/security").Body.String(), `action="/admin/security/totp/start"`) {
		t.Fatal("setup button missing after reauth")
	}
}

func TestAdminSignOutOthersAndLocale(t *testing.T) {
	e := newAdminEnv(t)
	phone := e.browser("198.51.100.10:1000")
	phone.signIn(staffEmail, staffPassword)
	laptop := e.browser("198.51.100.11:1000")
	laptop.signIn(staffEmail, staffPassword)
	laptop.get("/admin/security")
	if rec := laptop.post("/admin/security/signout-others", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("signout others: %d", rec.Code)
	}
	if rec := phone.get("/admin"); rec.Code != http.StatusSeeOther {
		t.Fatal("other session still valid")
	}
	if rec := laptop.get("/admin"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `lang="fr-MA"`) {
		t.Fatalf("current session: %d", rec.Code)
	}
	if rec := laptop.post("/admin/locale", url.Values{"next": {"/admin/security"}}); location(rec) != "/admin/security" {
		t.Fatalf("locale next: %s", location(rec))
	}
	if rec := laptop.get("/admin"); !strings.Contains(rec.Body.String(), `dir="rtl"`) {
		t.Fatal("locale not switched")
	}
	if rec := laptop.post("/admin/locale", url.Values{"next": {"https://evil.example/"}}); location(rec) != "/admin" {
		t.Fatalf("locale open redirect: %s", location(rec))
	}
}
