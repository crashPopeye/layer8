// Package github talks to the GitHub API with the user's own token.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

// Token returns the configured token, or the gh CLI's token when none is configured.
func Token(configured string) (string, error) {
	if t := strings.TrimSpace(configured); t != "" {
		return t, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return "", errors.New("no GitHub token: run `gh auth login` or paste a token in Settings")
	}
	return strings.TrimSpace(string(out)), nil
}

type Client struct {
	token string
	HTTP  *http.Client // API calls: short overall timeout
	// Downloads (release assets, job logs) can take minutes on slow links; they are
	// bounded by the caller's context instead of a client timeout.
	Download *http.Client
	API      string // tests override
}

func New(token string) *Client {
	return &Client{
		token:    token,
		HTTP:     &http.Client{Timeout: 60 * time.Second},
		Download: &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 60 * time.Second}},
		API:      "https://api.github.com",
	}
}

type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("github: HTTP %d: %s", e.Status, e.Message) }

func (c *Client) do(ctx context.Context, method, path string, body any, accept string) (*http.Response, error) {
	return c.doWith(c.HTTP, ctx, method, path, body, accept)
}

func (c *Client) doWith(hc *http.Client, ctx context.Context, method, path string, body any, accept string) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.API+path, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if accept == "" {
		accept = "application/vnd.github+json"
	}
	req.Header.Set("Accept", accept)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: %w", err)
	}
	if res.StatusCode >= 300 {
		defer res.Body.Close()
		var e struct {
			Message string `json:"message"`
			Errors  []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		_ = json.Unmarshal(b, &e)
		msg := e.Message
		for _, x := range e.Errors {
			if x.Message != "" {
				msg += "; " + x.Message
			}
		}
		if msg == "" {
			msg = strings.TrimSpace(string(b))
		}
		return nil, &APIError{Status: res.StatusCode, Message: msg}
	}
	return res, nil
}

