package codex

import (
	"context"

	"github.com/crashPopeye/layer8/internal/claude"
)

// Probe starts the app-server in cwd, lists its models and skills and stops it. No model call is made.
func Probe(ctx context.Context, binary, cwd string) (claude.Capabilities, error) {
	p, err := spawn(ctx, claude.Options{Binary: binary, Cwd: cwd}, claude.Handlers{})
	if err != nil {
		return claude.Capabilities{}, err
	}
	defer func() { p.Kill(); <-p.done }()

	var caps claude.Capabilities
	cursor := ""
	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var page struct {
			Data []struct {
				Model       string `json:"model"`
				DisplayName string `json:"displayName"`
				Description string `json:"description"`
				Efforts     []struct {
					ReasoningEffort string `json:"reasoningEffort"`
				} `json:"supportedReasoningEfforts"`
			} `json:"data"`
			NextCursor string `json:"nextCursor"`
		}
		if err := p.call(ctx, "model/list", params, &page); err != nil {
			return claude.Capabilities{}, err
		}
		for _, m := range page.Data {
			model := claude.Model{Value: m.Model, DisplayName: m.DisplayName, Description: m.Description}
			for _, e := range m.Efforts {
				model.Efforts = append(model.Efforts, e.ReasoningEffort)
			}
			caps.Models = append(caps.Models, model)
		}
		if cursor = page.NextCursor; cursor == "" {
			break
		}
	}

	var skills struct {
		Data []struct {
			Skills []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Enabled     bool   `json:"enabled"`
			} `json:"skills"`
		} `json:"data"`
	}
	if err := p.call(ctx, "skills/list", map[string]any{"cwds": []string{cwd}}, &skills); err != nil {
		return claude.Capabilities{}, err
	}
	for _, entry := range skills.Data {
		for _, s := range entry.Skills {
			if s.Enabled {
				caps.Commands = append(caps.Commands, claude.Command{Name: "$" + s.Name, Description: s.Description})
			}
		}
	}
	return caps, nil
}
