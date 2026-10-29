package lifecycle

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/internal/cmdutil"
)

var batch = []api.HmeEmail{
	{AnonymousID: "id-a", Hme: "a@icloud.com", Label: "forum", IsActive: true, CreateTimestamp: time.Date(2021, 3, 14, 12, 0, 0, 0, time.UTC).UnixMilli()},
	{AnonymousID: "id-b", Hme: "b@icloud.com", Label: "", IsActive: false, CreateTimestamp: time.Date(2022, 5, 2, 12, 0, 0, 0, time.UTC).UnixMilli()},
	{AnonymousID: "id-c", Hme: "c@icloud.com", Label: "shop", IsActive: true, CreateTimestamp: time.Date(2023, 1, 9, 12, 0, 0, 0, time.UTC).UnixMilli()},
}

// deactivateStep records what it acted on and fails where told to.
func deactivateStep(acted *[]string, fail map[string]error) step {
	return step{
		use:  "ihme deactivate",
		done: "Deactivated",
		skip: func(e api.HmeEmail) string {
			if !e.IsActive {
				return "already inactive"
			}
			return ""
		},
		act: func(e api.HmeEmail) error {
			*acted = append(*acted, e.AnonymousID)
			return fail[e.AnonymousID]
		},
	}
}

func statuses(results []outcome) string {
	var s []string
	for _, r := range results {
		s = append(s, r.Status)
	}
	return strings.Join(s, " ")
}

func TestRunManyReportsEach(t *testing.T) {
	var acted []string
	var out, errOut strings.Builder
	results, err := runMany(&out, &errOut, batch, deactivateStep(&acted, nil), false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(acted, " ") != "id-a id-c" {
		t.Errorf("acted on %v, want the two active ones", acted)
	}
	if got := statuses(results); got != "deactivated skipped deactivated" {
		t.Errorf("statuses = %q", got)
	}
	want := "Deactivated a@icloud.com\nb@icloud.com is already inactive\nDeactivated c@icloud.com\n"
	if out.String() != want || errOut.Len() != 0 {
		t.Errorf("stdout = %q, stderr = %q", out.String(), errOut.String())
	}
}

// Any failure stops the batch. Apple refusing an address, a rejected
// session, and a sign-in retry that hit rate pressure (transient, not
// a rejection, so the next call would retry the sign-in again) all
// stop at the first address, hand back the error itself so Explain
// and the exit code still fit it, and name one command for the rest.
func TestRunManyStopsAtFirstFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		exit int
	}{
		{"apple refuses the address", errors.New("deactivate failed: Something went wrong (code -41001)"), 1},
		{"session rejected", &api.HTTPError{Status: 401, URL: "https://p50-maildomainws.icloud.com/v1/hme/deactivate"}, 2},
		{"sign-in retry under rate pressure", &api.TransientError{Err: &api.HTTPError{Status: 421, URL: "https://setup.icloud.com/setup/ws/1/accountLogin"}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var acted []string
			var out, errOut strings.Builder
			results, err := runMany(&out, &errOut, batch, deactivateStep(&acted, map[string]error{"id-a": tc.err}), false)
			if strings.Join(acted, " ") != "id-a" {
				t.Errorf("acted on %v; nothing may run after a failure", acted)
			}
			if got := statuses(results); got != "failed skipped not_attempted" {
				t.Errorf("statuses = %q", got)
			}
			if !errors.Is(err, tc.err) || cmdutil.ExitCode(err) != tc.exit {
				t.Errorf("err = %v (exit %d), want the failure itself, exit %d", err, cmdutil.ExitCode(err), tc.exit)
			}
			want := "Stopped at a@icloud.com: 2 of 3 not deactivated. Once the error below is fixed, retry them with:\n  ihme deactivate a@icloud.com c@icloud.com\n"
			if errOut.String() != want {
				t.Errorf("stderr = %q, want %q", errOut.String(), want)
			}
			if out.String() != "b@icloud.com is already inactive\n" {
				t.Errorf("stdout = %q", out.String())
			}
		})
	}
}

func TestRunManyQuietForJSON(t *testing.T) {
	var acted []string
	var out, errOut strings.Builder
	results, err := runMany(&out, &errOut, batch, deactivateStep(&acted, nil), true)
	if err != nil || out.Len() != 0 || errOut.Len() != 0 {
		t.Errorf("--json printed progress: %q %q (err %v)", out.String(), errOut.String(), err)
	}
	if results[1].Reason != "already inactive" || results[0].ID != "id-a" || results[0].Hme != "a@icloud.com" {
		t.Errorf("results = %+v", results)
	}
	if retryCommand(deactivateStep(&acted, nil), results) != "" {
		t.Error("a finished batch must not hint a retry")
	}
}

func TestConfirmDelete(t *testing.T) {
	inactive := []api.HmeEmail{batch[1]}

	t.Run("active without --force refuses before asking", func(t *testing.T) {
		var screen strings.Builder
		err := confirmDelete(strings.NewReader("y\n"), &screen, batch, false, false, true)
		if err == nil || !strings.Contains(err.Error(), "nothing deleted: 2 of 3 still active") ||
			!strings.Contains(err.Error(), "a@icloud.com") || !strings.Contains(err.Error(), "c@icloud.com") {
			t.Errorf("err = %v", err)
		}
		if screen.Len() != 0 {
			t.Errorf("asked anyway:\n%s", screen.String())
		}
	})

	t.Run("no terminal needs --yes", func(t *testing.T) {
		err := confirmDelete(strings.NewReader(""), &strings.Builder{}, inactive, false, false, false)
		if err == nil || !strings.Contains(err.Error(), "pass --yes") {
			t.Errorf("err = %v", err)
		}
		if err := confirmDelete(strings.NewReader(""), &strings.Builder{}, inactive, false, true, false); err != nil {
			t.Errorf("--yes: %v", err)
		}
	})

	t.Run("lists every address and takes only y", func(t *testing.T) {
		var screen strings.Builder
		if err := confirmDelete(strings.NewReader("y\n"), &screen, batch, true, false, true); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			"These 3 addresses will be deleted. This cannot be undone.",
			"a@icloud.com", "forum", "2021-03-14", "b@icloud.com", "2022-05-02", "c@icloud.com", "2023-01-09",
			"2 of them are active; --force deactivates them first.", "Delete them? [y/N]",
		} {
			if !strings.Contains(screen.String(), want) {
				t.Errorf("screen lacks %q:\n%s", want, screen.String())
			}
		}
		for _, answer := range []string{"\n", "n\n", "yes please\n", ""} {
			if err := confirmDelete(strings.NewReader(answer), &strings.Builder{}, batch, true, false, true); !errors.Is(err, cmdutil.ErrCancelled) {
				t.Errorf("answer %q: err = %v, want cancelled", answer, err)
			}
		}
	})
}
