package tree

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Render formats a node as readable text for the CLI and MCP.
func Render(n Node) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n", n.Addr, n.Title)
	keys := make([]string, 0, len(n.Fields))
	for k := range n.Fields {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "  %s: %s\n", k, value(n.Fields[k]))
	}
	if len(n.Children) > 0 {
		b.WriteString("children:\n")
		renderSummaries(&b, n.Children, "  ")
	}
	if len(n.Actions) > 0 {
		b.WriteString("actions:\n")
		for _, a := range n.Actions {
			fmt.Fprintf(&b, "  %s\n", strings.Join(nonEmpty(a.Name, signature(a.Args), "— "+a.Help), " "))
		}
	}
	return b.String()
}

func renderSummaries(b *strings.Builder, list []Summary, indent string) {
	for _, s := range list {
		status := ""
		if s.Status != "" {
			status = "[" + s.Status + "]"
		}
		fmt.Fprintf(b, "%s%s\n", indent, strings.Join(nonEmpty(string(s.Addr), s.Title, status), "  "))
		renderSummaries(b, s.Children, indent+"  ")
	}
}

func value(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	data, _ := json.Marshal(v)
	return string(data)
}

func nonEmpty(parts ...string) []string {
	return slices.DeleteFunc(parts, func(s string) bool { return s == "" })
}

// RenderResults formats do results one per line as "ok|error <addr>: <message>".
func RenderResults(results []Result) string {
	var b strings.Builder
	for _, r := range results {
		status := "ok"
		if !r.OK {
			status = "error"
		}
		fmt.Fprintf(&b, "%s %s: %s\n", status, r.Addr, r.Message)
	}
	return b.String()
}
