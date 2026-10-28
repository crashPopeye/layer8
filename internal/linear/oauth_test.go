package linear

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
)

func TestOAuthFlow(t *testing.T) {
	var challenge string
	issued := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge || r.Form.Get("code") != "c1" || r.Form.Get("client_id") != "cid" {
				t.Errorf("exchange form %v", r.Form)
			}
		case "refresh_token":
			if r.Form.Get("refresh_token") != "r1" {
				t.Errorf("refresh form %v", r.Form)
			}
		}
		issued++
		// The first token is already expired, so the next use refreshes it.
		fmt.Fprintf(w, `{"access_token":"a%d","refresh_token":"r%d","expires_in":%d}`, issued, issued, (issued-1)*86400)
	}))
	defer srv.Close()

	o := NewOAuth(filepath.Join(t.TempDir(), "tok.json"))
	o.ClientID, o.TokenURL = "cid", srv.URL
	ctx := context.Background()
	if auth, err := o.Authorization(ctx); auth != "" || err != nil || o.Connected() {
		t.Fatalf("not connected yet: %q %v", auth, err)
	}
	u, err := o.AuthorizeURL("http://127.0.0.1:7878/api/linear/callback")
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.Parse(u)
	challenge = q.Query().Get("code_challenge")
	if err := o.Finish(ctx, "x", "wrong", "c1"); err == nil {
		t.Fatal("a foreign state must be refused")
	}
	if err := o.Finish(ctx, "x", q.Query().Get("state"), "c1"); err != nil {
		t.Fatal(err)
	}
	if auth, err := o.Authorization(ctx); auth != "Bearer a2" || err != nil {
		t.Fatalf("refreshed auth %q %v", auth, err)
	}
	if auth, _ := o.Authorization(ctx); auth != "Bearer a2" || issued != 2 {
		t.Fatalf("fresh token reused: %q after %d grants", auth, issued)
	}
	o.Disconnect()
	if o.Connected() {
		t.Fatal("still connected")
	}
}
