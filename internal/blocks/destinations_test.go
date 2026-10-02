package blocks

import (
	"encoding/json"
	"testing"
)

func TestDestinations(t *testing.T) {
	cases := []struct {
		typ, data string
		want      []Destination
	}{
		{"link", `{"url":"https://a.example/x","title":"A"}`, []Destination{{"url", "https://a.example/x"}}},
		{"link", `{"url":"mailto:a@example.com","title":"A"}`, nil},
		{"service", `{"service":"instagram","value":"alice"}`, []Destination{{"value", "https://www.instagram.com/alice/"}}},
		{"service", `{"service":"website","value":"https://shop.example"}`, []Destination{{"value", "https://shop.example"}}},
		{"service", `{"service":"bitcoin","value":"bc1qxy2kgdygjrsqtzq2n0yrf2493p83kkfjhx0wlh"}`, nil},
		{"embed", `{"url":"https://www.youtube.com/watch?v=abc"}`, []Destination{{"url", "https://www.youtube.com/watch?v=abc"}}},
		{"qr", `{"value":"https://q.example"}`, []Destination{{"value", "https://q.example"}}},
		{"qr", `{"value":"hello"}`, nil},
		{"image", `{"upload_id":"x","url":"https://i.example"}`, []Destination{{"url", "https://i.example"}}},
		{"contact", `{"name":"A","url":"https://c.example"}`, []Destination{{"url", "https://c.example"}}},
		{"text", `{"text":"https://t.example"}`, nil},
	}
	for _, c := range cases {
		got := Destinations(c.typ, json.RawMessage(c.data))
		if len(got) != len(c.want) {
			t.Errorf("%s %s: got %+v, want %+v", c.typ, c.data, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s %s: got %+v, want %+v", c.typ, c.data, got[i], c.want[i])
			}
		}
	}
}
