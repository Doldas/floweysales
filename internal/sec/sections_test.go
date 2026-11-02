package sec

import (
	"context"
	"strings"
	"testing"
)

func TestFilingSectionExtraction(t *testing.T) {
	raw := []byte(`<html><head><style>private</style></head><body><p>TABLE OF CONTENTS</p>
 <p>Item 7. Management Discussion</p><p>Item 8. Accounts</p>
 <h2>Item 7. Management's Discussion and Analysis</h2>
 <p>Operating segments generated important revenues, cash flow and financial statements.</p>
 <p>Our CFO improved for five quarters; repeated information should not be guessed.</p>
 <h2>Item 7A. Market Risks</h2><p>Foreign exchange.</p></body></html>`)
	text := cleanFilingHTML(raw)
	snippet, truncated, err := extractItem(text, "7", 12000)
	if err != nil {
		t.Fatal(err)
	}
	if truncated || !strings.Contains(snippet, "Our CFO improved") || strings.Contains(snippet, "Foreign exchange") {
		t.Fatalf("wrong excerpt %q", snippet)
	}
	if !allowedItem("10-K", "7") || allowedItem("10-Q", "7") || allowedItem("8-K", "3") {
		t.Fatal("invalid form item policy")
	}
	if _, _, err := extractItem(text, "9A", 12000); err == nil {
		t.Fatal("missing SEC section must not hallucinate")
	}
}

func TestItemNumbersAreNormalised(t *testing.T) {
	for in, want := range map[string]string{"7": "7", "7.0": "7", "Item 7": "7", "item 7a": "7A", " 1a ": "1A", "2.02": "2.02", "7.": "7", "ITEM 2.02": "2.02"} {
		if got := normalizeItem(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

func TestUnsupportedItemsListWhatIsSupported(t *testing.T) {
	c := NewClient("FloweySales test@example.com")
	for _, tc := range []struct{ form, item string }{{"10-Q", "7.0"}, {"10-K", "99"}, {"S-1", "1"}} {
		_, err := c.FilingSection(context.Background(), "NVDA", tc.form, tc.item)
		if err == nil || !strings.Contains(err.Error(), "Supported:") || !strings.Contains(err.Error(), "10-Q items 1 (financial statements), 2 (MD&A)") {
			t.Errorf("%s %s: %v", tc.form, tc.item, err)
		}
	}
	// "7.0" is item 7: it passes the form/item check and fails later, on the network, not on the item.
	if _, err := NewClient("").FilingSection(context.Background(), "NVDA", "10-K", "7.0"); err == nil || strings.Contains(err.Error(), "Supported:") {
		t.Errorf("10-K 7.0 must be accepted as item 7: %v", err)
	}
}
