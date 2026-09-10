package service

import (
	"context"
	"crypto/tls"
	"net"
	"polysync/internal/model"
	"testing"
	"time"

	"polysync/internal/store"
)

func TestStopClosesIdleConnectionsAndReleasesPort(t *testing.T) {
	data, err := store.Open(t.TempDir(), "127.0.0.1:0", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := New(data)
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	address := net.JoinHostPort("127.0.0.1", intText(s.ListenPort()))
	// An accepted connection that never even sends a TLS handshake must not
	// prevent a user from exiting the tray.
	raw, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	// Also keep an authenticated TLS connection idle before the protocol hello.
	peer := New(data)
	secure, err := peer.dialTLS(context.Background(), address, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer secure.Close()
	done := make(chan struct{})
	go func() { s.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown blocked on an idle peer")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("port still occupied after Stop: %v", err)
	}
	listener.Close()
	if _, err := s.dialTLS(context.Background(), address, nil); err == nil {
		t.Fatal("stopped service accepted outbound work")
	}
}

func TestStopCancelsOutgoingSync(t *testing.T) {
	localStore, err := store.Open(t.TempDir(), "127.0.0.1:0", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	remoteStore, err := store.Open(t.TempDir(), "127.0.0.1:0", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	remote := New(remoteStore)
	certificate, err := remote.identityCertificate()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS13})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	remoteConfig := remoteStore.Config()
	if err := localStore.SavePairedDevice(model.PairedDevice{ID: remoteConfig.DeviceID, PublicKey: remoteConfig.IdentityPublicKey}); err != nil {
		t.Fatal(err)
	}
	if err := localStore.AddShare(model.Share{ID: "outgoing", Name: "test", Path: t.TempDir(), PeerDeviceID: remoteConfig.DeviceID, PeerAddress: listener.Addr().String(), State: "active"}); err != nil {
		t.Fatal(err)
	}
	local := New(localStore)
	if err := local.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer local.Stop()
	accepted := make(chan net.Conn, 1)
	go func() {
		connection, err := listener.Accept()
		if err == nil {
			accepted <- connection
		}
	}()
	result := make(chan error, 1)
	go func() { result <- local.SyncShare(context.Background(), "outgoing", true) }()
	var connection net.Conn
	select {
	case connection = <-accepted:
	case <-time.After(2 * time.Second):
		t.Fatal("sync did not connect")
	}
	defer connection.Close()
	connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := connection.Read(make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	stopped := make(chan struct{})
	go func() { local.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("outgoing sync prevented exit")
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("interrupted sync reported success")
		}
	default:
		t.Fatal("Stop returned before outgoing sync finished")
	}
}
