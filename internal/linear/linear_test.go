package linear

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// fake returns a client of a GraphQL server that checks the API key header and
// answers each operation with reply(query, variables).
func fake(t *testing.T, reply func(query string, vars map[string]any) string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "lin_api_x" {
			t.Errorf("Authorization = %q, want the bare key lin_api_x", got)
		}
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("request body: %v", err)
		}
		io.WriteString(w, reply(req.Query, req.Variables))
	}))
	t.Cleanup(srv.Close)
	return NewAt(srv.URL, "lin_api_x")
}

const flow1297 = `{"data":{"issue":{"id":"u-1297","identifier":"FLOW-1297","title":"Media ingest retry",
  "url":"https://linear.app/scoreplay/issue/FLOW-1297","branchName":"alex/flow-1297-media-ingest-retry",
  "state":{"name":"In Progress"},"team":{"key":"FLOW"},
  "description":"Retry failed ingests with backoff.\n\nKeep the last error on the asset.",
  "assignee":{"name":"Alex"},"project":{"name":"Media pipeline"},"parent":{"identifier":"FLOW-1200","title":"Media ingest"},
  "labels":{"nodes":[{"name":"backend"},{"name":"reliability"}]},
  "children":{"nodes":[
    {"id":"u-1298","identifier":"FLOW-1298","title":"Retry queue","url":"https://linear.app/scoreplay/issue/FLOW-1298","branchName":"alex/flow-1298-retry-queue","state":{"name":"Todo"},"team":{"key":"FLOW"}},
    {"id":"u-1299","identifier":"FLOW-1299","title":"Backoff policy","url":"https://linear.app/scoreplay/issue/FLOW-1299","branchName":"alex/flow-1299-backoff-policy","state":{"name":"Backlog"},"team":{"key":"FLOW"}}]},
  "attachments":{"nodes":[{"title":"Incident 2026-11-18","url":"https://notion.so/incident-1118"}]},
  "comments":{"nodes":[
    {"body":"Cap retries at 5.","createdAt":"2026-11-21T09:30:00.000Z","user":{"name":"Marie"}},
    {"body":"Seen on 3 clubs this week.","createdAt":"2026-11-20T14:02:00.000Z","user":null}]}}}}`

func TestIssueAndTicketMarkdown(t *testing.T) {
	c := fake(t, func(q string, v map[string]any) string {
		if v["id"] != "FLOW-1297" || !strings.Contains(q, "children(first: 50)") {
			t.Errorf("query %q with %v", q, v)
		}
		return flow1297
	})
	i, err := c.Issue(context.Background(), "FLOW-1297")
	if err != nil {
		t.Fatal(err)
	}
	if i.BranchName != "alex/flow-1297-media-ingest-retry" || i.State != "In Progress" || i.Team != "FLOW" || i.Parent != "FLOW-1200 Media ingest" {
		t.Fatalf("issue = %+v", i.Summary)
	}
	if len(i.Children) != 2 || i.Children[1].Identifier != "FLOW-1299" || i.Children[1].BranchName != "alex/flow-1299-backoff-policy" {
		t.Fatalf("children = %+v", i.Children)
	}
	if len(i.Comments) != 2 || i.Comments[0].Author != "unknown" {
		t.Fatalf("comments are not oldest first with a fallback author: %+v", i.Comments)
	}

	got := TicketMarkdown(i)
	golden := filepath.Join("testdata", "ticket.golden.md")
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("ticket.md\n%s\nwant\n%s", got, want)
	}
}

func TestFind(t *testing.T) {
	c := fake(t, func(q string, v map[string]any) string {
		switch {
		case strings.Contains(q, "assignedIssues"):
			return `{"data":{"viewer":{"assignedIssues":{"nodes":[{"identifier":"FLOW-1"}]}}}}`
		case strings.Contains(q, "searchIssues"):
			if v["term"] != "ingest retry" {
				t.Errorf("term = %v", v["term"])
			}
			return `{"data":{"searchIssues":{"nodes":[{"identifier":"FLOW-2"},{"identifier":"FLOW-3"}]}}}`
		case strings.Contains(q, "issue(id"):
			if v["id"] != "FLOW-1297" {
				return `{"errors":[{"message":"Entity not found"}]}`
			}
			return flow1297
		}
		t.Errorf("unexpected query %q", q)
		return `{}`
	})
	ctx := context.Background()
	for q, want := range map[string][]string{
		"":             {"FLOW-1"},
		"flow-1297":    {"FLOW-1297"},
		"ingest retry": {"FLOW-2", "FLOW-3"},
	} {
		got, err := c.Find(ctx, q)
		if err != nil {
			t.Fatalf("Find(%q): %v", q, err)
		}
		ids := make([]string, len(got))
		for i, s := range got {
			ids[i] = s.Identifier
		}
		if strings.Join(ids, ",") != strings.Join(want, ",") {
			t.Errorf("Find(%q) = %v, want %v", q, ids, want)
		}
	}
}

func TestSetState(t *testing.T) {
	var updated map[string]any
	c := fake(t, func(q string, v map[string]any) string {
		if strings.Contains(q, "issueUpdate") {
			updated = v
			return `{"data":{"issueUpdate":{"success":true}}}`
		}
		return `{"data":{"issue":{"id":"u-1297","team":{"states":{"nodes":[{"id":"s-todo","name":"Todo"},{"id":"s-review","name":"In Review"}]}}}}}`
	})
	if err := c.SetState(context.Background(), "FLOW-1297", "In Review"); err != nil {
		t.Fatal(err)
	}
	if updated["id"] != "u-1297" || updated["state"] != "s-review" {
		t.Fatalf("issueUpdate variables = %v", updated)
	}
	if err := c.SetState(context.Background(), "FLOW-1297", "Shipped"); err == nil {
		t.Fatal("moved to a state the team does not have")
	}
}

func TestErrors(t *testing.T) {
	c := fake(t, func(string, map[string]any) string { return `{"errors":[{"message":"Authentication required"}]}` })
	if _, err := c.MyIssues(context.Background()); err == nil || !strings.Contains(err.Error(), "Authentication required") {
		t.Fatalf("err = %v", err)
	}
	if New("") != nil || New("  ") != nil {
		t.Fatal("an empty key made a client")
	}
}
