package linear_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/crashPopeye/layer8/internal/linear"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/tree"
)

const apiKey = "lin_api_good"

var operation = regexp.MustCompile(`^(?:query|mutation) (\w+)`)

// fakeLinear is a GraphQL server answering by operation name and recording each call.
type fakeLinear struct {
	mu       sync.Mutex
	auths    []string
	vars     map[string]map[string]any
	calls    map[string]int
	token    string
	override map[string]string
}

func (f *fakeLinear) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Query     string
		Variables map[string]any
	}
	json.NewDecoder(r.Body).Decode(&body)
	auth := r.Header.Get("Authorization")
	f.mu.Lock()
	f.auths = append(f.auths, auth)
	op := operation.FindStringSubmatch(body.Query)[1]
	f.vars[op] = body.Variables
	f.calls[op]++
	valid := auth == apiKey || (f.token != "" && auth == "Bearer "+f.token)
	data, ok := f.override[op]
	if !ok {
		data, ok = responses[op]
	}
	f.mu.Unlock()
	if !valid {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"errors":[{"message":"Authentication required, not authenticated"}]}`))
		return
	}
	if !ok {
		http.Error(w, "unknown operation "+op, http.StatusBadRequest)
		return
	}
	w.Write([]byte(`{"data":` + data + `}`))
}

func newFake() *fakeLinear {
	return &fakeLinear{vars: map[string]map[string]any{}, calls: map[string]int{}, override: map[string]string{}}
}

func (f *fakeLinear) answer(op, data string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.override[op] = data
}

func (f *fakeLinear) count(op string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[op]
}

func (f *fakeLinear) lastAuth() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.auths[len(f.auths)-1]
}

const ref482 = `{"id":"uuid-482","identifier":"ENG-482","title":"Rate-limit uploads","url":"https://linear.app/acme/issue/ENG-482","state":{"name":"In Progress","type":"started"}}`

var responses = map[string]string{
	"Viewer":   `{"viewer":{"id":"u1","name":"Alex","email":"alex@example.com"}}`,
	"MyIssues": `{"viewer":{"assignedIssues":{"nodes":[` + ref482 + `]}}}`,
	"Search":   `{"searchIssues":{"nodes":[` + ref482 + `,{"id":"uuid-9","identifier":"ENG-9","title":"Upload limits doc","url":"u","state":{"name":"Todo","type":"unstarted"}}]}}`,
	"Issue": `{"issue":{"id":"uuid-482","identifier":"ENG-482","title":"Rate-limit uploads","description":"Uploads need a per-user limit.",
		"url":"https://linear.app/acme/issue/ENG-482","branchName":"eng-482-rate-limit-uploads",
		"team":{"id":"team-eng","key":"ENG","name":"Engineering"},"state":{"name":"In Progress","type":"started"},
		"assignee":{"id":"u1","name":"Alex","email":"alex@example.com"},"labels":{"nodes":[{"name":"backend"},{"name":"perf"}]},
		"parent":{"id":"uuid-400","identifier":"ENG-400","title":"Uploads v2","url":"https://linear.app/acme/issue/ENG-400","state":{"name":"Todo","type":"unstarted"}},
		"comments":{"nodes":[{"body":"Second","createdAt":"2026-11-21T10:00:00Z","user":{"name":"Sam"}},{"body":"First","createdAt":"2026-11-20T09:00:00Z","user":null}]},
		"attachments":{"nodes":[{"title":"Spec","url":"https://example.com/spec"}]}}}`,
	"CreateIssue": `{"issueCreate":{"success":true,"issue":{"id":"uuid-500","identifier":"ENG-500","title":"pr1-store","url":"u","state":{"name":"Todo","type":"unstarted"}}}}`,
	"AttachURL":   `{"attachmentLinkURL":{"success":true}}`,
	"AttachPR":    `{"attachmentLinkGitHubPR":{"success":true}}`,
}

func setup(t *testing.T, clientID string) (*tree.Tree, *linear.Client, *fakeLinear, http.Handler) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	tr := tree.New(db)
	tr.Register(tree.Kind{Name: "app", Get: func(context.Context, tree.Addr) (tree.Node, error) { return tree.Node{Title: "layer8"}, nil }})
	fake := newFake()
	api := httptest.NewServer(fake)
	t.Cleanup(api.Close)
	c := linear.New(filepath.Join(t.TempDir(), "linear.json"), clientID)
	c.APIEndpoint = api.URL
	callback := linear.Register(tr, c, "http://127.0.0.1:7979/api/linear/callback")
	return tr, c, fake, callback
}

var user = tree.Actor{Kind: "user"}

func do(t *testing.T, tr *tree.Tree, target tree.Addr, action, args string) tree.Result {
	t.Helper()
	return tr.Do(context.Background(), user, []tree.Op{{Target: target, Action: action, Args: json.RawMessage(args)}})[0]
}

func get(t *testing.T, tr *tree.Tree, addr tree.Addr) tree.Node {
	t.Helper()
	n, err := tr.Get(context.Background(), user, addr, 1)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func actionNames(n tree.Node) string {
	var names []string
	for _, a := range n.Actions {
		names = append(names, a.Name)
	}
	return strings.Join(names, ",")
}

func TestAPIKeyFlow(t *testing.T) {
	tr, c, fake, _ := setup(t, "")

	if app := get(t, tr, "app"); len(app.Children) != 1 || app.Children[0].Addr != "linear" {
		t.Fatalf("app children = %+v", app.Children)
	}
	root := get(t, tr, "linear")
	if root.Fields["connected"] != false || actionNames(root) != "connect" || len(root.Children) != 0 {
		t.Fatalf("disconnected root = %+v", root)
	}

	if r := do(t, tr, "linear", "connect", `{"apiKey":"wrong"}`); r.OK || !strings.Contains(r.Message, "not authenticated") {
		t.Fatalf("bad key = %+v", r)
	}
	if c.Connected() {
		t.Fatal("bad key must not be stored")
	}
	if r := do(t, tr, "linear", "connect", `{"apiKey":"`+apiKey+`"}`); !r.OK || r.Message != "connected to Linear as Alex (alex@example.com)" {
		t.Fatalf("connect = %+v", r)
	}
	if info, err := os.Stat(c.Path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("linear.json: %v %v", info, err)
	}
	if fake.lastAuth() != apiKey {
		t.Fatalf("api key must be sent raw, got %q", fake.lastAuth())
	}

	root = get(t, tr, "linear:")
	if root.Fields["connected"] != true || root.Fields["viewer"].(linear.User).Name != "Alex" || actionNames(root) != "disconnect,search" {
		t.Fatalf("connected root = %+v", root)
	}
	if len(root.Children) != 1 || !reflect.DeepEqual(root.Children[0], tree.Summary{Addr: "linear:ENG-482", Kind: "linear", Title: "ENG-482 Rate-limit uploads", Status: "In Progress"}) {
		t.Fatalf("my issues = %+v", root.Children)
	}

	r := do(t, tr, "linear", "search", `{"query":"upload"}`)
	if hits := r.Data.([]tree.Summary); !r.OK || len(hits) != 2 || hits[1].Addr != "linear:ENG-9" || fake.vars["Search"]["term"] != "upload" {
		t.Fatalf("search = %+v", r)
	}

	issue := get(t, tr, "linear:ENG-482")
	f := issue.Fields
	if issue.Title != "ENG-482 Rate-limit uploads" || f["branchName"] != "eng-482-rate-limit-uploads" || fake.vars["Issue"]["id"] != "ENG-482" ||
		f["team"].(map[string]any)["key"] != "ENG" || len(f["labels"].([]any)) != 2 || f["parent"].(map[string]any)["identifier"] != "ENG-400" ||
		f["comments"].([]any)[0].(map[string]any)["body"] != "First" || len(f["attachments"].([]any)) != 1 ||
		!strings.HasPrefix(f["markdown"].(string), "# ENG-482 Rate-limit uploads") {
		t.Fatalf("issue fields = %v", f)
	}
	if len(issue.Actions) != 0 {
		t.Fatalf("issue actions = %s", actionNames(issue))
	}

	if r := do(t, tr, "linear", "disconnect", ""); !r.OK || c.Connected() {
		t.Fatalf("disconnect = %+v", r)
	}
}

func TestSubIssueAndAttach(t *testing.T) {
	_, c, fake, _ := setup(t, "")
	ctx := context.Background()
	if _, err := c.Search(ctx, "x"); err != linear.ErrNotConnected {
		t.Fatalf("err = %v, want ErrNotConnected", err)
	}
	if _, err := c.Connect(ctx, apiKey); err != nil {
		t.Fatal(err)
	}

	sub, err := c.CreateSubIssue(ctx, "ENG-482", "pr1-store", "First PR")
	if err != nil || sub.Identifier != "ENG-500" {
		t.Fatalf("sub-issue = %+v, %v", sub, err)
	}
	input := fake.vars["CreateIssue"]["input"].(map[string]any)
	if input["teamId"] != "team-eng" || input["parentId"] != "uuid-482" || input["title"] != "pr1-store" || input["description"] != "First PR" {
		t.Fatalf("issueCreate input = %v", input)
	}

	if err := c.AttachURL(ctx, "uuid-482", "https://github.com/acme/api/pull/7", "PR #7"); err != nil {
		t.Fatal(err)
	}
	if fake.vars["AttachPR"]["url"] != "https://github.com/acme/api/pull/7" || fake.vars["AttachURL"] != nil {
		t.Fatalf("GitHub PR must use attachmentLinkGitHubPR: %v", fake.vars)
	}
	if err := c.AttachURL(ctx, "uuid-482", "https://example.com/doc", "Doc"); err != nil {
		t.Fatal(err)
	}
	if fake.vars["AttachURL"]["title"] != "Doc" {
		t.Fatalf("attachmentLinkURL vars = %v", fake.vars["AttachURL"])
	}
}

// fakeTokens is an OAuth token endpoint that checks PKCE and issues numbered tokens.
type fakeTokens struct {
	challenge string
	forms     []url.Values
	expiresIn []int
	api       *fakeLinear
}

func (f *fakeTokens) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	f.forms = append(f.forms, r.PostForm)
	n := len(f.forms)
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if r.PostForm.Get("code") != "the-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
	case "refresh_token":
		if r.PostForm.Get("refresh_token") != "refresh-1" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}
	f.api.mu.Lock()
	f.api.token = "access-" + string(rune('0'+n))
	f.api.mu.Unlock()
	json.NewEncoder(w).Encode(map[string]any{"access_token": f.api.token, "refresh_token": "refresh-1", "expires_in": f.expiresIn[n-1], "token_type": "Bearer"})
}

func TestOAuthPKCE(t *testing.T) {
	tr, c, fake, callback := setup(t, "client-123")
	tokens := &fakeTokens{api: fake, expiresIn: []int{30, 86400}}
	ts := httptest.NewServer(tokens)
	defer ts.Close()
	c.TokenEndpoint = ts.URL

	if names := actionNames(get(t, tr, "linear")); names != "connect,connect-oauth" {
		t.Fatalf("actions = %s", names)
	}
	r := do(t, tr, "linear", "connect-oauth", "")
	u, err := url.Parse(r.Data.(map[string]string)["url"])
	if !r.OK || err != nil || !strings.HasPrefix(u.String(), "https://linear.app/oauth/authorize?") {
		t.Fatalf("connect-oauth = %+v", r)
	}
	q := u.Query()
	if q.Get("client_id") != "client-123" || q.Get("redirect_uri") != "http://127.0.0.1:7979/api/linear/callback" ||
		q.Get("code_challenge_method") != "S256" || q.Get("response_type") != "code" {
		t.Fatalf("authorize query = %v", q)
	}
	tokens.challenge = q.Get("code_challenge")

	call := func(query string) int {
		w := httptest.NewRecorder()
		callback.ServeHTTP(w, httptest.NewRequest("GET", "/api/linear/callback?"+query, nil))
		return w.Code
	}
	if code := call("state=forged&code=the-code"); code != http.StatusBadRequest {
		t.Fatalf("forged state = %d", code)
	}
	if code := call("state=" + q.Get("state") + "&code=the-code"); code != http.StatusOK {
		t.Fatalf("callback = %d", code)
	}
	if code := call("state=" + q.Get("state") + "&code=the-code"); code != http.StatusBadRequest {
		t.Fatalf("replayed state = %d", code)
	}
	exchange := tokens.forms[0]
	if exchange.Get("client_id") != "client-123" || exchange.Get("redirect_uri") != q.Get("redirect_uri") || exchange.Has("client_secret") {
		t.Fatalf("exchange form = %v", exchange)
	}

	if _, err := c.Viewer(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(tokens.forms) != 2 || tokens.forms[1].Get("grant_type") != "refresh_token" || fake.lastAuth() != "Bearer access-2" {
		t.Fatalf("token expiring within a minute must refresh: forms=%v auth=%q", tokens.forms, fake.lastAuth())
	}
	if _, err := c.Viewer(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(tokens.forms) != 2 || fake.lastAuth() != "Bearer access-2" {
		t.Fatalf("fresh token must not refresh: forms=%d auth=%q", len(tokens.forms), fake.lastAuth())
	}
	if names := actionNames(get(t, tr, "linear")); names != "disconnect,search" {
		t.Fatalf("actions after sign-in = %s", names)
	}
}

func TestTicketMarkdown(t *testing.T) {
	var i linear.Issue
	data := responses["Issue"]
	json.Unmarshal([]byte(data[len(`{"issue":`):len(data)-1]), &i)
	got := linear.TicketMarkdown(i)
	want := "# ENG-482 Rate-limit uploads\n\n" +
		"- **Linear:** [ENG-482](https://linear.app/acme/issue/ENG-482)\n" +
		"- **Team:** Engineering\n" +
		"- **State:** In Progress\n" +
		"- **Assignee:** Alex\n" +
		"- **Labels:** backend, perf\n" +
		"- **Parent:** [ENG-400](https://linear.app/acme/issue/ENG-400) Uploads v2\n" +
		"- **Branch:** `eng-482-rate-limit-uploads`\n" +
		"\n## Description\n\nUploads need a per-user limit.\n" +
		"\n## Attachments\n\n- [Spec](https://example.com/spec)\n" +
		"\n## Comments\n" +
		"\n**Unknown** · 2026-11-20 09:00\n\nFirst\n" +
		"\n**Sam** · 2026-11-21 10:00\n\nSecond\n"
	if got != want {
		t.Fatalf("markdown:\n%s\nwant:\n%s", got, want)
	}
}
