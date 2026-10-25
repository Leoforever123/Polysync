package service

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"polysync/internal/model"
	"polysync/internal/store"
	"strings"
	"testing"
	"time"
)

func basicService(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(t.TempDir(), "127.0.0.1:0", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := New(st)
	t.Cleanup(s.Stop)
	return s
}
func TestCorruptConflictReturnsExplicitStatusError(t *testing.T) {
	s := basicService(t)
	if err := s.store.SaveConflict(model.Conflict{ID: "bad", ShareID: "test", Status: "pending"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.store.Dir(), "conflicts", "test", "bad.json")
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	result := httptest.NewRecorder()
	s.handleStatus(result, httptest.NewRequest("GET", "/api/status", nil))
	if result.Code != 500 || !strings.Contains(result.Body.String(), "冲突记录读取失败") {
		t.Fatal(result.Code, result.Body.String())
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	result = httptest.NewRecorder()
	s.handleStatus(result, httptest.NewRequest("GET", "/api/status", nil))
	if result.Code != 200 {
		t.Fatal(result.Code, result.Body.String())
	}
}
func TestConflictResolutionChecksObjectIntegrityAndShareLock(t *testing.T) {
	s := basicService(t)
	root := t.TempDir()
	if err := s.store.AddShare(model.Share{ID: "test", Path: root, Name: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte("expected"))
	h := hex.EncodeToString(hash[:])
	p := s.store.ObjectPath(h)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("corrupt!"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.store.SaveConflict(model.Conflict{ID: "conflict", ShareID: "test", Path: "file", Status: "pending", RemoteExists: true, RemoteHash: h}); err != nil {
		t.Fatal(err)
	}
	lock := s.lockFor("test")
	lock.Lock()
	err := s.ResolveConflict(context.Background(), "conflict", "remote", "")
	lock.Unlock()
	if err != ErrBusy {
		t.Fatal("resolution must exclude concurrent sync", err)
	}
	if err := s.ResolveConflict(context.Background(), "conflict", "remote", ""); err == nil {
		t.Fatal("corrupt object accepted")
	}
	original, err := os.ReadFile(filepath.Join(root, "file"))
	if err != nil || string(original) != "original" {
		t.Fatal("destination changed", err)
	}
	conflict, _, err := s.store.Conflict("conflict")
	if err != nil || conflict.Status != "pending" {
		t.Fatal("conflict resolved after failure", err)
	}
	if err := os.WriteFile(p, []byte("expected"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveConflict(context.Background(), "conflict", "remote", ""); err != nil {
		t.Fatal(err)
	}
	original, err = os.ReadFile(filepath.Join(root, "file"))
	if err != nil || string(original) != "expected" {
		t.Fatal("recovery failed", err)
	}
	deadline := time.Now().Add(time.Second)
	for s.Statuses()["test"].LastAttempt.IsZero() {
		if time.Now().After(deadline) {
			t.Fatal("resolution did not schedule sync after unlocking")
		}
		time.Sleep(time.Millisecond)
	}
}
func TestActivityCheckIncludesPendingAndCoreWorkWithoutDiskReads(t *testing.T) {
	s := basicService(t)
	if err := os.MkdirAll(filepath.Join(s.store.Dir(), "conflicts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.store.Dir(), "conflicts", "bad.json"), []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	if s.HasActiveWork() {
		t.Fatal("idle")
	}
	if err := s.beginSyncWork(); err != nil {
		t.Fatal(err)
	}
	if !s.HasActiveWork() {
		t.Fatal("sync omitted")
	}
	s.endSyncWork()
	_, done, err := s.beginTransferWork(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !s.HasActiveWork() {
		t.Fatal("preparation omitted")
	}
	done()
	s.transfers.tasks["pending"] = &TransferTask{ID: "pending", State: "preparing"}
	if !s.HasActiveWork() {
		t.Fatal("pending upload omitted")
	}
	s.transfers.tasks["pending"].State = "complete"
	if s.HasActiveWork() {
		t.Fatal("completed receipt treated as active")
	}
}
func TestInboundTLSHandshakeTimesOut(t *testing.T) {
	s := basicService(t)
	cert, err := s.identityCertificate()
	if err != nil {
		t.Fatal(err)
	}
	s.certificate = cert
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		done <- inboundHandshake(context.Background(), tls.Server(server, s.serverTLSConfig()), 40*time.Millisecond)
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("idle TLS accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("handshake leaked")
	}
}
func TestInboundConnectionBudgetRecoversAndStopReleasesPort(t *testing.T) {
	s := basicService(t)
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	address := fmt.Sprintf("127.0.0.1:%d", s.ListenPort())
	connections := []net.Conn{}
	defer func() {
		for _, c := range connections {
			c.Close()
		}
	}()
	wait := func(n int) {
		t.Helper()
		until := time.Now().Add(2 * time.Second)
		for len(s.inboundSlots) != n {
			if time.Now().After(until) {
				t.Fatalf("slots=%d want %d", len(s.inboundSlots), n)
			}
			time.Sleep(time.Millisecond)
		}
	}
	for i := 0; i < cap(s.inboundSlots); i++ {
		c, err := net.DialTimeout("tcp", address, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, c)
	}
	wait(cap(s.inboundSlots))
	extra, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer extra.Close()
	if err := extra.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := extra.Read(make([]byte, 1)); err == nil {
		t.Fatal("excess connection retained")
	} else if e, ok := err.(net.Error); ok && e.Timeout() {
		t.Fatal("budget did not close excess connection")
	}
	for _, c := range connections {
		c.Close()
	}
	wait(0)
	secure, err := s.dialTLS(context.Background(), address, nil)
	if err != nil {
		t.Fatal("budget did not recover", err)
	}
	secure.Close()
	s.Stop()
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal("port retained", err)
	}
	listener.Close()
}
func TestPairAdmissionsAreBoundedDeduplicatedAndExpire(t *testing.T) {
	s := basicService(t)
	now := time.Now()
	makePending := func(i int) pendingPair {
		return pendingPair{Request: model.PairingRequest{ID: fmt.Sprint(i), DeviceID: fmt.Sprint(i), PublicKey: fmt.Sprint(i), Code: "123456", ExpiresAt: now.Add(time.Minute)}}
	}
	for i := 0; i < 8; i++ {
		if err := s.admitPairRequest("host", makePending(i), now); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.admitPairRequest("host", makePending(8), now); err == nil {
		t.Fatal("source rate unbounded")
	}
	other := makePending(0)
	other.Request.PublicKey = "attacker"
	other.Request.DeviceID = "attacker"
	if err := s.admitPairRequest("other", other, now); err == nil {
		t.Fatal("request overwritten")
	}
	if s.pairingRequests["0"].Request.PublicKey != "0" {
		t.Fatal("pending identity replaced")
	}
	for i := 8; i < 32; i++ {
		if err := s.admitPairRequest(fmt.Sprint(i), makePending(i), now); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.admitPairRequest("overflow", makePending(33), now); err == nil {
		t.Fatal("pending requests unbounded")
	}
	later := now.Add(61 * time.Second)
	next := makePending(34)
	next.Request.ExpiresAt = later.Add(time.Minute)
	if err := s.admitPairRequest("host", next, later); err != nil {
		t.Fatal("expired budget retained", err)
	}
	if len(s.pairingRequests) != 1 || len(s.pairSources) != 1 {
		t.Fatal("expired state retained")
	}
}
