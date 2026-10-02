// Package render produces the server-side HTML for public pages.
package render

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"strings"

	"git.subcult.tv/subculture-collective/zelda/internal/blocks"
	"git.subcult.tv/subculture-collective/zelda/internal/catalog"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

var tmpl = template.Must(template.ParseFS(templateFS, "templates/*.html"))

// Static returns the public page assets served under /static/.
func Static() fs.FS {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return sub
}

// AssetVersion changes whenever a static file changes, for cache busting.
var AssetVersion = func() string {
	h := sha256.New()
	_ = fs.WalkDir(staticFS, "static", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, _ := staticFS.ReadFile(path)
		h.Write(b)
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:10]
}()

// Page is the input to the public page template.
type Page struct {
	AppName      string
	BaseURL      string
	Slug         string
	Title        string
	Bio          string
	Avatar       string
	AvatarAbs    string
	CanonicalURL string
	Theme        catalog.Theme
	Items        []blocks.Item
	Preview      bool
	Live         bool
	Nonce        string
	// Badges marks blocks that the owner preview shows but visitors don't,
	// keyed by block ID.
	Badges map[string]Badge
	// HideBranding drops the "Made with" footer link (Pro).
	HideBranding bool

	// Filled by WritePage.
	CSSVars      template.CSS
	AssetVersion string
}

// ThemeFrom decodes stored theme JSON, falling back to the default theme.
func ThemeFrom(raw json.RawMessage) catalog.Theme {
	var t catalog.Theme
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &t)
	}
	t, _, err := t.Normalize()
	if err != nil {
		return catalog.DefaultTheme()
	}
	return t
}

func cssVars(t catalog.Theme, bgImage string) template.CSS {
	c := t.Colors
	bg2 := t.Background.Color2
	if bg2 == "" {
		bg2 = c.Surface
	}
	var b strings.Builder
	// Colors are validated #rrggbb values and the font stack comes from a fixed map.
	fmt.Fprintf(&b, "--bg:%s;--surface:%s;--text:%s;--muted:%s;--accent:%s;--btn-bg:%s;--btn-text:%s;--bg2:%s;--font:%s;",
		c.BG, c.Surface, c.Text, c.Muted, c.Accent, c.ButtonBG, c.ButtonText, bg2, catalog.FontStacks[t.Font])
	if bgImage != "" && !strings.ContainsAny(bgImage, `"'()\`) {
		fmt.Fprintf(&b, `--bg-image:url("%s");`, bgImage)
	}
	return template.CSS(b.String()) //nolint:gosec // see comment above
}

// WritePage renders a link page. bgImage is the resolved background upload URL.
func WritePage(w io.Writer, p Page, bgImage string) error {
	p.CSSVars = cssVars(p.Theme, bgImage)
	p.AssetVersion = AssetVersion
	return tmpl.ExecuteTemplate(w, "page", p)
}

// Simple is the input to the landing and error template.
type Simple struct {
	AppName      string
	BaseURL      string
	Title        string
	Heading      string
	Message      string
	Landing      bool
	ServiceCount int
	AssetVersion string
	// Form, when set, replaces the home link with a one-button POST form.
	Form *SimpleForm
}

// SimpleForm posts a token back to the server, such as a sign-up
// confirmation.
type SimpleForm struct {
	Action string
	Token  string
	Button string
}

// WriteSimple renders the landing page or an error page.
func WriteSimple(w io.Writer, s Simple) error {
	s.AssetVersion = AssetVersion
	s.ServiceCount = len(catalog.Services())
	return tmpl.ExecuteTemplate(w, "simple", s)
}
