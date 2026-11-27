package claude

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/crashPopeye/layer8/internal/harness"
)

// Probe starts claude in cwd, reads its commands, skills, models, agents and MCP
// servers through the control protocol, and kills it without calling a model.
func (h Harness) Probe(ctx context.Context, cwd string) (harness.Capabilities, error) {
	p, err := h.spawn(cwd, h.args(harness.StartOptions{}), nil, discard{})
	if err != nil {
		return harness.Capabilities{}, err
	}
	defer p.Close(0)
	body, err := p.request(ctx, "initialize", map[string]any{"hooks": nil})
	if err != nil {
		return harness.Capabilities{}, err
	}
	var init struct {
		Commands []struct {
			Name         string `json:"name"`
			Description  string `json:"description"`
			ArgumentHint string `json:"argumentHint"`
			Builtin      bool   `json:"builtin"`
		} `json:"commands"`
		Models []struct {
			Value       string   `json:"value"`
			DisplayName string   `json:"displayName"`
			Description string   `json:"description"`
			Efforts     []string `json:"supportedEffortLevels"`
		} `json:"models"`
		Agents []harness.Agent `json:"agents"`
	}
	if err := json.Unmarshal(body, &init); err != nil {
		return harness.Capabilities{}, err
	}
	var c harness.Capabilities
	for _, cmd := range init.Commands {
		plugin, _, _ := strings.Cut(cmd.Name, ":")
		if plugin == cmd.Name {
			plugin = ""
		}
		c.Commands = append(c.Commands, harness.Command{Name: cmd.Name, Description: cmd.Description,
			ArgumentHint: cmd.ArgumentHint, Plugin: plugin, Builtin: cmd.Builtin})
	}
	for _, m := range init.Models {
		c.Models = append(c.Models, harness.Model(m))
	}
	c.Agents = init.Agents
	c.MCPServers = p.mcpServers(ctx)
	return c, nil
}

// mcpServers polls mcp_status until two answers list the same servers with none
// pending, for at most mcpSettle.
func (p *proc) mcpServers(ctx context.Context) []harness.MCPServer {
	deadline := time.After(mcpSettle)
	var last []harness.MCPServer
	for i := 0; ; i++ {
		body, err := p.request(ctx, "mcp_status", nil)
		if err != nil {
			return last
		}
		var st struct {
			MCPServers []harness.MCPServer `json:"mcpServers"`
		}
		json.Unmarshal(body, &st)
		pending := slices.ContainsFunc(st.MCPServers, func(s harness.MCPServer) bool { return s.Status == "pending" })
		if i > 0 && len(last) == len(st.MCPServers) && !pending {
			return st.MCPServers
		}
		last = st.MCPServers
		select {
		case <-deadline:
			return last
		case <-ctx.Done():
			return last
		case <-time.After(500 * time.Millisecond):
		}
	}
}

const mcpSettle = 5 * time.Second

type discard struct{}

func (discard) OnEvent(harness.Event) {}

func (discard) OnPermission(_ harness.PermissionRequest, respond func(harness.Decision)) {
	respond(harness.Decision{})
}

func (discard) OnExit(error) {}
