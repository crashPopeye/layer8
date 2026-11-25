package linear

import (
	"fmt"
	"strings"
)

// TicketMarkdown renders an issue as the ticket.md agents read.
func TicketMarkdown(i Issue) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s: %s\n\n", i.Identifier, i.Title)
	field := func(label, value string) {
		if value != "" {
			fmt.Fprintf(&b, "- %s: %s\n", label, value)
		}
	}
	field("URL", i.URL)
	field("State", i.State)
	field("Team", i.Team)
	field("Assignee", i.Assignee)
	field("Project", i.Project)
	field("Parent", i.Parent)
	field("Labels", strings.Join(i.Labels, ", "))
	field("Branch", i.BranchName)

	desc := strings.TrimSpace(i.Description)
	if desc == "" {
		desc = "_No description._"
	}
	fmt.Fprintf(&b, "\n## Description\n\n%s\n", desc)

	if len(i.Children) > 0 {
		b.WriteString("\n## Sub-issues\n\n")
		for _, c := range i.Children {
			fmt.Fprintf(&b, "- %s: %s (%s), branch `%s`\n", c.Identifier, c.Title, c.State, c.BranchName)
		}
	}
	if len(i.Attachments) > 0 {
		b.WriteString("\n## Attachments\n\n")
		for _, l := range i.Attachments {
			fmt.Fprintf(&b, "- [%s](%s)\n", l.Title, l.URL)
		}
	}
	if len(i.Comments) > 0 {
		b.WriteString("\n## Comments\n")
		for _, c := range i.Comments {
			date, _, _ := strings.Cut(c.CreatedAt, "T")
			fmt.Fprintf(&b, "\n### %s (%s)\n\n%s\n", c.Author, date, strings.TrimSpace(c.Body))
		}
	}
	return b.String()
}
