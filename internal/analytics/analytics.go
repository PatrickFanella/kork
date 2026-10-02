// Package analytics turns request headers into anonymous aggregate labels,
// computes click-through, signs unsubscribe links, and runs the stats jobs
// (retention cleanup and the weekly email).
package analytics

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"math"
	"net/url"
	"regexp"
	"strings"
)

const (
	Direct         = "direct"
	UnknownCountry = "unknown"
	maxReferrerLen = 100
)

var hostRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)

// ReferrerHost reduces a Referer header to a lowercase host without "www.".
// An empty, unparsable or non-http(s) referrer, or one from selfHost, is
// Direct. Paths, queries and ports are dropped.
func ReferrerHost(referer, selfHost string) string {
	if referer == "" || len(referer) > 2048 {
		return Direct
	}
	u, err := url.Parse(referer)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return Direct
	}
	host := normHost(u.Hostname())
	if host == "" || !hostRe.MatchString(host) || host == normHost(stripPort(selfHost)) {
		return Direct
	}
	if len(host) > maxReferrerLen {
		host = strings.TrimRight(host[:maxReferrerLen], ".-")
	}
	return host
}

func normHost(h string) string {
	h = strings.TrimSuffix(strings.ToLower(h), ".")
	return strings.TrimPrefix(h, "www.")
}

func stripPort(h string) string {
	if i := strings.LastIndex(h, ":"); i > 0 && !strings.Contains(h[i:], "]") {
		return h[:i]
	}
	return h
}

// Country normalizes Cloudflare's CF-IPCountry header to an ISO 3166 alpha-2
// code. Missing values, XX (unknown) and T1 (Tor) become UnknownCountry.
func Country(header string) string {
	c := strings.ToUpper(strings.TrimSpace(header))
	if len(c) != 2 || c[0] < 'A' || c[0] > 'Z' || c[1] < 'A' || c[1] > 'Z' || c == "XX" || c == "T1" {
		return UnknownCountry
	}
	return c
}

// ClickThrough is clicks divided by views, rounded to four decimals; zero when
// there were no views. Cached page loads are not counted as views, so the
// result can exceed 1.
func ClickThrough(clicks, views int64) float64 {
	if views <= 0 || clicks <= 0 {
		return 0
	}
	return math.Round(float64(clicks)/float64(views)*10000) / 10000
}

const unsubscribePurpose = "weekly-stats-unsubscribe:"

// UnsubscribeToken signs a user ID for the weekly email's unsubscribe link.
// Tokens do not expire, so links in old emails keep working.
func UnsubscribeToken(key []byte, userID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(userID)) + "." + base64.RawURLEncoding.EncodeToString(mac(key, userID))
}

// VerifyUnsubscribe returns the user ID of a valid token.
func VerifyUnsubscribe(key []byte, token string) (string, bool) {
	idPart, sigPart, ok := strings.Cut(token, ".")
	if !ok || len(token) > 200 {
		return "", false
	}
	id, err1 := base64.RawURLEncoding.DecodeString(idPart)
	sig, err2 := base64.RawURLEncoding.DecodeString(sigPart)
	if err1 != nil || err2 != nil || len(id) == 0 || !hmac.Equal(sig, mac(key, string(id))) {
		return "", false
	}
	return string(id), true
}

func mac(key []byte, userID string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(unsubscribePurpose + userID))
	return h.Sum(nil)
}
