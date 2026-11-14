// Package collections holds Market Collections: user-owned, reusable sets of
// instruments (watch collections, model portfolios and, later, views of real
// portfolios). It owns the domain types, validation and the export format;
// storage implements Store on MySQL and MemoryStore serves tests.
//
// A collection belongs to exactly one user. Every Store method takes the
// owner's id and must never return or change another user's rows.
package collections

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"floweysales/internal/market"
)

// Kind separates what a collection means financially. The kinds share the
// storage but not the semantics: only a model portfolio carries weights, and
// a watch collection never implies an allocation.
type Kind string

const (
	// KindWatch is a set of instruments followed for monitoring and research. No weights.
	KindWatch Kind = "watch"
	// KindModel is a hypothetical allocation: explicit weights, no claim of ownership.
	KindModel Kind = "model"
	// KindLinked is a view derived from real portfolio positions. Reserved: it
	// needs a connected portfolio source and explicit permissions first.
	KindLinked Kind = "linked"
)

// Bounds that keep one user's collections cheap to list and filter in memory.
const (
	MaxCollectionsPerUser = 200
	MaxMembers            = 1000
	MaxNameLen            = 80
	MaxDescriptionLen     = 2000
	MaxCategoryLen        = 60
	MaxNoteLen            = 1000
	MaxTags               = 20
	MaxTagLen             = 40
	DefaultPageSize       = 100
	MaxPageSize           = 500
)

var (
	ErrNotFound        = errors.New("collection not found")
	ErrMemberNotFound  = errors.New("instrument is not in this collection")
	ErrNameTaken       = errors.New("you already have a collection with that name")
	ErrDuplicateMember = errors.New("the instrument is already in this collection")
	ErrLimit           = errors.New("collection limit reached")
	ErrInvalid         = errors.New("invalid collection request")
	ErrUnsupportedKind = errors.New("linked portfolio views are not available yet; create a watch collection or a model portfolio")
)

