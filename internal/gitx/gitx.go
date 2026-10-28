// Package gitx wraps the git CLI. Every call runs `git -C <dir>` so the user's
// own git config, credentials and hooks apply.
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Error carries git's stderr so callers can show it or match on it.
type Error struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *Error) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = e.Err.Error()
	}
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), msg)
}

func (e *Error) Unwrap() error { return e.Err }

const defaultTimeout = 2 * time.Minute

// Run runs git in dir and returns trimmed stdout.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := RunRaw(ctx, dir, args...)
	return strings.TrimRight(out, "\n"), err
}

// RunRaw returns stdout untouched (diffs need their trailing newlines).
func RunRaw(ctx context.Context, dir string, args ...string) (string, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	// Never block on a credential or editor prompt: there is no terminal.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true", "GIT_SEQUENCE_EDITOR=true", "LC_ALL=C")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), &Error{Args: args, Stderr: stderr.String(), Err: err}
	}
	return stdout.String(), nil
}

// RepoRoot returns the top-level directory of the repo containing path, or "" if none.
func RepoRoot(ctx context.Context, path string) string {
	out, err := Run(ctx, path, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	return out
}

func RefExists(ctx context.Context, repo, ref string) bool {
	_, err := Run(ctx, repo, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	return err == nil
}

func RevParse(ctx context.Context, repo, ref string) (string, error) {
	return Run(ctx, repo, "rev-parse", "--verify", ref+"^{commit}")
}

func HasRemote(ctx context.Context, repo, remote string) bool {
	out, err := Run(ctx, repo, "remote")
	if err != nil {
		return false
	}
	for _, r := range strings.Split(out, "\n") {
		if strings.TrimSpace(r) == remote {
			return true
		}
	}
	return false
}

func DefaultBranch(ctx context.Context, repo string) string {
	if out, err := Run(ctx, repo, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		return strings.TrimPrefix(out, "origin/")
	}
	for _, c := range []string{"main", "master", "trunk", "develop"} {
		if RefExists(ctx, repo, "refs/heads/"+c) || RefExists(ctx, repo, "refs/remotes/origin/"+c) {
			return c
		}
	}
	out, _ := Run(ctx, repo, "rev-parse", "--abbrev-ref", "HEAD")
	return out
}

// BaseRef is the ref new work starts from: origin's copy of branch when there is one.
func BaseRef(ctx context.Context, repo, branch string) string {
	if HasRemote(ctx, repo, "origin") && RefExists(ctx, repo, "refs/remotes/origin/"+branch) {
		return "origin/" + branch
	}
	return branch
}

// Fetch is best effort; offline work must keep going.
func Fetch(ctx context.Context, repo string) error {
	if !HasRemote(ctx, repo, "origin") {
		return nil
	}
	_, err := Run(ctx, repo, "fetch", "--quiet", "--prune", "origin")
	return err
}

// AddWorktree creates a worktree for branch, reusing the branch if it already exists
// locally or on origin so restarting a task never forks history.
func AddWorktree(ctx context.Context, repo, path, branch, base string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	switch {
	case RefExists(ctx, repo, "refs/heads/"+branch):
		_, err := Run(ctx, repo, "worktree", "add", path, branch)
		return err
	case RefExists(ctx, repo, "refs/remotes/origin/"+branch):
		_, err := Run(ctx, repo, "worktree", "add", "--track", "-b", branch, path, "origin/"+branch)
		return err
	default:
		_, err := Run(ctx, repo, "worktree", "add", "--no-track", "-b", branch, path, base)
		return err
	}
}

func RemoveWorktree(ctx context.Context, repo, path string) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		_, _ = Run(ctx, repo, "worktree", "prune")
		return nil
	}
	_, err := Run(ctx, repo, "worktree", "remove", path)
	return err
}

// Dirty reports uncommitted changes, including untracked files.
func Dirty(ctx context.Context, dir string) (bool, error) {
	out, err := Run(ctx, dir, "status", "--porcelain")
	return out != "", err
}

func CurrentBranch(ctx context.Context, dir string) string {
	out, _ := Run(ctx, dir, "branch", "--show-current")
	return out
}

func IsAncestor(ctx context.Context, dir, ancestor, descendant string) bool {
	_, err := Run(ctx, dir, "merge-base", "--is-ancestor", ancestor, descendant)
	return err == nil
}

func MergeBase(ctx context.Context, dir, a, b string) (string, error) {
	return Run(ctx, dir, "merge-base", a, b)
}

