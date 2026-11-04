package server

import (
	"encoding/xml"
	"net/http/httptest"
	"strings"
	"testing"

	"git.subcult.tv/subculture-collective/zelda/internal/config"
)

func TestPlatformSEO(t *testing.T) {
	s := &Server{cfg: &config.Config{BaseURL: "https://kork.example", AppName: "Kork"}}
	robots := httptest.NewRecorder()
	s.handleRobots(robots, httptest.NewRequest("GET", "/robots.txt", nil))
	if !strings.HasPrefix(robots.Header().Get("Content-Type"), "text/plain") || !strings.Contains(robots.Body.String(), "Sitemap: https://kork.example/sitemap.xml") {
		t.Fatal("robots must be text and advertise the configured sitemap")
	}
	response := httptest.NewRecorder()
	s.handleSitemap(response, httptest.NewRequest("GET", "/sitemap.xml", nil))
	var set struct {
		URLs []struct {
			Loc string `xml:"loc"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal(response.Body.Bytes(), &set); err != nil {
		t.Fatal(err)
	}
	if len(set.URLs) != 3 || set.URLs[0].Loc != "https://kork.example/" {
		t.Fatalf("unexpected platform URLs: %+v", set.URLs)
	}
	landing := httptest.NewRecorder()
	s.handleLanding(landing, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(landing.Body.String(), `rel="canonical" href="https://kork.example/"`) {
		t.Fatal("landing missing canonical")
	}
	if !strings.Contains(landing.Body.String(), `property="og:site_name" content="Kork"`) {
		t.Fatal("landing missing og:site_name")
	}
	favicon := httptest.NewRecorder()
	s.handleFavicon(favicon, httptest.NewRequest("GET", "/favicon.ico", nil))
	if favicon.Code != 200 || favicon.Header().Get("Content-Type") != "image/x-icon" || favicon.Body.Len() == 0 {
		t.Fatalf("favicon.ico: status %d, type %q, %d bytes", favicon.Code, favicon.Header().Get("Content-Type"), favicon.Body.Len())
	}
	notFound := httptest.NewRecorder()
	s.notFoundPage(notFound)
	if notFound.Code != 404 || !strings.Contains(notFound.Body.String(), `content="noindex, follow"`) {
		t.Fatal("missing page must return 404 and noindex")
	}
}