func (c *Client) JSON(ctx context.Context, method, path string, body, out any) error {
	res, err := c.do(ctx, method, path, body, "")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func (c *Client) GraphQL(ctx context.Context, query string, vars map[string]any, out any) error {
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.JSON(ctx, http.MethodPost, "/graphql", map[string]any{"query": query, "variables": vars}, &env); err != nil {
		return err
	}
	if len(env.Errors) > 0 {
		msgs := []string{}
		for _, e := range env.Errors {
			msgs = append(msgs, e.Message)
		}
		return fmt.Errorf("github graphql: %s", strings.Join(msgs, "; "))
	}
	return json.Unmarshal(env.Data, out)
}

type PullRequest struct {
	Number int    `json:"number"`
	URL    string `json:"html_url"`
	Draft  bool   `json:"draft"`
	State  string `json:"state"`
	Base   struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func (c *Client) CreatePR(ctx context.Context, owner, repo, head, base, title, body string, draft bool) (*PullRequest, error) {
	var pr PullRequest
	err := c.JSON(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/%s/pulls", owner, repo),
		map[string]any{"head": head, "base": base, "title": title, "body": body, "draft": draft}, &pr)
	return &pr, err
}

// FindPR returns the open PR whose head is branch, or nil.
func (c *Client) FindPR(ctx context.Context, owner, repo, branch string) (*PullRequest, error) {
	var prs []PullRequest
	err := c.JSON(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/pulls?state=all&head=%s", owner, repo, url.QueryEscape(owner+":"+branch)), nil, &prs)
	if err != nil || len(prs) == 0 {
		return nil, err
	}
	return &prs[0], nil
}

func (c *Client) SetPRBase(ctx context.Context, owner, repo string, number int, base string) error {
	return c.JSON(ctx, http.MethodPatch, fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, number), map[string]any{"base": base}, nil)
}

type Check struct {
	Name       string `json:"name"`
	Status     string `json:"status"`     // check runs: QUEUED, IN_PROGRESS, COMPLETED; status contexts: ""
	Conclusion string `json:"conclusion"` // SUCCESS, FAILURE, ... or the status context's state
	URL        string `json:"url"`
	JobID      int64  `json:"jobId"` // GitHub Actions job id when the check is an Actions job
}

func (c Check) Failed() bool {
	switch c.Conclusion {
	case "FAILURE", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED", "STARTUP_FAILURE", "ERROR":
		return true
	}
	return false
}

type ThreadComment struct {
	Author string `json:"author"`
	Body   string `json:"body"`
	URL    string `json:"url"`
}

type Thread struct {
	Path     string          `json:"path"`
	Line     int             `json:"line"`
	Outdated bool            `json:"outdated"`
	Comments []ThreadComment `json:"comments"`
}

type PRStatus struct {
	Number         int      `json:"number"`
	URL            string   `json:"url"`
	State          string   `json:"state"` // OPEN, MERGED, CLOSED
	Draft          bool     `json:"draft"`
	Base           string   `json:"base"`
	HeadSHA        string   `json:"headSha"`
	Mergeable      string   `json:"mergeable"` // MERGEABLE, CONFLICTING, UNKNOWN
	ReviewDecision string   `json:"reviewDecision"`
	CIState        string   `json:"ciState"` // SUCCESS, FAILURE, PENDING, ERROR, "" (no checks)
	Checks         []Check  `json:"checks"`
	OpenThreads    []Thread `json:"openThreads"`
}

const prQuery = `query($owner: String!, $repo: String!, $number: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      number url state isDraft baseRefName headRefOid mergeable reviewDecision
      commits(last: 1) { nodes { commit { statusCheckRollup { state contexts(first: 100) { nodes {
        __typename
        ... on CheckRun { name status conclusion detailsUrl databaseId checkSuite { app { slug } } }
        ... on StatusContext { context state targetUrl }
      } } } } } }
      reviewThreads(first: 100) { nodes { isResolved isOutdated path line originalLine
        comments(first: 30) { nodes { author { login } body url } } } }
    }
  }
}`

func (c *Client) PRStatus(ctx context.Context, owner, repo string, number int) (*PRStatus, error) {
	var data struct {
		Repository struct {
			PullRequest *struct {
				Number         int    `json:"number"`
				URL            string `json:"url"`
				State          string `json:"state"`
				IsDraft        bool   `json:"isDraft"`
				BaseRefName    string `json:"baseRefName"`
				HeadRefOid     string `json:"headRefOid"`
				Mergeable      string `json:"mergeable"`
				ReviewDecision string `json:"reviewDecision"`
				Commits        struct {
					Nodes []struct {
						Commit struct {
							StatusCheckRollup *struct {
								State    string `json:"state"`
								Contexts struct {
									Nodes []struct {
										Typename   string `json:"__typename"`
										Name       string `json:"name"`
										Status     string `json:"status"`
										Conclusion string `json:"conclusion"`
										DetailsURL string `json:"detailsUrl"`
										DatabaseID int64  `json:"databaseId"`
										CheckSuite *struct {
											App *struct {
												Slug string `json:"slug"`
											} `json:"app"`
										} `json:"checkSuite"`
										Context   string `json:"context"`
										State     string `json:"state"`
										TargetURL string `json:"targetUrl"`
									} `json:"nodes"`
								} `json:"contexts"`
							} `json:"statusCheckRollup"`
						} `json:"commit"`
					} `json:"nodes"`
				} `json:"commits"`
				ReviewThreads struct {
					Nodes []struct {
						IsResolved   bool   `json:"isResolved"`
						IsOutdated   bool   `json:"isOutdated"`
						Path         string `json:"path"`
						Line         *int   `json:"line"`
						OriginalLine *int   `json:"originalLine"`
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
	}
	if err := c.GraphQL(ctx, prQuery, map[string]any{"owner": owner, "repo": repo, "number": number}, &data); err != nil {
		return nil, err
	}
	pr := data.Repository.PullRequest
	if pr == nil {
		return nil, fmt.Errorf("github: PR #%d not found", number)
	}
	st := &PRStatus{Number: pr.Number, URL: pr.URL, State: pr.State, Draft: pr.IsDraft, Base: pr.BaseRefName,
		HeadSHA: pr.HeadRefOid, Mergeable: pr.Mergeable, ReviewDecision: pr.ReviewDecision, Checks: []Check{}, OpenThreads: []Thread{}}
	if len(pr.Commits.Nodes) > 0 {
		if roll := pr.Commits.Nodes[0].Commit.StatusCheckRollup; roll != nil {
			st.CIState = roll.State
			for _, n := range roll.Contexts.Nodes {
				if n.Typename == "CheckRun" {
					ck := Check{Name: n.Name, Status: n.Status, Conclusion: n.Conclusion, URL: n.DetailsURL}
					if n.CheckSuite != nil && n.CheckSuite.App != nil && n.CheckSuite.App.Slug == "github-actions" {
						ck.JobID = n.DatabaseID
					}
					st.Checks = append(st.Checks, ck)
				} else {
					st.Checks = append(st.Checks, Check{Name: n.Context, Conclusion: n.State, URL: n.TargetURL})
				}
			}
		}
	}
	for _, t := range pr.ReviewThreads.Nodes {
		if t.IsResolved {
			continue
		}
		th := Thread{Path: t.Path, Outdated: t.IsOutdated}
		if t.Line != nil {
			th.Line = *t.Line
		} else if t.OriginalLine != nil {
			th.Line = *t.OriginalLine
		}
		for _, cm := range t.Comments.Nodes {
			author := "ghost"
			if cm.Author != nil {
				author = cm.Author.Login
			}
			th.Comments = append(th.Comments, ThreadComment{Author: author, Body: cm.Body, URL: cm.URL})
		}
		st.OpenThreads = append(st.OpenThreads, th)
	}
	return st, nil
}

// JobLogTail returns the last maxLines lines of a GitHub Actions job log.
func (c *Client) JobLogTail(ctx context.Context, owner, repo string, jobID int64, maxLines int) (string, error) {
	res, err := c.doWith(c.downloader(), ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/actions/jobs/%d/logs", owner, repo, jobID), nil, "")
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n"), nil
}

type Release struct {
	TagName    string  `json:"tag_name"`
	Name       string  `json:"name"`
	Body       string  `json:"body"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	HTMLURL    string  `json:"html_url"`
	Assets     []Asset `json:"assets"`
}

type Asset struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	URL  string `json:"url"` // API URL; download with Accept: application/octet-stream
}

func (c *Client) LatestRelease(ctx context.Context, owner, repo string) (*Release, error) {
	var r Release
	if err := c.JSON(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/releases/latest", owner, repo), nil, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Client) downloader() *http.Client {
	if c.Download != nil {
		return c.Download
	}
	return c.HTTP
}

// DownloadAsset streams a release asset (works for private repos) into w.
func (c *Client) DownloadAsset(ctx context.Context, owner, repo string, id int64, w io.Writer) error {
	res, err := c.doWith(c.downloader(), ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/releases/assets/%d", owner, repo, id), nil, "application/octet-stream")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, err = io.Copy(w, res.Body)
	return err
}
