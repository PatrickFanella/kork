// Package blocks validates block data and converts blocks into render items.
package blocks

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"git.subcult.tv/subculture-collective/zelda/internal/catalog"
)

// FieldError points at the invalid data key.
type FieldError struct {
	Field string
	Err   error
}

func (e *FieldError) Error() string { return e.Err.Error() }

func fieldErr(field, msg string) error { return &FieldError{Field: field, Err: errors.New(msg)} }

type Link struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle,omitempty"`
	ThumbnailID string `json:"thumbnail_upload_id,omitempty"`
	ShowQR      bool   `json:"show_qr,omitempty"`
}

type Service struct {
	Service string `json:"service"`
	Value   string `json:"value"`
	Label   string `json:"label,omitempty"`
	ShowQR  bool   `json:"show_qr,omitempty"`
}

type Heading struct {
	Text string `json:"text"`
}

type Text struct {
	Text string `json:"text"`
}

type Divider struct{}

type Image struct {
	UploadID string `json:"upload_id"`
	Alt      string `json:"alt"`
	URL      string `json:"url,omitempty"`
}

type Embed struct {
	URL string `json:"url"`
}

type QR struct {
	Value string `json:"value"`
	Label string `json:"label,omitempty"`
}

type Copy struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type Contact struct {
	Name  string `json:"name"`
	Phone string `json:"phone,omitempty"`
	Email string `json:"email,omitempty"`
	Org   string `json:"org,omitempty"`
	URL   string `json:"url,omitempty"`
	Note  string `json:"note,omitempty"`
}

type Map struct {
	Address string `json:"address"`
	Label   string `json:"label,omitempty"`
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// UploadChecker reports whether an upload ID belongs to the current user.
type UploadChecker func(id string) bool

func clean(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s))
}

func oneLine(s string) string { return strings.Join(strings.Fields(clean(s)), " ") }

func limit(field, v string, max int, required bool) (string, error) {
	if required && v == "" {
		return "", fieldErr(field, "required")
	}
	if utf8.RuneCountInString(v) > max {
		return "", fieldErr(field, fmt.Sprintf("must be %d characters or fewer", max))
	}
	return v, nil
}

func checkUpload(field, id string, owns UploadChecker, required bool) (string, error) {
	if id == "" {
		if required {
			return "", fieldErr(field, "upload an image")
		}
		return "", nil
	}
	if !uuidRe.MatchString(id) || owns == nil || !owns(id) {
		return "", fieldErr(field, "unknown upload")
	}
	return id, nil
}

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fieldErr("data", "invalid block data")
	}
	return nil
}

