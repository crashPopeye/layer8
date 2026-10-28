package launch

import (
	"reflect"
	"strings"
	"testing"

	"github.com/crashPopeye/layer8/internal/config"
)

func TestTerminal(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	c, err := Terminal(config.Config{Terminal: config.TerminalGhostty}, "/w", "tmux attach")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-na", "Ghostty", "--args", "--working-directory=/w", "-e", "/bin/zsh", "-lic", "tmux attach"}
	if c.Name != "open" || !reflect.DeepEqual(c.Args, want) {
		t.Fatalf("ghostty: %v %v", c.Name, c.Args)
	}

	c, _ = Terminal(config.Config{Terminal: config.TerminalIterm}, "/my dir", `echo "hi"`)
	if c.Name != "osascript" || !strings.Contains(c.Args[1], `write text "cd '/my dir' && echo \"hi\""`) {
		t.Fatalf("iterm: %v", c.Args)
	}

	c, _ = Terminal(config.Config{Terminal: config.TerminalApple}, "/w", "ls")
	if c.Name != "osascript" || !strings.Contains(c.Args[1], `do script "cd /w && ls"`) {
		t.Fatalf("terminal: %v", c.Args)
	}

	c, err = Terminal(config.Config{Terminal: config.TerminalCustom, TerminalCommand: "alacritty --working-directory {cwd} -e sh -c {cmd}"}, "/a b", "tmux attach")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "alacritty" || !reflect.DeepEqual(c.Args, []string{"--working-directory", "/a b", "-e", "sh", "-c", "tmux attach"}) {
		t.Fatalf("custom: %v", c.Args)
	}
	if _, err := Terminal(config.Config{Terminal: config.TerminalCustom}, "/", "x"); err == nil {
		t.Fatal("custom without command should fail")
	}
}

func TestEditor(t *testing.T) {
	c, _ := Editor(config.Config{Editor: config.EditorZed}, "/w")
	if c.Name != "open" || !reflect.DeepEqual(c.Args, []string{"-a", "Zed", "/w"}) {
		t.Fatalf("zed: %v", c.Args)
	}
}
