package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/crashPopeye/layer8/internal/config"
)

// Info is how clients reach the daemon.
type Info struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

func LocalInfo() (Info, error) {
	paths, err := config.ResolvePaths()
	if err != nil {
		return Info{}, err
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return Info{}, err
	}
	tok, err := config.Token(paths)
	if err != nil {
		return Info{}, err
	}
	return Info{URL: fmt.Sprintf("http://127.0.0.1:%d", cfg.Get().Port), Token: tok}, nil
}

type Client struct {
	Info
	HTTP *http.Client
}

func NewClient(i Info) *Client {
	return &Client{Info: i, HTTP: &http.Client{Timeout: 5 * time.Minute}}
}

var ErrUnreachable = errors.New("layer8d is not running")

// Do calls the API; out may be nil.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.URL+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%w (%v)", ErrUnreachable, err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(res.Body).Decode(&e)
		if e.Error == "" {
			e.Error = res.Status
		}
		return errors.New(e.Error)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func (c *Client) Version(ctx context.Context) (string, error) {
	var v struct {
		Version string `json:"version"`
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err := c.Do(ctx, http.MethodGet, "/api/version", nil, &v)
	return v.Version, err
}

// Events streams server-sent events until ctx ends.
func (c *Client) Events(ctx context.Context, fn func(name string, data json.RawMessage)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL+"/api/events", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	res, err := (&http.Client{}).Do(req)
	if err != nil {
		return fmt.Errorf("%w (%v)", ErrUnreachable, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("events: %s", res.Status)
	}
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 64*1024), 64*1024*1024)
	var name string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			fn(name, json.RawMessage(strings.TrimPrefix(line, "data: ")))
		case line == "":
			name = ""
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	return sc.Err()
}
