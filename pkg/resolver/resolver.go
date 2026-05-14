package resolver

import (
	"fmt"
	"strings"

	"github.com/lroolle/ihme-cli/api"
)

func Resolve(ref string, emails []api.HmeEmail) (*api.HmeEmail, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("empty reference")
	}

	for i := range emails {
		if emails[i].AnonymousID == ref {
			return &emails[i], nil
		}
	}

	for i := range emails {
		if emails[i].Hme == ref {
			return &emails[i], nil
		}
	}

	lower := strings.ToLower(ref)
	for i := range emails {
		if strings.ToLower(emails[i].Label) == lower {
			return &emails[i], nil
		}
	}

	var matches []*api.HmeEmail
	for i := range emails {
		if strings.Contains(strings.ToLower(emails[i].Label), lower) {
			matches = append(matches, &emails[i])
		}
	}

	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no address matching %q", ref)
	case 1:
		return matches[0], nil
	default:
		labels := make([]string, len(matches))
		for i, m := range matches {
			labels[i] = m.Label
		}
		return nil, fmt.Errorf("ambiguous reference %q matches %d addresses: %s", ref, len(matches), strings.Join(labels, ", "))
	}
}
