package discovery

import (
	"encoding/base64"
	"polysync/internal/model"
	"testing"
	"time"
)

func TestExternalDiscoveryFoundLostAndValidation(t *testing.T) {
	s := NewExternal("self")
	d := model.NearbyDevice{ID: "peer", Name: "Phone", PublicKey: base64.RawStdEncoding.EncodeToString(make([]byte, 32)), Addresses: []string{"192.168.1.2:45123"}, Fingerprint: "untrusted"}
	if err := s.ReplaceExternal([]model.NearbyDevice{d}); err != nil {
		t.Fatal(err)
	}
	d.Addresses[0] = "mutated"
	s.mu.Lock()
	item := s.nearby["peer"]
	item.LastSeen = time.Now().Add(-time.Hour)
	s.nearby["peer"] = item
	s.mu.Unlock()
	devices := s.Nearby(map[string]bool{"peer": true})
	if len(devices) != 1 || !devices[0].Online || !devices[0].Paired || devices[0].Addresses[0] != "192.168.1.2:45123" || devices[0].Fingerprint != Fingerprint(d.PublicKey) {
		t.Fatalf("unexpected devices: %+v", devices)
	}
	for _, address := range []string{"attacker.example:80", "192.168.1.2:0", "0.0.0.0:45123", "224.0.0.251:5353"} {
		d.Addresses = []string{address}
		if err := s.ReplaceExternal([]model.NearbyDevice{d}); err == nil {
			t.Fatalf("accepted %s", address)
		}
	}
	if _, ok := s.Device("peer"); ok {
		t.Fatal("invalid peer kept stale discovery data alive")
	}
	if err := s.ReplaceExternal(nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Device("peer"); ok {
		t.Fatal("lost device retained")
	}
	s.Close()
}
