package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"polysync/internal/model"
	"polysync/internal/store"
)

func TestBidirectionalSyncDeletionAndConflict(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	leftRoot := filepath.Join(t.TempDir(), "left")
	rightRoot := filepath.Join(t.TempDir(), "right")
	if err := os.MkdirAll(leftRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rightRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, leftRoot, "left.txt", "from left")
	writeTestFile(t, rightRoot, "right.txt", "from right")

	leftStore, err := store.Open(t.TempDir(), "127.0.0.1:0", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rightStore, err := store.Open(t.TempDir(), "127.0.0.1:0", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	const shareID = "integration-share"
	const secret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	leftConfig := leftStore.Config()
	rightConfig := rightStore.Config()
	if err := leftStore.SavePairedDevice(model.PairedDevice{ID: rightConfig.DeviceID, Name: rightConfig.DeviceName, PublicKey: rightConfig.IdentityPublicKey}); err != nil {
		t.Fatal(err)
	}
	if err := rightStore.SavePairedDevice(model.PairedDevice{ID: leftConfig.DeviceID, Name: leftConfig.DeviceName, PublicKey: leftConfig.IdentityPublicKey}); err != nil {
		t.Fatal(err)
	}
	if err := leftStore.AddShare(model.Share{ID: shareID, Secret: secret, Name: "Test", Path: leftRoot, PeerDeviceID: rightConfig.DeviceID, State: "active", IntervalSeconds: 30}); err != nil {
		t.Fatal(err)
	}
	if err := rightStore.AddShare(model.Share{ID: shareID, Secret: secret, Name: "Test", Path: rightRoot, PeerDeviceID: leftConfig.DeviceID, State: "active", IntervalSeconds: 30}); err != nil {
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
	defer func() { cancel(); left.Stop(); right.Stop() }()

	leftShare, _ := leftStore.Share(shareID)
	leftShare.PeerAddress = "127.0.0.1:" + intText(right.ListenPort())
	if err := leftStore.UpdateShare(leftShare); err != nil {
		t.Fatal(err)
	}
	if err := left.SyncShare(ctx, shareID, true); err != nil {
		t.Fatal(err)
	}
	assertTestFile(t, leftRoot, "right.txt", "from right")
	assertTestFile(t, rightRoot, "left.txt", "from left")

	writeTestFile(t, leftRoot, "left.txt", "left edit")
	if err := os.Remove(filepath.Join(leftRoot, "right.txt")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, rightRoot, "new.txt", "new on right")
	if err := left.SyncShare(ctx, shareID, true); err != nil {
		t.Fatal(err)
	}
	assertTestFile(t, rightRoot, "left.txt", "left edit")
	if _, err := os.Stat(filepath.Join(rightRoot, "right.txt")); !os.IsNotExist(err) {
		t.Fatalf("deletion was not propagated: %v", err)
	}
	assertTestFile(t, leftRoot, "new.txt", "new on right")
	if countRegularFiles(t, filepath.Join(rightStore.Dir(), "history", shareID)) == 0 {
		t.Fatal("overwritten and deleted files were not archived")
	}

	writeTestFile(t, leftRoot, "left.txt", "conflict from left")
	writeTestFile(t, rightRoot, "left.txt", "conflict from right")
	if err := left.SyncShare(ctx, shareID, true); err != nil {
		t.Fatal(err)
	}
	assertTestFile(t, leftRoot, "left.txt", "conflict from right")
	assertTestFile(t, rightRoot, "left.txt", "conflict from right")
	leftMatches, _ := filepath.Glob(filepath.Join(leftRoot, "left.polysync-conflict-*.txt"))
	rightMatches, _ := filepath.Glob(filepath.Join(rightRoot, "left.polysync-conflict-*.txt"))
	if len(leftMatches) != 1 || len(rightMatches) != 1 {
		t.Fatalf("conflict copies missing: left=%v right=%v", leftMatches, rightMatches)
	}
	assertTestFile(t, leftRoot, filepath.Base(leftMatches[0]), "conflict from left")
	conflicts, err := leftStore.Conflicts()
	if err != nil || len(conflicts) != 1 || conflicts[0].Status != "pending" {
		t.Fatalf("conflict was not persisted: %#v, %v", conflicts, err)
	}
	content, err := left.ConflictContent(conflicts[0].ID)
	if err != nil || !content.Text || content.Local != "conflict from left" || content.Remote != "conflict from right" {
		t.Fatalf("conflict text versions unavailable: %#v, %v", content, err)
	}
	if err := left.ResolveConflict(ctx, conflicts[0].ID, "merged", "merged result"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, readErr := os.ReadFile(filepath.Join(rightRoot, "left.txt"))
		rightConflicts, _ := rightStore.Conflicts()
		resolved := len(rightConflicts) == 1 && rightConflicts[0].Status == "resolved"
		if readErr == nil && string(data) == "merged result" && resolved {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("merged conflict did not propagate: content=%q err=%v conflicts=%#v", data, readErr, rightConflicts)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func writeTestFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertTestFile(t *testing.T, root, relative, expected string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, relative))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != expected {
		t.Fatalf("%s: got %q, want %q", relative, data, expected)
	}
}

func intText(value int) string { return strings.TrimSpace(fmtInt(value)) }

func fmtInt(value int) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	result := ""
	for value > 0 {
		result = string(digits[value%10]) + result
		value /= 10
	}
	return result
}

func countRegularFiles(t *testing.T, root string) int {
	t.Helper()
	count := 0
	err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return count
}
