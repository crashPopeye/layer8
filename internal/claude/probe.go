package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

// Command is a slash command or skill the agent accepts as "/name args".
type Command struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	ArgumentHint string `json:"argumentHint"`
}

type Model struct {
	Value       string   `json:"value"`
	DisplayName string   `json:"displayName"`
	Description string   `json:"description"`
	Efforts     []string `json:"supportedEffortLevels"`
}

// Capabilities are what a harness offers in a directory: its models and commands.
type Capabilities struct {
	Models   []Model   `json:"models"`
	Commands []Command `json:"commands"`
}

// Probe starts the CLI in cwd, reads the initialize response and stops it. No API call is made.
func Probe(ctx context.Context, binary, cwd string) (Capabilities, error) {
	if binary == "" {
		binary = "claude"
	}
	cmd := exec.CommandContext(ctx, binary, Args(Options{})...)
	cmd.Dir = cwd
	cmd.Env = append(CleanEnv(os.Environ()), "CLAUDE_CODE_ENTRYPOINT=sdk-ts")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return Capabilities{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Capabilities{}, err
	}
	if err := cmd.Start(); err != nil {
		return Capabilities{}, fmt.Errorf("claude CLI: %w", err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	if _, err := fmt.Fprintln(stdin, `{"request_id":"probe","type":"control_request","request":{"subtype":"initialize"}}`); err != nil {
		return Capabilities{}, err
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var line struct {
			Type     string `json:"type"`
			Response struct {
				RequestID string       `json:"request_id"`
				Error     string       `json:"error"`
				Response  Capabilities `json:"response"`
			} `json:"response"`
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil || line.Type != "control_response" || line.Response.RequestID != "probe" {
			continue
		}
		if line.Response.Error != "" {
			return Capabilities{}, fmt.Errorf("claude initialize: %s", line.Response.Error)
		}
		return line.Response.Response, nil
	}
	if err := ctx.Err(); err != nil {
		return Capabilities{}, err
	}
	return Capabilities{}, fmt.Errorf("claude exited before answering initialize")
}
