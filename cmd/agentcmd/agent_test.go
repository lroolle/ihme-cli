package agentcmd

import (
	"strings"
	"testing"
)

func TestResolveTask(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		args   []string
		want   string
		errs   bool
	}{
		{name: "interactive when nothing is given"},
		{name: "prompt flag", prompt: "new for github", want: "new for github"},
		{name: "positional words", args: []string{"new", "for", "github"}, want: "new for github"},
		{name: "whitespace prompt is no task", prompt: "   "},
		{name: "both is refused", prompt: "a", args: []string{"b"}, errs: true},
	}
	for _, tc := range cases {
		got, err := resolveTask(tc.prompt, tc.args)
		if tc.errs {
			if err == nil {
				t.Errorf("%s: expected an error", tc.name)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%s: resolveTask = %q, %v — want %q", tc.name, got, err, tc.want)
		}
	}
}

func TestPromptFlagIsRegistered(t *testing.T) {
	cmd := NewCmdAgent()
	flag := cmd.Flags().Lookup("prompt")
	if flag == nil || flag.Shorthand != "p" {
		t.Fatalf("--prompt/-p not registered: %+v", flag)
	}
	if !strings.Contains(cmd.Long, "--prompt") {
		t.Error("help text never mentions --prompt")
	}
}
