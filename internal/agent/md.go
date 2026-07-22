package agent

import (
	"io"
	"strings"
)

// mdANSI streams model text to a terminal, translating the inline
// markdown the model is invited to use (**bold**, *italic*, `code`)
// into ANSI styling. It is stateful so markers split across stream
// chunks still pair up. Underscores are never treated as emphasis —
// they are address and identifier characters in this domain.
type mdANSI struct {
	w                  io.Writer
	bold, italic, code bool
	starCarry          bool // chunk ended on a lone '*': bold or italic is undecided
}

func newMDANSI(w io.Writer) *mdANSI { return &mdANSI{w: w} }

func (m *mdANSI) WriteText(text string) error {
	runes := []rune(text)
	if m.starCarry {
		runes = append([]rune{'*'}, runes...)
		m.starCarry = false
	}
	var out strings.Builder
	for i := 0; i < len(runes); {
		r := runes[i]
		switch {
		case r == '`':
			m.code = !m.code
			if m.code {
				out.WriteString("\x1b[36m")
			} else {
				out.WriteString("\x1b[39m")
			}
			i++
		case r == '*' && !m.code:
			if i == len(runes)-1 {
				m.starCarry = true
				i++
				continue
			}
			if runes[i+1] == '*' {
				m.bold = !m.bold
				if m.bold {
					out.WriteString("\x1b[1m")
				} else {
					out.WriteString("\x1b[22m")
				}
				i += 2
			} else {
				m.italic = !m.italic
				if m.italic {
					out.WriteString("\x1b[3m")
				} else {
					out.WriteString("\x1b[23m")
				}
				i++
			}
		default:
			out.WriteRune(r)
			i++
		}
	}
	_, err := io.WriteString(m.w, out.String())
	return err
}

// Close flushes a pending star and resets open styling so bold or
// italic never leaks into the shell prompt after the run.
func (m *mdANSI) Close() error {
	var tail string
	if m.starCarry {
		tail = "*"
		m.starCarry = false
	}
	if m.bold || m.italic || m.code {
		tail += "\x1b[0m"
		m.bold, m.italic, m.code = false, false, false
	}
	if tail == "" {
		return nil
	}
	_, err := io.WriteString(m.w, tail)
	return err
}

// passthrough satisfies the same surface for non-terminal output:
// markdown reaches pipes and files untouched.
type mdPassthrough struct{ w io.Writer }

func (p mdPassthrough) WriteText(text string) error {
	_, err := io.WriteString(p.w, text)
	return err
}

func (p mdPassthrough) Close() error { return nil }

// mdWriter is the streaming text sink the renderer writes assistant
// prose through.
type mdWriter interface {
	WriteText(string) error
	Close() error
}
