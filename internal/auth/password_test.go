package auth

import (
	"encoding/base64"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

func TestHashAndVerify(t *testing.T) {
	h, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("unexpected PHC prefix: %s", h)
	}
	if ok, rehash := VerifyPassword(h, "correct horse battery staple"); !ok || rehash {
		t.Fatalf("ok=%v rehash=%v", ok, rehash)
	}
	if ok, _ := VerifyPassword(h, "wrong"); ok {
		t.Fatal("wrong password accepted")
	}
}

func TestVerifyRejectsMalformedAndAbusiveHashes(t *testing.T) {
	for _, h := range []string{
		"",
		"$argon2i$v=19$m=19456,t=2,p=1$c2FsdHNhbHQ$aGFzaGhhc2hoYXNoaGFzaA",
		"$argon2id$v=19$m=99999999,t=2,p=1$c2FsdHNhbHQ$aGFzaGhhc2hoYXNoaGFzaA",
		"$argon2id$v=19$m=19456,t=99,p=1$c2FsdHNhbHQ$aGFzaGhhc2hoYXNoaGFzaA",
		"$argon2id$v=19$m=19456,t=2,p=1$!!$aGFzaGhhc2hoYXNoaGFzaA",
	} {
		if ok, _ := VerifyPassword(h, "x"); ok {
			t.Errorf("accepted %q", h)
		}
	}
}

func TestWeakParametersNeedRehash(t *testing.T) {
	salt := []byte("0123456789abcdef")
	key := argon2.IDKey([]byte("pw"), salt, 1, 4096, 1, 32)
	b64 := base64.RawStdEncoding
	weak := "$argon2id$v=19$m=4096,t=1,p=1$" + b64.EncodeToString(salt) + "$" + b64.EncodeToString(key)
	ok, rehash := VerifyPassword(weak, "pw")
	if !ok || !rehash {
		t.Fatalf("weak hash: ok=%v rehash=%v", ok, rehash)
	}
}

func TestTooLongPasswordRefused(t *testing.T) {
	if _, err := HashPassword(strings.Repeat("a", MaxPasswordLength+1)); err == nil {
		t.Fatal("over-long password hashed")
	}
}
