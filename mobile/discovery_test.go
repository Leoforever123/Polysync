package mobile

import (
	"encoding/json"
	"fmt"
	"polysync/internal/model"
	"strings"
	"testing"
)

func TestNativeDiscoveryUsesExistingPairingIdentityChecks(t *testing.T) {
	a, b := testEngine(t), testEngine(t)
	info, err := b.DiscoveryInfo()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(info, "private") {
		t.Fatal("private identity advertised")
	}
	config := b.svc.Store().Config()
	d := model.NearbyDevice{ID: config.DeviceID, Name: config.DeviceName, PublicKey: config.IdentityPublicKey, Addresses: []string{fmt.Sprintf("127.0.0.1:%d", b.svc.ListenPort())}}
	data, _ := json.Marshal([]model.NearbyDevice{d})
	if err = a.UpdateDiscovery(string(data)); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"deviceId": d.ID})
	if _, err = a.Request("POST", "/api/pair/start", string(body)); err != nil {
		t.Fatal(err)
	}
	if len(b.svc.PairingRequests()) != 1 || b.svc.PairingRequests()[0].Code != "" {
		t.Fatal("discovery skipped receiver approval")
	}
	if len(a.svc.Store().Config().PairedDevices) != 0 {
		t.Fatal("discovery trusted an unpaired device")
	}
	d.PublicKey = a.svc.Store().Config().IdentityPublicKey
	data, _ = json.Marshal([]model.NearbyDevice{d})
	if err = a.UpdateDiscovery(string(data)); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Request("POST", "/api/pair/start", string(body)); err == nil {
		t.Fatal("accepted mismatched mDNS/TLS key")
	}
	if err = a.UpdateDiscovery("[]"); err != nil {
		t.Fatal(err)
	}
	if len(a.svc.NearbyDevices()) != 0 {
		t.Fatal("lost device retained")
	}
	if _, err = a.Request("POST", "/api/pair/start", string(body)); err == nil {
		t.Fatal("paired lost device without an address")
	}
}
