package pluginsdk

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Tools let a plugin contribute a tab of interactive tools instead of (or in
// addition to) market data. The manifest declares the tab and each tool's
// inputs; the backend renders a form, sends the values with the "tool"
// method and shows the blocks the plugin returns. Plugins hold no state
// between calls: the user's data travels in the inputs and is not stored.

// Limits the backend enforces, so plugins and hosts agree.
const (
	MaxTools          = 24
	MaxToolInputs     = 24
	MaxToolInputBytes = 8 << 20 // all inputs of one call, JSON encoded
	MaxToolFileBytes  = 8 << 20 // one file block in a result, before encoding
	MaxToolBlocks     = 200
	MaxToolTableRows  = 5000
)

// Input types a tool can ask for.
const (
	InputText     = "text"
	InputTextarea = "textarea"
	InputNumber   = "number"
	InputDate     = "date"
	InputSelect   = "select"
	InputCheckbox = "checkbox"
	// InputFile is one file read as text; the value is its content.
	InputFile = "file"
	// InputFiles is several files; the value is a list of {name, content}.
	InputFiles = "files"
)

// Block types a tool result can contain.
const (
	BlockText   = "text"   // Text, plain paragraphs separated by blank lines
	BlockNotice = "notice" // Text with a Level
	BlockTable  = "table"  // Title, Columns and Rows
	BlockFile   = "file"   // a download
	BlockStats  = "stats"  // Stats: label/value tiles
)

// Notice levels.
const (
	LevelInfo    = "info"
	LevelSuccess = "success"
	LevelWarning = "warning"
	LevelError   = "error"
)

// ToolsInfo is what a plugin adds to the app: one tab with several tools.
type ToolsInfo struct {
	Tab   ToolsTab `json:"tab"`
	Tools []Tool   `json:"tools"`
}

// ToolsTab is the navigation entry and heading of the plugin's tab.
type ToolsTab struct {
	Label       string `json:"label"`
	Icon        string `json:"icon,omitempty"` // a Bootstrap Icons class such as "bi-receipt"
	Description string `json:"description,omitempty"`
}

// Tool is one form the user can run.
type Tool struct {
	ID          string      `json:"id"`
	Label       string      `json:"label"`
	Description string      `json:"description,omitempty"`
	Submit      string      `json:"submit,omitempty"` // button label, default "Run"
	Inputs      []ToolInput `json:"inputs,omitempty"`
}

// ToolInput is one field of a tool's form.
type ToolInput struct {
	Name        string       `json:"name"`
	Label       string       `json:"label"`
	Type        string       `json:"type"`
	Required    bool         `json:"required,omitempty"`
	Default     string       `json:"default,omitempty"`
	Placeholder string       `json:"placeholder,omitempty"`
	Help        string       `json:"help,omitempty"`
	Accept      string       `json:"accept,omitempty"` // file types, such as ".csv,.txt"
	Options     []ToolOption `json:"options,omitempty"`
}

// ToolOption is one choice of a select input.
type ToolOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// ToolParams is the "tool" request. Input values are strings, numbers and
// booleans; a "files" input is a list of {name, content} objects.
type ToolParams struct {
	Tool  string         `json:"tool"`
	Input map[string]any `json:"input"`
}

// ToolFile is a file input value.
type ToolFile struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// ToolResult is what a tool returns for display.
type ToolResult struct {
	Title  string      `json:"title,omitempty"`
	Blocks []ToolBlock `json:"blocks"`
}

// ToolBlock is one piece of a result. Which fields apply depends on Type.
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

// ToolColumn heads a table column; Align is "left" (default) or "right".
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

// ToolOutput is a downloadable file. Encoding is "utf8" (Content is the
// text) or "base64" (Content is the base64 of the bytes, for other charsets).
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

var knownInputTypes = map[string]bool{
	InputText: true, InputTextarea: true, InputNumber: true, InputDate: true,
	InputSelect: true, InputCheckbox: true, InputFile: true, InputFiles: true,
}

// Validate checks the tools declaration.
func (t ToolsInfo) Validate() error {
	if strings.TrimSpace(t.Tab.Label) == "" || len(t.Tab.Label) > 24 {
		return fmt.Errorf("tools tab label must be 1-24 characters")
	}
	if len(t.Tab.Description) > 400 {
		return fmt.Errorf("tools tab description is too long")
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
		if strings.TrimSpace(tool.Label) == "" || len(tool.Label) > 60 || len(tool.Description) > 800 || len(tool.Submit) > 30 {
			return fmt.Errorf("tool %s: label, description or submit text has a bad length", tool.ID)
		}
		if len(tool.Inputs) > MaxToolInputs {
			return fmt.Errorf("tool %s: at most %d inputs", tool.ID, MaxToolInputs)
		}
		names := map[string]bool{}
		for _, in := range tool.Inputs {
			if !inputNamePattern.MatchString(in.Name) || names[in.Name] {
				return fmt.Errorf("tool %s: invalid or duplicate input %q", tool.ID, in.Name)
			}
			names[in.Name] = true
			if !knownInputTypes[in.Type] {
				return fmt.Errorf("tool %s input %s: unknown type %q", tool.ID, in.Name, in.Type)
			}
			if strings.TrimSpace(in.Label) == "" || len(in.Label) > 80 || len(in.Help) > 600 || len(in.Placeholder) > 200 || len(in.Default) > 2000 {
				return fmt.Errorf("tool %s input %s: label, help or default has a bad length", tool.ID, in.Name)
			}
			if (in.Type == InputSelect) != (len(in.Options) > 0) {
				return fmt.Errorf("tool %s input %s: only select inputs have options, and they need at least one", tool.ID, in.Name)
			}
			if len(in.Options) > 60 {
				return fmt.Errorf("tool %s input %s: too many options", tool.ID, in.Name)
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

// String returns an input as text ("" when absent or not a scalar).
func (p ToolParams) String(name string) string {
	switch v := p.Input[name].(type) {
	case string:
		return v
	case float64:
		return strings.TrimSpace(strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", v), "0"), "."))
	case bool:
		if v {
			return "true"
		}
	}
	return ""
}

// Bool returns a checkbox input.
func (p ToolParams) Bool(name string) bool { b, _ := p.Input[name].(bool); return b }

// Files returns a "files" input; a single "file" input is returned as one
// unnamed file so handlers can accept both.
func (p ToolParams) Files(name string) []ToolFile {
	switch v := p.Input[name].(type) {
	case string:
		if v != "" {
			return []ToolFile{{Content: v}}
		}
	case []any:
		out := make([]ToolFile, 0, len(v))
		for _, item := range v {
			raw, _ := json.Marshal(item)
			var f ToolFile
			if json.Unmarshal(raw, &f) == nil && f.Content != "" {
				out = append(out, f)
			}
		}
		return out
	}
	return nil
}

// Validate checks a result against the display contract.
func (r ToolResult) Validate() error {
	if len(r.Blocks) > MaxToolBlocks {
		return fmt.Errorf("a tool result has at most %d blocks", MaxToolBlocks)
	}
	for i, b := range r.Blocks {
		switch b.Type {
		case BlockText:
		case BlockNotice:
			switch b.Level {
			case LevelInfo, LevelSuccess, LevelWarning, LevelError:
			default:
				return fmt.Errorf("block %d: unknown notice level %q", i, b.Level)
			}
		case BlockTable:
			if len(b.Rows) > MaxToolTableRows {
				return fmt.Errorf("block %d: at most %d table rows", i, MaxToolTableRows)
			}
			for _, row := range b.Rows {
				if len(row) != len(b.Columns) {
					return fmt.Errorf("block %d: a table row has %d cells for %d columns", i, len(row), len(b.Columns))
				}
			}
		case BlockStats:
			if len(b.Stats) == 0 {
				return fmt.Errorf("block %d: stats block is empty", i)
			}
		case BlockFile:
			f := b.File
			if f == nil || f.Name == "" || strings.ContainsAny(f.Name, `/\`) || f.MIME == "" || (f.Encoding != "utf8" && f.Encoding != "base64") {
				return fmt.Errorf("block %d: file needs a plain name, a MIME type and utf8 or base64 encoding", i)
			}
			if len(f.Content) > MaxToolFileBytes*4/3+8 {
				return fmt.Errorf("block %d: file is larger than %d bytes", i, MaxToolFileBytes)
			}
		default:
			return fmt.Errorf("block %d: unknown block type %q", i, b.Type)
		}
	}
	return nil
}
