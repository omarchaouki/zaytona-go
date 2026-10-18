package security

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
	"time"

	altcha "github.com/altcha-org/altcha-lib-go/v2"

	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/db/dbtest"
)

var pgURL string

func TestMain(m *testing.M) { os.Exit(dbtest.RunMain(m, &pgURL)) }

func TestFormToken(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	f := &FormTokens{key: []byte("k"), now: func() time.Time { return now }}
	tok := f.Issue()
	if f.Check(tok) != FormTokenTooFast {
		t.Fatal("instant submit accepted")
	}
	now = now.Add(4 * time.Second)
	if f.Check(tok) != FormTokenOK {
		t.Fatal("valid token refused")
	}
	now = now.Add(25 * time.Hour)
	if f.Check(tok) != FormTokenExpired {
		t.Fatal("old token accepted")
	}
	other := &FormTokens{key: []byte("other"), now: f.now}
	for _, bad := range []string{"", "abc", "123.", tok + "x", other.Issue()} {
		if f.Check(bad) != FormTokenInvalid {
			t.Errorf("%q not rejected as invalid", bad)
		}
	}
}

// solve plays the browser: finds the counter and builds the base64 payload.
func solve(t *testing.T, ch altcha.Challenge) string {
	t.Helper()
	sol, err := altcha.SolveChallenge(altcha.SolveChallengeOptions{Challenge: ch, DeriveKey: altcha.DeriveKeySHA()})
	if err != nil || sol == nil {
		t.Fatalf("solve: %v", err)
	}
	raw, err := json.Marshal(altcha.Payload{Challenge: ch, Solution: *sol})
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func TestPowSingleUseReplayForgedExpired(t *testing.T) {
	pool := dbtest.NewDatabase(t, pgURL)
	ctx := context.Background()
	p := NewPow([]byte("secret-key-for-tests-0123456789ab"), dbq.New(pool)).WithMaxCounter(2000)

	ch, err := p.Issue()
	if err != nil {
		t.Fatal(err)
	}
	if ch.Parameters.Algorithm != "SHA-256" || ch.Parameters.KeySignature == "" || ch.Signature == "" {
		t.Fatalf("challenge: %+v", ch)
	}
	payload := solve(t, ch)
	if got, err := p.Check(ctx, payload); err != nil || got != PowOK {
		t.Fatalf("first use: %v %v", got, err)
	}
	if got, _ := p.Check(ctx, payload); got != PowReplayed {
		t.Fatalf("replay: %v", got)
	}

	// Forged: a challenge signed with another key.
	forger := NewPow([]byte("attacker-key-0123456789abcdef0123"), dbq.New(pool)).WithMaxCounter(2000)
	fch, _ := forger.Issue()
	if got, _ := p.Check(ctx, solve(t, fch)); got != PowInvalid {
		t.Fatalf("forged: %v", got)
	}

	// Tampered: raise the expiry of a valid challenge.
	ch2, _ := p.Issue()
	ch2.Parameters.ExpiresAt += 3600
	if got, _ := p.Check(ctx, solve(t, ch2)); got != PowInvalid {
		t.Fatalf("tampered: %v", got)
	}

	// Expired.
	past := p.WithMaxCounter(2000)
	past.now = func() time.Time { return time.Now().Add(-3 * time.Hour) }
	old, _ := past.Issue()
	if got, _ := p.Check(ctx, solve(t, old)); got != PowExpired {
		t.Fatalf("expired: %v", got)
	}

	for _, bad := range []string{"", "!!!", base64.StdEncoding.EncodeToString([]byte("{}"))} {
		if got, _ := p.Check(ctx, bad); got == PowOK {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestBrowserCompatibleDerivation(t *testing.T) {
	// The browser computes SHA-256(salt || nonce || uint32 big-endian counter);
	// web/static/js/lib/pow.js must stay in line with DeriveKeySHA.
	params := altcha.ChallengeParameters{Algorithm: "SHA-256", Cost: 1, KeyLength: 32}
	key, err := altcha.DeriveKeySHA()(params, []byte{1, 2}, []byte{3, 4, 0, 0, 0, 5})
	want := sha256.Sum256([]byte{1, 2, 3, 4, 0, 0, 0, 5})
	if err != nil || !bytes.Equal(key, want[:]) {
		t.Fatalf("derivation changed: %x (want %x) %v", key, want, err)
	}
}
