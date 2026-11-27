// Package linear is the Linear GraphQL client and the linear node kind.
package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// ErrNotConnected is returned when no Linear credentials are stored.
var ErrNotConnected = errors.New("linear is not connected: use the connect action on linear")

// Credentials is the content of linear.json: an API key, or OAuth tokens.
type Credentials struct {
	APIKey       string    `json:"apiKey,omitempty"`
	AccessToken  string    `json:"accessToken,omitempty"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt,omitzero"`
}

// Client calls the Linear API with the credentials stored at Path.
type Client struct {
	Path              string
	ClientID          string
	APIEndpoint       string
	TokenEndpoint     string
	AuthorizeEndpoint string
	HTTP              *http.Client

	mu      sync.Mutex
	pending map[string]string
}

// New returns a client for Linear's production endpoints that stores credentials at path.
func New(path, clientID string) *Client {
	return &Client{
		Path:              path,
		ClientID:          clientID,
		APIEndpoint:       "https://api.linear.app/graphql",
		TokenEndpoint:     "https://api.linear.app/oauth/token",
		AuthorizeEndpoint: "https://linear.app/oauth/authorize",
		HTTP:              &http.Client{Timeout: 30 * time.Second},
	}
}

// User is a Linear user.
type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Team is a Linear team.
type Team struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

// State is an issue's workflow state; Type is backlog, unstarted, started, completed or canceled.
type State struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// IssueRef is the short form of an issue used in lists.
type IssueRef struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	State      State  `json:"state"`
}

// Comment is a comment on an issue.
type Comment struct {
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
	User      *User     `json:"user"`
}

// Attachment is a link attached to an issue.
type Attachment struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// Issue is the full detail of an issue.
type Issue struct {
	ID          string       `json:"id"`
	Identifier  string       `json:"identifier"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	URL         string       `json:"url"`
	BranchName  string       `json:"branchName"`
	Team        Team         `json:"team"`
	State       State        `json:"state"`
	Assignee    *User        `json:"assignee"`
	Labels      []string     `json:"labels"`
	Parent      *IssueRef    `json:"parent"`
	Comments    []Comment    `json:"comments"`
	Attachments []Attachment `json:"attachments"`
}

type nodes[T any] struct {
	Nodes []T `json:"nodes"`
}

// UnmarshalJSON reads the GraphQL shape, where lists are {"nodes":[…]} connections.
func (i *Issue) UnmarshalJSON(b []byte) error {
	type plain Issue
	var w struct {
		plain
		Labels      nodes[struct{ Name string }] `json:"labels"`
		Comments    nodes[Comment]               `json:"comments"`
		Attachments nodes[Attachment]            `json:"attachments"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*i = Issue(w.plain)
	for _, l := range w.Labels.Nodes {
		i.Labels = append(i.Labels, l.Name)
	}
	i.Comments = w.Comments.Nodes
	slices.SortFunc(i.Comments, func(a, b Comment) int { return a.CreatedAt.Compare(b.CreatedAt) })
	i.Attachments = w.Attachments.Nodes
	return nil
}

const issueRef = `id identifier title url state { name type }`

const (
	viewerQuery   = `query Viewer { viewer { id name email } }`
	myIssuesQuery = `query MyIssues { viewer { assignedIssues(first: 50, orderBy: updatedAt, filter: {state: {type: {nin: ["completed", "canceled"]}}}) { nodes { ` + issueRef + ` } } } }`
	searchQuery   = `query Search($term: String!) { searchIssues(term: $term, first: 20) { nodes { ` + issueRef + ` } } }`
	issueQuery    = `query Issue($id: String!) { issue(id: $id) { id identifier title description url branchName
		team { id key name } state { name type } assignee { id name email } labels { nodes { name } } parent { ` + issueRef + ` }
		comments { nodes { body createdAt user { id name email } } } attachments { nodes { title url } } } }`
	createIssueMutation = `mutation CreateIssue($input: IssueCreateInput!) { issueCreate(input: $input) { success issue { ` + issueRef + ` } } }`
	attachURLMutation   = `mutation AttachURL($issueId: String!, $url: String!, $title: String) { attachmentLinkURL(issueId: $issueId, url: $url, title: $title) { success } }`
	attachPRMutation    = `mutation AttachPR($issueId: String!, $url: String!, $title: String) { attachmentLinkGitHubPR(issueId: $issueId, url: $url, title: $title) { success } }`
)

// Connected reports whether credentials are stored.
func (c *Client) Connected() bool {
	_, err := c.load()
	return err == nil
}

