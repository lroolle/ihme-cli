package new

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/internal/inboxkey"
)

func TestReservationClipboardPolicy(t *testing.T) {
	originalTerminal, originalCopy := clipboardTerminal, copyText
	t.Cleanup(func() { clipboardTerminal, copyText = originalTerminal, originalCopy })
	for _, tc := range []struct {
		name                                                       string
		terminal, json, jq, copy, noCopy, harness, shared, failure bool
		want                                                       bool
	}{
		{name: "terminal", terminal: true, want: true},
		{name: "shared link", terminal: true, shared: true, want: true},
		{name: "opt out", terminal: true, noCopy: true},
		{name: "pipe"},
		{name: "json", terminal: true, json: true},
		{name: "jq", terminal: true, jq: true},
		{name: "harness", terminal: true, harness: true},
		{name: "explicit copy", json: true, copy: true, want: true},
		{name: "clipboard unavailable", terminal: true, failure: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("IHME_NO_PROMPT", "")
			if tc.harness {
				t.Setenv("IHME_NO_PROMPT", "1")
			}
			cmd := NewCmdNew()
			cmd.Flags().Bool("json", tc.json, "")
			query := ""
			if tc.jq {
				query = ".hme"
			}
			cmd.Flags().String("jq", query, "")
			if tc.copy {
				cmd.Flags().Set("copy", "true")
			}
			if tc.noCopy {
				cmd.Flags().Set("no-copy", "true")
			}
			cmd.SetOut(os.Stdout)
			var status bytes.Buffer
			cmd.SetErr(&status)
			clipboardTerminal = func(int) bool { return tc.terminal }
			copied := ""
			copyText = func(text string) error {
				copied = text
				if tc.failure {
					return errors.New("unavailable")
				}
				return nil
			}
			reserved := &api.HmeEmail{Hme: "test@example.com"}
			var grant *inboxkey.Grant
			want := reserved.Hme
			if tc.shared {
				grant = &inboxkey.Grant{Link: "https://mail.example.com/k/test/key"}
				want = grant.Link
			}
			copyReserved(cmd, reserved, grant)
			if tc.want && copied != want || !tc.want && copied != "" {
				t.Fatalf("unexpected clipboard: %q", copied)
			}
			if strings.Contains(status.String(), want) {
				t.Fatal("clipboard content leaked into status")
			}
			if tc.failure && !strings.Contains(status.String(), "unavailable") {
				t.Fatal("failure not reported")
			}
		})
	}
}
