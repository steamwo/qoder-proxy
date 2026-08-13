package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/steamwo/qoder-proxy/internal/desktop"
)

func main() {
	openBrowser := true
	shutdown := false
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--no-browser":
			openBrowser = false
		case "--shutdown":
			shutdown = true
		case "--help", "-h":
			fmt.Println("usage: qoder-proxy-service [--no-browser] [--shutdown]")
			return
		default:
			fmt.Fprintln(os.Stderr, "unknown option:", arg)
			os.Exit(2)
		}
	}

	if shutdown {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := desktop.ShutdownUnifiedService(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "qoder-proxy-service shutdown:", err)
			os.Exit(1)
		}
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := desktop.RunUnifiedService(ctx, desktop.UnifiedServiceOptions{OpenBrowser: openBrowser}); err != nil {
		fmt.Fprintln(os.Stderr, "qoder-proxy-service:", err)
		os.Exit(1)
	}
}
