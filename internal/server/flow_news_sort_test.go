package server

import (
	"strings"
	"testing"
)

func TestNewsRowsSortNewestFirstAndUndatedLast(t *testing.T) {
	rows := []map[string]any{
		{"id": "undated", "date_status": "unavailable"},
		{"id": "old", "published": "2026-10-30T09:00:00Z"},
		{"id": "new", "published": "2026-11-03T14:32:00Z"},
		{"id": "undated2"},
	}
	sortNewsRows(rows)
	got := []string{}
	for _, r := range rows {
		got = append(got, r["id"].(string))
	}
	if strings.Join(got, ",") != "new,old,undated,undated2" {
		t.Fatalf("order = %v", got)
	}
}
