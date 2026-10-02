package feed

import (
	"errors"
	"net/url"
	"os"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

func TestLatest(t *testing.T) {
	cases := []struct {
		file, base string
		want       Item
	}{
		{"youtube.xml", "https://www.youtube.com/feeds/videos.xml?channel_id=UCabcdefghijklmnopqrstuv", Item{
			Key: "yt:video:AAAAAAAAAAA", Title: "Inking a zine cover", Link: "https://www.youtube.com/watch?v=AAAAAAAAAAA",
			Published: time.Date(2026, 9, 28, 15, 0, 0, 0, time.UTC), Thumbnail: "https://i1.ytimg.com/vi/AAAAAAAAAAA/hqdefault.jpg",
		}},
		{"rss.xml", "https://alice.example/feed.xml", Item{
			Key: "https://alice.example/posts/newest", Title: "Newest post & more notes", Link: "https://alice.example/posts/newest",
			Published: time.Date(2026, 10, 1, 23, 0, 0, 0, time.UTC), Thumbnail: "https://cdn.alice.example/newest.jpg",
		}},
		{"atom.xml", "https://bob.example/feed.atom", Item{
			Key: "urn:uuid:2", Title: "Printing at home", Link: "https://bob.example/2026/printing",
			Published: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC),
		}},
	}
	for _, c := range cases {
		got, err := Latest(fixture(t, c.file), mustURL(c.base))
		if err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		if got != c.want {
			t.Errorf("%s:\n got %+v\nwant %+v", c.file, got, c.want)
		}
	}
}

func TestLatestRelativeLinksAndFallbacks(t *testing.T) {
	// The second RSS item alone: relative link and enclosure image resolve
	// against the feed URL, and media:title does not replace the title.
	body := []byte(`<rss version="2.0" xmlns:media="http://search.yahoo.com/mrss/"><channel><item>
<title>Second post</title><media:title>Not the title</media:title><link>/posts/second</link><guid>post-2</guid>
<enclosure url="/img/second.png" type="image/png"/></item></channel></rss>`)
	got, err := Latest(body, mustURL("https://alice.example/feed.xml"))
	if err != nil {
		t.Fatal(err)
	}
	want := Item{Key: "post-2", Title: "Second post", Link: "https://alice.example/posts/second", Thumbnail: "https://alice.example/img/second.png"}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}

	// A YouTube entry without media:thumbnail falls back to i.ytimg.com.
	body = []byte(`<feed xmlns="http://www.w3.org/2005/Atom" xmlns:yt="http://www.youtube.com/xml/schemas/2015"><entry>
<yt:videoId>CCCCCCCCCCC</yt:videoId><title>No thumb</title><link href="https://www.youtube.com/watch?v=CCCCCCCCCCC"/></entry></feed>`)
	got, _ = Latest(body, nil)
	if got.Thumbnail != "https://i.ytimg.com/vi/CCCCCCCCCCC/hqdefault.jpg" {
		t.Errorf("youtube fallback thumbnail: %q", got.Thumbnail)
	}

	// Latin-1 feeds decode.
	body = append([]byte(`<?xml version="1.0" encoding="ISO-8859-1"?><rss><channel><item><title>Caf`), 0xe9)
	body = append(body, []byte(`</title><link>https://x.example/c</link></item></channel></rss>`)...)
	if got, err = Latest(body, nil); err != nil || got.Title != "Café" {
		t.Errorf("latin-1: %+v %v", got, err)
	}
}

func TestLatestErrors(t *testing.T) {
	cases := map[string]error{
		`<html><body>hi</body></html>`:                                                ErrNotFeed,
		`not xml at all`:                                                              ErrNotFeed,
		`<rss><channel><title>x</title></channel></rss>`:                              ErrNoItems,
		`<rss><channel><item><title>no link</title></item></channel></rss>`:           ErrNoItems,
		`<rss><channel><item><link>javascript:alert(1)</link></item></channel></rss>`: ErrNoItems,
	}
	for body, want := range cases {
		if _, err := Latest([]byte(body), mustURL("https://x.example/feed")); !errors.Is(err, want) {
			t.Errorf("%q: got %v, want %v", body, err, want)
		}
	}
}

func TestChannelID(t *testing.T) {
	id, ok := ChannelID(fixture(t, "youtube-channel.html"))
	if !ok || id != "UCabcdefghijklmnopqrstuv" {
		t.Fatalf("ChannelID = %q, %v", id, ok)
	}
	// Only the externalId: still found.
	if id, ok := ChannelID([]byte(`{"externalId":"UC1234567890123456789012"}`)); !ok || id != "UC1234567890123456789012" {
		t.Errorf("externalId only: %q %v", id, ok)
	}
	if _, ok := ChannelID([]byte(`<html>no channel</html>`)); ok {
		t.Error("found an ID in a page without one")
	}
}
