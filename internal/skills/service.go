package skills

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"floweysales/internal/storage"
)

// Store is the part of userspace skills need. *storage.Database satisfies it.
type Store interface {
	ListUserspaceFiles(ctx context.Context, owner int64) ([]storage.UserspaceFile, error)
	ReadUserspaceFile(ctx context.Context, owner int64, rawPath string) ([]byte, string, error)
	WriteUserspaceFile(ctx context.Context, owner int64, rawPath, mime string, content []byte) error
}

// Service reads and writes the skills of one user at a time.
type Service struct{ Store Store }

// Entries lists every skill folder of the user that parses, drafts included,
// in name order. A folder that does not parse is skipped here; it is neither
// offered to the model nor loadable.
func (s Service) Entries(ctx context.Context, owner int64) ([]Entry, error) {
	files, err := s.Store.ListUserspaceFiles(ctx, owner)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, f := range files {
		name, ok := skillFolder(f.Path)
		if !ok || len(out) >= maxFolders {
			continue
		}
		raw, _, err := s.Store.ReadUserspaceFile(ctx, owner, f.Path)
		if err != nil {
			continue
		}
		if e, err := Parse(name, raw); err == nil {
			out = append(out, e)
		}
	}
	return out, nil
}

// skillFolder returns the folder name when p is exactly /skills/<name>/SKILL.md.
func skillFolder(p string) (string, bool) {
	rest, ok := strings.CutPrefix(p, Dir)
	if !ok {
		return "", false
	}
	name, file, ok := strings.Cut(rest, "/")
	return name, ok && file == FileName
}

// Index is the prompt section for the user's enabled skills.
func (s Service) Index(ctx context.Context, owner int64) string {
	entries, err := s.Entries(ctx, owner)
	if err != nil {
		return ""
	}
	return Index(entries)
}

// Loaded is what use_skill returns.
type Loaded struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Instructions string   `json:"instructions,omitempty"`
	Files        []string `json:"other_files,omitempty"`
	File         string   `json:"file,omitempty"`
	Content      string   `json:"content,omitempty"`
	Truncated    bool     `json:"truncated,omitempty"`
}

// Use loads an enabled skill, or one text file from its folder when file is
// not empty. A draft or unknown name is refused alike.
func (s Service) Use(ctx context.Context, owner int64, name, file string) (Loaded, error) {
	name = strings.TrimSpace(name)
	notFound := fmt.Errorf("no skill named %q; the names are in the list in the instructions", name)
	if !ValidName(name) {
		return Loaded{}, notFound
	}
	raw, _, err := s.Store.ReadUserspaceFile(ctx, owner, Dir+name+"/"+FileName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Loaded{}, notFound
		}
		return Loaded{}, err
	}
	e, err := Parse(name, raw)
	if err != nil || e.Draft {
		return Loaded{}, notFound
	}
	files, err := s.Store.ListUserspaceFiles(ctx, owner)
	if err != nil {
		return Loaded{}, err
	}
	var others []string
	for _, f := range files {
		if rel, ok := strings.CutPrefix(f.Path, Dir+name+"/"); ok && rel != FileName {
			others = append(others, rel)
		}
	}
	out := Loaded{Name: name, Type: e.Type}
	if file == "" {
		out.Instructions, out.Truncated = clip(e.Body)
		out.Files = others
		return out, nil
	}
	file = path.Clean(strings.TrimSpace(file))
	known := false
	for _, o := range others {
		known = known || o == file
	}
	if !known {
		return Loaded{}, fmt.Errorf("skill %q has no file %q; its files are: %s", name, file, strings.Join(others, ", "))
	}
	content, _, err := s.Store.ReadUserspaceFile(ctx, owner, Dir+name+"/"+file)
	if err != nil {
		return Loaded{}, err
	}
	if !utf8.Valid(content) || strings.ContainsRune(string(content), 0) {
		return Loaded{}, fmt.Errorf("%q is not a text file", file)
	}
	out.File = file
	out.Content, out.Truncated = clip(string(content))
	return out, nil
}

func clip(s string) (string, bool) {
	if len(s) <= MaxReturnBytes {
		return s, false
	}
	cut := MaxReturnBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}

// SaveDraft stores a skill the agent proposed. It is always a draft, it never
// replaces an existing folder, and nothing it writes reaches a prompt until
// the user removes the draft line.
func (s Service) SaveDraft(ctx context.Context, owner int64, e Entry) (string, error) {
	e.Name, e.Description = strings.TrimSpace(e.Name), strings.Join(strings.Fields(e.Description), " ")
	if e.Type == "" {
		e.Type = TypeSkill
	}
	e.Draft = true
	switch {
	case !ValidName(e.Name):
		return "", fmt.Errorf("%q is not a valid skill name: lowercase letters, digits and hyphens, at most %d characters", e.Name, maxName)
	case e.Description == "":
		return "", errors.New("a description is required")
	case len([]rune(e.Description)) > MaxDescription:
		return "", fmt.Errorf("the description is longer than %d characters", MaxDescription)
	case e.Type != TypeSkill && e.Type != TypeKnowledge:
		return "", errors.New("type must be skill or knowledge")
	case strings.TrimSpace(e.Body) == "":
		return "", errors.New("the instructions are empty")
	case len(e.Body) > MaxBodyBytes:
		return "", fmt.Errorf("the instructions are longer than %d bytes", MaxBodyBytes)
	}
	files, err := s.Store.ListUserspaceFiles(ctx, owner)
	if err != nil {
		return "", err
	}
	for _, f := range files {
		if strings.HasPrefix(f.Path, Dir+e.Name+"/") {
			return "", fmt.Errorf("a skill named %q already exists and is not replaced; pick another name", e.Name)
		}
	}
	p := Dir + e.Name + "/" + FileName
	if err := s.Store.WriteUserspaceFile(ctx, owner, p, "text/markdown", []byte(Render(e))); err != nil {
		return "", err
	}
	return p, nil
}
