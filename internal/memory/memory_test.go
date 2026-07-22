package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJournalAppendCreatesDatedFile(t *testing.T) {
	s := At(t.TempDir())
	if err := s.JournalAppend("- 09:15 reserved calm_mule@icloud.com for [[github]]"); err != nil {
		t.Fatal(err)
	}
	name := time.Now().Format("2006_01_02") + ".md"
	raw, err := os.ReadFile(filepath.Join(s.Root(), "journals", name))
	if err != nil {
		t.Fatalf("journal file missing: %v", err)
	}
	if !strings.Contains(string(raw), "calm_mule") {
		t.Errorf("journal content = %q", raw)
	}
}

func TestPageAppendSanitizesFilenameOnly(t *testing.T) {
	s := At(t.TempDir())
	if err := s.PageAppend("GitHub Work/Team", "reserved a@icloud.com"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(s.Root(), "pages", "github-work-team.md"))
	if err != nil {
		t.Fatalf("sanitized page missing: %v", err)
	}
	if got := string(raw); got != "- reserved a@icloud.com\n" {
		t.Errorf("page content = %q", got)
	}
}

func TestSearchReturnsWholePageOnExactTitle(t *testing.T) {
	s := At(t.TempDir())
	must(t, s.PageAppend("github", "first line about the repo"))
	must(t, s.PageAppend("github", "second line"))
	must(t, s.JournalAppend("- touched [[github]] today"))

	hits := s.Search("github", 10)
	if len(hits) < 2 {
		t.Fatalf("hits = %+v", hits)
	}
	if hits[0].Source != "pages/github.md" || !strings.Contains(hits[0].Text, "second line") {
		t.Errorf("exact page should come whole and first, got %+v", hits[0])
	}
	if hits[1].Source != "journals/"+time.Now().Format("2006_01_02")+".md" {
		t.Errorf("journal hit missing, got %+v", hits[1])
	}
}

func TestSearchHonorsLimitAndMissesCleanly(t *testing.T) {
	s := At(t.TempDir())
	for range 5 {
		must(t, s.JournalAppend("- looked at figma"))
	}
	if hits := s.Search("figma", 3); len(hits) != 3 {
		t.Errorf("limit ignored: %d hits", len(hits))
	}
	if hits := s.Search("netflix", 3); hits != nil {
		t.Errorf("expected no hits, got %+v", hits)
	}
}

// The exact-page hit counts toward the limit — a small limit must not
// be overrun by the whole-page match plus a full budget of lines.
func TestSearchExactPageCountsTowardLimit(t *testing.T) {
	s := At(t.TempDir())
	must(t, s.PageAppend("figma", "the design tool account"))
	must(t, s.JournalAppend("- touched figma today"))
	if hits := s.Search("figma", 1); len(hits) != 1 {
		t.Errorf("limit 1 overrun: %d hits", len(hits))
	}
}

func TestGraphDerivesLinksAndBacklinks(t *testing.T) {
	s := At(t.TempDir())
	must(t, s.PageAppend("flashcards", "the user prefers short addresses — see [[github]]"))
	must(t, s.JournalAppend("- reserved x for [[github]]"))

	nodes := s.Graph()
	byTitle := map[string]Node{}
	for _, n := range nodes {
		byTitle[n.Title] = n
	}
	gh, ok := byTitle["github"]
	if !ok || gh.Backlinks != 2 {
		t.Errorf("github node = %+v (want 2 backlinks: one page, one journal)", gh)
	}
	fc, ok := byTitle["flashcards"]
	if !ok || len(fc.Links) != 1 || fc.Links[0] != "github" {
		t.Errorf("flashcards node = %+v", fc)
	}
}

func TestRecentJournalsKeepsNewestWithinBudget(t *testing.T) {
	s := At(t.TempDir())
	old := strings.Repeat("- old entry about things past\n", 10)
	must(t, s.appendFile(filepath.Join("journals", "2020_01_01.md"), old))
	must(t, s.appendFile(filepath.Join("journals", "2026_07_21.md"), "- fresh entry\n"))

	got := s.RecentJournals(50)
	if !strings.Contains(got, "fresh entry") {
		t.Errorf("newest journal dropped: %q", got)
	}
	if strings.Contains(got, "old entry") {
		t.Errorf("budget ignored, old journal included: %q", got)
	}
}

func TestStatsCountsFlashcards(t *testing.T) {
	s := At(t.TempDir())
	must(t, s.PageAppend(FlashcardsPage, "card one"))
	must(t, s.PageAppend(FlashcardsPage, "card two"))
	must(t, s.JournalAppend("- something"))

	st := s.Stats()
	if st.Journals != 1 || st.Pages != 1 || st.Flashcards != 2 {
		t.Errorf("stats = %+v", st)
	}
}

// Open must resolve a place even in a bare environment — the agent
// finds its own memory home without configuration.
func TestOpenPrefersExplicitPath(t *testing.T) {
	t.Setenv("IHME_MEMORY_PATH", "/tmp/elsewhere")
	if got := Open().Root(); got != "/tmp/elsewhere" {
		t.Errorf("root = %q", got)
	}
	t.Setenv("IHME_MEMORY_PATH", "")
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg")
	if got := Open().Root(); got != filepath.Join("/tmp/xdg", "ihme", "memory") {
		t.Errorf("xdg root = %q", got)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
