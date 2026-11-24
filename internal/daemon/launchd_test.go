package daemon

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func TestPlistGolden(t *testing.T) {
	a := Agent{Exe: "/Applications/layer8.app/Contents/MacOS/layer8", Home: "/Users/alex/.layer8", Addr: "127.0.0.1:7878"}
	golden := filepath.Join("testdata", "agent.plist")
	if *update {
		os.WriteFile(golden, []byte(a.Plist()), 0o644)
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got := a.Plist(); got != string(want) {
		t.Fatalf("plist differs from %s:\n%s", golden, got)
	}
}

func TestEnsureReplacesTheAgent(t *testing.T) {
	dir := t.TempDir()
	var calls, full []string
	running := "0.3.0"
	a := Agent{
		Exe:       "/Applications/layer8.app/Contents/MacOS/layer8",
		Home:      filepath.Join(dir, "home"),
		Addr:      "127.0.0.1:7878",
		PlistPath: filepath.Join(dir, "LaunchAgents", Label+".plist"),
		Launchctl: func(args ...string) ([]byte, error) {
			calls, full = append(calls, args[0]), append(full, strings.Join(args, " "))
			if args[0] == "kickstart" {
				running = "1.0.0"
			}
			if args[0] == "bootout" {
				return []byte("Boot-out failed: 3: No such process"), errors.New("exit status 3")
			}
			return nil, nil
		},
		Version: func() (string, error) { return running, nil },
	}
	if err := a.Ensure(context.Background(), "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"bootout", "bootstrap", "kickstart"}; !slices.Equal(calls, want) {
		t.Fatalf("launchctl calls = %v, want %v", calls, want)
	}
	plist, err := os.ReadFile(a.PlistPath)
	if err != nil || string(plist) != a.Plist() {
		t.Fatalf("plist not written: %v", err)
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	if want := []string{"bootstrap " + domain + " " + a.PlistPath, "kickstart " + domain + "/" + Label}; !slices.Equal(full[1:], want) {
		t.Fatalf("launchctl argv = %q, want %q", full[1:], want)
	}

	calls = nil
	if err := a.Ensure(context.Background(), "1.0.0"); err != nil || len(calls) != 0 {
		t.Fatalf("ensure with the right daemon up called %v, err %v", calls, err)
	}

	if err := a.Ensure(context.Background(), "dev"); err == nil || len(calls) != 0 {
		t.Fatalf("a dev build touched launchd: %v, err %v", calls, err)
	}
}
