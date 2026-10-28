// Package launch opens the user's own terminal and editor apps.
package launch

import (
	"errors"
	"os"
	"os/exec"
	"strings"

	"github.com/crashPopeye/layer8/internal/config"
	"github.com/crashPopeye/layer8/internal/tmux"
)

type Command struct {
	Name string
	Args []string
}

func appleScriptString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// fill splits the template on whitespace before substituting, so values containing
// spaces stay a single argument.
func fill(template string, vars map[string]string) (Command, error) {
	fields := strings.Fields(template)
	if len(fields) == 0 {
		return Command{}, errors.New("empty command template")
	}
	for i, f := range fields {
		for k, v := range vars {
			f = strings.ReplaceAll(f, "{"+k+"}", v)
		}
		fields[i] = f
	}
	return Command{Name: fields[0], Args: fields[1:]}, nil
}

func userShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/zsh"
}

// Terminal returns how to open the configured terminal app (macOS) in cwd running cmd.
// Each app is started through its own launcher so the user's profile and keybindings apply.
func Terminal(cfg config.Config, cwd, cmd string) (Command, error) {
	// Login + interactive so rc files load before cmd runs.
	viaShell := []string{userShell(), "-lic", cmd}
	line := "cd " + tmux.Quote(cwd) + " && " + cmd
	switch cfg.Terminal {
	case config.TerminalGhostty:
		return Command{"open", append([]string{"-na", "Ghostty", "--args", "--working-directory=" + cwd, "-e"}, viaShell...)}, nil
	case config.TerminalWezterm:
		return Command{"open", append([]string{"-na", "WezTerm", "--args", "start", "--cwd", cwd, "--"}, viaShell...)}, nil
	case config.TerminalKitty:
		return Command{"open", append([]string{"-na", "kitty", "--args", "--directory", cwd}, viaShell...)}, nil
	case config.TerminalIterm:
		script := strings.Join([]string{
			`tell application "iTerm"`,
			`  activate`,
			`  set w to (create window with default profile)`,
			`  tell current session of w to write text ` + appleScriptString(line),
			`end tell`,
		}, "\n")
		return Command{"osascript", []string{"-e", script}}, nil
	case config.TerminalCustom:
		if strings.TrimSpace(cfg.TerminalCommand) == "" {
			return Command{}, errors.New("terminal is set to custom but no command is configured")
		}
		return fill(cfg.TerminalCommand, map[string]string{"cwd": cwd, "cmd": cmd})
	default:
		script := "tell application \"Terminal\"\n  activate\n  do script " + appleScriptString(line) + "\nend tell"
		return Command{"osascript", []string{"-e", script}}, nil
	}
}

func Editor(cfg config.Config, path string) (Command, error) {
	switch cfg.Editor {
	case config.EditorCursor:
		return Command{"open", []string{"-na", "Cursor", "--args", path}}, nil
	case config.EditorZed:
		return Command{"open", []string{"-a", "Zed", path}}, nil
	case config.EditorIdea:
		return Command{"open", []string{"-na", "IntelliJ IDEA", "--args", path}}, nil
	case config.EditorCustom:
		if strings.TrimSpace(cfg.EditorCommand) == "" {
			return Command{}, errors.New("editor is set to custom but no command is configured")
		}
		return fill(cfg.EditorCommand, map[string]string{"path": path})
	default:
		return Command{"open", []string{"-na", "Visual Studio Code", "--args", "--new-window", path}}, nil
	}
}

// Start launches c detached; the child outlives layer8.
func Start(c Command, dir string) error {
	cmd := exec.Command(c.Name, c.Args...)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // reap; open/osascript exit as soon as the app is launched
	return nil
}
