// Package billing talks to Stripe and Patreon and decides how long each
// grants Pro. It holds no database code: the store keeps per-source state and
// plans.Resolve turns it into the account's plan.
package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

// SignatureTolerance is how far a Stripe-Signature timestamp may be from now.
// It matches the default in Stripe's own libraries.
const SignatureTolerance = 5 * time.Minute

var (
	ErrNoSignature      = errors.New("missing or malformed Stripe-Signature header")
	ErrBadSignature     = errors.New("no Stripe-Signature v1 signature matches the payload")
	ErrSignatureExpired = errors.New("Stripe-Signature timestamp is outside the tolerance")
)

// VerifySignature checks a Stripe webhook per
// https://docs.stripe.com/webhooks#verify-manually: the header is a
// comma-separated list of key=value pairs with one timestamp t and one or more
// v1 signatures (other schemes such as v0 are ignored). The expected
// signature is the hex HMAC-SHA256 of "t.payload" keyed with the endpoint's
// signing secret. At least one v1 must match (constant-time compare), and t
// must be within tolerance of now in either direction.
func VerifySignature(payload []byte, header, secret string, tolerance time.Duration, now time.Time) error {
	var (
		ts    int64
		haveT bool
		sigs  [][]byte
	)
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return ErrNoSignature
			}
			ts, haveT = n, true
		case "v1":
			if b, err := hex.DecodeString(v); err == nil {
				sigs = append(sigs, b)
			}
		}
	}
	if !haveT || len(sigs) == 0 {
		return ErrNoSignature
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(ts, 10)))
	mac.Write([]byte("."))
	mac.Write(payload)
	want := mac.Sum(nil)
	matched := false
	for _, s := range sigs {
		if hmac.Equal(s, want) {
			matched = true
		}
	}
	if !matched {
		return ErrBadSignature
	}
	if d := now.Sub(time.Unix(ts, 0)); d > tolerance || d < -tolerance {
		return ErrSignatureExpired
	}
	return nil
}

// SignForTest builds a Stripe-Signature header for payload at t. Tests and
// local tooling use it; the server never signs.
func SignForTest(payload []byte, secret string, t time.Time) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(payload)
	return "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}
