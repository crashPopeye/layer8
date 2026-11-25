// Package linear reads Linear issues and moves their state through Linear's
// GraphQL API, authenticated with a personal API key.
package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Endpoint is Linear's GraphQL API.
const Endpoint = "https://api.linear.app/graphql"

// Client calls Linear with one personal API key.
type Client struct {
	key      string
	endpoint string
	http     *http.Client
}

// New returns a client authenticated with a personal API key, or nil when key is empty.
func New(key string) *Client {
	return NewAt(Endpoint, key)
}

// NewAt is New against another GraphQL endpoint.
func NewAt(endpoint, key string) *Client {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	return &Client{key: key, endpoint: endpoint, http: &http.Client{Timeout: 30 * time.Second}}
}

// Summary is an issue as lists show it.
type Summary struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	BranchName string `json:"branchName"`
	State      string `json:"state"`
	Team       string `json:"team"`
}

// Issue is one issue with everything an agent needs to work on it.
type Issue struct {
	Summary
	Description string    `json:"description"`
	Assignee    string    `json:"assignee"`
	Project     string    `json:"project"`
	Parent      string    `json:"parent"`
	Labels      []string  `json:"labels"`
	Children    []Summary `json:"children"`
	Comments    []Comment `json:"comments"`
	Attachments []Link    `json:"attachments"`
}

type Comment struct {
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
}

type Link struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

const summaryFields = `id identifier title url branchName state { name } team { key }`

type name struct {
	Name string `json:"name"`
}

type nodes[T any] struct {
	Nodes []T `json:"nodes"`
}

type rawSummary struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	BranchName string `json:"branchName"`
	State      name   `json:"state"`
	Team       struct {
		Key string `json:"key"`
	} `json:"team"`
}

func (r rawSummary) summary() Summary {
	return Summary{ID: r.ID, Identifier: r.Identifier, Title: r.Title, URL: r.URL, BranchName: r.BranchName, State: r.State.Name, Team: r.Team.Key}
}

func summaries(raw []rawSummary) []Summary {
	out := make([]Summary, len(raw))
	for i, r := range raw {
		out[i] = r.summary()
	}
	return out
}

