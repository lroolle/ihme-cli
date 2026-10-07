package output

import (
	"fmt"
	"time"
)

// Ago renders t relative to now in the coarsest unit that still
// tells addresses apart: "just now", "5m ago", "3h ago", "4d ago",
// then a plain date once it is older than a month.
func Ago(t, now time.Time) string {
	t = t.In(now.Location()) // the reader's calendar, not the sender's
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 31*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case t.Year() == now.Year():
		return t.Format("Jan 02")
	default:
		return t.Format("Jan 02 2006")
	}
}
