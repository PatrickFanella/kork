package blocks

import (
	"encoding/json"
	"html"
	"html/template"
	"net/url"
	"regexp"
	"strings"

	"git.subcult.tv/subculture-collective/zelda/internal/catalog"
	"git.subcult.tv/subculture-collective/zelda/internal/icons"
	"git.subcult.tv/subculture-collective/zelda/internal/qr"
)

// Item is one renderable block for the public page template.
type Item struct {
	ID   string
	Type string
	// Kind selects the template: button, copy, heading, text, divider, image, embed or qr.
	Kind     string
	Href     string
	External bool
	// Rel is "me" for profile links so fediverse verification works.
	Rel      string
	Title    string
	Subtitle string
	Icon     template.HTML
	QR       template.HTML
	Copy     string
	Body     template.HTML
	Image    string
	Alt      string
	// Embed fields
	EmbedSrc    string
	EmbedTitle  string
	EmbedHeight int // fixed pixel height; 0 means 16:9
	Track       bool
}

// Context carries per-request rendering inputs.
type Context struct {
	Host      string // request host, used for Twitch's parent parameter
	UploadURL func(id string) string
	QRFG      string
	QRBG      string
	VCardURL  func(blockID string) string
}

// Block is the stored form the renderer consumes.
type Block struct {
	ID   string
	Type string
	Data json.RawMessage
}

// Build converts stored blocks into render items, skipping blocks that no
// longer validate (for example a service removed from the catalog).
func Build(ctx Context, list []Block) []Item {
	items := make([]Item, 0, len(list))
	for _, b := range list {
		if it, ok := build(ctx, b); ok {
			it.ID, it.Type = b.ID, b.Type
			switch {
			case b.Type == "heading", b.Type == "text", b.Type == "divider", b.Type == "image", b.Type == "embed", b.Type == "qr":
				it.Kind = b.Type
			case it.Href == "" && it.Copy != "":
				it.Kind = "copy"
			default:
				it.Kind = "button"
			}
			items = append(items, it)
		}
	}
	return items
}

func (c Context) qrSVG(data string) template.HTML {
	svg, err := qr.SVG(data, c.QRFG, c.QRBG)
	if err != nil {
		return ""
	}
	return template.HTML(svg) //nolint:gosec // generated from fixed markup and validated colors
}

