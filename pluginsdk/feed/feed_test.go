package feed

import (
	"strings"
	"testing"
	"time"
)

const rss = `<?xml version="1.0"?><rss version="2.0"><channel><item>
<title>Saab wins &amp; grows</title><link>https://example.com/a?x=1</link><description>Order &lt;b&gt;book&lt;/b&gt; up</description>
<pubDate>Sat, 07 Nov 2026 19:23:00 +0000</pubDate></item>
<item><title>No date</title><link>https://example.com/b</link></item>
<item><title>Bad date</title><link>https://example.com/c</link><pubDate>yesterday</pubDate></item>
<item><title></title><link>https://example.com/d</link></item>
<item><title>Plain http</title><link>http://example.com/e</link><pubDate>Sat, 07 Nov 2026 10:00:00 GMT</pubDate></item>
</channel></rss>`

func TestParseRSSKeepsDatesAndNeverGuesses(t *testing.T) {
	items, err := Parse([]byte(rss), 0)
	if err != nil || len(items) != 4 {
		t.Fatalf("items %d err %v", len(items), err)
	}
	want := time.Date(2026, 11, 7, 19, 23, 0, 0, time.UTC)
	if items[0].Title != "Saab wins & grows" || items[0].Summary != "Order book up" || !items[0].Published.Equal(want) {
		t.Fatalf("first item: %+v", items[0])
	}
	if !items[1].Published.IsZero() || !items[2].Published.IsZero() {
		t.Fatal("a missing or unreadable date must stay zero")
	}
}

func TestParseAtomAndLimit(t *testing.T) {
	atom := `<feed xmlns="http://www.w3.org/2005/Atom"><entry><title>A</title><link rel="alternate" href="https://e.com/a"/><updated>2026-11-06T08:00:00Z</updated></entry>
<entry><title>B</title><link href="https://e.com/b"/><published>2026-11-05T08:00:00+01:00</published></entry></feed>`
	items, err := Parse([]byte(atom), 1)
	if err != nil || len(items) != 1 || items[0].Link != "https://e.com/a" || !items[0].Published.Equal(time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("%+v %v", items, err)
	}
}

func TestParseIsSafe(t *testing.T) {
	evil := `<?xml version="1.0"?><!DOCTYPE r [<!ENTITY x SYSTEM "file:///etc/passwd">]><rss><channel><item><title>&x;t</title><link>https://e.com</link></item></channel></rss>`
	items, _ := Parse([]byte(evil), 0)
	for _, it := range items {
		if strings.Contains(it.Title, "root:") {
			t.Fatal("external entity was expanded")
		}
	}
	if _, err := Parse([]byte(strings.Repeat("a", MaxBytes+1)), 0); err == nil {
		t.Fatal("an oversized feed must be refused")
	}
	if _, err := Parse([]byte("not xml <"), 0); err == nil {
		t.Fatal("garbage must be refused")
	}
}

func TestNewsItemsKeepOnlyHTTPSAndZeroUnknownDates(t *testing.T) {
	items, _ := Parse([]byte(rss), 0)
	news := NewsItems(items, "SAAB-B.ST", "YAHOO", 10)
	if len(news) != 3 || news[0].Published != 1794079380 || news[1].Published != 0 || news[0].Provider != "YAHOO" {
		t.Fatalf("%+v", news)
	}
	if got := NewsItems(items, "X", "Y", 1); len(got) != 1 {
		t.Fatal("limit not applied")
	}
}

func TestParseTimeFormats(t *testing.T) {
	for _, s := range []string{"Sat, 07 Nov 2026 19:23:00 +0000", "Sat, 7 Nov 2026 19:23:00 GMT", "2026-11-07T19:23:00Z", "2026-11-07T20:23:00+01:00"} {
		if tm, ok := ParseTime(s); !ok || tm.Unix() != 1794079380 {
			t.Fatalf("%q -> %v %v", s, tm, ok)
		}
	}
	for _, s := range []string{"", "soon", "Mon, 01 Jan 0001 00:00:00 +0000"} {
		if _, ok := ParseTime(s); ok {
			t.Fatalf("%q should not parse", s)
		}
	}
}
