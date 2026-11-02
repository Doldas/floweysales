package market

import (
	"regexp"
	"sort"
	"strings"
)

// The world view links headlines to the parts of the market they tend to
// move. The links are a fixed, reviewable table (not model output): a theme
// says which sectors, commodities and rates are exposed and in which
// direction if the theme's driver rises. A headline carries no verdict on
// its own; the table says who is exposed, not what will happen.

// Exposure roles, read as "if the theme's driver rises".
const (
	RoleBenefits = "benefits" // tends to gain
	RoleHurt     = "hurt"     // tends to lose
	RoleMixed    = "mixed"    // depends on the details
)

// Exposure kinds.
const (
	KindSector    = "sector"
	KindCommodity = "commodity"
	KindMacro     = "macro"
)

// Exposure is one part of the market a theme touches.
type Exposure struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Role string `json:"role"`
	Why  string `json:"why"`
}

// Theme is a recurring driver in the news: what it is called, the words that
// mark a headline as belonging to it, and who is exposed.
type Theme struct {
	ID        string     `json:"id"`
	Label     string     `json:"label"`
	Driver    string     `json:"driver"` // what "rises" means for the roles
	Query     string     `json:"-"`      // web search query for fresh headlines
	Keywords  []string   `json:"-"`
	Exposures []Exposure `json:"exposures"`
}

// Sector names used by exposures and SectorOf (GICS style).
const (
	SectorEnergy      = "Energy"
	SectorMaterials   = "Materials"
	SectorIndustrials = "Industrials"
	SectorDiscret     = "Consumer Discretionary"
	SectorStaples     = "Consumer Staples"
	SectorHealth      = "Health Care"
	SectorFinancials  = "Financials"
	SectorTech        = "Information Technology"
	SectorComms       = "Communication Services"
	SectorUtilities   = "Utilities"
	SectorRealEstate  = "Real Estate"
)

// Sectors lists the sector names in display order.
var Sectors = []string{SectorEnergy, SectorMaterials, SectorIndustrials, SectorDiscret, SectorStaples, SectorHealth,
	SectorFinancials, SectorTech, SectorComms, SectorUtilities, SectorRealEstate}

func sec(name, role, why string) Exposure { return Exposure{KindSector, name, role, why} }
func com(name, role, why string) Exposure { return Exposure{KindCommodity, name, role, why} }
func mac(name, role, why string) Exposure { return Exposure{KindMacro, name, role, why} }

