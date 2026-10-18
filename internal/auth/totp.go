package auth

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"image/png"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// TOTP (RFC 6238): 30 s steps, 6 digits, one step of clock drift each way.
// Secrets are stored encrypted (AES-256-GCM, key derived from APP_SECRET with
// a key id byte in front for rotation). A step is accepted once.
const (
	totpPeriod       = 30
	totpSkew         = 1
	secretKeyID      = byte(1)
	recoveryCount    = 10
	totpIssuer       = "Huilerie El Hadaji"
	recoveryAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"
)

var errSecretBox = errors.New("auth: cannot decrypt TOTP secret")

// SecretBox encrypts TOTP secrets at rest.
type SecretBox struct{ aead cipher.AEAD }

// NewSecretBox uses the 32-byte "totp-encryption" key.
func NewSecretBox(key []byte) (*SecretBox, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SecretBox{aead: aead}, nil
}

// Seal returns keyID || nonce || ciphertext.
func (b *SecretBox) Seal(secret string) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte{secretKeyID}, nonce...)
	return b.aead.Seal(out, nonce, []byte(secret), []byte{secretKeyID}), nil
}

// Open decrypts a sealed secret.
func (b *SecretBox) Open(sealed []byte) (string, error) {
	ns := b.aead.NonceSize()
	if len(sealed) < 1+ns || sealed[0] != secretKeyID {
		return "", errSecretBox
	}
	plain, err := b.aead.Open(nil, sealed[1:1+ns], sealed[1+ns:], []byte{secretKeyID})
	if err != nil {
		return "", errSecretBox
	}
	return string(plain), nil
}

// Enrollment is a new TOTP secret to show (QR code + text) before activation.
type Enrollment struct {
	Secret string
	URL    string
	QRData string // data:image/png;base64,... (rendered on the server, no third party)
}

// NewEnrollment generates a secret for account (the admin email).
func NewEnrollment(account string) (Enrollment, error) {
	key, err := totp.Generate(totp.GenerateOpts{Issuer: totpIssuer, AccountName: account, Period: totpPeriod, Digits: otp.DigitsSix})
	if err != nil {
		return Enrollment{}, err
	}
	return EnrollmentFor(key.Secret(), account)
}

// EnrollmentFor rebuilds the QR code of an existing (pending) secret.
func EnrollmentFor(secret, account string) (Enrollment, error) {
	key, err := totp.Generate(totp.GenerateOpts{Issuer: totpIssuer, AccountName: account, Period: totpPeriod, Digits: otp.DigitsSix, Secret: mustDecodeSecret(secret)})
	if err != nil {
		return Enrollment{}, err
	}
	img, err := key.Image(240, 240)
	if err != nil {
		return Enrollment{}, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return Enrollment{}, err
	}
	return Enrollment{
		Secret: key.Secret(),
		URL:    key.URL(),
		QRData: "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()),
	}, nil
}

func mustDecodeSecret(s string) []byte {
	b, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(s))
	if err != nil {
		return nil
	}
	return b
}

// MatchTOTP returns the time step a 6-digit code matches (now, or one step
// either side), so the caller can refuse a step that was already used.
func MatchTOTP(secret, code string, now time.Time) (step int64, ok bool) {
	code = strings.TrimSpace(strings.ReplaceAll(code, " ", ""))
	if len(code) != 6 {
		return 0, false
	}
	current := now.Unix() / totpPeriod
	for d := -totpSkew; d <= totpSkew; d++ {
		s := current + int64(d)
		want, err := totp.GenerateCodeCustom(secret, time.Unix(s*totpPeriod, 0), totp.ValidateOpts{Period: totpPeriod, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
		if err == nil && constantEqual(want, code) {
			return s, true
		}
	}
	return 0, false
}

// NewRecoveryCodes returns codes to show once ("abcd-efgh") and their hashes to store.
func NewRecoveryCodes() (codes []string, hashes [][]byte, err error) {
	for range recoveryCount {
		raw := make([]byte, 8)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, err
		}
		var sb strings.Builder
		for i, b := range raw {
			if i == 4 {
				sb.WriteByte('-')
			}
			sb.WriteByte(recoveryAlphabet[int(b)%len(recoveryAlphabet)])
		}
		code := sb.String()
		codes = append(codes, code)
		hashes = append(hashes, HashRecoveryCode(code))
	}
	return codes, hashes, nil
}

// HashRecoveryCode normalizes (case, spaces, dash) then hashes a recovery code.
func HashRecoveryCode(code string) []byte {
	c := strings.ToLower(strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(code)))
	sum := sha256.Sum256([]byte("recovery:" + c))
	return sum[:]
}
