// layer8d is layer8's background service and command-line interface.
//
//	layer8d daemon          run the service (launchd does this)
//	layer8d ensure          start or upgrade the service; print how to reach it (JSON)
//	layer8 <command>        the CLI (layer8 is a symlink to this binary)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/crashPopeye/layer8/internal/daemon"
	"github.com/crashPopeye/layer8/ui"
)

// version is set at build time: -ldflags "-X main.version=0.2.0".
var version = "dev"

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	var err error
	switch args[0] {
	case "daemon":
		err = daemon.Serve(version, ui.FS)
	case "ensure":
		var info daemon.Info
		info, err = daemon.Ensure(context.Background(), version)
		if err == nil {
			err = json.NewEncoder(os.Stdout).Encode(info)
		}
	case "uninstall-agent":
		err = daemon.UninstallAgent()
	case "version", "--version", "-v":
		fmt.Println(version)
	case "help", "--help", "-h":
		usage()
	default:
		err = runCLI(args)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "layer8:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, strings.TrimLeft(`
layer8: parallel agent workbench

Projects
  layer8 projects                       list projects and their folders
  layer8 project add NAME [PATH...]     a project groups one or more folders
  layer8 folder add PROJECT PATH        any folder; git repos get worktrees

Tasks
  layer8 status                         agents running, approvals waiting
  layer8 tasks                          tasks with their PRs and sessions
  layer8 new ENG-123 [-p project] [-f folder]     start a task from a Linear ticket
  layer8 new -t "title" [-p project] [-f folder]  start a task without a ticket
  layer8 split TASK                     create PR slices from the task's plan.md
  layer8 sync [TASK]                    rebase onto the latest default branch

Agents
  layer8 run TASK research|plan|implement [-s PR] [extra instructions]
  layer8 chat TASK [-s PR] message      free-form session
  layer8 say SESSION message            follow-up message
  layer8 stop SESSION
  layer8 log SESSION [-f]               transcript (-f: follow)
  layer8 approvals
  layer8 approve ID | layer8 deny ID [reason]

Pull requests
  layer8 pr TASK -s PR                  push and open the PR
  layer8 fix-ci TASK -s PR | layer8 review TASK -s PR

Tools
  layer8 open TASK [-s PR] terminal|editor
  layer8 install-cli                    put "layer8" on your PATH
  layer8 version

TASK is a Linear identifier (ENG-123), a task slug, or an id prefix.
SESSION is a session id prefix (see layer8 tasks).
`, "\n"))
}
