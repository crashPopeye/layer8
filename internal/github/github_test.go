package github

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPRStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || r.URL.Path != "/graphql" {
			t.Errorf("bad request %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		io.WriteString(w, `{"data":{"repository":{"pullRequest":{"number":5,"url":"u","state":"OPEN","isDraft":true,
		"baseRefName":"main","headRefOid":"abc","mergeable":"MERGEABLE","reviewDecision":"CHANGES_REQUESTED",
		"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"FAILURE","contexts":{"nodes":[
		  {"__typename":"CheckRun","name":"test","status":"COMPLETED","conclusion":"FAILURE","detailsUrl":"d","databaseId":99,"checkSuite":{"app":{"slug":"github-actions"}}},
		  {"__typename":"CheckRun","name":"other","status":"COMPLETED","conclusion":"SUCCESS","detailsUrl":"d2","databaseId":7,"checkSuite":{"app":{"slug":"circleci"}}},
		  {"__typename":"StatusContext","context":"ci/legacy","state":"ERROR","targetUrl":"t"}]}}}}]},
		"reviewThreads":{"nodes":[
		  {"isResolved":true,"isOutdated":false,"path":"a.go","line":1,"originalLine":1,"comments":{"nodes":[]}},
		  {"isResolved":false,"isOutdated":true,"path":"b.go","line":null,"originalLine":12,"comments":{"nodes":[{"author":null,"body":"fix","url":"cu"}]}}]}}}}}`)
	}))
	defer srv.Close()
	c := New("tok")
	c.API = srv.URL
	st, err := c.PRStatus(context.Background(), "o", "r", 5)
	if err != nil {
		t.Fatal(err)
	}
	if st.CIState != "FAILURE" || len(st.Checks) != 3 || st.Checks[0].JobID != 99 || st.Checks[1].JobID != 0 {
		t.Fatalf("checks %+v", st)
	}
	if !st.Checks[0].Failed() || st.Checks[1].Failed() || !st.Checks[2].Failed() {
		t.Fatal("failed() wrong")
	}
	if len(st.OpenThreads) != 1 || st.OpenThreads[0].Line != 12 || st.OpenThreads[0].Comments[0].Author != "ghost" {
		t.Fatalf("threads %+v", st.OpenThreads)
	}
}

func TestAPIErrorAndLogTail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/logs") {
			io.WriteString(w, "1\n2\n3\n4\n")
			return
		}
		w.WriteHeader(422)
		io.WriteString(w, `{"message":"Validation Failed","errors":[{"message":"A pull request already exists"}]}`)
	}))
	defer srv.Close()
	c := New("tok")
	c.API = srv.URL
	_, err := c.CreatePR(context.Background(), "o", "r", "h", "main", "t", "b", true)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err %v", err)
	}
	tail, err := c.JobLogTail(context.Background(), "o", "r", 1, 2)
	if err != nil || tail != "3\n4" {
		t.Fatalf("tail %q %v", tail, err)
	}
}
