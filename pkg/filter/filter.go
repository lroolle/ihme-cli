package filter

import (
	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/pkg/tags"
)

type Options struct {
	Active   bool
	Inactive bool
	Tag      string
}

func Apply(emails []api.HmeEmail, opts Options) []api.HmeEmail {
	result := make([]api.HmeEmail, 0, len(emails))

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
		result = append(result, e)
	}
	return result
}
