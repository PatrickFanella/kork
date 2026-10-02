// Package catalog defines the services, block types, and themes a page can use.
package catalog

import (
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// Input describes what the user types for a service.
type Input string

const (
	InputHandle Input = "handle"
	InputURL    Input = "url"
	InputText   Input = "text"
	InputEmail  Input = "email"
	InputPhone  Input = "phone"
)

// Service is a destination such as Instagram, Cash App or a Bitcoin address.
type Service struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Input       Input  `json:"input"`
	Placeholder string `json:"placeholder"`
	Prefix      string `json:"prefix"`
	CopyOnly    bool   `json:"copy_only"`
	// Icon is a key into the icon set; empty falls back to the category icon.
	Icon string `json:"icon,omitempty"`

	// template builds the URL from a normalized handle; "{v}" is replaced.
	template string
	// hosts restricts pasted URLs (InputURL, or full URLs given for a handle).
	hosts []string
	// handle validates a normalized handle; nil means defaultHandle.
	handle *regexp.Regexp
	// qrScheme, for copy-only payment addresses, prefixes the QR payload (e.g. "bitcoin:").
	qrScheme string
}

// Category groups services in the editor.
type Category struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

var Categories = []Category{
	{"social", "Social"},
	{"video", "Video & streaming"},
	{"music", "Music & audio"},
	{"creator", "Creator & shop"},
	{"payment", "Payments & tips"},
	{"crypto", "Crypto"},
	{"contact", "Contact & messaging"},
	{"web", "Web & professional"},
}

var (
	defaultHandle = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	subdomainSafe = regexp.MustCompile(`^[A-Za-z0-9-]{1,63}$`)
	domainHandle  = regexp.MustCompile(`^[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$`)
	fediHandle    = regexp.MustCompile(`^@?([A-Za-z0-9_.-]{1,64})@([A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+)$`)
	cashtag       = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,20}$`)
	cryptoAddr    = regexp.MustCompile(`^[A-Za-z0-9:]{20,120}$`)
	lnAddress     = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$`)
	emailRe       = regexp.MustCompile(`^[^\s@<>"']+@[^\s@<>"']+\.[^\s@<>"']+$`)
	phoneRe       = regexp.MustCompile(`^\+?[0-9 ()./-]{5,24}$`)
	discordName   = regexp.MustCompile(`^[a-z0-9_.]{2,32}$`)
)

func h(key, name, cat, tmpl, placeholder string, hosts ...string) Service {
	return Service{Key: key, Name: name, Category: cat, Input: InputHandle, Placeholder: placeholder,
		Prefix: "@", template: tmpl, hosts: hosts, Icon: key}
}

func u(key, name, cat, placeholder string, hosts ...string) Service {
	return Service{Key: key, Name: name, Category: cat, Input: InputURL, Placeholder: placeholder,
		hosts: hosts, Icon: key}
}

func noPrefix(s Service, prefix string) Service { s.Prefix = prefix; return s }

func withHandle(s Service, re *regexp.Regexp) Service { s.handle = re; return s }

func copyOnly(key, name, cat, placeholder, scheme string, re *regexp.Regexp) Service {
	return Service{Key: key, Name: name, Category: cat, Input: InputText, Placeholder: placeholder,
		CopyOnly: true, handle: re, qrScheme: scheme, Icon: key}
}

