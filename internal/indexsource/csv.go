package indexsource

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// MaxCSVBytes bounds an upload.
const MaxCSVBytes = 512 * 1024

// Meta is what the user states about the file, beside the file itself.
type Meta struct {
	AsOf           string
	SourceName     string
	LicenseTerms   string
	LicenseConfirm bool
}

// exchangeSuffix maps an exchange name or code a CSV may carry to the
// Yahoo-style suffix the rest of the app reads. Only exchanges FloweySales
// recognizes are here; anything else is an error, never a guess.
var exchangeSuffix = map[string]string{
	"ST": "ST", "XSTO": "ST", "STO": "ST", "NASDAQ STOCKHOLM": "ST", "STOCKHOLM": "ST", "SS": "ST",
}

// countrySuffix is the suffix a list for that country must carry. US is empty.
var countrySuffix = map[string]string{"SE": "ST", "US": ""}

var symbolRe = regexp.MustCompile(`^[A-Z0-9][A-Z0-9._-]{0,19}$`)

// ParseCSV reads a constituent file. It returns the snapshot, or the full
// list of reasons it was refused (one per problem, with row numbers).
func ParseCSV(data []byte, spec Spec, meta Meta, now time.Time) (Snapshot, []string, error) {
	var problems []string
	fail := func(f string, a ...any) { problems = append(problems, fmt.Sprintf(f, a...)) }
	if err := Confirm(meta.LicenseTerms, meta.LicenseConfirm); err != nil {
		fail("%s", err)
	}
	if strings.TrimSpace(meta.SourceName) == "" || len(meta.SourceName) > 200 {
		fail("the source name is required (up to 200 characters)")
	}
	asOf, err := time.Parse(DateLayout, strings.TrimSpace(meta.AsOf))
	switch {
	case err != nil:
		fail("as_of must be a date written YYYY-MM-DD (UTC)")
	case asOf.After(now.UTC()):
		fail("as_of %s is in the future", meta.AsOf)
	}
	if len(data) > MaxCSVBytes {
		return Snapshot{}, append(problems, "the file is larger than 512 KiB"), errors.New("file too large")
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	header, err := r.Read()
	if err != nil {
		return Snapshot{}, append(problems, "the file is empty or has no header row"), errors.New("no header")
	}
	col := map[string]int{}
	for i, h := range header {
		name := strings.ToLower(strings.TrimSpace(h))
		if _, dup := col[name]; dup {
			fail("column %q appears twice", name)
		}
		col[name] = i
	}
	symCol, ok := col["symbol"]
	if !ok {
		return Snapshot{}, append(problems, "the header needs a symbol column (optional: name, weight, exchange, isin)"), errors.New("no symbol column")
	}
	get := func(rec []string, name string) string {
		if i, ok := col[name]; ok && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}
	snap := Snapshot{IndexID: spec.ID, Provider: ProviderCSV, SourceName: strings.TrimSpace(meta.SourceName), AsOf: strings.TrimSpace(meta.AsOf),
		ImportedAt: now.UTC().Format(time.RFC3339), LicenseTerms: strings.TrimSpace(meta.LicenseTerms), LicenseConfirm: meta.LicenseConfirm, Warnings: []string{}}
	seen := map[string]int{}
	seenISIN := map[string]string{}
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
			continue // blank line
		}
		raw := ""
		if symCol < len(rec) {
			raw = rec[symCol]
		}
		m, err := normalizeMember(raw, get(rec, "exchange"), spec)
		if err != nil {
			fail("row %d: %v", row, err)
			continue
		}
		m.Name = get(rec, "name")
		if isin := strings.ToUpper(get(rec, "isin")); isin != "" {
			if !ValidISIN(isin) {
				fail("row %d: %q is not a valid ISIN (12 characters with a correct check digit; leave it blank when unknown)", row, isin)
				continue
			}
			m.ISIN = isin
		}
		if w := get(rec, "weight"); w != "" {
			v, perr := strconv.ParseFloat(strings.TrimSuffix(w, "%"), 64)
			if perr != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 100 {
				fail("row %d: weight %q is not a number from 0 to 100 (leave it blank when unknown)", row, w)
				continue
			}
			m.Weight = &v
		}
		if first, dup := seen[m.Symbol]; dup {
			fail("row %d: %s appears twice (first on row %d)", row, m.Symbol, first)
			continue
		}
		if m.ISIN != "" {
			if other, dup := seenISIN[m.ISIN]; dup {
				fail("row %d: ISIN %s is already used by %s", row, m.ISIN, other)
				continue
			}
			seenISIN[m.ISIN] = m.Symbol
		}
		seen[m.Symbol] = row
		snap.Members = append(snap.Members, m)
		if len(snap.Members) > MaxMembers {
			fail("more than %d rows", MaxMembers)
			break
		}
	}
	problems = append(problems, CheckCount(len(snap.Members), spec, &snap)...)
	weighted := 0
	var sum float64
	for _, m := range snap.Members {
		if m.Weight != nil {
			weighted++
			sum += *m.Weight
		}
	}
	if weighted > 0 && weighted < len(snap.Members) {
		snap.Warnings = append(snap.Warnings, fmt.Sprintf("%d of %d rows have no weight; they stay blank, not 0.", len(snap.Members)-weighted, len(snap.Members)))
	}
	if weighted == len(snap.Members) && weighted > 0 && (sum < 95 || sum > 105) {
		snap.Warnings = append(snap.Warnings, fmt.Sprintf("The weights add up to %.1f, not about 100; check the column.", sum))
	}
	snap.Warnings = append(snap.Warnings, "Only today's members were supplied; past membership is not known, so a backtest on this list is survivorship-biased.")
	if len(problems) > 0 {
		return Snapshot{}, problems, errors.New("the file was refused")
	}
	return snap, nil, nil
}

