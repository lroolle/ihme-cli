package output

import (
	"testing"
	"time"
)

func TestAgo(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	cases := map[time.Duration]string{
		10 * time.Second:     "just now",
		5 * time.Minute:      "5m ago",
		3 * time.Hour:        "3h ago",
		4 * 24 * time.Hour:   "4d ago",
		60 * 24 * time.Hour:  "Aug 07",
		400 * 24 * time.Hour: "Sep 01 2025",
	}
	for d, want := range cases {
		if got := Ago(now.Add(-d), now); got != want {
			t.Errorf("Ago(-%v) = %q, want %q", d, got, want)
		}
	}

	// Jan 1 01:00 in UTC+8 is still Dec 31 of the year before in UTC.
	sent := time.Date(2026, 1, 1, 1, 0, 0, 0, time.FixedZone("CST", 8*3600))
	if got := Ago(sent, now); got != "Dec 31 2025" {
		t.Errorf("Ago(sender's zone) = %q, want the reader's date %q", got, "Dec 31 2025")
	}
}
