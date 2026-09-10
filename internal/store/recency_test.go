package store

import (
	"testing"
	"time"
)

func TestLastSyncTimeSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, "127.0.0.1:0", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if !s.LastSyncTime("share", "peer").IsZero() {
		t.Fatal("new folder has a last sync")
	}
	before := time.Now().Add(-time.Second)
	if err := s.SaveBaseline("share", "peer", nil); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir, "127.0.0.1:0", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.LastSyncTime("share", "peer").After(before) {
		t.Fatal("last sync lost on restart")
	}
	if !reopened.LastSyncTime("share", "other").IsZero() {
		t.Fatal("last sync leaked across peers")
	}
}