// WorldThemes is the table of themes. Keywords are matched as whole words or
// phrases in lower case.
var WorldThemes = []Theme{
	{ID: "oil_energy", Label: "Oil and energy prices", Driver: "oil and gas prices",
		Query:    "oil price OPEC crude gas market news today",
		Keywords: []string{"oil", "crude", "brent", "wti", "opec", "natural gas", "lng", "refinery", "gasoline", "energy prices", "pipeline"},
		Exposures: []Exposure{
			sec(SectorEnergy, RoleBenefits, "Producers and services earn more per barrel."),
			sec(SectorIndustrials, RoleHurt, "Airlines, shipping and freight carry fuel as a large cost."),
			sec(SectorDiscret, RoleHurt, "Fuel costs squeeze household budgets and travel demand."),
			sec(SectorMaterials, RoleHurt, "Energy-heavy chemicals and fertilizer producers pay more for input."),
			com("Crude oil", RoleBenefits, "The price itself."),
			com("Natural gas", RoleBenefits, "Often moves with oil and with weather."),
			mac("Inflation expectations", RoleBenefits, "Energy feeds directly into headline inflation."),
		}},
	{ID: "rates_inflation", Label: "Interest rates and inflation", Driver: "interest rates and inflation",
		Query:    "Federal Reserve interest rates inflation central bank news today",
		Keywords: []string{"fed", "federal reserve", "interest rate", "rate hike", "rate cut", "ecb", "riksbank", "boe", "bank of england", "central bank", "inflation", "cpi", "pce", "treasury yield", "bond yield", "yields"},
		Exposures: []Exposure{
			sec(SectorTech, RoleHurt, "Long-dated growth earnings are discounted harder when yields rise."),
			sec(SectorRealEstate, RoleHurt, "Higher financing costs and rates compete with property yields."),
			sec(SectorUtilities, RoleHurt, "Debt-heavy, bond-like dividend payers lose appeal against safer yields."),
			sec(SectorFinancials, RoleMixed, "Banks gain on wider margins but face credit losses if growth slows."),
			com("Gold", RoleHurt, "Pays no yield, so real rates are its opportunity cost."),
			mac("US dollar", RoleBenefits, "Higher US yields attract capital."),
		}},
	{ID: "geopolitics", Label: "Conflict and geopolitics", Driver: "geopolitical tension",
		Query:    "war conflict sanctions geopolitical tension markets news today",
		Keywords: []string{"war", "invasion", "missile", "sanctions", "ceasefire", "geopolitical", "military", "nato", "ukraine", "russia", "middle east", "iran", "israel", "taiwan", "strait", "red sea", "embargo"},
		Exposures: []Exposure{
			sec(SectorIndustrials, RoleBenefits, "Defense and aerospace contractors see higher orders."),
			sec(SectorEnergy, RoleBenefits, "Supply risk lifts oil and gas prices."),
			sec(SectorDiscret, RoleHurt, "Travel and consumer spending weaken under uncertainty."),
			com("Gold", RoleBenefits, "A traditional safe haven."),
			com("Crude oil", RoleBenefits, "Risk premium on supply routes and producers."),
			mac("Equity volatility", RoleBenefits, "Risk-off moves lift volatility."),
		}},
	{ID: "trade_tariffs", Label: "Trade and tariffs", Driver: "tariffs and trade barriers",
		Query:    "tariffs trade war export controls supply chain news today",
		Keywords: []string{"tariff", "tariffs", "trade war", "trade deal", "export controls", "export ban", "import duties", "trade barriers", "supply chain", "customs", "wto", "free trade"},
		Exposures: []Exposure{
			sec(SectorIndustrials, RoleHurt, "Exporters and global supply chains pay more or lose markets."),
			sec(SectorTech, RoleHurt, "Hardware and chip supply chains span borders."),
			sec(SectorDiscret, RoleHurt, "Imported goods get more expensive for retailers and carmakers."),
			sec(SectorMaterials, RoleMixed, "Protected domestic producers gain, users of imported input lose."),
			mac("Inflation expectations", RoleBenefits, "Tariffs are passed on to consumer prices."),
		}},
	{ID: "ai_semis", Label: "AI and semiconductors", Driver: "AI investment and chip demand",
		Query:    "artificial intelligence chips semiconductors data center demand news today",
		Keywords: []string{"artificial intelligence", "ai", "chip", "chips", "semiconductor", "semiconductors", "nvidia", "data center", "gpu", "foundry", "tsmc", "cloud capex", "llm"},
		Exposures: []Exposure{
			sec(SectorTech, RoleBenefits, "Chip makers, equipment and software that sell into AI spending."),
			sec(SectorUtilities, RoleBenefits, "Data centers raise power demand."),
			sec(SectorComms, RoleMixed, "Platforms spend heavily; returns are still unproven."),
			sec(SectorIndustrials, RoleBenefits, "Electrical equipment and construction for data centers."),
			com("Copper", RoleBenefits, "Grid and data-center build-out is copper-intensive."),
		}},
	{ID: "china_economy", Label: "China and emerging markets", Driver: "Chinese growth and stimulus",
		Query:    "China economy stimulus property demand exports news today",
		Keywords: []string{"china", "chinese", "beijing", "yuan", "renminbi", "pboc", "evergrande", "property crisis", "emerging markets", "hong kong"},
		Exposures: []Exposure{
			sec(SectorMaterials, RoleBenefits, "China is the largest buyer of metals and bulk commodities."),
			sec(SectorDiscret, RoleBenefits, "Luxury, autos and travel sell heavily into China."),
			sec(SectorIndustrials, RoleBenefits, "Machinery and capital goods follow Chinese investment."),
			com("Copper", RoleBenefits, "A read on Chinese industrial demand."),
			com("Iron ore", RoleBenefits, "Steel and construction demand."),
		}},
	{ID: "credit_banking", Label: "Credit and banking stress", Driver: "credit stress",
		Query:    "bank credit stress defaults loan losses liquidity news today",
		Keywords: []string{"bank run", "banking crisis", "default", "defaults", "credit crunch", "credit spreads", "loan losses", "bankruptcy", "liquidity", "downgrade", "private credit", "commercial real estate"},
		Exposures: []Exposure{
			sec(SectorFinancials, RoleHurt, "Lenders take losses and funding costs rise."),
			sec(SectorRealEstate, RoleHurt, "Property depends on refinancing."),
			sec(SectorUtilities, RoleBenefits, "Defensive cash flows attract money in a flight to quality."),
			sec(SectorStaples, RoleBenefits, "Defensive demand holds up."),
			com("Gold", RoleBenefits, "Safe haven in financial stress."),
		}},
	{ID: "growth_labor", Label: "Growth and the job market", Driver: "economic growth",
		Query:    "jobs report unemployment GDP recession economy news today",
		Keywords: []string{"recession", "gdp", "unemployment", "jobs report", "payrolls", "layoffs", "consumer spending", "retail sales", "pmi", "soft landing", "slowdown", "consumer confidence"},
		Exposures: []Exposure{
			sec(SectorDiscret, RoleBenefits, "Spending on cars, travel and retail follows growth."),
			sec(SectorIndustrials, RoleBenefits, "Orders follow the cycle."),
			sec(SectorFinancials, RoleBenefits, "Loan demand and credit quality improve."),
			sec(SectorStaples, RoleHurt, "Defensives lag when growth is strong, and hold up when it fades."),
			sec(SectorHealth, RoleHurt, "Defensives lag when growth is strong, and hold up when it fades."),
			com("Copper", RoleBenefits, "Industrial demand."),
		}},
	{ID: "dollar_fx", Label: "Currencies and the dollar", Driver: "the US dollar",
		Query:    "US dollar currency exchange rate krona euro yen news today",
		Keywords: []string{"dollar", "usd", "currency", "currencies", "forex", "exchange rate", "krona", "sek", "euro", "yen", "devaluation", "intervention"},
		Exposures: []Exposure{
			sec(SectorTech, RoleHurt, "Large US exporters earn much of their revenue abroad."),
			sec(SectorMaterials, RoleHurt, "Commodities priced in dollars get more expensive for others."),
			com("Gold", RoleHurt, "Moves against the dollar."),
			com("Crude oil", RoleHurt, "Priced in dollars; a stronger dollar weighs on demand."),
			mac("Foreign earnings", RoleHurt, "Overseas profits translate into fewer dollars."),
		}},
	{ID: "metals_mining", Label: "Metals and mining", Driver: "metal prices",
		Query:    "gold silver copper lithium nickel metals prices mining news today",
		Keywords: []string{"gold", "silver", "copper", "lithium", "nickel", "iron ore", "steel", "aluminium", "aluminum", "mining", "rare earth", "rare earths", "uranium", "platinum"},
		Exposures: []Exposure{
			sec(SectorMaterials, RoleBenefits, "Miners and metal producers earn more per tonne or ounce."),
			sec(SectorIndustrials, RoleHurt, "Manufacturers and builders pay more for input."),
			com("Gold", RoleBenefits, "The price itself."),
			com("Copper", RoleBenefits, "The price itself."),
			com("Lithium", RoleBenefits, "The price itself; key to batteries."),
		}},
	{ID: "food_weather", Label: "Food, weather and climate", Driver: "food and weather disruption",
		Query:    "crop harvest drought wheat grain food prices weather climate news today",
		Keywords: []string{"wheat", "corn", "grain", "harvest", "drought", "flood", "hurricane", "heatwave", "crop", "crops", "food prices", "fertilizer", "cocoa", "coffee", "sugar", "el nino", "wildfire"},
		Exposures: []Exposure{
			sec(SectorStaples, RoleHurt, "Food makers and grocers face higher input costs."),
			sec(SectorMaterials, RoleBenefits, "Fertilizer and agricultural producers gain when crop prices rise."),
			sec(SectorUtilities, RoleMixed, "Weather swings demand for power and can damage assets."),
			com("Wheat and grains", RoleBenefits, "The price itself."),
			com("Cocoa and coffee", RoleBenefits, "Weather-driven supply shocks."),
			mac("Inflation expectations", RoleBenefits, "Food is a large part of the price index."),
		}},
	{ID: "health_regulation", Label: "Health policy and drug news", Driver: "health-care policy pressure",
		Query:    "FDA drug approval pricing healthcare policy pharma news today",
		Keywords: []string{"fda", "drug pricing", "drug approval", "clinical trial", "pharma", "biotech", "medicare", "vaccine", "obesity drug", "glp-1", "healthcare reform"},
		Exposures: []Exposure{
			sec(SectorHealth, RoleMixed, "Approvals lift single names; price controls weigh on the group."),
			sec(SectorStaples, RoleMixed, "Weight-loss drugs may cut food and drink demand."),
		}},
	{ID: "regulation_tech", Label: "Regulation and antitrust", Driver: "regulatory pressure",
		Query:    "antitrust regulation lawsuit fine big tech competition authority news today",
		Keywords: []string{"antitrust", "regulator", "regulators", "lawsuit", "competition authority", "doj", "ftc", "eu commission", "data privacy", "gdpr", "breakup"},
		Exposures: []Exposure{
			sec(SectorTech, RoleHurt, "Fines and forced changes to business models."),
			sec(SectorComms, RoleHurt, "Platforms and media are the usual targets."),
			sec(SectorFinancials, RoleMixed, "Capital rules and fintech oversight cut both ways."),
		}},
}

