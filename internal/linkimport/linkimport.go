// Package linkimport turns another service's link page (Linktree and
// similar) into Kork blocks. Parsers only read already-fetched HTML; the
// caller fetches with internal/fetch.
package linkimport

import (
	"encoding/json"
	"net/url"
	"strings"
	"unicode/utf8"

	"git.subcult.tv/subculture-collective/zelda/internal/blocks"
	"git.subcult.tv/subculture-collective/zelda/internal/catalog"
	"git.subcult.tv/subculture-collective/zelda/internal/htmlmeta"
)

// MaxItems caps how many blocks one import can propose (the page limit).
const MaxItems = 100

// Entry is one thing found on a link page.
type Entry struct {
	// Heading is true for section titles, which have no URL.
	Heading bool
	URL     string
	Title   string
}

// Profile is what a parser extracted from a page.
type Profile struct {
	Parser    string
	Title     string
	Bio       string
	AvatarURL string
	Entries   []Entry
}

// Parser recognizes one kind of link page. Parse returns ok=false when the
// page is not one it understands, so the next parser can try.
type Parser interface {
	Name() string
	Parse(page *url.URL, doc *htmlmeta.Doc) (*Profile, bool)
}

// Parsers are tried in order; the generic parser accepts any page.
var Parsers = []Parser{Linktree{}, Generic{}}

// Parse runs the parsers over a fetched page.
func Parse(page *url.URL, body []byte) *Profile {
	doc := htmlmeta.Parse(page, body)
	for _, p := range Parsers {
		if prof, ok := p.Parse(page, doc); ok {
			prof.Parser = p.Name()
			prof.Title = clip(prof.Title, 80)
			prof.Bio = clip(prof.Bio, 300)
			if !strings.HasPrefix(prof.AvatarURL, "https://") && !strings.HasPrefix(prof.AvatarURL, "http://") {
				prof.AvatarURL = ""
			}
			return prof
		}
	}
	return &Profile{}
}

// Item is a proposed block, already validated by the blocks package so the
// editor can create it unchanged.
type Item struct {
	Type  string          `json:"type"`
	Data  json.RawMessage `json:"data"`
	Label string          `json:"label"`
}

// Items maps entries to blocks: known destinations become service blocks,
// headings become heading blocks, everything else becomes a link block.
// Invalid and duplicate entries are dropped.
func Items(p *Profile) []Item {
	out := []Item{}
	seen := map[string]bool{}
	add := func(typ string, data any) bool {
		raw, _ := json.Marshal(data)
		v, err := blocks.Validate(typ, raw, nil)
		if err != nil {
			return false
		}
		key := typ + string(v)
		if seen[key] {
			return true
		}
		seen[key] = true
		out = append(out, Item{Type: typ, Data: v, Label: blocks.Label(typ, v)})
		return true
	}
	for _, e := range p.Entries {
		if len(out) >= MaxItems {
			break
		}
		title := clip(e.Title, 80)
		if e.Heading {
			if title != "" {
				add("heading", blocks.Heading{Text: title})
			}
			continue
		}
		if key, value, ok := catalog.MatchURL(e.URL); ok {
			label := ""
			if svc, _ := catalog.LookupService(key); svc != nil && title != "" && !strings.EqualFold(title, svc.Name) {
				label = title
			}
			if add("service", blocks.Service{Service: key, Value: value, Label: label}) {
				continue
			}
		}
		if title == "" {
			title = clip(fallbackTitle(e.URL), 80)
		}
		add("link", blocks.Link{URL: e.URL, Title: title})
	}
	return out
}

func fallbackTitle(raw string) string {
	if i := strings.Index(raw, ":"); i > 0 && (raw[:i] == "mailto" || raw[:i] == "tel") {
		return raw[i+1:]
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	return strings.TrimPrefix(u.Hostname(), "www.") + strings.TrimSuffix(u.Path, "/")
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// sameSite reports whether u is on host or one of its subdomains, ignoring www.
func sameSite(u, host string) bool {
	p, err := url.Parse(u)
	if err != nil {
		return false
	}
	h := strings.TrimPrefix(strings.ToLower(p.Hostname()), "www.")
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	return h == host || strings.HasSuffix(h, "."+host)
}

// Generic collects a page's title, description, image and outbound links.
type Generic struct{}

func (Generic) Name() string { return "generic" }

func (Generic) Parse(page *url.URL, doc *htmlmeta.Doc) (*Profile, bool) {
	return &Profile{Title: doc.Title, Bio: doc.Description, AvatarURL: doc.Image,
		Entries: externalAnchors(page, doc, nil)}, true
}

// externalAnchors returns anchors that leave the page's site, plus mailto
// and tel links, minus any whose host is in skip.
func externalAnchors(page *url.URL, doc *htmlmeta.Doc, skip []string) []Entry {
	var out []Entry
	seen := map[string]bool{}
next:
	for _, a := range doc.Anchors {
		if seen[a.URL] {
			continue
		}
		web := strings.HasPrefix(a.URL, "http://") || strings.HasPrefix(a.URL, "https://")
		if web {
			if page != nil && sameSite(a.URL, page.Hostname()) {
				continue
			}
			for _, h := range skip {
				if sameSite(a.URL, h) {
					continue next
				}
			}
		}
		seen[a.URL] = true
		out = append(out, Entry{URL: a.URL, Title: a.Text})
	}
	return out
}
