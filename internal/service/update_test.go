package service

import (
	"context"
	"polysync/internal/store"
	"testing"
)

func TestUpdateGateRejectsActiveWorkAndResumesAfterFailure(t *testing.T) {
	st, e := store.Open(t.TempDir(), "127.0.0.1:0", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	s := New(st)
	defer s.Stop()
	if e = s.beginSyncWork(); e != nil {
		t.Fatal(e)
	}
	if e = s.PrepareUpdate(); e == nil {
		t.Fatal("update accepted active sync")
	}
	s.endSyncWork()
	_, done, e := s.beginTransferWork(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if e = s.PrepareUpdate(); e == nil {
		t.Fatal("update accepted active transfer")
	}
	done()
	if e = s.PrepareUpdate(); e != nil {
		t.Fatal(e)
	}
	if e = s.beginSyncWork(); e == nil {
		t.Fatal("sync admitted during install")
	}
	if _, _, e = s.beginTransferWork(context.Background()); e == nil {
		t.Fatal("transfer admitted during install")
	}
	if _, e = s.CreateTransfer("unknown", nil); e == nil {
		t.Fatal("new task admitted")
	}
	s.CancelUpdate()
	if e = s.beginSyncWork(); e != nil {
		t.Fatal(e)
	}
	s.endSyncWork()
}
func TestUpdateGateRejectsPendingUpload(t *testing.T) {
	st, e := store.Open(t.TempDir(), "127.0.0.1:0", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	s := New(st)
	defer s.Stop()
	s.transfers.tasks["pending"] = &TransferTask{ID: "pending", State: "preparing"}
	if e = s.PrepareUpdate(); e == nil {
		t.Fatal("pending upload interrupted")
	}
	delete(s.transfers.tasks, "pending")
	if e = s.PrepareUpdate(); e != nil {
		t.Fatal(e)
	}
}
