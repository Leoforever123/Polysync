package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTransferHTTPStalledUploadCanCancel(t *testing.T) {
	a, b := transferPair(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	task, err := a.CreateTransfer(b.store.Config().DeviceID, []TransferFile{{Name: "stall.txt", Size: 10}})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_, err = fmt.Fprintf(connection, "PUT /api/transfers/%s/upload HTTP/1.1\r\nHost: %s\r\nContent-Type: multipart/form-data; boundary=test\r\nContent-Length: 1000\r\n\r\n--test\r\nContent-Disposition: form-data; name=\"file\"; filename=\"stall.txt\"\r\n\r\n", task.ID, strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	bound := false
	for time.Now().Before(deadline) {
		a.transfers.mu.Lock()
		bound = a.transfers.cancels[task.ID] != nil
		a.transfers.mu.Unlock()
		if bound {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !bound {
		t.Fatal("HTTP upload not bound")
	}
	client := http.Client{Timeout: 2 * time.Second}
	response, err := client.Post(server.URL+"/api/transfers/"+task.ID+"/cancel", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.Status)
	}
	result := waitTransfer(t, a, task.ID)
	if result.State != "cancelled" {
		t.Fatal(result)
	}
}
func TestCancelledUploadCannotBecomeBackgroundSend(t *testing.T) {
	a, b := transferPair(t)
	task, err := a.CreateTransfer(b.store.Config().DeviceID, []TransferFile{{Name: "x.txt", Size: 0}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.startSendTransfer(task.ID, ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	result, _ := a.transfers.Task(task.ID)
	if result.State != "preparing" {
		t.Fatal("cancelled upload resurrected", result)
	}
	if len(b.transfers.List()) != 0 {
		t.Fatal("receiver contacted")
	}
}
func TestTransferAPIBlocksCrossOriginWrites(t *testing.T) {
	a, _ := transferPair(t)
	request := httptest.NewRequest("PUT", "http://localhost/api/transfer-settings", strings.NewReader("{}"))
	request.Header.Set("Origin", "https://untrusted.example")
	response := httptest.NewRecorder()
	a.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("unexpected status %d", response.Code)
	}
}
