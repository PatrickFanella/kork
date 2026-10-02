package blocks

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/catalog"
)

// Email sign-up, events and latest video/post blocks (#11).

// Sign-up destinations.
const (
	DestKork  = "kork"
	DestBrevo = "brevo"
)

// Signup is an email sign-up form. The Brevo API key is never part of the
// stored data: the server seals it separately and sets BrevoKeySet.
type Signup struct {
	Title       string  `json:"title"`
	Description string  `json:"description,omitempty"`
	ButtonLabel string  `json:"button_label,omitempty"`
	Destination string  `json:"destination"`
	BrevoListID FlexInt `json:"brevo_list_id,omitempty"`
	BrevoKeySet bool    `json:"brevo_key_set,omitempty"`
}

// FlexInt accepts a JSON number or a string of digits.
type FlexInt int64

func (n *FlexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		*n = 0
		return nil
	}
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return fmt.Errorf("not a whole number")
	}
	*n = FlexInt(v)
	return nil
}

// Event is one date in an events block.
type Event struct {
	Date      string `json:"date"`           // YYYY-MM-DD
	Time      string `json:"time,omitempty"` // HH:MM, 24-hour
	Title     string `json:"title"`          // title or venue
	City      string `json:"city,omitempty"`
	TicketURL string `json:"ticket_url,omitempty"`
	SoldOut   bool   `json:"sold_out,omitempty"`
}

// Events is a list of upcoming dates.
type Events struct {
	Events []Event `json:"events"`
}

// MaxEvents is the most events one block holds.
const MaxEvents = 50

// Latest shows the newest item of a YouTube channel or RSS/Atom feed.
type Latest struct {
	Source string `json:"source"`
	Label  string `json:"label,omitempty"`
}

// SecretFields are write-only keys removed from block data before
// validation; see TakeSecrets.
var SecretFields = map[string][]string{"email_signup": {"brevo_api_key"}}

// TakeSecrets removes write-only keys (such as a Brevo API key) from raw
// block data. It returns the remaining data and the removed string values.
func TakeSecrets(typ string, raw json.RawMessage) (json.RawMessage, map[string]string, error) {
	keys := SecretFields[typ]
	if len(keys) == 0 || len(raw) == 0 || string(raw) == "null" {
		return raw, nil, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, nil, fieldErr("data", "invalid block data")
	}
	out := map[string]string{}
	for _, k := range keys {
		v, ok := m[k]
		if !ok {
			continue
		}
		delete(m, k)
		var s string
		if json.Unmarshal(v, &s) != nil {
			return nil, nil, fieldErr(k, "must be text")
		}
		if s = strings.TrimSpace(s); s != "" {
			out[k] = s
		}
	}
	rest, err := json.Marshal(m)
	return rest, out, err
}

func validateSignup(raw json.RawMessage) (any, error) {
	var b Signup
	if err := decode(raw, &b); err != nil {
		return nil, err
	}
	var err error
	if b.Title, err = limit("title", oneLine(b.Title), 80, true); err != nil {
		return nil, err
	}
	if b.Description, err = limit("description", oneLine(b.Description), 200, false); err != nil {
		return nil, err
	}
	if b.ButtonLabel, err = limit("button_label", oneLine(b.ButtonLabel), 30, false); err != nil {
		return nil, err
	}
	switch b.Destination {
	case "", DestKork:
		b.Destination = DestKork
		b.BrevoListID = 0
	case DestBrevo:
		if b.BrevoListID <= 0 || b.BrevoListID > 1<<40 {
			return nil, fieldErr("brevo_list_id", "enter the ID of your Brevo list (a number)")
		}
	default:
		return nil, fieldErr("destination", "choose where sign-ups go")
	}
	// Set by the server from the stored key, never by the client.
	b.BrevoKeySet = false
	return b, nil
}

var (
	dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	timeRe = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
)

