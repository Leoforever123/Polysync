package main

import (
	"polysync/internal/service"
	"testing"
	"time"
)

func TestTransferTrayUsesCompletionOrderAndLimitsRecentFiles(t *testing.T) {
	now := time.Now()
	older := service.TransferTask{ID: "older", Direction: "receive", State: "complete", Updated: now.Add(-time.Hour), Files: []service.TransferFile{{Name: "old", State: "complete"}}}
	newer := service.TransferTask{ID: "newer", Direction: "receive", State: "complete", Updated: now, Files: []service.TransferFile{{Name: "missing", State: "missing"}, {Name: "ok", State: "complete"}, {Name: "bad", State: "interrupted"}, {Name: "3", State: "complete"}, {Name: "4", State: "complete"}, {Name: "5", State: "complete"}, {Name: "6", State: "complete"}}}
	active := service.TransferTask{ID: "active", Direction: "send", State: "sending", PeerName: "peer"}
	result := transferTraySnapshot([]service.TransferTask{older, active, newer})
	if len(result.Recent) != 5 || result.Recent[0].TaskID != "newer" || !result.Recent[0].Missing || result.Recent[1].Index != 1 {
		t.Fatal(result)
	}
	if len(result.Active) != 1 || result.Active[0].ID != "active" {
		t.Fatal(result.Active)
	}
	for _, file := range result.Recent {
		if file.Name == "bad" {
			t.Fatal("incomplete file exposed")
		}
	}
}
