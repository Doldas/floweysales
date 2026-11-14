package market

import "testing"

func TestRegionOfComesFromTheTickerSuffix(t *testing.T) {
	for symbol, want := range map[string]string{
		"VOLV-B.ST": "SE", "EQNR.OL": "NO", "7203.T": "JP", "SAP.DE": "DE", "BHP.AX": "AU", "shop.to": "CA",
		"AAPL": "", "BTC-USD": "", "EURUSD=X": "", "ABC.ZZ": "", ".ST": "", "": "",
	} {
		if got := RegionOf(symbol); got != want {
			t.Errorf("%q: %q, want %q", symbol, got, want)
		}
	}
}
