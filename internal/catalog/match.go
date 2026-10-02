package catalog

import (
	"net/url"
	"strings"
)

// shortLinkHosts carry content IDs, not profile names, in their paths.
var shortLinkHosts = map[string]bool{"youtu.be": true, "on.soundcloud.com": true}

// MatchURL maps a destination URL to a catalog service and the raw value to
// pass to that service's Resolve. Handle services match only plain profile
// URLs (instagram.com/alice → "alice"); URL services match any URL on their
// hosts. mailto: and tel: links map to the email and phone services. ok is
// false when the URL should stay a plain link.
func MatchURL(raw string) (key, value string, ok bool) {
	raw = strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(raw, "mailto:"):
		addr, _, _ := strings.Cut(strings.TrimPrefix(raw, "mailto:"), "?")
		if a, err := url.PathUnescape(addr); err == nil && emailRe.MatchString(a) {
			return "email", a, true
		}
		return "", "", false
	case strings.HasPrefix(raw, "tel:"):
		if n := strings.TrimPrefix(raw, "tel:"); phoneRe.MatchString(n) {
			return "phone", n, true
		}
		return "", "", false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", "", false
	}
	host := strings.ToLower(u.Hostname())

	// Prefer the most specific allowed host: music.youtube.com over youtube.com.
	best := 0
	var candidates []*Service
	for i := range services {
		s := &services[i]
		if s.CopyOnly || (s.Input != InputHandle && s.Input != InputURL) {
			continue
		}
		for _, h := range s.hosts {
			if host != h && !strings.HasSuffix(host, "."+h) {
				continue
			}
			switch {
			case len(h) > best:
				best, candidates = len(h), []*Service{s}
			case len(h) == best:
				candidates = append(candidates, s)
			}
			break
		}
	}
	for _, s := range candidates {
		if s.Input == InputURL {
			if _, err := s.Resolve(raw); err == nil {
				return s.Key, raw, true
			}
			continue
		}
		if handle := handleFromURL(s, u); handle != "" {
			if _, err := s.Resolve(handle); err == nil {
				return s.Key, handle, true
			}
		}
	}
	return "", "", false
}

// handleFromURL extracts the {v} part of a handle service's template from u,
// or returns "" when u is not a plain profile URL.
func handleFromURL(s *Service, u *url.URL) string {
	tmpl := strings.TrimPrefix(strings.TrimPrefix(s.template, "https://"), "http://")
	tHost, tPath, _ := strings.Cut(tmpl, "/")
	tPath = "/" + tPath
	host := strings.ToLower(u.Hostname())
	path := strings.TrimSuffix(u.Path, "/")

	if pre, post, inHost := strings.Cut(tHost, "{v}"); inHost {
		// Subdomain profiles such as {v}.bandcamp.com.
		if pre != "" || path != "" {
			return ""
		}
		label := strings.TrimSuffix(host, post)
		if label == host || label == "" || label == "www" || strings.Contains(label, ".") {
			return ""
		}
		return label
	}
	if shortLinkHosts[host] {
		return ""
	}
	pre, post, found := strings.Cut(tPath, "{v}")
	if !found {
		return ""
	}
	post = strings.TrimSuffix(post, "/")
	if !strings.HasPrefix(path, pre) || !strings.HasSuffix(path, post) || len(path) < len(pre)+len(post) {
		return ""
	}
	handle := path[len(pre) : len(path)-len(post)]
	if handle == "" || strings.Contains(handle, "/") {
		return ""
	}
	return handle
}
