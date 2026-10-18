package httpx

import "strings"

// CSPFeatures lists the optional third parties enabled for a request. Their
// origins are added to the policy only when the feature is on.
type CSPFeatures struct {
	MetaPixel bool   // pixel enabled (and consent given when required)
	Turnstile bool   // Cloudflare Turnstile on public forms
	MapEmbed  bool   // Google Maps iframe loaded on click
	UmamiHost string // e.g. https://cloud.umami.is, empty when off
	// HTTPS adds upgrade-insecure-requests (never on plain-http localhost).
	HTTPS bool
}

// BuildCSP returns the Content-Security-Policy for one request.
func BuildCSP(nonce string, f CSPFeatures) string {
	script := []string{"'self'", "'nonce-" + nonce + "'", "'strict-dynamic'"}
	img := []string{"'self'", "data:"}
	connect := []string{"'self'"}
	var frame []string

	if f.MetaPixel {
		script = append(script, "https://connect.facebook.net")
		img = append(img, "https://www.facebook.com")
		connect = append(connect, "https://www.facebook.com", "https://connect.facebook.net")
	}
	if f.UmamiHost != "" {
		script = append(script, f.UmamiHost)
		connect = append(connect, f.UmamiHost)
	}
	if f.Turnstile {
		script = append(script, "https://challenges.cloudflare.com")
		frame = append(frame, "https://challenges.cloudflare.com")
	}
	if f.MapEmbed {
		frame = append(frame, "https://www.google.com")
	}

	d := []string{
		"default-src 'none'",
		"script-src " + strings.Join(script, " "),
		"style-src 'self'",
		"img-src " + strings.Join(img, " "),
		"font-src 'self'",
		"connect-src " + strings.Join(connect, " "),
	}
	if len(frame) > 0 {
		d = append(d, "frame-src "+strings.Join(frame, " "))
	}
	d = append(d,
		"form-action 'self'",
		"frame-ancestors 'none'",
		"base-uri 'none'",
		"object-src 'none'",
		"manifest-src 'self'",
		"worker-src 'self'",
	)
	if f.HTTPS {
		d = append(d, "upgrade-insecure-requests")
	}
	return strings.Join(d, "; ")
}
