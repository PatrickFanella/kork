package catalog

import (
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	cases := []struct {
		service, in, url, display string
	}{
		{"instagram", "@patrick__eff", "https://www.instagram.com/patrick__eff/", "@patrick__eff"},
		{"instagram", "https://www.instagram.com/subcult_tv/", "https://www.instagram.com/subcult_tv/", "instagram.com/subcult_tv"},
		{"x", "subcult_tv", "https://x.com/subcult_tv", "@subcult_tv"},
		{"cashapp", "$subcult", "https://cash.app/$subcult", "$subcult"},
		{"paypal", "subcult", "https://paypal.me/subcult", "paypal.me/subcult"},
		{"venmo", "@sub-cult", "https://venmo.com/u/sub-cult", "@sub-cult"},
		{"bluesky", "subcult.bsky.social", "https://bsky.app/profile/subcult.bsky.social", "@subcult.bsky.social"},
		{"mastodon", "@alice@mastodon.social", "https://mastodon.social/@alice", "@alice@mastodon.social"},
		{"bandcamp", "subcult", "https://subcult.bandcamp.com/", "subcult"},
		{"email", "hi@subcult.tv", "mailto:hi@subcult.tv", "hi@subcult.tv"},
		{"whatsapp", "+1 (555) 123-4567", "https://wa.me/15551234567", "+1 (555) 123-4567"},
		{"phone", "+1 555 123 4567", "tel:+15551234567", "+1 555 123 4567"},
		{"spotify", "https://open.spotify.com/artist/abc", "https://open.spotify.com/artist/abc", "open.spotify.com/artist/abc"},
	}
	for _, c := range cases {
		svc, ok := LookupService(c.service)
		if !ok {
			t.Fatalf("missing service %q", c.service)
		}
		r, err := svc.Resolve(c.in)
		if err != nil {
			t.Errorf("%s %q: %v", c.service, c.in, err)
			continue
		}
		if r.URL != c.url || r.Display != c.display {
			t.Errorf("%s %q: got url=%q display=%q, want %q %q", c.service, c.in, r.URL, r.Display, c.url, c.display)
		}
	}
}

func TestResolveRejects(t *testing.T) {
	cases := []struct{ service, in string }{
		{"instagram", "https://evil.example.com/instagram.com"},
		{"instagram", "bad handle with spaces"},
		{"instagram", "javascript:alert(1)"},
		{"spotify", "https://open.spotify.com.evil.com/x"},
		{"cashapp", "$1nvalid!"},
		{"bandcamp", "a.b"},
		{"website", "javascript:alert(1)"},
		{"website", "https://user:pass@example.com"},
		{"email", "not-an-email"},
		{"bitcoin", "short"},
		{"mastodon", "alice"},
		{"instagram", ""},
	}
	for _, c := range cases {
		svc, _ := LookupService(c.service)
		if _, err := svc.Resolve(c.in); err == nil {
			t.Errorf("%s %q: expected error", c.service, c.in)
		}
	}
}

func TestCopyOnlyQRPayload(t *testing.T) {
	svc, _ := LookupService("bitcoin")
	r, err := svc.Resolve("bc1qxy2kgdygjrsqtzq2n0yrf2493p83kkfjhx0wlh")
	if err != nil {
		t.Fatal(err)
	}
	if r.URL != "" || r.QRPayload != "bitcoin:bc1qxy2kgdygjrsqtzq2n0yrf2493p83kkfjhx0wlh" {
		t.Fatalf("unexpected %+v", r)
	}
}

func TestCatalogConsistency(t *testing.T) {
	cats := map[string]bool{}
	for _, c := range Categories {
		cats[c.Key] = true
	}
	seen := map[string]bool{}
	for _, s := range Services() {
		if seen[s.Key] {
			t.Errorf("duplicate service %q", s.Key)
		}
		seen[s.Key] = true
		if !cats[s.Category] {
			t.Errorf("%s: unknown category %q", s.Key, s.Category)
		}
		if s.Input == InputHandle && s.Key != "mastodon" && !strings.Contains(s.template, "{v}") {
			t.Errorf("%s: handle service without template", s.Key)
		}
		if s.Input == InputHandle && s.Key != "mastodon" && !strings.HasPrefix(s.template, "https://") {
			t.Errorf("%s: template must be https", s.Key)
		}
	}
}

func TestThemeNormalize(t *testing.T) {
	th, field, err := Theme{Preset: "paper"}.Normalize()
	if err != nil {
		t.Fatalf("%s: %v", field, err)
	}
	if th.Colors.BG != "#f4efe6" || th.Font != "serif" || th.Layout != "stack" {
		t.Fatalf("preset not applied: %+v", th)
	}
	for _, bad := range []Theme{
		{Preset: "nope"},
		{Colors: Colors{BG: "red"}},
		{Colors: Colors{BG: "#fff;}body{"}},
		{Font: "comic"},
		{Background: Background{Kind: "image"}},
		{Background: Background{Kind: "video"}},
	} {
		if _, _, err := bad.Normalize(); err == nil {
			t.Errorf("expected error for %+v", bad)
		}
	}
}
