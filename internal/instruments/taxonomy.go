package instruments

import "strings"

// The platform taxonomy. Providers classify instruments in their own words;
// FloweySales maps those words onto a small fixed set so collections can be
// compared across providers. A value that has no mapping stays unmapped and
// is shown as the provider wrote it, never forced into the nearest bucket.

// Term is one value of a taxonomy dimension.
type Term struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// AssetClasses, in display order.
var AssetClasses = []Term{
	{"equity", "Equity"}, {"fund", "Fund"}, {"fixed_income", "Fixed income"}, {"commodity", "Commodity"},
	{"crypto", "Crypto"}, {"currency", "Currency"}, {"index", "Index"}, {"other", "Other"},
}

// Sectors, in display order. The names are FloweySales' own.
var Sectors = []Term{
	{"technology", "Technology"}, {"health_care", "Health care"}, {"financials", "Financials"},
	{"consumer_discretionary", "Consumer discretionary"}, {"consumer_staples", "Consumer staples"},
	{"industrials", "Industrials"}, {"energy", "Energy"}, {"materials", "Materials"}, {"utilities", "Utilities"},
	{"real_estate", "Real estate"}, {"communication", "Communication"},
}

// Regions, in display order.
var Regions = []Term{
	{"north_america", "North America"}, {"latin_america", "Latin America"}, {"europe", "Europe"},
	{"middle_east_africa", "Middle East and Africa"}, {"asia_pacific", "Asia-Pacific"},
}

func known(terms []Term, id string) bool {
	for _, t := range terms {
		if t.ID == id {
			return true
		}
	}
	return false
}

// KnownAssetClass, KnownSector and KnownRegion validate ids.
func KnownAssetClass(id string) bool { return known(AssetClasses, id) }
func KnownSector(id string) bool     { return known(Sectors, id) }
func KnownRegion(id string) bool     { return known(Regions, id) }

// providerSectors maps provider sector and industry wording, lower case, to a
// sector. Shared across providers: Yahoo, Finnhub and most data vendors use
// overlapping words. Extend it with real spellings only.
var providerSectors = map[string]string{}

func init() {
	for sector, words := range map[string][]string{
		"technology": {"technology", "information technology", "tech", "semiconductors", "semiconductor", "software",
			"software & services", "technology hardware", "technology hardware & equipment", "electronic technology",
			"electronic equipment, instruments & components", "it services", "communications equipment"},
		"health_care": {"healthcare", "health care", "pharmaceuticals", "pharmaceutical", "biotechnology",
			"life sciences tools & services", "health care providers & services", "health care equipment & supplies",
			"medical devices", "health technology"},
		"financials": {"financial services", "financials", "financial", "banking", "banks", "insurance",
			"capital markets", "diversified financial services", "consumer finance", "finance"},
		"consumer_discretionary": {"consumer cyclical", "consumer discretionary", "retail", "retail trade", "automobiles",
			"auto components", "hotels, restaurants & leisure", "textiles, apparel & luxury goods", "leisure products",
			"diversified consumer services", "household durables", "distributors", "consumer durables"},
		"consumer_staples": {"consumer defensive", "consumer staples", "food products", "beverages", "tobacco",
			"household products", "personal products", "food & staples retailing", "consumer non-durables"},
		"industrials": {"industrials", "industrial", "aerospace & defense", "machinery", "airlines", "logistics & transportation",
			"road & rail", "construction", "building", "electrical equipment", "industrial conglomerates", "marine",
			"professional services", "trading companies & distributors", "commercial services & supplies", "transportation",
			"producer manufacturing"},
		"energy":    {"energy", "oil & gas", "oil, gas & consumable fuels", "energy equipment & services", "energy minerals"},
		"materials": {"basic materials", "materials", "chemicals", "metals & mining", "packaging", "non-energy minerals", "process industries"},
		"utilities": {"utilities", "utility"},
		"real_estate": {"real estate", "reits", "reit", "equity real estate investment trusts (reits)",
			"real estate management & development"},
		"communication": {"communication services", "communications", "communication", "telecommunication",
			"telecommunication services", "telecommunications", "media", "entertainment", "interactive media & services"},
	} {
		for _, w := range words {
			providerSectors[w] = sector
		}
	}
}

// MapSector maps a provider's sector, or failing that its industry, to a
// platform sector. It returns "" when neither has a mapping.
func MapSector(sector, industry string) string {
	for _, raw := range []string{sector, industry} {
		if id, ok := providerSectors[strings.ToLower(strings.Join(strings.Fields(raw), " "))]; ok {
			return id
		}
	}
	return ""
}

// AssetClassOf derives an asset class from a profile kind, or failing that
// from a member's stored asset type. It returns "" when neither says.
func AssetClassOf(profileKind, memberType string) string {
	switch profileKind {
	case KindCompany:
		return "equity"
	case KindFund:
		return "fund"
	case KindCommodity:
		return "commodity"
	case KindCrypto:
		return "crypto"
	case KindIndex:
		return "index"
	case KindCurrency:
		return "currency"
	}
	switch memberType {
	case "equity":
		return "equity"
	case "etf", "fund":
		return "fund"
	case "bond":
		return "fixed_income"
	case "commodity", "future":
		return "commodity"
	case "crypto":
		return "crypto"
	case "currency":
		return "currency"
	case "index":
		return "index"
	case "option", "other":
		return "other"
	}
	return ""
}

var countryRegions = map[string]string{}

func init() {
	for region, codes := range map[string]string{
		"north_america":      "US CA BM",
		"latin_america":      "MX BR AR CL CO PE UY VE EC BO PY CR PA GT DO JM TT BS KY PR",
		"europe":             "SE NO DK FI IS GB IE DE FR NL BE LU CH AT IT ES PT GR PL CZ SK HU RO BG HR SI EE LV LT CY MT MC LI JE GG IM UA RS FO GI",
		"middle_east_africa": "IL AE SA QA KW BH OM JO EG MA TN NG KE GH ZA TR",
		"asia_pacific":       "JP CN HK TW KR SG IN ID MY TH PH VN AU NZ PK BD LK KZ MO",
	} {
		for _, c := range strings.Fields(codes) {
			countryRegions[c] = region
		}
	}
}

// RegionOf maps an ISO 3166-1 alpha-2 country code to a region, or "".
func RegionOf(country string) string {
	return countryRegions[strings.ToUpper(strings.TrimSpace(country))]
}
