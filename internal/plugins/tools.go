package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Tool plugins contribute a tab of interactive tools (see pluginsdk/tools.go
// for the contract). The backend validates the declaration at install time,
// validates every call's input against it and every result against the
// display contract, so the UI can render whatever it receives.

// Limits; they mirror pluginsdk.
const (
	MaxTools          = 24
	MaxToolInputs     = 24
	MaxToolInputBytes = 8 << 20
	MaxToolFileBytes  = 8 << 20
	MaxToolBlocks     = 200
	MaxToolTableRows  = 5000
)

// ToolsInfo is a plugin's tab and tools; see pluginsdk.ToolsInfo.
type ToolsInfo struct {
	Tab   ToolsTab `json:"tab"`
	Tools []Tool   `json:"tools"`
}

// ToolsTab is the navigation entry of a tools plugin.
type ToolsTab struct {
	Label       string `json:"label"`
	Icon        string `json:"icon,omitempty"`
	Description string `json:"description,omitempty"`
}

// Tool is one form.
type Tool struct {
	ID          string      `json:"id"`
	Label       string      `json:"label"`
	Description string      `json:"description,omitempty"`
	Submit      string      `json:"submit,omitempty"`
	Inputs      []ToolInput `json:"inputs,omitempty"`
}

// ToolInput is one form field.
type ToolInput struct {
	Name        string       `json:"name"`
	Label       string       `json:"label"`
	Type        string       `json:"type"`
	Required    bool         `json:"required,omitempty"`
	Default     string       `json:"default,omitempty"`
	Placeholder string       `json:"placeholder,omitempty"`
	Help        string       `json:"help,omitempty"`
	Accept      string       `json:"accept,omitempty"`
	Options     []ToolOption `json:"options,omitempty"`
}

// ToolOption is one choice of a select.
type ToolOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// ToolResult is what a tool returns.
type ToolResult struct {
	Title  string      `json:"title,omitempty"`
	Blocks []ToolBlock `json:"blocks"`
}

// ToolBlock is one piece of a result.
type ToolBlock struct {
	Type    string       `json:"type"`
	Title   string       `json:"title,omitempty"`
	Text    string       `json:"text,omitempty"`
	Level   string       `json:"level,omitempty"`
	Columns []ToolColumn `json:"columns,omitempty"`
	Rows    [][]string   `json:"rows,omitempty"`
	File    *ToolOutput  `json:"file,omitempty"`
	Stats   []ToolStat   `json:"stats,omitempty"`
}

// ToolColumn heads a table column.
type ToolColumn struct {
	Label string `json:"label"`
	Align string `json:"align,omitempty"`
}

// ToolStat is a label/value tile.
type ToolStat struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Hint  string `json:"hint,omitempty"`
}

// ToolOutput is a downloadable file.
type ToolOutput struct {
	Name        string `json:"name"`
	MIME        string `json:"mime"`
	Encoding    string `json:"encoding"`
	Content     string `json:"content"`
	Description string `json:"description,omitempty"`
}