// Validate checks and normalizes block data, returning the JSON to store.
func Validate(typ string, raw json.RawMessage, owns UploadChecker) (json.RawMessage, error) {
	var out any
	var err error
	switch typ {
	case "link":
		var b Link
		if err = decode(raw, &b); err != nil {
			return nil, err
		}
		if b.URL, err = catalog.CheckLinkURL(b.URL); err != nil {
			return nil, &FieldError{"url", err}
		}
		if b.Title, err = limit("title", oneLine(b.Title), 80, true); err != nil {
			return nil, err
		}
		if b.Subtitle, err = limit("subtitle", oneLine(b.Subtitle), 120, false); err != nil {
			return nil, err
		}
		if b.ThumbnailID, err = checkUpload("thumbnail_upload_id", b.ThumbnailID, owns, false); err != nil {
			return nil, err
		}
		out = b
	case "service":
		var b Service
		if err = decode(raw, &b); err != nil {
			return nil, err
		}
		svc, ok := catalog.LookupService(b.Service)
		if !ok {
			return nil, fieldErr("service", "unknown service")
		}
		r, rerr := svc.Resolve(b.Value)
		if rerr != nil {
			return nil, &FieldError{"value", rerr}
		}
		b.Value = r.Value
		if b.Label, err = limit("label", oneLine(b.Label), 80, false); err != nil {
			return nil, err
		}
		out = b
	case "heading":
		var b Heading
		if err = decode(raw, &b); err != nil {
			return nil, err
		}
		if b.Text, err = limit("text", oneLine(b.Text), 80, true); err != nil {
			return nil, err
		}
		out = b
	case "text":
		var b Text
		if err = decode(raw, &b); err != nil {
			return nil, err
		}
		if b.Text, err = limit("text", clean(b.Text), 1000, true); err != nil {
			return nil, err
		}
		out = b
	case "divider":
		out = Divider{}
	case "image":
		var b Image
		if err = decode(raw, &b); err != nil {
			return nil, err
		}
		if b.UploadID, err = checkUpload("upload_id", b.UploadID, owns, true); err != nil {
			return nil, err
		}
		if b.Alt, err = limit("alt", oneLine(b.Alt), 200, false); err != nil {
			return nil, err
		}
		if b.URL != "" {
			if b.URL, err = catalog.CheckLinkURL(b.URL); err != nil {
				return nil, &FieldError{"url", err}
			}
		}
		out = b
	case "embed":
		var b Embed
		if err = decode(raw, &b); err != nil {
			return nil, err
		}
		b.URL = strings.TrimSpace(b.URL)
		if _, ok := EmbedSource(b.URL, "example.com"); !ok {
			return nil, fieldErr("url", "paste a YouTube, Vimeo, Spotify, SoundCloud, Twitch or Apple Music link")
		}
		out = b
	case "qr":
		var b QR
		if err = decode(raw, &b); err != nil {
			return nil, err
		}
		if b.Value, err = limit("value", strings.TrimSpace(b.Value), 600, true); err != nil {
			return nil, err
		}
		if b.Label, err = limit("label", oneLine(b.Label), 80, false); err != nil {
			return nil, err
		}
		out = b
	case "copy":
		var b Copy
		if err = decode(raw, &b); err != nil {
			return nil, err
		}
		if b.Label, err = limit("label", oneLine(b.Label), 80, true); err != nil {
			return nil, err
		}
		if b.Value, err = limit("value", oneLine(b.Value), 300, true); err != nil {
			return nil, err
		}
		out = b
	case "contact":
		var b Contact
		if err = decode(raw, &b); err != nil {
			return nil, err
		}
		if b.Name, err = limit("name", oneLine(b.Name), 100, true); err != nil {
			return nil, err
		}
		if b.Org, err = limit("org", oneLine(b.Org), 100, false); err != nil {
			return nil, err
		}
		if b.Note, err = limit("note", oneLine(b.Note), 300, false); err != nil {
			return nil, err
		}
		if b.Phone = oneLine(b.Phone); b.Phone != "" {
			svc, _ := catalog.LookupService("phone")
			if _, perr := svc.Resolve(b.Phone); perr != nil {
				return nil, &FieldError{"phone", perr}
			}
		}
		if b.Email = oneLine(b.Email); b.Email != "" {
			svc, _ := catalog.LookupService("email")
			if _, perr := svc.Resolve(b.Email); perr != nil {
				return nil, &FieldError{"email", perr}
			}
		}
		if b.URL != "" {
			if b.URL, err = catalog.CheckLinkURL(b.URL); err != nil {
				return nil, &FieldError{"url", err}
			}
		}
		out = b
	case "map":
		var b Map
		if err = decode(raw, &b); err != nil {
			return nil, err
		}
		if b.Address, err = limit("address", oneLine(b.Address), 200, true); err != nil {
			return nil, err
		}
		if b.Label, err = limit("label", oneLine(b.Label), 80, false); err != nil {
			return nil, err
		}
		out = b
	default:
		return nil, fieldErr("type", "unknown block type")
	}
	return json.Marshal(out)
}

// Label is the short name shown in the editor's block list.
func Label(typ string, raw json.RawMessage) string {
	switch typ {
	case "link":
		var b Link
		_ = json.Unmarshal(raw, &b)
		return b.Title
	case "service":
		var b Service
		_ = json.Unmarshal(raw, &b)
		if b.Label != "" {
			return b.Label
		}
		if svc, ok := catalog.LookupService(b.Service); ok {
			return svc.Name
		}
		return b.Service
	case "heading", "text":
		var b Text
		_ = json.Unmarshal(raw, &b)
		return truncate(b.Text, 60)
	case "image":
		var b Image
		_ = json.Unmarshal(raw, &b)
		if b.Alt != "" {
			return b.Alt
		}
		return "Image"
	case "embed":
		var b Embed
		_ = json.Unmarshal(raw, &b)
		return truncate(b.URL, 60)
	case "qr":
		var b QR
		_ = json.Unmarshal(raw, &b)
		if b.Label != "" {
			return b.Label
		}
		return "QR: " + truncate(b.Value, 50)
	case "copy":
		var b Copy
		_ = json.Unmarshal(raw, &b)
		return b.Label
	case "contact":
		var b Contact
		_ = json.Unmarshal(raw, &b)
		return "Contact: " + b.Name
	case "map":
		var b Map
		_ = json.Unmarshal(raw, &b)
		if b.Label != "" {
			return b.Label
		}
		return b.Address
	case "divider":
		return "Divider"
	}
	return typ
}

func truncate(s string, n int) string {
	s = oneLine(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
