package linear

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// ClientID is layer8's public Linear OAuth application. PKCE replaces its secret, so
// anyone can connect their own workspace without creating an application.
const ClientID = "e95dee155b156088fd77ec7340193b48"

const (
	authorizeURL = "https://linear.app/oauth/authorize"
	tokenURL     = "https://api.linear.app/oauth/token"
)

type Token struct {
	Access  string    `json:"accessToken"`
	Refresh string    `json:"refreshToken"`
	Expiry  time.Time `json:"expiry"`
}

// OAuth signs in to Linear with the authorization-code flow and PKCE (no client
// secret), and keeps the token in a file, refreshing it before it expires.
type OAuth struct {
	Path     string
	ClientID string // tests override
	TokenURL string // tests override
	HTTP     *http.Client

	mu       sync.Mutex
	state    string // of the sign-in in progress
	verifier string
}

func NewOAuth(path string) *OAuth {
	return &OAuth{Path: path, ClientID: ClientID, TokenURL: tokenURL, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func randomString() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// AuthorizeURL starts a sign-in; it replaces any sign-in still in progress.
func (o *OAuth) AuthorizeURL(redirect string) (string, error) {
	if o.ClientID == "" {
		return "", errors.New("this build of layer8 has no Linear OAuth application; use an API key")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.state, o.verifier = randomString(), randomString()
	sum := sha256.Sum256([]byte(o.verifier))
	q := url.Values{
		"client_id":             {o.ClientID},
		"redirect_uri":          {redirect},
		"response_type":         {"code"},
		"scope":                 {"read,write"},
		"state":                 {o.state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"prompt":                {"consent"},
	}
	return authorizeURL + "?" + q.Encode(), nil
}

// Finish exchanges the code Linear redirected back with, if state matches the sign-in in progress.
func (o *OAuth) Finish(ctx context.Context, redirect, state, code string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.state == "" || state != o.state {
		return errors.New("this sign-in link is stale; start again from layer8")
	}
	verifier := o.verifier
	o.state, o.verifier = "", ""
	tok, err := o.grant(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirect},
		"client_id":     {o.ClientID},
		"code_verifier": {verifier},
	})
	if err != nil {
		return err
	}
	return o.save(tok)
}

// Authorization returns the Authorization header value, refreshing the token when it
// is about to expire, or "" when Linear is not connected.
func (o *OAuth) Authorization(ctx context.Context) (string, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	tok, err := o.load()
	if err != nil || tok == nil {
		return "", err
	}
	if time.Until(tok.Expiry) < time.Minute {
		fresh, err := o.grant(ctx, url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {tok.Refresh},
			"client_id":     {o.ClientID},
		})
		if err != nil {
			return "", fmt.Errorf("refresh Linear sign-in (connect again in Settings): %w", err)
		}
		if err := o.save(fresh); err != nil {
			return "", err
		}
		tok = fresh
	}
	return "Bearer " + tok.Access, nil
}

func (o *OAuth) Connected() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	tok, _ := o.load()
	return tok != nil
}

func (o *OAuth) Disconnect() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := os.Remove(o.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (o *OAuth) grant(ctx context.Context, form url.Values) (*Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := o.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("linear: %w", err)
	}
	defer res.Body.Close()
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		Description  string `json:"error_description"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("linear: HTTP %d: %w", res.StatusCode, err)
	}
	if res.StatusCode != http.StatusOK || body.AccessToken == "" {
		return nil, fmt.Errorf("linear: HTTP %d: %s %s", res.StatusCode, body.Error, body.Description)
	}
	return &Token{
		Access:  body.AccessToken,
		Refresh: body.RefreshToken,
		Expiry:  time.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
	}, nil
}

func (o *OAuth) load() (*Token, error) {
	b, err := os.ReadFile(o.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var tok Token
	if err := json.Unmarshal(b, &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

func (o *OAuth) save(tok *Token) error {
	b, err := json.Marshal(tok)
	if err != nil {
		return err
	}
	tmp := o.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, o.Path)
}