func build(ctx Context, b Block) (Item, bool) {
	switch b.Type {
	case "link":
		var d Link
		if json.Unmarshal(b.Data, &d) != nil {
			return Item{}, false
		}
		it := Item{Href: d.URL, External: isWeb(d.URL), Title: d.Title, Subtitle: d.Subtitle, Track: true, Icon: icons.Generic("link")}
		if d.ThumbnailID != "" {
			it.Image = ctx.UploadURL(d.ThumbnailID)
		}
		if d.ShowQR {
			it.QR = ctx.qrSVG(d.URL)
		}
		return it, true
	case "service":
		var d Service
		if json.Unmarshal(b.Data, &d) != nil {
			return Item{}, false
		}
		svc, ok := catalog.LookupService(d.Service)
		if !ok {
			return Item{}, false
		}
		r, err := svc.Resolve(d.Value)
		if err != nil {
			return Item{}, false
		}
		title := d.Label
		if title == "" {
			title = svc.Name
		}
		it := Item{Title: title, Subtitle: r.Display, Icon: icons.For(svc.Icon, svc.Category), Track: true}
		if svc.CopyOnly {
			it.Copy = r.Value
		} else {
			it.Href = r.URL
			it.External = isWeb(r.URL)
			if svc.Category == "social" || svc.Category == "video" || svc.Key == "github" || svc.Key == "codeberg" || svc.Key == "website" {
				it.Rel = "me"
			}
		}
		if d.ShowQR || svc.Category == "crypto" {
			it.QR = ctx.qrSVG(r.QRPayload)
		}
		return it, true
	case "heading":
		var d Heading
		if json.Unmarshal(b.Data, &d) != nil {
			return Item{}, false
		}
		return Item{Title: d.Text}, true
	case "text":
		var d Text
		if json.Unmarshal(b.Data, &d) != nil {
			return Item{}, false
		}
		return Item{Body: Autolink(d.Text)}, true
	case "divider":
		return Item{}, true
	case "image":
		var d Image
		if json.Unmarshal(b.Data, &d) != nil {
			return Item{}, false
		}
		return Item{Image: ctx.UploadURL(d.UploadID), Alt: d.Alt, Href: d.URL, External: isWeb(d.URL), Track: d.URL != ""}, true
	case "embed":
		var d Embed
		if json.Unmarshal(b.Data, &d) != nil {
			return Item{}, false
		}
		src, ok := EmbedSource(d.URL, ctx.Host)
		if !ok {
			return Item{}, false
		}
		return Item{EmbedSrc: src.URL, EmbedTitle: src.Provider + " player", EmbedHeight: src.Height}, true
	case "qr":
		var d QR
		if json.Unmarshal(b.Data, &d) != nil {
			return Item{}, false
		}
		svg := ctx.qrSVG(d.Value)
		if svg == "" {
			return Item{}, false
		}
		return Item{Title: d.Label, QR: svg, Copy: d.Value}, true
	case "copy":
		var d Copy
		if json.Unmarshal(b.Data, &d) != nil {
			return Item{}, false
		}
		return Item{Title: d.Label, Subtitle: d.Value, Copy: d.Value, Icon: icons.Generic("copy"), Track: true}, true
	case "contact":
		var d Contact
		if json.Unmarshal(b.Data, &d) != nil {
			return Item{}, false
		}
		sub := d.Org
		if sub == "" {
			sub = d.Name
		}
		return Item{Title: "Save contact", Subtitle: sub, Href: ctx.VCardURL(b.ID), Icon: icons.Generic("contact"), Track: true}, true
	case "map":
		var d Map
		if json.Unmarshal(b.Data, &d) != nil {
			return Item{}, false
		}
		title := d.Label
		if title == "" {
			title = "Location"
		}
		href := "https://www.google.com/maps/search/?api=1&query=" + url.QueryEscape(d.Address)
		return Item{Title: title, Subtitle: d.Address, Href: href, External: true, Icon: icons.Generic("map"), Track: true}, true
	}
	return Item{}, false
}

