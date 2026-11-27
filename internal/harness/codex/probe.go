package codex

import (
	"cmp"
	"context"
	"path/filepath"
	"strings"

	"github.com/crashPopeye/layer8/internal/harness"
)

type skill struct {
	Name             string `json:"name"`
	Description      string `json:"description"`
	ShortDescription string `json:"shortDescription"`
	Path             string `json:"path"`
	Enabled          bool   `json:"enabled"`
	Interface        *struct {
		ShortDescription string `json:"shortDescription"`
	} `json:"interface"`
}

// listSkills adds the skills folder of each plugin dir to codex's skill roots and
// returns the enabled skills codex sees in cwd.
func (p *proc) listSkills(ctx context.Context, cwd string, pluginDirs []string) ([]skill, error) {
	if len(pluginDirs) > 0 {
		var roots []string
		for _, d := range pluginDirs {
			roots = append(roots, filepath.Join(d, "skills"))
		}
		if err := p.call(ctx, "skills/extraRoots/set", map[string]any{"extraRoots": roots}, nil); err != nil {
			return nil, err
		}
	}
	var r struct {
		Data []struct {
			Skills []skill `json:"skills"`
		} `json:"data"`
	}
	if err := p.call(ctx, "skills/list", map[string]any{"cwds": []string{cwd}}, &r); err != nil {
		return nil, err
	}
	var out []skill
	for _, e := range r.Data {
		for _, s := range e.Skills {
			if s.Enabled {
				out = append(out, s)
			}
		}
	}
	return out, nil
}

// Probe starts codex app-server in cwd and reads its skills, models and MCP servers
// without starting a thread.
func (h Harness) Probe(ctx context.Context, cwd string) (harness.Capabilities, error) {
	p, err := h.spawn(cwd, nil, discard{})
	if err != nil {
		return harness.Capabilities{}, err
	}
	defer p.Close(0)
	if err := p.initialize(ctx); err != nil {
		return harness.Capabilities{}, err
	}
	skills, err := p.listSkills(ctx, cwd, h.PluginDirs)
	if err != nil {
		return harness.Capabilities{}, err
	}
	type effort struct {
		ReasoningEffort string `json:"reasoningEffort"`
	}
	models, err := pages[struct {
		ID          string   `json:"id"`
		DisplayName string   `json:"displayName"`
		Description string   `json:"description"`
		Efforts     []effort `json:"supportedReasoningEfforts"`
	}](ctx, p, "model/list", map[string]any{})
	if err != nil {
		return harness.Capabilities{}, err
	}
	servers, _ := pages[struct {
		Name       string `json:"name"`
		AuthStatus string `json:"authStatus"`
	}](ctx, p, "mcpServerStatus/list", map[string]any{"detail": "toolsAndAuthOnly"})

	var c harness.Capabilities
	for _, s := range skills {
		plugin, _, ok := strings.Cut(s.Name, ":")
		if !ok {
			plugin = ""
		}
		desc := cmp.Or(s.ShortDescription, s.Description)
		if s.Interface != nil {
			desc = cmp.Or(s.Interface.ShortDescription, desc)
		}
		c.Commands = append(c.Commands, harness.Command{Name: s.Name, Description: desc, Plugin: plugin})
	}
	for _, m := range models {
		var efforts []string
		for _, e := range m.Efforts {
			efforts = append(efforts, e.ReasoningEffort)
		}
		c.Models = append(c.Models, harness.Model{Value: m.ID, DisplayName: m.DisplayName, Description: m.Description, Efforts: efforts})
	}
	for _, s := range servers {
		status := "connected"
		if s.AuthStatus == "notLoggedIn" {
			status = "needs-auth"
		}
		c.MCPServers = append(c.MCPServers, harness.MCPServer{Name: s.Name, Status: status})
	}
	return c, nil
}

type discard struct{}

func (discard) OnEvent(harness.Event) {}

func (discard) OnPermission(_ harness.PermissionRequest, respond func(harness.Decision)) {
	respond(harness.Decision{})
}

func (discard) OnExit(error) {}
