package service

func (s *Service) SetTransferNotifier(notify func(string, string)) {
	s.transfers.mu.Lock()
	defer s.transfers.mu.Unlock()
	s.transfers.notify = notify
}
func (s *Service) Transfers() []TransferTask      { return s.transfers.List() }
func (s *Service) CancelTransfer(id string) error { return s.transfers.Cancel(id) }
func (t TransferTask) Active() bool               { return transferActive(t.State) }
