package skills

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"

	"floweysales/internal/storage"
)

type memStore struct{ files map[int64]map[string]string }

func newMem() *memStore { return &memStore{files: map[int64]map[string]string{}} }

func (m *memStore) put(owner int64, p, content string) {
	if m.files[owner] == nil {
		m.files[owner] = map[string]string{}
	}
	m.files[owner][p] = content
}

func (m *memStore) ListUserspaceFiles(_ context.Context, owner int64) ([]storage.UserspaceFile, error) {
	var out []storage.UserspaceFile
	for p, c := range m.files[owner] {
		out = append(out, storage.UserspaceFile{Path: p, Size: len(c)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (m *memStore) ReadUserspaceFile(_ context.Context, owner int64, p string) ([]byte, string, error) {
	c, ok := m.files[owner][p]
	if !ok {
		return nil, "", sql.ErrNoRows
	}
	return []byte(c), "text/markdown", nil
}

func (m *memStore) WriteUserspaceFile(_ context.Context, owner int64, p, _ string, c []byte) error {
	m.put(owner, p, string(c))
	return nil
}

const dcf = "---\nname: dcf-walkthrough\ndescription: Value a company with a DCF, step by step\n---\n\n# DCF\n\n1. Get the filings.\n"

func TestParseClaudeSkill(t *testing.T) {
	e, err := Parse("dcf-walkthrough", []byte("\xef\xbb\xbf"+strings.ReplaceAll(dcf, "\n", "\r\n")))
	if err != nil || e.Name != "dcf-walkthrough" || e.Type != TypeSkill || e.Draft || !strings.HasPrefix(e.Body, "# DCF") {
		t.Fatalf("%+v %v", e, err)
	}
	e, err = Parse("a-b", []byte("---\nname: a-b\ndescription: >\n  folded over\n  two lines\nallowed-tools: Read\ntype: knowledge\nstatus: draft\n---\nbody"))
	if err != nil || e.Description != "folded over two lines" || e.Type != TypeKnowledge || !e.Draft || e.Body != "body" {
		t.Fatalf("%+v %v", e, err)
	}
}

func TestParseRefusesBrokenSkills(t *testing.T) {
	for name, raw := range map[string]string{
		"no frontmatter": "# just text",
		"unclosed":       "---\nname: x\ndescription: d\n",
		"name mismatch":  "---\nname: other\ndescription: d\n---\nbody",
		"no description": "---\nname: x\n---\nbody",
		"bad type":       "---\nname: x\ndescription: d\ntype: tool\n---\nbody",
	} {
		if _, err := Parse("x", []byte(raw)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := Parse("Bad_Name", []byte("---\nname: Bad_Name\ndescription: d\n---\nb")); err == nil {
		t.Error("an invalid folder name was accepted")
	}
}

func TestIndexListsEnabledOnlyAndIsCapped(t *testing.T) {
	var entries []Entry
	entries = append(entries, Entry{Name: "hidden-draft", Description: "must not show", Draft: true, Type: TypeSkill})
	for i := 0; i < MaxIndexEntries+5; i++ {
		entries = append(entries, Entry{Name: fmt.Sprintf("s%02d", i), Description: "d", Type: TypeSkill})
	}
	idx := Index(entries)
	if strings.Contains(idx, "hidden-draft") || !strings.Contains(idx, "...and 5 more") || strings.Count(idx, "\n- s") != MaxIndexEntries {
		t.Fatalf("index:\n%s", idx)
	}
	long := Index([]Entry{{Name: "k", Description: strings.Repeat("x", 1000) + "\nsecond line", Type: TypeKnowledge}})
	if len(long) > 600 || strings.Contains(long, "second line") || !strings.Contains(long, "k (knowledge): ") {
		t.Fatalf("a long description must be cut to one line: %d", len(long))
	}
	if Index(nil) != "" || Index([]Entry{{Name: "d", Description: "d", Draft: true}}) != "" {
		t.Error("nothing enabled must give no index")
	}
	big := make([]Entry, 30)
	for i := range big {
		big[i] = Entry{Name: fmt.Sprintf("big%02d", i), Description: strings.Repeat("y", 290), Type: TypeSkill}
	}
	if got := Index(big); len(got) > MaxIndexBytes+300 || !strings.Contains(got, "more; use_skill") {
		t.Fatalf("the byte cap must hold: %d", len(got))
	}
}

func TestUseLoadsSkillAndFiles(t *testing.T) {
	m := newMem()
	m.put(1, "/skills/dcf-walkthrough/SKILL.md", dcf)
	m.put(1, "/skills/dcf-walkthrough/notes/wacc.md", "WACC notes")
	m.put(1, "/skills/dcf-walkthrough/logo.bin", "a\x00b")
	m.put(2, "/skills/theirs/SKILL.md", "---\nname: theirs\ndescription: d\n---\nprivate text")
	svc := Service{m}
	ctx := context.Background()

	got, err := svc.Use(ctx, 1, "dcf-walkthrough", "")
	if err != nil || !strings.Contains(got.Instructions, "Get the filings") || len(got.Files) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	if f, err := svc.Use(ctx, 1, "dcf-walkthrough", "notes/wacc.md"); err != nil || f.Content != "WACC notes" {
		t.Fatalf("%+v %v", f, err)
	}
	for _, file := range []string{"../../theirs/SKILL.md", "/etc/hosts", "missing.md", "logo.bin"} {
		if _, err := svc.Use(ctx, 1, "dcf-walkthrough", file); err == nil {
			t.Errorf("file %q was served", file)
		}
	}
	if _, err := svc.Use(ctx, 1, "theirs", ""); err == nil {
		t.Error("another user's skill was loaded")
	}
	if _, err := svc.Use(ctx, 1, "../x", ""); err == nil {
		t.Error("a path as a name was accepted")
	}
}

func TestDraftsCannotBeLoadedUntilEnabled(t *testing.T) {
	m := newMem()
	svc := Service{m}
	ctx := context.Background()
	p, err := svc.SaveDraft(ctx, 1, Entry{Name: "my-flow", Description: `Say "hi": a #test`, Body: "Do the thing."})
	if err != nil || p != "/skills/my-flow/SKILL.md" {
		t.Fatalf("%q %v", p, err)
	}
	if _, err := svc.Use(ctx, 1, "my-flow", ""); err == nil {
		t.Fatal("a draft was loaded")
	}
	if idx := svc.Index(ctx, 1); idx != "" {
		t.Fatalf("a draft reached the index: %s", idx)
	}
	// The user enables it by removing the draft line, as in the Files page.
	m.put(1, p, strings.Replace(m.files[1][p], "status: draft\n", "", 1))
	got, err := svc.Use(ctx, 1, "my-flow", "")
	if err != nil || got.Instructions != "Do the thing." {
		t.Fatalf("%+v %v", got, err)
	}
	if !strings.Contains(svc.Index(ctx, 1), `my-flow: Say "hi": a #test`) {
		t.Fatalf("an enabled skill must be listed with its description intact: %s", svc.Index(ctx, 1))
	}
}

func TestSaveDraftRefusals(t *testing.T) {
	m := newMem()
	m.put(1, "/skills/taken/SKILL.md", dcf)
	svc := Service{m}
	ctx := context.Background()
	ok := Entry{Name: "fine", Description: "d", Body: "b"}
	cases := map[string]func(*Entry){
		"bad name":  func(e *Entry) { e.Name = "Has Space" },
		"path name": func(e *Entry) { e.Name = "../evil" },
		"no desc":   func(e *Entry) { e.Description = " " },
		"long desc": func(e *Entry) { e.Description = strings.Repeat("x", MaxDescription+1) },
		"no body":   func(e *Entry) { e.Body = "" },
		"huge body": func(e *Entry) { e.Body = strings.Repeat("x", MaxBodyBytes+1) },
		"bad type":  func(e *Entry) { e.Type = "tool" },
		"existing":  func(e *Entry) { e.Name = "taken" },
	}
	for name, mutate := range cases {
		e := ok
		mutate(&e)
		if _, err := svc.SaveDraft(ctx, 1, e); err == nil {
			t.Errorf("%s was saved", name)
		}
	}
	if m.files[1]["/skills/taken/SKILL.md"] != dcf {
		t.Error("an existing skill was overwritten")
	}
	// A newline in the description cannot add a frontmatter key.
	if _, err := svc.SaveDraft(ctx, 1, Entry{Name: "inj", Description: "d\nstatus: published", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	e, err := Parse("inj", []byte(m.files[1]["/skills/inj/SKILL.md"]))
	if err != nil || !e.Draft {
		t.Fatalf("%+v %v", e, err)
	}
}

func TestEntriesSkipBrokenFolders(t *testing.T) {
	m := newMem()
	m.put(1, "/skills/good/SKILL.md", "---\nname: good\ndescription: d\n---\nb")
	m.put(1, "/skills/broken/SKILL.md", "no frontmatter")
	m.put(1, "/skills/good/other/SKILL.md", "---\nname: other\ndescription: d\n---\nb")
	m.put(1, "/notes/SKILL.md", "---\nname: notes\ndescription: d\n---\nb")
	got, err := Service{m}.Entries(context.Background(), 1)
	if err != nil || len(got) != 1 || got[0].Name != "good" {
		t.Fatalf("%+v %v", got, err)
	}
}
