// Package feed reads the newest item from an RSS or Atom feed (including
// YouTube channel feeds) and finds a YouTube channel ID in a channel page.
package feed

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrNoItems means the document parsed as a feed but has no usable item.
var ErrNoItems = errors.New("the feed has no items")

// ErrNotFeed means the document is not RSS or Atom.
var ErrNotFeed = errors.New("that address isn't an RSS or Atom feed")

// Item is one feed entry, with links resolved against the feed URL.
type Item struct {
	// Key identifies the item across fetches: its guid/id, else its link.
	Key       string
	Title     string
	Link      string
	Published time.Time // zero when the feed gives no date
	// Thumbnail is a remote image URL, or "".
	Thumbnail string
}

const nsMedia = "http://search.yahoo.com/mrss/"

type link struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
	Text string `xml:",chardata"`
}

type media struct {
	URL    string `xml:"url,attr"`
	Medium string `xml:"medium,attr"`
	Type   string `xml:"type,attr"`
}

type mediaGroup struct {
	Thumbnails []media `xml:"http://search.yahoo.com/mrss/ thumbnail"`
	Contents   []media `xml:"http://search.yahoo.com/mrss/ content"`
}

type text struct {
	XMLName xml.Name
	Text    string `xml:",chardata"`
}

type entry struct {
	// Titles also collects media:title; item() skips that one.
	Titles     []text       `xml:"title"`
	Links      []link       `xml:"link"`
	GUID       string       `xml:"guid"`
	ID         string       `xml:"id"`
	PubDate    string       `xml:"pubDate"`
	Published  string       `xml:"published"`
	Updated    string       `xml:"updated"`
	DCDate     string       `xml:"http://purl.org/dc/elements/1.1/ date"`
	Enclosures []media      `xml:"enclosure"`
	Thumbnails []media      `xml:"http://search.yahoo.com/mrss/ thumbnail"`
	Contents   []media      `xml:"http://search.yahoo.com/mrss/ content"`
	Groups     []mediaGroup `xml:"http://search.yahoo.com/mrss/ group"`
	ITunes     []link       `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd image"`
	VideoID    string       `xml:"http://www.youtube.com/xml/schemas/2015 videoId"`
}

type document struct {
	XMLName xml.Name
	Channel struct {
		Items []entry `xml:"item"`
	} `xml:"channel"`
	Items   []entry `xml:"item"`  // RSS 1.0 puts items beside the channel
	Entries []entry `xml:"entry"` // Atom
}

// Latest parses an RSS 2.0, RSS 1.0 or Atom document and returns its newest
// item: the one with the latest date, or the first item when none is dated.
// base is the feed's own URL, used to resolve relative links.
func Latest(body []byte, base *url.URL) (Item, error) {
	d := xml.NewDecoder(bytes.NewReader(body))
	d.Strict = false
	d.Entity = xml.HTMLEntity
	d.CharsetReader = charsetReader
	var doc document
	if err := d.Decode(&doc); err != nil {
		return Item{}, ErrNotFeed
	}
	switch strings.ToLower(doc.XMLName.Local) {
	case "rss", "feed", "rdf":
	default:
		return Item{}, ErrNotFeed
	}
	entries := doc.Entries
	entries = append(entries, doc.Channel.Items...)
	entries = append(entries, doc.Items...)
	var best Item
	found := false
	for _, e := range entries {
		it, ok := e.item(base)
		if !ok {
			continue
		}
		if !found || it.Published.After(best.Published) {
			best, found = it, true
		}
	}
	if !found {
		return Item{}, ErrNoItems
	}
	return best, nil
}

