// Command layer8 opens the layer8 window, or runs or ensures its daemon.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/crashPopeye/layer8/internal/api"
	"github.com/crashPopeye/layer8/internal/client"
	"github.com/crashPopeye/layer8/internal/daemon"
	"github.com/crashPopeye/layer8/internal/window"
)

const usage = `usage:
  layer8                 open the window (installs or refreshes the daemon's LaunchAgent first)
  layer8 daemon          serve the API and UI in the foreground
  layer8 daemon ensure   install or refresh the LaunchAgent and wait for the daemon

LAYER8_HOME and LAYER8_ADDR override ~/.layer8 and 127.0.0.1:7878.`

func main() {
	home, addr := client.Home(), client.Addr()
	switch strings.Join(os.Args[1:], " ") {
	case "":
		if err := window.Run(home, addr, api.Version); err != nil {
			log.Fatal(err)
		}
	case "daemon":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := daemon.Run(ctx, home, addr); err != nil {
			log.Fatal(err)
		}
	case "daemon ensure":
		agent, err := daemon.NewAgent(home, addr)
		if err == nil {
			err = agent.Ensure(context.Background(), api.Version)
		}
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("layer8 daemon %s on http://%s\n", api.Version, addr)
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
}