// CheckCount compares the list size with the index's nominal size. Far from
// nominal is a refusal (a wrong file); modestly off is a warning on snap.
func CheckCount(n int, spec Spec, snap *Snapshot) []string {
	if n == 0 {
		return []string{"the list has no rows"}
	}
	if spec.Nominal <= 0 {
		return nil
	}
	lo, hi := spec.Nominal/2, spec.Nominal*3/2+5
	if n < lo || n > hi {
		return []string{fmt.Sprintf("%d rows is far from the %d in the %s name (expected about %d to %d); this looks like the wrong list", n, spec.Nominal, spec.Name, lo, hi)}
	}
	if n != spec.Nominal {
		snap.Warnings = append(snap.Warnings, fmt.Sprintf("The list has %d securities; the index name says %d. Share classes or a recent change can explain the gap.", n, spec.Nominal))
	}
	return nil
}

// normalizeMember turns a supplied symbol (and optional exchange) into the
// app's ticker format, or says why it cannot.
func normalizeMember(rawSymbol, exchange string, spec Spec) (Member, error) {
	s := strings.ToUpper(strings.TrimSpace(rawSymbol))
	if s == "" {
		return Member{}, errors.New("the symbol is empty")
	}
	s = strings.ReplaceAll(s, " ", "-")
	want, known := countrySuffix[spec.Country]
	if !known {
		return Member{}, fmt.Errorf("%s lists are not supported", spec.Country)
	}
	exch := strings.ToUpper(strings.TrimSpace(exchange))
	if want == "" { // US: class shares are written with a dash (BRK-B)
		if _, foreign := exchangeSuffix[exch]; exch != "" && foreign {
			return Member{}, fmt.Errorf("%s: exchange %q is not a US exchange", s, exchange)
		}
		return finish(strings.ReplaceAll(s, ".", "-"), exchange)
	}
	if i := strings.LastIndex(s, "."); i > 0 {
		if s[i+1:] != want {
			return Member{}, fmt.Errorf("%s: the suffix is not .%s", s, want)
		}
		if exch != "" && exchangeSuffix[exch] != want {
			return Member{}, fmt.Errorf("%s: exchange %q does not match the .%s suffix", s, exchange, want)
		}
		return finish(strings.ReplaceAll(s[:i], ".", "-")+"."+want, exchange)
	}
	if exch == "" {
		return Member{}, fmt.Errorf("%s: no exchange; write it as %s.%s or fill the exchange column (for example %s)", s, s, want, want)
	}
	if exchangeSuffix[exch] != want {
		return Member{}, fmt.Errorf("%s: exchange %q is not one of this index's (%s)", s, exchange, want)
	}
	return finish(s+"."+want, exchange)
}

func finish(symbol, exchange string) (Member, error) {
	if !symbolRe.MatchString(symbol) {
		return Member{}, fmt.Errorf("%q is not a valid symbol", symbol)
	}
	return Member{Symbol: symbol, Exchange: strings.TrimSpace(exchange)}, nil
}

// NormalizeUS makes a provider symbol (BRK.B) the app's form (BRK-B).
func NormalizeUS(raw string) (string, bool) {
	s := strings.ToUpper(strings.TrimSpace(raw))
	s = strings.ReplaceAll(strings.ReplaceAll(s, ".", "-"), " ", "-")
	return s, symbolRe.MatchString(s)
}
