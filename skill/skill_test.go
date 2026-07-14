package skill

import (
	"strings"
	"testing"
)

func TestInstructionsStripFrontmatter(t *testing.T) {
	got := Instructions()
	if strings.HasPrefix(got, "---") || strings.Contains(got[:200], "triggers:") {
		t.Fatalf("frontmatter not stripped:\n%.200s", got)
	}
	for _, want := range []string{"Execution adapters", "taste", "search_addresses"} {
		if !strings.Contains(got, want) {
			t.Fatalf("procedure missing %q", want)
		}
	}
}