// invalid wraps ErrInvalid with a message a user can act on.
func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// Collection is one user's named set of instruments.
type Collection struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Kind        Kind      `json:"kind"`
	Category    string    `json:"category"`
	Tags        []string  `json:"tags"`
	Archived    bool      `json:"archived"`
	MemberCount int       `json:"member_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Instrument identifies one listing. Provider and Symbol are the identity the
// rest of FloweySales uses (the same ticker on two providers or exchanges is
// two instruments); the other fields are descriptive metadata recorded when
// the instrument was added, so a later identity layer can link listings
// across providers. They are never used to merge two members.
type Instrument struct {
	Provider string `json:"provider"`
	Symbol   string `json:"symbol"`
	Name     string `json:"name,omitempty"`
	Type     string `json:"type,omitempty"`
	Exchange string `json:"exchange,omitempty"`
	MIC      string `json:"mic,omitempty"`
	ISIN     string `json:"isin,omitempty"`
	Currency string `json:"currency,omitempty"`
	Country  string `json:"country,omitempty"`
}

// Key is the provider-qualified identity, "PROVIDER:SYMBOL".
func (i Instrument) Key() string { return i.Provider + ":" + i.Symbol }

// Member is one instrument in a collection. Weight is a hypothetical
// allocation in percent and exists only in model portfolios.
type Member struct {
	ID           int64      `json:"id"`
	CollectionID int64      `json:"collection_id"`
	Instrument   Instrument `json:"instrument"`
	Note         string     `json:"note"`
	Tags         []string   `json:"tags"`
	Weight       *float64   `json:"weight,omitempty"`
	AddedAt      time.Time  `json:"added_at"`
}

// Patch changes the given fields of a collection; nil leaves a field as is.
// The kind cannot change: a watch list does not become an allocation.
type Patch struct {
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	Category    *string   `json:"category"`
	Tags        *[]string `json:"tags"`
	Archived    *bool     `json:"archived"`
}

// MemberPatch changes a member's own annotations. ClearWeight removes a weight.
type MemberPatch struct {
	Note        *string   `json:"note"`
	Tags        *[]string `json:"tags"`
	Weight      *float64  `json:"weight"`
	ClearWeight bool      `json:"clear_weight"`
}

// Page selects a slice of a collection's members.
type Page struct {
	Limit  int
	Offset int
}

// Normalize applies the default and maximum page size.
func (p Page) Normalize() Page {
	if p.Limit <= 0 {
		p.Limit = DefaultPageSize
	}
	if p.Limit > MaxPageSize {
		p.Limit = MaxPageSize
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	return p
}

// Store persists collections. Implementations check ownership on every call,
// enforce MaxCollectionsPerUser and MaxMembers, unique names per user (case
// insensitive) and one member per instrument key per collection.
type Store interface {
	ListCollections(ctx context.Context, userID int64) ([]Collection, error)
	GetCollection(ctx context.Context, userID, id int64) (Collection, error)
	CreateCollection(ctx context.Context, userID int64, c Collection) (Collection, error)
	UpdateCollection(ctx context.Context, userID int64, c Collection) (Collection, error)
	DeleteCollection(ctx context.Context, userID, id int64) error
	ListMembers(ctx context.Context, userID, collectionID int64, page Page) ([]Member, int, error)
	AllMembers(ctx context.Context, userID, collectionID int64) ([]Member, error)
	AddMember(ctx context.Context, userID, collectionID int64, m Member) (Member, error)
	UpdateMember(ctx context.Context, userID int64, m Member) (Member, error)
	RemoveMember(ctx context.Context, userID, collectionID, memberID int64) error
	// TransferMember copies a member, with its note and tags, into another of
	// the user's collections; with move it also removes the original, in one step.
	TransferMember(ctx context.Context, userID, fromID, memberID, toID int64, move bool, weight *float64) (Member, error)
	// CollectionsContaining lists the user's collections that hold provider:symbol.
	CollectionsContaining(ctx context.Context, userID int64, provider, symbol string) ([]Collection, error)
}

// ParseKind reads a kind; empty is a watch collection.
func ParseKind(s string) (Kind, error) {
	switch k := Kind(strings.ToLower(strings.TrimSpace(s))); k {
	case "", KindWatch:
		return KindWatch, nil
	case KindModel:
		return KindModel, nil
	case KindLinked:
		return KindLinked, ErrUnsupportedKind
	default:
		return "", invalid("kind must be watch or model")
	}
}

func cleanText(field, s string, max int, multiline bool) (string, error) {
	s = strings.TrimSpace(s)
	if !utf8.ValidString(s) {
		return "", invalid("%s is not valid text", field)
	}
	if utf8.RuneCountInString(s) > max {
		return "", invalid("%s must be at most %d characters", field, max)
	}
	for _, r := range s {
		if unicode.IsControl(r) && !(multiline && (r == '\n' || r == '\t')) {
			return "", invalid("%s contains a control character", field)
		}
	}
	return s, nil
}

// NormalizeName trims a collection name and checks its length.
func NormalizeName(s string) (string, error) {
	name, err := cleanText("name", s, MaxNameLen, false)
	if err == nil && name == "" {
		err = invalid("name is required")
	}
	return name, err
}

// NormalizeTags trims tags, drops empties and case-insensitive duplicates and
// keeps the first spelling.
func NormalizeTags(tags []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, t := range tags {
		t, err := cleanText("tag", t, MaxTagLen, false)
		if err != nil {
			return nil, err
		}
		key := strings.ToLower(t)
		if t == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	if len(out) > MaxTags {
		return nil, invalid("at most %d tags", MaxTags)
	}
	return out, nil
}

// NormalizeCollection validates a new collection's user-editable fields.
func NormalizeCollection(c Collection) (Collection, error) {
	var err error
	if c.Name, err = NormalizeName(c.Name); err != nil {
		return c, err
	}
	if c.Description, err = cleanText("description", c.Description, MaxDescriptionLen, true); err != nil {
		return c, err
	}
	if c.Category, err = cleanText("category", c.Category, MaxCategoryLen, false); err != nil {
		return c, err
	}
	if c.Tags, err = NormalizeTags(c.Tags); err != nil {
		return c, err
	}
	if c.Kind, err = ParseKind(string(c.Kind)); err != nil {
		return c, err
	}
	return c, nil
}

// Apply returns c with the patch applied and validated.
func (p Patch) Apply(c Collection) (Collection, error) {
	if p.Name != nil {
		c.Name = *p.Name
	}
	if p.Description != nil {
		c.Description = *p.Description
	}
	if p.Category != nil {
		c.Category = *p.Category
	}
	if p.Tags != nil {
		c.Tags = *p.Tags
	}
	if p.Archived != nil {
		c.Archived = *p.Archived
	}
	return NormalizeCollection(c)
}

// instrumentTypes are the asset types a member may declare; empty is unknown.
var instrumentTypes = map[string]bool{
	"equity": true, "etf": true, "fund": true, "index": true, "commodity": true, "future": true,
	"crypto": true, "currency": true, "bond": true, "option": true, "other": true,
}

// NormalizeInstrument validates the identity and canonicalizes the metadata.
// Unknown metadata stays empty: nothing is guessed from the symbol.
func NormalizeInstrument(in Instrument) (Instrument, error) {
	symbol, err := CanonicalSymbol(in.Symbol)
	if err != nil {
		return in, err
	}
	in.Symbol = symbol
	provider := market.ParseProvider(in.Provider)
	if !provider.Valid() {
		return in, invalid("provider is required (for example the one the search result came from)")
	}
	in.Provider = provider.ID()
	if in.Name, err = cleanText("instrument name", in.Name, 200, false); err != nil {
		return in, err
	}
	if in.Exchange, err = cleanText("exchange", in.Exchange, 60, false); err != nil {
		return in, err
	}
	in.Type = strings.ToLower(strings.TrimSpace(in.Type))
	if in.Type != "" && !instrumentTypes[in.Type] {
		in.Type = "other"
	}
	in.MIC = strings.ToUpper(strings.TrimSpace(in.MIC))
	if in.MIC != "" && !isCode(in.MIC, 4, 4) {
		return in, invalid("MIC must be a 4-character ISO 10383 code")
	}
	in.ISIN = strings.ToUpper(strings.TrimSpace(in.ISIN))
	if in.ISIN != "" && !ValidISIN(in.ISIN) {
		return in, invalid("ISIN %q fails its check digit", in.ISIN)
	}
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if in.Currency != "" && !isCode(in.Currency, 3, 3) {
		return in, invalid("currency must be a 3-letter ISO 4217 code")
	}
	in.Country = strings.ToUpper(strings.TrimSpace(in.Country))
	if in.Country != "" && !isCode(in.Country, 2, 2) {
		return in, invalid("country must be a 2-letter ISO 3166 code")
	}
	return in, nil
}

// CanonicalSymbol upper-cases a symbol. It accepts what market.CanonicalSymbol
// accepts plus '^' and '=', which index (^GSPC) and futures (GC=F) symbols use:
// a collection may hold instruments the tracker cannot follow.
func CanonicalSymbol(raw string) (string, error) {
	symbol := strings.ToUpper(strings.TrimSpace(raw))
	if len(symbol) == 0 || len(symbol) > 40 {
		return "", invalid("symbol must be 1-40 characters")
	}
	for _, r := range symbol {
		if !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && !strings.ContainsRune(".-_:/^=", r) {
			return "", invalid("invalid symbol character %q", r)
		}
	}
	return symbol, nil
}

func isCode(s string, minLen, maxLen int) bool {
	if len(s) < minLen || len(s) > maxLen {
		return false
	}
	for _, r := range s {
		if !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// ValidISIN checks an ISIN's shape and its Luhn check digit (ISO 6166).
func ValidISIN(s string) bool {
	if len(s) != 12 || !isCode(s, 12, 12) || s[0] < 'A' || s[0] > 'Z' || s[1] < 'A' || s[1] > 'Z' || s[11] < '0' || s[11] > '9' {
		return false
	}
	var digits []int
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			v := int(r-'A') + 10
			digits = append(digits, v/10, v%10)
		} else {
			digits = append(digits, int(r-'0'))
		}
	}
	sum := 0
	for i := len(digits) - 1; i >= 0; i-- {
		d := digits[i]
		if (len(digits)-1-i)%2 == 1 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
}

// checkWeight validates a member weight against the collection's kind.
func checkWeight(kind Kind, w *float64) error {
	if w == nil {
		return nil
	}
	if kind != KindModel {
		return invalid("only model portfolios have weights; a watch collection is not an allocation")
	}
	if math.IsNaN(*w) || *w <= 0 || *w > 100 {
		return invalid("weight must be a percentage above 0 and at most 100")
	}
	return nil
}

// weightTolerance absorbs rounding when hypothetical weights add up to 100%.
const weightTolerance = 1e-6

// checkWeightSum refuses weights that add up to more than 100%.
func checkWeightSum(members []Member, exceptID int64, add *float64) error {
	if add == nil {
		return nil
	}
	sum := *add
	for _, m := range members {
		if m.ID != exceptID && m.Weight != nil {
			sum += *m.Weight
		}
	}
	if sum > 100+weightTolerance {
		return invalid("weights would add up to %.4g%%; a model portfolio cannot exceed 100%%", sum)
	}
	return nil
}

// NormalizeMember validates a new member for a collection of the given kind.
func NormalizeMember(kind Kind, m Member) (Member, error) {
	var err error
	if m.Instrument, err = NormalizeInstrument(m.Instrument); err != nil {
		return m, err
	}
	if m.Note, err = cleanText("note", m.Note, MaxNoteLen, true); err != nil {
		return m, err
	}
	if m.Tags, err = NormalizeTags(m.Tags); err != nil {
		return m, err
	}
	return m, checkWeight(kind, m.Weight)
}