func validateEvents(raw json.RawMessage) (any, error) {
	var b Events
	if err := decode(raw, &b); err != nil {
		return nil, err
	}
	if len(b.Events) == 0 {
		return nil, fieldErr("events", "add at least one event")
	}
	if len(b.Events) > MaxEvents {
		return nil, fieldErr("events", fmt.Sprintf("a block can list up to %d events", MaxEvents))
	}
	for i := range b.Events {
		e := &b.Events[i]
		bad := func(msg string) error { return fieldErr("events", fmt.Sprintf("Event %d: %s", i+1, msg)) }
		e.Date = strings.TrimSpace(e.Date)
		d, err := time.Parse("2006-01-02", e.Date)
		if !dateRe.MatchString(e.Date) || err != nil || d.Year() < 2000 || d.Year() > 2100 {
			return nil, bad("enter the date as YYYY-MM-DD")
		}
		if e.Time = strings.TrimSpace(e.Time); e.Time != "" && !timeRe.MatchString(e.Time) {
			return nil, bad("enter the time as HH:MM (24-hour), or leave it empty")
		}
		if e.Title = oneLine(e.Title); e.Title == "" {
			return nil, bad("enter a title or venue")
		}
		if len([]rune(e.Title)) > 100 {
			return nil, bad("the title must be 100 characters or fewer")
		}
		if e.City = oneLine(e.City); len([]rune(e.City)) > 80 {
			return nil, bad("the city must be 80 characters or fewer")
		}
		if e.TicketURL = strings.TrimSpace(e.TicketURL); e.TicketURL != "" {
			u, err := catalog.CheckLinkURL(e.TicketURL)
			if err != nil || !isWeb(u) {
				return nil, bad("the ticket link must be an http(s) address")
			}
			e.TicketURL = u
		}
	}
	sort.SliceStable(b.Events, func(i, j int) bool { return eventKey(b.Events[i]) < eventKey(b.Events[j]) })
	return b, nil
}

func eventKey(e Event) string {
	t := e.Time
	if t == "" {
		t = "99:99" // events without a time sort after timed ones on the same day
	}
	return e.Date + " " + t
}

// EventEnds is when an event drops off the page: the end of its date in
// the last time zone to finish that day (UTC-12), which is 12:00 UTC the
// next day. Event dates carry no zone, so this never hides an event while
// it is still that date anywhere.
func EventEnds(e Event) time.Time {
	d, err := time.Parse("2006-01-02", e.Date)
	if err != nil {
		return time.Time{}
	}
	return d.Add(36 * time.Hour)
}

// UpcomingEvents returns the events not yet over at now, in date order.
func UpcomingEvents(list []Event, now time.Time) []Event {
	out := make([]Event, 0, len(list))
	for _, e := range list {
		if now.Before(EventEnds(e)) {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return eventKey(out[i]) < eventKey(out[j]) })
	return out
}

// Latest source kinds.
const (
	SourceYouTubeHandle  = "youtube_handle"
	SourceYouTubeChannel = "youtube_channel"
	SourceYouTubeUser    = "youtube_user"
	SourceFeed           = "feed"
)

var (
	ytHandle    = regexp.MustCompile(`^@?([A-Za-z0-9._-]{3,30})$`)
	ytChannelID = regexp.MustCompile(`^UC[A-Za-z0-9_-]{22}$`)
	ytUser      = regexp.MustCompile(`^[A-Za-z0-9]{1,60}$`)
)

func isYouTubeHost(h string) bool {
	h = strings.ToLower(h)
	return h == "youtube.com" || h == "www.youtube.com" || h == "m.youtube.com"
}

// NormalizeSource turns what a creator typed (an @handle, a YouTube channel
// address or any feed URL) into the stored source.
func NormalizeSource(in string) (string, error) {
	s := strings.TrimSpace(in)
	if s == "" {
		return "", fieldErr("source", "required")
	}
	if len(s) > 500 {
		return "", fieldErr("source", "must be 500 characters or fewer")
	}
	if strings.HasPrefix(s, "@") {
		m := ytHandle.FindStringSubmatch(s)
		if m == nil {
			return "", fieldErr("source", "that isn't a valid YouTube handle")
		}
		return "https://www.youtube.com/@" + m[1], nil
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return "", fieldErr("source", "enter a YouTube channel, @handle, or an RSS/Atom feed address")
	}
	if isYouTubeHost(u.Hostname()) {
		segs := strings.Split(strings.Trim(u.Path, "/"), "/")
		switch {
		case strings.HasPrefix(segs[0], "@") && ytHandle.MatchString(segs[0]):
			return "https://www.youtube.com/" + segs[0], nil
		case segs[0] == "channel" && len(segs) > 1 && ytChannelID.MatchString(segs[1]):
			return "https://www.youtube.com/channel/" + segs[1], nil
		case segs[0] == "user" && len(segs) > 1 && ytUser.MatchString(segs[1]):
			return "https://www.youtube.com/user/" + segs[1], nil
		case u.Path == "/feeds/videos.xml" && ytChannelID.MatchString(u.Query().Get("channel_id")):
			return "https://www.youtube.com/channel/" + u.Query().Get("channel_id"), nil
		}
		return "", fieldErr("source", "use the channel's @handle or its youtube.com/channel/… address")
	}
	u.Fragment = ""
	return u.String(), nil
}

// SourceKind splits a normalized source into its kind and value: the
// handle (without @), channel ID, legacy user name, or feed URL.
func SourceKind(source string) (kind, value string) {
	const yt = "https://www.youtube.com/"
	switch {
	case strings.HasPrefix(source, yt+"@"):
		return SourceYouTubeHandle, strings.TrimPrefix(source, yt+"@")
	case strings.HasPrefix(source, yt+"channel/"):
		return SourceYouTubeChannel, strings.TrimPrefix(source, yt+"channel/")
	case strings.HasPrefix(source, yt+"user/"):
		return SourceYouTubeUser, strings.TrimPrefix(source, yt+"user/")
	}
	return SourceFeed, source
}