var (
	toolIDPattern    = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,39}$`)
	inputNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	iconPattern      = regexp.MustCompile(`^bi-[a-z0-9-]{1,40}$`)
)

var knownInputTypes = map[string]bool{"text": true, "textarea": true, "number": true, "date": true, "select": true, "checkbox": true, "file": true, "files": true}

// Validate checks the declaration.
func (t ToolsInfo) Validate() error {
	if strings.TrimSpace(t.Tab.Label) == "" || len(t.Tab.Label) > 24 || len(t.Tab.Description) > 400 {
		return errors.New("tools tab needs a label of 1-24 characters")
	}
	if t.Tab.Icon != "" && !iconPattern.MatchString(t.Tab.Icon) {
		return fmt.Errorf("tools tab icon %q must be a bi-* icon class", t.Tab.Icon)
	}
	if len(t.Tools) == 0 || len(t.Tools) > MaxTools {
		return fmt.Errorf("a plugin declares 1-%d tools", MaxTools)
	}
	seen := map[string]bool{}
	for _, tool := range t.Tools {
		if !toolIDPattern.MatchString(tool.ID) || seen[tool.ID] {
			return fmt.Errorf("invalid or duplicate tool id %q", tool.ID)
		}
		seen[tool.ID] = true
		if strings.TrimSpace(tool.Label) == "" || len(tool.Label) > 60 || len(tool.Description) > 800 || len(tool.Submit) > 30 || len(tool.Inputs) > MaxToolInputs {
			return fmt.Errorf("tool %s: label, description or inputs have a bad size", tool.ID)
		}
		names := map[string]bool{}
		for _, in := range tool.Inputs {
			if !inputNamePattern.MatchString(in.Name) || names[in.Name] || !knownInputTypes[in.Type] {
				return fmt.Errorf("tool %s: invalid, duplicate or unknown-type input %q", tool.ID, in.Name)
			}
			names[in.Name] = true
			if strings.TrimSpace(in.Label) == "" || len(in.Label) > 80 || len(in.Help) > 600 || len(in.Placeholder) > 200 || len(in.Default) > 2000 || len(in.Accept) > 80 {
				return fmt.Errorf("tool %s input %s: label, help or default has a bad length", tool.ID, in.Name)
			}
			if (in.Type == "select") != (len(in.Options) > 0) || len(in.Options) > 60 {
				return fmt.Errorf("tool %s input %s: only select inputs have options, and they need at least one", tool.ID, in.Name)
			}
		}
	}
	return nil
}

// Tool returns the declared tool with the given id.
func (t ToolsInfo) Tool(id string) (Tool, bool) {
	for _, tool := range t.Tools {
		if tool.ID == id {
			return tool, true
		}
	}
	return Tool{}, false
}

// ErrToolInput marks a call the backend rejected before reaching the plugin.
var ErrToolInput = errors.New("invalid tool input")

// ErrNoSuchTool is returned for an unknown plugin or tool.
var ErrNoSuchTool = errors.New("no such tool")

// checkInput validates the submitted values against the tool's form.
func checkInput(tool Tool, input map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > MaxToolInputBytes {
		return nil, fmt.Errorf("%w: input is larger than %d MB", ErrToolInput, MaxToolInputBytes>>20)
	}
	clean := map[string]any{}
	for _, in := range tool.Inputs {
		v, present := input[in.Name]
		empty := !present || v == nil || v == "" || v == false
		if list, ok := v.([]any); ok && len(list) == 0 {
			empty = true
		}
		if empty {
			if in.Required && in.Type != "checkbox" {
				return nil, fmt.Errorf("%w: %s is required", ErrToolInput, in.Label)
			}
			continue
		}
		switch in.Type {
		case "text", "textarea", "date", "file":
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("%w: %s must be text", ErrToolInput, in.Label)
			}
			clean[in.Name] = s
		case "number":
			switch n := v.(type) {
			case float64:
				clean[in.Name] = n
			default:
				return nil, fmt.Errorf("%w: %s must be a number", ErrToolInput, in.Label)
			}
		case "checkbox":
			b, ok := v.(bool)
			if !ok {
				return nil, fmt.Errorf("%w: %s must be true or false", ErrToolInput, in.Label)
			}
			clean[in.Name] = b
		case "select":
			s, _ := v.(string)
			found := false
			for _, o := range in.Options {
				found = found || o.Value == s
			}
			if !found {
				return nil, fmt.Errorf("%w: %s is not one of the choices", ErrToolInput, in.Label)
			}
			clean[in.Name] = s
		case "files":
			list, ok := v.([]any)
			if !ok {
				return nil, fmt.Errorf("%w: %s must be a list of files", ErrToolInput, in.Label)
			}
			files := make([]map[string]string, 0, len(list))
			for _, item := range list {
				obj, ok := item.(map[string]any)
				content, _ := obj["content"].(string)
				name, _ := obj["name"].(string)
				if !ok {
					return nil, fmt.Errorf("%w: %s must be a list of files", ErrToolInput, in.Label)
				}
				files = append(files, map[string]string{"name": name, "content": content})
			}
			clean[in.Name] = files
		}
	}
	return clean, nil
}

// Validate checks a plugin's result against the display contract.
func (r ToolResult) Validate() error {
	if len(r.Blocks) > MaxToolBlocks {
		return fmt.Errorf("result has more than %d blocks", MaxToolBlocks)
	}
	for i, b := range r.Blocks {
		switch b.Type {
		case "text":
		case "notice":
			if b.Level != "info" && b.Level != "success" && b.Level != "warning" && b.Level != "error" {
				return fmt.Errorf("block %d: unknown notice level %q", i, b.Level)
			}
		case "table":
			if len(b.Rows) > MaxToolTableRows {
				return fmt.Errorf("block %d: more than %d table rows", i, MaxToolTableRows)
			}
			for _, row := range b.Rows {
				if len(row) != len(b.Columns) {
					return fmt.Errorf("block %d: a table row does not match its columns", i)
				}
			}
		case "stats":
			if len(b.Stats) == 0 {
				return fmt.Errorf("block %d: empty stats", i)
			}
		case "file":
			f := b.File
			if f == nil || f.Name == "" || strings.ContainsAny(f.Name, `/\`) || f.MIME == "" || (f.Encoding != "utf8" && f.Encoding != "base64") || len(f.Content) > MaxToolFileBytes*4/3+8 {
				return fmt.Errorf("block %d: invalid file", i)
			}
		default:
			return fmt.Errorf("block %d: unknown block type %q", i, b.Type)
		}
	}
	return nil
}

// ToolPlugin is an installed tools plugin as the UI sees it.
type ToolPlugin struct {
	PluginID string    `json:"plugin_id"`
	Name     string    `json:"name"`
	Version  string    `json:"version"`
	Terms    string    `json:"terms,omitempty"`
	Ready    bool      `json:"ready"`
	Missing  []string  `json:"missing,omitempty"`
	Tools    ToolsInfo `json:"tools"`
}

// ToolPlugins lists the installed plugins that contribute tools.
func (m *Manager) ToolPlugins() []ToolPlugin {
	out := []ToolPlugin{}
	for _, p := range m.Installed() {
		if p.Manifest.Tools == nil {
			continue
		}
		_, missing := m.env(&p.Manifest)
		out = append(out, ToolPlugin{PluginID: p.Manifest.ID, Name: p.Manifest.Name, Version: p.Manifest.Version,
			Ready: len(missing) == 0, Missing: missing, Tools: *p.Manifest.Tools})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PluginID < out[j].PluginID })
	return out
}

// CallTool runs one tool of an installed plugin. The input is checked against
// the declared form first; nothing is stored or logged by the backend.
func (m *Manager) CallTool(ctx context.Context, pluginID, toolID string, input map[string]any) (ToolResult, error) {
	var installed *InstalledPlugin
	for _, p := range m.Installed() {
		if p.Manifest.ID == pluginID && p.Manifest.Tools != nil {
			p := p
			installed = &p
		}
	}
	if installed == nil {
		return ToolResult{}, fmt.Errorf("%w: plugin %q offers no tools", ErrNoSuchTool, pluginID)
	}
	tool, ok := installed.Manifest.Tools.Tool(toolID)
	if !ok {
		return ToolResult{}, fmt.Errorf("%w: %s has no tool %q", ErrNoSuchTool, pluginID, toolID)
	}
	if _, missing := m.env(&installed.Manifest); len(missing) > 0 {
		return ToolResult{}, &CallError{Code: CodeNotConfigured, Message: strings.Join(missing, ", ") + " must be configured on the backend"}
	}
	clean, err := checkInput(tool, input)
	if err != nil {
		return ToolResult{}, err
	}
	proc, err := m.process(*installed)
	if err != nil {
		return ToolResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, m.cfg.CallTimeout)
	defer cancel()
	var out ToolResult
	if err := proc.call(ctx, "tool", map[string]any{"tool": toolID, "input": clean}, &out); err != nil {
		return ToolResult{}, err
	}
	if err := out.Validate(); err != nil {
		return ToolResult{}, &CallError{Code: CodeInternal, Message: "plugin returned an invalid result: " + err.Error()}
	}
	if out.Blocks == nil {
		out.Blocks = []ToolBlock{}
	}
	return out, nil
}
