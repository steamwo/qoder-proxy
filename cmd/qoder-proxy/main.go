package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/steamwo/qoder-proxy/internal/credential"
	"github.com/steamwo/qoder-proxy/internal/desktop"
)

// The headless entry point intentionally reuses the same credential, settings,
// proxy manager, and Qoder backend as the desktop app while excluding Gio,
// window, tray, and rendering code. It is useful both for servers and for
// measuring the native UI memory overhead of the desktop build.
func main() {
	if len(os.Args) > 1 && os.Args[1] != "serve" {
		fmt.Fprintln(os.Stderr, "usage: qoder-proxy-headless [serve]")
		os.Exit(2)
	}

	store, err := credential.New(os.Getenv("QODER_PROXY_CREDENTIALS"))
	if err != nil {
		fatal(err)
	}
	cred, err := store.Load()
	if err != nil {
		fatal(err)
	}
	if cred.Expired() {
		fatal(fmt.Errorf("stored Qoder credential is expired; log in with the desktop app first"))
	}

	settings := desktop.LoadSettings()
	var proxy desktop.ProxyManager
	if err := proxy.Start(cred, settings); err != nil {
		fatal(err)
	}
	fmt.Printf("Qoder Proxy headless listening on %s\n", proxy.Addr())
	fmt.Println("Press Ctrl+C to stop.")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	signal.Stop(stop)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := proxy.Stop(ctx); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "qoder-proxy-headless:", err)
	os.Exit(1)
}
