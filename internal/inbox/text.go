package inbox

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// maxBodyText caps what one message page renders. Past it the text is
// cut with a note: a page is for reading, not for archiving.
const maxBodyText = 200 << 10

// htmlToText renders an HTML mail body as plain text. Nothing from the
// HTML survives as markup: no images, no styles, no remote anything.
// Links keep their target as visible text ("Verify <https://...>"),
// because the target is what a careful reader needs to see.
func htmlToText(src string) string {
	z := html.NewTokenizer(strings.NewReader(src))
	var b strings.Builder
	// raw is the script/style/title/noscript element being skipped.
	// The tokenizer hands such an element's content over as one text
	// token, so no depth counting is needed, and an unclosed <head>
	// (common in mail) cannot swallow the body.
	var raw atom.Atom
	var href string
	var linkText strings.Builder
	inLink := false

	newline := func() {
		s := b.String()
		if !strings.HasSuffix(s, "\n") && s != "" {
			b.WriteByte('\n')
		}
	}
	paragraph := func() {
		newline()
		if !strings.HasSuffix(b.String(), "\n\n") && b.Len() > 0 {
			b.WriteByte('\n')
		}
	}

	endLink := func() {
		if inLink {
			inLink = false
			b.WriteString(linkLine(strings.TrimSpace(collapse(linkText.String())), href))
		}
	}

	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			endLink() // an <a> never closed still keeps its text
			return tidy(b.String())
		case html.StartTagToken, html.SelfClosingTagToken, html.EndTagToken:
			tok := z.Token()
			start := tt != html.EndTagToken
			switch tok.DataAtom {
			case atom.Script, atom.Style, atom.Title, atom.Noscript:
				if tt == html.StartTagToken {
					raw = tok.DataAtom
				} else if tt == html.EndTagToken && raw == tok.DataAtom {
					raw = 0
				}
			case atom.Br:
				b.WriteByte('\n')
			case atom.P, atom.Div, atom.Table, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Blockquote, atom.Ul, atom.Ol, atom.Hr:
				paragraph()
			case atom.Tr:
				newline()
			case atom.Td, atom.Th:
				if start && !strings.HasSuffix(b.String(), "\n") && b.Len() > 0 {
					b.WriteByte(' ')
				}
			case atom.Li:
				if start {
					newline()
					b.WriteString("- ")
				}
			case atom.A:
				endLink() // a new <a> closes one left open
				if start {
					href, inLink = "", true
					linkText.Reset()
					for _, a := range tok.Attr {
						if a.Key == "href" {
							href = strings.TrimSpace(a.Val)
						}
					}
				}
			}
		case html.TextToken:
			if raw != 0 {
				continue
			}
			text := collapse(string(z.Text()))
			if inLink {
				linkText.WriteString(text)
			} else {
				b.WriteString(text)
			}
		}
	}
}

// linkLine is how a link reads in plain text.
func linkLine(text, href string) string {
	web := strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://")
	switch {
	case !web:
		return text
	case text == "" || text == href:
		return href
	default:
		return text + " <" + href + ">"
	}
}

var spaceRun = regexp.MustCompile(`[ \t\f\v\x{00a0}]+`)

func collapse(s string) string {
	s = strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
	return spaceRun.ReplaceAllString(s, " ")
}

// invisible are characters that render as nothing. Marketing mail
// pads previews with the zero-width ones; the bidi controls can make
// a URL or a sender display in an order other than the one it has,
// which is a phishing tool, not formatting.
var invisible = strings.NewReplacer(
	"\u200b", "", "\u200c", "", "\u200d", "", "\u2060", "",
	"\ufeff", "", "\u034f", "", "\u00ad", "",
	"\u202a", "", "\u202b", "", "\u202c", "", "\u202d", "", "\u202e", "",
	"\u2066", "", "\u2067", "", "\u2068", "", "\u2069", "",
	"\u200e", "", "\u200f", "", "\u061c", "",
)

// clean is invisible applied to a one-line field: subject, sender.
func clean(s string) string { return strings.TrimSpace(invisible.Replace(s)) }

// tidy normalizes plain text for display: no invisible padding, no
// trailing spaces, at most one blank line in a row, capped length.
func tidy(s string) string {
	s = invisible.Replace(strings.ReplaceAll(s, "\r\n", "\n"))
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, line := range lines {
		line = strings.TrimRight(line, " \t\u00a0")
		if strings.TrimSpace(line) == "" {
			blank++
			if blank > 1 {
				continue
			}
			line = ""
		} else {
			blank = 0
		}
		out = append(out, line)
	}
	s = strings.TrimSpace(strings.Join(out, "\n"))
	if len(s) > maxBodyText {
		cut := maxBodyText
		for cut > 0 && !isRuneStart(s[cut]) {
			cut--
		}
		s = s[:cut] + "\n\n[message truncated]"
	}
	return s
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// Segment is a run of body text; URL is set when the run is a link.
type Segment struct {
	Text string
	URL  string
}

var urlPattern = regexp.MustCompile(`https?://[^\s<>"'\x60]+`)

// linkify splits text into plain runs and http(s) links. The template
// escapes every run and html/template vets every href, so nothing in
// a message can become markup on our page.
func linkify(text string) []Segment {
	var segs []Segment
	last := 0
	for _, m := range urlPattern.FindAllStringIndex(text, -1) {
		start, end := m[0], m[1]
		end = start + len(trimURL(text[start:end]))
		if start > last {
			segs = append(segs, Segment{Text: text[last:start]})
		}
		segs = append(segs, Segment{Text: text[start:end], URL: text[start:end]})
		last = end
	}
	if last < len(text) {
		segs = append(segs, Segment{Text: text[last:]})
	}
	return segs
}

// trimURL drops punctuation that ends a sentence rather than the URL,
// keeping a closing paren the URL itself opened.
func trimURL(u string) string {
	open, closed := strings.Count(u, "("), strings.Count(u, ")") // once: a ")))..." tail must not cost n^2
	for len(u) > 0 {
		c := u[len(u)-1]
		switch {
		case strings.IndexByte(".,;:!?'\"]}>", c) >= 0:
			u = u[:len(u)-1]
		case c == ')' && open < closed:
			u = u[:len(u)-1]
			closed--
		default:
			return u
		}
	}
	return u
}
