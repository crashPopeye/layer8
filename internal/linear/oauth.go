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
	"strings"
	"time"
)

// StartOAuth begins a PKCE sign-in and returns the Linear consent URL, which sends the user back to redirect.
func (c *Client) StartOAuth(redirect string) (string, error) {
	if c.ClientID == "" {
		return "", errors.New("set linearClientId in config.json to sign in with Linear")
	}
	verifier, state := random(), random()
	c.mu.Lock()
	if c.pending == nil {
		c.pending = map[string]string{}
	}
	c.pending[state] = verifier
	c.mu.Unlock()
	challenge := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"client_id":             {c.ClientID},
		"redirect_uri":          {redirect},
		"response_type":         {"code"},
		"scope":                 {"read,write"},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
	}
	return c.AuthorizeEndpoint + "?" + q.Encode(), nil
}

// FinishOAuth exchanges the code Linear returned with state for tokens, and stores them.
func (c *Client) FinishOAuth(ctx context.Context, state, code, redirect string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	verifier, ok := c.pending[state]
	if !ok {
		return errors.New("unknown or already used sign-in state: start again with connect-oauth")
	}
	delete(c.pending, state)
	cr, err := c.token(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirect},
		"code_verifier": {verifier},
	})
	if err != nil {
		return err
	}
	return c.save(cr)
}

func (c *Client) refresh(ctx context.Context, refreshToken string) (Credentials, error) {
	cr, err := c.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}})
	if err != nil {
		return Credentials{}, fmt.Errorf("refreshing the Linear token: %w", err)
	}
	if cr.RefreshToken == "" {
		cr.RefreshToken = refreshToken
	}
	return cr, c.save(cr)
}

func (c *Client) token(ctx context.Context, form url.Values) (Credentials, error) {
	form.Set("client_id", c.ClientID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Credentials{}, err
	}
	defer resp.Body.Close()
	var r struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		ExpiresIn        int    `json:"expires_in"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	json.NewDecoder(resp.Body).Decode(&r)
	if resp.StatusCode != http.StatusOK || r.AccessToken == "" {
		return Credentials{}, fmt.Errorf("linear token endpoint: %s %s %s", resp.Status, r.Error, r.ErrorDescription)
	}
	cr := Credentials{AccessToken: r.AccessToken, RefreshToken: r.RefreshToken}
	if r.ExpiresIn > 0 {
		cr.ExpiresAt = time.Now().Add(time.Duration(r.ExpiresIn) * time.Second)
	}
	return cr, nil
}

// random returns 43 URL-safe characters, a valid PKCE verifier.
func random() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