var services = []Service{
	// Social
	h("x", "X / Twitter", "social", "https://x.com/{v}", "username", "x.com", "twitter.com"),
	h("instagram", "Instagram", "social", "https://www.instagram.com/{v}/", "username", "instagram.com"),
	h("tiktok", "TikTok", "social", "https://www.tiktok.com/@{v}", "username", "tiktok.com"),
	h("threads", "Threads", "social", "https://www.threads.net/@{v}", "username", "threads.net", "threads.com"),
	withHandle(h("bluesky", "Bluesky", "social", "https://bsky.app/profile/{v}", "name.bsky.social", "bsky.app"), domainHandle),
	{Key: "mastodon", Name: "Mastodon / Fediverse", Category: "social", Input: InputHandle, Placeholder: "@you@mastodon.social", Icon: "mastodon"},
	h("facebook", "Facebook", "social", "https://www.facebook.com/{v}", "page or username", "facebook.com", "fb.com"),
	h("snapchat", "Snapchat", "social", "https://www.snapchat.com/add/{v}", "username", "snapchat.com"),
	h("pinterest", "Pinterest", "social", "https://www.pinterest.com/{v}/", "username", "pinterest.com"),
	noPrefix(h("reddit", "Reddit", "social", "https://www.reddit.com/user/{v}", "username", "reddit.com"), "u/"),
	h("tumblr", "Tumblr", "social", "https://www.tumblr.com/{v}", "blog name", "tumblr.com"),
	h("vsco", "VSCO", "social", "https://vsco.co/{v}", "username", "vsco.co"),
	h("letterboxd", "Letterboxd", "social", "https://letterboxd.com/{v}/", "username", "letterboxd.com"),
	u("goodreads", "Goodreads", "social", "https://www.goodreads.com/user/show/...", "goodreads.com"),
	u("lemmy", "Lemmy", "social", "https://lemmy.world/u/you"),
	u("cara", "Cara", "social", "https://cara.app/you", "cara.app"),

	// Video & streaming
	h("youtube", "YouTube", "video", "https://www.youtube.com/@{v}", "handle", "youtube.com", "youtu.be"),
	h("twitch", "Twitch", "video", "https://www.twitch.tv/{v}", "channel", "twitch.tv"),
	h("kick", "Kick", "video", "https://kick.com/{v}", "channel", "kick.com"),
	h("vimeo", "Vimeo", "video", "https://vimeo.com/{v}", "username", "vimeo.com"),
	u("rumble", "Rumble", "video", "https://rumble.com/c/...", "rumble.com"),
	u("nebula", "Nebula", "video", "https://nebula.tv/...", "nebula.tv"),
	u("peertube", "PeerTube", "video", "https://instance/c/channel"),

	// Music & audio
	u("spotify", "Spotify", "music", "https://open.spotify.com/artist/...", "open.spotify.com", "spotify.link"),
	u("apple_music", "Apple Music", "music", "https://music.apple.com/...", "music.apple.com"),
	u("apple_podcasts", "Apple Podcasts", "music", "https://podcasts.apple.com/...", "podcasts.apple.com"),
	h("soundcloud", "SoundCloud", "music", "https://soundcloud.com/{v}", "username", "soundcloud.com", "on.soundcloud.com"),
	withHandle(noPrefix(h("bandcamp", "Bandcamp", "music", "https://{v}.bandcamp.com/", "artist subdomain", "bandcamp.com"), ""), subdomainSafe),
	u("youtube_music", "YouTube Music", "music", "https://music.youtube.com/channel/...", "music.youtube.com"),
	u("tidal", "TIDAL", "music", "https://tidal.com/browse/artist/...", "tidal.com", "listen.tidal.com"),
	u("deezer", "Deezer", "music", "https://www.deezer.com/artist/...", "deezer.com"),
	h("mixcloud", "Mixcloud", "music", "https://www.mixcloud.com/{v}/", "username", "mixcloud.com"),
	h("audiomack", "Audiomack", "music", "https://audiomack.com/{v}", "username", "audiomack.com"),
	u("pocket_casts", "Pocket Casts", "music", "https://pca.st/...", "pca.st", "pocketcasts.com"),

	// Creator & shop
	h("patreon", "Patreon", "creator", "https://www.patreon.com/{v}", "creator name", "patreon.com"),
	withHandle(noPrefix(h("substack", "Substack", "creator", "https://{v}.substack.com/", "publication subdomain", "substack.com"), ""), subdomainSafe),
	h("medium", "Medium", "creator", "https://medium.com/@{v}", "username", "medium.com"),
	h("etsy", "Etsy", "creator", "https://www.etsy.com/shop/{v}", "shop name", "etsy.com"),
	withHandle(noPrefix(h("itchio", "itch.io", "creator", "https://{v}.itch.io/", "username", "itch.io"), ""), subdomainSafe),
	h("throne", "Throne wishlist", "creator", "https://throne.com/{v}", "username", "throne.com"),
	u("amazon_wishlist", "Amazon wishlist", "creator", "https://www.amazon.com/hz/wishlist/ls/...", "amazon.com", "amazon.co.uk", "amazon.ca", "amazon.de", "a.co"),
	withHandle(noPrefix(h("gumroad", "Gumroad", "creator", "https://{v}.gumroad.com/", "username", "gumroad.com"), ""), subdomainSafe),
	u("shopify", "Shop / store", "creator", "https://yourstore.com"),
	h("onlyfans", "OnlyFans", "creator", "https://onlyfans.com/{v}", "username", "onlyfans.com"),
	h("fansly", "Fansly", "creator", "https://fansly.com/{v}", "username", "fansly.com"),
	h("artstation", "ArtStation", "creator", "https://www.artstation.com/{v}", "username", "artstation.com"),
	h("behance", "Behance", "creator", "https://www.behance.net/{v}", "username", "behance.net"),
	h("dribbble", "Dribbble", "creator", "https://dribbble.com/{v}", "username", "dribbble.com"),
	h("deviantart", "DeviantArt", "creator", "https://www.deviantart.com/{v}", "username", "deviantart.com"),
	u("gofundme", "GoFundMe", "creator", "https://www.gofundme.com/f/...", "gofundme.com", "gofund.me"),
	u("kickstarter", "Kickstarter", "creator", "https://www.kickstarter.com/projects/...", "kickstarter.com"),

	// Payments & tips
	noPrefix(h("paypal", "PayPal", "payment", "https://paypal.me/{v}", "paypal.me name", "paypal.me", "paypal.com"), "paypal.me/"),
	withHandle(noPrefix(h("cashapp", "Cash App", "payment", "https://cash.app/${v}", "cashtag", "cash.app"), "$"), cashtag),
	h("venmo", "Venmo", "payment", "https://venmo.com/u/{v}", "username", "venmo.com", "account.venmo.com"),
	noPrefix(h("kofi", "Ko-fi", "payment", "https://ko-fi.com/{v}", "username", "ko-fi.com"), "ko-fi.com/"),
	noPrefix(h("buymeacoffee", "Buy Me a Coffee", "payment", "https://buymeacoffee.com/{v}", "username", "buymeacoffee.com"), "buymeacoffee.com/"),
	h("github_sponsors", "GitHub Sponsors", "payment", "https://github.com/sponsors/{v}", "username", "github.com"),
	noPrefix(h("liberapay", "Liberapay", "payment", "https://liberapay.com/{v}", "username", "liberapay.com"), ""),
	noPrefix(h("revolut", "Revolut", "payment", "https://revolut.me/{v}", "revtag", "revolut.me"), "@"),
	noPrefix(h("monzo", "Monzo", "payment", "https://monzo.me/{v}", "username", "monzo.me"), "monzo.me/"),
	noPrefix(h("strike", "Strike", "payment", "https://strike.me/{v}", "username", "strike.me"), ""),
	u("stripe", "Stripe payment link", "payment", "https://buy.stripe.com/...", "buy.stripe.com", "donate.stripe.com"),
	u("wise", "Wise", "payment", "https://wise.com/pay/me/...", "wise.com"),
	copyOnly("zelle", "Zelle", "payment", "email or phone registered with Zelle", "", regexp.MustCompile(`^[^\s<>"']{5,64}$`)),
	copyOnly("apple_cash", "Apple Cash", "payment", "phone or email", "", regexp.MustCompile(`^[^\s<>"']{5,64}$`)),

	// Crypto
	copyOnly("bitcoin", "Bitcoin", "crypto", "bc1q...", "bitcoin:", cryptoAddr),
	copyOnly("lightning", "Lightning address", "crypto", "you@getalby.com", "lightning:", lnAddress),
	copyOnly("ethereum", "Ethereum", "crypto", "0x...", "ethereum:", cryptoAddr),
	copyOnly("solana", "Solana", "crypto", "address", "solana:", cryptoAddr),
	copyOnly("monero", "Monero", "crypto", "4...", "monero:", cryptoAddr),
	copyOnly("litecoin", "Litecoin", "crypto", "ltc1...", "litecoin:", cryptoAddr),
	copyOnly("dogecoin", "Dogecoin", "crypto", "D...", "dogecoin:", cryptoAddr),
	copyOnly("usdc", "USDC", "crypto", "address (say which chain in the label)", "", cryptoAddr),

	// Contact & messaging
	{Key: "email", Name: "Email", Category: "contact", Input: InputEmail, Placeholder: "you@example.com", Icon: "email"},
	{Key: "phone", Name: "Phone call", Category: "contact", Input: InputPhone, Placeholder: "+1 555 123 4567", Icon: "phone"},
	{Key: "sms", Name: "Text message", Category: "contact", Input: InputPhone, Placeholder: "+1 555 123 4567", Icon: "sms"},
	{Key: "whatsapp", Name: "WhatsApp", Category: "contact", Input: InputPhone, Placeholder: "+1 555 123 4567", Icon: "whatsapp"},
	h("telegram", "Telegram", "contact", "https://t.me/{v}", "username", "t.me", "telegram.me"),
	u("signal", "Signal", "contact", "https://signal.me/#eu/...", "signal.me", "signal.group"),
	u("discord_invite", "Discord server", "contact", "https://discord.gg/invite", "discord.gg", "discord.com"),
	copyOnly("discord", "Discord username", "contact", "username", "", discordName),
	u("matrix", "Matrix", "contact", "https://matrix.to/#/@you:matrix.org", "matrix.to"),
	h("calendly", "Calendly", "contact", "https://calendly.com/{v}", "username", "calendly.com"),
	noPrefix(h("calcom", "Cal.com", "contact", "https://cal.com/{v}", "username", "cal.com"), "cal.com/"),

	// Web & professional
	u("website", "Website", "web", "https://example.com"),
	noPrefix(h("linkedin", "LinkedIn", "web", "https://www.linkedin.com/in/{v}", "profile id", "linkedin.com"), "in/"),
	h("github", "GitHub", "web", "https://github.com/{v}", "username", "github.com"),
	h("gitlab", "GitLab", "web", "https://gitlab.com/{v}", "username", "gitlab.com"),
	h("codeberg", "Codeberg", "web", "https://codeberg.org/{v}", "username", "codeberg.org"),
	u("steam", "Steam", "web", "https://steamcommunity.com/id/...", "steamcommunity.com", "store.steampowered.com"),
	u("rss", "RSS feed", "web", "https://example.com/feed.xml"),
	u("blog", "Blog", "web", "https://example.com/blog"),
}

