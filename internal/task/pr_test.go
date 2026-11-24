package task

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crashPopeye/layer8/internal/gh"
)

// gitIdentity lets commits made through gitx run without the user's git config.
func gitIdentity(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(k, "t")
	}
	for _, k := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(k, "t@t")
	}
}

// fakeGH answers gh subcommands ("pr create", "pr view", "api graphql") with canned
// output and records every argv and stdin.
type fakeGH struct {
	out   map[string]string
	calls [][]string
	stdin []string
}

func (f *fakeGH) cli() gh.CLI {
	return gh.CLI{Run: func(ctx context.Context, dir, stdin string, args ...string) ([]byte, error) {
		f.calls, f.stdin = append(f.calls, args), append(f.stdin, stdin)
		out, ok := f.out[args[0]+" "+args[1]]
		if !ok {
			return nil, gh.ErrNoPR
		}
		return []byte(out), nil
	}}
}

func TestPRTarget(t *testing.T) {
	ctx := context.Background()
	svc := service(t)
	p, err := svc.AddProject(ctx, repo(t))
	if err != nil {
		t.Fatal(err)
	}
	parent, err := svc.Create(ctx, CreateInput{ProjectID: p.ID, Title: "Parent"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := svc.Create(ctx, CreateInput{ParentID: parent.ID, Title: "Child"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		number int
		state  string
		want   string
	}{
		{"parent without PR", 0, "", "main"},
		{"parent PR open", 7, "OPEN", "l8/parent"},
		{"parent PR merged", 7, "MERGED", "main"},
		{"parent PR closed", 7, "CLOSED", "main"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := svc.store.SetTaskPR(parent.ID, c.number, "", c.state); err != nil {
				t.Fatal(err)
			}
			ct, _ := svc.store.Task(child.ID)
			if got, err := svc.prTarget(ct); err != nil || got != c.want {
				t.Fatalf("target = %q, %v", got, err)
			}
		})
	}
	top, _ := svc.store.Task(parent.ID)
	if got, _ := svc.prTarget(top); got != "main" {
		t.Fatalf("top-level target = %q", got)
	}
}

func TestCommitPushAndOpenPR(t *testing.T) {
	gitIdentity(t)
	ctx := context.Background()
	svc := service(t)
	fake := &fakeGH{out: map[string]string{"pr create": "https://github.com/o/r/pull/142\n"}}
	svc.gh = fake.cli()
	clone := repo(t)
	p, err := svc.AddProject(ctx, clone)
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.Create(ctx, CreateInput{ProjectID: p.ID, Title: "Add lint", Prompt: "Add a make lint target."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Commit(ctx, task.ID, "nothing yet"); err == nil {
		t.Fatal("committed a clean worktree")
	}
	if err := os.WriteFile(filepath.Join(task.Worktree, "lint.mk"), []byte("lint:\n\tgo vet ./...\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changes, err := svc.Changes(ctx, task.ID)
	if err != nil || len(changes.Files) != 1 || !changes.Files[0].Untracked {
		t.Fatalf("changes = %+v, %v", changes, err)
	}
	first, err := svc.Commit(ctx, task.ID, "Add lint target")
	if err != nil {
		t.Fatal(err)
	}
	if got := git(t, task.Worktree, "status", "--porcelain"); got != "" {
		t.Fatalf("worktree not clean after commit: %q", got)
	}

	pr, err := svc.OpenPR(ctx, task.ID, PRInput{Title: "Add lint", Body: "## Why the change\n\nLint.", Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	if pr.Number != 142 {
		t.Fatalf("pr = %+v", pr)
	}
	argv := fake.calls[0]
	for _, want := range [][]string{{"--draft"}, {"--base", "main"}, {"--head", "l8/add-lint"}, {"--body-file", "-"}} {
		if i := slices.Index(argv, want[0]); i < 0 || !slices.Equal(argv[i:i+len(want)], want) {
			t.Errorf("gh argv %v lacks %v", argv, want)
		}
	}
	if fake.stdin[0] != "## Why the change\n\nLint." {
		t.Errorf("body = %q", fake.stdin[0])
	}
	stored, _ := svc.store.Task(task.ID)
	if stored.PRNumber != 142 || stored.PRState != "OPEN" || stored.LastPushed != first {
		t.Fatalf("stored = %+v", stored)
	}
	upstream := filepath.Join(filepath.Dir(clone), "upstream.git")
	if got := git(t, upstream, "rev-parse", "l8/add-lint"); got != first {
		t.Fatalf("upstream branch = %s, want %s", got, first)
	}
	if _, err := svc.OpenPR(ctx, task.ID, PRInput{Title: "Again"}); err == nil {
		t.Fatal("opened a second PR")
	}

	git(t, task.Worktree, "commit", "-q", "--amend", "-m", "Add lint target (amended)")
	amended := git(t, task.Worktree, "rev-parse", "HEAD")
	if err := svc.Push(ctx, task.ID); err != nil {
		t.Fatalf("push after amend: %v", err)
	}
	if got := git(t, upstream, "rev-parse", "l8/add-lint"); got != amended {
		t.Fatalf("upstream after lease push = %s, want %s", got, amended)
	}
}

func TestFixReviewsSendsTheThreads(t *testing.T) {
	ctx := context.Background()
	svc, st, ps := serviceWithSessions(t)
	fake := &fakeGH{out: map[string]string{"api graphql": `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[
	  {"isResolved":false,"path":"cmd/l8/main.go","line":42,"comments":{"nodes":[{"author":{"login":"alice"},"body":"name this flag --task"}]}}]}}}}}`}}
	svc.gh = fake.cli()
	p, err := svc.AddProject(ctx, repo(t))
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.Create(ctx, CreateInput{ProjectID: p.ID, Title: "Reviewed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.FixReviews(ctx, task.ID, ""); err == nil {
		t.Fatal("fixed reviews of a task without a PR")
	}
	if err := st.SetTaskPR(task.ID, 9, "https://github.com/o/r/pull/9", "OPEN"); err != nil {
		t.Fatal(err)
	}
	ss, err := svc.FixReviews(ctx, task.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if ss.Label != "fix-review" || ps.count() != 1 {
		t.Fatalf("session = %+v, procs = %d", ss, ps.count())
	}
	sent := ps.get(0).sent[0]
	for _, want := range []string{"#9", "cmd/l8/main.go:42", "alice: name this flag --task"} {
		if !strings.Contains(sent, want) {
			t.Errorf("message lacks %q:\n%s", want, sent)
		}
	}
}
