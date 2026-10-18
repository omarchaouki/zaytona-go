package auth

import (
	"bufio"
	"crypto/subtle"
	_ "embed"
	"strings"
	"unicode/utf8"
)

// MinPasswordLength is the shortest new password. New-password rules
// (SECURITY.md section 6): 12 to 128 characters, not one of the most common
// passwords, not containing the email name, not the current one. No
// composition rules.
const MinPasswordLength = 12

// Password problems, mapped to i18n keys by the handlers.
const (
	PasswordOK = iota
	PasswordTooShort
	PasswordTooLong
	PasswordCommon
	PasswordContainsEmail
	PasswordSame
)

//go:embed common-passwords.txt
var commonPasswordsTxt string

var commonPasswords = func() map[string]bool {
	m := map[string]bool{}
	sc := bufio.NewScanner(strings.NewReader(commonPasswordsTxt))
	for sc.Scan() {
		if w := strings.TrimSpace(sc.Text()); w != "" && !strings.HasPrefix(w, "#") {
			m[strings.ToLower(w)] = true
		}
	}
	return m
}()

// CheckNewPassword returns PasswordOK or the first problem found.
func CheckNewPassword(next, email, current string) int {
	n := utf8.RuneCountInString(next)
	switch {
	case n < MinPasswordLength:
		return PasswordTooShort
	case len(next) > MaxPasswordLength:
		return PasswordTooLong
	case next == current:
		return PasswordSame
	}
	lower := strings.ToLower(next)
	if commonPasswords[lower] || commonPasswords[strings.TrimRight(lower, "0123456789!.")] {
		return PasswordCommon
	}
	if name, _, _ := strings.Cut(strings.ToLower(email), "@"); len(name) >= 4 && strings.Contains(lower, name) {
		return PasswordContainsEmail
	}
	return PasswordOK
}

func constantEqual(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
