// Package gh drives pull requests through the GitHub CLI with the user's own login.
package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// ErrNoPR is returned when a branch has no pull request.
var ErrNoPR = errors.New("no pull request")

// CLI runs gh in a repository's worktree; its zero value runs the real gh.
type CLI struct {
	// Run replaces running gh, for tests.
	Run func(ctx context.Context, dir, stdin string, args ...string) ([]byte, error)
}

func (c CLI) run(ctx context.Context, dir, stdin string, args ...string) ([]byte, error) {
	if c.Run != nil {
		return c.Run(ctx, dir, stdin, args...)
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1", "NO_COLOR=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("gh %s: %s", args[0], msg)
	}
	return stdout.Bytes(), nil
}

// PR is a pull request with its checks and unresolved review threads.
type PR struct {
	Number         int      `json:"number"`
	URL            string   `json:"url"`
	State          string   `json:"state"`
	Draft          bool     `json:"draft"`
	ReviewDecision string   `json:"reviewDecision"`
	Mergeable      string   `json:"mergeable"`
	Head           string   `json:"head"`
	Base           string   `json:"base"`
	Checks         []Check  `json:"checks"`
	Threads        []Thread `json:"threads"`
}

// Check is one CI check; State is PENDING, SUCCESS, NEUTRAL, SKIPPED or a failure such as FAILURE.
type Check struct {
	Name  string `json:"name"`
	State string `json:"state"`
	URL   string `json:"url"`
}

// Failed reports whether the check ended in a failure.
func (c Check) Failed() bool {
	switch c.State {
	case "FAILURE", "ERROR", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED", "STARTUP_FAILURE":
		return true
	}
	return false
}

// Thread is an unresolved review thread on a line of the PR.
type Thread struct {
	Path     string    `json:"path"`
	Line     int       `json:"line"`
	Outdated bool      `json:"outdated"`
	Comments []Comment `json:"comments"`
}

type Comment struct {
	Author string `json:"author"`
	Body   string `json:"body"`
	URL    string `json:"url"`
}

// NewPR is a pull request to open from Head into Base.
type NewPR struct {
	Head  string
	Base  string
	Title string
	Body  string
	Draft bool
}

// PRCreate opens a pull request and returns its number and URL.
func (c CLI) PRCreate(ctx context.Context, dir string, in NewPR) (PR, error) {
	args := []string{"pr", "create", "--head", in.Head, "--base", in.Base, "--title", in.Title, "--body-file", "-"}
	if in.Draft {
		args = append(args, "--draft")
	}
	out, err := c.run(ctx, dir, in.Body, args...)
	if err != nil {
		return PR{}, err
	}
	url := lastLine(string(out))
	n, err := strconv.Atoi(url[strings.LastIndex(url, "/")+1:])
	if err != nil {
		return PR{}, fmt.Errorf("gh pr create: unexpected output %q", url)
	}
	return PR{Number: n, URL: url, State: "OPEN", Draft: in.Draft, Head: in.Head, Base: in.Base, Checks: []Check{}, Threads: []Thread{}}, nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

const viewFields = "number,url,state,isDraft,reviewDecision,mergeable,headRefName,baseRefName,statusCheckRollup"

// PRView returns the pull request ref names (a number or a branch), without its threads.
func (c CLI) PRView(ctx context.Context, dir, ref string) (PR, error) {
	out, err := c.run(ctx, dir, "", "pr", "view", ref, "--json", viewFields)
	if err != nil {
		if strings.Contains(err.Error(), "no pull requests found") {
			return PR{}, ErrNoPR
		}
		return PR{}, err
	}
	var v struct {
		Number            int    `json:"number"`
		URL               string `json:"url"`
		State             string `json:"state"`
		IsDraft           bool   `json:"isDraft"`
		ReviewDecision    string `json:"reviewDecision"`
		Mergeable         string `json:"mergeable"`
		HeadRefName       string `json:"headRefName"`
		BaseRefName       string `json:"baseRefName"`
		StatusCheckRollup []struct {
			Typename   string `json:"__typename"`
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			DetailsURL string `json:"detailsUrl"`
			Context    string `json:"context"`
			State      string `json:"state"`
			TargetURL  string `json:"targetUrl"`
		} `json:"statusCheckRollup"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return PR{}, fmt.Errorf("gh pr view: %w", err)
	}
	pr := PR{Number: v.Number, URL: v.URL, State: v.State, Draft: v.IsDraft, ReviewDecision: v.ReviewDecision,
		Mergeable: v.Mergeable, Head: v.HeadRefName, Base: v.BaseRefName, Checks: []Check{}, Threads: []Thread{}}
	for _, r := range v.StatusCheckRollup {
		switch {
		case r.Typename == "StatusContext":
			pr.Checks = append(pr.Checks, Check{Name: r.Context, State: r.State, URL: r.TargetURL})
		case r.Status != "COMPLETED":
			pr.Checks = append(pr.Checks, Check{Name: r.Name, State: "PENDING", URL: r.DetailsURL})
		default:
			pr.Checks = append(pr.Checks, Check{Name: r.Name, State: r.Conclusion, URL: r.DetailsURL})
		}
	}
	return pr, nil
}

// PRReady marks a draft pull request ready for review.
func (c CLI) PRReady(ctx context.Context, dir string, number int) error {
	_, err := c.run(ctx, dir, "", "pr", "ready", strconv.Itoa(number))
	return err
}

const threadsQuery = `query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      reviewThreads(first: 100) { nodes { isResolved isOutdated path line originalLine
        comments(first: 50) { nodes { author { login } body url } } } }
    }
  }
}`

// Threads returns the unresolved review threads of a pull request.
func (c CLI) Threads(ctx context.Context, dir string, number int) ([]Thread, error) {
	out, err := c.run(ctx, dir, "", "api", "graphql", "-f", "query="+threadsQuery,
		"-F", "owner={owner}", "-F", "name={repo}", "-F", "number="+strconv.Itoa(number))
	if err != nil {
		return nil, err
	}
	var v struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						Nodes []struct {
							IsResolved   bool   `json:"isResolved"`
							IsOutdated   bool   `json:"isOutdated"`
							Path         string `json:"path"`
							Line         int    `json:"line"`
							OriginalLine int    `json:"originalLine"`
							Comments     struct {
								Nodes []struct {
									Author *struct {
										Login string `json:"login"`
									} `json:"author"`
									Body string `json:"body"`
									URL  string `json:"url"`
								} `json:"nodes"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return nil, fmt.Errorf("gh api graphql: %w", err)
	}
	threads := []Thread{}
	for _, n := range v.Data.Repository.PullRequest.ReviewThreads.Nodes {
		if n.IsResolved {
			continue
		}
		th := Thread{Path: n.Path, Line: n.Line, Outdated: n.IsOutdated, Comments: []Comment{}}
		if th.Line == 0 {
			th.Line = n.OriginalLine
		}
		for _, cm := range n.Comments.Nodes {
			author := "ghost"
			if cm.Author != nil {
				author = cm.Author.Login
			}
			th.Comments = append(th.Comments, Comment{Author: author, Body: cm.Body, URL: cm.URL})
		}
		threads = append(threads, th)
	}
	return threads, nil
}
