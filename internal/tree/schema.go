package tree

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
)

// schema is the JSON Schema subset actions use for their args.
type schema struct {
	Type       string             `json:"type"`
	Properties map[string]*schema `json:"properties"`
	Required   []string           `json:"required"`
	Enum       []any              `json:"enum"`
	Items      *schema            `json:"items"`
}

func parseSchema(raw json.RawMessage) (*schema, error) {
	s := &schema{Type: "object", Properties: map[string]*schema{}}
	if len(raw) == 0 {
		return s, nil
	}
	return s, json.Unmarshal(raw, s)
}

func validate(rawSchema, args json.RawMessage) error {
	s, err := parseSchema(rawSchema)
	if err != nil {
		return fmt.Errorf("bad args schema: %w", err)
	}
	var v any
	if err := json.Unmarshal(args, &v); err != nil {
		return fmt.Errorf("args: %w", err)
	}
	return s.check("args", v)
}

func (s *schema) check(path string, v any) error {
	if len(s.Enum) > 0 && !slices.ContainsFunc(s.Enum, func(e any) bool { return reflect.DeepEqual(e, v) }) {
		return fmt.Errorf("%s must be one of %v", path, s.Enum)
	}
	ok := true
	switch s.Type {
	case "object":
		m, isMap := v.(map[string]any)
		if !isMap {
			ok = false
			break
		}
		for _, name := range s.Required {
			if _, found := m[name]; !found {
				return fmt.Errorf("%s.%s is required", path, name)
			}
		}
		for name, val := range m {
			p, known := s.Properties[name]
			if !known && s.Properties != nil {
				return fmt.Errorf("%s.%s is not a known argument", path, name)
			}
			if known {
				if err := p.check(path+"."+name, val); err != nil {
					return err
				}
			}
		}
	case "array":
		items, isSlice := v.([]any)
		if !isSlice {
			ok = false
			break
		}
		for i, item := range items {
			if s.Items != nil {
				if err := s.Items.check(fmt.Sprintf("%s[%d]", path, i), item); err != nil {
					return err
				}
			}
		}
	case "string":
		_, ok = v.(string)
	case "boolean":
		_, ok = v.(bool)
	case "number":
		_, ok = v.(float64)
	case "integer":
		f, isNum := v.(float64)
		ok = isNum && f == math.Trunc(f)
	}
	if !ok {
		return fmt.Errorf("%s must be of type %s", path, s.Type)
	}
	return nil
}

// signature renders an args schema as "{path, name?}".
func signature(raw json.RawMessage) string {
	s, err := parseSchema(raw)
	if err != nil || len(s.Properties) == 0 {
		return ""
	}
	names := slices.Clone(s.Required)
	var optional []string
	for name := range s.Properties {
		if !slices.Contains(s.Required, name) {
			optional = append(optional, name+"?")
		}
	}
	slices.Sort(optional)
	return "{" + strings.Join(append(names, optional...), ", ") + "}"
}