func (e entry) item(base *url.URL) (Item, bool) {
	var it Item
	for _, t := range e.Titles {
		if t.XMLName.Space != nsMedia {
			it.Title = clip(strings.Join(strings.Fields(t.Text), " "), 200)
			break
		}
	}
	for _, l := range e.Links {
		href := strings.TrimSpace(l.Href)
		if href == "" {
			href = strings.TrimSpace(l.Text)
		}
		if href == "" || (l.Rel != "" && l.Rel != "alternate") {
			continue
		}
		if u := resolve(base, href); u != "" {
			it.Link = u
			break
		}
	}
	if it.Link == "" && isURL(e.GUID) {
		it.Link = resolve(base, e.GUID)
	}
	if it.Link == "" {
		return Item{}, false
	}
	if it.Title == "" {
		it.Title = it.Link
	}
	it.Key = strings.TrimSpace(e.GUID)
	if it.Key == "" {
		it.Key = strings.TrimSpace(e.ID)
	}
	if it.Key == "" {
		it.Key = it.Link
	}
	it.Key = clip(it.Key, 500)
	for _, s := range []string{e.Published, e.PubDate, e.DCDate, e.Updated} {
		if t, ok := parseDate(s); ok {
			it.Published = t
			break
		}
	}
	it.Thumbnail = e.thumbnail(base)
	return it, true
}

func (e entry) thumbnail(base *url.URL) string {
	var candidates []string
	for _, g := range e.Groups {
		for _, m := range g.Thumbnails {
			candidates = append(candidates, m.URL)
		}
	}
	for _, m := range e.Thumbnails {
		candidates = append(candidates, m.URL)
	}
	images := func(list []media) {
		for _, m := range list {
			if m.Medium == "image" || strings.HasPrefix(m.Type, "image/") {
				candidates = append(candidates, m.URL)
			}
		}
	}
	for _, g := range e.Groups {
		images(g.Contents)
	}
	images(e.Contents)
	images(e.Enclosures)
	for _, l := range e.ITunes {
		candidates = append(candidates, l.Href)
	}
	if id := strings.TrimSpace(e.VideoID); youTubeVideoID.MatchString(id) {
		candidates = append(candidates, "https://i.ytimg.com/vi/"+id+"/hqdefault.jpg")
	}
	for _, c := range candidates {
		if u := resolve(base, strings.TrimSpace(c)); u != "" {
			return u
		}
	}
	return ""
}

var youTubeVideoID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func isURL(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// resolve returns ref as an absolute http(s) URL, or "".
func resolve(base *url.URL, ref string) string {
	if ref == "" || len(ref) > 2000 {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return ""
	}
	return u.String()
}

var dateLayouts = []string{
	time.RFC3339Nano,
	time.RFC1123Z,
	time.RFC1123,
	"Mon, 2 Jan 2006 15:04:05 -0700",
	"Mon, 2 Jan 2006 15:04:05 MST",
	"Mon, 2 Jan 2006 15:04 -0700",
	"2 Jan 2006 15:04:05 -0700",
	time.RFC822Z,
	time.RFC822,
	"2006-01-02T15:04:05",
	"2006-01-02",
}

func parseDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// charsetReader accepts UTF-8 and the Latin-1 family common in older feeds.
func charsetReader(label string, in io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "utf-8", "utf8", "us-ascii", "ascii":
		return in, nil
	case "iso-8859-1", "latin1", "latin-1", "windows-1252", "cp1252":
		b, err := io.ReadAll(in)
		if err != nil {
			return nil, err
		}
		var out strings.Builder
		for _, c := range b {
			out.WriteRune(rune(c))
		}
		return strings.NewReader(out.String()), nil
	}
	return nil, fmt.Errorf("unsupported feed encoding %q", label)
}

var channelIDPatterns = []*regexp.Regexp{
	regexp.MustCompile(`<link rel="canonical" href="https://www\.youtube\.com/channel/(UC[A-Za-z0-9_-]{22})"`),
	regexp.MustCompile(`"externalId":"(UC[A-Za-z0-9_-]{22})"`),
	regexp.MustCompile(`<meta itemprop="identifier" content="(UC[A-Za-z0-9_-]{22})"`),
	regexp.MustCompile(`"channelId":"(UC[A-Za-z0-9_-]{22})"`),
}

// ChannelID finds the channel ID in a YouTube channel page. It prefers the
// canonical link and externalId, which name the page's own channel, over
// channelId, which can also refer to other channels on the page.
func ChannelID(page []byte) (string, bool) {
	for _, re := range channelIDPatterns {
		if m := re.FindSubmatch(page); m != nil {
			return string(m[1]), true
		}
	}
	return "", false
}
