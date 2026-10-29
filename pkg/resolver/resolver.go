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

	if hme := byIDOrAddress(ref, emails); hme != nil {
		return hme, nil
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

// byIDOrAddress finds ref as an anonymousId (full, or a unique prefix
// of 6+ characters) or a full address; nil when neither.
func byIDOrAddress(ref string, emails []api.HmeEmail) *api.HmeEmail {
	for i := range emails {
		if emails[i].AnonymousID == ref {
			return &emails[i]
		}
	}

	if len(ref) >= 6 {
		var idMatches []*api.HmeEmail
		for i := range emails {
			if strings.HasPrefix(emails[i].AnonymousID, ref) {
				idMatches = append(idMatches, &emails[i])
			}
		}
		if len(idMatches) == 1 {
			return idMatches[0]
		}
	}

	for i := range emails {
		if emails[i].Hme == ref {
			return &emails[i]
		}
	}
	return nil
}

// ResolveAll resolves refs for a batch, in order. It takes IDs and
// full addresses only: a label is a guess at one address, and an
// unquoted "$label" that splits in two must not become two matches.
// An address named twice comes back once. If any ref fails, none
// come back: a batch that changes the account must not start on a
// typo.
func ResolveAll(refs []string, emails []api.HmeEmail) ([]api.HmeEmail, error) {
	var (
		found []api.HmeEmail
		bad   []string
		seen  = map[string]bool{}
	)
	for _, ref := range refs {
		hme := byIDOrAddress(strings.TrimSpace(ref), emails)
		if hme == nil {
			bad = append(bad, fmt.Sprintf("%q", ref))
			continue
		}
		if !seen[hme.AnonymousID] {
			seen[hme.AnonymousID] = true
			found = append(found, *hme)
		}
	}
	if len(bad) > 0 {
		return nil, fmt.Errorf("nothing changed: not an ID or address (several at once take the ID column of 'ihme list' or full addresses, not labels)\n  %s",
			strings.Join(bad, "\n  "))
	}
	return found, nil
}
