package catalog

import "testing"

func TestMatchURL(t *testing.T) {
	cases := []struct{ in, key, value string }{
		{"https://www.instagram.com/alice/", "instagram", "alice"},
		{"https://instagram.com/alice?utm_source=linktree", "instagram", "alice"},
		{"https://www.tiktok.com/@alice", "tiktok", "alice"},
		{"https://twitter.com/alice", "x", "alice"},
		{"https://x.com/alice", "x", "alice"},
		{"https://www.youtube.com/@alice", "youtube", "alice"},
		{"https://github.com/alice", "github", "alice"},
		{"https://github.com/sponsors/alice", "github_sponsors", "alice"},
		{"https://cash.app/$alice", "cashapp", "alice"},
		{"https://alice.bandcamp.com/", "bandcamp", "alice"},
		{"https://www.linkedin.com/in/alice-a", "linkedin", "alice-a"},
		{"https://open.spotify.com/artist/abc123", "spotify", "https://open.spotify.com/artist/abc123"},
		{"https://music.youtube.com/channel/xyz", "youtube_music", "https://music.youtube.com/channel/xyz"},
		{"mailto:alice@example.com", "email", "alice@example.com"},
		{"tel:+15551234567", "phone", "+15551234567"},
	}
	for _, c := range cases {
		key, value, ok := MatchURL(c.in)
		if !ok || key != c.key || value != c.value {
			t.Errorf("%s: got %q %q %v, want %q %q", c.in, key, value, ok, c.key, c.value)
		}
	}
	for _, in := range []string{
		"https://example.com/alice",
		"https://github.com/alice/repo",
		"https://www.youtube.com/watch?v=abc",
		"https://youtu.be/abc",
		"https://www.instagram.com/",
		"https://bandcamp.com/",
		"javascript:alert(1)",
		"mailto:not-an-email",
	} {
		if key, value, ok := MatchURL(in); ok {
			t.Errorf("%s: should stay a link, got %q %q", in, key, value)
		}
	}
}
