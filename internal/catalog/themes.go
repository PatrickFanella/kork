package catalog

import (
	"errors"
	"regexp"
)

// Colors are the CSS custom properties a theme controls.
type Colors struct {
	BG         string `json:"bg"`
	Surface    string `json:"surface"`
	Text       string `json:"text"`
	Muted      string `json:"muted"`
	Accent     string `json:"accent"`
	ButtonBG   string `json:"button_bg"`
	ButtonText string `json:"button_text"`
}

// Background controls what sits behind the page.
type Background struct {
	Kind          string `json:"kind"`
	Color2        string `json:"color2,omitempty"`
	ImageUploadID string `json:"image_upload_id,omitempty"`
}

// Theme is stored as JSON on each page.
type Theme struct {
	Preset      string     `json:"preset"`
	Colors      Colors     `json:"colors"`
	Font        string     `json:"font"`
	ButtonStyle string     `json:"button_style"`
	ButtonShape string     `json:"button_shape"`
	Layout      string     `json:"layout"`
	Background  Background `json:"background"`
	// HideBranding asks to drop the "Made with" footer link. Pages honor it
	// only while the owner's plan includes plans.RemoveBranding.
	HideBranding bool `json:"hide_branding,omitempty"`
	// FontUploads names uploaded fonts (upload IDs) for body text and
	// headings. Pages use them only while the owner's plan includes
	// plans.CustomFonts; otherwise Font applies.
	FontUploads *FontUploads `json:"font_uploads,omitempty"`
}

// FontUploads holds uploaded font IDs; empty means the preset font.
type FontUploads struct {
	Body    string `json:"body,omitempty"`
	Display string `json:"display,omitempty"`
}

// Preset is a named starting theme.
type Preset struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Theme
}

// Option is a selectable value with a display name.
type Option struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

var Presets = []Preset{
	{"signal", "Signal", Theme{Colors: Colors{"#08060c", "#17101e", "#f5eff8", "#b7a8c0", "#c05cff", "#17101e", "#f5eff8"},
		Font: "condensed", ButtonStyle: "ledger", ButtonShape: "square", Layout: "ledger", Background: Background{Kind: "solid"}}},
	{"minimal-light", "Minimal light", Theme{Colors: Colors{"#fafafa", "#ffffff", "#111111", "#666666", "#111111", "#111111", "#ffffff"},
		Font: "system", ButtonStyle: "filled", ButtonShape: "rounded", Layout: "stack", Background: Background{Kind: "solid"}}},
	{"minimal-dark", "Minimal dark", Theme{Colors: Colors{"#0d0d0f", "#1a1a1f", "#f2f2f2", "#9a9aa5", "#ffffff", "#1f1f25", "#f2f2f2"},
		Font: "system", ButtonStyle: "outline", ButtonShape: "rounded", Layout: "stack", Background: Background{Kind: "solid"}}},
	{"paper", "Paper", Theme{Colors: Colors{"#f4efe6", "#fffaf1", "#2b2621", "#7a6f62", "#b5482f", "#fffaf1", "#2b2621"},
		Font: "serif", ButtonStyle: "shadow", ButtonShape: "square", Layout: "stack", Background: Background{Kind: "solid"}}},
	{"neon", "Neon", Theme{Colors: Colors{"#05010f", "#140a2a", "#f7f2ff", "#a99bd0", "#39ff9f", "#140a2a", "#39ff9f"},
		Font: "mono", ButtonStyle: "outline", ButtonShape: "pill", Layout: "stack", Background: Background{Kind: "gradient", Color2: "#2a0a4a"}}},
	{"pastel", "Pastel", Theme{Colors: Colors{"#fde7f3", "#ffffff", "#3a2340", "#86668c", "#d6337f", "#ffffff", "#3a2340"},
		Font: "rounded", ButtonStyle: "soft", ButtonShape: "pill", Layout: "stack", Background: Background{Kind: "gradient", Color2: "#e3ecff"}}},
	{"forest", "Forest", Theme{Colors: Colors{"#0f1a14", "#17271e", "#e8f1e4", "#9fb5a2", "#9ad36a", "#1d3326", "#e8f1e4"},
		Font: "serif", ButtonStyle: "filled", ButtonShape: "rounded", Layout: "stack", Background: Background{Kind: "solid"}}},
	{"sunset", "Sunset", Theme{Colors: Colors{"#ff7a59", "#ffffff", "#2d1320", "#5c2f3f", "#2d1320", "#2d1320", "#fff3ec"},
		Font: "condensed", ButtonStyle: "filled", ButtonShape: "pill", Layout: "stack", Background: Background{Kind: "gradient", Color2: "#ffc85c"}}},
	{"terminal", "Terminal", Theme{Colors: Colors{"#000000", "#0a0f0a", "#33ff66", "#1f9a3f", "#33ff66", "#000000", "#33ff66"},
		Font: "mono", ButtonStyle: "ledger", ButtonShape: "square", Layout: "ledger", Background: Background{Kind: "solid"}}},
	{"brutalist", "Brutalist", Theme{Colors: Colors{"#ffef00", "#ffffff", "#000000", "#222222", "#000000", "#ffffff", "#000000"},
		Font: "condensed", ButtonStyle: "shadow", ButtonShape: "square", Layout: "stack", Background: Background{Kind: "solid"}}},
}

