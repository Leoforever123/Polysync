package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sort"
	"syscall"
	"time"

	"polysync/internal/service"
	"polysync/internal/store"
	"polysync/internal/tray"
)

var version = "dev"

func main() {
	dataDir := flag.String("data-dir", "", "configuration and state directory")
	listenAddr := flag.String("listen", ":45123", "TCP sync listen address")
	uiAddr := flag.String("ui", "127.0.0.1:45124", "local web console address")
	openUI := flag.Bool("open", true, "open the web console in the default browser")
	useTray := flag.Bool("tray", true, "show Windows system tray icon")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("PolySync", version)
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, cancel, *dataDir, *listenAddr, *uiAddr, *openUI, *useTray); err != nil {
		log.Print(err)
		if *useTray {
			tray.ShowError("PolySync 无法启动。请检查是否已有实例运行或端口被占用。\n\n" + err.Error())
		}
		os.Exit(1)
	}
}

func run(ctx context.Context, cancel context.CancelFunc, dataDir, listenAddr, uiAddr string, openUI, useTray bool) error {
	// Bind the console first: startup failures never leave an invisible sync service.
	listener, err := net.Listen("tcp", uiAddr)
	if err != nil {
		return fmt.Errorf("控制台监听失败: %w", err)
	}
	defer listener.Close()
	dataStore, err := store.Open(dataDir, listenAddr, uiAddr)
	if err != nil {
		return err
	}
	syncService := service.New(dataStore)
	notifications := make(chan tray.Notification, 16)
	syncService.SetTransferNotifier(func(text, id string) {
		select {
		case notifications <- tray.Notification{Text: text, Route: "#transfer/task/" + id}:
		default:
		}
	})
	syncService.SetTrayEnabled(useTray && runtime.GOOS == "windows")
	if err := syncService.Start(ctx); err != nil {
		syncService.Stop()
		return err
	}
	defer syncService.Stop()
	server := &http.Server{Handler: syncService.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	serverErrors := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		serverErrors <- err
		cancel()
	}()
	address := "http://" + listener.Addr().String()
	open := func() {
		if err := openBrowser(address); err != nil {
			log.Printf("打开控制台失败: %v", err)
		}
	}
	log.Printf("PolySync %s 已启动：控制台 %s，TCP 端口 %d", version, address, syncService.ListenPort())
	if openUI {
		go open()
	}
	var trayErr error
	if useTray {
		trayErr = tray.Run(ctx, tray.Options{
			OpenUI: open,
			OpenRoute: func(route string) {
				if err := openBrowser(address + route); err != nil {
					log.Printf("打开页面失败: %v", err)
				}
			},
			Notifications:  notifications,
			SaveFile:       syncService.SaveTransferFile,
			OpenReceived:   syncService.OpenTransferFile,
			CancelTransfer: syncService.CancelTransfer,
			Transfers:      func() tray.TransferSnapshot { return transferTraySnapshot(syncService.Transfers()) },
			OpenFolder:     openFolder,
			Quit:           cancel,
			Sync:           func(ctx context.Context, id string) error { return syncService.SyncShare(ctx, id, true) },
			Folders: func() []tray.Folder {
				statuses := syncService.Statuses()
				var folders []tray.Folder
				for _, share := range dataStore.Config().Shares {
					status := statuses[share.ID]
					lastSync := status.LastSync
					if lastSync.IsZero() {
						lastSync = dataStore.LastSyncTime(share.ID, share.PeerDeviceID)
					}
					folders = append(folders, tray.Folder{ID: share.ID, Name: share.Name, Path: share.Path, State: status.State, Message: status.Message, LastSync: lastSync, Active: share.State == "active" && share.PeerDeviceID != ""})
				}
				return folders
			},
		})
	} else {
		<-ctx.Done()
	}
	cancel()
	log.Print("正在停止 PolySync…")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
	}
	if trayErr != nil {
		return trayErr
	}
	select {
	case err := <-serverErrors:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
	default:
	}
	return nil
}

func openBrowser(address string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", address).Run()
	case "darwin":
		return exec.Command("open", address).Run()
	default:
		return exec.Command("xdg-open", address).Run()
	}
}
func openFolder(path string) error {
	if info, err := os.Stat(path); err != nil {
		return err
	} else if !info.IsDir() {
		return fmt.Errorf("文件夹不存在: %s", path)
	}
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer.exe", path).Start()
	case "darwin":
		return exec.Command("open", path).Run()
	default:
		return exec.Command("xdg-open", path).Run()
	}
}

func transferTraySnapshot(tasks []service.TransferTask) tray.TransferSnapshot {
	result := tray.TransferSnapshot{}
	for _, task := range tasks {
		if task.Active() {
			result.Active = append(result.Active, tray.TransferSummary{ID: task.ID, Label: task.PeerName + " · " + task.Message})
		}
		if task.Direction != "receive" {
			continue
		}
		for index, file := range task.Files {
			if file.State == "complete" || file.State == "missing" {
				result.Recent = append(result.Recent, tray.ReceivedFile{TaskID: task.ID, Index: index, Name: file.Name, PeerName: task.PeerName, Missing: file.State == "missing", Received: task.Updated})
			}
		}
	}
	sort.SliceStable(result.Recent, func(i, j int) bool { return result.Recent[i].Received.After(result.Recent[j].Received) })
	if len(result.Recent) > 5 {
		result.Recent = result.Recent[:5]
	}
	return result

}
