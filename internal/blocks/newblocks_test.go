package blocks

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func fieldOf(err error) string {
	var fe *FieldError
	if errors.As(err, &fe) {
		return fe.Field
	}
	return ""
}

func TestValidateSignup(t *testing.T) {
	out, err := Validate("email_signup", json.RawMessage(`{"title":"  Join   the list ","brevo_list_id":"9","brevo_key_set":true}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"title":"Join the list","destination":"kork"}` {
		t.Errorf("kork default: %s", out)
	}
	out, err = Validate("email_signup", json.RawMessage(`{"title":"News","destination":"brevo","brevo_list_id":"12","button_label":"Join"}`), nil)
	if err != nil || string(out) != `{"title":"News","button_label":"Join","destination":"brevo","brevo_list_id":12}` {
		t.Errorf("brevo: %s %v", out, err)
	}
	for body, field := range map[string]string{
		`{"destination":"kork"}`:                                         "title",
		`{"title":"x","destination":"mailchimp"}`:                        "destination",
		`{"title":"x","destination":"brevo"}`:                            "brevo_list_id",
		`{"title":"x","destination":"brevo","brevo_list_id":"abc"}`:      "data",
		`{"title":"x","button_label":"` + strings.Repeat("b", 31) + `"}`: "button_label",
	} {
		if _, err := Validate("email_signup", json.RawMessage(body), nil); fieldOf(err) != field {
			t.Errorf("%s: field %q, err %v", body, fieldOf(err), err)
		}
	}
}

func TestTakeSecrets(t *testing.T) {
	rest, secrets, err := TakeSecrets("email_signup", json.RawMessage(`{"title":"x","brevo_api_key":"  xkeysib-1 "}`))
	if err != nil || secrets["brevo_api_key"] != "xkeysib-1" || strings.Contains(string(rest), "xkeysib") {
		t.Fatalf("rest=%s secrets=%v err=%v", rest, secrets, err)
	}
	if _, secrets, _ = TakeSecrets("email_signup", json.RawMessage(`{"brevo_api_key":""}`)); len(secrets) != 0 {
		t.Errorf("empty key should be absent: %v", secrets)
	}
	if rest, secrets, _ = TakeSecrets("link", json.RawMessage(`{"brevo_api_key":"x"}`)); secrets != nil || !strings.Contains(string(rest), "brevo_api_key") {
		t.Errorf("other types untouched: %s %v", rest, secrets)
	}
	if _, _, err = TakeSecrets("email_signup", json.RawMessage(`{"brevo_api_key":5}`)); fieldOf(err) != "brevo_api_key" {
		t.Errorf("non-string key: %v", err)
	}
	// A key smuggled in under data is dropped by Validate too.
	out, _ := Validate("email_signup", json.RawMessage(`{"title":"x","brevo_api_key":"leak"}`), nil)
	if strings.Contains(string(out), "leak") {
		t.Errorf("Validate kept the key: %s", out)
	}
}

func TestValidateEvents(t *testing.T) {
	out, err := Validate("events", json.RawMessage(`{"events":[
		{"date":"2026-11-02","title":" The Hall ","city":"Chicago","ticket_url":"https://tix.example/2"},
		{"date":"2026-10-20","time":"20:30","title":"Club","sold_out":true},
		{"date":"2026-10-20","time":"18:00","title":"Matinee"}]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	var ev Events
	json.Unmarshal(out, &ev) //nolint:errcheck
	if got := []string{ev.Events[0].Title, ev.Events[1].Title, ev.Events[2].Title}; fmt.Sprint(got) != "[Matinee Club The Hall]" {
		t.Errorf("sorted: %v", got)
	}
	bad := map[string]string{
		`{"events":[]}`: "add at least one event",
		`{"events":[{"date":"2026-13-01","title":"x"}]}`:                              "Event 1: enter the date",
		`{"events":[{"date":"10/20/2026","title":"x"}]}`:                              "Event 1: enter the date",
		`{"events":[{"date":"2026-10-20","time":"8pm","title":"x"}]}`:                 "Event 1: enter the time",
		`{"events":[{"date":"2026-10-20","title":"ok"},{"date":"2026-10-21"}]}`:       "Event 2: enter a title",
		`{"events":[{"date":"2026-10-20","title":"x","ticket_url":"javascript:x"}]}`:  "ticket link",
		`{"events":[{"date":"2026-10-20","title":"x","ticket_url":"mailto:a@b.co"}]}`: "ticket link",
	}
	for body, msg := range bad {
		_, err := Validate("events", json.RawMessage(body), nil)
		if fieldOf(err) != "events" || !strings.Contains(err.Error(), msg) {
			t.Errorf("%s: %v", body, err)
		}
	}
	many := make([]Event, MaxEvents+1)
	for i := range many {
		many[i] = Event{Date: "2026-10-20", Title: "x"}
	}
	raw, _ := json.Marshal(Events{many})
	if _, err := Validate("events", raw, nil); err == nil || !strings.Contains(err.Error(), "up to 50") {
		t.Errorf("51 events: %v", err)
	}
}

func TestUpcomingEventsAndRender(t *testing.T) {
	list := []Event{
		{Date: "2026-10-01", Title: "Past"},
		{Date: "2026-10-02", Time: "21:00", Title: "Tonight", TicketURL: "https://tix.example/t"},
		{Date: "2026-10-09", Time: "20:30", Title: "Next week", City: "Chicago", SoldOut: true},
	}
	now := time.Date(2026, 10, 2, 18, 0, 0, 0, time.UTC)
	up := UpcomingEvents(list, now)
	if len(up) != 2 || up[0].Title != "Tonight" {
		t.Fatalf("upcoming: %+v", up)
	}
	// An event stays until 12:00 UTC the day after its date.
	if got := UpcomingEvents(list, time.Date(2026, 10, 3, 11, 59, 0, 0, time.UTC)); len(got) != 2 {
		t.Errorf("before cutoff: %d", len(got))
	}
	if got := UpcomingEvents(list, time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)); len(got) != 1 {
		t.Errorf("at cutoff: %d", len(got))
	}

	data, _ := json.Marshal(Events{list})
	items := Build(Context{Now: now}, []Block{{ID: "b1", Type: "events", Data: data}})
	if len(items) != 1 || items[0].Kind != "events" || len(items[0].Events) != 2 {
		t.Fatalf("build: %+v", items)
	}
	ev := items[0].Events[1]
	if ev.Month != "Oct" || ev.Day != "9" || ev.Weekday != "Fri" || ev.Time != "8:30 PM" || ev.Datetime != "2026-10-09T20:30" || !ev.SoldOut {
		t.Errorf("view: %+v", ev)
	}
	// All past: the block disappears.
	if items := Build(Context{Now: now.AddDate(0, 1, 0)}, []Block{{ID: "b1", Type: "events", Data: data}}); len(items) != 0 {
		t.Errorf("all past: %+v", items)
	}
	if got := Destinations("events", data); len(got) != 1 || got[0].URL != "https://tix.example/t" || got[0].Field != "events" {
		t.Errorf("destinations: %+v", got)
	}
}

func TestNormalizeSource(t *testing.T) {
	ok := map[string]string{
		"@AliceDraws":                                              "https://www.youtube.com/@AliceDraws",
		"youtube.com/@alice.draws":                                 "https://www.youtube.com/@alice.draws",
		"https://m.youtube.com/@alice/videos":                      "https://www.youtube.com/@alice",
		"https://www.youtube.com/channel/UCabcdefghijklmnopqrstuv": "https://www.youtube.com/channel/UCabcdefghijklmnopqrstuv",
		"https://www.youtube.com/feeds/videos.xml?channel_id=UCabcdefghijklmnopqrstuv": "https://www.youtube.com/channel/UCabcdefghijklmnopqrstuv",
		"https://www.youtube.com/user/alicedraws":                                      "https://www.youtube.com/user/alicedraws",
		"alice.example/feed.xml":                                                       "https://alice.example/feed.xml",
		"https://alice.example/rss#top":                                                "https://alice.example/rss",
	}
	for in, want := range ok {
		if got, err := NormalizeSource(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, in := range []string{"", "@a", "@bad handle", "https://youtube.com/c/alice", "https://youtube.com/watch?v=AAAAAAAAAAA", "ftp://x.example/feed", "https://u:p@x.example/feed"} {
		if _, err := NormalizeSource(in); fieldOf(err) != "source" {
			t.Errorf("%q: want source error, got %v", in, err)
		}
	}
	kinds := map[string][2]string{
		"https://www.youtube.com/@alice":                           {SourceYouTubeHandle, "alice"},
		"https://www.youtube.com/channel/UCabcdefghijklmnopqrstuv": {SourceYouTubeChannel, "UCabcdefghijklmnopqrstuv"},
		"https://www.youtube.com/user/alice":                       {SourceYouTubeUser, "alice"},
		"https://alice.example/feed.xml":                           {SourceFeed, "https://alice.example/feed.xml"},
	}
	for src, want := range kinds {
		if k, v := SourceKind(src); k != want[0] || v != want[1] {
			t.Errorf("SourceKind(%q) = %q %q", src, k, v)
		}
	}
}

func TestLatestBuildAndLabel(t *testing.T) {
	data, err := Validate("latest", json.RawMessage(`{"source":"@alice"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := Label("latest", data); got != "Latest video: @alice" {
		t.Errorf("label: %q", got)
	}
	ctx := Context{UploadURL: func(id string) string { return "/u/" + id }}
	// Nothing cached: the block is skipped.
	if items := Build(ctx, []Block{{ID: "b1", Type: "latest", Data: data}}); len(items) != 0 {
		t.Errorf("no cache: %+v", items)
	}
	ctx.Latest = func(id string) (LatestItem, bool) {
		return LatestItem{Title: "Inking", Link: "https://www.youtube.com/watch?v=AAAAAAAAAAA",
			Published: time.Date(2026, 9, 28, 15, 0, 0, 0, time.UTC), ThumbnailID: "thumb"}, id == "b1"
	}
	items := Build(ctx, []Block{{ID: "b1", Type: "latest", Data: data}})
	if len(items) != 1 {
		t.Fatal("no item")
	}
	it := items[0]
	if it.Kind != "latest" || it.Title != "Inking" || it.Subtitle != "Latest video" || it.Image != "/u/thumb" || it.Date != "Sep 28, 2026" || !it.External || !it.Track {
		t.Errorf("item: %+v", it)
	}
	feedData, _ := Validate("latest", json.RawMessage(`{"source":"https://alice.example/feed.xml","label":"From the blog"}`), nil)
	if got := Label("latest", feedData); got != "From the blog" {
		t.Errorf("label: %q", got)
	}
	if d := Destinations("latest", feedData); len(d) != 1 || d[0].Field != "source" {
		t.Errorf("feed destination: %+v", d)
	}
	if d := Destinations("latest", data); len(d) != 0 {
		t.Errorf("youtube handle is not a destination: %+v", d)
	}
}

func TestSignupBuild(t *testing.T) {
	data, _ := Validate("email_signup", json.RawMessage(`{"title":"Newsletter","description":"Monthly."}`), nil)
	items := Build(Context{}, []Block{{ID: "b1", Type: "email_signup", Data: data}})
	if len(items) != 1 || items[0].Kind != "signup" || items[0].Button != "Subscribe" || items[0].Subtitle != "Monthly." {
		t.Fatalf("items: %+v", items)
	}
	if Label("email_signup", data) != "Newsletter" {
		t.Error("label")
	}
}
