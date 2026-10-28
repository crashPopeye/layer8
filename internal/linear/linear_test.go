package linear

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fake(t *testing.T, handler func(query string, vars map[string]any) string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "lin_api_x" {
			t.Errorf("auth header = %q", r.Header.Get("Authorization"))
		}
		b, _ := io.ReadAll(r.Body)
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		json.Unmarshal(b, &req)
		io.WriteString(w, handler(req.Query, req.Variables))
	}))
	t.Cleanup(srv.Close)
	c := New("lin_api_x")
	c.Endpoint = srv.URL
	return c
}

func TestIssueAndTicket(t *testing.T) {
	c := fake(t, func(q string, v map[string]any) string {
		if v["id"] != "ENG-1" {
			t.Errorf("vars %v", v)
		}
		return `{"data":{"issue":{"id":"u1","identifier":"ENG-1","title":"Thing","url":"https://linear.app/x/issue/ENG-1",
		"branchName":"alex/eng-1-thing","priority":2,"updatedAt":"2026-01-01","state":{"name":"Todo","type":"unstarted","color":"#fff"},
		"team":{"key":"ENG"},"teamRef":{"id":"t1"},"description":"Do it","assignee":{"id":"a1","name":"Alex"},
		"labels":{"nodes":[{"name":"bug"}]},"parent":null,
		"comments":{"nodes":[{"body":"second","createdAt":"2026-01-03T00:00:00Z","user":{"name":"B"}},{"body":"first","createdAt":"2026-01-02T00:00:00Z","user":null}]},
		"attachments":{"nodes":[{"title":"PR","url":"https://github.com/x/y/pull/1"}]}}}}`
	})
	i, err := c.Issue(context.Background(), "ENG-1")
	if err != nil {
		t.Fatal(err)
	}
	if i.TeamID != "t1" || i.AssigneeID != "a1" || i.Team != "ENG" || i.BranchName != "alex/eng-1-thing" {
		t.Fatalf("issue %+v", i)
	}
	md := TicketMarkdown(i)
	for _, want := range []string{"# ENG-1: Thing", "## Description\n\nDo it", "- Labels: bug", "[PR](https://github.com/x/y/pull/1)"} {
		if !strings.Contains(md, want) {
			t.Errorf("ticket missing %q:\n%s", want, md)
		}
	}
	if strings.Index(md, "first") > strings.Index(md, "second") {
		t.Error("comments not in chronological order")
	}
	if !strings.Contains(md, "### unknown (2026-01-02)") {
		t.Error("missing author fallback")
	}
}

func TestErrors(t *testing.T) {
	c := fake(t, func(string, map[string]any) string { return `{"errors":[{"message":"bad key"}]}` })
	if _, err := c.MyIssues(context.Background()); err == nil || !strings.Contains(err.Error(), "bad key") {
		t.Fatalf("err = %v", err)
	}
	if _, err := New("").MyIssues(context.Background()); err == nil {
		t.Fatal("missing key should fail")
	}
}

func TestCreateSubIssue(t *testing.T) {
	c := fake(t, func(q string, v map[string]any) string {
		in := v["input"].(map[string]any)
		if in["parentId"] != "u1" || in["teamId"] != "t1" || in["assigneeId"] != "a1" {
			t.Errorf("input %v", in)
		}
		return `{"data":{"issueCreate":{"success":true,"issue":{"id":"u2","identifier":"ENG-2","title":"Part","url":"u","branchName":"alex/eng-2-part","priority":0,"updatedAt":"","state":{"name":"Todo","type":"unstarted","color":""},"team":{"key":"ENG"}}}}}`
	})
	s, err := c.CreateSubIssue(context.Background(), &Issue{IssueSummary: IssueSummary{ID: "u1"}, TeamID: "t1", AssigneeID: "a1"}, "Part", "")
	if err != nil || s.Identifier != "ENG-2" || s.BranchName != "alex/eng-2-part" {
		t.Fatalf("%v %+v", err, s)
	}
}
