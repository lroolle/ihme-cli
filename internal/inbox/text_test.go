package inbox

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode"
)

func TestHTMLToText(t *testing.T) {
	src := `<html><head><title>T</title><style>.x{color:red}</style></head><body>
<div style="display:none">preview&#8203;&#8203;&#847;&#847;</div>
<p>Your code is <b>482913</b>.</p>
<script>alert(1)</script>
<p><a href="https://example.com/verify?t=1">Verify email</a> or <a href="https://example.com/x">https://example.com/x</a>.</p>
<ul><li>one</li><li>two</li></ul>
<img src="https://tracker.example/pixel.gif" alt="">
<a href="mailto:help@example.com">Help</a>
</body></html>`
	got := htmlToText(src)
	for _, want := range []string{
		"Your code is 482913.",
		"Verify email <https://example.com/verify?t=1>",
		"or https://example.com/x.",
		"- one\n- two",
		"Help",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, bad := range []string{"alert", "color:red", "tracker", "mailto:", "\u200b", "\u034f", "<b>"} {
		if strings.Contains(got, bad) {
			t.Errorf("leaked %q into:\n%s", bad, got)
		}
	}
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("more than one blank line in a row:\n%q", got)
	}
}

// Mail HTML is often broken. Broken HTML must still show its text.
func TestHTMLToTextSurvivesBrokenMarkup(t *testing.T) {
	cases := map[string]string{
		"unclosed head": `<html><head><meta charset=utf-8><body><p>Your code is 123456`,
		"unclosed link": `<p>Hi <a href="https://x.example/v">verify here</p><p>Your code is 123456</p>`,
		"style in body": `<body><style>p{}</style><p>Your code is 123456</p>`,
	}
	for name, src := range cases {
		if got := htmlToText(src); !strings.Contains(got, "Your code is 123456") {
			t.Errorf("%s: lost the text, got %q", name, got)
		}
	}
	if got := htmlToText(`<p>Hi <a href="https://x.example/v">verify`); !strings.Contains(got, "verify <https://x.example/v>") {
		t.Errorf("an unclosed final link lost its target: %q", got)
	}
}

// Bidi controls can display "moc.evil" as "live.com"; they go.
func TestBidiControlsAreStripped(t *testing.T) {
	in := "Pay at https://evil.example/\u202egpj.exe and \u2066x\u2069"
	if got := tidy(in); strings.ContainsAny(got, "\u202e\u2066\u2069") {
		t.Errorf("bidi controls survived: %q", got)
	}
	if got := clean("Bank\u202e <x@evil.example>"); strings.ContainsRune(got, '\u202e') {
		t.Errorf("bidi control survived in a header: %q", got)
	}
	// Every character Unicode lists as a bidi control.
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if unicode.Is(unicode.Bidi_Control, r) && strings.ContainsRune(clean("a"+string(r)+"b"), r) {
			t.Errorf("bidi control %U survived", r)
		}
	}
}

// A ")))..." tail is trimmed in one pass: this runs on every page view.
func TestTrimURLIsLinear(t *testing.T) {
	if got := trimURL("https://x.example/a_(b))."); got != "https://x.example/a_(b)" {
		t.Errorf("trimURL = %q", got)
	}
	start := time.Now()
	if got := trimURL("https://x.example/" + strings.Repeat(")", 1<<20)); got != "https://x.example/" {
		t.Errorf("trimURL kept the tail: %d bytes", len(got))
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("trimURL took %v on a 1 MB tail of ')'", d)
	}
}

// Anyone can mail the address, so a message nested past reason is
// refused before the parser sees it.
func TestDeepMIMEIsRefused(t *testing.T) {
	var b strings.Builder
	b.WriteString("MIME-Version: 1.0\r\n")
	for i := 0; i <= maxMultipart; i++ {
		fmt.Fprintf(&b, "Content-Type: multipart/mixed; boundary=\"b%d\"\r\n\r\n--b%d\r\n", i, i)
	}
	b.WriteString("Content-Type: text/plain\r\n\r\ndeep\r\n")
	if got := parseBody([]byte(b.String())); !got.TooComplex || got.Text != "" {
		t.Errorf("deep MIME = %+v, want TooComplex and no text", got)
	}
}

func TestAttachmentNamesAreCleaned(t *testing.T) {
	raw := "MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n" +
		"--b\r\nContent-Type: text/plain\r\n\r\nsee attached\r\n" +
		"--b\r\nContent-Type: application/octet-stream\r\n" +
		"Content-Disposition: attachment; filename=\"invoice\u202efdp.exe\"\r\n\r\nxx\r\n--b--\r\n"
	got := parseBody([]byte(raw))
	if got.TooComplex || got.Text != "see attached" || len(got.Attachments) != 1 {
		t.Fatalf("parse = %+v", got)
	}
	if strings.ContainsRune(got.Attachments[0], '\u202e') || !strings.HasPrefix(got.Attachments[0], "invoicefdp.exe") {
		t.Errorf("attachment name = %q, want the bidi control gone", got.Attachments[0])
	}
}

func TestLinkify(t *testing.T) {
	segs := linkify("Go to https://a.example/p?q=1. Or (https://b.example/x_(y)) then javascript:alert(1)")
	var urls []string
	var all strings.Builder
	for _, s := range segs {
		all.WriteString(s.Text)
		if s.URL != "" {
			urls = append(urls, s.URL)
		}
	}
	if all.String() != "Go to https://a.example/p?q=1. Or (https://b.example/x_(y)) then javascript:alert(1)" {
		t.Errorf("segments lost text: %q", all.String())
	}
	want := []string{"https://a.example/p?q=1", "https://b.example/x_(y)"}
	if strings.Join(urls, " ") != strings.Join(want, " ") {
		t.Errorf("urls = %q, want %q", urls, want)
	}
}

func TestTidyTruncatesOnRuneBoundary(t *testing.T) {
	long := strings.Repeat("验", maxBodyText) // 3 bytes each
	got := tidy(long)
	if !strings.HasSuffix(got, "[message truncated]") {
		t.Fatal("not truncated")
	}
	if !strings.HasPrefix(strings.TrimSuffix(got, "\n\n[message truncated]"), "验") || strings.ContainsRune(got, '\ufffd') {
		t.Error("cut inside a rune")
	}
}
