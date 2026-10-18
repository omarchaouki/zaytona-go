package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

// Form token: the page embeds a signed render time; a submission is accepted
// only if it arrives at least MinFill after rendering (people need a few
// seconds, scripts post instantly) and within MaxAge.
const (
	MinFill = 3 * time.Second
	MaxAge  = 24 * time.Hour
)

// FormTokenCheck is the outcome of CheckFormToken.
type FormTokenCheck int

// Form token outcomes.
const (
	FormTokenOK FormTokenCheck = iota
	FormTokenInvalid
	FormTokenTooFast
	FormTokenExpired
)

// FormTokens issues and checks render-time tokens.
type FormTokens struct {
	key []byte
	now func() time.Time
}

// NewFormTokens uses the "form-token" key derived from APP_SECRET.
func NewFormTokens(key []byte) *FormTokens { return &FormTokens{key: key, now: time.Now} }

func (f *FormTokens) sign(ts string) string {
	m := hmac.New(sha256.New, f.key)
	m.Write([]byte("form-token:" + ts))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)[:18])
}

// Issue returns "<unix ms>.<signature>" for now.
func (f *FormTokens) Issue() string { return f.IssueAt(f.now()) }

// IssueAt signs a given render time (tests use a time a few seconds back).
func (f *FormTokens) IssueAt(t time.Time) string {
	ts := strconv.FormatInt(t.UnixMilli(), 10)
	return ts + "." + f.sign(ts)
}

// Check verifies a token's signature and age.
func (f *FormTokens) Check(token string) FormTokenCheck {
	ts, sig, ok := strings.Cut(token, ".")
	if !ok || len(token) > 64 {
		return FormTokenInvalid
	}
	ms, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || !hmac.Equal([]byte(sig), []byte(f.sign(ts))) {
		return FormTokenInvalid
	}
	age := f.now().Sub(time.UnixMilli(ms))
	switch {
	case age < MinFill:
		return FormTokenTooFast
	case age > MaxAge:
		return FormTokenExpired
	}
	return FormTokenOK
}
