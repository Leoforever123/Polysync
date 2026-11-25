package mobile

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"polysync/internal/model"
	"polysync/internal/rootidentity"
)

func TestWorkspaceVolumeLossReplacementAndReturn(t *testing.T) {
	for _, installation := range []string{"bound", "before-upgrade"} {
		t.Run(installation, func(t *testing.T) {
			e := testEngine(t)
			const id, original, other = "folder", "original document", "another disk"
			path := filepath.Join(e.dir, "workspaces", id)
			doc := filepath.Join(path, "notes")
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(doc, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(e.dir, "imports", "source"), []byte("to import"), 0600); err != nil {
				t.Fatal(err)
			}
			st := e.svc.Store()
			if err := st.AddShare(model.Share{ID: id, Path: path, PeerDeviceID: "peer"}); err != nil {
				t.Fatal(err)
			}
			var pendingExport string
			if installation == "before-upgrade" {
				digest := fmt.Sprintf("%x", sha256.Sum256([]byte(original)))
				if err := st.SaveBaseline(id, "peer", []model.Entry{{Path: "notes", Hash: digest}}); err != nil {
					t.Fatal(err)
				}
				e.Stop()
				cfg := st.Config()
				cfg.Shares[0].RootID = ""
				data, err := json.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(st.Dir(), "config.json"), data, 0600); err != nil {
					t.Fatal(err)
				}
				files, err := os.ReadDir(path)
				if err != nil {
					t.Fatal(err)
				}
				for _, f := range files {
					if rootidentity.Reserved(f.Name()) {
						if err := os.Remove(filepath.Join(path, f.Name())); err != nil {
							t.Fatal(err)
						}
					}
				}
			} else {
				var err error
				pendingExport, err = e.WorkspaceExportPath(id, "notes")
				if err != nil {
					t.Fatal(err)
				}
			}
			away := filepath.Join(filepath.Dir(path), "disconnected")
			if err := os.Rename(path, away); err != nil {
				t.Fatal(err)
			}
			if installation == "before-upgrade" {
				if err := e.Start(e.dir, "Phone", "127.0.0.1:0"); err != nil {
					t.Fatal(err)
				}
			}
			requirePaused := func() {
				t.Helper()
				if list, err := e.WorkspaceFiles(id); err == nil {
					t.Fatalf("listed offline workspace: %s", list)
				}
				if err := e.ImportToWorkspace(id, "source", "import.txt"); err == nil {
					t.Fatal("wrote into offline workspace")
				}
				if p, err := e.WorkspaceExportPath(id, "notes"); err == nil {
					t.Fatalf("exported from offline workspace: %s", p)
				}
			}
			requirePaused()
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("missing workspace recreated: %v", err)
			}
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(doc, []byte(other), 0600); err != nil {
				t.Fatal(err)
			}
			requirePaused()
			files, err := os.ReadDir(path)
			if err != nil || len(files) != 1 || files[0].Name() != "notes" {
				t.Fatalf("modified replacement: %v, %v", files, err)
			}
			if pendingExport != "" {
				// The Android document picker can complete after a volume swap.
				data, err := os.ReadFile(pendingExport)
				if err != nil || string(data) != original {
					t.Fatalf("picker redirected to replacement: %q, %v", data, err)
				}
			}
			if err := os.Rename(path, path+"-replacement"); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(away, path); err != nil {
				t.Fatal(err)
			}
			list, err := e.WorkspaceFiles(id)
			if err != nil || !strings.Contains(list, "\"notes\"") {
				t.Fatalf("restored listing %s, %v", list, err)
			}
			if err := e.ImportToWorkspace(id, "source", "import.txt"); err != nil {
				t.Fatal(err)
			}
			exported, err := e.WorkspaceExportPath(id, "notes")
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(exported)
			if err != nil || string(data) != original {
				t.Fatalf("restored export = %q, %v", data, err)
			}
			if pendingExport != "" && pendingExport != exported {
				t.Fatal("duplicate export object for identical bytes")
			}
			sh, _ := e.svc.Store().Share(id)
			if err := rootidentity.Check(path, sh.RootID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFailedWorkspaceInvitationOnlyRemovesItsUnusedMarker(t *testing.T) {
	for _, state := range []string{"empty", "user-file", "pending", "changed-marker", "replaced"} {
		t.Run(state, func(t *testing.T) {
			e := testEngine(t)
			st := e.svc.Store()
			peer, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer peer.Close()
			// Drop the handshake after the invite registered its provisional share.
			go func() {
				c, err := peer.Accept()
				if err == nil {
					c.Close()
				}
			}()
			if err := st.SavePairedDevice(model.PairedDevice{ID: "peer", Addresses: []string{peer.Addr().String()}}); err != nil {
				t.Fatal(err)
			}
			currentHandler := e.handler
			var workspace, marker, replacementID string
			e.handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				currentHandler.ServeHTTP(w, req)
				if len(st.Config().Shares) != 0 {
					t.Fatal("failed invitation still registered")
				}
				created, err := os.ReadDir(filepath.Join(e.dir, "workspaces"))
				if err != nil || len(created) != 1 {
					t.Fatalf("provisional directories %v, %v", created, err)
				}
				workspace = filepath.Join(e.dir, "workspaces", created[0].Name())
				files, err := os.ReadDir(workspace)
				if err != nil || len(files) != 1 || !rootidentity.Reserved(files[0].Name()) {
					t.Fatalf("provisional marker %v, %v", files, err)
				}
				marker = filepath.Join(workspace, files[0].Name())
				switch state {
				case "user-file":
					if err := os.WriteFile(filepath.Join(workspace, "notes"), []byte("keep"), 0600); err != nil {
						t.Fatal(err)
					}
				case "pending":
					if err := st.AddShare(model.Share{ID: "pending", Path: workspace, State: "pending"}); err != nil {
						t.Fatal(err)
					}
				case "replaced":
					if err := os.Rename(workspace, workspace+"-original"); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(workspace, 0700); err != nil {
						t.Fatal(err)
					}
					replacementID, err = rootidentity.Bind(workspace)
					if err != nil {
						t.Fatal(err)
					}
				case "changed-marker":
					f, err := os.OpenFile(marker, os.O_WRONLY, 0)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := f.WriteString("keep"); err != nil {
						t.Fatal(err)
					}
					if err := f.Close(); err != nil {
						t.Fatal(err)
					}
				}
			})
			if _, err := e.CreateWorkspace("Documents", "peer", ""); err == nil {
				t.Fatal("expected invitation failure")
			}
			if workspace == "" {
				t.Fatal("invitation didn't reach service")
			}
			if state == "empty" {
				if _, err := os.Lstat(workspace); !os.IsNotExist(err) {
					t.Fatalf("orphaned empty workspace: %v", err)
				}
				return
			}
			if state == "replaced" {
				if err := rootidentity.Check(workspace, replacementID); err != nil {
					t.Fatalf("replacement was touched: %v", err)
				}
				if _, err := os.Stat(filepath.Join(workspace+"-original", filepath.Base(marker))); err != nil {
					t.Fatalf("lost original marker: %v", err)
				}
				return
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("removed occupied workspace: %v", err)
			}
			switch state {
			case "user-file":
				data, err := os.ReadFile(filepath.Join(workspace, "notes"))
				if err != nil || string(data) != "keep" {
					t.Fatalf("lost user file %q, %v", data, err)
				}
			case "changed-marker":
				data, err := os.ReadFile(marker)
				if err != nil || string(data) != "keep" {
					t.Fatalf("removed changed marker %q, %v", data, err)
				}
			case "pending":
				sh, ok := st.Share("pending")
				if !ok || rootidentity.Check(sh.Path, sh.RootID) != nil {
					t.Fatal("pending workspace lost its binding")
				}
			}
		})
	}
}
