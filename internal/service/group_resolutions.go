package service

import (
	"errors"
	"slices"

	"polysync/internal/model"
)

// Only confirmed observations in this folder establish that an event exists.
// Peers on 0.5.0 can use bare IDs for a conflict seen on both sides. Newer
// collaborators can relay a decision after committing its witnessed file fork.
func (s *Service) confirmedDecisions(sh model.Share, state model.GroupState, ids []string, includeLocal bool) ([]string, error) {
	records, err := s.store.ShareConflicts(sh.ID)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool)
	for _, rev := range state.Records {
		for event := range rev.Forks {
			known[event] = true
		}
	}
	var result []string
	for _, c := range records {
		if c.ShareID != sh.ID {
			continue
		}
		known[c.ID] = true
		if includeLocal && c.Status == "resolved" {
			result = append(result, c.ID)
		}
	}
	for _, id := range ids {
		if known[id] {
			result = append(result, id)
		}
	}
	return sortedIDs(result), nil
}

func (s *Service) groupResolvedIDs(sh model.Share, state model.GroupState) ([]string, error) {
	return s.confirmedDecisions(sh, state, state.Resolved, true)
}

func sortedIDs(ids []string) []string {
	slices.Sort(ids)
	return slices.Compact(ids)
}

func validateGroupDecisions(ids []string, readOnly bool) error {
	if readOnly && len(ids) > 0 {
		return errors.New("此跟随设备无权发布冲突处理结果")
	}
	for _, id := range ids {
		if !validHex(id, 16) {
			return errors.New("无效的冲突处理记录")
		}
	}
	return nil
}

func (s *Service) commitGroupDecisions(sh model.Share, state model.GroupState, incoming []string) error {
	if len(state.Resolved) == 0 && len(incoming) == 0 {
		return nil
	}
	joined, err := s.confirmedDecisions(sh, state, append(slices.Clone(state.Resolved), incoming...), false)
	if err != nil {
		return err
	}
	if !slices.Equal(joined, state.Resolved) {
		state.Resolved = joined
		if err := s.store.SaveGroupState(sh.ReplicaID, state); err != nil {
			return err
		}
	}
	return s.markConflictsResolved(sh.ID, state.Resolved)
}
