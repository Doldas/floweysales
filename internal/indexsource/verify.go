package indexsource

import (
	"fmt"
	"math"
	"time"
)

// StaleDays is how old a list may be before it stops counting as verified.
const StaleDays = 100

// Verification levels. "verified" is not an endorsement by the index
// provider and not proof that the list is official: it says the list passed
// every automated check below and that you attested to its source. Anything
// else is "approximate" and is shown as such.
const (
	LevelVerified    = "verified"
	LevelApproximate = "approximate"
)

// Check is one automated test of a list.
type Check struct {
	ID     string `json:"id"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// Verification is the outcome of all checks for one snapshot.
type Verification struct {
	Level   string  `json:"level"`
	Checks  []Check `json:"checks"`
	Summary string  `json:"summary"`
	// IdentityShare is the share of members that carry an ISIN (0 to 1).
	IdentityShare float64 `json:"identity_share"`
}

// Assess runs the checks against the index's registry entry. It never
// changes the snapshot and never needs the network.
func Assess(s Snapshot, spec Spec, now time.Time) Verification {
	var checks []Check
	add := func(id string, ok bool, detail string) {
		checks = append(checks, Check{ID: id, OK: ok, Detail: detail})
	}

	add("licence_confirmed", s.LicenseConfirm && len(s.LicenseTerms) >= 10,
		"you confirmed that you may use this data and recorded the terms")

	n := len(s.Members)
	switch {
	case spec.Nominal <= 0:
		add("size_matches_name", true, "the index has no nominal size to compare with")
	default:
		// Share classes and recent changes explain a small gap, never a large one.
		tol := int(math.Ceil(float64(spec.Nominal) * 0.1))
		if tol < 2 {
			tol = 2
		}
		diff := n - spec.Nominal
		if diff < 0 {
			diff = -diff
		}
		add("size_matches_name", diff <= tol, fmt.Sprintf("%d securities; the name says %d (accepted gap: %d)", n, spec.Nominal, tol))
	}

	if age, ok := s.Age(now); !ok {
		add("fresh", false, "the as-of date cannot be read")
	} else {
		add("fresh", age >= 0 && age <= StaleDays, fmt.Sprintf("the list is %d days old (limit %d)", age, StaleDays))
	}

	if s.Unverified {
		add("provider_response_checked", false, s.UnverifiedNote)
	} else {
		add("provider_response_checked", true, "the source format is checked against its documentation")
	}

	withISIN, weighted := 0, 0
	var sum float64
	for _, m := range s.Members {
		if m.ISIN != "" {
			withISIN++
		}
		if m.Weight != nil {
			weighted++
			sum += *m.Weight
		}
	}
	if weighted == 0 {
		add("weights_plausible", true, "no weights supplied")
	} else {
		add("weights_plausible", weighted == n && sum >= 95 && sum <= 105, fmt.Sprintf("%d of %d rows have a weight; they add up to %.1f", weighted, n, sum))
	}

	v := Verification{Checks: checks}
	if n > 0 {
		v.IdentityShare = float64(withISIN) / float64(n)
	}
	failed := 0
	for _, c := range checks {
		if !c.OK {
			failed++
		}
	}
	if failed == 0 {
		v.Level = LevelVerified
		v.Summary = "Passed every automated check. This is not an endorsement by the index provider; the source is the one you attested."
	} else {
		v.Level = LevelApproximate
		v.Summary = fmt.Sprintf("%d of %d checks did not pass; treat this list as approximate.", failed, len(checks))
	}
	return v
}
