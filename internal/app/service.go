// Package app is the application service shared by the Cobra
// commands and the embedded agent's tools: both are thin adapters
// over these operations. Domain logic (candidate generation,
// ref resolution, metadata merge semantics) lives here exactly
// once; rendering, prompting, and consent stay in the adapters.
package app

import (
	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/pkg/filter"
	"github.com/lroolle/ihme-cli/pkg/resolver"
	"github.com/lroolle/ihme-cli/pkg/tags"
)

// API is the slice of api.Client the service depends on.
type API interface {
	ListHme() (*api.ListHmeResult, error)
	GenerateHme() (string, error)
	ReserveHme(hme, label, note string) (*api.HmeEmail, error)
	UpdateHmeMetadata(anonymousID, label, note string) error
	DeactivateHme(anonymousID string) error
}

// Service exposes the HME operations used by both adapters.
type Service struct {
	api API
}

func New(a API) *Service { return &Service{api: a} }

// List returns addresses with filters applied.
func (s *Service) List(opts filter.Options) ([]api.HmeEmail, error) {
	result, err := s.api.ListHme()
	if err != nil {
		return nil, err
	}
	return filter.Apply(result.HmeEmails, opts), nil
}

// Resolve finds one address by anonymousId prefix, email, or label.
func (s *Service) Resolve(ref string) (*api.HmeEmail, error) {
	result, err := s.api.ListHme()
	if err != nil {
		return nil, err
	}
	return resolver.Resolve(ref, result.HmeEmails)
}

// Generate collects up to n distinct candidate addresses. Apple
// returns candidates one at a time and may repeat; three consecutive
// duplicates end the round early. A partial pool is returned if an
// error occurs after at least one candidate was collected.
func (s *Service) Generate(n int) ([]string, error) {
	seen := make(map[string]bool)
	var candidates []string
	dupeStreak := 0
	for dupeStreak < 3 && len(candidates) < n {
		hme, err := s.api.GenerateHme()
		if err != nil {
			if len(candidates) > 0 {
				break
			}
			return nil, err
		}
		if seen[hme] {
			dupeStreak++
			continue
		}
		seen[hme] = true
		dupeStreak = 0
		candidates = append(candidates, hme)
	}
	return candidates, nil
}

// Reserve claims a generated candidate under a label, serializing
// tags into the note field.
func (s *Service) Reserve(address, label string, tagList []string, note string) (*api.HmeEmail, error) {
	return s.api.ReserveHme(address, label, tags.Serialize(tagList, note))
}

// Deactivate resolves ref and deactivates it. changed is false when
// the address was already inactive (no API call made).
func (s *Service) Deactivate(ref string) (hme *api.HmeEmail, changed bool, err error) {
	hme, err = s.Resolve(ref)
	if err != nil {
		return nil, false, err
	}
	if !hme.IsActive {
		return hme, false, nil
	}
	if err := s.api.DeactivateHme(hme.AnonymousID); err != nil {
		return nil, false, err
	}
	return hme, true, nil
}

// MetaPatch updates only its non-nil fields; Tags replaces all
// existing tags (not additive), matching `ihme edit` semantics.
type MetaPatch struct {
	Label *string
	Note  *string
	Tags  *[]string
}

// UpdateMeta resolves ref and applies the patch, preserving
// unspecified fields.
func (s *Service) UpdateMeta(ref string, patch MetaPatch) (*api.HmeEmail, error) {
	hme, err := s.Resolve(ref)
	if err != nil {
		return nil, err
	}

	newLabel := hme.Label
	if patch.Label != nil {
		newLabel = *patch.Label
	}
	parsed := tags.Parse(hme.Note)
	newNote := parsed.Note
	if patch.Note != nil {
		newNote = *patch.Note
	}
	newTags := parsed.Tags
	if patch.Tags != nil {
		newTags = *patch.Tags
	}

	if err := s.api.UpdateHmeMetadata(hme.AnonymousID, newLabel, tags.Serialize(newTags, newNote)); err != nil {
		return nil, err
	}
	return hme, nil
}