// ThemeByID finds a theme, or nil.
func ThemeByID(id string) *Theme {
	for i := range WorldThemes {
		if WorldThemes[i].ID == id {
			return &WorldThemes[i]
		}
	}
	return nil
}

var wordSplit = regexp.MustCompile(`[^a-z0-9\-]+`)

// normalizeText lower-cases and pads text so that whole-word and whole-phrase
// matching is a substring test.
func normalizeText(s string) string {
	return " " + strings.Join(strings.Fields(wordSplit.ReplaceAllString(strings.ToLower(s), " ")), " ") + " "
}

// ClassifyHeadline returns the ids of the themes whose keywords appear in the
// text, best match first. A headline can belong to several themes.
func ClassifyHeadline(text string) []string {
	padded := normalizeText(text)
	type hit struct {
		id    string
		score int
	}
	var hits []hit
	for _, t := range WorldThemes {
		score := 0
		for _, kw := range t.Keywords {
			if strings.Contains(padded, " "+kw+" ") {
				score++
			}
		}
		if score > 0 {
			hits = append(hits, hit{t.ID, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.id
	}
	return out
}

// builtinSectors names the sector of well-known listings. A symbol outside
// the table has no sector until a user or PAPER_SYMBOL_SECTORS supplies one.
var builtinSectors = map[string]string{
	"AAPL": SectorTech, "MSFT": SectorTech, "NVDA": SectorTech, "AVGO": SectorTech, "ORCL": SectorTech, "AMD": SectorTech, "INTC": SectorTech,
	"CRM": SectorTech, "ADBE": SectorTech, "CSCO": SectorTech, "QCOM": SectorTech, "TSM": SectorTech, "ASML": SectorTech, "ERIC-B.ST": SectorTech,
	"GOOGL": SectorComms, "GOOG": SectorComms, "META": SectorComms, "NFLX": SectorComms, "DIS": SectorComms, "T": SectorComms, "VZ": SectorComms, "SPOT": SectorComms,
	"AMZN": SectorDiscret, "TSLA": SectorDiscret, "HD": SectorDiscret, "MCD": SectorDiscret, "NKE": SectorDiscret, "SBUX": SectorDiscret, "BKNG": SectorDiscret,
	"VOLV-B.ST": SectorIndustrials, "HM-B.ST": SectorDiscret, "EVO.ST": SectorDiscret,
	"WMT": SectorStaples, "COST": SectorStaples, "PG": SectorStaples, "KO": SectorStaples, "PEP": SectorStaples, "PM": SectorStaples,
	"JPM": SectorFinancials, "BAC": SectorFinancials, "WFC": SectorFinancials, "GS": SectorFinancials, "MS": SectorFinancials, "V": SectorFinancials,
	"MA": SectorFinancials, "BRK-B": SectorFinancials, "SEB-A.ST": SectorFinancials, "SHB-A.ST": SectorFinancials, "SWED-A.ST": SectorFinancials, "INVE-B.ST": SectorFinancials,
	"XOM": SectorEnergy, "CVX": SectorEnergy, "COP": SectorEnergy, "SLB": SectorEnergy, "OXY": SectorEnergy, "EQNR": SectorEnergy, "SHEL": SectorEnergy,
	"JNJ": SectorHealth, "LLY": SectorHealth, "UNH": SectorHealth, "PFE": SectorHealth, "MRK": SectorHealth, "ABBV": SectorHealth, "NOVO-B.CO": SectorHealth,
	"CAT": SectorIndustrials, "BA": SectorIndustrials, "GE": SectorIndustrials, "HON": SectorIndustrials, "LMT": SectorIndustrials, "RTX": SectorIndustrials,
	"UPS": SectorIndustrials, "DAL": SectorIndustrials, "ATCO-A.ST": SectorIndustrials, "SAAB-B.ST": SectorIndustrials, "ABB.ST": SectorIndustrials,
	"FCX": SectorMaterials, "NEM": SectorMaterials, "LIN": SectorMaterials, "BOL.ST": SectorMaterials, "SSAB-A.ST": SectorMaterials,
	"NEE": SectorUtilities, "DUK": SectorUtilities, "SO": SectorUtilities,
	"PLD": SectorRealEstate, "AMT": SectorRealEstate, "O": SectorRealEstate, "SBB-B.ST": SectorRealEstate, "CAST.ST": SectorRealEstate,
}

// SectorOf returns the built-in sector of a symbol, or "".
func SectorOf(symbol string) string {
	return builtinSectors[strings.ToUpper(strings.TrimSpace(symbol))]
}

// NormalizeSector maps a free-text sector (for example "Technology" or
// "Health care") to one of Sectors, or "" when it is not recognized.
func NormalizeSector(name string) string {
	n := normalizeText(name)
	for _, s := range Sectors {
		if n == normalizeText(s) {
			return s
		}
	}
	aliases := map[string]string{
		"technology": SectorTech, "tech": SectorTech, "it": SectorTech, "information technology": SectorTech,
		"healthcare": SectorHealth, "health care": SectorHealth, "pharma": SectorHealth,
		"financial": SectorFinancials, "financial services": SectorFinancials, "banks": SectorFinancials,
		"consumer cyclical": SectorDiscret, "consumer discretionary": SectorDiscret, "consumer defensive": SectorStaples,
		"communication": SectorComms, "telecom": SectorComms, "communications": SectorComms,
		"basic materials": SectorMaterials, "industrial": SectorIndustrials, "oil and gas": SectorEnergy,
		"utility": SectorUtilities, "property": SectorRealEstate,
	}
	return aliases[strings.TrimSpace(strings.ToLower(name))]
}

// ExposureOf returns how a theme touches a sector, if it does.
func (t Theme) ExposureOf(sector string) (Exposure, bool) {
	for _, x := range t.Exposures {
		if x.Kind == KindSector && x.Name == sector {
			return x, true
		}
	}
	return Exposure{}, false
}
