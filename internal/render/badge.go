package render

import "time"

// Badge is a schedule note shown on a block in the owner preview. Display is
// UTC; page.js rewrites it in the viewer's time zone from Datetime.
type Badge struct {
	Text     string
	Datetime string
	Display  string
}

// NewBadge builds a badge such as "Expired" at time at.
func NewBadge(text string, at time.Time) Badge {
	at = at.UTC()
	return Badge{Text: text, Datetime: at.Format(time.RFC3339), Display: at.Format("Jan 2, 2006 15:04 UTC")}
}
