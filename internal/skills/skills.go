// Package skills reads and writes a user's agent skills and knowledge.
//
// A skill is a folder /skills/<name>/ in the user's userspace holding a
// SKILL.md (frontmatter with name and description, then instructions) and
// optional text files beside it, the layout Claude and Codex skills use.
// Knowledge is the same folder with "type: knowledge": reference to consult
// rather than a procedure to follow. A skill the agent proposed carries
// "status: draft" and stays out of prompts until the user removes that line.
package skills

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	// Dir is the userspace folder that holds every skill.
	Dir = "/skills/"
	// FileName is the one file a skill must have.
	FileName = "SKILL.md"

	TypeSkill     = "skill"
	TypeKnowledge = "knowledge"

	// MaxDescription caps the one-line description shown in the prompt index.
	MaxDescription = 300
	// MaxIndexEntries and MaxIndexBytes keep the index from filling a small
	// context window; entries past either cap are still loadable by name.
	MaxIndexEntries = 40
	MaxIndexBytes   = 4000
	// MaxReturnBytes caps what use_skill returns in one call.
	MaxReturnBytes = 32000
	// MaxBodyBytes caps the instructions the agent may save in one skill.
	MaxBodyBytes = 20000
	// maxName is the longest skill name.
	maxName = 64
	// maxFolders bounds how many skills one prompt build reads.
	maxFolders = 100
)

var nameRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidName reports whether s can name a skill folder.
func ValidName(s string) bool { return len(s) <= maxName && nameRE.MatchString(s) }

// Entry is one parsed skill or knowledge folder.
type Entry struct {
	Name        string
	Description string
	Type        string
	Draft       bool
	Body        string
}

// Parse reads a SKILL.md. folder is the name of the folder it sits in, which
// the frontmatter name must match. Frontmatter keys other than name,
// description, type and status are accepted and ignored.
func Parse(folder string, raw []byte) (Entry, error) {
	text := strings.ReplaceAll(strings.TrimPrefix(string(raw), "\xef\xbb\xbf"), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return Entry{}, fmt.Errorf("SKILL.md must start with a --- frontmatter block")
	}
	rest := text[len("---\n"):]
	var front, body string
	if i := strings.Index(rest, "\n---\n"); i >= 0 {
		front, body = rest[:i], rest[i+len("\n---\n"):]
	} else if strings.HasSuffix(rest, "\n---") {
		front = strings.TrimSuffix(rest, "\n---")
	} else {
		return Entry{}, fmt.Errorf("the frontmatter block is not closed with ---")
	}
	fields := parseFields(front)
	e := Entry{Name: fields["name"], Description: fields["description"], Type: fields["type"], Body: strings.TrimSpace(body)}
	if e.Type == "" {
		e.Type = TypeSkill
	}
	e.Draft = strings.EqualFold(fields["status"], "draft")
	switch {
	case e.Name != folder:
		return Entry{}, fmt.Errorf("frontmatter name %q must match the folder name %q", e.Name, folder)
	case !ValidName(folder):
		return Entry{}, fmt.Errorf("%q is not a valid skill name: lowercase letters, digits and hyphens, at most %d characters", folder, maxName)
	case e.Description == "":
		return Entry{}, fmt.Errorf("the description is empty")
	case e.Type != TypeSkill && e.Type != TypeKnowledge:
		return Entry{}, fmt.Errorf("type must be skill or knowledge, not %q", e.Type)
	}
	return e, nil
}

// parseFields reads the flat "key: value" frontmatter these files use. An
// indented line continues the previous value, and a bare > or | starts a
// folded value, as in YAML.
func parseFields(front string) map[string]string {
	fields := map[string]string{}
	var key string
	for _, line := range strings.Split(front, "\n") {
		if line == "" {
			continue
		}
		if (line[0] == ' ' || line[0] == '\t') && key != "" {
			fields[key] = strings.TrimSpace(fields[key] + " " + strings.TrimSpace(line))
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			key = ""
			continue
		}
		key = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		if strings.Trim(v, ">|-+") == "" {
			v = ""
		}
		fields[key] = unquote(v)
	}
	return fields
}

func unquote(v string) string {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		return strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(v[1 : len(v)-1])
	}
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	}
	return v
}

// Render is the SKILL.md text of an entry, with the description quoted so any
// text in it stays one valid frontmatter value.
func Render(e Entry) string {
	var b strings.Builder
	b.WriteString("---\nname: " + e.Name + "\n")
	b.WriteString("description: \"" + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(e.Description) + "\"\n")
	b.WriteString("type: " + e.Type + "\n")
	if e.Draft {
		b.WriteString("status: draft\n")
	}
	b.WriteString("---\n\n" + strings.TrimSpace(e.Body) + "\n")
	return b.String()
}

// Index is the prompt section that lists the enabled entries, one line each,
// within the entry and size caps. It is empty when there is nothing to list.
func Index(entries []Entry) string {
	var b strings.Builder
	shown, enabled := 0, 0
	for _, e := range entries {
		if e.Draft {
			continue
		}
		enabled++
		line := "\n- " + e.Name
		if e.Type == TypeKnowledge {
			line += " (knowledge)"
		}
		line += ": " + oneLine(e.Description)
		if shown >= MaxIndexEntries || b.Len()+len(line) > MaxIndexBytes {
			continue
		}
		b.WriteString(line)
		shown++
	}
	if enabled == 0 {
		return ""
	}
	out := "\nThe user's saved skills and knowledge. When one fits the request, load it with use_skill before answering and follow it:" + b.String()
	if enabled > shown {
		out += fmt.Sprintf("\n- ...and %d more; use_skill loads any of them by name.", enabled-shown)
	}
	return out
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > MaxDescription {
		s = string(r[:MaxDescription-1]) + "…"
	}
	return s
}
