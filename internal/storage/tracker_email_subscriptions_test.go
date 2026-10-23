package storage

import "testing"

func TestTrackerSubscriptionValidation(t *testing.T) {
	ok := TrackerEmailSubscription{Symbol: "AAPL", Provider: "YAHOO", GoldenCross: true, CooldownMinutes: 10}
	if err := ValidateTrackerSubscription(ok); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []TrackerEmailSubscription{
		{Symbol: "../BAD", Provider: "YAHOO", CooldownMinutes: 10},
		{Symbol: "AAPL", Provider: "YAHOO", CooldownMinutes: 1441},
		{Symbol: "AAPL", Provider: "INVALID PROVIDER", CooldownMinutes: 10},
	} {
		if ValidateTrackerSubscription(tc) == nil {
			t.Errorf("accepted invalid subscription: %+v", tc)
		}
	}
}
