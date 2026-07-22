package agent

import (
	"strings"
	"testing"
)

func TestMDANSIStylesAcrossChunkBoundaries(t *testing.T) {
	var out strings.Builder
	md := newMDANSI(&out)
	// A bold marker split across two chunks must still pair up.
	for _, chunk := range []string{"pick *", "*calm.river*", "* over *plain* via `taste`"} {
		if err := md.WriteText(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err := md.Close(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for marker, code := range map[string]string{
		"bold on": "\x1b[1m", "bold off": "\x1b[22m",
		"italic on": "\x1b[3m", "italic off": "\x1b[23m",
		"code on": "\x1b[36m", "code off": "\x1b[39m",
	} {
		if !strings.Contains(got, code) {
			t.Fatalf("missing %s in %q", marker, got)
		}
	}
	if strings.Contains(got, "*") || strings.Contains(got, "`") {
		t.Fatalf("markdown markers survived: %q", got)
	}
	if !strings.Contains(got, "calm.river") || !strings.Contains(got, "taste") {
		t.Fatalf("content lost: %q", got)
	}
}

func TestMDANSICloseResetsOpenStylingAndFlushesCarry(t *testing.T) {
	var out strings.Builder
	md := newMDANSI(&out)
	if err := md.WriteText("**left open, trailing star *"); err != nil {
		t.Fatal(err)
	}
	if err := md.Close(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.HasSuffix(got, "*\x1b[0m") {
		t.Fatalf("expected flushed star + reset at end, got %q", got)
	}
}

func TestMDANSILeavesUnderscoresAndCodeContentAlone(t *testing.T) {
	var out strings.Builder
	md := newMDANSI(&out)
	if err := md.WriteText("`anonymous_id` and snake_case stay literal"); err != nil {
		t.Fatal(err)
	}
	_ = md.Close()
	got := out.String()
	if !strings.Contains(got, "anonymous_id") || !strings.Contains(got, "snake_case") {
		t.Fatalf("underscored identifiers mangled: %q", got)
	}
}
