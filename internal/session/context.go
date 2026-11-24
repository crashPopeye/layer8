package session

import (
	"strings"
	"text/template"

	"github.com/crashPopeye/layer8/internal/store"
)

// ContextData is what a session's context block describes.
type ContextData struct {
	Session      store.Session
	Task         store.Task
	Parent       *store.Task
	Project      store.Project
	ArtifactsDir string
}

var contextTemplate = template.Must(template.New("context").Parse(`<layer8_session>
Session ID: {{.Session.ID}} ({{.Session.Label}})
Task: {{.Task.Title}} · {{.Task.ID}} · workflow {{.Task.Workflow}}{{with .Task.Phase}} · phase {{.}}{{end}}
Branch: {{.Task.Branch}} · worktree {{.Task.Worktree}}
{{with .Parent}}Parent task: {{.Title}} · {{.ID}}; its artifacts are at .layer8/task/parent/ (read-only)
{{end}}Autonomous: {{if .Task.Autonomous}}yes - answer your own questions with your recommendation and record the choice{{else}}no{{end}}
Drive the app with the l8 CLI (see the l8:orchestrate skill). "current" means this task/session.
</layer8_session>
<artifacts_directory_information>
Artifacts directory: .layer8/task/ (symlink to {{.ArtifactsDir}}). Every session of this task shares it. List it with ` + "`ls -La`" + `. Edit files there only with Write/Edit.
Inline previews: ` + "```task-artifact <file>```" + `.
</artifacts_directory_information>
{{with .Project.Instructions}}<project_instructions>
{{.}}
</project_instructions>
{{end}}`))

// ContextBlock renders the text appended to a session's system prompt.
func ContextBlock(d ContextData) string {
	var b strings.Builder
	if err := contextTemplate.Execute(&b, d); err != nil {
		panic(err)
	}
	return b.String()
}
