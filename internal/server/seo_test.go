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
	notFound := httptest.NewRecorder()
	s.notFoundPage(notFound)
	if notFound.Code != 404 || !strings.Contains(notFound.Body.String(), `content="noindex, follow"`) {
		t.Fatal("missing page must return 404 and noindex")
	}
}
