package agent

import (
	"context"
	"net"
	"path/filepath"
	"testing"
)

// TestConsentSocketRoundTrip proves the card crosses the process
// boundary intact and the raw answer comes back — the property the
// --via consent path stands on.
func TestConsentSocketRoundTrip(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "consent.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()

	var got userPrompt
	go serveConsentSocket(l, func(_ context.Context, p userPrompt) (string, error) {
		got = p
		return "y", nil
	})

	ask := socketAsker(sock)
	prompt := userPrompt{
		Kind: promptConsent, Title: "Create this Hide My Email address?",
		Subject: "glen_arbor@icloud.com",
		Facts:   [][2]string{{"label", "github"}},
		Why:     "a place on a map",
		Passed:  [][2]string{{"63.fryer@icloud.com", "serial number"}},
	}
	answer, err := ask(context.Background(), prompt)
	if err != nil || answer != "y" {
		t.Fatalf("ask = %q, %v", answer, err)
	}
	if got.Subject != prompt.Subject || got.Why != prompt.Why || len(got.Passed) != 1 {
		t.Errorf("card mangled in transit: %+v", got)
	}

	// Second exchange on the same socket: the loop serves the whole
	// run, not one question.
	if answer, err := ask(context.Background(), prompt); err != nil || answer != "y" {
		t.Fatalf("second ask = %q, %v", answer, err)
	}

	// A dead socket is an error the gate reports, not a hang.
	l.Close()
	if _, err := socketAsker(sock)(context.Background(), prompt); err == nil {
		t.Error("dead consent channel must error")
	}
}
