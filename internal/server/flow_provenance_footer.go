package server

import (
	"fmt"
	"sort"
	"strings"

	"floweysales/internal/provenance"
)

// provenanceFooter is the Sources line under a report table: the distinct
// origins of its rows, or an explicit statement when a row records none.
func provenanceFooter(rows []map[string]any) string {
	seen := map[string]bool{}
	order := []string{}
	unknown := 0
	for _, row := range rows {
		raw, ok := row["provenance"].(map[string]any)
		if !ok {
			unknown++
			continue
		}
		status, _ := raw["status"].(string)
		if status == provenance.Unknown {
			unknown++
			continue
		}
		who, _ := raw["provider"].(string)
		if who == "" {
			if m, ok := raw["method"].(map[string]any); ok {
				who, _ = m["id"].(string)
			}
		}
		kind, _ := raw["kind"].(string)
		label := strings.TrimSpace(who + " (" + kind + ")")
		if !seen[label] {
			seen[label] = true
			order = append(order, label)
		}
	}
	parts := []string{}
	if len(order) > 0 {
		sort.Strings(order)
		parts = append(parts, strings.Join(order, "; "))
	}
	if unknown > 0 {
		parts = append(parts, fmt.Sprintf("source unknown for %d row(s)", unknown))
	}
	return "\n_Sources: " + strings.Join(parts, "; ") + ". Row-level detail is in the provenance field of each row._\n"
}
