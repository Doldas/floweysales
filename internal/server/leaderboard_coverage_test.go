package server

import (
	"strings"
	"testing"

	"floweysales/internal/universecheck"
)

// The coverage spike (#209) names candidate markets by ticker suffix. Each
// suffix it relies on must be one the leaderboard can read an exchange from,
// with the same country.
func TestCoverageCandidatesMatchKnownListings(t *testing.T) {
	for _, m := range universecheck.Candidates() {
		if m.Suffix == "" {
			continue
		}
		l, ok := suffixListings[m.Suffix]
		if !ok || l.Country != m.Country {
			t.Errorf("%s: suffix .%s is %+v here, candidate says %s", m.ID, m.Suffix, l, m.Country)
		}
	}
}

// A region is offered only after the spike clears it. Until a maintainer has
// run cmd/universe-coverage with provider access, no list exists for these.
func TestUnverifiedRegionsStayWithoutUniverses(t *testing.T) {
	for _, u := range leaderboardUniverses {
		switch u.Region {
		case "asia", "africa", "south_america", "oceania":
			t.Errorf("%s: %s has no verified coverage; run cmd/universe-coverage first", u.ID, u.Region)
		}
		for _, s := range u.Symbols {
			for _, m := range universecheck.Candidates() {
				if m.Suffix != "" && strings.HasSuffix(s, "."+m.Suffix) && m.Region != "north_america" && m.Region != "europe" {
					t.Errorf("%s: symbol %s belongs to the unverified market %s", u.ID, s, m.ID)
				}
			}
		}
	}
}
