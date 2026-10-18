package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
)

// NewToken returns a 256-bit random token (URL-safe) for a cookie or form.
func NewToken() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	return base64.RawURLEncoding.EncodeToString(raw), raw, nil
}

// HashToken is what the database stores for a session token (SHA-256).
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// CSRFString is the form/header form of a session CSRF token.
func CSRFString(raw []byte) string { return base64.RawURLEncoding.EncodeToString(raw) }

// CSRFValid compares a submitted token with the session's, in constant time.
func CSRFValid(sessionRaw []byte, submitted string) bool {
	got, err := base64.RawURLEncoding.DecodeString(submitted)
	return err == nil && len(got) == 32 && subtle.ConstantTimeCompare(got, sessionRaw) == 1
}
