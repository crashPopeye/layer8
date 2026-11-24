// Command layer8 runs the layer8 daemon.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/crashPopeye/layer8/internal/client"
	"github.com/crashPopeye/layer8/internal/daemon"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "daemon" {
		fmt.Fprintln(os.Stderr, "usage: layer8 daemon    serve the API and UI (LAYER8_HOME, LAYER8_ADDR)")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := daemon.Run(ctx, client.Home(), client.Addr()); err != nil {
		log.Fatal(err)
	}
}
