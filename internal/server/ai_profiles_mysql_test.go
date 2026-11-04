package server

import (
	"context"
	"errors"
	"os"
	"testing"

	"floweysales/internal/storage"
)

// The real storage behind model profiles and saved teams. It needs the
// disposable Liquibase-migrated MySQL that CI starts.
func TestModelProfilesAndSavedTeamsOnMySQL(t *testing.T) {
	dsn := os.Getenv("STORAGE_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("requires disposable Liquibase-migrated MySQL")
	}
	db, err := storage.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	// Start clean: other tests share the database.
	existing, _ := db.ListModelProfiles(ctx, false)
	for _, p := range existing {
		_ = db.DeleteModelProfile(ctx, p.ID)
	}

	yes, window, cost := true, 128000, 3.5
	a, err := db.SaveModelProfile(ctx, storage.ModelProfile{Name: "Deep", Description: "slow", Model: "big", FallbackModel: "small", SupportsTools: &yes, ContextWindow: &window, CostInputPerMTok: &cost, Enabled: true, IsDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	b, err := db.SaveModelProfile(ctx, storage.ModelProfile{Name: "Fast", Model: "fast", Enabled: true, IsDefault: true, Position: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SaveModelProfile(ctx, storage.ModelProfile{Name: "Deep", Model: "other", Enabled: true}); !errors.Is(err, storage.ErrProfileNameTaken) {
		t.Errorf("a duplicate name must be refused, got %v", err)
	}
	list, err := db.ListModelProfiles(ctx, false)
	if err != nil || len(list) != 2 {
		t.Fatalf("%v %+v", err, list)
	}
	defaults := 0
	for _, p := range list {
		if p.IsDefault {
			defaults++
			if p.ID != b {
				t.Errorf("the last profile saved as default should be the default, got %d", p.ID)
			}
		}
	}
	if defaults != 1 {
		t.Errorf("exactly one default, got %d", defaults)
	}
	first, _ := db.GetModelProfile(ctx, a)
	if first.SupportsTools == nil || !*first.SupportsTools || first.ContextWindow == nil || *first.ContextWindow != 128000 || first.FallbackModel != "small" || first.CostOutputPerMTok != nil {
		t.Errorf("known values round-trip and unknown ones stay unknown: %+v", first)
	}

	// Teams belong to their user, and deleting a profile releases it from the teams that used it.
	team := storage.SavedTeam{Name: "Panel", Chair: true, Members: []storage.SavedTeamMember{{Persona: "owner", ProfileID: &a}, {Persona: "teacher"}}}
	id, err := db.SaveSavedTeam(ctx, 101, team)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SaveSavedTeam(ctx, 101, team); !errors.Is(err, storage.ErrTeamNameTaken) {
		t.Errorf("a duplicate team name must be refused, got %v", err)
	}
	if _, err := db.SaveSavedTeam(ctx, 202, team); err != nil {
		t.Errorf("another user may use the same name: %v", err)
	}
	if _, err := db.GetSavedTeam(ctx, 202, id); !errors.Is(err, storage.ErrTeamNotFound) {
		t.Errorf("a team is not readable by another user, got %v", err)
	}
	team.ID, team.Name, team.Members = id, "Panel 2", team.Members[:1]
	if _, err := db.SaveSavedTeam(ctx, 202, team); !errors.Is(err, storage.ErrTeamNotFound) {
		t.Errorf("a team is not replaceable by another user, got %v", err)
	}
	if _, err := db.SaveSavedTeam(ctx, 101, team); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetSavedTeam(ctx, 101, id)
	if err != nil || got.Name != "Panel 2" || len(got.Members) != 1 || got.Members[0].ProfileID == nil || *got.Members[0].ProfileID != a {
		t.Fatalf("replace: %v %+v", err, got)
	}
	if err := db.DeleteModelProfile(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetSavedTeam(ctx, 101, id)
	if got.Members[0].ProfileID != nil {
		t.Errorf("a member whose profile was deleted falls back to the default: %+v", got.Members[0])
	}
	if err := db.DeleteSavedTeam(ctx, 101, id); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteSavedTeam(ctx, 101, id); !errors.Is(err, storage.ErrTeamNotFound) {
		t.Errorf("delete twice: %v", err)
	}
	_ = db.DeleteModelProfile(ctx, b)
}
