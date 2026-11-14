package collections

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Service applies validation and the financial rules of each kind on top of
// a Store. HTTP handlers, agent tools and flow nodes all go through it, so a
// collection behaves the same wherever it is used.
type Service struct {
	Store Store
}

// Archive filters on the archived flag.
type Archive string

const (
	ArchiveExclude Archive = "exclude" // the default: active collections only
	ArchiveInclude Archive = "include"
	ArchiveOnly    Archive = "only"
)

// Filter narrows and orders a user's collections.
type Filter struct {
	Query    string // matched against name, description, category and tags
	Kind     Kind
	Category string
	Tag      string
	Archive  Archive
	Sort     string // name (default), updated, created, members
}

// List returns the user's collections that match the filter.
func (s Service) List(ctx context.Context, userID int64, f Filter) ([]Collection, error) {
	all, err := s.Store.ListCollections(ctx, userID)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(f.Query))
	out := []Collection{}
	for _, c := range all {
		switch f.Archive {
		case ArchiveInclude:
		case ArchiveOnly:
			if !c.Archived {
				continue
			}
		default:
			if c.Archived {
				continue
			}
		}
		if f.Kind != "" && c.Kind != f.Kind {
			continue
		}
		if f.Category != "" && !strings.EqualFold(c.Category, f.Category) {
			continue
		}
		if f.Tag != "" && !hasTag(c.Tags, f.Tag) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(c.Name+"\n"+c.Description+"\n"+c.Category+"\n"+strings.Join(c.Tags, "\n")), q) {
			continue
		}
		out = append(out, c)
	}
	less := func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) }
	switch f.Sort {
	case "updated":
		less = func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) }
	case "created":
		less = func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) }
	case "members":
		less = func(i, j int) bool { return out[i].MemberCount > out[j].MemberCount }
	}
	sort.SliceStable(out, less)
	return out, nil
}

func hasTag(tags []string, tag string) bool {
	for _, t := range tags {
		if strings.EqualFold(t, tag) {
			return true
		}
	}
	return false
}

// Get returns one of the user's collections.
func (s Service) Get(ctx context.Context, userID, id int64) (Collection, error) {
	return s.Store.GetCollection(ctx, userID, id)
}

// Create validates and saves a new collection.
func (s Service) Create(ctx context.Context, userID int64, c Collection) (Collection, error) {
	c, err := NormalizeCollection(c)
	if err != nil {
		return Collection{}, err
	}
	return s.Store.CreateCollection(ctx, userID, c)
}

// Update applies a patch to one of the user's collections.
func (s Service) Update(ctx context.Context, userID, id int64, p Patch) (Collection, error) {
	c, err := s.Store.GetCollection(ctx, userID, id)
	if err != nil {
		return Collection{}, err
	}
	if c, err = p.Apply(c); err != nil {
		return Collection{}, err
	}
	return s.Store.UpdateCollection(ctx, userID, c)
}

// Delete removes a collection and its members. The instruments themselves,
// and any other collection that holds them, are untouched.
func (s Service) Delete(ctx context.Context, userID, id int64) error {
	return s.Store.DeleteCollection(ctx, userID, id)
}

// Members returns one page of a collection's members and the total count.
func (s Service) Members(ctx context.Context, userID, id int64, page Page) ([]Member, int, error) {
	return s.Store.ListMembers(ctx, userID, id, page.Normalize())
}

// AddMember adds an instrument to a collection.
func (s Service) AddMember(ctx context.Context, userID, id int64, m Member) (Member, error) {
	c, err := s.Store.GetCollection(ctx, userID, id)
	if err != nil {
		return Member{}, err
	}
	if m, err = NormalizeMember(c.Kind, m); err != nil {
		return Member{}, err
	}
	if m.Weight != nil {
		members, err := s.Store.AllMembers(ctx, userID, id)
		if err != nil {
			return Member{}, err
		}
		if err := checkWeightSum(members, 0, m.Weight); err != nil {
			return Member{}, err
		}
	}
	return s.Store.AddMember(ctx, userID, id, m)
}

