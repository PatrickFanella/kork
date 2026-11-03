// Package ics reads the events of an iCalendar (RFC 5545) feed: start time,
// summary, location and URL. It covers what tour-date calendars publish. It
// does not expand recurrence rules: an event with an RRULE is read as its
// first occurrence.
package ics

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"time"
	// Event times name IANA zones; embed the database so they resolve in
	// minimal containers.
	_ "time/tzdata"
)

// Event is one VEVENT.
type Event struct {
	// Start is in the event's own zone (its TZID, UTC for a "Z" time, or
	// UTC for floating and all-day times, which have no zone).
	Start time.Time
	// AllDay is true when DTSTART is a date without a time.
	AllDay    bool
	Summary   string
	Location  string
	URL       string
	Recurring bool // the event had an RRULE; only its first date is used
}

// ErrNotCalendar means the data has no VCALENDAR.
var ErrNotCalendar = errors.New("this isn't an iCalendar (.ics) feed")

// maxEvents bounds how many VEVENTs are read from one feed.
const maxEvents = 2000

// Parse reads the VEVENTs in data. Events without a usable DTSTART are
// skipped and counted in skipped.
func Parse(data []byte) (events []Event, skipped int, err error) {
	lines := unfold(data)
	inCal, inEvent, sawCal := false, false, false
	var cur Event
	var startOK bool
	for _, line := range lines {
		name, params, value := split(line)
		switch {
		case name == "BEGIN" && strings.EqualFold(value, "VCALENDAR"):
			inCal, sawCal = true, true
		case name == "END" && strings.EqualFold(value, "VCALENDAR"):
			inCal = false
		case !inCal:
		case name == "BEGIN" && strings.EqualFold(value, "VEVENT"):
			inEvent, cur, startOK = true, Event{}, false
		case name == "END" && strings.EqualFold(value, "VEVENT"):
			if inEvent {
				if startOK {
					events = append(events, cur)
				} else {
					skipped++
				}
			}
			inEvent = false
			if len(events) >= maxEvents {
				return events, skipped, nil
			}
		case !inEvent:
		case name == "DTSTART":
			t, allDay, ok := parseTime(value, params)
			cur.Start, cur.AllDay, startOK = t, allDay, ok
		case name == "SUMMARY":
			cur.Summary = unescape(value)
		case name == "LOCATION":
			cur.Location = unescape(value)
		case name == "URL":
			cur.URL = strings.TrimSpace(value)
		case name == "RRULE":
			cur.Recurring = true
		}
	}
	if !sawCal {
		return nil, 0, ErrNotCalendar
	}
	return events, skipped, nil
}

// unfold joins RFC 5545 folded lines (a line starting with a space or tab
// continues the previous one) and accepts LF as well as CRLF.
func unfold(data []byte) []string {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) && len(out) > 0 {
			out[len(out)-1] += line[1:]
			continue
		}
		out = append(out, line)
	}
	return out
}

// split separates "NAME;PARAM=V;P2=V2:value" into its parts. Parameter
// values may be quoted and contain ':' or ';'.
func split(line string) (name string, params map[string]string, value string) {
	inQuote := false
	end := -1
	for i, r := range line {
		if r == '"' {
			inQuote = !inQuote
		}
		if r == ':' && !inQuote {
			end = i
			break
		}
	}
	if end < 0 {
		return "", nil, ""
	}
	head, value := line[:end], line[end+1:]
	parts := strings.Split(head, ";")
	name = strings.ToUpper(strings.TrimSpace(parts[0]))
	for _, p := range parts[1:] {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			continue
		}
		if params == nil {
			params = map[string]string{}
		}
		params[strings.ToUpper(k)] = strings.Trim(v, `"`)
	}
	return name, params, value
}

func parseTime(value string, params map[string]string) (time.Time, bool, bool) {
	value = strings.TrimSpace(value)
	if params["VALUE"] == "DATE" || len(value) == 8 {
		t, err := time.Parse("20060102", value)
		return t, true, err == nil
	}
	if strings.HasSuffix(value, "Z") {
		t, err := time.Parse("20060102T150405Z", value)
		return t, false, err == nil
	}
	loc := time.UTC
	if tz := params["TZID"]; tz != "" {
		// Some calendars prefix the zone with a path ("/Europe/Berlin").
		if l, err := time.LoadLocation(strings.TrimPrefix(tz, "/")); err == nil {
			loc = l
		}
	}
	t, err := time.ParseInLocation("20060102T150405", value, loc)
	return t, false, err == nil
}

// unescape undoes RFC 5545 text escaping.
func unescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n', 'N':
			b.WriteByte(' ')
		default: // \\ \; \,
			b.WriteByte(s[i])
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
