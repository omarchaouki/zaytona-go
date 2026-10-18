package auth

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestSecretBox(t *testing.T) {
	box, err := NewSecretBox(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.Seal("JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("JBSWY3DPEHPK3PXP")) || sealed[0] != secretKeyID {
		t.Fatal("secret not encrypted or key id missing")
	}
	again, _ := box.Seal("JBSWY3DPEHPK3PXP")
	if bytes.Equal(sealed, again) {
		t.Fatal("nonce reused")
	}
	if got, err := box.Open(sealed); err != nil || got != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("open: %q %v", got, err)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := box.Open(sealed); err == nil {
		t.Fatal("tampered secret accepted")
	}
	other, _ := NewSecretBox(bytes.Repeat([]byte{8}, 32))
	if _, err := other.Open(again); err == nil {
		t.Fatal("wrong key accepted")
	}
	if _, err := box.Open([]byte{1, 2}); err == nil {
		t.Fatal("short input accepted")
	}
}

func TestMatchTOTP(t *testing.T) {
	e, err := NewEnrollment("owner@huilerie.example")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(e.QRData, "data:image/png;base64,") || !strings.Contains(e.URL, "issuer=Huilerie") {
		t.Fatalf("enrollment: %+v", e.URL)
	}
	now := time.Date(2026, 10, 16, 12, 0, 15, 0, time.UTC)
	step := now.Unix() / 30
	for _, d := range []int64{-1, 0, 1} {
		code, _ := totp.GenerateCode(e.Secret, now.Add(time.Duration(d*30)*time.Second))
		got, ok := MatchTOTP(e.Secret, code[:3]+" "+code[3:], now)
		if !ok || got != step+d {
			t.Fatalf("drift %d: step %d ok %v", d, got, ok)
		}
	}
	old, _ := totp.GenerateCode(e.Secret, now.Add(-90*time.Second))
	if _, ok := MatchTOTP(e.Secret, old, now); ok {
		t.Fatal("code from 3 steps ago accepted")
	}
	for _, bad := range []string{"", "12345", "1234567", "abcdef"} {
		if _, ok := MatchTOTP(e.Secret, bad, now); ok {
			t.Fatalf("%q accepted", bad)
		}
	}
	// The QR code of a pending secret is rebuilt from the stored secret.
	again, err := EnrollmentFor(e.Secret, "owner@huilerie.example")
	if err != nil || again.Secret != e.Secret {
		t.Fatalf("rebuild: %v", err)
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes, hashes, err := NewRecoveryCodes()
	if err != nil || len(codes) != 10 || len(hashes) != 10 {
		t.Fatalf("%d codes, %v", len(codes), err)
	}
	seen := map[string]bool{}
	for i, c := range codes {
		if len(c) != 9 || c[4] != '-' || seen[c] {
			t.Fatalf("code %q", c)
		}
		seen[c] = true
		if !bytes.Equal(HashRecoveryCode(" "+strings.ToUpper(strings.ReplaceAll(c, "-", ""))+" "), hashes[i]) {
			t.Fatal("normalization differs")
		}
	}
}

func TestCheckNewPassword(t *testing.T) {
	cases := []struct {
		next, email, current string
		want                 int
	}{
		{"short", "a@b.c", "x", PasswordTooShort},
		{strings.Repeat("a", 129), "a@b.c", "x", PasswordTooLong},
		{"same old password", "a@b.c", "same old password", PasswordSame},
		{"Password1234", "a@b.c", "x", PasswordCommon},
		{"azertyuiop123!", "a@b.c", "x", PasswordCommon},
		{"fatima loves olives", "fatima@example.com", "x", PasswordContainsEmail},
		{"olive harvest at dawn", "owner@example.com", "x", PasswordOK},
		{"زيت الزيتون البلدي", "owner@example.com", "x", PasswordOK},
	}
	for _, c := range cases {
		if got := CheckNewPassword(c.next, c.email, c.current); got != c.want {
			t.Errorf("%q: %d, want %d", c.next, got, c.want)
		}
	}
}

func TestTokensAndCSRF(t *testing.T) {
	tok, raw, err := NewToken()
	if err != nil || len(tok) != 43 || len(raw) != 32 {
		t.Fatalf("token %q %v", tok, err)
	}
	if len(HashToken(tok)) != 32 || bytes.Equal(HashToken(tok), raw) {
		t.Fatal("hash")
	}
	if !CSRFValid(raw, CSRFString(raw)) {
		t.Fatal("valid token refused")
	}
	for _, bad := range []string{"", "x", CSRFString(raw)[:42], CSRFString(bytes.Repeat([]byte{1}, 32))} {
		if CSRFValid(raw, bad) {
			t.Fatalf("%q accepted", bad)
		}
	}
}