// UpdateMember changes a member's note, tags or weight.
func (s Service) UpdateMember(ctx context.Context, userID, id, memberID int64, p MemberPatch) (Member, error) {
	c, err := s.Store.GetCollection(ctx, userID, id)
	if err != nil {
		return Member{}, err
	}
	members, err := s.Store.AllMembers(ctx, userID, id)
	if err != nil {
		return Member{}, err
	}
	var m *Member
	for i := range members {
		if members[i].ID == memberID {
			m = &members[i]
		}
	}
	if m == nil {
		return Member{}, ErrMemberNotFound
	}
	if p.Note != nil {
		if m.Note, err = cleanText("note", *p.Note, MaxNoteLen, true); err != nil {
			return Member{}, err
		}
	}
	if p.Tags != nil {
		if m.Tags, err = NormalizeTags(*p.Tags); err != nil {
			return Member{}, err
		}
	}
	switch {
	case p.ClearWeight:
		m.Weight = nil
	case p.Weight != nil:
		if err := checkWeight(c.Kind, p.Weight); err != nil {
			return Member{}, err
		}
		if err := checkWeightSum(members, memberID, p.Weight); err != nil {
			return Member{}, err
		}
		w := *p.Weight
		m.Weight = &w
	}
	return s.Store.UpdateMember(ctx, userID, *m)
}

// RemoveMember takes an instrument out of one collection only.
func (s Service) RemoveMember(ctx context.Context, userID, id, memberID int64) error {
	return s.Store.RemoveMember(ctx, userID, id, memberID)
}

// Transfer copies or moves a member into another of the user's collections.
// A weight never travels: a target model portfolio takes the given weight
// (or none), and a watch collection takes none.
func (s Service) Transfer(ctx context.Context, userID, fromID, memberID, toID int64, move bool, weight *float64) (Member, error) {
	if fromID == toID {
		return Member{}, invalid("choose a different collection")
	}
	if _, err := s.Store.GetCollection(ctx, userID, fromID); err != nil {
		return Member{}, err
	}
	target, err := s.Store.GetCollection(ctx, userID, toID)
	if err != nil {
		return Member{}, err
	}
	if err := checkWeight(target.Kind, weight); err != nil {
		return Member{}, err
	}
	if weight != nil {
		members, err := s.Store.AllMembers(ctx, userID, toID)
		if err != nil {
			return Member{}, err
		}
		if err := checkWeightSum(members, 0, weight); err != nil {
			return Member{}, err
		}
	}
	return s.Store.TransferMember(ctx, userID, fromID, memberID, toID, move, weight)
}

// Containing lists the user's collections that hold an instrument.
func (s Service) Containing(ctx context.Context, userID int64, in Instrument) ([]Collection, error) {
	in, err := NormalizeInstrument(Instrument{Provider: in.Provider, Symbol: in.Symbol})
	if err != nil {
		return nil, err
	}
	return s.Store.CollectionsContaining(ctx, userID, in.Provider, in.Symbol)
}

// ExportFormat and ExportVersion identify the export document (see docs/market-collections.md).
const (
	ExportFormat       = "floweysales.collections"
	ExportVersion      = 1
	MaxImportedPerFile = 50
)

// Document is the export and import file.
type Document struct {
	Format      string             `json:"format"`
	Version     int                `json:"version"`
	ExportedAt  time.Time          `json:"exported_at"`
	Collections []ExportCollection `json:"collections"`
}

