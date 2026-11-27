// Package github opens pull requests for tasks and keeps their status, checks and review threads current.
package github

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/crashPopeye/layer8/internal/gitx"
)

// Client calls the GitHub REST and GraphQL APIs.
type Client struct {
	API   string
	HTTP  *http.Client
	Token func(ctx context.Context) (string, error)
}

// GHToken returns a token source that uses token when set, else asks `gh auth token`.
func GHToken(token string) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		if token != "" {
			return token, nil
		}
		out, err := gitx.Command(ctx, "gh", "auth", "token").Output()
		if err != nil {
			return "", fmt.Errorf("no GitHub token: set githubToken in config.json or run gh auth login (%v)", err)
		}
		return strings.TrimSpace(string(out)), nil
	}
}

func (c *Client) call(ctx context.Context, method, path string, body, out any) error {
	token, err := c.Token(ctx)
	if err != nil {
		return err
	}
	var payload io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(c.API, "/")+path, payload)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		var e struct {
			Message string
			Errors  []struct{ Message string }
		}
		json.Unmarshal(data, &e)
		for _, sub := range e.Errors {
			e.Message += ": " + sub.Message
		}
		return fmt.Errorf("github %s %s: %d %s", method, path, res.StatusCode, e.Message)
	}
	if s, ok := out.(*string); ok {
		*s = string(data)
		return nil
	}
	return json.Unmarshal(data, out)
}

var remotePattern = regexp.MustCompile(`github\.com[:/]+([^/]+/[^/]+?)(?:\.git)?/?$`)

// Repo returns "owner/name" of the worktree's configured origin URL.
func Repo(ctx context.Context, dir string) (string, error) {
	url, err := gitx.Run(ctx, dir, "config", "--get", "remote.origin.url")
	if err != nil {
		return "", fmt.Errorf("no origin remote: %w", err)
	}
	m := remotePattern.FindStringSubmatch(url)
	if m == nil {
		return "", fmt.Errorf("origin %s is not a github.com repository", url)
	}
	return m[1], nil
}

