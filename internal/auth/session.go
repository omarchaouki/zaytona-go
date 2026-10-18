package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
)

// Session lifetimes (SECURITY.md section 6).
const (
	IdleTimeout      = 7 * 24 * time.Hour
	AbsoluteTimeout  = 30 * 24 * time.Hour
	RecentAuthWindow = 10 * time.Minute
	// touchEvery limits writes: last_seen/idle expiry move at most this often.
	touchEvery = 5 * time.Minute
)

// ErrNoSession means no valid session cookie (missing, unknown or expired).
var ErrNoSession = errors.New("auth: no session")

// Current is the signed-in admin of a request.
type Current struct {
	Session dbq.Session
	Admin   dbq.Admin
}

// CSRF is the synchronizer token to put in forms (field "csrf") and htmx headers.
func (c *Current) CSRF() string { return CSRFString(c.Session.CsrfToken) }

// RecentAuth reports whether the admin proved their password or TOTP code recently.
func (c *Current) RecentAuth(now time.Time) bool {
	return now.Sub(c.Session.RecentAuthAt) < RecentAuthWindow
}

// IsOwner reports the owner role.
func (c *Current) IsOwner() bool { return c.Admin.Role == dbq.AdminRoleOwner }

// TOTPEnabled reports whether 2FA is on for this admin.
func (c *Current) TOTPEnabled() bool {
	return c.Admin.TotpEnabledAt != nil && len(c.Admin.TotpSecretEnc) > 0
}

// Sessions creates, loads and ends admin sessions. Only SHA-256 of the cookie
// token is stored, so a database read does not give a usable cookie.
type Sessions struct {
	Q *dbq.Queries
	// Secure selects the __Host- cookie with the Secure flag (production, https).
	Secure bool
	Now    func() time.Time
}

// CookieName is __Host-zaytona_session over https, zaytona_session on http://localhost.
func (s *Sessions) CookieName() string {
	if s.Secure {
		return "__Host-zaytona_session"
	}
	return "zaytona_session"
}

func (s *Sessions) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Start creates a session for admin and sets the cookie. mfaPending sessions
// only reach the 2FA page until the code is checked.
func (s *Sessions) Start(ctx context.Context, w http.ResponseWriter, admin dbq.Admin, mfaPending bool, ipHash []byte, userAgent string) (*Current, error) {
	token, _, err := NewToken()
	if err != nil {
		return nil, err
	}
	_, csrf, err := NewToken()
	if err != nil {
		return nil, err
	}
	now := s.now()
	ua := truncate(userAgent, 200)
	params := dbq.CreateSessionParams{
		ID:                HashToken(token),
		AdminID:           admin.ID,
		CsrfToken:         csrf,
		MfaPending:        mfaPending,
		IdleExpiresAt:     now.Add(IdleTimeout),
		AbsoluteExpiresAt: now.Add(AbsoluteTimeout),
		IpHash:            ipHash,
		UserAgent:         &ua,
	}
	if err := s.Q.CreateSession(ctx, params); err != nil {
		return nil, err
	}
	s.setCookie(w, token, params.AbsoluteExpiresAt)
	return &Current{
		Session: dbq.Session{
			ID: params.ID, AdminID: admin.ID, CsrfToken: csrf, MfaPending: mfaPending,
			CreatedAt: now, LastSeenAt: now, IdleExpiresAt: params.IdleExpiresAt,
			AbsoluteExpiresAt: params.AbsoluteExpiresAt, RecentAuthAt: now, IpHash: ipHash, UserAgent: &ua,
		},
		Admin: admin,
	}, nil
}

// Load returns the session of the request cookie, sliding its idle expiry.
func (s *Sessions) Load(ctx context.Context, r *http.Request) (*Current, error) {
	c, err := r.Cookie(s.CookieName())
	if err != nil || len(c.Value) != 43 {
		return nil, ErrNoSession
	}
	row, err := s.Q.GetSessionWithAdmin(ctx, HashToken(c.Value))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, err
	}
	now := s.now()
	if now.Sub(row.Session.LastSeenAt) > touchEvery {
		if err := s.Q.TouchSession(ctx, dbq.TouchSessionParams{ID: row.Session.ID, IdleExpiresAt: now.Add(IdleTimeout)}); err != nil {
			return nil, err
		}
	}
	return &Current{Session: row.Session, Admin: row.Admin}, nil
}

// Rotate replaces the session token (login, 2FA, password or 2FA change) and
// keeps its state. With othersToo, every other session of the admin ends too.
func (s *Sessions) Rotate(ctx context.Context, w http.ResponseWriter, cur *Current, othersToo bool) (*Current, error) {
	if othersToo {
		if err := s.Q.DeleteAdminSessions(ctx, cur.Admin.ID); err != nil {
			return nil, err
		}
	} else if err := s.Q.DeleteSession(ctx, cur.Session.ID); err != nil {
		return nil, err
	}
	ua := ""
	if cur.Session.UserAgent != nil {
		ua = *cur.Session.UserAgent
	}
	return s.Start(ctx, w, cur.Admin, false, cur.Session.IpHash, ua)
}

// End deletes the session and clears the cookie.
func (s *Sessions) End(ctx context.Context, w http.ResponseWriter, cur *Current) error {
	s.clearCookie(w)
	if cur == nil {
		return nil
	}
	return s.Q.DeleteSession(ctx, cur.Session.ID)
}

// ClearCookie removes the cookie without touching the database.
func (s *Sessions) ClearCookie(w http.ResponseWriter) { s.clearCookie(w) }

func (s *Sessions) setCookie(w http.ResponseWriter, token string, expires time.Time) {
	// Secure follows the site scheme: on in production (https), off only for http://localhost.
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: HttpOnly and SameSite=Lax set; Secure as above
		Name:     s.CookieName(),
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		Secure:   s.Secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Sessions) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: same flags as setCookie
		Name: s.CookieName(), Value: "", Path: "/", MaxAge: -1,
		Secure: s.Secure, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}
