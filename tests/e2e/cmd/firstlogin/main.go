// Command firstlogin completes the forced password change of a freshly seeded
// owner, so CI can run the e2e scenarios and the admin audit with a normal
// account (test databases only):
//
//	BASE_URL=http://localhost:3000 ADMIN_EMAIL=... ADMIN_PASSWORD=<seed> NEW_PASSWORD=<new> go run ./tests/e2e/cmd/firstlogin
//
// Passwords are read from the environment and never printed.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

var csrfRe = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "firstlogin:", err)
		os.Exit(1)
	}
}

func run() error {
	base := strings.TrimRight(os.Getenv("BASE_URL"), "/")
	if base == "" {
		base = "http://localhost:3000"
	}
	email, old, next := os.Getenv("ADMIN_EMAIL"), os.Getenv("ADMIN_PASSWORD"), os.Getenv("NEW_PASSWORD")
	if email == "" || old == "" || len(next) < 12 {
		return errors.New("set ADMIN_EMAIL, ADMIN_PASSWORD and NEW_PASSWORD (12+ characters)")
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	ctx := context.Background()

	// post returns the path the redirects ended on, the status and the body.
	post := func(path string, form url.Values) (string, int, string, error) {
		// BASE_URL comes from the operator running this test tool.
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, strings.NewReader(form.Encode())) //nolint:gosec // G704: operator-supplied test URL
		if err != nil {
			return "", 0, "", err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := client.Do(req) //nolint:gosec // G704: operator-supplied test URL
		if err != nil {
			return "", 0, "", err
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return resp.Request.URL.Path, resp.StatusCode, string(body), err
	}

	where, status, body, err := post("/admin/login", url.Values{"email": {email}, "password": {old}})
	if err != nil {
		return err
	}
	switch where {
	case "/admin":
		fmt.Println("no password change pending")
		return nil
	case "/admin/password":
	default:
		return fmt.Errorf("sign-in ended on %s (%d)", where, status)
	}
	m := csrfRe.FindStringSubmatch(body)
	if m == nil {
		return errors.New("no CSRF token on the password page")
	}
	where, status, _, err = post("/admin/password", url.Values{
		"csrf": {m[1]}, "username": {email}, "current": {old}, "next": {next}, "confirm": {next},
	})
	if err != nil {
		return err
	}
	if where != "/admin" {
		return fmt.Errorf("password change refused (%d on %s)", status, where)
	}
	fmt.Println("first-login password change done")
	return nil
}
