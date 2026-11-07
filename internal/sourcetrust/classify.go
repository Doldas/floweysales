// Package sourcetrust classifies what a retrieved source is, says what it can
// be evidence for, keeps its dates apart, and ranks candidates for a given
// task. Everything here is deterministic and rule based: an unknown host is
// "unknown", a missing date stays missing, and no score is presented as a
// credibility percentage. A ranking explains itself with named factors.
package sourcetrust

import (
	"net/url"
	"strings"
)

// Type says what kind of publisher a source is. It is separate from the
// evidence Role: a company press release has type company_ir and role
// self_reported.
const (
	TypeOfficialFiling       = "official_filing"       // SEC and other regulatory filings, annual and interim reports
	TypeExchange             = "exchange_announcement" // exchange or issuer announcement
	TypeGovernmentData       = "government_data"       // government or statistical dataset
	TypePeerReviewed         = "peer_reviewed"         // original peer-reviewed study
	TypeCompanyIR            = "company_ir"            // company investor relations or press release
	TypeFirstPartyDataset    = "first_party_dataset"   // a data provider's own records
	TypeEstablishedNews      = "established_news"      // established editorial news
	TypeProfessionalAnalysis = "professional_analysis"
	TypeCommunity            = "community_social" // Reddit, forums, social networks
	TypeMarketing            = "marketing"        // promotional or sponsored content
	TypeUnknown              = "unknown"          // aggregator or not recognized
)

// Role says what a source can be evidence for.
const (
	RolePrimary      = "primary"       // the record itself: a filing, a dataset, a study
	RoleSelfReported = "self_reported" // what a party says about itself; evidence of the statement, not independent verification
	RoleSecondary    = "secondary"     // reporting about primary material
	RoleOpinion      = "opinion"       // analysis, commentary, recommendation
	RoleSentiment    = "sentiment"     // what people say and think; never a source of financial facts
	RoleUnknown      = "unknown"
)

// Classification is the result of the rules for one source.
type Classification struct {
	Type  string `json:"source_type"`
	Role  string `json:"evidence_role"`
	Basis string `json:"type_basis,omitempty"` // the rule that matched, or "no rule matched"
}

type hostRule struct {
	typ   string
	hosts []string
}

// Host rules are checked in order. A host matches itself and any subdomain.
var hostRules = []hostRule{
	{TypeOfficialFiling, []string{"sec.gov", "sedarplus.ca", "sedar.com", "esma.europa.eu", "companieshouse.gov.uk", "bolagsverket.se", "bundesanzeiger.de", "asx.com.au", "edinet-fsa.go.jp", "finansinspektionen.se"}},
	{TypeExchange, []string{"nyse.com", "nasdaqomxnordic.com", "nasdaq-omx.com", "londonstockexchange.com", "euronext.com", "deutsche-boerse.com", "boerse-frankfurt.de", "six-group.com", "tsx.com", "jpx.co.jp", "hkexnews.hk", "cboe.com"}},
	{TypeGovernmentData, []string{"bls.gov", "bea.gov", "census.gov", "federalreserve.gov", "stlouisfed.org", "treasury.gov", "ecb.europa.eu", "europa.eu", "imf.org", "worldbank.org", "oecd.org", "scb.se", "riksbank.se", "ons.gov.uk", "bankofengland.co.uk", "statcan.gc.ca", "destatis.de", "bis.org"}},
	{TypePeerReviewed, []string{"nature.com", "science.org", "pubmed.ncbi.nlm.nih.gov", "ncbi.nlm.nih.gov", "doi.org", "sciencedirect.com", "link.springer.com", "onlinelibrary.wiley.com", "jstor.org", "cell.com", "thelancet.com", "academic.oup.com", "pnas.org", "journals.sagepub.com", "tandfonline.com"}},
	{TypeCompanyIR, []string{"prnewswire.com", "businesswire.com", "globenewswire.com", "accesswire.com", "newsfilecorp.com", "mynewsdesk.com", "cision.com"}},
	{TypeCommunity, []string{"reddit.com", "redd.it", "news.ycombinator.com", "stocktwits.com", "x.com", "twitter.com", "facebook.com", "discord.com", "quora.com", "stackexchange.com", "stackoverflow.com", "flashback.org", "investorshub.advfn.com", "t.me", "bsky.app", "threads.net", "tiktok.com", "youtube.com", "medium.com", "substack.com", "linkedin.com"}},
	{TypeEstablishedNews, []string{"reuters.com", "bloomberg.com", "ft.com", "wsj.com", "apnews.com", "cnbc.com", "bbc.com", "bbc.co.uk", "nytimes.com", "economist.com", "theguardian.com", "marketwatch.com", "barrons.com", "washingtonpost.com", "forbes.com", "fortune.com", "di.se", "dn.se", "svd.se", "placera.se", "handelsblatt.com", "lemonde.fr", "nikkei.com", "axios.com", "politico.com"}},
	{TypeProfessionalAnalysis, []string{"seekingalpha.com", "fool.com", "morningstar.com", "thestreet.com", "investopedia.com", "valueinvestorsclub.com", "gurufocus.com", "simplywall.st", "zacks.com", "tipranks.com"}},
	{TypeUnknown, []string{"finance.yahoo.com", "yahoo.com", "msn.com", "benzinga.com", "marketbeat.com", "stockanalysis.com", "investing.com", "stocktitan.net", "macrotrends.net", "companiesmarketcap.com", "wikipedia.org", "google.com"}},
}