// identifier matches issue identifiers such as FLOW-1297.
var identifier = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*-[0-9]+$`)

// Find lists the open issues assigned to the key's user when q is empty, the
// issue q names when q is an identifier, and otherwise the issues matching q.
func (c *Client) Find(ctx context.Context, q string) ([]Summary, error) {
	q = strings.TrimSpace(q)
	switch {
	case q == "":
		return c.MyIssues(ctx)
	case identifier.MatchString(q):
		if i, err := c.Issue(ctx, strings.ToUpper(q)); err == nil {
			return []Summary{i.Summary}, nil
		}
	}
	return c.Search(ctx, q)
}

// MyIssues lists the open issues assigned to the key's user, most recently updated first.
func (c *Client) MyIssues(ctx context.Context) ([]Summary, error) {
	var data struct {
		Viewer struct {
			AssignedIssues nodes[rawSummary] `json:"assignedIssues"`
		} `json:"viewer"`
	}
	q := `query { viewer { assignedIssues(first: 50, orderBy: updatedAt,
	  filter: { state: { type: { nin: ["completed", "canceled"] } } }) { nodes { ` + summaryFields + ` } } } }`
	if err := c.gql(ctx, q, nil, &data); err != nil {
		return nil, err
	}
	return summaries(data.Viewer.AssignedIssues.Nodes), nil
}

// Search lists the issues matching term.
func (c *Client) Search(ctx context.Context, term string) ([]Summary, error) {
	var data struct {
		SearchIssues nodes[rawSummary] `json:"searchIssues"`
	}
	q := `query($term: String!) { searchIssues(term: $term, first: 30) { nodes { ` + summaryFields + ` } } }`
	if err := c.gql(ctx, q, map[string]any{"term": term}, &data); err != nil {
		return nil, err
	}
	return summaries(data.SearchIssues.Nodes), nil
}

// Issue fetches one issue, by UUID or identifier, with its children, comments and attachments.
func (c *Client) Issue(ctx context.Context, id string) (Issue, error) {
	var data struct {
		Issue *struct {
			rawSummary
			Description string            `json:"description"`
			Assignee    *name             `json:"assignee"`
			Project     *name             `json:"project"`
			Parent      *rawSummary       `json:"parent"`
			Labels      nodes[name]       `json:"labels"`
			Children    nodes[rawSummary] `json:"children"`
			Attachments nodes[Link]       `json:"attachments"`
			Comments    nodes[rawComment] `json:"comments"`
		} `json:"issue"`
	}
	q := `query($id: String!) { issue(id: $id) { ` + summaryFields + `
	  description assignee { name } project { name } parent { identifier title }
	  labels { nodes { name } }
	  children(first: 50) { nodes { ` + summaryFields + ` } }
	  attachments { nodes { title url } }
	  comments(first: 100) { nodes { body createdAt user { name } } } } }`
	if err := c.gql(ctx, q, map[string]any{"id": id}, &data); err != nil {
		return Issue{}, err
	}
	r := data.Issue
	if r == nil {
		return Issue{}, fmt.Errorf("linear: no issue %s", id)
	}
	i := Issue{
		Summary:     r.summary(),
		Description: r.Description,
		Children:    summaries(r.Children.Nodes),
		Attachments: r.Attachments.Nodes,
	}
	if r.Assignee != nil {
		i.Assignee = r.Assignee.Name
	}
	if r.Project != nil {
		i.Project = r.Project.Name
	}
	if r.Parent != nil {
		i.Parent = r.Parent.Identifier + " " + r.Parent.Title
	}
	for _, l := range r.Labels.Nodes {
		i.Labels = append(i.Labels, l.Name)
	}
	for _, cm := range r.Comments.Nodes {
		i.Comments = append(i.Comments, cm.comment())
	}
	slices.SortStableFunc(i.Comments, func(a, b Comment) int { return strings.Compare(a.CreatedAt, b.CreatedAt) })
	return i, nil
}

type rawComment struct {
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
	User      *name  `json:"user"`
}

func (r rawComment) comment() Comment {
	author := "unknown"
	if r.User != nil {
		author = r.User.Name
	}
	return Comment{Author: author, Body: r.Body, CreatedAt: r.CreatedAt}
}

// SetState moves the issue id to its team's workflow state named state, e.g. "In Review".
func (c *Client) SetState(ctx context.Context, id, state string) error {
	var data struct {
		Issue *struct {
			ID   string `json:"id"`
			Team struct {
				States nodes[struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				}] `json:"states"`
			} `json:"team"`
		} `json:"issue"`
	}
	q := `query($id: String!) { issue(id: $id) { id team { states { nodes { id name } } } } }`
	if err := c.gql(ctx, q, map[string]any{"id": id}, &data); err != nil {
		return err
	}
	if data.Issue == nil {
		return fmt.Errorf("linear: no issue %s", id)
	}
	stateID := ""
	for _, s := range data.Issue.Team.States.Nodes {
		if strings.EqualFold(s.Name, state) {
			stateID = s.ID
		}
	}
	if stateID == "" {
		return fmt.Errorf("linear: the team of %s has no state %q", id, state)
	}
	var res struct {
		IssueUpdate struct {
			Success bool `json:"success"`
		} `json:"issueUpdate"`
	}
	m := `mutation($id: String!, $state: String!) { issueUpdate(id: $id, input: { stateId: $state }) { success } }`
	if err := c.gql(ctx, m, map[string]any{"id": data.Issue.ID, "state": stateID}, &res); err != nil {
		return err
	}
	if !res.IssueUpdate.Success {
		return fmt.Errorf("linear: moving %s to %s did not succeed", id, state)
	}
	return nil
}

// gql posts one GraphQL operation and decodes its data into out.
func (c *Client) gql(ctx context.Context, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.key)
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("linear: %w", err)
	}
	defer res.Body.Close()
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(res.Body).Decode(&env); err != nil {
		return fmt.Errorf("linear: HTTP %d: %w", res.StatusCode, err)
	}
	if len(env.Errors) > 0 {
		msgs := make([]string, len(env.Errors))
		for i, e := range env.Errors {
			msgs[i] = e.Message
		}
		return fmt.Errorf("linear: %s", strings.Join(msgs, "; "))
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("linear: HTTP %d", res.StatusCode)
	}
	return json.Unmarshal(env.Data, out)
}
