// Command layer8d is the layer8 daemon and, under any name, the layer8 CLI.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/crashPopeye/layer8/internal/config"
	"github.com/crashPopeye/layer8/internal/daemon"
)

var version = "dev"

const usage = `usage:
  layer8d serve                              run the daemon
  layer8d ensure                             install or refresh the launchd agent, print {"url","token"}
  layer8 get [addr] [--json] [--depth 0|1|2] read a node (default app)
  layer8 do <addr> <action> [k=v|k:=json]... apply an action
  layer8 do --json '[{"target","action","args"}…]'`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "layer8:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	switch args[0] {
	case "serve":
		return daemon.Serve(cfg, version)
	case "ensure":
		token, err := daemon.Ensure(cfg, version)
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(map[string]string{"url": cfg.URL(), "token": token})
	case "get", "do":
		token, err := cfg.Token()
		if err != nil {
			return err
		}
		c := client{url: cfg.URL(), token: token}
		if args[0] == "get" {
			return c.get(args[1:], stdout)
		}
		return c.do(args[1:], stdout)
	}
	return errors.New(usage)
}
