package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestRunStopsConsoleAndReleasesPorts(t *testing.T) {
	free := func() string {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		address := l.Addr().String()
		l.Close()
		return address
	}
	ui, syncAddr := free(), free()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	dir := t.TempDir()
	go func() { done <- run(ctx, cancel, dir, syncAddr, ui, false, false) }()
	client := http.Client{Timeout: time.Second}
	ready := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get("http://" + ui + "/api/status")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		t.Fatal("console did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("app failed to stop")
	}
	for _, addr := range []string{ui, syncAddr} {
		l, err := net.Listen("tcp", addr)
		if err != nil {
			t.Fatalf("port not released: %v", err)
		}
		l.Close()
	}
}
