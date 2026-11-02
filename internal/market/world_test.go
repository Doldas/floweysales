package market

import "testing"

func TestClassifyHeadlineMatchesWholeWords(t *testing.T) {
	got := ClassifyHeadline("Brent crude jumps as OPEC cuts output")
	if len(got) == 0 || got[0] != "oil_energy" {
		t.Fatalf("themes = %v, want oil_energy first", got)
	}
	// "coil" and "ai" inside other words must not match.
	if got := ClassifyHeadline("Coil maker said to fail, again"); len(got) != 0 {
		t.Fatalf("substring matched: %v", got)
	}
}

func TestClassifyHeadlineCanHitSeveralThemes(t *testing.T) {
	got := ClassifyHeadline("Fed holds rates while oil climbs on Iran sanctions")
	want := map[string]bool{"rates_inflation": true, "oil_energy": true, "geopolitics": true}
	for _, id := range got {
		delete(want, id)
	}
	if len(want) != 0 {
		t.Fatalf("missing themes %v in %v", want, got)
	}
}

func TestEveryThemeHasQueryKeywordsAndSectorExposure(t *testing.T) {
	ids := map[string]bool{}
	for _, th := range WorldThemes {
		if ids[th.ID] {
			t.Fatalf("duplicate theme %s", th.ID)
		}
		ids[th.ID] = true
		if th.Query == "" || len(th.Keywords) == 0 || len(th.Exposures) == 0 {
			t.Errorf("%s is incomplete", th.ID)
		}
		for _, x := range th.Exposures {
			if x.Why == "" || (x.Role != RoleBenefits && x.Role != RoleHurt && x.Role != RoleMixed) {
				t.Errorf("%s: bad exposure %+v", th.ID, x)
			}
			if x.Kind == KindSector && NormalizeSector(x.Name) != x.Name {
				t.Errorf("%s: unknown sector %q", th.ID, x.Name)
			}
		}
	}
}

func TestSectorLookup(t *testing.T) {
	if SectorOf(" xom ") != SectorEnergy {
		t.Fatal("XOM should be Energy")
	}
	if SectorOf("NOPE") != "" {
		t.Fatal("unknown symbol must have no sector")
	}
	for in, want := range map[string]string{"technology": SectorTech, "Health care": SectorHealth, "consumer defensive": SectorStaples, "Utilities": SectorUtilities, "mystery": ""} {
		if got := NormalizeSector(in); got != want {
			t.Errorf("NormalizeSector(%q) = %q, want %q", in, got, want)
		}
	}
}
