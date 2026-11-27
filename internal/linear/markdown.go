package linear

import (
	"fmt"
	"strings"
)

// TicketMarkdown renders an issue as the 01-ticket.md artifact.
func TicketMarkdown(i Issue) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s %s\n\n", i.Identifier, i.Title)
	field := func(name, value string) {
		if value != "" {
			fmt.Fprintf(&b, "- **%s:** %s\n", name, value)
		}
	}
	field("Linear", fmt.Sprintf("[%s](%s)", i.Identifier, i.URL))
	field("Team", i.Team.Name)
	field("State", i.State.Name)
	if i.Assignee != nil {
		field("Assignee", i.Assignee.Name)
	}
	field("Labels", strings.Join(i.Labels, ", "))
	if i.Parent != nil {
		field("Parent", fmt.Sprintf("[%s](%s) %s", i.Parent.Identifier, i.Parent.URL, i.Parent.Title))
	}
	if i.BranchName != "" {
		field("Branch", "`"+i.BranchName+"`")
	}
	if d := strings.TrimSpace(i.Description); d != "" {
		fmt.Fprintf(&b, "\n## Description\n\n%s\n", d)
	}
	if len(i.Attachments) > 0 {
		b.WriteString("\n## Attachments\n\n")
		for _, a := range i.Attachments {
			fmt.Fprintf(&b, "- [%s](%s)\n", a.Title, a.URL)
		}
	}
	if len(i.Comments) > 0 {
		b.WriteString("\n## Comments\n")
		for _, c := range i.Comments {
			author := "Unknown"
			if c.User != nil {
				author = c.User.Name
			}
			fmt.Fprintf(&b, "\n**%s** · %s\n\n%s\n", author, c.CreatedAt.Format("2006-01-02 15:04"), strings.TrimSpace(c.Body))
		}
	}
	return b.String()
}