func validateLatest(raw json.RawMessage) (any, error) {
	var b Latest
	if err := decode(raw, &b); err != nil {
		return nil, err
	}
	var err error
	if b.Source, err = NormalizeSource(b.Source); err != nil {
		return nil, err
	}
	if b.Label, err = limit("label", oneLine(b.Label), 60, false); err != nil {
		return nil, err
	}
	return b, nil
}

func signupLabel(raw json.RawMessage) string {
	var b Signup
	_ = json.Unmarshal(raw, &b)
	return b.Title
}

func eventsLabel(raw json.RawMessage) string {
	var b Events
	_ = json.Unmarshal(raw, &b)
	if len(b.Events) == 1 {
		return "Events: " + truncate(b.Events[0].Title, 50)
	}
	return fmt.Sprintf("Events (%d dates)", len(b.Events))
}

func latestLabel(raw json.RawMessage) string {
	var b Latest
	_ = json.Unmarshal(raw, &b)
	if b.Label != "" {
		return b.Label
	}
	kind, v := SourceKind(b.Source)
	switch kind {
	case SourceYouTubeHandle:
		return "Latest video: @" + v
	case SourceYouTubeChannel, SourceYouTubeUser:
		return "Latest video"
	}
	if u, err := url.Parse(v); err == nil && u.Host != "" {
		return "Latest post: " + strings.TrimPrefix(u.Hostname(), "www.")
	}
	return "Latest post"
}

// LatestItem is the cached newest feed item for a latest block.
type LatestItem struct {
	Title     string
	Link      string
	Published time.Time
	// ThumbnailID is an upload owned by the page owner, or "".
	ThumbnailID string
}

// EventView is one event prepared for the template.
type EventView struct {
	Datetime string // machine-readable date or date and time
	Month    string // "Oct"
	Day      string // "9"
	Weekday  string // "Fri"
	Time     string // "8:30 PM", or ""
	Title    string
	City     string
	Href     string
	SoldOut  bool
}

func eventViews(list []Event) []EventView {
	out := make([]EventView, 0, len(list))
	for _, e := range list {
		d, err := time.Parse("2006-01-02", e.Date)
		if err != nil {
			continue
		}
		v := EventView{Datetime: e.Date, Month: d.Format("Jan"), Day: d.Format("2"), Weekday: d.Format("Mon"),
			Title: e.Title, City: e.City, Href: e.TicketURL, SoldOut: e.SoldOut}
		if t, err := time.Parse("15:04", e.Time); err == nil && e.Time != "" {
			v.Datetime += "T" + e.Time
			v.Time = t.Format("3:04 PM")
		}
		out = append(out, v)
	}
	return out
}

func buildNew(ctx Context, b Block) (Item, bool, bool) {
	switch b.Type {
	case "email_signup":
		var d Signup
		if json.Unmarshal(b.Data, &d) != nil {
			return Item{}, false, true
		}
		label := d.ButtonLabel
		if label == "" {
			label = "Subscribe"
		}
		return Item{Kind: "signup", Title: d.Title, Subtitle: d.Description, Button: label}, true, true
	case "events":
		var d Events
		if json.Unmarshal(b.Data, &d) != nil {
			return Item{}, false, true
		}
		now := ctx.Now
		if now.IsZero() {
			now = time.Now()
		}
		up := UpcomingEvents(d.Events, now)
		if len(up) == 0 {
			return Item{}, false, true
		}
		return Item{Kind: "events", Events: eventViews(up), Track: true}, true, true
	case "latest":
		var d Latest
		if json.Unmarshal(b.Data, &d) != nil || ctx.Latest == nil {
			return Item{}, false, true
		}
		li, ok := ctx.Latest(b.ID)
		if !ok || !isWeb(li.Link) {
			return Item{}, false, true
		}
		kicker := d.Label
		if kicker == "" {
			kicker = "Latest post"
			if k, _ := SourceKind(d.Source); k != SourceFeed {
				kicker = "Latest video"
			}
		}
		it := Item{Kind: "latest", Href: li.Link, External: true, Title: li.Title, Subtitle: kicker, Track: true}
		if !li.Published.IsZero() {
			it.Date = li.Published.UTC().Format("Jan 2, 2006")
			it.Datetime = li.Published.UTC().Format(time.RFC3339)
		}
		if li.ThumbnailID != "" && ctx.UploadURL != nil {
			it.Image = ctx.UploadURL(li.ThumbnailID)
		}
		return it, true, true
	}
	return Item{}, false, false
}