func isWeb(u string) bool { return strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") }

var urlInText = regexp.MustCompile(`https?://[^\s<>"']+[^\s<>"'.,;:!?)\]]`)

// Autolink escapes text, keeps line breaks, and links bare http(s) URLs.
func Autolink(text string) template.HTML {
	var b strings.Builder
	last := 0
	for _, m := range urlInText.FindAllStringIndex(text, -1) {
		b.WriteString(html.EscapeString(text[last:m[0]]))
		u := text[m[0]:m[1]]
		if _, err := url.Parse(u); err == nil {
			b.WriteString(`<a href="` + html.EscapeString(u) + `" rel="nofollow ugc noopener" target="_blank">` + html.EscapeString(u) + `</a>`)
		} else {
			b.WriteString(html.EscapeString(u))
		}
		last = m[1]
	}
	b.WriteString(html.EscapeString(text[last:]))
	return template.HTML(strings.ReplaceAll(b.String(), "\n", "<br>")) //nolint:gosec // every fragment is escaped above
}

// EmbedSrc is a resolved player URL.
type EmbedSrc struct {
	Provider string
	URL      string
	Height   int
}

var (
	ytID      = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	digits    = regexp.MustCompile(`^[0-9]{1,15}$`)
	spotifyRe = regexp.MustCompile(`^/(?:intl-[a-z-]+/)?(track|album|playlist|artist|episode|show)/([A-Za-z0-9]{10,40})$`)
	twitchCh  = regexp.MustCompile(`^[A-Za-z0-9_]{2,25}$`)
	clipSlug  = regexp.MustCompile(`^[A-Za-z0-9_-]{4,100}$`)
)

// EmbedSource maps a share URL to an embeddable player on an allowlisted host.
func EmbedSource(raw, host string) (EmbedSrc, bool) {
	p, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || p.Scheme != "https" {
		return EmbedSrc{}, false
	}
	h := strings.TrimPrefix(strings.ToLower(p.Hostname()), "www.")
	h = strings.TrimPrefix(h, "m.")
	path := strings.TrimSuffix(p.Path, "/")
	segs := strings.Split(strings.TrimPrefix(path, "/"), "/")
	yt := func(id string) (EmbedSrc, bool) {
		if !ytID.MatchString(id) {
			return EmbedSrc{}, false
		}
		return EmbedSrc{"YouTube", "https://www.youtube-nocookie.com/embed/" + id, 0}, true
	}
	switch h {
	case "youtube.com", "music.youtube.com":
		if path == "/watch" {
			return yt(p.Query().Get("v"))
		}
		if len(segs) == 2 && (segs[0] == "shorts" || segs[0] == "embed" || segs[0] == "live") {
			return yt(segs[1])
		}
	case "youtu.be":
		if len(segs) == 1 {
			return yt(segs[0])
		}
	case "vimeo.com":
		if len(segs) == 1 && digits.MatchString(segs[0]) {
			return EmbedSrc{"Vimeo", "https://player.vimeo.com/video/" + segs[0], 0}, true
		}
	case "open.spotify.com":
		if m := spotifyRe.FindStringSubmatch(path); m != nil {
			height := 352
			if m[1] == "track" || m[1] == "episode" {
				height = 152
			}
			return EmbedSrc{"Spotify", "https://open.spotify.com/embed/" + m[1] + "/" + m[2], height}, true
		}
	case "soundcloud.com":
		if len(segs) >= 1 && segs[0] != "" {
			height := 166
			if len(segs) >= 2 && segs[1] == "sets" {
				height = 450
			}
			clean := "https://soundcloud.com" + path
			return EmbedSrc{"SoundCloud", "https://w.soundcloud.com/player/?url=" + url.QueryEscape(clean) + "&visual=false", height}, true
		}
	case "twitch.tv":
		parent := url.QueryEscape(hostOnly(host))
		if len(segs) == 2 && segs[0] == "videos" && digits.MatchString(segs[1]) {
			return EmbedSrc{"Twitch", "https://player.twitch.tv/?video=" + segs[1] + "&parent=" + parent + "&autoplay=false", 0}, true
		}
		if len(segs) == 3 && segs[1] == "clip" && clipSlug.MatchString(segs[2]) {
			return EmbedSrc{"Twitch", "https://clips.twitch.tv/embed?clip=" + segs[2] + "&parent=" + parent + "&autoplay=false", 0}, true
		}
		if len(segs) == 1 && twitchCh.MatchString(segs[0]) {
			return EmbedSrc{"Twitch", "https://player.twitch.tv/?channel=" + segs[0] + "&parent=" + parent + "&autoplay=false", 0}, true
		}
	case "clips.twitch.tv":
		if len(segs) == 1 && clipSlug.MatchString(segs[0]) {
			return EmbedSrc{"Twitch", "https://clips.twitch.tv/embed?clip=" + segs[0] + "&parent=" + url.QueryEscape(hostOnly(host)) + "&autoplay=false", 0}, true
		}
	case "music.apple.com":
		if len(segs) >= 3 {
			height := 450
			if p.Query().Get("i") != "" || (len(segs) >= 2 && segs[1] == "song") {
				height = 175
			}
			src := "https://embed.music.apple.com" + strings.TrimSuffix(p.EscapedPath(), "/")
			if i := p.Query().Get("i"); digits.MatchString(i) {
				src += "?i=" + i
			}
			return EmbedSrc{"Apple Music", src, height}, true
		}
	}
	return EmbedSrc{}, false
}

func hostOnly(h string) string {
	if i := strings.LastIndex(h, ":"); i > 0 && !strings.Contains(h[i:], "]") {
		return h[:i]
	}
	return h
}
