// Package fixture builds synthetic constituent files for tests. Every company
// is invented ("Synthetic Co 007"); no real index membership, name or ISIN is
// used, so nothing here is proprietary data. The files have the shape of a
// real import and pass the same validation.
package fixture

import (
	"fmt"
	"strings"
)

// Row is one synthetic constituent.
type Row struct {
	Symbol   string
	Name     string
	Weight   string
	Exchange string
	ISIN     string
}

// ISIN builds a well-formed ISIN with a correct check digit from a country
// code and a serial number (not a real security).
func ISIN(country string, serial int) string {
	body := fmt.Sprintf("%s%09d", country, serial)
	var digits []int
	for _, r := range body {
		if r >= 'A' && r <= 'Z' {
			v := int(r-'A') + 10
			digits = append(digits, v/10, v%10)
		} else {
			digits = append(digits, int(r-'0'))
		}
	}
	sum, double := 0, true // the check digit is appended, so the rightmost digit so far is doubled
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
	return fmt.Sprintf("%s%d", body, (10-sum%10)%10)
}

// Rows makes n rows. Symbols are prefix plus a three-digit number plus the
// suffix (".ST" for a Stockholm list, "" for a US list). With isinCountry set
// every row carries an ISIN, serials starting at serialBase.
func Rows(prefix, suffix string, n, serialBase int, isinCountry string) []Row {
	rows := make([]Row, n)
	for i := range rows {
		r := Row{Symbol: fmt.Sprintf("%s%03d%s", prefix, i+1, suffix), Name: fmt.Sprintf("Synthetic Co %s%03d", prefix, i+1)}
		if isinCountry != "" {
			r.ISIN = ISIN(isinCountry, serialBase+i)
		}
		rows[i] = r
	}
	return rows
}

// CSV renders rows with the optional columns that any row uses.
func CSV(rows []Row) string {
	hasW, hasE, hasI := false, false, false
	for _, r := range rows {
		hasW, hasE, hasI = hasW || r.Weight != "", hasE || r.Exchange != "", hasI || r.ISIN != ""
	}
	head := []string{"symbol", "name"}
	if hasW {
		head = append(head, "weight")
	}
	if hasE {
		head = append(head, "exchange")
	}
	if hasI {
		head = append(head, "isin")
	}
	var b strings.Builder
	b.WriteString(strings.Join(head, ",") + "\n")
	for _, r := range rows {
		f := []string{r.Symbol, r.Name}
		if hasW {
			f = append(f, r.Weight)
		}
		if hasE {
			f = append(f, r.Exchange)
		}
		if hasI {
			f = append(f, r.ISIN)
		}
		b.WriteString(strings.Join(f, ",") + "\n")
	}
	return b.String()
}

// History renders "date,added,removed" rows; each entry is {date, added, removed}.
func History(changes [][3]string) string {
	var b strings.Builder
	b.WriteString("date,added,removed\n")
	for _, c := range changes {
		b.WriteString(c[0] + "," + c[1] + "," + c[2] + "\n")
	}
	return b.String()
}
