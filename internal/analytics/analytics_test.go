package analytics

import (
	"strings"
	"testing"
	"time"
)

func TestReferrerHost(t *testing.T) {
	self := "kork.example:8080"
	cases := map[string]string{
		"":                                    Direct,
		"https://www.Instagram.com/p/abc?x=1": "instagram.com",
		"http://t.co/xyz":                     "t.co",
		"https://news.ycombinator.com:443/":   "news.ycombinator.com",
		"https://kork.example/alice":          Direct, // same host, port ignored
		"https://www.kork.example/":           Direct,
		"android-app://com.google.android.gm": Direct,
		"not a url":                           Direct,
		"https://user:pw@evil.example/":       "evil.example",
		"https://bad_host!.example/":          Direct,
		"https://xn--bcher-kva.example/":      "xn--bcher-kva.example",
	}
	for in, want := range cases {
		if got := ReferrerHost(in, self); got != want {
			t.Errorf("ReferrerHost(%q) = %q, want %q", in, got, want)
		}
	}
	long := "https://" + strings.Repeat("a", 60) + "." + strings.Repeat("b", 60) + ".example/"
	if got := ReferrerHost(long, self); len(got) > maxReferrerLen || strings.HasSuffix(got, ".") {
		t.Errorf("long referrer not capped: %q (%d)", got, len(got))
	}
}

func TestCountry(t *testing.T) {
	cases := map[string]string{"US": "US", " de ": "DE", "": UnknownCountry, "XX": UnknownCountry,
		"T1": UnknownCountry, "USA": UnknownCountry, "1A": UnknownCountry}
	for in, want := range cases {
		if got := Country(in); got != want {
			t.Errorf("Country(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClickThrough(t *testing.T) {
	cases := []struct {
		clicks, views int64
		want          float64
	}{
		{0, 0, 0}, {5, 0, 0}, {0, 10, 0}, {1, 4, 0.25}, {1, 3, 0.3333}, {2, 3, 0.6667}, {6, 4, 1.5},
	}
	for _, c := range cases {
		if got := ClickThrough(c.clicks, c.views); got != c.want {
			t.Errorf("ClickThrough(%d, %d) = %v, want %v", c.clicks, c.views, got, c.want)
		}
	}
	if Percent(0.25) != "25%" || Percent(0.0333) != "3.3%" || Percent(0) != "0%" {
		t.Errorf("Percent: %s %s %s", Percent(0.25), Percent(0.0333), Percent(0))
	}
}

func TestUnsubscribeToken(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	id := "6f1c9a52-2b8e-4c35-9d0e-3a2f1b4c5d6e"
	tok := UnsubscribeToken(key, id)
	if got, ok := VerifyUnsubscribe(key, tok); !ok || got != id {
		t.Fatalf("valid token rejected: %q %v", got, ok)
	}
	if _, ok := VerifyUnsubscribe([]byte("another key, another key, abcdef"), tok); ok {
		t.Error("token accepted with the wrong key")
	}
	other := UnsubscribeToken(key, "00000000-0000-0000-0000-000000000001")
	forged := strings.SplitN(tok, ".", 2)[0] + "." + strings.SplitN(other, ".", 2)[1]
	for _, bad := range []string{"", "abc", tok + "x", forged, "." + strings.SplitN(tok, ".", 2)[1]} {
		if _, ok := VerifyUnsubscribe(key, bad); ok {
			t.Errorf("bad token accepted: %q", bad)
		}
	}
}

func TestWeekStart(t *testing.T) {
	for in, want := range map[string]string{
		"2026-10-02T15:04:05Z": "2026-09-28", // Friday
		"2026-09-28T00:00:00Z": "2026-09-28", // Monday
		"2026-10-04T23:59:59Z": "2026-09-28", // Sunday
		"2026-10-05T00:00:01Z": "2026-10-05",
	} {
		tm, _ := time.Parse(time.RFC3339, in)
		if got := WeekStart(tm).Format(time.DateOnly); got != want {
			t.Errorf("WeekStart(%s) = %s, want %s", in, got, want)
		}
	}
}
