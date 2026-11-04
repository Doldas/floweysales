package ai

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Preflight checks a tool call against the tool's own schema before it runs,
// so a malformed call comes back to the model as a clear correction instead
// of a confusing failure inside the tool. It is lenient on purpose: it
// rejects an unknown tool, a missing required argument and a value of the
// wrong kind (a list where text belongs), and lets numbers arrive as numeric
// text, which models do often and the tools already read.

var (
	toolSchemaOnce sync.Once
	toolSchemas    map[string]map[string]any
)

func schemaOf(name string) (map[string]any, bool) {
	toolSchemaOnce.Do(func() {
		toolSchemas = map[string]map[string]any{}
		for _, t := range ToolDefs() {
			toolSchemas[t.Function.Name] = t.Function.Parameters
		}
	})
	s, ok := toolSchemas[name]
	return s, ok
}

// ValidateToolArgs returns nil when the call can be dispatched, or an error
// that says exactly what to fix.
func ValidateToolArgs(name, argsJSON string) error {
	schema, ok := schemaOf(name)
	if !ok {
		return fmt.Errorf("there is no tool called %q; use one of the tools you were given", name)
	}
	args := map[string]any{}
	if a := strings.TrimSpace(argsJSON); a != "" && a != "null" {
		if err := json.Unmarshal([]byte(a), &args); err != nil {
			return fmt.Errorf("the arguments of %s must be one JSON object: %v", name, err)
		}
	}
	var problems []string
	for _, key := range requiredKeys(schema) {
		if v, present := args[key]; !present || v == nil {
			problems = append(problems, fmt.Sprintf("%q is required", key))
		}
	}
	props, _ := schema["properties"].(map[string]any)
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		prop, known := props[key].(map[string]any)
		if !known || args[key] == nil {
			continue
		}
		if msg := kindProblem(prop, args[key]); msg != "" {
			problems = append(problems, fmt.Sprintf("%q %s", key, msg))
		}
		if allowed := enumOf(prop); len(allowed) > 0 {
			if s, isStr := args[key].(string); isStr && !contains(allowed, s) {
				problems = append(problems, fmt.Sprintf("%q must be one of %s", key, strings.Join(allowed, ", ")))
			}
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("the call to %s is not valid: %s. Send it again with corrected arguments", name, strings.Join(problems, "; "))
}

func requiredKeys(schema map[string]any) []string {
	switch r := schema["required"].(type) {
	case []string:
		return r
	case []any:
		out := make([]string, 0, len(r))
		for _, v := range r {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func enumOf(prop map[string]any) []string {
	switch e := prop["enum"].(type) {
	case []string:
		return e
	case []any:
		out := make([]string, 0, len(e))
		for _, v := range e {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// kindProblem says what is wrong when v is not the kind the property declares.
func kindProblem(prop map[string]any, v any) string {
	want, _ := prop["type"].(string)
	switch want {
	case "string":
		switch v.(type) {
		case []any, map[string]any:
			return "must be text"
		}
	case "array":
		if _, ok := v.([]any); !ok {
			return "must be a list"
		}
	case "object":
		if _, ok := v.(map[string]any); !ok {
			return "must be an object"
		}
	case "boolean":
		switch x := v.(type) {
		case bool:
		case string:
			if x != "true" && x != "false" {
				return "must be true or false"
			}
		default:
			return "must be true or false"
		}
	case "integer", "number":
		switch x := v.(type) {
		case float64:
			if want == "integer" && x != float64(int64(x)) {
				return "must be a whole number"
			}
		case string:
			var f float64
			if _, err := fmt.Sscan(x, &f); err != nil {
				return "must be a number"
			}
		default:
			return "must be a number"
		}
	}
	return ""
}
