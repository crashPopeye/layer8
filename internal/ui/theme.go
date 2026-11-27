package ui

import (
	"bytes"
	"cmp"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/crashPopeye/layer8/internal/tree"
)

// Theme is a set of shadcn CSS token overrides, such as "primary" or "radius", on top of the window's
// default stylesheet: Shared applies in both colour schemes, Dark and Light in one each.
type Theme struct {
	Shared map[string]string `json:"shared,omitempty"`
	Dark   map[string]string `json:"dark,omitempty"`
	Light  map[string]string `json:"light,omitempty"`
}

//go:embed themes.json
var builtinJSON []byte

// DefaultTheme is the theme in use until another is set.
const DefaultTheme = "layer8"

var (
	tokenPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{3,8}$`)
)

func builtinThemes() map[string]Theme {
	var m map[string]Theme
	if err := json.Unmarshal(builtinJSON, &m); err != nil {
		panic(err)
	}
	return m
}

func parseTheme(data []byte) (Theme, error) {
	var t Theme
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return t, err
	}
	for _, tokens := range []map[string]string{t.Shared, t.Dark, t.Light} {
		for k, v := range tokens {
			if !tokenPattern.MatchString(k) || v == "" || strings.ContainsAny(v, ";{}") {
				return t, fmt.Errorf("token %q: %q is not a CSS token name and value", k, v)
			}
		}
	}
	return t, nil
}

func (s *Service) themes() (map[string]Theme, map[string]string) {
	all, errs := builtinThemes(), map[string]string{}
	paths, _ := filepath.Glob(s.path("themes", "*.json"))
	for _, p := range paths {
		name := strings.TrimSuffix(filepath.Base(p), ".json")
		data, err := os.ReadFile(p)
		var t Theme
		if err == nil {
			t, err = parseTheme(data)
		}
		if err != nil {
			errs[name] = err.Error()
			continue
		}
		if _, builtin := all[name]; !builtin {
			all[name] = t
		}
	}
	return all, errs
}

func (s *Service) themeFields(st state) (map[string]any, error) {
	all, errs := s.themes()
	fields := map[string]any{
		"theme":   cmp.Or(st.Theme, DefaultTheme),
		"mode":    cmp.Or(st.Mode, "dark"),
		"accent":  st.Accent,
		"themes":  all,
		"builtin": slices.Sorted(maps.Keys(builtinThemes())),
		"dir":     s.path("themes"),
	}
	if len(errs) > 0 {
		fields["errors"] = errs
	}
	return fields, nil
}

func (s *Service) themeActions() []tree.Action {
	when := on("theme")
	return []tree.Action{
		{Name: "set", Help: "Switch the theme or the colour mode", When: when, Run: s.setTheme,
			Args: json.RawMessage(`{"type":"object","properties":{
				"theme":{"type":"string","description":"One of the themes field"},
				"mode":{"enum":["dark","light","system"]}}}`)},
		{Name: "set-accent", Help: "Override the theme's primary colour, or clear the override with an empty color", When: when, Run: s.setAccent,
			Args: json.RawMessage(`{"type":"object","required":["color"],"properties":{"color":{"type":"string","description":"Hex colour such as #a78bfa"}}}`)},
		{Name: "save", Help: "Save a theme to the themes folder and switch to it", When: when, Run: s.saveTheme,
			Args: json.RawMessage(`{"type":"object","required":["name","tokens"],"properties":{
				"name":{"type":"string"},
				"tokens":{"type":"object","description":"{shared?, dark?, light?}, each a map of token name (background, primary, radius, sans…) to CSS value"}}}`)},
	}
}

func (s *Service) setTheme(_ context.Context, _ tree.Node, args json.RawMessage, _ tree.Actor) (tree.Result, error) {
	a, err := decode[struct{ Theme, Mode string }](args)
	if err != nil {
		return tree.Result{}, err
	}
	if all, _ := s.themes(); a.Theme != "" {
		if _, ok := all[a.Theme]; !ok {
			return tree.Result{}, fmt.Errorf("no theme %q", a.Theme)
		}
	}
	err = s.update(func(st *state) error {
		st.Theme = cmp.Or(a.Theme, st.Theme)
		st.Mode = cmp.Or(a.Mode, st.Mode)
		return nil
	})
	return tree.Result{Message: "theme updated"}, err
}

func (s *Service) setAccent(_ context.Context, _ tree.Node, args json.RawMessage, _ tree.Actor) (tree.Result, error) {
	a, err := decode[struct{ Color string }](args)
	if err != nil {
		return tree.Result{}, err
	}
	if a.Color != "" && !colorPattern.MatchString(a.Color) {
		return tree.Result{}, fmt.Errorf("color %q is not a hex colour", a.Color)
	}
	err = s.update(func(st *state) error {
		st.Accent = a.Color
		return nil
	})
	return tree.Result{Message: "accent updated"}, err
}

func (s *Service) saveTheme(_ context.Context, _ tree.Node, args json.RawMessage, _ tree.Actor) (tree.Result, error) {
	a, err := decode[struct {
		Name   string
		Tokens json.RawMessage
	}](args)
	if err != nil {
		return tree.Result{}, err
	}
	if _, builtin := builtinThemes()[a.Name]; builtin || !tokenPattern.MatchString(a.Name) {
		return tree.Result{}, fmt.Errorf("theme name %q must be lowercase letters, digits and dashes, and not a built-in", a.Name)
	}
	t, err := parseTheme(a.Tokens)
	if err != nil {
		return tree.Result{}, err
	}
	data, _ := json.MarshalIndent(t, "", "  ")
	if err := writeFile(s.path("themes", a.Name+".json"), data); err != nil {
		return tree.Result{}, err
	}
	err = s.update(func(st *state) error {
		st.Theme = a.Name
		return nil
	})
	return tree.Result{Message: "saved theme " + a.Name}, err
}
