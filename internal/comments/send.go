package comments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/crashPopeye/layer8/internal/tree"
)

func (s *Service) send(ctx context.Context, n tree.Node, args json.RawMessage, by tree.Actor) (tree.Result, error) {
	var a struct{ Session string }
	json.Unmarshal(args, &a)
	session := liveSession(n.Children)
	if a.Session != "" {
		session = tree.Addr("session:" + strings.TrimPrefix(a.Session, "session:"))
	}
	if session == "" {
		return tree.Result{}, fmt.Errorf("%s has no live session to send comments to", n.Addr)
	}
	all, _ := n.Fields["comments"].([]Comment)
	open := slices.DeleteFunc(slices.Clone(all), func(c Comment) bool { return c.Status != "open" })
	artifacts, _ := n.Fields["artifacts"].(string)
	text, _ := json.Marshal(map[string]string{"text": Format(open, artifacts)})
	if res := s.tree.Do(ctx, by, []tree.Op{{Target: session, Action: "say", Args: text}})[0]; !res.OK {
		return tree.Result{}, errors.New(res.Message)
	}
	ids := make([]any, len(open))
	for i, c := range open {
		ids[i] = c.Addr.Path()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE comments SET status = 'sent', sent_to = ? WHERE id8 IN (?`+strings.Repeat(", ?", len(ids)-1)+`)`,
		append([]any{string(session)}, ids...)...)
	if err != nil {
		return tree.Result{}, err
	}
	s.changed(ctx, open...)
	return tree.Result{Addr: session, Message: fmt.Sprintf("sent %d comments to %s", len(open), session), Data: map[string]any{"session": session, "sent": len(open)}}, nil
}

// liveSession returns the last session among children that is neither done nor failed.
func liveSession(children []tree.Summary) tree.Addr {
	for _, c := range slices.Backward(children) {
		if c.Kind == "session" && c.Status != "done" && c.Status != "failed" {
			return c.Addr
		}
	}
	return ""
}

// Format writes comments as one message asking the agent to address and resolve each,
// with artifact paths made absolute under the artifacts folder.
func Format(list []Comment, artifacts string) string {
	var b strings.Builder
	noun := "comments"
	if len(list) == 1 {
		noun = "comment"
	}
	fmt.Fprintf(&b, "I left %d review %s in layer8. Address each one (the layer8:review-comments skill walks through it), then resolve it with the layer8 `do` tool, for example "+
		"{\"ops\":[{\"target\":\"%s\",\"action\":\"resolve\",\"args\":{\"reply\":\"what changed\"}}]}.\n", len(list), noun, list[0].Addr)
	for i, c := range list {
		fmt.Fprintf(&b, "\n%d. %s on %s\n", i+1, c.Addr, location(c, artifacts))
		if q := strings.TrimSpace(c.Quote); q != "" {
			b.WriteString("   > " + strings.ReplaceAll(q, "\n", "\n   > ") + "\n")
		}
		b.WriteString("   " + strings.ReplaceAll(c.Body, "\n", "\n   ") + "\n")
	}
	return b.String()
}

func location(c Comment, artifacts string) string {
	where := c.Path
	if c.Target == "artifact" {
		where = filepath.Join(artifacts, c.Path)
	}
	switch {
	case c.Line == 0:
	case c.LineEnd > c.Line:
		where += fmt.Sprintf(" lines %d-%d", c.Line, c.LineEnd)
	default:
		where += fmt.Sprintf(" line %d", c.Line)
	}
	switch {
	case c.Target == "diff" && c.Side == "old":
		where += " (diff, base version)"
	case c.Target == "diff":
		where += " (diff)"
	}
	return where
}
