package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"polysync/internal/model"
	"polysync/internal/protocol"
	"polysync/internal/store"
)

func transferPair(t *testing.T) (*Service, *Service) {
	t.Helper()
	makeService := func() *Service {
		data, err := store.Open(t.TempDir(), "127.0.0.1:0", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		s := New(data)
		if err := s.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.Stop)
		return s
	}
	a, b := makeService(), makeService()
	for _, pair := range [][2]*Service{{a, b}, {b, a}} {
		config := pair[1].store.Config()
		err := pair[0].store.SavePairedDevice(model.PairedDevice{ID: config.DeviceID, Name: config.DeviceName, PublicKey: config.IdentityPublicKey, Addresses: []string{"127.0.0.1:" + intText(pair[1].ListenPort())}})
		if err != nil {
			t.Fatal(err)
		}
	}
	return a, b
}
func sendTestTransfer(t *testing.T, a, b *Service, contents []string) TransferTask {
	t.Helper()
	files := make([]TransferFile, len(contents))
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for i, text := range contents {
		files[i] = TransferFile{Name: "文档.txt", Size: int64(len(text))}
		part, err := writer.CreateFormFile("file", files[i].Name)
		if err != nil {
			t.Fatal(err)
		}
		part.Write([]byte(text))
	}
	writer.Close()
	task, err := a.CreateTransfer(b.store.Config().DeviceID, files)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.UploadTransfer(context.Background(), task.ID, multipart.NewReader(&body, writer.Boundary()), nil); err != nil {
		t.Fatal(err)
	}
	return waitTransfer(t, a, task.ID)
}
func waitTransfer(t *testing.T, s *Service, id string) TransferTask {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		task, err := s.transfers.Task(id)
		if err != nil {
			t.Fatal(err)
		}
		if !transferActive(task.State) {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("transfer did not finish")
	return TransferTask{}
}
func TestTransferAutoReceiveSaveAndRestart(t *testing.T) {
	a, b := transferPair(t)
	sent := sendTestTransfer(t, a, b, []string{"from A", ""})
	if sent.State != "complete" {
		t.Fatalf("send failed: %s", sent.Message)
	}
	received := b.transfers.List()
	if len(received) != 1 || received[0].State != "complete" {
		t.Fatalf("receive not completed: %#v", received)
	}
	task := received[0]
	for i, want := range []string{"from A", ""} {
		f, err := transferFileOpen(task, i)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(f)
		f.Close()
		if string(data) != want {
			t.Fatal("received content mismatch")
		}
	}
	dest := filepath.Join(t.TempDir(), "saved.txt")
	if err := b.transfers.SaveCopy(task.ID, 0, dest, false); err != nil {
		t.Fatal(err)
	}
	if err := b.transfers.SaveCopy(task.ID, 0, dest, false); err == nil {
		t.Fatal("overwrote existing destination without consent")
	}
	os.WriteFile(dest, []byte("existing"), 0600)
	if err := b.transfers.SaveCopy(task.ID, 0, dest, true); err != nil {
		t.Fatal(err)
	}
	original, err := transferFileOpen(task, 0)
	if err != nil {
		t.Fatal("save removed original")
	}
	original.Close()
	settings := b.transfers.Settings()
	settings.Directory = t.TempDir()
	if err := b.SetTransferSettings(settings); err != nil {
		t.Fatal(err)
	}
	again := sendTestTransfer(t, a, b, []string{"new inbox"})
	if again.State != "complete" {
		t.Fatal(again.Message)
	}
	newest := b.transfers.List()[0]
	if filepath.Dir(newest.StorageDir) != settings.Directory || newest.StorageDir == task.StorageDir {
		t.Fatal("inbox switch lost task location")
	}
	loaded := newTransferManager(b.store.Dir())
	if err := loaded.load(); err != nil {
		t.Fatal(err)
	}
	if len(loaded.List()) != 2 || loaded.Settings().Directory != settings.Directory {
		t.Fatal("settings or records lost after restart")
	}
	os.Remove(filepath.Join(task.StorageDir, task.Files[0].Blob))
	for _, entry := range loaded.List() {
		if entry.ID == task.ID && entry.Files[0].State != "missing" {
			t.Fatal("missing file not indicated")
		}
	}
	if err := loaded.Remove(newest.ID, false); err != nil {
		t.Fatal(err)
	}
	file, err := transferFileOpen(newest, 0)
	if err != nil {
		t.Fatal("record removal deleted content")
	}
	file.Close()
	if len(a.store.Config().Shares) != 0 || len(b.store.Config().Shares) != 0 {
		t.Fatal("temporary transfer created a sync relationship")
	}
}
func TestTransferReceiverDisabledAndRetry(t *testing.T) {
	a, b := transferPair(t)
	settings := b.transfers.Settings()
	settings.AutoReceive = false
	if err := b.SetTransferSettings(settings); err != nil {
		t.Fatal(err)
	}
	sent := sendTestTransfer(t, a, b, []string{"must not arrive"})
	if sent.State != "failed" || !strings.Contains(sent.Message, "关闭") {
		t.Fatal(sent)
	}
	if len(b.transfers.List()) != 0 {
		t.Fatal("disabled receiver created a task")
	}
	settings.AutoReceive = true
	if err := b.SetTransferSettings(settings); err != nil {
		t.Fatal(err)
	}
	task, err := a.RetryTransfer(sent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task = waitTransfer(t, a, task.ID); task.State != "complete" {
		t.Fatal(task)
	}
}
func TestTransferRejectsUnpairedAndBadNames(t *testing.T) {
	a, b := transferPair(t)
	// An unrelated client has a valid PolySync certificate but is not paired.
	data, err := store.Open(t.TempDir(), "127.0.0.1:0", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	stranger := New(data)
	connection, err := stranger.dialTLS(context.Background(), "127.0.0.1:"+intText(b.ListenPort()), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	framer := protocol.NewFramer(connection)
	config := stranger.store.Config()
	framer.WriteJSON(protocol.Hello{Type: "hello", Protocol: model.ProtocolVersion, Operation: "transfer", DeviceID: config.DeviceID})
	var result protocol.Result
	if err := framer.ReadJSON(&result); err != nil {
		t.Fatal(err)
	}
	if result.OK {
		t.Fatal("unpaired sender accepted")
	}
	for _, name := range []string{"../escape", "C:\\escape", "a/b", "CON", "file:stream", "x\x00y", "x."} {
		if _, err := a.CreateTransfer(b.store.Config().DeviceID, []TransferFile{{Name: name, Size: 1}}); err == nil {
			t.Fatalf("unsafe name allowed: %q", name)
		}
	}
}
func TestTransferCorruptPayloadAndCancel(t *testing.T) {
	a, b := transferPair(t)
	peer, _ := a.store.PairedDevice(b.store.Config().DeviceID)
	connection, err := a.dialTLS(context.Background(), peer.Addresses[0], &peer)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	framer := protocol.NewFramer(connection)
	framer.WriteJSON(protocol.Hello{Type: "hello", Protocol: model.ProtocolVersion, Operation: "transfer", DeviceID: a.store.Config().DeviceID})
	var result protocol.Result
	framer.ReadJSON(&result)
	hash := sha256.Sum256([]byte("good"))
	framer.WriteJSON(transferOffer{Type: "transfer_offer", Files: []TransferFile{{Name: "x.txt", Size: 4, Hash: hex.EncodeToString(hash[:])}}})
	framer.ReadJSON(&result)
	if !result.OK {
		t.Fatal(result.Message)
	}
	framer.CopyFrom(strings.NewReader("evil"), 4)
	framer.ReadJSON(&result)
	if result.OK {
		t.Fatal("corrupt file acknowledged")
	}
	task := waitTransfer(t, b, b.transfers.List()[0].ID)
	if task.State != "failed" {
		t.Fatal(task)
	}
	entries, _ := os.ReadDir(task.StorageDir)
	if len(entries) != 0 {
		t.Fatal("corrupt partial file retained")
	}
	// An upload that stalls must be cancellable before the browser supplies bytes.
	pending, err := a.CreateTransfer(peer.ID, []TransferFile{{Name: "stall.txt", Size: 10}})
	if err != nil {
		t.Fatal(err)
	}
	pr, pw := io.Pipe()
	defer pw.Close()
	finished := make(chan error, 1)
	go func() {
		finished <- a.UploadTransfer(context.Background(), pending.ID, multipart.NewReader(pr, "boundary"), pr)
	}()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		a.transfers.mu.Lock()
		bound := a.transfers.cancels[pending.ID] != nil
		a.transfers.mu.Unlock()
		if bound {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err := a.transfers.Cancel(pending.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("cancelled upload succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not close stalled upload")
	}
}
func TestTransferDirectoryOverlapAndRecovery(t *testing.T) {
	a, _ := transferPair(t)
	root := t.TempDir()
	if err := a.store.AddShare(model.Share{ID: "sync", Path: root}); err != nil {
		t.Fatal(err)
	}
	settings := a.transfers.Settings()
	settings.Directory = filepath.Join(root, "inbox")
	if err := a.SetTransferSettings(settings); err == nil {
		t.Fatal("overlapping inbox accepted")
	}
	if err := a.saveTransferAwareShare(model.Share{ID: "reverse", Path: a.transfers.Settings().Directory}, false); err == nil {
		t.Fatal("sync may include inbox")
	}
	task, err := a.transfers.create("peer", "test", "receive", []TransferFile{{Name: "interrupted.txt", Size: 9}})
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(task.StorageDir, task.Files[0].Blob+".part"), []byte("partial"), 0600)
	loaded := newTransferManager(a.store.Dir())
	if err := loaded.load(); err != nil {
		t.Fatal(err)
	}
	recovered, _ := loaded.Task(task.ID)
	if recovered.State != "interrupted" {
		t.Fatal("unfinished task not interrupted")
	}
	entries, _ := os.ReadDir(task.StorageDir)
	if len(entries) != 0 {
		t.Fatal("incomplete payload retained after restart")
	}
}

func TestTransferSaveCancellationPreservesDestination(t *testing.T) {
	a, b := transferPair(t)
	sendTestTransfer(t, a, b, []string{"complete file"})
	received := b.transfers.List()[0]
	destination := filepath.Join(t.TempDir(), "saved.txt")
	if err := os.WriteFile(destination, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := b.transfers.saveCopyContext(ctx, received.ID, 0, destination, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	content, err := os.ReadFile(destination)
	if err != nil || string(content) != "keep" {
		t.Fatal("destination changed on cancellation", err)
	}
}
