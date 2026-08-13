package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/steamwo/qoder-proxy/internal/desktop"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	openBrowser := true
	controlListen := ""
	for _, arg := range os.Args[1:] {
		switch {
		case arg == "--no-browser":
			openBrowser = false
		case len(arg) > len("--control=") && arg[:len("--control=")] == "--control=":
			controlListen = arg[len("--control="):]
		case arg == "--help" || arg == "-h":
			fmt.Println("usage: qoder-proxy-service [--no-browser] [--control=127.0.0.1:39091]")
			return
		default:
			fmt.Fprintln(os.Stderr, "unknown option:", arg)
			os.Exit(2)
		}
	}

	if err := desktop.RunBackgroundService(ctx, desktop.ServiceOptions{
		ControlListen: controlListen,
		OpenBrowser:   openBrowser,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "qoder-proxy-service:", err)
		os.Exit(1)
	}
}
