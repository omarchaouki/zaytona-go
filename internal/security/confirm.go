package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
)

// ConfirmationTokens sign order references for the confirmation URL
// (/{locale}/order/{reference}?t=...): references are sequential, so the page
// is only shown with the token handed out when the order was placed.
type ConfirmationTokens struct{ key []byte }

// NewConfirmationTokens uses the "confirmation-token" key derived from APP_SECRET.
func NewConfirmationTokens(key []byte) *ConfirmationTokens { return &ConfirmationTokens{key: key} }

// For returns the token of a reference (22 URL-safe characters, 128 bits).
func (c *ConfirmationTokens) For(reference string) string {
	m := hmac.New(sha256.New, c.key)
	m.Write([]byte("order-view:" + reference))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)[:16])
}

// Valid compares in constant time.
func (c *ConfirmationTokens) Valid(reference, token string) bool {
	return len(token) == 22 && hmac.Equal([]byte(token), []byte(c.For(reference)))
}
