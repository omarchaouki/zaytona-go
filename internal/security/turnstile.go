package security

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const turnstileVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

// Turnstile verifies optional Cloudflare Turnstile tokens (TURNSTILE_* env).
type Turnstile struct {
	secret string
	client *http.Client
}

// NewTurnstile returns nil when no secret is configured (feature off).
func NewTurnstile(secret string) *Turnstile {
	if secret == "" {
		return nil
	}
	return &Turnstile{secret: secret, client: &http.Client{Timeout: 5 * time.Second}}
}

// Verify reports whether the token is valid. remoteIP may be empty.
func (t *Turnstile) Verify(ctx context.Context, token, remoteIP string) bool {
	if token == "" || len(token) > 4096 {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	form := url.Values{"secret": {t.secret}, "response": {token}}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, turnstileVerifyURL, strings.NewReader(form.Encode()))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := t.client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		Success bool `json:"success"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out) != nil {
		return false
	}
	return out.Success
}
