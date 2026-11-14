package market

import (
	"strings"
	"time"
	_ "time/tzdata" // exchange time zones must resolve without system zoneinfo
)

// Market session states. "unknown" is a real answer: it is returned whenever the
// exchange cannot be identified, never guessed.
const (
	SessionOpen    = "open"
	SessionClosed  = "closed"
	SessionUnknown = "unknown"
)

// Where a session state came from.
const (
	// BasisProvider: the provider itself reported the state.
	BasisProvider = "provider"
	// BasisRegularHours: computed from the exchange's regular weekday hours.
	// Public holidays and early closes are not known, so "open" means "inside
	// regular hours" and may be wrong on a holiday. "closed" (weekend or outside
	// the hours) is reliable.
	BasisRegularHours = "regular_hours"
)

// Session is the market-session fact for one instrument at one moment.
type Session struct {
	State    string `json:"state"`
	Basis    string `json:"basis,omitempty"`
	Exchange string `json:"exchange,omitempty"`
}

type tradingWindow struct{ openMin, closeMin int } // minutes after local midnight

type exchangeHours struct {
	name    string
	zone    string
	windows []tradingWindow
}

func hm(h, m int) int { return h*60 + m }

func single(name, zone string, open, closing int) exchangeHours {
	return exchangeHours{name, zone, []tradingWindow{{open, closing}}}
}

var (
	hoursUS   = single("US exchanges", "America/New_York", hm(9, 30), hm(16, 0))
	hoursXSTO = single("Nasdaq Stockholm", "Europe/Stockholm", hm(9, 0), hm(17, 30))
	hoursXLON = single("London Stock Exchange", "Europe/London", hm(8, 0), hm(16, 30))
	hoursXETR = single("Xetra / Frankfurt", "Europe/Berlin", hm(9, 0), hm(17, 30))
	hoursXPAR = single("Euronext Paris", "Europe/Paris", hm(9, 0), hm(17, 30))
	hoursXAMS = single("Euronext Amsterdam", "Europe/Amsterdam", hm(9, 0), hm(17, 30))
	hoursXCSE = single("Nasdaq Copenhagen", "Europe/Copenhagen", hm(9, 0), hm(17, 0))
	hoursXHEL = single("Nasdaq Helsinki", "Europe/Helsinki", hm(10, 0), hm(18, 30))
	hoursXOSL = single("Oslo Børs", "Europe/Oslo", hm(9, 0), hm(16, 20))
	hoursXSWX = single("SIX Swiss Exchange", "Europe/Zurich", hm(9, 0), hm(17, 30))
	hoursXMIL = single("Borsa Italiana", "Europe/Rome", hm(9, 0), hm(17, 30))
	hoursXMAD = single("Bolsa de Madrid", "Europe/Madrid", hm(9, 0), hm(17, 30))
	hoursXTSE = single("Toronto Stock Exchange", "America/Toronto", hm(9, 30), hm(16, 0))
	hoursXASX = single("ASX", "Australia/Sydney", hm(10, 0), hm(16, 0))
	hoursXJSE = single("Johannesburg Stock Exchange", "Africa/Johannesburg", hm(9, 0), hm(17, 0))
	hoursXTKS = exchangeHours{"Tokyo Stock Exchange", "Asia/Tokyo", []tradingWindow{{hm(9, 0), hm(11, 30)}, {hm(12, 30), hm(15, 30)}}}
	hoursXHKG = exchangeHours{"Hong Kong Exchange", "Asia/Hong_Kong", []tradingWindow{{hm(9, 30), hm(12, 0)}, {hm(13, 0), hm(16, 0)}}}

	// Yahoo-style ticker suffixes.
	bySuffix = map[string]exchangeHours{
		"ST": hoursXSTO, "L": hoursXLON, "DE": hoursXETR, "F": hoursXETR, "PA": hoursXPAR, "AS": hoursXAMS,
		"CO": hoursXCSE, "HE": hoursXHEL, "OL": hoursXOSL, "SW": hoursXSWX, "MI": hoursXMIL, "MC": hoursXMAD,
		"TO": hoursXTSE, "AX": hoursXASX, "T": hoursXTKS, "HK": hoursXHKG, "JO": hoursXJSE,
	}
	// Exchange codes of US listings, which carry no ticker suffix.
	usExchangeCodes = map[string]bool{
		"NMS": true, "NGM": true, "NCM": true, "NAS": true, "NASDAQ": true, "NYQ": true, "NYSE": true,
		"ASE": true, "AMEX": true, "PCX": true, "BTS": true, "XNYS": true, "XNAS": true, "XASE": true, "ARCX": true,
	}
)

