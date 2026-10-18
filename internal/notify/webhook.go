package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// Webhook posts order.created events. The URL is admin-controlled, so it is
// treated as hostile (SECURITY.md section 11): HTTPS only in production, no
// credentials in the URL, every dialled address checked against private,
// loopback, link-local, multicast, CGNAT and unspecified ranges in the dialer
// itself (so DNS rebinding cannot swap it), no redirects, 5 s timeout,
// response capped at 64 KiB and discarded, body signed with HMAC-SHA256.
type Webhook struct {
	secret []byte
	dev    bool // development: http and loopback allowed (local receivers)
	client *http.Client
}

// ErrBlockedAddress is returned when the webhook host resolves to a forbidden range.
var ErrBlockedAddress = errors.New("webhook: address not allowed")

// NewWebhook builds the client. dev allows http:// and loopback receivers.
func NewWebhook(secret []byte, dev bool) *Webhook {
	w := &Webhook{secret: secret, dev: dev}
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil || !w.allowed(ap.Addr()) {
				return ErrBlockedAddress
			}
			return nil
		},
	}
	w.client = &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			Proxy:                 nil, // never through an env proxy: the check must see the real peer
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 5 * time.Second,
			MaxIdleConns:          4,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return w
}

var blocked = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
		"172.16.0.0/12", "192.0.0.0/24", "192.168.0.0/16", "198.18.0.0/15", "224.0.0.0/4", "240.0.0.0/4",
		"::/128", "::1/128", "fc00::/7", "fe80::/10", "ff00::/8", "64:ff9b::/96",
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// allowed reports whether ip may be dialled (IPv4-mapped IPv6 is unmapped first).
func (w *Webhook) allowed(ip netip.Addr) bool {
	ip = ip.Unmap()
	if w.dev && ip.IsLoopback() {
		return true
	}
	if !ip.IsValid() || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	for _, p := range blocked {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

// ValidateURL checks a webhook URL before it is saved or used.
func (w *Webhook) ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	switch {
	case err != nil || u.Host == "" || len(raw) > 2000:
		return errors.New("webhook: invalid URL")
	case u.User != nil:
		return errors.New("webhook: credentials in the URL are not allowed")
	case u.Scheme == "https":
	case u.Scheme == "http" && w.dev:
	default:
		return errors.New("webhook: https only")
	}
	// An IP literal is checked now too (names are checked at dial time).
	if ip, err := netip.ParseAddr(strings.Trim(u.Hostname(), "[]")); err == nil && !w.allowed(ip.Unmap()) {
		return ErrBlockedAddress
	}
	return nil
}

// Post sends {"event":"order.created","order":...} with X-Zaytona-Signature:
// sha256=<hex HMAC-SHA256 of the body>.
func (w *Webhook) Post(ctx context.Context, raw string, o Order) error {
	if err := w.ValidateURL(raw); err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{"event": "order.created", "order": o})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, raw, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "zaytona-webhook/1")
	req.Header.Set("X-Zaytona-Signature", "sha256="+Sign(w.secret, body))
	resp, err := w.client.Do(req) // #nosec G704 -- admin URL, checked by ValidateURL and the dialer
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("webhook responded %d", resp.StatusCode)
	}
	return nil
}

// Sign returns the hex HMAC-SHA256 of body (receivers recompute it to verify).
func Sign(secret, body []byte) string {
	m := hmac.New(sha256.New, secret)
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}