// NewPR is what CreatePR sends.
type NewPR struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"head"`
	Base  string `json:"base"`
	Draft bool   `json:"draft"`
}

// FindOpenPR returns the number of the open pull request from branch, or 0.
func (c *Client) FindOpenPR(ctx context.Context, repo, branch string) (int, error) {
	owner, _, _ := strings.Cut(repo, "/")
	var list []struct{ Number int }
	err := c.call(ctx, "GET", "/repos/"+repo+"/pulls?state=open&head="+owner+":"+branch, nil, &list)
	if err != nil || len(list) == 0 {
		return 0, err
	}
	return list[0].Number, nil
}

// CreatePR opens a pull request and returns its number.
func (c *Client) CreatePR(ctx context.Context, repo string, pr NewPR) (int, error) {
	var out struct{ Number int }
	err := c.call(ctx, "POST", "/repos/"+repo+"/pulls", pr, &out)
	return out.Number, err
}

// LogTail returns up to lines lines of a GitHub Actions job's log, without timestamps, ending at its last error.
func (c *Client) LogTail(ctx context.Context, repo string, job int64, lines int) (string, error) {
	var log string
	if err := c.call(ctx, "GET", fmt.Sprintf("/repos/%s/actions/jobs/%d/logs", repo, job), nil, &log); err != nil {
		return "", err
	}
	all := strings.Split(strings.TrimRight(log, "\n"), "\n")
	end := len(all)
	for i := len(all) - 1; i >= 0; i-- {
		if strings.Contains(all[i], "##[error]") {
			end = i + 1
			break
		}
	}
	tail := all[max(0, end-lines):end]
	for i, l := range tail {
		tail[i] = timestamp.ReplaceAllString(strings.TrimSuffix(l, "\r"), "")
	}
	return strings.Join(tail, "\n"), nil
}

var timestamp = regexp.MustCompile(`^\d{4}-\d\d-\d\dT[\d:.]+Z `)

// PR is a pull request's state as a task shows it.
type PR struct {
	Number            int      `json:"number"`
	URL               string   `json:"url"`
	State             string   `json:"state"`
	Draft             bool     `json:"draft"`
	Base              string   `json:"base"`
	Mergeable         string   `json:"mergeable"`
	ReviewDecision    string   `json:"reviewDecision"`
	ChecksState       string   `json:"checksState"`
	Checks            []Check  `json:"checks"`
	UnresolvedThreads int      `json:"unresolvedThreads"`
	Threads           []Thread `json:"threads"`
}

// Check is one CI check of the PR's head commit; State is pending, success, failure or skipped.
type Check struct {
	Name    string `json:"name"`
	State   string `json:"state"`
	URL     string `json:"url"`
	Job     int64  `json:"job,omitempty"`
	LogTail string `json:"logTail,omitempty"`
}

// Thread is an unresolved review thread.
type Thread struct {
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Author   string `json:"author"`
	Body     string `json:"body"`
	URL      string `json:"url"`
	Outdated bool   `json:"outdated"`
	Replies  int    `json:"replies"`
}

const statusQuery = `query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      number url state isDraft baseRefName mergeable reviewDecision
      reviewThreads(first: 100) { nodes { isResolved isOutdated path line
        comments(first: 50) { totalCount nodes { author { login } body url } } } }
      commits(last: 1) { nodes { commit { statusCheckRollup { contexts(first: 100) { nodes {
        __typename
        ... on CheckRun { name status conclusion detailsUrl databaseId checkSuite { app { slug } } }
        ... on StatusContext { context state targetUrl }
      } } } } } }
    }
  }
}`

type statusData struct {
	Repository struct {
		PullRequest *struct {
			Number         int
			URL            string
			State          string
			IsDraft        bool
			BaseRefName    string
			Mergeable      string
			ReviewDecision string
			ReviewThreads  struct {
				Nodes []struct {
					IsResolved, IsOutdated bool
					Path                   string
					Line                   int
					Comments               struct {
						TotalCount int
						Nodes      []struct {
							Author    struct{ Login string }
							Body, URL string
						}
					}
				}
			}
			Commits struct {
				Nodes []struct {
					Commit struct {
						StatusCheckRollup *struct {
							Contexts struct{ Nodes []checkNode }
						}
					}
				}
			}
		}
	}
}

type checkNode struct {
	Typename                             string `json:"__typename"`
	Name, Status, Conclusion, DetailsURL string
	DatabaseID                           int64
	CheckSuite                           struct{ App struct{ Slug string } }
	Context, State, TargetURL            string
}

// Status reads a pull request's state, checks and unresolved threads.
func (c *Client) Status(ctx context.Context, repo string, number int) (PR, error) {
	owner, name, _ := strings.Cut(repo, "/")
	var res struct {
		Data   statusData
		Errors []struct{ Message string }
	}
	body := map[string]any{"query": statusQuery, "variables": map[string]any{"owner": owner, "name": name, "number": number}}
	if err := c.call(ctx, "POST", "/graphql", body, &res); err != nil {
		return PR{}, err
	}
	if len(res.Errors) > 0 {
		return PR{}, fmt.Errorf("github graphql: %s", res.Errors[0].Message)
	}
	p := res.Data.Repository.PullRequest
	if p == nil {
		return PR{}, fmt.Errorf("%s has no pull request #%d", repo, number)
	}
	pr := PR{Number: p.Number, URL: p.URL, State: strings.ToLower(p.State), Draft: p.IsDraft, Base: p.BaseRefName,
		Mergeable: strings.ToLower(p.Mergeable), ReviewDecision: strings.ToLower(p.ReviewDecision), Checks: []Check{}, Threads: []Thread{}}
	for _, t := range p.ReviewThreads.Nodes {
		if t.IsResolved || len(t.Comments.Nodes) == 0 {
			continue
		}
		first := t.Comments.Nodes[0]
		pr.Threads = append(pr.Threads, Thread{Path: t.Path, Line: t.Line, Author: first.Author.Login, Body: first.Body,
			URL: first.URL, Outdated: t.IsOutdated, Replies: t.Comments.TotalCount - 1})
	}
	pr.UnresolvedThreads = len(pr.Threads)
	for _, commit := range p.Commits.Nodes {
		if rollup := commit.Commit.StatusCheckRollup; rollup != nil {
			for _, n := range rollup.Contexts.Nodes {
				pr.Checks = append(pr.Checks, n.check())
			}
		}
	}
	pr.ChecksState = overall(pr.Checks)
	return pr, nil
}

func (n checkNode) check() Check {
	if n.Typename == "StatusContext" {
		state := map[string]string{"SUCCESS": "success", "FAILURE": "failure", "ERROR": "failure"}[n.State]
		return Check{Name: n.Context, State: cmp.Or(state, "pending"), URL: n.TargetURL}
	}
	c := Check{Name: n.Name, URL: n.DetailsURL, State: "pending"}
	if n.Status == "COMPLETED" {
		c.State = map[string]string{"SUCCESS": "success", "NEUTRAL": "skipped", "SKIPPED": "skipped", "STALE": "skipped"}[n.Conclusion]
		c.State = cmp.Or(c.State, "failure")
	}
	if n.CheckSuite.App.Slug == "github-actions" {
		c.Job = n.DatabaseID
	}
	return c
}

// overall is failure if any check failed, else pending if any runs, else success; empty without checks.
func overall(checks []Check) string {
	for _, state := range []string{"failure", "pending"} {
		if slices.ContainsFunc(checks, func(c Check) bool { return c.State == state }) {
			return state
		}
	}
	if len(checks) == 0 {
		return ""
	}
	return "success"
}
