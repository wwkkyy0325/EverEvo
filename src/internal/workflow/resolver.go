package workflow

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// tmplRe matches {{ ... }} placeholders (non-greedy).
var tmplRe = regexp.MustCompile(`\{\{(.+?)\}\}`)

// ResolveTemplate replaces all {{node_id.path.to.field}} placeholders
// with values from the outputs map. Returns the resolved string.
func ResolveTemplate(tmpl string, outputs map[string]any) (string, error) {
	var firstErr error
	result := tmplRe.ReplaceAllStringFunc(tmpl, func(match string) string {
		inner := strings.TrimSpace(match[2 : len(match)-2])
		val, err := resolvePath(inner, outputs)
		if err != nil {
			firstErr = err
			return match // keep original placeholder on error
		}
		// JSON-marshal arrays/maps so they can be round-tripped; plain fmt for scalars.
		switch val.(type) {
		case []any, map[string]any:
			b, _ := json.Marshal(val)
			return string(b)
		default:
			return fmt.Sprintf("%v", val)
		}
	})
	return result, firstErr
}

// ResolveMap walks a map and resolves template strings in all string values.
func ResolveMap(params map[string]any, outputs map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(params))
	for k, v := range params {
		switch val := v.(type) {
		case string:
			resolved, err := ResolveTemplate(val, outputs)
			if err != nil {
				return nil, err
			}
			out[k] = resolved
		case map[string]any:
			resolved, err := ResolveMap(val, outputs)
			if err != nil {
				return nil, err
			}
			out[k] = resolved
		default:
			out[k] = v
		}
	}
	return out, nil
}

// ResolveExpression resolves templates and returns the underlying value.
// Unlike ResolveTemplate (which always returns a string), this returns
// the actual typed value (bool, number, string, array, map).
func ResolveExpression(expr string, outputs map[string]any) (any, error) {
	inner := strings.TrimSpace(expr)
	// If the whole expression is a single {{...}}, return the resolved value as-is
	if strings.HasPrefix(inner, "{{") && strings.HasSuffix(inner, "}}") {
		path := strings.TrimSpace(inner[2 : len(inner)-2])
		return resolvePath(path, outputs)
	}
	// Otherwise treat it as a template string
	return ResolveTemplate(inner, outputs)
}

// resolvePath walks a dot-separated path into the outputs map.
// Supports array indexing: items.0.name
func resolvePath(path string, outputs map[string]any) (any, error) {
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty path")
	}

	// First segment is the node ID
	nodeID := parts[0]
	val, ok := outputs[nodeID]
	if !ok {
		return nil, fmt.Errorf("node %q not found in outputs", nodeID)
	}

	// Walk remaining path segments
	cur := val
	for i := 1; i < len(parts); i++ {
		switch v := cur.(type) {
		case map[string]any:
			var ok bool
			cur, ok = v[parts[i]]
			if !ok {
				return nil, fmt.Errorf("field %q not found in %s", parts[i], strings.Join(parts[:i], "."))
			}
		case []any:
			idx, err := strconv.Atoi(parts[i])
			if err != nil {
				return nil, fmt.Errorf("array index %q is not a number in %s", parts[i], path)
			}
			if idx < 0 || idx >= len(v) {
				return nil, fmt.Errorf("array index %d out of bounds (len=%d) in %s", idx, len(v), path)
			}
			cur = v[idx]
		default:
			return nil, fmt.Errorf("cannot index into %T at %s", cur, strings.Join(parts[:i], "."))
		}
	}
	return cur, nil
}

// HasTemplates returns true if the string contains {{...}} placeholders.
func HasTemplates(s string) bool {
	return tmplRe.MatchString(s)
}
