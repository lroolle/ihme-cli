package cmdutil

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lroolle/ihme-cli/api"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var pickNow = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

func ms(d time.Duration) int64 { return pickNow.Add(-d).UnixMilli() }

var pickEmails = []api.HmeEmail{
	{AnonymousID: "old", Hme: "old@icloud.com", Label: "old", IsActive: true, CreateTimestamp: ms(40 * 24 * time.Hour)},
	{AnonymousID: "new", Hme: "new@icloud.com", Label: "netflix", IsActive: true, CreateTimestamp: ms(2 * time.Hour)},
	{AnonymousID: "off", Hme: "off@icloud.com", Label: "", IsActive: false, CreateTimestamp: ms(time.Hour)},
	{AnonymousID: "mid", Hme: "mid@icloud.com", Label: "github", IsActive: true, CreateTimestamp: ms(3 * 24 * time.Hour)},
}

var active = Pick{Verb: "deactivate", Want: func(e api.HmeEmail) bool { return e.IsActive }, None: "no active addresses"}

func TestPickEnterTakesNewest(t *testing.T) {
	var out strings.Builder
	got, err := PickAddress(strings.NewReader("\n"), &out, pickEmails, active, pickNow)
	if err != nil {
		t.Fatal(err)
	}
	if got.AnonymousID != "new" {
		t.Errorf("Enter picked %s, want the newest active (new)", got.AnonymousID)
	}
	screen := out.String()
	if strings.Contains(screen, "off@icloud.com") {
		t.Error("an inactive address was offered for deactivation")
	}
	first, second := strings.Index(screen, "new@"), strings.Index(screen, "mid@")
	if first < 0 || second < 0 || first > second {
		t.Errorf("not newest first:\n%s", screen)
	}
	for _, want := range []string{"Which address to deactivate?", "2h ago", "netflix", "Enter for 1"} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}
}

func TestPickByNumber(t *testing.T) {
	got, err := PickAddress(strings.NewReader("3\n"), &strings.Builder{}, pickEmails, active, pickNow)
	if err != nil {
		t.Fatal(err)
	}
	if got.AnonymousID != "old" {
		t.Errorf("3 picked %s, want old", got.AnonymousID)
	}
}

func TestPickCancelAndEOF(t *testing.T) {
	for _, in := range []string{"q\n", "n\n", ""} {
		_, err := PickAddress(strings.NewReader(in), &strings.Builder{}, pickEmails, active, pickNow)
		if !errors.Is(err, ErrCancelled) {
			t.Errorf("input %q: err = %v, want ErrCancelled", in, err)
		}
	}
	if ExitCode(ErrCancelled) != 0 || Explain(ErrCancelled) != "Cancelled." {
		t.Error("a cancel is not a failure")
	}
}

func TestPickRejectsOutOfRange(t *testing.T) {
	_, err := PickAddress(strings.NewReader("9\n"), &strings.Builder{}, pickEmails, active, pickNow)
	if err == nil || !strings.Contains(err.Error(), "pick 1-3") {
		t.Errorf("err = %v, want a pick 1-3 hint", err)
	}
}

func TestPickNothingQualifies(t *testing.T) {
	none := Pick{Verb: "reactivate", Want: func(e api.HmeEmail) bool { return false }, None: "no inactive addresses to reactivate"}
	_, err := PickAddress(strings.NewReader("\n"), &strings.Builder{}, pickEmails, none, pickNow)
	if err == nil || err.Error() != none.None {
		t.Errorf("err = %v, want %q", err, none.None)
	}
}

func TestPickCapsTheList(t *testing.T) {
	var many []api.HmeEmail
	for i := range 12 {
		many = append(many, api.HmeEmail{AnonymousID: fmt.Sprint(i), Hme: fmt.Sprintf("a%d@icloud.com", i), IsActive: true, CreateTimestamp: ms(time.Duration(i) * time.Hour)})
	}
	var out strings.Builder
	if _, err := PickAddress(strings.NewReader("\n"), &out, many, active, pickNow); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "... 3 older") {
		t.Errorf("the overflow is not named:\n%s", out.String())
	}
}

// Without a terminal (tests, pipes, agents) an omitted ref stays the
// usage error: nothing may block on a prompt nobody can answer.
func TestRefOrPickWithoutTerminal(t *testing.T) {
	cmd := &cobra.Command{Use: "deactivate"}
	cmd.Flags().Bool("json", false, "")
	cmd.Flags().String("jq", "", "")
	args := RefOrPick("ihme deactivate <ref>", "ihme deactivate github.com")
	if err := args(cmd, nil); err == nil || !strings.Contains(err.Error(), "<ref> required") {
		t.Errorf("no ref without a terminal: err = %v", err)
	}
	if err := args(cmd, []string{"github"}); err != nil {
		t.Errorf("one ref: %v", err)
	}
	if err := args(cmd, []string{"a", "b"}); err == nil {
		t.Error("two refs must fail")
	}
}

// At a terminal, each guard alone must still turn prompting off:
// they are what keep agents and scripts from hanging on a question.
func TestCanPromptGuards(t *testing.T) {
	isTerminal = func(int) bool { return true }
	t.Cleanup(func() { isTerminal = term.IsTerminal })
	newCmd := func() *cobra.Command {
		cmd := &cobra.Command{Use: "deactivate"}
		cmd.Flags().Bool("json", false, "")
		cmd.Flags().String("jq", "", "")
		return cmd
	}
	t.Setenv("IHME_NO_PROMPT", "")
	if !CanPrompt(newCmd()) {
		t.Fatal("a terminal with no guard set must be able to prompt")
	}
	cmd := newCmd()
	cmd.Flags().Set("json", "true")
	if CanPrompt(cmd) {
		t.Error("--json did not turn prompting off")
	}
	cmd = newCmd()
	cmd.Flags().Set("jq", ".count")
	if CanPrompt(cmd) {
		t.Error("--jq did not turn prompting off")
	}
	t.Setenv("IHME_NO_PROMPT", "1")
	if CanPrompt(newCmd()) {
		t.Error("IHME_NO_PROMPT did not turn prompting off")
	}
}
