package post

import (
	"fmt"
	"math"

	"gopkg.in/yaml.v3"
)

// ExtraValues parses the extra frontmatter src into values that encode as
// JSON: nested maps with string keys, lists, strings, numbers, booleans and
// null. It returns an empty map when src is empty. Dates and timestamps stay
// the string as written (2026-01-02 is "2026-01-02"), keys that are not
// strings become the string YAML shows for them, and infinities and NaN become
// the strings "+Inf", "-Inf" and "NaN", which JSON cannot hold as numbers.
func ExtraValues(src string) (map[string]any, error) {
	root, err := parseExtra(src)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return map[string]any{}, nil
	}
	keepTimestamps(root)
	var decoded map[string]any
	if err := root.Decode(&decoded); err != nil {
		return nil, &FieldError{"extra frontmatter", fmt.Sprintf("cannot be read as values: %v", err)}
	}
	out, _ := jsonSafe(decoded).(map[string]any)
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

// keepTimestamps retags timestamp scalars as strings, so decoding leaves them
// as written. It does not follow aliases: an anchored node is visited where
// it is defined.
func keepTimestamps(n *yaml.Node) {
	if n.Kind == yaml.ScalarNode && n.ShortTag() == "!!timestamp" {
		n.Tag = "!!str"
	}
	for _, c := range n.Content {
		keepTimestamps(c)
	}
}

// jsonSafe rewrites a decoded YAML value so encoding/json accepts it.
func jsonSafe(v any) any {
	switch v := v.(type) {
	case map[string]any:
		for k, e := range v {
			v[k] = jsonSafe(e)
		}
		return v
	case map[any]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			out[fmt.Sprint(k)] = jsonSafe(e)
		}
		return out
	case []any:
		for i, e := range v {
			v[i] = jsonSafe(e)
		}
		return v
	case float64:
		switch {
		case math.IsNaN(v):
			return "NaN"
		case math.IsInf(v, 1):
			return "+Inf"
		case math.IsInf(v, -1):
			return "-Inf"
		}
	}
	return v
}
