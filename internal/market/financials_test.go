package market

import "testing"

func TestQuarterLabelIsExplicitCalendar(t *testing.T) {
	if got := quarterLabel("2026-07-31"); got != "CY2026 Q3" {
		t.Fatalf("calendar quarter = %q, want CY2026 Q3", got)
	}
}
