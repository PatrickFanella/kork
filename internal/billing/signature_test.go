package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"testing"
	"time"
)

// sign computes the v1 signature independently of SignForTest.
func sign(payload, secret string, ts int64) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(strconv.FormatInt(ts, 10) + "." + payload))
	return hex.EncodeToString(m.Sum(nil))
}

func TestVerifySignature(t *testing.T) {
	const secret = "whsec_test_secret"
	payload := `{"id":"evt_1","type":"customer.subscription.updated"}`
	now := time.Unix(1_790_000_000, 0)
	ts := now.Unix() - 30
	good := sign(payload, secret, ts)
	tsS := strconv.FormatInt(ts, 10)

	cases := []struct {
		name   string
		header string
		body   string
		now    time.Time
		want   error
	}{
		{"valid", "t=" + tsS + ",v1=" + good, payload, now, nil},
		{"valid with v0 and spaces", "t=" + tsS + ", v1=" + good + ", v0=deadbeef", payload, now, nil},
		{"helper agrees", SignForTest([]byte(payload), secret, time.Unix(ts, 0)), payload, now, nil},
		{"second of several v1", "t=" + tsS + ",v1=" + sign(payload, "whsec_old", ts) + ",v1=" + good, payload, now, nil},
		{"first of several v1", "t=" + tsS + ",v1=" + good + ",v1=" + sign(payload, "whsec_old", ts), payload, now, nil},
		{"tampered payload", "t=" + tsS + ",v1=" + good, payload + " ", now, ErrBadSignature},
		{"tampered timestamp", "t=" + strconv.FormatInt(ts+1, 10) + ",v1=" + good, payload, now, ErrBadSignature},
		{"wrong secret", "t=" + tsS + ",v1=" + sign(payload, "whsec_other", ts), payload, now, ErrBadSignature},
		{"only v0", "t=" + tsS + ",v0=" + good, payload, now, ErrNoSignature},
		{"no timestamp", "v1=" + good, payload, now, ErrNoSignature},
		{"empty", "", payload, now, ErrNoSignature},
		{"bad timestamp", "t=abc,v1=" + good, payload, now, ErrNoSignature},
		{"expired", "t=" + tsS + ",v1=" + good, payload, time.Unix(ts, 0).Add(SignatureTolerance + time.Second), ErrSignatureExpired},
		{"at tolerance", "t=" + tsS + ",v1=" + good, payload, time.Unix(ts, 0).Add(SignatureTolerance), nil},
		{"from the future", "t=" + tsS + ",v1=" + good, payload, time.Unix(ts, 0).Add(-SignatureTolerance - time.Second), ErrSignatureExpired},
	}
	for _, c := range cases {
		err := VerifySignature([]byte(c.body), c.header, secret, SignatureTolerance, c.now)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
}
