package discovery

import (
	"encoding/base64"
	"polysync/internal/model"
	"testing"
)

func TestPartialSnapshotAdvancesValidPeersAndExpiresOldOnes(t *testing.T) {
	s := NewExternal("self")
	key := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	old := model.NearbyDevice{ID: "old", PublicKey: key, Addresses: []string{"192.168.1.2:45123"}}
	if err := s.ReplaceExternal([]model.NearbyDevice{old}); err != nil {
		t.Fatal(err)
	}
	good := old
	good.ID = "good"
	bad := old
	bad.ID = "bad"
	bad.Addresses = []string{"evil.example:80"}
	if err := s.ReplaceExternal([]model.NearbyDevice{bad, good}); err == nil {
		t.Fatal("invalid record not reported")
	}
	if _, ok := s.Device("good"); !ok {
		t.Fatal("valid peer discarded")
	}
	if _, ok := s.Device("old"); ok {
		t.Fatal("lost peer retained")
	}
	if _, ok := s.Device("bad"); ok {
		t.Fatal("invalid peer retained")
	}
	if err := s.ReplaceExternal(make([]model.NearbyDevice, 129)); err == nil {
		t.Fatal("oversized snapshot accepted")
	}
	if _, ok := s.Device("good"); !ok {
		t.Fatal("envelope failure unexpectedly changed snapshot")
	}
	if err := s.ReplaceExternal(nil); err != nil {
		t.Fatal(err)
	}
	if len(s.Nearby(nil)) != 0 {
		t.Fatal("empty snapshot did not clear peers")
	}
}
