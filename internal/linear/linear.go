// Package linear is a small client for Linear's GraphQL API.
package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

const endpoint = "https://api.linear.app/graphql"

type Client struct {
	APIKey   string
	HTTP     *http.Client
	Endpoint string // tests override
}

func New(apiKey string) *Client {
	return &Client{APIKey: apiKey, HTTP: &http.Client{Timeout: 30 * time.Second}, Endpoint: endpoint}
}

type State struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Color string `json:"color"`
}

type IssueSummary struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	BranchName string `json:"branchName"`
	Priority   int    `json:"priority"`
	State      State  `json:"state"`
	Team       string `json:"team"`
	UpdatedAt  string `json:"updatedAt"`
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

type Issue struct {
	IssueSummary
	TeamID      string    `json:"teamId"`
	Description string    `json:"description"`
	Assignee    string    `json:"assignee"`
	AssigneeID  string    `json:"assigneeId"`
	Labels      []string  `json:"labels"`
	Parent      string    `json:"parent"`
	Comments    []Comment `json:"comments"`
	Links       []Link    `json:"links"`
}

func (c *Client) gql(ctx context.Context, query string, vars map[string]any, out any) error {
	if c.APIKey == "" {
		return errors.New("Linear API key is not configured")
	}
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	// Personal API keys go in Authorization without a Bearer prefix.
	req.Header.Set("Authorization", c.APIKey)
	res, err := c.HTTP.Do(req)
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

const summaryFields = `id identifier title url branchName priority updatedAt state { name type color } team { key }`

type rawSummary struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	BranchName string `json:"branchName"`
	Priority   int    `json:"priority"`
	UpdatedAt  string `json:"updatedAt"`
	State      State  `json:"state"`
	Team       struct {
		Key string `json:"key"`
	} `json:"team"`
}

func (r rawSummary) flat() IssueSummary {
	return IssueSummary{r.ID, r.Identifier, r.Title, r.URL, r.BranchName, r.Priority, r.State, r.Team.Key, r.UpdatedAt}
}

func flatten(in []rawSummary) []IssueSummary {
	out := make([]IssueSummary, len(in))
	for i, r := range in {
		out[i] = r.flat()
	}
	return out
}

// MyIssues lists open issues assigned to the API key's user, most recently updated first.
func (c *Client) MyIssues(ctx context.Context) ([]IssueSummary, error) {
	var data struct {
		Viewer struct {
			AssignedIssues struct {
				Nodes []rawSummary `json:"nodes"`
			} `json:"assignedIssues"`
		} `json:"viewer"`
	}
	q := `query { viewer { assignedIssues(first: 100, orderBy: updatedAt,
	  filter: { state: { type: { nin: ["completed", "canceled"] } } }) { nodes { ` + summaryFields + ` } } } }`
	if err := c.gql(ctx, q, nil, &data); err != nil {
		return nil, err
	}
	return flatten(data.Viewer.AssignedIssues.Nodes), nil
}

func (c *Client) Search(ctx context.Context, term string) ([]IssueSummary, error) {
	var data struct {
		SearchIssues struct {
			Nodes []rawSummary `json:"nodes"`
		} `json:"searchIssues"`
	}
	q := `query($term: String!) { searchIssues(term: $term, first: 30) { nodes { ` + summaryFields + ` } } }`
	if err := c.gql(ctx, q, map[string]any{"term": term}, &data); err != nil {
		return nil, err
	}
	return flatten(data.SearchIssues.Nodes), nil
}

