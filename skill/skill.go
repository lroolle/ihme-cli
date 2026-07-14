// Package skill embeds SKILL.md — the portable operational procedure
// shared by external agents (who read the file and drive the ihme
// executable) and the embedded agent (which invokes it as a task
// turn over in-process tools).
package skill

import (
	_ "embed"
	"strings"
)

//go:embed SKILL.md
var raw string

// Instructions returns the procedure body with the YAML frontmatter
// stripped: frontmatter is loader metadata for external agent
// harnesses, not instructions.
func Instructions() string {
	body := raw
	if strings.HasPrefix(body, "---") {
		if _, rest, ok := strings.Cut(body[3:], "\n---"); ok {
			body = rest
			if i := strings.IndexByte(body, '\n'); i >= 0 {
				body = body[i+1:]
			}
		}
	}
	return strings.TrimSpace(body)
}