func HasUpstream(ctx context.Context, dir string) bool {
	_, err := Run(ctx, dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	return err == nil
}

// RebaseOnto replays the commits after oldBase onto newBase. On conflict the rebase
// is aborted so the worktree is left exactly as it was, and ErrConflict is returned.
func RebaseOnto(ctx context.Context, dir, newBase, oldBase string) error {
	_, err := Run(ctx, dir, "rebase", "--onto", newBase, oldBase)
	if err == nil {
		return nil
	}
	if rebaseInProgress(dir) {
		// The rebase may have died because ctx was cancelled; abort on a fresh context
		// so the worktree is never left mid-rebase.
		actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		_, _ = Run(actx, dir, "rebase", "--abort")
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: %v", ErrConflict, err)
	}
	return err
}

var ErrConflict = errors.New("rebase conflict")

func rebaseInProgress(dir string) bool {
	gitDir, err := Run(context.Background(), dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return false
	}
	for _, d := range []string{"rebase-merge", "rebase-apply"} {
		if _, err := os.Stat(filepath.Join(gitDir, d)); err == nil {
			return true
		}
	}
	return false
}

// ErrDiverged means origin has commits for the branch that the local branch lacks.
var ErrDiverged = errors.New("GitHub has commits on this branch that your local branch doesn't (for example a suggestion committed on the PR); pull them first: git pull --rebase")

// RemoteDiverged reports whether origin/<branch> holds work that is not in HEAD and
// was not pushed by us (lastPushed). Call after a fetch.
func RemoteDiverged(ctx context.Context, dir, branch, lastPushed string) bool {
	remote, err := RevParse(ctx, dir, "refs/remotes/origin/"+branch)
	if err != nil {
		return false // never pushed
	}
	return remote != lastPushed && !IsAncestor(ctx, dir, remote, "HEAD")
}

// SafePush pushes HEAD to origin/<branch>. A rebased branch is force-pushed, but only
// when origin's copy is what we pushed last or is already contained in HEAD, and the
// lease pins that exact commit, so nobody else's work is ever overwritten.
// preRebase, when set, is HEAD from before a rebase layer8 just did: origin's copy
// being contained in it is also safe. It returns the pushed SHA.
func SafePush(ctx context.Context, dir, branch, lastPushed, preRebase string) (string, error) {
	_, _ = Run(ctx, dir, "fetch", "--quiet", "origin", branch) // fine to fail: branch may not exist yet
	head, err := RevParse(ctx, dir, "HEAD")
	if err != nil {
		return "", err
	}
	args := []string{"push", "--set-upstream", "origin", "HEAD:refs/heads/" + branch}
	if remote, err := RevParse(ctx, dir, "refs/remotes/origin/"+branch); err == nil {
		if remote == head {
			_, err := Run(ctx, dir, "branch", "--set-upstream-to=origin/"+branch)
			return head, err
		}
		contained := remote == lastPushed || IsAncestor(ctx, dir, remote, "HEAD") ||
			(preRebase != "" && IsAncestor(ctx, dir, remote, preRebase))
		if !contained {
			return "", ErrDiverged
		}
		args = []string{"push", "--force-with-lease=" + branch + ":" + remote, "--set-upstream", "origin", "HEAD:refs/heads/" + branch}
	}
	if _, err := Run(ctx, dir, args...); err != nil {
		return "", err
	}
	return head, nil
}

// OriginRepo returns owner and name when origin points at GitHub.
func OriginRepo(ctx context.Context, repo string) (owner, name string, ok bool) {
	url, err := Run(ctx, repo, "remote", "get-url", "origin")
	if err != nil {
		return "", "", false
	}
	return ParseGitHubURL(url)
}

var ghURL = regexp.MustCompile(`^(?:https://(?:[^@/]+@)?github\.com/|git@github\.com:|ssh://git@github\.com/)([^/]+)/(.+?)(?:\.git)?/?$`)

func ParseGitHubURL(url string) (owner, name string, ok bool) {
	m := ghURL.FindStringSubmatch(strings.TrimSpace(url))
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

var (
	branchBad   = regexp.MustCompile(`[^a-z0-9/._-]+`)
	branchDots  = regexp.MustCompile(`\.{2,}`)
	branchSlash = regexp.MustCompile(`/{2,}`)
)

// SanitizeBranch makes a free-form name safe as a branch name.
func SanitizeBranch(name string) string {
	s := strings.ToLower(name)
	s = branchBad.ReplaceAllString(s, "-")
	s = branchDots.ReplaceAllString(s, ".")
	s = branchSlash.ReplaceAllString(s, "/")
	s = strings.Trim(s, "-./")
	s = strings.ReplaceAll(s, "/.", "/")
	s = strings.TrimSuffix(s, ".lock")
	if len(s) > 100 {
		s = strings.Trim(s[:100], "-./")
	}
	return s
}

type Commit struct {
	SHA     string `json:"sha"`
	Short   string `json:"short"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	Author  string `json:"author"`
	Date    string `json:"date"`
}

// Log lists commits reachable from head but not from base, newest first.
func Log(ctx context.Context, dir, base, head string) ([]Commit, error) {
	const sep, end = "\x1f", "\x1e"
	out, err := Run(ctx, dir, "log", "--format=%H"+sep+"%h"+sep+"%s"+sep+"%b"+sep+"%an"+sep+"%aI"+end, base+".."+head)
	if err != nil {
		return nil, err
	}
	commits := []Commit{}
	for _, rec := range strings.Split(out, end) {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.Split(rec, sep)
		if len(f) != 6 {
			continue
		}
		commits = append(commits, Commit{SHA: f[0], Short: f[1], Subject: f[2], Body: strings.TrimSpace(f[3]), Author: f[4], Date: f[5]})
	}
	return commits, nil
}

// CommitAll stages everything and commits. Returns the new commit's SHA.
func CommitAll(ctx context.Context, dir, message string) (string, error) {
	if strings.TrimSpace(message) == "" {
		return "", errors.New("empty commit message")
	}
	if _, err := Run(ctx, dir, "add", "-A"); err != nil {
		return "", err
	}
	if _, err := Run(ctx, dir, "commit", "-m", message); err != nil {
		return "", err
	}
	return RevParse(ctx, dir, "HEAD")
}

func UntrackedFiles(ctx context.Context, dir string) ([]string, error) {
	out, err := Run(ctx, dir, "ls-files", "--others", "--exclude-standard")
	if err != nil || out == "" {
		return nil, err
	}
	return strings.Split(out, "\n"), nil
}