// Issue fetches one issue by UUID or identifier ("ENG-123").
func (c *Client) Issue(ctx context.Context, id string) (*Issue, error) {
	var data struct {
		Issue *struct {
			rawSummary
			Description string `json:"description"`
			Team2       struct {
				ID string `json:"id"`
			} `json:"teamRef"`
			Assignee *struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"assignee"`
			Labels struct {
				Nodes []struct {
					Name string `json:"name"`
				} `json:"nodes"`
			} `json:"labels"`
			Parent *struct {
				Identifier string `json:"identifier"`
				Title      string `json:"title"`
			} `json:"parent"`
			Comments struct {
				Nodes []struct {
					Body      string `json:"body"`
					CreatedAt string `json:"createdAt"`
					User      *struct {
						Name string `json:"name"`
					} `json:"user"`
				} `json:"nodes"`
			} `json:"comments"`
			Attachments struct {
				Nodes []Link `json:"nodes"`
			} `json:"attachments"`
		} `json:"issue"`
	}
	q := `query($id: String!) { issue(id: $id) { ` + summaryFields + `
	  description
	  teamRef: team { id }
	  assignee { id name }
	  labels { nodes { name } }
	  parent { identifier title }
	  comments(first: 100) { nodes { body createdAt user { name } } }
	  attachments { nodes { title url } } } }`
	if err := c.gql(ctx, q, map[string]any{"id": id}, &data); err != nil {
		return nil, err
	}
	i := data.Issue
	if i == nil {
		return nil, fmt.Errorf("linear: issue %s not found", id)
	}
	out := &Issue{IssueSummary: i.flat(), TeamID: i.Team2.ID, Description: i.Description, Links: i.Attachments.Nodes}
	if i.Assignee != nil {
		out.Assignee, out.AssigneeID = i.Assignee.Name, i.Assignee.ID
	}
	for _, l := range i.Labels.Nodes {
		out.Labels = append(out.Labels, l.Name)
	}
	if i.Parent != nil {
		out.Parent = i.Parent.Identifier + " " + i.Parent.Title
	}
	for _, cm := range i.Comments.Nodes {
		author := "unknown"
		if cm.User != nil {
			author = cm.User.Name
		}
		out.Comments = append(out.Comments, Comment{Author: author, Body: cm.Body, CreatedAt: cm.CreatedAt})
	}
	sort.Slice(out.Comments, func(a, b int) bool { return out.Comments[a].CreatedAt < out.Comments[b].CreatedAt })
	return out, nil
}

// CreateSubIssue creates a child of parent in the same team, assigned like the parent.
func (c *Client) CreateSubIssue(ctx context.Context, parent *Issue, title, description string) (*IssueSummary, error) {
	input := map[string]any{"teamId": parent.TeamID, "parentId": parent.ID, "title": title, "description": description}
	if parent.AssigneeID != "" {
		input["assigneeId"] = parent.AssigneeID
	}
	var data struct {
		IssueCreate struct {
			Success bool        `json:"success"`
			Issue   *rawSummary `json:"issue"`
		} `json:"issueCreate"`
	}
	q := `mutation($input: IssueCreateInput!) { issueCreate(input: $input) { success issue { ` + summaryFields + ` } } }`
	if err := c.gql(ctx, q, map[string]any{"input": input}, &data); err != nil {
		return nil, err
	}
	if !data.IssueCreate.Success || data.IssueCreate.Issue == nil {
		return nil, errors.New("linear: issueCreate did not succeed")
	}
	s := data.IssueCreate.Issue.flat()
	return &s, nil
}

// TicketMarkdown renders the issue as the agent reads it (ticket.md).
func TicketMarkdown(i *Issue) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s: %s\n\n", i.Identifier, i.Title)
	fmt.Fprintf(&b, "- URL: %s\n- State: %s\n- Team: %s\n", i.URL, i.State.Name, i.Team)
	if i.Assignee != "" {
		fmt.Fprintf(&b, "- Assignee: %s\n", i.Assignee)
	}
	if len(i.Labels) > 0 {
		fmt.Fprintf(&b, "- Labels: %s\n", strings.Join(i.Labels, ", "))
	}
	if i.Parent != "" {
		fmt.Fprintf(&b, "- Parent: %s\n", i.Parent)
	}
	desc := strings.TrimSpace(i.Description)
	if desc == "" {
		desc = "_No description._"
	}
	fmt.Fprintf(&b, "\n## Description\n\n%s\n", desc)
	if len(i.Links) > 0 {
		b.WriteString("\n## Links\n\n")
		for _, l := range i.Links {
			fmt.Fprintf(&b, "- [%s](%s)\n", l.Title, l.URL)
		}
	}
	if len(i.Comments) > 0 {
		b.WriteString("\n## Comments\n")
		for _, c := range i.Comments {
			date := c.CreatedAt
			if len(date) >= 10 {
				date = date[:10]
			}
			fmt.Fprintf(&b, "\n### %s (%s)\n\n%s\n", c.Author, date, strings.TrimSpace(c.Body))
		}
	}
	return b.String()
}
