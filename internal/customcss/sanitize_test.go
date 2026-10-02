package customcss

import (
	"strings"
	"testing"
)

const (
	ownID   = "11111111-2222-3333-4444-555555555555"
	otherID = "99999999-8888-7777-6666-555555555555"
)

func own(id string) bool { return id == ownID }

func sanitize(src string) Result { return Sanitize(src, own) }

// assertSafe checks the properties every output must have.
func assertSafe(t *testing.T, src string, r Result) {
	t.Helper()
	lower := strings.ToLower(r.CSS)
	for _, bad := range []string{"</", "<!--", "-->", "<style", "<script", "javascript:", "expression(", "@import",
		"@charset", "@namespace", "behavior", "binding", "http:", "https:", "data:", "//evil"} {
		if strings.Contains(lower, bad) {
			t.Errorf("input %q produced %q containing %q", src, r.CSS, bad)
		}
	}
	// Re-sanitizing the output must be stable: nothing new is dropped.
	again := Sanitize(r.CSS, own)
	if again.CSS != r.CSS {
		t.Errorf("not idempotent for %q:\n first %q\nsecond %q (%v)", src, r.CSS, again.CSS, again.Warnings)
	}
}

func TestSanitizeKeepsOrdinaryCSS(t *testing.T) {
	cases := []struct{ in, want string }{
		{`.btn { color: red; border-radius: 4px }`, `body.kork-page .btn{color:red;border-radius:4px}`},
		{`a:hover, h1 > small { text-decoration: underline !important }`,
			`body.kork-page a:hover,body.kork-page h1 > small{text-decoration:underline !important}`},
		{`body { background: #000 }`, `body.kork-page{background:#000}`},
		{`body.dark p { color: white }`, `body.kork-page.dark p{color:white}`},
		{`html body .page { max-width: 40rem }`, `body.kork-page .page{max-width:40rem}`},
		{`:root { --accent: #ff00aa }`, `body.kork-page{--accent:#ff00aa}`},
		{`* { margin: 0 }`, `body.kork-page *{margin:0}`},
		{`.btn { background: linear-gradient(90deg, rgb(0 0 0 / 50%), var(--accent)) }`,
			`body.kork-page .btn{background:linear-gradient(90deg, rgb(0 0 0 / 50%), var(--accent))}`},
		{`@media (max-width: 600px) { .page { padding: 0 } }`, `@media (max-width: 600px){body.kork-page .page{padding:0}}`},
		{`@supports (display: grid) { .blocks { display: grid } }`, `@supports (display: grid){body.kork-page .blocks{display:grid}}`},
		{`@keyframes pulse { from { opacity: 1 } 50% { opacity: .5 } to { opacity: 1 } }`,
			`@keyframes pulse{from{opacity:1}50%{opacity:.5}to{opacity:1}}`},
		{`.avatar { background: url(/u/` + ownID + `) }`, `body.kork-page .avatar{background:url("/u/` + ownID + `")}`},
		{`.x { background: url("/u/` + ownID + `") }`, `body.kork-page .x{background:url("/u/` + ownID + `")}`},
		{`@font-face { font-family: Mine; src: url(/u/` + ownID + `) format("woff2"); font-display: swap }`,
			`@font-face{font-family:Mine;src:url("/u/` + ownID + `") format("woff2");font-display:swap}`},
		{`.q::before { content: "a \"quoted\" <b>" }`, `body.kork-page .q::before{content:"a \22 quoted\22  \3c b\3e "}`},
		{`h1{font-size:2em}/* trailing comment`, `body.kork-page h1{font-size:2em}`},
	}
	for _, c := range cases {
		r := sanitize(c.in)
		if r.CSS != c.want {
			t.Errorf("Sanitize(%q)\n got %q\nwant %q\nwarnings %v", c.in, r.CSS, c.want, r.Warnings)
		}
		if len(r.Warnings) != 0 {
			t.Errorf("Sanitize(%q) warned: %v", c.in, r.Warnings)
		}
		assertSafe(t, c.in, r)
	}
}