// Connect checks apiKey against Linear, stores it, and returns the key's user.
func (c *Client) Connect(ctx context.Context, apiKey string) (User, error) {
	var r struct{ Viewer User }
	if err := c.post(ctx, apiKey, viewerQuery, nil, &r); err != nil {
		return User{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return r.Viewer, c.save(Credentials{APIKey: apiKey})
}

// Disconnect forgets the stored credentials.
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.Remove(c.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Viewer returns the connected user.
func (c *Client) Viewer(ctx context.Context) (User, error) {
	var r struct{ Viewer User }
	return r.Viewer, c.query(ctx, viewerQuery, nil, &r)
}

// MyIssues returns the connected user's open assigned issues, most recently updated first.
func (c *Client) MyIssues(ctx context.Context) ([]IssueRef, error) {
	var r struct {
		Viewer struct{ AssignedIssues nodes[IssueRef] }
	}
	return r.Viewer.AssignedIssues.Nodes, c.query(ctx, myIssuesQuery, nil, &r)
}

// Search returns issues matching term.
func (c *Client) Search(ctx context.Context, term string) ([]IssueRef, error) {
	var r struct{ SearchIssues nodes[IssueRef] }
	return r.SearchIssues.Nodes, c.query(ctx, searchQuery, map[string]any{"term": term}, &r)
}

// Issue returns the issue with id, a UUID or an identifier such as ENG-482.
func (c *Client) Issue(ctx context.Context, id string) (Issue, error) {
	var r struct{ Issue *Issue }
	if err := c.query(ctx, issueQuery, map[string]any{"id": id}, &r); err != nil {
		return Issue{}, err
	}
	if r.Issue == nil {
		return Issue{}, fmt.Errorf("linear issue %s not found", id)
	}
	return *r.Issue, nil
}

// CreateSubIssue creates an issue under parentID, in the parent's team.
func (c *Client) CreateSubIssue(ctx context.Context, parentID, title, description string) (IssueRef, error) {
	parent, err := c.Issue(ctx, parentID)
	if err != nil {
		return IssueRef{}, err
	}
	input := map[string]any{"teamId": parent.Team.ID, "parentId": parent.ID, "title": title}
	if description != "" {
		input["description"] = description
	}
	var r struct {
		IssueCreate struct {
			Success bool
			Issue   IssueRef
		}
	}
	if err := c.query(ctx, createIssueMutation, map[string]any{"input": input}, &r); err != nil {
		return IssueRef{}, err
	}
	if !r.IssueCreate.Success {
		return IssueRef{}, errors.New("linear: issue was not created")
	}
	return r.IssueCreate.Issue, nil
}

// AttachURL links url to the issue, as a GitHub pull request when it is one.
func (c *Client) AttachURL(ctx context.Context, issueID, url, title string) error {
	mutation, field := attachURLMutation, "attachmentLinkURL"
	if isGitHubPR(url) {
		mutation, field = attachPRMutation, "attachmentLinkGitHubPR"
	}
	var r map[string]struct{ Success bool }
	if err := c.query(ctx, mutation, map[string]any{"issueId": issueID, "url": url, "title": title}, &r); err != nil {
		return err
	}
	if !r[field].Success {
		return errors.New("linear: attachment was not created")
	}
	return nil
}

func isGitHubPR(url string) bool {
	return strings.HasPrefix(url, "https://github.com/") && strings.Contains(url, "/pull/")
}

func (c *Client) query(ctx context.Context, query string, vars map[string]any, out any) error {
	auth, err := c.authorization(ctx)
	if err != nil {
		return err
	}
	return c.post(ctx, auth, query, vars, out)
}

// authorization returns the Authorization header value, refreshing OAuth tokens that expire within a minute.
func (c *Client) authorization(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cr, err := c.load()
	if err != nil {
		return "", err
	}
	if cr.APIKey != "" {
		return cr.APIKey, nil
	}
	if !cr.ExpiresAt.IsZero() && time.Until(cr.ExpiresAt) < time.Minute && cr.RefreshToken != "" {
		if cr, err = c.refresh(ctx, cr.RefreshToken); err != nil {
			return "", err
		}
	}
	return "Bearer " + cr.AccessToken, nil
}

func (c *Client) post(ctx context.Context, auth, query string, vars map[string]any, out any) error {
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.APIEndpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", auth)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var r struct {
		Data   json.RawMessage
		Errors []struct{ Message string }
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return fmt.Errorf("linear: %s", resp.Status)
	}
	if len(r.Errors) > 0 {
		return fmt.Errorf("linear: %s", r.Errors[0].Message)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("linear: %s", resp.Status)
	}
	return json.Unmarshal(r.Data, out)
}

func (c *Client) load() (Credentials, error) {
	data, err := os.ReadFile(c.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return Credentials{}, ErrNotConnected
	}
	if err != nil {
		return Credentials{}, err
	}
	var cr Credentials
	if err := json.Unmarshal(data, &cr); err != nil {
		return Credentials{}, fmt.Errorf("%s: %w", c.Path, err)
	}
	return cr, nil
}

func (c *Client) save(cr Credentials) error {
	data, _ := json.MarshalIndent(cr, "", "  ")
	return os.WriteFile(c.Path, data, 0o600)
}