// ExportCollection is one collection in a Document. Ids are left out: they
// belong to the installation that wrote the file.
type ExportCollection struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Kind        Kind           `json:"kind"`
	Category    string         `json:"category,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Archived    bool           `json:"archived,omitempty"`
	Members     []ExportMember `json:"members"`
}

// ExportMember is one instrument in an exported collection.
type ExportMember struct {
	Instrument Instrument `json:"instrument"`
	Note       string     `json:"note,omitempty"`
	Tags       []string   `json:"tags,omitempty"`
	Weight     *float64   `json:"weight,omitempty"`
}

// Export writes the given collections, or all of the user's when ids is empty.
func (s Service) Export(ctx context.Context, userID int64, ids []int64, now time.Time) (Document, error) {
	var picked []Collection
	if len(ids) == 0 {
		all, err := s.Store.ListCollections(ctx, userID)
		if err != nil {
			return Document{}, err
		}
		picked = all
	} else {
		for _, id := range ids {
			c, err := s.Store.GetCollection(ctx, userID, id)
			if err != nil {
				return Document{}, err
			}
			picked = append(picked, c)
		}
	}
	doc := Document{Format: ExportFormat, Version: ExportVersion, ExportedAt: now.UTC(), Collections: []ExportCollection{}}
	for _, c := range picked {
		members, err := s.Store.AllMembers(ctx, userID, c.ID)
		if err != nil {
			return Document{}, err
		}
		ec := ExportCollection{Name: c.Name, Description: c.Description, Kind: c.Kind, Category: c.Category, Tags: c.Tags, Archived: c.Archived, Members: []ExportMember{}}
		for _, m := range members {
			ec.Members = append(ec.Members, ExportMember{Instrument: m.Instrument, Note: m.Note, Tags: m.Tags, Weight: m.Weight})
		}
		doc.Collections = append(doc.Collections, ec)
	}
	return doc, nil
}

// OnConflict says what an import does with a name the user already has.
type OnConflict string

const (
	ConflictRename OnConflict = "rename" // "Technology" becomes "Technology (2)"
	ConflictSkip   OnConflict = "skip"
)

// ImportResult reports what an import did, per collection in the file.
type ImportResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // created, renamed or skipped
	ID      int64  `json:"id,omitempty"`
	SavedAs string `json:"saved_as,omitempty"`
	Members int    `json:"members"`
}

// Import validates the whole document first, so a bad file changes nothing,
// then creates one new collection per entry. Existing collections are never
// merged into or overwritten.
func (s Service) Import(ctx context.Context, userID int64, doc Document, onConflict OnConflict) ([]ImportResult, error) {
	if doc.Format != ExportFormat {
		return nil, invalid("not a FloweySales collections file (format must be %q)", ExportFormat)
	}
	if doc.Version != ExportVersion {
		return nil, invalid("unsupported file version %d; this installation reads version %d", doc.Version, ExportVersion)
	}
	if len(doc.Collections) == 0 || len(doc.Collections) > MaxImportedPerFile {
		return nil, invalid("a file holds 1 to %d collections", MaxImportedPerFile)
	}
	if onConflict != ConflictSkip {
		onConflict = ConflictRename
	}
	type prepared struct {
		c       Collection
		members []Member
	}
	var all []prepared
	for i, ec := range doc.Collections {
		c, err := NormalizeCollection(Collection{Name: ec.Name, Description: ec.Description, Kind: ec.Kind, Category: ec.Category, Tags: ec.Tags, Archived: ec.Archived})
		if err != nil {
			return nil, fmt.Errorf("collection %d: %w", i+1, err)
		}
		if len(ec.Members) > MaxMembers {
			return nil, invalid("collection %q has more than %d instruments", c.Name, MaxMembers)
		}
		seen := map[string]bool{}
		var members []Member
		for j, em := range ec.Members {
			m, err := NormalizeMember(c.Kind, Member{Instrument: em.Instrument, Note: em.Note, Tags: em.Tags, Weight: em.Weight})
			if err != nil {
				return nil, fmt.Errorf("collection %q, instrument %d: %w", c.Name, j+1, err)
			}
			if seen[m.Instrument.Key()] {
				return nil, invalid("collection %q lists %s twice", c.Name, m.Instrument.Key())
			}
			seen[m.Instrument.Key()] = true
			members = append(members, m)
		}
		if err := checkWeightSum(members, -1, new(float64)); err != nil {
			return nil, fmt.Errorf("collection %q: %w", c.Name, err)
		}
		all = append(all, prepared{c, members})
	}

	existing, err := s.Store.ListCollections(ctx, userID)
	if err != nil {
		return nil, err
	}
	taken := map[string]bool{}
	for _, c := range existing {
		taken[strings.ToLower(c.Name)] = true
	}
	var results []ImportResult
	for _, p := range all {
		res := ImportResult{Name: p.c.Name, Status: "created"}
		if taken[strings.ToLower(p.c.Name)] {
			if onConflict == ConflictSkip {
				res.Status = "skipped"
				results = append(results, res)
				continue
			}
			p.c.Name = freeName(p.c.Name, taken)
			res.Status = "renamed"
		}
		created, err := s.Store.CreateCollection(ctx, userID, p.c)
		if err != nil {
			return results, fmt.Errorf("saving %q: %w", p.c.Name, err)
		}
		taken[strings.ToLower(created.Name)] = true
		res.ID, res.SavedAs = created.ID, created.Name
		for _, m := range p.members {
			if _, err := s.Store.AddMember(ctx, userID, created.ID, m); err != nil && !errors.Is(err, ErrDuplicateMember) {
				return results, fmt.Errorf("saving %q: %w", created.Name, err)
			}
			res.Members++
		}
		results = append(results, res)
	}
	return results, nil
}

// freeName appends " (2)", " (3)" … until the name is unused and still fits.
func freeName(name string, taken map[string]bool) string {
	for n := 2; ; n++ {
		suffix := fmt.Sprintf(" (%d)", n)
		base := []rune(name)
		if max := MaxNameLen - len([]rune(suffix)); len(base) > max {
			base = base[:max]
		}
		candidate := string(base) + suffix
		if !taken[strings.ToLower(candidate)] {
			return candidate
		}
	}
}
