package blocks

import (
	"encoding/json"
	"net/url"
	"strings"

	"git.subcult.tv/subculture-collective/zelda/internal/catalog"
)

// Destination is a web URL a block sends visitors to, with the data key it
// came from.
type Destination struct {
	Field string
	URL   string
}

// Destinations lists the http(s) destinations in validated block data: link,
// image and contact URLs, service profile URLs, embeds, and QR codes that
// encode a web address.
func Destinations(typ string, raw json.RawMessage) []Destination {
	var out []Destination
	add := func(field, u string) {
		u = strings.TrimSpace(u)
		if p, err := url.Parse(u); err == nil && (p.Scheme == "http" || p.Scheme == "https") && p.Host != "" {
			out = append(out, Destination{field, u})
		}
	}
	switch typ {
	case "link":
		var b Link
		_ = json.Unmarshal(raw, &b)
		add("url", b.URL)
	case "image":
		var b Image
		_ = json.Unmarshal(raw, &b)
		add("url", b.URL)
	case "contact":
		var b Contact
		_ = json.Unmarshal(raw, &b)
		add("url", b.URL)
	case "embed":
		var b Embed
		_ = json.Unmarshal(raw, &b)
		add("url", b.URL)
	case "qr":
		var b QR
		_ = json.Unmarshal(raw, &b)
		add("value", b.Value)
	case "service":
		var b Service
		_ = json.Unmarshal(raw, &b)
		if svc, ok := catalog.LookupService(b.Service); ok {
			if r, err := svc.Resolve(b.Value); err == nil {
				add("value", r.URL)
			}
		}
	}
	return out
}
