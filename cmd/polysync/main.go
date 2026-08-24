package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"polysync/internal/service"
	"polysync/internal/store"
)

var version = "dev"

func main() {
	dataDir := flag.String("data-dir", "", "configuration and state directory")
	listenAddr := flag.String("listen", ":45123", "TCP sync listen address")
	uiAddr := flag.String("ui", "127.0.0.1:45124", "local web console address")
	openUI := flag.Bool("open", true, "open the web console in the default browser")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("PolySync", version)
		return
	}

	dataStore, err := store.Open(*dataDir, *listenAddr, *uiAddr)
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	syncService := service.New(dataStore)
	if err := syncService.Start(ctx); err != nil {
		log.Fatal(err)
	}
	server := &http.Server{
		Addr: *uiAddr, Handler: syncService.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("PolySync %s 已启动：控制台 http://%s，TCP 端口 %d", version, *uiAddr, syncService.ListenPort())
		serverErrors <- server.ListenAndServe()
	}()
	if *openUI {
		go func() {
			time.Sleep(350 * time.Millisecond)
			_ = openBrowser("http://" + *uiAddr)
		}()
	}

	select {
	case <-ctx.Done():
		log.Print("正在停止 PolySync…")
	case err := <-serverErrors:
		if err != nil && err != http.ErrServerClosed {
			log.Printf("控制台服务错误: %v", err)
		}
		cancel()
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
	syncService.Stop()
}

func openBrowser(address string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", address).Start()
	case "darwin":
		return exec.Command("open", address).Start()
	default:
		return exec.Command("xdg-open", address).Start()
	}
}
