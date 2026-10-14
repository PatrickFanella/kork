package server

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
)

func (s *Server) handleRobots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "User-agent: *\nDisallow: /api/\nDisallow: /app/\nDisallow: /preview/\nSitemap: %s/sitemap.xml\n", strings.TrimRight(s.cfg.BaseURL, "/"))
}

func (s *Server) handleSitemap(w http.ResponseWriter, r *http.Request) {
	// Creator pages already have their own canonical URLs, including custom
	// domains. This sitemap covers the platform's source-owned public pages.
	type entry struct {
		Loc string `xml:"loc"`
	}
	set := struct {
		XMLName xml.Name `xml:"urlset"`
		XMLNS   string   `xml:"xmlns,attr"`
		URLs    []entry  `xml:"url"`
	}{XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	base := strings.TrimRight(s.cfg.BaseURL, "/")
	for _, path := range []string{"/", "/terms", "/privacy"} {
		set.URLs = append(set.URLs, entry{Loc: base + path})
	}
	body, err := xml.Marshal(set)
	if err != nil {
		http.Error(w, "sitemap unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Write([]byte(xml.Header)) //nolint:errcheck
	w.Write(body)               //nolint:errcheck
}
