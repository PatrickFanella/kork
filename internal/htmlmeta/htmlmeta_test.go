package htmlmeta

import (
	"net/url"
	"testing"
)

func TestParse(t *testing.T) {
	base, _ := url.Parse("https://example.com/blog/post")
	d := Parse(base, []byte(`<!doctype html><html><head>
<title>  Plain   title </title>
<meta property="og:title" content="OG title">
<meta name="description" content="Plain description">
<meta property="og:image" content="/img/card.png">
<script id="__NEXT_DATA__" type="application/json">{"a":"x  y"}</script>
</head><body>
<a href="https://other.example/x#frag">Other <b>site</b></a>
<a href="#top">skip</a>
<a href="javascript:alert(1)">skip</a>
<a href="mailto:a@example.com">Mail</a>
<a href="../about">About</a>
</body></html>`))
	if d.Title != "OG title" || d.Description != "Plain description" || d.Image != "https://example.com/img/card.png" {
		t.Fatalf("meta: %+v", d)
	}
	if d.Scripts["__NEXT_DATA__"] != `{"a":"x  y"}` {
		t.Fatalf("script: %q", d.Scripts["__NEXT_DATA__"])
	}
	want := []Anchor{{"https://other.example/x", "Other site"}, {"mailto:a@example.com", "Mail"}, {"https://example.com/about", "About"}}
	if len(d.Anchors) != len(want) {
		t.Fatalf("anchors: %+v", d.Anchors)
	}
	for i, a := range want {
		if d.Anchors[i] != a {
			t.Errorf("anchor %d: got %+v want %+v", i, d.Anchors[i], a)
		}
	}
}

func TestParseFallbackTitle(t *testing.T) {
	d := Parse(nil, []byte(`<title>Only title</title><meta property="og:image" content="data:image/png;base64,xx">`))
	if d.Title != "Only title" || d.Image != "" {
		t.Fatalf("%+v", d)
	}
}