var marketingMarkers = []string{"sponsored content", "sponsored post", "advertisement", "advertorial", "promoted content", "paid partnership", "partner content"}

func hostOf(raw string) (host, path string) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www."), strings.ToLower(u.Path)
}

func hostMatches(host, domain string) bool {
	return host == domain || strings.HasSuffix(host, "."+domain)
}

func roleOf(typ string) string {
	switch typ {
	case TypeOfficialFiling, TypeExchange, TypeGovernmentData, TypePeerReviewed, TypeFirstPartyDataset:
		return RolePrimary
	case TypeCompanyIR, TypeMarketing:
		return RoleSelfReported
	case TypeEstablishedNews:
		return RoleSecondary
	case TypeProfessionalAnalysis:
		return RoleOpinion
	case TypeCommunity:
		return RoleSentiment
	}
	return RoleUnknown
}

// Classify assigns a type and an evidence role from the address, title and
// snippet. It never guesses: a host that no rule names is unknown, and an
// unknown source has the unknown role.
func Classify(rawURL, title, snippet string) Classification {
	host, path := hostOf(rawURL)
	if host == "" {
		return Classification{Type: TypeUnknown, Role: RoleUnknown, Basis: "no usable address"}
	}
	mk := strings.ToLower(title + " " + snippet)
	for _, m := range marketingMarkers {
		if strings.Contains(mk, m) {
			return Classification{Type: TypeMarketing, Role: RoleSelfReported, Basis: "marked as " + m}
		}
	}
	for _, r := range hostRules {
		for _, d := range r.hosts {
			if hostMatches(host, d) {
				return Classification{Type: r.typ, Role: roleOf(r.typ), Basis: "host " + d}
			}
		}
	}
	if strings.HasSuffix(host, ".gov") || strings.Contains(host, ".gov.") || strings.HasSuffix(host, ".go.jp") {
		return Classification{Type: TypeGovernmentData, Role: RolePrimary, Basis: "government domain"}
	}
	labels := strings.Split(host, ".")
	if len(labels) > 2 {
		switch labels[0] {
		case "investor", "investors", "ir", "corporate":
			return Classification{Type: TypeCompanyIR, Role: RoleSelfReported, Basis: "investor-relations subdomain"}
		case "forum", "forums", "community", "boards":
			return Classification{Type: TypeCommunity, Role: RoleSentiment, Basis: "forum subdomain"}
		}
	}
	for _, p := range []string{"/investor-relations", "/investors/", "/investor/", "/press-release", "/press-releases", "/newsroom", "/ir/"} {
		if strings.Contains(path, p) {
			return Classification{Type: TypeCompanyIR, Role: RoleSelfReported, Basis: "investor-relations path"}
		}
	}
	return Classification{Type: TypeUnknown, Role: RoleUnknown, Basis: "no rule matched"}
}

// ClassifyTool is the classification of a source a first-party tool returned,
// such as get_sec_section.
func ClassifyTool(tool string) (Classification, bool) {
	if tool == "get_sec_section" {
		return Classification{Type: TypeOfficialFiling, Role: RolePrimary, Basis: "SEC filing tool"}, true
	}
	return Classification{}, false
}

// CanSupportFinancialFact reports whether a source of this role may be the
// sole support for a reported financial figure such as revenue or EPS.
// Sentiment, opinion and unknown sources cannot; self-reported sources count
// as evidence of what the company stated.
func CanSupportFinancialFact(role string) bool {
	switch role {
	case RolePrimary, RoleSelfReported, RoleSecondary:
		return true
	}
	return false
}
