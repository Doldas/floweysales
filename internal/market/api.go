package market

import (
	"fmt"
	"os"
)

// MarketApi routes asset queries to the correct provider engine.
type MarketApi struct {
	finnhubAPIKey string
}

// NewMarketApi creates the API facade. The Finnhub API key is resolved from
// the FINNHUB_API_KEY environment variable.
func NewMarketApi(finnhubKeyFromProperties string) *MarketApi {
	key := os.Getenv("FINNHUB_API_KEY")
	if key == "" {
		key = finnhubKeyFromProperties
	}
	return &MarketApi{finnhubAPIKey: key}
}

// FetchAsset fetches a full asset record from the given provider.
func (api *MarketApi) FetchAsset(query string, provider Provider) (MarketAsset, error) {
	switch provider {
	case ProviderBinance:
		return FetchBinance(query)
	case ProviderYahoo:
		return FetchYahoo(query)
	case ProviderFinnhub:
		return FetchFinnhub(query, api.finnhubAPIKey)
	case ProviderAvanza:
		return FetchAvanza(query)
	default:
		return nil, fmt.Errorf("[MarketApi] Error: Engine not registered for provider: %s", provider)
	}
}

// GetLivePrice returns the current price for a symbol, or -1 on failure
// (mirroring the C++ behaviour).
func (api *MarketApi) GetLivePrice(symbol string, provider Provider) float64 {
	asset, err := api.FetchAsset(symbol, provider)
	if err != nil {
		return -1.0
	}
	if asset.Price() <= 0 {
		return -1.0
	}
	return asset.Price()
}

// Market is a lightweight symbol/price pair.
type Market struct {
	Symbol string  `json:"symbol"`
	Price  float64 `json:"price"`
}

// GetTopMarkets returns representative top instruments for a provider.
func (api *MarketApi) GetTopMarkets(provider Provider) []Market {
	var results []Market

	switch provider {
	case ProviderBinance:
		if p := api.GetLivePrice("BTCUSDT", ProviderBinance); p > 0 {
			results = append(results, Market{Symbol: "BTCUSDT", Price: p})
		}
		if p := api.GetLivePrice("ETHUSDT", ProviderBinance); p > 0 {
			results = append(results, Market{Symbol: "ETHUSDT", Price: p})
		}
	case ProviderAvanza:
		topFunds := []string{
			"600075", // Swedbank Robur Access Global A
			"43993",  // Avanza Zero
			"935532", // Länsförsäkringar Global Index
		}
		for _, id := range topFunds {
			if p := api.GetLivePrice(id, ProviderAvanza); p > 0 {
				results = append(results, Market{Symbol: id, Price: p})
			}
		}
	default:
		topStocks := []string{"AAPL", "MSFT", "NVDA", "AMZN", "META", "TSLA", "GIB", "GOOGL", "NFLX", "AMD"}
		for _, sym := range topStocks {
			if p := api.GetLivePrice(sym, provider); p > 0 {
				results = append(results, Market{Symbol: sym, Price: p})
			}
		}
	}

	return results
}
