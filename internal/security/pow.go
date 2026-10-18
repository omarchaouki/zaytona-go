package security

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"time"

	altcha "github.com/altcha-org/altcha-lib-go/v2"

	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
)

// Proof of work (ALTCHA v2, self-hosted, no third party), same parameters as
// the reference: the server signs a challenge whose answer is a counter it
// picked at random in [PowMinCounter, PowMaxCounter]; the browser finds it by
// hashing (about a second on a phone, in the background while the visitor
// fills the form). Verifying costs one HMAC. Each solution is accepted once.
const (
	PowMinCounter = 5_000
	PowMaxCounter = 60_000
	PowTTL        = 2 * time.Hour
	powAlgorithm  = "SHA-256"
)

// PowCheck is the outcome of Pow.Check.
type PowCheck int

// Proof-of-work outcomes.
const (
	PowOK PowCheck = iota
	PowMissing
	PowInvalid
	PowExpired
	PowReplayed
)

// Pow issues and verifies ALTCHA challenges. Single use is recorded in
// pow_used (SHA-256 of the challenge signature).
type Pow struct {
	sigSecret, keySecret string
	q                    *dbq.Queries
	maxCounter           int
	now                  func() time.Time
}

// NewPow derives its two HMAC secrets from the "altcha-hmac" key.
func NewPow(key []byte, q *dbq.Queries) *Pow {
	derive := func(label string) string {
		m := hmac.New(sha256.New, key)
		m.Write([]byte(label))
		return hex.EncodeToString(m.Sum(nil))
	}
	return &Pow{sigSecret: derive("signature"), keySecret: derive("key"), q: q, maxCounter: PowMaxCounter, now: time.Now}
}

// WithMaxCounter lowers the difficulty (tests).
func (p *Pow) WithMaxCounter(n int) *Pow {
	c := *p
	c.maxCounter = n
	return &c
}

// Issue creates a signed challenge.
func (p *Pow) Issue() (altcha.Challenge, error) {
	lo := min(PowMinCounter, p.maxCounter)
	n, err := rand.Int(rand.Reader, big.NewInt(int64(p.maxCounter-lo+1)))
	if err != nil {
		return altcha.Challenge{}, err
	}
	counter := lo + int(n.Int64())
	expires := p.now().Add(PowTTL)
	return altcha.CreateChallenge(altcha.CreateChallengeOptions{
		Algorithm:              powAlgorithm,
		Cost:                   1,
		Counter:                &counter,
		DeriveKey:              altcha.DeriveKeySHA(),
		ExpiresAt:              &expires,
		HMACSignatureSecret:    p.sigSecret,
		HMACKeySignatureSecret: p.keySecret,
	})
}

// Check verifies the browser's payload (base64 JSON {challenge, solution}) and
// claims it, so a second use of the same solution is a replay.
func (p *Pow) Check(ctx context.Context, payload string) (PowCheck, error) {
	if payload == "" {
		return PowMissing, nil
	}
	if len(payload) > 4096 {
		return PowInvalid, nil
	}
	// Malformed input is a rejection, not a server error: nil errors on purpose.
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return PowInvalid, nil //nolint:nilerr // see above
	}
	var pl altcha.Payload
	if err := json.Unmarshal(raw, &pl); err != nil || pl.Challenge.Signature == "" {
		return PowInvalid, nil //nolint:nilerr // see above
	}
	res, err := altcha.VerifySolution(altcha.VerifySolutionOptions{
		Challenge:              pl.Challenge,
		Solution:               pl.Solution,
		DeriveKey:              altcha.DeriveKeySHA(),
		HMACSignatureSecret:    p.sigSecret,
		HMACKeySignatureSecret: p.keySecret,
	})
	switch {
	case err != nil:
		return PowInvalid, nil //nolint:nilerr // see above
	case res.Expired:
		return PowExpired, nil
	case !res.Verified:
		return PowInvalid, nil
	}
	sum := sha256.Sum256([]byte(pl.Challenge.Signature))
	expires := time.Unix(pl.Challenge.Parameters.ExpiresAt, 0)
	n, err := p.q.UsePowSignature(ctx, dbq.UsePowSignatureParams{SignatureHash: sum[:], ExpiresAt: expires})
	if err != nil {
		return PowInvalid, err
	}
	if n == 0 {
		return PowReplayed, nil
	}
	return PowOK, nil
}