func TestSanitizeHostileInput(t *testing.T) {
	cases := []struct {
		name, in string
		keep     string // a fragment that must survive, if any
		warn     string // a fragment one warning must contain
	}{
		{"style breakout in string", `.a{content:"</style><script>alert(1)</script>"}`, `\3c /style\3e `, ""},
		{"style breakout bare", `.a{color:red}</style><script>alert(1)</script>`, "color:red", "isn't a complete rule"},
		{"breakout in selector", `.a</style><img src=x onerror=alert(1)>{color:red}`, "", "removed"},
		{"html comment open", `<!-- .a{color:red} -->`, "color:red", ""},
		{"import", `@import url(https://evil.example/x.css); .a{color:red}`, "color:red", "@import is not allowed"},
		{"import string", `@import "https://evil.example/x.css";`, "", "@import"},
		{"charset", `@charset "utf-8"; .a{color:red}`, "color:red", "@charset is not allowed"},
		{"namespace", `@namespace svg url(http://www.w3.org/2000/svg);`, "", "@namespace is not allowed"},
		{"remote url", `.a{background:url(https://evil.example/track.png)}`, "", "own uploads"},
		{"protocol-relative url", `.a{background:url(//evil.example/x)}`, "", "own uploads"},
		{"data url", `.a{background:url(data:image/png;base64,AAAA)}`, "", "own uploads"},
		{"quoted remote url", `.a{background:url( "https://evil.example/x" )}`, "", "own uploads"},
		{"someone else's upload", `.a{background:url(/u/` + otherID + `)}`, "", "isn't yours"},
		{"upload path traversal", `.a{background:url(/u/` + ownID + `/../../x)}`, "", "own uploads"},
		{"image-set", `.a{background:image-set("https://evil.example/x.png" 1x)}`, "", "image-set() is not allowed"},
		{"expression", `.a{width:expression(alert(1))}`, "", "expression() is not allowed"},
		{"escaped url function", `.a{background:\75 rl(https://evil.example/x)}`, "", "own uploads"},
		{"escaped url uppercase", `.a{background:U\52 L(https://evil.example/x)}`, "", "own uploads"},
		{"escaped expression", `.a{width:\65 xpression(alert(1))}`, "", "expression() is not allowed"},
		{"comment inside url keyword", `.a{background:u/**/rl(https://evil.example/x)}`, "", ""},
		{"comment hiding import", `@im/**/port url(https://evil.example/x.css); .a{color:red}`, "color:red", ""},
		{"escaped import", `@\69 mport "https://evil.example/x.css"; .a{color:red}`, "color:red", "@import is not allowed"},
		{"behavior", `.a{behavior:url(/u/` + ownID + `)}`, "", "behavior is not allowed"},
		{"escaped behavior", `.a{\62 ehavior:url(x.htc)}`, "", "behavior is not allowed"},
		{"moz-binding", `.a{-moz-binding:url(https://evil.example/x.xml#x)}`, "", "-moz-binding is not allowed"},
		{"javascript url", `.a{background:url(javascript:alert(1))}`, "", "invalid value"},
		{"javascript url quoted", `.a{background:url("javascript:alert(1)")}`, "", "own uploads"},
		{"breakout in media", `@media (width</style>){.a{color:red}}`, "", ""},
		{"media range", `@media (width <= 600px){.a{color:red}}`, `@media (width <= 600px)`, ""},
		{"javascript in string", `.a{content:"java\73 cript:alert(1)"}`, "", "javascript: is not allowed"},
		{"javascript with whitespace", `.a{content:"java\9 script:x"}`, "", "javascript: is not allowed"},
		{"font-face remote", `@font-face{font-family:X;src:url(https://evil.example/f.woff2)}`, "", "own uploads"},
		{"font-face local only", `@font-face{font-family:X;src:local(Arial)}`, "", "src must use url()"},
		{"nested rule", `.a{color:red; &:hover{color:blue}}`, "color:red", "nested rules"},
		{"nested media", `.a{@media print{color:blue}}`, "", "nested rules"},
		{"layer", `@layer base{.a{color:red}}`, "", "@layer is not allowed"},
		{"page", `@page{margin:0}`, "", "@page is not allowed"},
		{"bad string", ".a{content:\"unterminated\n}", "", ""},
		{"backslash newline", ".a{content:\"a\\\nb\"}", `"ab"`, ""},
		{"cdc in value", `.a{color:red-->}`, "", ""},
		{"unknown function", `.a{width:foo(1px)}`, "", "foo() is not allowed"},
		{"url in selector", `.a:not(url(https://evil.example)){color:red}`, "", "url() is not allowed here"},
		{"null bytes", ".a{col\x00or:red}", "", ""},
		{"invalid utf8", ".a{content:\"\xff\xfe\"}", "", ""},
		{"vbscript", `.a{content:"vbscript:x"}`, "", "javascript: is not allowed"},
		{"src function", `.a{background:src("https://evil.example/x")}`, "", "own uploads"},
		{"url function unquoted args", `.a{background:url("/u/` + ownID + `" x)}`, "", "single quoted address"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := sanitize(c.in)
			assertSafe(t, c.in, r)
			if c.keep != "" && !strings.Contains(r.CSS, c.keep) {
				t.Errorf("%q: output %q lacks %q (warnings %v)", c.in, r.CSS, c.keep, r.Warnings)
			}
			if c.warn != "" && !strings.Contains(strings.Join(r.Warnings, "\n"), c.warn) {
				t.Errorf("%q: warnings %v lack %q", c.in, r.Warnings, c.warn)
			}
		})
	}
}

