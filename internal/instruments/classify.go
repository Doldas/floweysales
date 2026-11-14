package instruments

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"floweysales/internal/collections"
	"floweysales/internal/market"
)

// Key identifies an instrument the same way a collection member does.
type Key struct {
	Provider string `json:"provider"`
	Symbol   string `json:"symbol"`
}

func (k Key) String() string { return k.Provider + ":" + k.Symbol }

// KeyOf is the key of a member instrument.
func KeyOf(in collections.Instrument) Key { return Key{Provider: in.Provider, Symbol: in.Symbol} }

// NormalizeKey validates and canonicalizes a key.
func NormalizeKey(provider, symbol string) (Key, error) {
	p := market.ParseProvider(provider)
	if !p.Valid() {
		return Key{}, fmt.Errorf("%w: provider is required", ErrInvalid)
	}
	s, err := collections.CanonicalSymbol(symbol)
	if err != nil {
		return Key{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return Key{Provider: p.ID(), Symbol: s}, nil
}

// ErrInvalid marks input the caller must correct.
var ErrInvalid = errors.New("invalid classification")

// Override is one user's own classification of an instrument. It is private
// to that user and never changes the provider's profile. Empty fields keep
// what the profile or the member says.
type Override struct {
	Key
	AssetClass string    `json:"asset_class,omitempty"`
	Sector     string    `json:"sector,omitempty"`
	Industry   string    `json:"industry,omitempty"`
	Country    string    `json:"country,omitempty"`
	Themes     []string  `json:"themes"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Limits for overrides.
const (
	MaxThemes   = 10
	MaxThemeLen = 40
	MaxIndustry = 100
)

// NormalizeOverride validates an override. An override with nothing set is
// valid; storing it is the same as deleting it.
func NormalizeOverride(o Override) (Override, error) {
	k, err := NormalizeKey(o.Provider, o.Symbol)
	if err != nil {
		return o, err
	}
	o.Key = k
	o.AssetClass = strings.TrimSpace(o.AssetClass)
	if o.AssetClass != "" && !KnownAssetClass(o.AssetClass) {
		return o, fmt.Errorf("%w: unknown asset class %q", ErrInvalid, o.AssetClass)
	}
	o.Sector = strings.TrimSpace(o.Sector)
	if o.Sector != "" && !KnownSector(o.Sector) {
		return o, fmt.Errorf("%w: unknown sector %q", ErrInvalid, o.Sector)
	}
	o.Industry = strings.Join(strings.Fields(o.Industry), " ")
	if len([]rune(o.Industry)) > MaxIndustry {
		return o, fmt.Errorf("%w: industry is at most %d characters", ErrInvalid, MaxIndustry)
	}
	o.Country = strings.ToUpper(strings.TrimSpace(o.Country))
	if o.Country != "" && code(o.Country, 2) == "" {
		return o, fmt.Errorf("%w: country must be a 2-letter ISO 3166 code", ErrInvalid)
	}
	seen := map[string]bool{}
	themes := []string{}
	for _, t := range o.Themes {
		t = strings.Join(strings.Fields(t), " ")
		if t == "" || seen[strings.ToLower(t)] {
			continue
		}
		if len([]rune(t)) > MaxThemeLen {
			return o, fmt.Errorf("%w: a theme is at most %d characters", ErrInvalid, MaxThemeLen)
		}
		seen[strings.ToLower(t)] = true
		themes = append(themes, t)
	}
	if len(themes) > MaxThemes {
		return o, fmt.Errorf("%w: at most %d themes", ErrInvalid, MaxThemes)
	}
	o.Themes = themes
	return o, nil
}

// Empty reports whether the override sets nothing.
func (o Override) Empty() bool {
	return o.AssetClass == "" && o.Sector == "" && o.Industry == "" && o.Country == "" && len(o.Themes) == 0
}

// Sources of a classified value.
const (
	SourceUser    = "user"    // the user's own override
	SourceProfile = "profile" // the provider's profile, mapped to the taxonomy
	SourceMember  = "member"  // stored with the member when it was added
)

// Fact is one classified value and where it came from. Value is a taxonomy id
// (or a code or free text for country and industry); empty means unknown. Raw
// is the provider's own wording when it differs from the mapped value, so an
// unmapped provider sector is still visible.
type Fact struct {
	Value    string `json:"value,omitempty"`
	Source   string `json:"source,omitempty"`
	Provider string `json:"provider,omitempty"`
	Raw      string `json:"raw,omitempty"`
}

// Classification is the effective classification of one instrument for one
// user: the override where the user set one, otherwise the provider profile,
// otherwise what the member stored. Themes come only from the user.
type Classification struct {
	AssetClass Fact     `json:"asset_class"`
	Sector     Fact     `json:"sector"`
	Industry   Fact     `json:"industry"`
	Country    Fact     `json:"country"`
	Region     Fact     `json:"region"`
	Themes     []string `json:"themes"`
}

// Classify combines a member instrument, its cached profile (nil when none)
// and the user's override (nil when none).
func Classify(in collections.Instrument, p *Profile, o *Override) Classification {
	var c Classification
	user := func(v string) Fact { return Fact{Value: v, Source: SourceUser} }
	fromProfile := func(v, raw string) Fact {
		f := Fact{Value: v, Source: SourceProfile, Provider: p.Provider}
		if raw != "" && raw != v {
			f.Raw = raw
		}
		return f
	}

	switch {
	case o != nil && o.AssetClass != "":
		c.AssetClass = user(o.AssetClass)
	case p != nil && AssetClassOf(p.Kind, "") != "":
		c.AssetClass = fromProfile(AssetClassOf(p.Kind, ""), "")
	case AssetClassOf("", in.Type) != "":
		c.AssetClass = Fact{Value: AssetClassOf("", in.Type), Source: SourceMember}
	}

	switch {
	case o != nil && o.Sector != "":
		c.Sector = user(o.Sector)
	case p != nil && (p.Sector != "" || p.Industry != ""):
		raw := p.Sector
		if raw == "" {
			raw = p.Industry
		}
		c.Sector = fromProfile(MapSector(p.Sector, p.Industry), raw)
	}

	switch {
	case o != nil && o.Industry != "":
		c.Industry = user(o.Industry)
	case p != nil && p.Industry != "":
		c.Industry = fromProfile(p.Industry, "")
	}

	switch {
	case o != nil && o.Country != "":
		c.Country = user(o.Country)
	case p != nil && p.Country != "":
		c.Country = fromProfile(p.Country, "")
	case in.Country != "":
		c.Country = Fact{Value: in.Country, Source: SourceMember}
	}
	if c.Country.Value != "" {
		c.Region = Fact{Value: RegionOf(c.Country.Value), Source: c.Country.Source, Provider: c.Country.Provider}
		if c.Region.Value == "" {
			c.Region = Fact{}
		}
	}

	c.Themes = []string{}
	if o != nil {
		c.Themes = append(c.Themes, o.Themes...)
	}
	return c
}
