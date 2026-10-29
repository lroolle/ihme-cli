package filter

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/pkg/tags"
)

type Options struct {
	Active   bool
	Inactive bool
	Tag      string
	Search   string
	Sort     string // "date", "date:asc", "label", "label:desc"
}

// SortUsage is the --sort flag help, shared by list and export.
const SortUsage = "Sort by: date (newest first), date:asc (oldest first), label, label:desc"

// Validate rejects a --sort value it does not know. Unknown values
// used to fall through to newest first, so a guess like "--sort
// oldest" silently listed the opposite of what it asked for; now
// it errors and names the grammar.
func (o Options) Validate() error {
	if o.Sort == "" {
		return nil
	}
	if _, _, ok := parseSort(o.Sort); !ok {
		return fmt.Errorf("unknown --sort %q — use date, date:asc, label, or label:desc", o.Sort)
	}
	return nil
}

func Apply(emails []api.HmeEmail, opts Options) []api.HmeEmail {
	result := make([]api.HmeEmail, 0, len(emails))

	var searchLower string
	if opts.Search != "" {
		searchLower = strings.ToLower(opts.Search)
	}

	for _, e := range emails {
		if opts.Active && !e.IsActive {
			continue
		}
		if opts.Inactive && e.IsActive {
			continue
		}
		if opts.Tag != "" {
			parsed := tags.Parse(e.Note)
			found := false
			for _, t := range parsed.Tags {
				if t == opts.Tag {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if searchLower != "" {
			if !strings.Contains(strings.ToLower(e.Label), searchLower) &&
				!strings.Contains(strings.ToLower(e.Hme), searchLower) &&
				!strings.Contains(strings.ToLower(e.Note), searchLower) {
				continue
			}
		}
		result = append(result, e)
	}

	if field, desc, ok := parseSort(opts.Sort); ok {
		less := func(a, b api.HmeEmail) bool {
			if field == "label" {
				return strings.ToLower(a.Label) < strings.ToLower(b.Label)
			}
			return a.CreateTimestamp < b.CreateTimestamp
		}
		// Strict both ways (flipping < used to give <=) and stable,
		// so addresses created in the same millisecond keep Apple's
		// order.
		sort.SliceStable(result, func(i, j int) bool {
			if desc {
				return less(result[j], result[i])
			}
			return less(result[i], result[j])
		})
	}

	return result
}

// parseSort reads field[:asc|:desc]. A bare date means newest
// first, a bare label A to Z. One grammar, no word aliases such as
// oldest (TASTE.md).
func parseSort(s string) (field string, desc bool, ok bool) {
	field, dir, _ := strings.Cut(s, ":")
	if field != "date" && field != "label" {
		return "", false, false
	}
	switch dir {
	case "":
		return field, field == "date", true
	case "asc":
		return field, false, true
	case "desc":
		return field, true, true
	}
	return "", false, false
}
