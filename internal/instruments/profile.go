// Package instruments holds what FloweySales knows about an instrument beyond
// its identity: the profile a provider states (the "profile" plugin
// capability), the platform taxonomy it maps to, each user's own
// classification overrides, and how members of a collection relate across
// providers. Provider facts and user overrides are kept apart: an override
// never changes a cached profile, and a profile never changes an override.
package instruments

import (
	"math"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"floweysales/internal/collections"
)

// Profile kinds, the same as pluginsdk.Profile*.
const (
	KindCompany   = "company"
	KindFund      = "fund"
	KindCommodity = "commodity"
	KindCrypto    = "crypto"
	KindIndex     = "index"
	KindCurrency  = "currency"
	KindOther     = "other"
)

var profileKinds = map[string]bool{
	KindCompany: true, KindFund: true, KindCommodity: true, KindCrypto: true, KindIndex: true, KindCurrency: true, KindOther: true,
}

// Profile is a provider's statement about one instrument; see
// pluginsdk.InstrumentProfile. Empty and nil fields are unknown.
type Profile struct {
	Symbol      string            `json:"symbol"`
	Kind        string            `json:"kind"`
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	ISIN        string            `json:"isin,omitempty"`
	Exchange    string            `json:"exchange,omitempty"`
	MIC         string            `json:"mic,omitempty"`
	Currency    string            `json:"currency,omitempty"`
	Country     string            `json:"country,omitempty"`
	Website     string            `json:"website,omitempty"`
	Sector      string            `json:"sector,omitempty"`
	Industry    string            `json:"industry,omitempty"`
	Company     *CompanyProfile   `json:"company,omitempty"`
	Fund        *FundProfile      `json:"fund,omitempty"`
	Commodity   *CommodityProfile `json:"commodity,omitempty"`
	Crypto      *CryptoProfile    `json:"crypto,omitempty"`
	AsOf        string            `json:"as_of,omitempty"`
	SourceURL   string            `json:"source_url,omitempty"`
	Provider    string            `json:"provider,omitempty"`
}

type CompanyProfile struct {
	Employees         *int64   `json:"employees,omitempty"`
	MarketCap         *float64 `json:"market_cap,omitempty"`
	SharesOutstanding *float64 `json:"shares_outstanding,omitempty"`
	IPODate           string   `json:"ipo_date,omitempty"`
	Headquarters      string   `json:"headquarters,omitempty"`
}

type FundProfile struct {
	Issuer        string        `json:"issuer,omitempty"`
	Category      string        `json:"category,omitempty"`
	AssetClass    string        `json:"asset_class,omitempty"`
	Benchmark     string        `json:"benchmark,omitempty"`
	ExpenseRatio  *float64      `json:"expense_ratio,omitempty"`
	TotalAssets   *float64      `json:"total_assets,omitempty"`
	InceptionDate string        `json:"inception_date,omitempty"`
	TopHoldings   []FundHolding `json:"top_holdings,omitempty"`
}

type FundHolding struct {
	Symbol string   `json:"symbol,omitempty"`
	Name   string   `json:"name,omitempty"`
	Weight *float64 `json:"weight,omitempty"`
}

type CommodityProfile struct {
	Underlying   string   `json:"underlying,omitempty"`
	Unit         string   `json:"unit,omitempty"`
	ContractSize *float64 `json:"contract_size,omitempty"`
	Expiry       string   `json:"expiry,omitempty"`
}

type CryptoProfile struct {
	Network           string   `json:"network,omitempty"`
	Categories        []string `json:"categories,omitempty"`
	CirculatingSupply *float64 `json:"circulating_supply,omitempty"`
	MaxSupply         *float64 `json:"max_supply,omitempty"`
	GenesisDate       string   `json:"genesis_date,omitempty"`
}

// Limits applied to plugin output.
const (
	MaxDescription = 4000
	MaxHoldings    = 25
	MaxCategories  = 20
	maxShortText   = 200
)

