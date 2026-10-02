package blocks

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	owns := func(id string) bool { return id == "11111111-1111-1111-1111-111111111111" }
	ok := []struct{ typ, data string }{
		{"link", `{"url":"example.com","title":"  My   site "}`},
		{"service", `{"service":"cashapp","value":"$subcult"}`},
		{"heading", `{"text":"Music"}`},
		{"text", `{"text":"line one\nline two"}`},
		{"divider", `{}`},
		{"image", `{"upload_id":"11111111-1111-1111-1111-111111111111","alt":"cover"}`},
		{"embed", `{"url":"https://www.youtube.com/watch?v=dQw4w9WgXcQ"}`},
		{"qr", `{"value":"WIFI:S:subcult;T:WPA;P:secret;;"}`},
		{"copy", `{"label":"Discord","value":"onnwee"}`},
		{"contact", `{"name":"Patrick","email":"hi@subcult.tv","phone":"+1 555 123 4567"}`},
		{"map", `{"address":"1 Infinite Loop, Cupertino"}`},
	}
	for _, c := range ok {
		if _, err := Validate(c.typ, json.RawMessage(c.data), owns); err != nil {
			t.Errorf("%s %s: %v", c.typ, c.data, err)
		}
	}
	out, _ := Validate("link", json.RawMessage(`{"url":"example.com","title":"  My   site "}`), owns)
	if !strings.Contains(string(out), `"url":"https://example.com"`) || !strings.Contains(string(out), `"title":"My site"`) {
		t.Errorf("link not normalized: %s", out)
	}

	bad := []struct{ typ, data, field string }{
		{"link", `{"url":"javascript:alert(1)","title":"x"}`, "url"},
		{"link", `{"url":"https://example.com"}`, "title"},
		{"service", `{"service":"myspace","value":"tom"}`, "service"},
		{"service", `{"service":"instagram","value":"bad handle"}`, "value"},
		{"image", `{"upload_id":"22222222-2222-2222-2222-222222222222"}`, "upload_id"},
		{"embed", `{"url":"https://evil.example.com/video"}`, "url"},
		{"contact", `{"name":"x","email":"nope"}`, "email"},
		{"heading", `{"text":"` + strings.Repeat("a", 81) + `"}`, "text"},
		{"bogus", `{}`, "type"},
	}
	for _, c := range bad {
		_, err := Validate(c.typ, json.RawMessage(c.data), owns)
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != c.field {
			t.Errorf("%s %s: want field %q, got %v", c.typ, c.data, c.field, err)
		}
	}
}

func TestEmbedSource(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://youtu.be/dQw4w9WgXcQ", "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ"},
		{"https://www.youtube.com/shorts/dQw4w9WgXcQ", "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ"},
		{"https://vimeo.com/76979871", "https://player.vimeo.com/video/76979871"},
		{"https://open.spotify.com/intl-de/track/4cOdK2wGLETKBW3PvgPWqT", "https://open.spotify.com/embed/track/4cOdK2wGLETKBW3PvgPWqT"},
		{"https://www.twitch.tv/subcult", "https://player.twitch.tv/?channel=subcult&parent=links.example.com&autoplay=false"},
		{"https://music.apple.com/us/album/x/123?i=456", "https://embed.music.apple.com/us/album/x/123?i=456"},
	}
	for _, c := range cases {
		got, ok := EmbedSource(c.in, "links.example.com:443")
		if !ok || got.URL != c.want {
			t.Errorf("%s: got %q %v, want %q", c.in, got.URL, ok, c.want)
		}
	}
	for _, bad := range []string{"http://youtu.be/dQw4w9WgXcQ", "https://youtube.com/watch?v=<script>", "https://example.com/x"} {
		if _, ok := EmbedSource(bad, "h"); ok {
			t.Errorf("%s: expected rejection", bad)
		}
	}
}

func TestAutolinkEscapes(t *testing.T) {
	got := string(Autolink("<b>hi</b> see https://example.com/a?b=1&c=2.\nnext"))
	want := `&lt;b&gt;hi&lt;/b&gt; see <a href="https://example.com/a?b=1&amp;c=2" rel="nofollow ugc noopener" target="_blank">https://example.com/a?b=1&amp;c=2</a>.<br>next`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestBuildKinds(t *testing.T) {
	ctx := Context{Host: "h", UploadURL: func(id string) string { return "/u/" + id }, QRFG: "#000000", QRBG: "#ffffff",
		VCardURL: func(id string) string { return "/vcard/" + id }}
	items := Build(ctx, []Block{
		{ID: "1", Type: "service", Data: json.RawMessage(`{"service":"bitcoin","value":"bc1qxy2kgdygjrsqtzq2n0yrf2493p83kkfjhx0wlh"}`)},
		{ID: "2", Type: "service", Data: json.RawMessage(`{"service":"instagram","value":"subcult_tv"}`)},
		{ID: "3", Type: "service", Data: json.RawMessage(`{"service":"removed","value":"x"}`)},
		{ID: "4", Type: "contact", Data: json.RawMessage(`{"name":"P"}`)},
	})
	if len(items) != 3 {
		t.Fatalf("want 3 items (unknown service skipped), got %d", len(items))
	}
	if items[0].Kind != "copy" || items[0].QR == "" {
		t.Errorf("bitcoin should be a copy item with QR: %+v", items[0])
	}
	if items[1].Kind != "button" || items[1].Rel != "me" || !strings.Contains(string(items[1].Icon), "<path") {
		t.Errorf("instagram button wrong: %+v", items[1])
	}
	if items[2].Href != "/vcard/4" {
		t.Errorf("contact href: %q", items[2].Href)
	}
}

func TestVCardEscapes(t *testing.T) {
	v := VCard(Contact{Name: "A;B", Note: "line\nbreak, comma"})
	if !strings.Contains(v, `FN:A\;B`) || !strings.Contains(v, `NOTE:line\nbreak\, comma`) {
		t.Fatal(v)
	}
}
