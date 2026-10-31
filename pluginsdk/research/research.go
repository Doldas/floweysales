// Package research helps plugins implement the "research" method the way
// FloweySales expects: only operations declared in plugin.json, validated
// symbol and date arguments, bounded redirect-free fetches and the standard
// result envelope. There is no generic upstream URL forwarding.
package research

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	sdk "github.com/Doldas/floweysales/pluginsdk"
)

// Symbol patterns for research arguments.
var (
	CryptoSymbol = regexp.MustCompile(`^[A-Z0-9]{2,25}$`)
	EquitySymbol = regexp.MustCompile(`^[A-Z0-9][A-Z0-9._:-]{0,29}$`)
)

// Find returns the declared operation, or an "unsupported" error.
func Find(m sdk.Manifest, operation string) (sdk.ResearchOperation, error) {
	operation = strings.ToLower(strings.TrimSpace(operation))
	for _, op := range m.Provider.Research {
		if op.Operation == operation {
			return op, nil
		}
	}
	return sdk.ResearchOperation{}, sdk.Errorf(sdk.CodeUnsupported, "unsupported read-only provider operation %s/%s", m.Provider.ID, operation)
}

// Query validates the symbol and date arguments of op and returns the base
// query (symbol, from, to). Dates are only accepted when op requires them and
// must span at most 31 days.
func Query(op sdk.ResearchOperation, p sdk.ResearchParams, symbolPattern *regexp.Regexp) (url.Values, error) {
	q := url.Values{}
	symbol := strings.ToUpper(strings.TrimSpace(p.Symbol))
	if op.SymbolRequired {
		if !symbolPattern.MatchString(symbol) {
			return nil, sdk.Errorf(sdk.CodeInvalidArgument, "invalid or missing symbol for this provider")
		}
		q.Set("symbol", symbol)
	}
	if op.DatesRequired {
		start, err1 := time.Parse("2006-01-02", p.From)
		end, err2 := time.Parse("2006-01-02", p.To)
		if err1 != nil || err2 != nil || end.Before(start) || end.Sub(start) > 31*24*time.Hour {
			return nil, sdk.Errorf(sdk.CodeInvalidArgument, "from and to must be YYYY-MM-DD, ordered, within 31 days")
		}
		q.Set("from", p.From)
		q.Set("to", p.To)
	} else if p.From != "" || p.To != "" {
		return nil, sdk.Errorf(sdk.CodeInvalidArgument, "dates are only supported for operations that require them")
	}
	return q, nil
}

// maxBody caps a research response so it stays model-context sized.
const maxBody = 512 * 1024

// FetchJSON performs one bounded, redirect-free GET and decodes JSON. Arrays
// are cut to 50 items. Upstream bodies and URLs (which may carry tokens) are
// never echoed in errors.
func FetchJSON(ctx context.Context, client *http.Client, providerID, endpoint string, headers map[string]string) (any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "FloweySales/1.0 (read-only research)")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	bounded := *client
	bounded.Timeout = 12 * time.Second
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("redirects are disabled for provider research")
	}
	resp, err := bounded.Do(req)
	if err != nil {
		return nil, sdk.Errorf(sdk.CodeUpstream, "provider request failed: %v", redactURL(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, sdk.Errorf(sdk.CodeUpstream, "%s returned HTTP %d (check subscription, quota, symbol)", providerID, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxBody {
		return nil, sdk.Errorf(sdk.CodeUpstream, "provider response exceeds 512 KiB; narrow query")
	}
	var data any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, sdk.Errorf(sdk.CodeUpstream, "provider did not return valid JSON")
	}
	if arr, ok := data.([]any); ok && len(arr) > 50 {
		data = arr[:50]
	}
	return data, nil
}

// redactURL drops the request URL from transport errors.
func redactURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}

// DefaultNote labels provider research snapshots.
const DefaultNote = "Provider-sourced snapshot, NOT independently audited/guaranteed live. Check currency, timestamp, reporting periods and license before comparing. No trading authorization."

// Result assembles the standard envelope.
func Result(providerID string, op sdk.ResearchOperation, symbol, source string, data any, note string) sdk.ResearchResult {
	if note == "" {
		note = DefaultNote
	}
	return sdk.ResearchResult{
		Provider: providerID, Operation: op.Operation, UpstreamPath: op.Path,
		Symbol:      strings.ToUpper(strings.TrimSpace(symbol)),
		RetrievedAt: time.Now().UTC().Format(time.RFC3339),
		Source:      source, Data: data, Note: note,
	}
}
