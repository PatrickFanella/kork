// Package icons returns inline SVG icons for services and block types.
package icons

import "html/template"

// Generic icons are 24×24 stroked outlines (drawn for this project).
var generic = map[string]string{
	"link":     `<path d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1"/><path d="M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1"/>`,
	"copy":     `<rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15V5a2 2 0 0 1 2-2h8"/>`,
	"contact":  `<rect x="3" y="4" width="18" height="16" rx="2"/><circle cx="9" cy="11" r="2.5"/><path d="M5.5 17a3.5 3.5 0 0 1 7 0M15 10h3M15 14h3"/>`,
	"map":      `<path d="M12 21s-7-6.2-7-11.5a7 7 0 0 1 14 0C19 14.8 12 21 12 21z"/><circle cx="12" cy="9.5" r="2.5"/>`,
	"email":    `<rect x="3" y="5" width="18" height="14" rx="2"/><path d="m3 7 9 6 9-6"/>`,
	"phone":    `<path d="M5 3h3l2 5-2.5 1.5a11 11 0 0 0 7 7L16 14l5 2v3a2 2 0 0 1-2 2A16 16 0 0 1 3 5a2 2 0 0 1 2-2z"/>`,
	"sms":      `<path d="M4 5h16v11H9l-5 4z"/><path d="M8 10h.01M12 10h.01M16 10h.01"/>`,
	"social":   `<circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/>`,
	"video":    `<rect x="3" y="5" width="18" height="14" rx="2"/><path d="m10 9 5 3-5 3z"/>`,
	"music":    `<path d="M9 18V5l11-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="17" cy="16" r="3"/>`,
	"creator":  `<path d="M12 3l2.6 5.3 5.9.9-4.3 4.1 1 5.8L12 16.4 6.8 19.1l1-5.8-4.3-4.1 5.9-.9z"/>`,
	"payment":  `<rect x="2" y="6" width="20" height="13" rx="2"/><path d="M2 10h20M6 15h4"/>`,
	"crypto":   `<circle cx="12" cy="12" r="9"/><path d="M9 8h4.5a2 2 0 0 1 0 4H9zm0 4h5a2 2 0 0 1 0 4H9zM9 8v8M11 6v2M11 16v2"/>`,
	"contactc": `<path d="M4 5h16v11H9l-5 4z"/>`,
	"web":      `<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18"/>`,
}

func strokeSVG(inner string) template.HTML {
	return template.HTML(`<svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">` + inner + `</svg>`) //nolint:gosec // constant markup
}

// Generic returns a built-in outline icon by name, or the link icon.
func Generic(name string) template.HTML {
	if p, ok := generic[name]; ok {
		return strokeSVG(p)
	}
	return strokeSVG(generic["link"])
}

// For returns the brand icon for a service key, falling back to its category.
func For(key, category string) template.HTML {
	if p, ok := brands[key]; ok {
		return template.HTML(`<svg viewBox="0 0 24 24" width="24" height="24" fill="currentColor" aria-hidden="true" focusable="false"><path d="` + p + `"/></svg>`) //nolint:gosec // generated constant paths
	}
	if _, ok := generic[key]; ok {
		return Generic(key)
	}
	if category == "contact" {
		return Generic("contactc")
	}
	return Generic(category)
}