func TestSanitizeNoBreakoutAtAll(t *testing.T) {
	// Whatever the input, the output must not contain markup-significant
	// sequences. Try fragments that combine escapes, comments and strings.
	pieces := []string{"<", "/", "style", ">", "!", "--", "\\3c ", "\\", "\"", "'", "/*", "*/", "url(", "{", "}", ";", ":", " ",
		".a", "@media", "(", ")", "\\2f ", "-", "&", "\n"}
	for i := 0; i < 4000; i++ {
		var b strings.Builder
		x := i
		for j := 0; j < 10; j++ {
			b.WriteString(pieces[x%len(pieces)])
			x = x*31 + j + 7
		}
		in := ".a{content:" + b.String() + "} " + b.String() + "{color:red}"
		r := sanitize(in)
		lower := strings.ToLower(r.CSS)
		if strings.Contains(lower, "</") || strings.Contains(lower, "<!--") || strings.Contains(lower, "-->") {
			t.Fatalf("breakout for %q: %q", in, r.CSS)
		}
	}
}

func TestSanitizeWarnsWithLines(t *testing.T) {
	r := sanitize(".a{color:red}\n@import \"x.css\";\n.b{width:expression(1)}")
	if len(r.Warnings) != 2 || !strings.HasPrefix(r.Warnings[0], "line 2:") || !strings.HasPrefix(r.Warnings[1], "line 3:") {
		t.Fatalf("warnings: %v", r.Warnings)
	}
	if r.CSS != "body.kork-page .a{color:red}" {
		t.Fatalf("css: %q", r.CSS)
	}
}

func TestSanitizeDeepNesting(t *testing.T) {
	r := sanitize(".a{width:" + strings.Repeat("calc(", 100) + "1px" + strings.Repeat(")", 100) + "}")
	if r.CSS != "" || len(r.Warnings) == 0 {
		t.Fatalf("deep nesting kept: %q %v", r.CSS, r.Warnings)
	}
}

func TestSanitizeEmpty(t *testing.T) {
	if r := sanitize("  \n "); r.CSS != "" || len(r.Warnings) != 0 {
		t.Fatalf("empty: %+v", r)
	}
}
