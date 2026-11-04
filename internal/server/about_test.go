package server

import "testing"

func TestAboutInfo(t *testing.T) {
	dev := aboutInfo("", "")
	if dev["development"] != true || dev["commit"] != nil || dev["build_time"] != nil {
		t.Fatalf("a build without data must say so: %v", dev)
	}
	rel := aboutInfo("abc1234", "2026-11-04T12:30:00+01:00")
	if rel["development"] != false || rel["commit"] != "abc1234" || rel["build_time"] != "2026-11-04T11:30:00Z" {
		t.Fatalf("build data must be shown in UTC: %v", rel)
	}
	if aboutInfo("abc", "yesterday")["build_time"] != nil {
		t.Fatal("an unparseable time must not be shown")
	}
	if rel["docs"] != "https://doldas.github.io/floweysales/" || rel["license"] != "MIT" {
		t.Fatalf("links and license: %v", rel)
	}
}