var serviceIndex = func() map[string]*Service {
	m := make(map[string]*Service, len(services))
	for i := range services {
		m[services[i].Key] = &services[i]
	}
	return m
}()

// Services returns every service sorted by category order, then name.
func Services() []Service {
	order := map[string]int{}
	for i, c := range Categories {
		order[c.Key] = i
	}
	out := append([]Service(nil), services...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return order[out[i].Category] < order[out[j].Category]
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// LookupService finds a service by key.
func LookupService(key string) (*Service, bool) {
	s, ok := serviceIndex[key]
	return s, ok
}

// Resolved is a normalized service value ready for rendering.
type Resolved struct {
	// Value is the normalized value stored back into the block.
	Value string
	// Display is what visitors see, e.g. "@alice" or "$alice".
	Display string
	// URL is the destination; empty for copy-only services.
	URL string
	// QRPayload is what the QR code encodes (URL, or scheme:address).
	QRPayload string
}

var ErrInvalidValue = errors.New("invalid value")

// Resolve validates and normalizes a raw value typed by the user.
func (s *Service) Resolve(raw string) (Resolved, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return Resolved{}, errors.New("value is required")
	}
	if len(v) > 300 {
		return Resolved{}, errors.New("value is too long")
	}
	switch {
	case s.CopyOnly:
		re := s.handle
		if re == nil {
			re = defaultHandle
		}
		if !re.MatchString(v) {
			return Resolved{}, errors.New("that doesn't look like a valid " + s.Name + " value")
		}
		payload := v
		if s.qrScheme != "" {
			payload = s.qrScheme + v
		}
		return Resolved{Value: v, Display: v, QRPayload: payload}, nil
	case s.Key == "mastodon":
		return resolveFediverse(v)
	case s.Input == InputEmail:
		v = strings.TrimPrefix(v, "mailto:")
		if !emailRe.MatchString(v) {
			return Resolved{}, errors.New("enter a valid email address")
		}
		link := "mailto:" + v
		return Resolved{Value: v, Display: v, URL: link, QRPayload: link}, nil
	case s.Input == InputPhone:
		if !phoneRe.MatchString(v) {
			return Resolved{}, errors.New("enter a phone number with country code, e.g. +1 555 123 4567")
		}
		digits := phoneDigits(v)
		var link string
		switch s.Key {
		case "whatsapp":
			link = "https://wa.me/" + strings.TrimPrefix(digits, "+")
		case "sms":
			link = "sms:" + digits
		default:
			link = "tel:" + digits
		}
		return Resolved{Value: v, Display: v, URL: link, QRPayload: link}, nil
	case s.Input == InputURL:
		link, err := checkURL(v, s.hosts)
		if err != nil {
			return Resolved{}, err
		}
		return Resolved{Value: link, Display: displayURL(link), URL: link, QRPayload: link}, nil
	}

	// Handle input: accept a pasted profile URL from an allowed host as-is.
	if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
		link, err := checkURL(v, s.hosts)
		if err != nil {
			return Resolved{}, err
		}
		return Resolved{Value: link, Display: displayURL(link), URL: link, QRPayload: link}, nil
	}
	handle := strings.TrimPrefix(v, s.Prefix)
	handle = strings.TrimPrefix(handle, "@")
	re := s.handle
	if re == nil {
		re = defaultHandle
	}
	if !re.MatchString(handle) {
		return Resolved{}, errors.New("that doesn't look like a valid " + s.Name + " name")
	}
	link := strings.ReplaceAll(s.template, "{v}", url.PathEscape(handle))
	return Resolved{Value: handle, Display: s.Prefix + handle, URL: link, QRPayload: link}, nil
}

