package sessions

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"time"

	"github.com/crashPopeye/layer8/internal/harness"
	"github.com/crashPopeye/layer8/internal/tasks"
	"github.com/crashPopeye/layer8/internal/tree"
)

const usageAddr tree.Addr = "usage"

// limit is a rate-limit window as a harness last reported it.
type limit struct {
	Harness string `json:"harness"`
	harness.Limit
	At string `json:"at"`
}

// sessionTokens is what one session used since a point in time.
type sessionTokens struct {
	Session tree.Addr `json:"session"`
	Task    tree.Addr `json:"task"`
	Title   string    `json:"title"`
	Harness string    `json:"harness"`
	harness.Tokens
}

// registerUsage adds the usage node: account rate-limit windows per harness and the tokens each session used today.
func (m *Manager) registerUsage(t *tree.Tree) {
	t.Register(tree.Kind{Name: "usage", Get: m.usage})
	t.AddChildren("app", func(context.Context, tree.Addr) ([]tree.Summary, error) {
		return []tree.Summary{{Addr: usageAddr, Kind: "usage", Title: "usage"}}, nil
	})
}

// setLimits merges the windows a harness reported over the ones it reported before.
func (m *Manager) setLimits(h string, limits []harness.Limit) {
	at := time.Now().UTC().Format(time.RFC3339)
	m.lmu.Lock()
	for _, l := range limits {
		m.limits[[2]string{h, l.Window}] = limit{Harness: h, Limit: l, At: at}
	}
	m.lmu.Unlock()
	m.tree.Changed(usageAddr)
}

func (m *Manager) usage(ctx context.Context, _ tree.Addr) (tree.Node, error) {
	now := time.Now()
	y, mo, d := now.Date()
	since := time.Date(y, mo, d, 0, 0, 0, 0, now.Location()).UTC().Format(time.RFC3339)
	today, err := m.tokensSince(ctx, since)
	if err != nil {
		return tree.Node{}, err
	}
	m.lmu.Lock()
	limits := slices.SortedFunc(maps.Values(m.limits), func(a, b limit) int {
		return cmp.Or(cmp.Compare(a.Harness, b.Harness), cmp.Compare(a.Window, b.Window))
	})
	m.lmu.Unlock()
	return tree.Node{Title: "usage", Fields: map[string]any{"limits": limits, "since": since, "today": today}}, nil
}

func (m *Manager) tokensSince(ctx context.Context, since string) ([]sessionTokens, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT s.id8, t.slug, s.title, s.harness,
		COALESCE(SUM(json_extract(e.data, '$.tokens.input')), 0), COALESCE(SUM(json_extract(e.data, '$.tokens.output')), 0),
		COALESCE(SUM(json_extract(e.data, '$.tokens.cacheRead')), 0), COALESCE(SUM(json_extract(e.data, '$.tokens.cacheWrite')), 0)
		FROM session_events e JOIN sessions s ON s.id = e.session_id JOIN tasks t ON t.id = s.task_id
		WHERE e.type = 'result' AND e.at >= ? AND json_extract(e.data, '$.tokens') IS NOT NULL
		GROUP BY s.id ORDER BY s.created_at, s.rowid`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []sessionTokens{}
	for rows.Next() {
		var st sessionTokens
		var id8, slug string
		if err := rows.Scan(&id8, &slug, &st.Title, &st.Harness, &st.Input, &st.Output, &st.CacheRead, &st.CacheWrite); err != nil {
			return nil, err
		}
		st.Session, st.Task = addr(id8), tasks.AddrOf(slug)
		list = append(list, st)
	}
	return list, rows.Err()
}
