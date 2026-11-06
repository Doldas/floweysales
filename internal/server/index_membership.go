package server

import (
	"fmt"
	"time"

	"floweysales/internal/indexsource"
)

const (
	membershipPointInTime  = "point_in_time"
	membershipCurrentOnly  = "current_members"
	membershipPartialFalls = "point_in_time_with_fallback"
)

// membershipInfo discloses which constituents a backtest on an index used.
type membershipInfo struct {
	Mode        string `json:"mode"`
	Provider    string `json:"provider"`
	SourceName  string `json:"source_name"`
	AsOf        string `json:"as_of"`
	HistoryFrom string `json:"history_from,omitempty"`
	PITCount    int    `json:"point_in_time_rebalances"`
	FallbackN   int    `json:"fallback_rebalances"`
	Note        string `json:"note"`
}

// membershipTrack counts, per rebalance, whether the members of that date
// were known or today's members stood in.
type membershipTrack struct {
	pit, fallback int
}

// allowed returns the set of symbols that may be ranked on a date, or nil
// when the universe is not an index with known history (all given symbols
// are then ranked).
func (t *membershipTrack) allowed(snap *indexsource.Snapshot, asOf time.Time) map[string]bool {
	if snap == nil || !snap.PointInTime {
		return nil
	}
	members, known := snap.MembersAt(asOf.UTC().Format(strategyDateLayout))
	if known {
		t.pit++
	} else {
		t.fallback++
	}
	set := make(map[string]bool, len(members))
	for _, m := range members {
		set[m] = true
	}
	return set
}

func (t *membershipTrack) info(snap *indexsource.Snapshot, missing int) *membershipInfo {
	if snap == nil {
		return nil
	}
	mi := &membershipInfo{Mode: membershipCurrentOnly, Provider: snap.Provider, SourceName: snap.SourceName, AsOf: snap.AsOf, HistoryFrom: snap.HistoryFrom,
		PITCount: t.pit, FallbackN: t.fallback}
	switch {
	case !snap.PointInTime:
		mi.Note = "Past membership is not known for this list, so every rebalance used today's members as of " + snap.AsOf + ". Companies that left the index are missing, which flatters past returns (survivorship bias)."
	case t.fallback > 0:
		mi.Mode = membershipPartialFalls
		mi.Note = fmt.Sprintf("%d rebalances used the members of their own date; %d fell before the history (%s) and used today's members, which is survivorship-biased.", t.pit, t.fallback, snap.HistoryFrom)
	default:
		mi.Mode = membershipPointInTime
		mi.Note = fmt.Sprintf("Each of the %d rebalances ranked the members of its own date, rebuilt from the add/remove records you supplied.", t.pit)
	}
	if mi.Mode != membershipCurrentOnly && missing > 0 {
		mi.Note += fmt.Sprintf(" %d symbols had no price history from your market-data provider and were skipped (often delisted companies), so some survivorship bias remains.", missing)
	}
	return mi
}

func dropWarning(list []string, code string) []string {
	out := list[:0:0]
	for _, w := range list {
		if w != code {
			out = append(out, w)
		}
	}
	return out
}
