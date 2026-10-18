// Package auth holds password hashing, sessions, CSRF tokens and TOTP.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters for new hashes (OWASP minimum: 19 MiB, 2 passes, 1 lane).
const (
	argonMemory  uint32 = 19456
	argonTime    uint32 = 2
	argonThreads uint8  = 1
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
)

// MaxPasswordLength caps the input to argon2 (and the new-password rule).
const MaxPasswordLength = 128

var errBadHash = errors.New("auth: malformed argon2id hash")

// HashPassword returns an argon2id PHC string:
// $argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash> (unpadded standard base64).
func HashPassword(password string) (string, error) {
	if len(password) > MaxPasswordLength {
		return "", errors.New("auth: password too long")
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword checks a password against a PHC hash using the parameters
// encoded in the hash. needsRehash is true when they are below the current target.
func VerifyPassword(encoded, password string) (ok, needsRehash bool) {
	if len(password) > MaxPasswordLength {
		return false, false
	}
	p, err := parsePHC(encoded)
	if err != nil {
		return false, false
	}
	key := argon2.IDKey([]byte(password), p.salt, p.time, p.memory, p.threads, uint32(len(p.key))) // #nosec G115 -- key length bounded by parsePHC
	if subtle.ConstantTimeCompare(key, p.key) != 1 {
		return false, false
	}
	return true, p.memory < argonMemory || p.time < argonTime
}

// dummyHash is verified when the email is unknown, so both paths cost one argon2 run.
var dummyHash = func() string {
	h, err := HashPassword("timing-equalizer-not-a-real-password")
	if err != nil {
		panic(err)
	}
	return h
}()

// VerifyDummy burns the same time as a real verification.
func VerifyDummy(password string) { VerifyPassword(dummyHash, password) }

type phc struct {
	memory, time uint32
	threads      uint8
	salt, key    []byte
}

func parsePHC(s string) (phc, error) {
	// "", "argon2id", "v=19", "m=..,t=..,p=..", salt, hash
	parts := strings.Split(s, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return phc{}, errBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return phc{}, errBadHash
	}
	var p phc
	var threads uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &threads); err != nil {
		return phc{}, errBadHash
	}
	// Refuse absurd parameters from a tampered row (DoS guard).
	if p.memory < 8 || p.memory > 1<<20 || p.time < 1 || p.time > 10 || threads < 1 || threads > 16 {
		return phc{}, errBadHash
	}
	p.threads = uint8(threads)
	b64 := base64.RawStdEncoding
	var err error
	if p.salt, err = b64.DecodeString(parts[4]); err != nil || len(p.salt) < 8 {
		return phc{}, errBadHash
	}
	if p.key, err = b64.DecodeString(parts[5]); err != nil || len(p.key) < 16 || len(p.key) > 64 {
		return phc{}, errBadHash
	}
	return p, nil
}
