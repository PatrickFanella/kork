package linkimport

import (
	"encoding/json"
	"net/url"
	"sort"
	"strings"

	"git.subcult.tv/subculture-collective/zelda/internal/htmlmeta"
)

// linktreeHosts are Linktree's own sites, skipped when scraping anchors.
var linktreeHosts = []string{"linktr.ee", "linktree.com"}

// Linktree reads the JSON Linktree embeds in its __NEXT_DATA__ script. On a
// linktr.ee page without that data it falls back to scraping anchors.
type Linktree struct{}

func (Linktree) Name() string { return "linktree" }

type ltLink struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	Position int    `json:"position"`
}

type ltAccount struct {
	Username          string   `json:"username"`
	PageTitle         string   `json:"pageTitle"`
	Description       string   `json:"description"`
	ProfilePictureURL string   `json:"profilePictureUrl"`
	Links             []ltLink `json:"links"`
	SocialLinks       []ltLink `json:"socialLinks"`
}

type ltNextData struct {
	Props struct {
		PageProps struct {
			Account     *ltAccount `json:"account"`
			Links       []ltLink   `json:"links"`
			SocialLinks []ltLink   `json:"socialLinks"`
		} `json:"pageProps"`
	} `json:"props"`
}

func (Linktree) Parse(page *url.URL, doc *htmlmeta.Doc) (*Profile, bool) {
	isLinktree := page != nil && (sameSite(page.String(), "linktr.ee") || sameSite(page.String(), "linktree.com"))
	var nd ltNextData
	if raw := doc.Scripts["__NEXT_DATA__"]; raw != "" && json.Unmarshal([]byte(raw), &nd) == nil && nd.Props.PageProps.Account != nil {
		pp := nd.Props.PageProps
		acct := pp.Account
		links := pp.Links
		if len(links) == 0 {
			links = acct.Links
		}
		social := pp.SocialLinks
		if len(social) == 0 {
			social = acct.SocialLinks
		}
		title := acct.PageTitle
		if title == "" && acct.Username != "" {
			title = "@" + acct.Username
		}
		prof := &Profile{Title: title, Bio: acct.Description, AvatarURL: acct.ProfilePictureURL}
		// Social icons sit above the buttons on Linktree, so import them first.
		for _, l := range sorted(social) {
			if u := htmlmeta.Resolve(page, l.URL); u != "" {
				prof.Entries = append(prof.Entries, Entry{URL: u})
			}
		}
		for _, l := range sorted(links) {
			if strings.EqualFold(l.Type, "HEADER") {
				prof.Entries = append(prof.Entries, Entry{Heading: true, Title: l.Title})
				continue
			}
			if u := htmlmeta.Resolve(page, l.URL); u != "" {
				prof.Entries = append(prof.Entries, Entry{URL: u, Title: l.Title})
			}
		}
		if prof.Title == "" {
			prof.Title = doc.Title
		}
		if prof.Bio == "" {
			prof.Bio = doc.Description
		}
		if prof.AvatarURL == "" {
			prof.AvatarURL = doc.Image
		}
		return prof, true
	}
	if !isLinktree {
		return nil, false
	}
	title := strings.TrimSuffix(strings.TrimSpace(doc.Title), "| Linktree")
	return &Profile{Title: strings.TrimSpace(title), Bio: doc.Description, AvatarURL: doc.Image,
		Entries: externalAnchors(page, doc, linktreeHosts)}, true
}

func sorted(ls []ltLink) []ltLink {
	out := append([]ltLink(nil), ls...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out
}
