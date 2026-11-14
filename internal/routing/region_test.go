package routing

import (
	"fmt"
	"testing"
)

func TestRegionalPolicyIsValidated(t *testing.T) {
	sources := []Source{sys("yahoo", MarketQuote, MarketHistory), sys("avanza", MarketQuote, MarketHistory), sys("brave", WebSearch)}
	ok := Policy{Domain: MarketQuote, Primary: "yahoo", ByRegion: map[string]string{"SE": "avanza", "NO": ""}}
	if err := ok.Normalize(sources); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(ok.ByRegion) != "map[SE:avanza]" {
		t.Errorf("an empty entry is dropped: %v", ok.ByRegion)
	}
	many := map[string]string{}
	for _, r := range Regions {
		many[r.Code] = "avanza"
	}
	for name, bad := range map[string]Policy{
		"unknown region":      {Domain: MarketQuote, Primary: "yahoo", ByRegion: map[string]string{"XX": "avanza"}},
		"wrong domain source": {Domain: MarketQuote, Primary: "yahoo", ByRegion: map[string]string{"SE": "brave"}},
		"missing source":      {Domain: MarketQuote, Primary: "yahoo", ByRegion: map[string]string{"SE": "ghost"}},
		"not regional":        {Domain: MarketHistory, Primary: "yahoo", ByRegion: map[string]string{"SE": "avanza"}},
	} {
		if err := bad.Normalize(sources); err == nil {
			t.Errorf("%s: expected a refusal", name)
		}
	}
	all := Policy{Domain: MarketQuote, Primary: "yahoo", ByRegion: many}
	if err := all.Normalize(sources); err != nil {
		t.Errorf("every known region may have an entry: %v", err)
	}
}

func TestPlanTriesTheRegionsSourceFirst(t *testing.T) {
	sources := []Source{sys("yahoo", MarketQuote), sys("finnhub", MarketQuote), sys("avanza", MarketQuote), userSrc("mine", 7, MarketQuote)}
	policy := &Policy{Domain: MarketQuote, Primary: "yahoo", Fallback: []string{"finnhub"}, ByRegion: map[string]string{"SE": "avanza"}}
	for region, want := range map[string]string{
		"SE": "avanza:region,yahoo:primary,finnhub:fallback",
		"NO": "yahoo:primary,finnhub:fallback",
		"":   "yahoo:primary,finnhub:fallback",
	} {
		plan, err := MakePlan(policy, sources, Request{Domain: MarketQuote, Region: region}, nil)
		if err != nil || ids(plan) != want {
			t.Errorf("region %q: %v %s", region, err, ids(plan))
		}
	}
	plan, _ := MakePlan(policy, sources, Request{Domain: MarketQuote, Region: "SE", UserID: 7}, nil)
	if got := ids(plan); got != "mine:user,avanza:region,yahoo:primary,finnhub:fallback" {
		t.Errorf("the user's own source still comes first: %s", got)
	}
	same := &Policy{Domain: MarketQuote, Primary: "avanza", ByRegion: map[string]string{"SE": "avanza"}}
	plan, _ = MakePlan(same, sources, Request{Domain: MarketQuote, Region: "SE"}, nil)
	if got := ids(plan); got != "avanza:region" {
		t.Errorf("a source is tried once: %s", got)
	}
}
