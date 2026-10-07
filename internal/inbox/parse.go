package inbox

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	_ "github.com/emersion/go-message/charset" // GBK, Shift_JIS, ISO-2022-JP, ...
	"github.com/emersion/go-message/mail"
)

// body is a parsed message: its readable text and the attachments it
// carries, which are named but never served.
type body struct {
	Text        string
	Attachments []string
	TooComplex  bool
}

// maxMultipart caps MIME nesting. Parsing cost grows faster than
// quadratically with depth, and anyone can mail the address; real
// mail nests a handful of levels, a forwarded chain a few dozen.
const maxMultipart = 64

// parseBody reads a raw RFC 5322 message. text/plain wins; an HTML-only
// message is converted to text. A part with a broken charset or
// encoding is skipped, not fatal: real mail is full of slightly broken
// parts, and the rest of the message is still worth reading.
func parseBody(raw []byte) body {
	if bytes.Count(bytes.ToLower(raw), []byte("multipart/")) > maxMultipart {
		return body{TooComplex: true}
	}
	mr, _ := mail.CreateReader(bytes.NewReader(raw))
	if mr == nil {
		return body{Text: tidy(afterHeaders(raw))}
	}
	var plain, htmls []string
	var attachments []string
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			if part == nil {
				break
			}
			continue
		}
		switch h := part.Header.(type) {
		case *mail.InlineHeader:
			ct, _, _ := h.ContentType()
			data, err := io.ReadAll(io.LimitReader(part.Body, 4*maxBodyText))
			if err != nil {
				continue
			}
			switch ct {
			case "text/plain", "":
				plain = append(plain, string(data))
			case "text/html":
				htmls = append(htmls, string(data))
			}
		case *mail.AttachmentHeader:
			name, _ := h.Filename()
			name = clean(name) // "invoice\u202efdp.exe" must not pass for a PDF
			if name == "" {
				name = "unnamed"
			}
			n, _ := io.Copy(io.Discard, part.Body)
			attachments = append(attachments, fmt.Sprintf("%s (%s)", name, humanSize(n)))
		}
	}
	text := tidy(strings.Join(plain, "\n\n"))
	if text == "" && len(htmls) > 0 {
		text = htmlToText(strings.Join(htmls, "\n"))
	}
	return body{Text: text, Attachments: attachments}
}

// afterHeaders is the last resort for a message go-message cannot
// read: everything after the header block, as text.
func afterHeaders(raw []byte) string {
	if i := bytes.Index(raw, []byte("\r\n\r\n")); i >= 0 {
		return string(raw[i+4:])
	}
	if i := bytes.Index(raw, []byte("\n\n")); i >= 0 {
		return string(raw[i+2:])
	}
	return ""
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	default:
		return fmt.Sprintf("%d B", n)
	}
}
