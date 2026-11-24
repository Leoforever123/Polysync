package service

import (
	"errors"
	"slices"

	"polysync/internal/model"
)

// The ledger belongs to this folder replica. Retaining acknowledgements for
// unseen events lets a collaborator relay decisions without a local dialog.
func (s *Service) groupResolvedIDs(sh model.Share, state model.GroupState) ([]string, error) {
	records, err := s.store.ShareConflicts(sh.ID)
	if err != nil {
		return nil, err
	}
	ids := slices.Clone(state.Resolved)
	for _, c := range records {
		if c.ShareID == sh.ID && c.Status == "resolved" {
			ids = append(ids, c.ID)
		}
	}
	return sortedIDs(ids), nil
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
	joined := sortedIDs(append(slices.Clone(state.Resolved), incoming...))
	if !slices.Equal(joined, state.Resolved) {
		state.Resolved = joined
		if err := s.store.SaveGroupState(sh.ReplicaID, state); err != nil {
			return err
		}
	}
	return s.markConflictsResolved(sh.ID, state.Resolved)
}
