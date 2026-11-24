package service

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"polysync/internal/model"
	"polysync/internal/syncer"
)

func TestSyncPreservesEditsAfterPlanning(t *testing.T) {
	for _, initiator := range []string{"sender", "receiver"} {
		for _, operation := range []string{"overwrite", "delete", "create"} {
			t.Run(initiator+"_initiates/"+operation, func(t *testing.T) {
				testEditAfterPlanning(t, initiator, operation)
			})
		}
	}
}

func testEditAfterPlanning(t *testing.T, initiator, operation string) {
	const (
		shareID  = "concurrent-edit"
		name     = "notes.txt"
		base     = "base version"
		outgoing = "peer version"
		edited   = "user version"
	)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	sender, receiver := transferPair(t)
	senderRoot, receiverRoot := t.TempDir(), t.TempDir()
	peers := []struct {
		local, remote *Service
		root          string
		baseline      []model.Entry
	}{
		{local: sender, remote: receiver, root: senderRoot},
		{local: receiver, remote: sender, root: receiverRoot},
	}
	for _, peer := range peers {
		share := model.Share{ID: shareID, Name: "Notes", State: "active", Path: peer.root, PeerDeviceID: peer.remote.store.Config().DeviceID}
		if err := peer.local.store.AddShare(share); err != nil {
			t.Fatal(err)
		}
		peer.local.RegisterShare(shareID)
	}
	client, server := sender, receiver
	if initiator == "receiver" {
		client, server = receiver, sender
	}
	serverLock := server.lockFor(shareID)
	syncRound := func() error {
		err := client.SyncShare(ctx, shareID, true)
		// The server releases its share lock after recording the round's outcome.
		serverLock.Lock()
		serverLock.Unlock()
		return err
	}

	// Keep a nonempty common baseline even when the planned destination is absent.
	writeTestFile(t, senderRoot, "unchanged.txt", "original")
	if operation != "create" {
		writeTestFile(t, senderRoot, name, base)
	}
	if err := syncRound(); err != nil {
		t.Fatal(err)
	}
	for i, peer := range peers {
		baseline, err := peer.local.store.LoadBaseline(shareID, peer.remote.store.Config().DeviceID)
		if err != nil {
			t.Fatal(err)
		}
		peers[i].baseline = baseline
	}
	if operation == "delete" {
		if err := os.Remove(filepath.Join(senderRoot, name)); err != nil {
			t.Fatal(err)
		}
	} else {
		writeTestFile(t, senderRoot, name, outgoing)
	}

	target := filepath.Join(receiverRoot, name)
	var originalTime time.Time
	if operation != "create" {
		info, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		originalTime = info.ModTime()
	}
	// Save on the receiver after both scans and planning, before either peer applies it.
	// Existing files deliberately retain their original size and modification time.
	var saved bool
	var saveErr error
	serverLock.Lock()
	buildPlan := server.planSync
	server.planSync = func(serverEntries, clientEntries, baseline []model.Entry, clientName string, now time.Time) model.Plan {
		plan := buildPlan(serverEntries, clientEntries, baseline, clientName, now)
		saved = true
		saveErr = os.WriteFile(target, []byte(edited), 0o644)
		if saveErr == nil && !originalTime.IsZero() {
			saveErr = os.Chtimes(target, originalTime, originalTime)
		}
		return plan
	}
	serverLock.Unlock()
	err := syncRound()
	serverLock.Lock()
	server.planSync = buildPlan
	serverLock.Unlock()
	if !saved || saveErr != nil {
		t.Fatalf("edit did not complete after planning: saved=%t, err=%v", saved, saveErr)
	}
	if err == nil {
		t.Error("sync accepted a stale plan")
	}
	assertTestFile(t, receiverRoot, name, edited)
	for _, peer := range peers {
		baseline, err := peer.local.store.LoadBaseline(shareID, peer.remote.store.Config().DeviceID)
		if err != nil || !slices.Equal(baseline, peer.baseline) {
			t.Fatalf("stale plan advanced baseline at %s: %v, %v", peer.root, baseline, err)
		}
		if status := peer.local.Statuses()[shareID]; status.State == "synced" {
			t.Fatalf("stale plan reported success at %s: %+v", peer.root, status)
		}
	}

	if err := syncRound(); err != nil {
		t.Fatalf("retry with fresh plan: %v", err)
	}
	primary, copyContent := edited, outgoing
	if server == sender && operation != "delete" {
		primary, copyContent = outgoing, edited
	}
	for _, peer := range peers {
		assertTestFile(t, peer.root, name, primary)
		records, err := peer.local.store.Conflicts()
		if err != nil || len(records) != 1 || records[0].Path != name || records[0].Status != "pending" {
			t.Fatalf("expected a conflict for the concurrent edit at %s: %+v, %v", peer.root, records, err)
		}
		if operation != "delete" {
			assertTestFile(t, peer.root, records[0].ConflictCopyPath, copyContent)
		}
		manifest, err := syncer.Scan(ctx, peer.root)
		if err != nil {
			t.Fatal(err)
		}
		baseline, err := peer.local.store.LoadBaseline(shareID, peer.remote.store.Config().DeviceID)
		if err != nil || !manifestsEqual(baseline, manifest) {
			t.Fatalf("retry baseline mismatch at %s: %v, %v", peer.root, baseline, err)
		}
	}
}