func resolveFediverse(v string) (Resolved, error) {
	if strings.HasPrefix(v, "https://") {
		link, err := checkURL(v, nil)
		if err != nil {
			return Resolved{}, err
		}
		return Resolved{Value: link, Display: displayURL(link), URL: link, QRPayload: link}, nil
	}
	m := fediHandle.FindStringSubmatch(v)
	if m == nil {
		return Resolved{}, errors.New("use the full handle, e.g. @you@mastodon.social")
	}
	user, host := m[1], strings.ToLower(m[2])
	link := "https://" + host + "/@" + user
	return Resolved{Value: "@" + user + "@" + host, Display: "@" + user + "@" + host, URL: link, QRPayload: link}, nil
}

func phoneDigits(v string) string {
	var b strings.Builder
	for i, r := range v {
		if r == '+' && i == 0 || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// CheckLinkURL validates a free-form link destination.
func CheckLinkURL(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(v, "mailto:"):
		if !emailRe.MatchString(strings.TrimPrefix(v, "mailto:")) {
			return "", errors.New("invalid email link")
		}
		return v, nil
	case strings.HasPrefix(v, "tel:"), strings.HasPrefix(v, "sms:"):
		if !phoneRe.MatchString(v[4:]) {
			return "", errors.New("invalid phone link")
		}
		return v, nil
	}
	if !strings.Contains(v, "://") {
		v = "https://" + v
	}
	return checkURL(v, nil)
}

// checkURL accepts http(s) URLs, optionally restricted to hosts (and their subdomains).
func checkURL(raw string, hosts []string) (string, error) {
	if len(raw) > 2000 {
		return "", errors.New("URL is too long")
	}
	p, err := url.Parse(raw)
	if err != nil || (p.Scheme != "https" && p.Scheme != "http") || p.Host == "" || p.User != nil {
		return "", errors.New("enter a full http(s) URL")
	}
	host := strings.ToLower(p.Hostname())
	if !strings.Contains(host, ".") {
		return "", errors.New("enter a full http(s) URL")
	}
	if len(hosts) > 0 {
		ok := false
		for _, allowed := range hosts {
			if host == allowed || strings.HasSuffix(host, "."+allowed) {
				ok = true
				break
			}
		}
		if !ok {
			return "", errors.New("URL must be on " + strings.Join(hosts, " or "))
		}
	}
	return p.String(), nil
}

func displayURL(link string) string {
	p, err := url.Parse(link)
	if err != nil {
		return link
	}
	d := strings.TrimPrefix(p.Host, "www.") + strings.TrimSuffix(p.EscapedPath(), "/")
	if len(d) > 48 {
		d = d[:47] + "…"
	}
	return d
}
