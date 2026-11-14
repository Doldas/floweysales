// Package instrumentstest is the shared behavior every instruments.Store
// must have; the memory store and the MySQL store both run it.
package instrumentstest

import (
	"context"
	"testing"
	"time"

	"floweysales/internal/instruments"
)

// Run checks s. alice and bob are two distinct user ids with no overrides.
// The symbols are made unique per run so a shared database can be reused.
func Run(t *testing.T, s instruments.Store, alice, bob int64, suffix string) {
	ctx := context.Background()
	t.Helper()
	nvda := instruments.Key{Provider: "TESTA", Symbol: "NVDA" + suffix}
	gold := instruments.Key{Provider: "TESTB", Symbol: "GC=F" + suffix}
	t.Cleanup(func() {
		_, _ = s.DeleteProfiles(ctx, "TESTA")
		_, _ = s.DeleteProfiles(ctx, "TESTB")
	})

	t.Run("profiles round-trip and replace", func(t *testing.T) {
		emp := int64(36000)
		now := time.Now().UTC().Truncate(time.Second)
		in := instruments.CachedProfile{Key: nvda, Status: instruments.StatusOK, RetrievedAt: now, ExpiresAt: now.Add(instruments.ProfileTTL),
			Profile: &instruments.Profile{Symbol: nvda.Symbol, Kind: "company", Name: "NVIDIA", Sector: "Technology", Provider: "TESTA",
				Company: &instruments.CompanyProfile{Employees: &emp}}}
		if err := s.PutProfile(ctx, in); err != nil {
			t.Fatal(err)
		}
		if err := s.PutProfile(ctx, instruments.CachedProfile{Key: gold, Status: instruments.StatusNotFound, RetrievedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		got, err := s.GetProfiles(ctx, []instruments.Key{nvda, gold, {Provider: "TESTA", Symbol: "NONE" + suffix}})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatalf("want 2 cached entries, got %d", len(got))
		}
		p := got[nvda]
		if p.Status != instruments.StatusOK || p.Profile == nil || p.Profile.Name != "NVIDIA" || p.Profile.Company == nil || *p.Profile.Company.Employees != emp {
			t.Fatalf("profile did not round-trip: %+v", p)
		}
		if !p.RetrievedAt.Equal(now) || !p.ExpiresAt.Equal(now.Add(instruments.ProfileTTL)) {
			t.Fatalf("times did not round-trip: %v %v", p.RetrievedAt, p.ExpiresAt)
		}
		if g := got[gold]; g.Status != instruments.StatusNotFound || g.Profile != nil {
			t.Fatalf("not-found entry: %+v", g)
		}
		in.Profile.Name = "NVIDIA Corporation"
		if err := s.PutProfile(ctx, in); err != nil {
			t.Fatal(err)
		}
		got, _ = s.GetProfiles(ctx, []instruments.Key{nvda})
		if got[nvda].Profile.Name != "NVIDIA Corporation" {
			t.Fatalf("profile was not replaced: %+v", got[nvda].Profile)
		}
		n, err := s.DeleteProfiles(ctx, "TESTB")
		if err != nil || n < 1 {
			t.Fatalf("delete by provider: %d %v", n, err)
		}
		got, _ = s.GetProfiles(ctx, []instruments.Key{nvda, gold})
		if _, ok := got[gold]; ok || len(got) != 1 {
			t.Fatalf("only TESTB should be cleared: %+v", got)
		}
	})

	t.Run("overrides are private to their user", func(t *testing.T) {
		o := instruments.Override{Key: nvda, Sector: "technology", Country: "US", Themes: []string{"AI", "Data centers"}}
		if err := s.PutOverride(ctx, alice, o); err != nil {
			t.Fatal(err)
		}
		mine, err := s.GetOverrides(ctx, alice, []instruments.Key{nvda, gold})
		if err != nil {
			t.Fatal(err)
		}
		got, ok := mine[nvda]
		if !ok || got.Sector != "technology" || got.Country != "US" || len(got.Themes) != 2 || got.Themes[0] != "AI" || got.UpdatedAt.IsZero() {
			t.Fatalf("override did not round-trip: %+v", got)
		}
		theirs, _ := s.GetOverrides(ctx, bob, []instruments.Key{nvda})
		if len(theirs) != 0 {
			t.Fatalf("bob sees alice's override: %+v", theirs)
		}
		o.Sector, o.Themes = "", nil
		if err := s.PutOverride(ctx, alice, o); err != nil {
			t.Fatal(err)
		}
		mine, _ = s.GetOverrides(ctx, alice, []instruments.Key{nvda})
		if mine[nvda].Sector != "" || len(mine[nvda].Themes) != 0 || mine[nvda].Country != "US" {
			t.Fatalf("override was not replaced: %+v", mine[nvda])
		}
		if err := s.PutOverride(ctx, alice, instruments.Override{Key: nvda}); err != nil {
			t.Fatal(err)
		}
		if mine, _ = s.GetOverrides(ctx, alice, []instruments.Key{nvda}); len(mine) != 0 {
			t.Fatalf("an empty override should delete: %+v", mine)
		}
		_ = s.PutOverride(ctx, bob, instruments.Override{Key: gold, AssetClass: "commodity"})
		if err := s.DeleteOverride(ctx, bob, gold); err != nil {
			t.Fatal(err)
		}
		if theirs, _ = s.GetOverrides(ctx, bob, []instruments.Key{gold}); len(theirs) != 0 {
			t.Fatalf("delete did not remove: %+v", theirs)
		}
	})
}
