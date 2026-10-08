package render

import (
	"html/template"
	"io"
	"strings"
)

// TermsVersion identifies the current terms and privacy text. Signup records
// it. Change it whenever the wording changes; drop "-draft" once the owner
// has approved the text.
const TermsVersion = "2026-10-08-draft"

// Legal is the input to the terms and privacy template.
type Legal struct {
	AppName      string
	BaseURL      string
	ContactEmail string
	Doc          string // "terms" or "privacy"
	Title        string
	TermsVersion string
	AssetVersion string
	// DraftNote is an HTML comment marking unapproved text. html/template
	// strips comments written in the template itself, so it is passed in.
	DraftNote template.HTML
}

const draftNote = `<!--
    DRAFT FOR OWNER REVIEW. This text was written from the code and has not
    been approved by SUBCULT or reviewed by a lawyer. Before removing "-draft"
    from render.TermsVersion, check: governing law, minimum age, notice of
    changes, the contact address (ZELDA_CONTACT_EMAIL), the processor list,
    proxy access logs, and backup retention.
  -->`

// WriteLegal renders /terms (doc "terms") or /privacy (doc "privacy").
func WriteLegal(w io.Writer, l Legal) error {
	l.AssetVersion = AssetVersion
	l.TermsVersion = TermsVersion
	if l.Doc == "terms" {
		l.Title = "Terms of service"
	} else {
		l.Doc, l.Title = "privacy", "Privacy policy"
	}
	if strings.HasSuffix(TermsVersion, "-draft") {
		l.DraftNote = template.HTML(draftNote) //nolint:gosec // constant
	}
	return tmpl.ExecuteTemplate(w, "legal", l)
}