// CleanProfile makes plugin output safe to store and show. Plugin output is
// untrusted: an unknown kind becomes "other", a malformed code, date, address
// or number is dropped rather than repaired, text is trimmed and bounded, and
// parts that do not belong to the kind are removed. Provider and Symbol are
// set from the request, so a plugin cannot file a profile under another
// instrument. ok is false when nothing usable is left.
func CleanProfile(p Profile, provider, symbol string) (Profile, bool) {
	p.Provider = provider
	p.Symbol = symbol
	p.Kind = strings.ToLower(strings.TrimSpace(p.Kind))
	if !profileKinds[p.Kind] {
		p.Kind = KindOther
	}
	p.Name = short(p.Name)
	p.Description = bounded(p.Description, MaxDescription)
	p.ISIN = strings.ToUpper(strings.TrimSpace(p.ISIN))
	if !collections.ValidISIN(p.ISIN) {
		p.ISIN = ""
	}
	p.Exchange = short(p.Exchange)
	p.MIC = code(p.MIC, 4)
	p.Currency = code(p.Currency, 3)
	p.Country = code(p.Country, 2)
	p.Website = webURL(p.Website)
	p.SourceURL = webURL(p.SourceURL)
	p.Sector = short(p.Sector)
	p.Industry = short(p.Industry)
	p.AsOf = timestamp(p.AsOf)

	if p.Kind != KindCompany {
		p.Company = nil
	}
	if p.Kind != KindFund {
		p.Fund = nil
	}
	if p.Kind != KindCommodity {
		p.Commodity = nil
	}
	if p.Kind != KindCrypto {
		p.Crypto = nil
	}
	if c := p.Company; c != nil {
		if c.Employees != nil && *c.Employees < 0 {
			c.Employees = nil
		}
		c.MarketCap = positive(c.MarketCap)
		c.SharesOutstanding = positive(c.SharesOutstanding)
		c.IPODate = date(c.IPODate)
		c.Headquarters = short(c.Headquarters)
		if *c == (CompanyProfile{}) {
			p.Company = nil
		}
	}
	if f := p.Fund; f != nil {
		f.Issuer, f.Category, f.AssetClass, f.Benchmark = short(f.Issuer), short(f.Category), short(f.AssetClass), short(f.Benchmark)
		f.ExpenseRatio = percent(f.ExpenseRatio)
		f.TotalAssets = positive(f.TotalAssets)
		f.InceptionDate = date(f.InceptionDate)
		holdings := f.TopHoldings[:0]
		for _, h := range f.TopHoldings {
			h.Symbol, h.Name, h.Weight = short(h.Symbol), short(h.Name), percent(h.Weight)
			if (h.Symbol != "" || h.Name != "") && len(holdings) < MaxHoldings {
				holdings = append(holdings, h)
			}
		}
		f.TopHoldings = holdings
		if len(f.TopHoldings) == 0 {
			f.TopHoldings = nil
		}
		if f.Issuer == "" && f.Category == "" && f.AssetClass == "" && f.Benchmark == "" && f.ExpenseRatio == nil && f.TotalAssets == nil && f.InceptionDate == "" && f.TopHoldings == nil {
			p.Fund = nil
		}
	}
	if c := p.Commodity; c != nil {
		c.Underlying, c.Unit = short(c.Underlying), short(c.Unit)
		c.ContractSize = positive(c.ContractSize)
		c.Expiry = date(c.Expiry)
		if *c == (CommodityProfile{}) {
			p.Commodity = nil
		}
	}
	if c := p.Crypto; c != nil {
		c.Network = short(c.Network)
		var cats []string
		for _, cat := range c.Categories {
			if cat = short(cat); cat != "" && len(cats) < MaxCategories {
				cats = append(cats, cat)
			}
		}
		c.Categories = cats
		c.CirculatingSupply = positive(c.CirculatingSupply)
		c.MaxSupply = positive(c.MaxSupply)
		c.GenesisDate = date(c.GenesisDate)
		if c.Network == "" && c.Categories == nil && c.CirculatingSupply == nil && c.MaxSupply == nil && c.GenesisDate == "" {
			p.Crypto = nil
		}
	}
	empty := p.Name == "" && p.Description == "" && p.ISIN == "" && p.Exchange == "" && p.MIC == "" && p.Currency == "" &&
		p.Country == "" && p.Website == "" && p.Sector == "" && p.Industry == "" &&
		p.Company == nil && p.Fund == nil && p.Commodity == nil && p.Crypto == nil
	return p, !empty
}

func bounded(s string, max int) string {
	s = strings.TrimSpace(strings.ToValidUTF8(s, ""))
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:max-1])) + "…"
}

// short is single-line text of at most maxShortText characters.
func short(s string) string {
	return bounded(strings.Join(strings.Fields(s), " "), maxShortText)
}

func code(s string, n int) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if len(s) != n {
		return ""
	}
	for _, r := range s {
		if !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') {
			return ""
		}
	}
	return s
}

func webURL(s string) string {
	s = strings.TrimSpace(s)
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || len(s) > 500 {
		return ""
	}
	return u.String()
}

func date(s string) string {
	s = strings.TrimSpace(s)
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return ""
	}
	return s
}

func timestamp(s string) string {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
	if err != nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func finite(v *float64) bool { return v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) }

func positive(v *float64) *float64 {
	if !finite(v) || *v <= 0 {
		return nil
	}
	return v
}

func percent(v *float64) *float64 {
	if !finite(v) || *v < 0 || *v > 100 {
		return nil
	}
	return v
}
