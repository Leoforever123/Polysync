package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"polysync/internal/model"
	"polysync/internal/store"
)

func TestPairInviteAndSync(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	leftStore, err := store.Open(t.TempDir(), "127.0.0.1:0", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rightStore, err := store.Open(t.TempDir(), "127.0.0.1:0", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	left := New(leftStore)
	right := New(rightStore)
	if err := left.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := right.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		left.Stop()
		right.Stop()
	}()

	rightConfig := rightStore.Config()
	rightAddress := "127.0.0.1:" + intText(right.ListenPort())
	nearby := model.NearbyDevice{ID: rightConfig.DeviceID, Name: rightConfig.DeviceName, PublicKey: rightConfig.IdentityPublicKey, Addresses: []string{rightAddress}, Online: true}
	sessionID, err := left.startPairingDevice(ctx, nearby, rightAddress)
	if err != nil {
		t.Fatal(err)
	}
	requests := right.PairingRequests()
	if len(requests) != 1 || requests[0].Code != "" {
		t.Fatalf("pairing must wait for receiver approval: %#v", requests)
	}
	if err := right.ApprovePairingRequest(requests[0].ID); err != nil {
		t.Fatal(err)
	}
	requests = right.PairingRequests()
	if len(requests) != 1 || len(requests[0].Code) != 6 {
		t.Fatalf("approved request has no six digit code: %#v", requests)
	}
	if err := left.ConfirmPairing(ctx, sessionID, requests[0].Code); err != nil {
		t.Fatal(err)
	}
	leftConfig := leftStore.Config()
	if _, exists := leftStore.PairedDevice(rightConfig.DeviceID); !exists {
		t.Fatal("initiator did not save paired device")
	}
	if _, exists := rightStore.PairedDevice(leftConfig.DeviceID); !exists {
		t.Fatal("receiver did not save paired device")
	}

	leftRoot := filepath.Join(t.TempDir(), "left")
	rightRoot := filepath.Join(t.TempDir(), "right")
	if err := os.MkdirAll(leftRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rightRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, leftRoot, "paired.txt", "secure sync")
	leftShare, err := left.InviteShare(ctx, rightConfig.DeviceID, "Paired", leftRoot, false, 30)
	if err != nil {
		t.Fatal(err)
	}
	invitations := right.ShareInvitations()
	if len(invitations) != 1 || invitations[0].ShareID != leftShare.ID {
		t.Fatalf("share invitation missing: %#v", invitations)
	}
	if _, err := right.AcceptShareInvitation(ctx, invitations[0].ID, rightRoot, false, 30); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, readErr := os.ReadFile(filepath.Join(rightRoot, "paired.txt"))
		if readErr == nil && string(data) == "secure sync" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("invited folder did not sync: %v", readErr)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
