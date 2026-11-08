package indexsource

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// MaxChanges bounds one history file.
const MaxChanges = 5000

// ValidISIN reports whether s is a well-formed ISIN: two letters, nine
// letters or digits and a check digit that passes the ISO 6166 Luhn test.
func ValidISIN(s string) bool {
	if len(s) != 12 {
		return false
	}
	var digits []int
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits = append(digits, int(r-'0'))
		case r >= 'A' && r <= 'Z':
			v := int(r-'A') + 10
			digits = append(digits, v/10, v%10)
		default:
			return false
		}
	}
	if s[0] < 'A' || s[0] > 'Z' || s[1] < 'A' || s[1] > 'Z' {
		return false
	}
	sum, double := 0, false
	for i := len(digits) - 1; i >= 0; i-- {
		d := digits[i]
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}

// ApplyHistoryCSV reads dated membership changes (header: date, added,
// removed; one of the last two may be blank) and attaches them to snap, which
// turns the list into a point-in-time list from the earliest change on. It
// returns every reason the file was refused. The changes are checked against
// the list: a symbol that joined must be a member today or have left later,
// and one that left must not be a member today unless it joined again.
func ApplyHistoryCSV(snap *Snapshot, data []byte, spec Spec) []string {
	var problems []string
	fail := func(f string, a ...any) { problems = append(problems, fmt.Sprintf(f, a...)) }
	if len(data) > MaxCSVBytes {
		return []string{"the history file is larger than 512 KiB"}
	}
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	header, err := r.Read()
	if err != nil {
		return []string{"the history file is empty or has no header row"}
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	dateCol, ok := col["date"]
	if !ok {
		return []string{"the history header needs a date column (and added and/or removed)"}
	}
	_, hasAdded := col["added"]
	_, hasRemoved := col["removed"]
	if !hasAdded && !hasRemoved {
		return []string{"the history header needs an added and/or a removed column"}
	}
	get := func(rec []string, name string) string {
		if i, ok := col[name]; ok && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}
	norm := func(raw string, row int, what string) string {
		if raw == "" {
			return ""
		}
		m, err := normalizeMember(raw, "", spec)
		if err != nil {
			fail("row %d: %s: %v", row, what, err)
			return ""
		}
		return m.Symbol
	}
	var changes []Change
	row := 1
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		row++
		if err != nil {
			fail("row %d: %v", row, err)
			continue
		}
		if len(rec) == 1 && strings.TrimSpace(rec[0]) == "" {
			continue
		}
		date := ""
		if dateCol < len(rec) {
			date = strings.TrimSpace(rec[dateCol])
		}
		if t, perr := time.Parse(DateLayout, date); perr != nil || t.Format(DateLayout) != date {
			fail("row %d: the date %q must be written YYYY-MM-DD", row, date)
			continue
		}
		if date > snap.AsOf {
			fail("row %d: %s is after the list's as-of date %s", row, date, snap.AsOf)
			continue
		}
		added, removed := norm(get(rec, "added"), row, "added"), norm(get(rec, "removed"), row, "removed")
		if get(rec, "added") == "" && get(rec, "removed") == "" {
			fail("row %d: a change needs an added or a removed symbol", row)
			continue
		}
		if added != "" && added == removed {
			fail("row %d: %s cannot be added and removed on the same row", row, added)
			continue
		}
		changes = append(changes, Change{Date: date, Added: added, Removed: removed})
		if len(changes) > MaxChanges {
			fail("more than %d change rows", MaxChanges)
			break
		}
	}
	if len(changes) == 0 && len(problems) == 0 {
		problems = append(problems, "the history file has no rows")
	}
	if len(problems) > 0 {
		return problems
	}
	sort.SliceStable(changes, func(a, b int) bool { return changes[a].Date < changes[b].Date })
	snap.Changes = changes
	snap.PointInTime = true
	snap.HistoryFrom = changes[0].Date
	// Replay backwards from today's list; a contradiction means the two files do not belong together.
	members := map[string]bool{}
	for _, m := range snap.Members {
		members[m.Symbol] = true
	}
	inconsistent := 0
	for i := len(changes) - 1; i >= 0; i-- {
		c := changes[i]
		if c.Added != "" {
			if !members[c.Added] {
				inconsistent++
			}
			delete(members, c.Added)
		}
		if c.Removed != "" {
			if members[c.Removed] {
				inconsistent++
			}
			members[c.Removed] = true
		}
	}
	if inconsistent > 0 {
		snap.Warnings = append(snap.Warnings, fmt.Sprintf("%d history rows do not fit today's list (a symbol joined that is not a member, or left but still is); check that both files are from the same source and date.", inconsistent))
	}
	snap.Warnings = dropSurvivorshipNotice(snap.Warnings)
	snap.Warnings = append(snap.Warnings, "Past membership is rebuilt from the history file you supplied, back to "+snap.HistoryFrom+"; earlier dates fall back to today's members and are labeled. Tickers can be reused by other companies.")
	return nil
}

// dropSurvivorshipNotice removes the "today's members only" notice that a
// list without history carries, once history has been attached.
func dropSurvivorshipNotice(w []string) []string {
	out := w[:0:0]
	for _, s := range w {
		if strings.HasPrefix(s, "Only today's members were supplied") {
			continue
		}
		out = append(out, s)
	}
	return out
}
