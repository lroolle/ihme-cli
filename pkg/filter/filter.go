package filter

import (
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
	Sort     string // "date", "label", "date:asc", "label:desc"
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

	if opts.Sort != "" {
		field, dir := parseSort(opts.Sort)
		sort.Slice(result, func(i, j int) bool {
			var less bool
			switch field {
			case "label":
				less = strings.ToLower(result[i].Label) < strings.ToLower(result[j].Label)
			default:
				less = result[i].CreateTimestamp > result[j].CreateTimestamp
			}
			if dir == "asc" && field == "date" {
				less = !less
			}
			if dir == "desc" && field == "label" {
				less = !less
			}
			return less
		})
	}

	return result
}

func parseSort(s string) (field, dir string) {
	parts := strings.SplitN(s, ":", 2)
	field = parts[0]
	if len(parts) == 2 {
		dir = parts[1]
	} else {
		switch field {
		case "label":
			dir = "asc"
		default:
			dir = "desc"
		}
	}
	return
}
