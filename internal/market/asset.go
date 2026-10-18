package market

import (
	"encoding/json"
	"strings"
)

// Provider enumerates the supported market data sources.
type Provider int

const (
	ProviderBinance Provider = iota
	ProviderYahoo
	ProviderFinnhub
	ProviderAvanza
)

// String returns the display name of the provider (mirrors C++ to_string).
func (p Provider) String() string {
	switch p {
	case ProviderBinance:
		return "Binance"
	case ProviderYahoo:
		return "Yahoo"
	case ProviderFinnhub:
		return "Finnhub"
	case ProviderAvanza:
		return "Avanza"
	}
	return "Unknown"
}

// ID returns the canonical identifier used in configuration and REST payloads
// (e.g. "YAHOO"), which is what the frontend option values expect.
func (p Provider) ID() string {
	switch p {
	case ProviderBinance:
		return "BINANCE"
	case ProviderYahoo:
		return "YAHOO"
	case ProviderFinnhub:
		return "FINNHUB"
	case ProviderAvanza:
		return "AVANZA"
	}
	return "YAHOO"
}

// ParseProvider converts a string like "FINNHUB" into a Provider.
// Defaults to Yahoo like the C++ implementation.
func ParseProvider(s string) Provider {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "FINNHUB":
		return ProviderFinnhub
	case "BINANCE":
		return ProviderBinance
	case "AVANZA":
		return ProviderAvanza
	default:
		return ProviderYahoo
	}
}

// MarketAsset is the common interface all parsed provider assets implement.
type MarketAsset interface {
	Symbol() string
	Name() string
	Price() float64
	Currency() string
	AssetDataProvider() string
	// ToJSON builds the payload stored as rawJson in the database and
	// streamed to the frontend as "marketData".
	ToJSON() map[string]any
}

// MarshalCompact marshals an asset to compact JSON text.
func MarshalCompact(a MarketAsset) string {
	b, err := json.Marshal(a.ToJSON())
	if err != nil {
		return "{}"
	}
	return string(b)
}
