package storage

import (
	"context"
	"time"
)

// WebSearchUsage is accumulated web search usage. Cost is in millionths of a
// US dollar and is an estimate from the administrator's prices.
type WebSearchUsage struct {
	Searches, PromptTokens, OutputTokens, CostMicros, Failures int64
}

// AddWebSearchUsage adds usage to a provider's row for a UTC day.
func (d *Database) AddWebSearchUsage(ctx context.Context, provider string, day time.Time, u WebSearchUsage) error {
	_, err := d.db.ExecContext(ctx,
		"INSERT INTO web_search_usage(provider,usage_day,searches,prompt_tokens,output_tokens,cost_micros,failures) VALUES(?,?,?,?,?,?,?) "+
			"ON DUPLICATE KEY UPDATE searches=searches+VALUES(searches),prompt_tokens=prompt_tokens+VALUES(prompt_tokens),"+
			"output_tokens=output_tokens+VALUES(output_tokens),cost_micros=cost_micros+VALUES(cost_micros),failures=failures+VALUES(failures)",
		provider, day.UTC().Format("2006-01-02"), u.Searches, u.PromptTokens, u.OutputTokens, u.CostMicros, u.Failures)
	return err
}

// SumWebSearchUsage totals a provider's usage over the UTC days from..to, inclusive.
func (d *Database) SumWebSearchUsage(ctx context.Context, provider string, from, to time.Time) (WebSearchUsage, error) {
	var u WebSearchUsage
	err := d.db.QueryRowContext(ctx,
		"SELECT COALESCE(SUM(searches),0),COALESCE(SUM(prompt_tokens),0),COALESCE(SUM(output_tokens),0),COALESCE(SUM(cost_micros),0),COALESCE(SUM(failures),0) "+
			"FROM web_search_usage WHERE provider=? AND usage_day BETWEEN ? AND ?",
		provider, from.UTC().Format("2006-01-02"), to.UTC().Format("2006-01-02")).
		Scan(&u.Searches, &u.PromptTokens, &u.OutputTokens, &u.CostMicros, &u.Failures)
	return u, err
}