var Fonts = []Option{
	{"system", "System sans"},
	{"serif", "Serif"},
	{"mono", "Monospace"},
	{"rounded", "Rounded"},
	{"condensed", "Condensed display"},
}

// FontStacks maps font keys to system font stacks; pages load no web fonts.
var FontStacks = map[string]string{
	"system":    `Inter, "Helvetica Neue", Arial, system-ui, sans-serif`,
	"serif":     `"Iowan Old Style", "Palatino Linotype", Palatino, Georgia, serif`,
	"mono":      `ui-monospace, "Cascadia Mono", "JetBrains Mono", Menlo, monospace`,
	"rounded":   `ui-rounded, "SF Pro Rounded", "Nunito", "Varela Round", system-ui, sans-serif`,
	"condensed": `"Arial Narrow", "Helvetica Neue Condensed", "Roboto Condensed", Impact, sans-serif`,
}

var ButtonStyles = []Option{{"filled", "Filled"}, {"outline", "Outline"}, {"soft", "Soft"}, {"shadow", "Hard shadow"}, {"ledger", "Ledger rows"}}
var ButtonShapes = []Option{{"square", "Square"}, {"rounded", "Rounded"}, {"pill", "Pill"}}
var Layouts = []Option{{"stack", "Stacked buttons"}, {"ledger", "Numbered ledger"}}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func hasOption(opts []Option, key string) bool {
	for _, o := range opts {
		if o.Key == key {
			return true
		}
	}
	return false
}

// LookupPreset returns a preset by key.
func LookupPreset(key string) (Preset, bool) {
	for _, p := range Presets {
		if p.Key == key {
			return p, true
		}
	}
	return Preset{}, false
}

// DefaultTheme is applied to new pages.
func DefaultTheme() Theme {
	p := Presets[0]
	t := p.Theme
	t.Preset = p.Key
	return t
}

// Normalize fills empty fields from the preset and rejects invalid values.
// It returns the JSON field name of the first invalid value.
func (t Theme) Normalize() (Theme, string, error) {
	p, ok := LookupPreset(t.Preset)
	if !ok {
		if t.Preset != "" {
			return t, "preset", errors.New("unknown preset")
		}
		p = Presets[0]
		t.Preset = p.Key
	}
	fill := func(dst *string, def string) {
		if *dst == "" {
			*dst = def
		}
	}
	c := &t.Colors
	fill(&c.BG, p.Colors.BG)
	fill(&c.Surface, p.Colors.Surface)
	fill(&c.Text, p.Colors.Text)
	fill(&c.Muted, p.Colors.Muted)
	fill(&c.Accent, p.Colors.Accent)
	fill(&c.ButtonBG, p.Colors.ButtonBG)
	fill(&c.ButtonText, p.Colors.ButtonText)
	for _, v := range []string{c.BG, c.Surface, c.Text, c.Muted, c.Accent, c.ButtonBG, c.ButtonText} {
		if !hexColor.MatchString(v) {
			return t, "colors", errors.New("colors must be #rrggbb")
		}
	}
	fill(&t.Font, p.Font)
	fill(&t.ButtonStyle, p.ButtonStyle)
	fill(&t.ButtonShape, p.ButtonShape)
	fill(&t.Layout, p.Layout)
	fill(&t.Background.Kind, p.Background.Kind)
	if t.Background.Kind == "gradient" {
		fill(&t.Background.Color2, p.Background.Color2)
		fill(&t.Background.Color2, c.Surface)
	}
	switch {
	case !hasOption(Fonts, t.Font):
		return t, "font", errors.New("unknown font")
	case !hasOption(ButtonStyles, t.ButtonStyle):
		return t, "button_style", errors.New("unknown button style")
	case !hasOption(ButtonShapes, t.ButtonShape):
		return t, "button_shape", errors.New("unknown button shape")
	case !hasOption(Layouts, t.Layout):
		return t, "layout", errors.New("unknown layout")
	}
	if f := t.FontUploads; f != nil && f.Body == "" && f.Display == "" {
		t.FontUploads = nil
	}
	switch t.Background.Kind {
	case "solid":
	case "gradient":
		if !hexColor.MatchString(t.Background.Color2) {
			return t, "background", errors.New("gradient color must be #rrggbb")
		}
	case "image":
		if t.Background.ImageUploadID == "" {
			return t, "background", errors.New("choose a background image")
		}
	default:
		return t, "background", errors.New("unknown background kind")
	}
	return t, "", nil
}