// regionBySuffix is the market (a country code) of each Yahoo-style suffix.
var regionBySuffix = map[string]string{
	"ST": "SE", "L": "GB", "DE": "DE", "F": "DE", "PA": "FR", "AS": "NL", "CO": "DK", "HE": "FI", "OL": "NO",
	"SW": "CH", "MI": "IT", "MC": "ES", "TO": "CA", "AX": "AU", "T": "JP", "HK": "HK", "JO": "ZA",
}

// RegionOf is the market of an instrument from its ticker suffix (".ST" is
// SE). A symbol without a known suffix has none: a bare ticker may be a US
// listing, a crypto pair or a currency, and is not guessed.
func RegionOf(symbol string) string {
	symbol = strings.TrimSpace(symbol)
	if i := strings.LastIndex(symbol, "."); i > 0 && i < len(symbol)-1 {
		return regionBySuffix[strings.ToUpper(symbol[i+1:])]
	}
	return ""
}

func exchangeFor(symbol, exchange string) (exchangeHours, bool) {
	if i := strings.LastIndex(symbol, "."); i > 0 && i < len(symbol)-1 {
		h, ok := bySuffix[strings.ToUpper(symbol[i+1:])]
		return h, ok
	}
	if usExchangeCodes[strings.ToUpper(strings.TrimSpace(exchange))] {
		return hoursUS, true
	}
	return exchangeHours{}, false
}

// SessionAt reports whether the regular trading session of the instrument's
// exchange is open at now. The exchange comes from the ticker suffix (".ST") or,
// for suffix-less US listings, from the exchange code a search result carried.
// Anything else is "unknown": a bare ticker such as BTC-USD or EURUSD=X may not
// be exchange-traded at all. No network is used and holidays are not known (see
// BasisRegularHours).
func SessionAt(symbol, exchange string, now time.Time) Session {
	h, ok := exchangeFor(strings.TrimSpace(symbol), exchange)
	if !ok {
		return Session{State: SessionUnknown}
	}
	loc, err := time.LoadLocation(h.zone)
	if err != nil {
		return Session{State: SessionUnknown}
	}
	local := now.In(loc)
	s := Session{State: SessionClosed, Basis: BasisRegularHours, Exchange: h.name}
	if wd := local.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return s
	}
	minute := local.Hour()*60 + local.Minute()
	for _, w := range h.windows {
		if minute >= w.openMin && minute < w.closeMin {
			s.State = SessionOpen
			return s
		}
	}
	return s
}

// QuoteMeta is what a provider states about one quote in Asset.Data. Every field
// is optional; absent means the provider did not say, never zero.
type QuoteMeta struct {
	QuoteTimeUnix int64  // `quoteTimeUnix`: when the exchange or provider stamped the quote
	DelaySeconds  *int   // `delaySeconds`: how far behind the market the quote is
	MarketState   string // `marketState`: "open" or "closed" as the provider states it
}

// QuoteMetaFrom reads the optional keys above. Values of the wrong type or out of
// range are ignored (unknown) rather than coerced.
func QuoteMetaFrom(data map[string]any) QuoteMeta {
	var m QuoteMeta
	if f, ok := data["quoteTimeUnix"].(float64); ok && f > 0 && f < 1e11 {
		m.QuoteTimeUnix = int64(f)
	}
	if f, ok := data["delaySeconds"].(float64); ok && f >= 0 && f <= 86400 {
		d := int(f)
		m.DelaySeconds = &d
	}
	if s, ok := data["marketState"].(string); ok {
		switch v := strings.ToLower(strings.TrimSpace(s)); v {
		case SessionOpen, SessionClosed:
			m.MarketState = v
		}
	}
	return m
}

// ResolveSession prefers the provider's own statement over the computed one.
func ResolveSession(symbol, exchange string, meta QuoteMeta, now time.Time) Session {
	if meta.MarketState != "" {
		return Session{State: meta.MarketState, Basis: BasisProvider}
	}
	return SessionAt(symbol, exchange, now)
}
