package cmdutil

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lroolle/ihme-cli/api"
)

func TestExplainAndExitCode(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantText string
		wantFix  string
		wantCode int
	}{
		{
			name:     "no session on disk",
			err:      ErrNotLoggedIn,
			wantText: "not signed in",
			wantFix:  "ihme auth login",
			wantCode: 2,
		},
		{
			name:     "apple rejected the session",
			err:      fmt.Errorf("listing HME: %w", &api.HTTPError{Status: 401, URL: "https://p137-maildomainws.icloud.com/v2/hme/list?dsid=123"}),
			wantText: "rejected this session",
			wantFix:  "ihme auth login",
			wantCode: 2,
		},
		{
			name:     "apple is having a bad day",
			err:      &api.TransientError{Err: fmt.Errorf("validating session: %w", &api.HTTPError{Status: 502, URL: "https://setup.icloud.com/setup/ws/1/validate"})},
			wantText: "temporarily unreachable",
			wantFix:  "again in a moment",
			wantCode: 1,
		},
		{
			name:     "everything else stays plain",
			err:      fmt.Errorf("no address matches \"netflix\""),
			wantText: "no address matches",
			wantCode: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Explain(tc.err)
			if !strings.HasPrefix(got, "Error: ") {
				t.Errorf("message should lead with Error:, got %q", got)
			}
			if !strings.Contains(got, tc.wantText) {
				t.Errorf("missing %q in %q", tc.wantText, got)
			}
			if tc.wantFix != "" && !strings.Contains(got, tc.wantFix) {
				t.Errorf("missing the fix %q in %q", tc.wantFix, got)
			}
			if strings.Contains(got, "dsid=") {
				t.Errorf("message leaks account identifiers: %q", got)
			}
			if code := ExitCode(tc.err); code != tc.wantCode {
				t.Errorf("exit code = %d, want %d", code, tc.wantCode)
			}
		})
	}

	if ExitCode(nil) != 0 {
		t.Error("nil error must exit 0")
	}
	if Explain(nil) != "" {
		t.Error("nil error must render nothing")
	}
}
