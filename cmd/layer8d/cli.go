package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/crashPopeye/layer8/internal/tree"
)

// client talks to a running daemon as the cli actor.
type client struct{ url, token string }

func (c client) call(method, path string, body, out any) error {
	var payload io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.url+path, payload)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-Layer8-Actor", "cli")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("daemon not reachable at %s (run layer8d ensure): %w", c.url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct{ Error string }
		json.NewDecoder(resp.Body).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return errors.New(e.Error)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// get prints a node as text, or as JSON with --json.
func (c client) get(args []string, w io.Writer) error {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the node as JSON")
	depth := fs.Int("depth", 1, "levels of children, 0 to 2")
	var addrs []string
	for {
		if err := fs.Parse(args); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		addrs, args = append(addrs, fs.Arg(0)), fs.Args()[1:]
	}
	addr := "app"
	switch len(addrs) {
	case 0:
	case 1:
		addr = addrs[0]
	default:
		return errors.New("get takes one address")
	}
	var raw json.RawMessage
	if err := c.call(http.MethodGet, fmt.Sprintf("/api/node?addr=%s&depth=%d", url.QueryEscape(addr), *depth), nil, &raw); err != nil {
		return err
	}
	if *asJSON {
		var b bytes.Buffer
		json.Indent(&b, raw, "", "  ")
		b.WriteByte('\n')
		_, err := b.WriteTo(w)
		return err
	}
	var n tree.Node
	if err := json.Unmarshal(raw, &n); err != nil {
		return err
	}
	_, err := io.WriteString(w, tree.Render(n))
	return err
}

// do applies one action from "addr action k=v k:=json…", or a JSON array of ops after --json.
func (c client) do(args []string, w io.Writer) error {
	ops, err := parseOps(args)
	if err != nil {
		return err
	}
	var resp struct{ Results []tree.Result }
	if err := c.call(http.MethodPost, "/api/do", map[string]any{"ops": ops}, &resp); err != nil {
		return err
	}
	io.WriteString(w, tree.RenderResults(resp.Results))
	for _, r := range resp.Results {
		if !r.OK {
			return errors.New("some actions failed")
		}
	}
	return nil
}

func parseOps(args []string) ([]tree.Op, error) {
	if len(args) == 2 && args[0] == "--json" {
		var ops []tree.Op
		return ops, json.Unmarshal([]byte(args[1]), &ops)
	}
	if len(args) < 2 {
		return nil, errors.New("usage: layer8 do <addr> <action> [k=v|k:=json]...")
	}
	values := map[string]json.RawMessage{}
	for _, kv := range args[2:] {
		if k, v, ok := strings.Cut(kv, ":="); ok && !strings.Contains(k, "=") {
			if !json.Valid([]byte(v)) {
				return nil, fmt.Errorf("%s: not valid JSON: %s", k, v)
			}
			values[k] = json.RawMessage(v)
			continue
		}
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return nil, fmt.Errorf("argument %q is not k=v or k:=json", kv)
		}
		values[k], _ = json.Marshal(v)
	}
	argsJSON, _ := json.Marshal(values)
	return []tree.Op{{Target: tree.Addr(args[0]), Action: args[1], Args: argsJSON}}, nil
}
