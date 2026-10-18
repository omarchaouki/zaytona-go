// Package security holds keyed hashes, signed tokens, anti-bot and rate limits.
package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// Hasher computes keyed hashes for values compared but never stored in clear
// (phones for verified reviews, IPs for rate limits and audit rows).
type Hasher struct{ key []byte }

// NewHasher takes a 32-byte key derived from APP_SECRET for one purpose.
func NewHasher(key []byte) *Hasher { return &Hasher{key: key} }

// Sum returns HMAC-SHA256(key, value).
func (h *Hasher) Sum(value string) []byte {
	m := hmac.New(sha256.New, h.key)
	m.Write([]byte(value))
	return m.Sum(nil)
}

// LogPrefix is a short hex prefix of the keyed hash, enough to correlate log
// lines without exposing the value.
func (h *Hasher) LogPrefix(value string) string {
	return hex.EncodeToString(h.Sum(value)[:6])
}

// RedactPhone masks a phone for logs: +212612345678 -> +2126****78.
func RedactPhone(p string) string {
	if len(p) < 8 {
		return "****"
	}
	return p[:5] + "****" + p[len(p)-2:]
}
