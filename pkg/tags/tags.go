package tags

import (
	"regexp"
	"strings"
)

var tagRe = regexp.MustCompile(`#(\w+)`)

type Parsed struct {
	Tags []string
	Note string
}

func Parse(note string) Parsed {
	parts := strings.SplitN(note, "|", 2)
	tagPart := parts[0]
	notePart := ""
	if len(parts) == 2 {
		notePart = strings.TrimSpace(parts[1])
	}

	matches := tagRe.FindAllStringSubmatch(tagPart, -1)
	t := make([]string, 0, len(matches))
	for _, m := range matches {
		t = append(t, m[1])
	}

	if len(t) == 0 && notePart == "" {
		notePart = strings.TrimSpace(note)
	}

	return Parsed{Tags: t, Note: notePart}
}

func Serialize(t []string, note string) string {
	if len(t) == 0 {
		return note
	}
	tagStr := ""
	for _, tag := range t {
		if !strings.HasPrefix(tag, "#") {
			tag = "#" + tag
		}
		tagStr += tag + " "
	}
	tagStr = strings.TrimSpace(tagStr)
	if note == "" {
		return tagStr
	}
	return tagStr + " | " + note
}

func All(notes []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, n := range notes {
		for _, t := range Parse(n).Tags {
			if !seen[t] {
				seen[t] = true
				result = append(result, t)
			}
		}
	}
	return result
}
