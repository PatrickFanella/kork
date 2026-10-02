// Package htmlmeta extracts page metadata (Open Graph and HTML fallbacks),
// anchors and embedded script data from fetched HTML.
package htmlmeta

import (
	"bytes"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// Anchor is an <a href> resolved against the page URL.
type Anchor struct {
	URL  string
	Text string
}

// Doc is what a page says about itself.
type Doc struct {
	Title       string
	Description string
	// Image is an absolute http(s) URL, or empty.
	Image   string
	Anchors []Anchor
	// Scripts maps script element ids to their text, e.g. "__NEXT_DATA__".
	Scripts map[string]string
}

const maxAnchors = 500

// Parse reads an HTML document. base resolves relative URLs.
func Parse(base *url.URL, body []byte) *Doc {
	d := &Doc{Scripts: map[string]string{}}
	root, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return d
	}
	meta := map[string]string{}
	var title string
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "title":
				if title == "" {
					title = text(n)
				}
			case "meta":
				key := strings.ToLower(attr(n, "property"))
				if key == "" {
					key = strings.ToLower(attr(n, "name"))
				}
				if key != "" {
					if _, seen := meta[key]; !seen {
						meta[key] = strings.TrimSpace(attr(n, "content"))
					}
				}
			case "link":
				if strings.EqualFold(attr(n, "rel"), "image_src") {
					if _, seen := meta["image_src"]; !seen {
						meta["image_src"] = attr(n, "href")
					}
				}
			case "script":
				if id := attr(n, "id"); id != "" && n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
					d.Scripts[id] = n.FirstChild.Data
				}
				return
			case "style", "noscript", "template":
				return
			case "a":
				if len(d.Anchors) < maxAnchors {
					if u := Resolve(base, attr(n, "href")); u != "" {
						d.Anchors = append(d.Anchors, Anchor{URL: u, Text: text(n)})
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)

	d.Title = first(meta["og:title"], meta["twitter:title"], title)
	d.Description = first(meta["og:description"], meta["twitter:description"], meta["description"])
	for _, k := range []string{"og:image:secure_url", "og:image", "og:image:url", "twitter:image", "twitter:image:src", "image_src"} {
		if u := Resolve(base, meta[k]); strings.HasPrefix(u, "http") {
			d.Image = u
			break
		}
	}
	return d
}

// Resolve makes href absolute against base. It returns "" for fragments,
// javascript: and other schemes besides http, https, mailto and tel.
func Resolve(base *url.URL, href string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") {
		return ""
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	switch u.Scheme {
	case "http", "https":
		if u.Host == "" {
			return ""
		}
		u.Fragment = ""
		return u.String()
	case "mailto", "tel":
		return u.String()
	}
	return ""
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func text(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

func first(vals ...string) string {
	for _, v := range vals {
		if v = strings.Join(strings.Fields(v), " "); v != "" {
			return v
		}
	}
	return ""
}
