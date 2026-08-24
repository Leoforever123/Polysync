package syncer

import (
	"testing"
	"time"

	"polysync/internal/model"
)

func entry(path, hash string) model.Entry {
	return model.Entry{Path: path, Hash: hash, Size: int64(len(hash)), Mode: 0o644}
}

func TestBuildPlanInitialMerge(t *testing.T) {
	plan := BuildPlan(
		[]model.Entry{entry("server.txt", "server")},
		[]model.Entry{entry("client.txt", "client")},
		nil, "laptop", time.Unix(0, 0),
	)
	if len(plan.ClientSends) != 1 || plan.ClientSends[0].Source != "client.txt" {
		t.Fatalf("unexpected client transfers: %#v", plan.ClientSends)
	}
	if len(plan.ServerSends) != 1 || plan.ServerSends[0].Source != "server.txt" {
		t.Fatalf("unexpected server transfers: %#v", plan.ServerSends)
	}
}

func TestBuildPlanTracksChangesAndDeletion(t *testing.T) {
	baseline := []model.Entry{entry("edited.txt", "old"), entry("deleted.txt", "old")}
	server := []model.Entry{entry("edited.txt", "old"), entry("deleted.txt", "old")}
	client := []model.Entry{entry("edited.txt", "new")}
	plan := BuildPlan(server, client, baseline, "laptop", time.Unix(0, 0))
	if len(plan.ClientSends) != 1 || plan.ClientSends[0].Source != "edited.txt" {
		t.Fatalf("client edit was not propagated: %#v", plan)
	}
	if len(plan.ServerDeletes) != 1 || plan.ServerDeletes[0] != "deleted.txt" {
		t.Fatalf("client deletion was not propagated: %#v", plan)
	}
}

func TestBuildPlanPreservesConflict(t *testing.T) {
	baseline := []model.Entry{entry("notes.txt", "old")}
	server := []model.Entry{entry("notes.txt", "server-new")}
	client := []model.Entry{entry("notes.txt", "client-new")}
	plan := BuildPlan(server, client, baseline, "My Laptop", time.Date(2026, 8, 23, 10, 11, 12, 0, time.UTC))
	if len(plan.Conflicts) != 1 || plan.Conflicts[0] != "notes.txt" {
		t.Fatalf("conflict not recorded: %#v", plan)
	}
	if len(plan.ClientSends) != 1 || plan.ClientSends[0].Dest != "notes.polysync-conflict-MyLaptop-20260823T101112Z-client-n.txt" {
		t.Fatalf("conflict copy name is wrong: %#v", plan.ClientSends)
	}
	if len(plan.ServerSends) != 2 {
		t.Fatalf("both conflict versions must be returned: %#v", plan.ServerSends)
	}
}
