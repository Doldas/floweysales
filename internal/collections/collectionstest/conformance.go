// Package collectionstest checks that a collections.Store keeps the rules
// every implementation shares: ownership, unique names, one member per
// instrument key, limits and transfers. The memory store runs it in every
// build; the MySQL store runs it in CI against the Liquibase schema.
package collectionstest

import (
	"context"
	"errors"
	"testing"

	"floweysales/internal/collections"
)

// Run exercises store through collections.Service. alice and bob must be
// user ids with no collections yet.
func Run(t *testing.T, store collections.Store, alice, bob int64) {
	t.Helper()
	ctx := context.Background()
	svc := collections.Service{Store: store}

	tech, err := svc.Create(ctx, alice, collections.Collection{Name: " Technology ", Category: "Theme", Tags: []string{"AI", "ai", "Chips"}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if tech.Name != "Technology" || tech.Kind != collections.KindWatch || len(tech.Tags) != 2 {
		t.Fatalf("normalized collection = %+v", tech)
	}
	if _, err := svc.Create(ctx, alice, collections.Collection{Name: "technology"}); !errors.Is(err, collections.ErrNameTaken) {
		t.Fatalf("a name differing only in case must be taken, got %v", err)
	}
	if _, err := svc.Create(ctx, bob, collections.Collection{Name: "Technology"}); err != nil {
		t.Fatalf("another user may reuse the name: %v", err)
	}
	med, err := svc.Create(ctx, alice, collections.Collection{Name: "Medicine"})
	if err != nil {
		t.Fatal(err)
	}

	// The same ticker on two providers is two instruments; the same
	// instrument may sit in several collections.
	nvda := collections.Instrument{Provider: "yahoo", Symbol: "nvda", Name: "NVIDIA", Exchange: "NASDAQ", Currency: "usd", Type: "equity"}
	m1, err := svc.AddMember(ctx, alice, tech.ID, collections.Member{Instrument: nvda, Tags: []string{"AI"}})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if m1.Instrument.Key() != "YAHOO:NVDA" || m1.Instrument.Currency != "USD" {
		t.Fatalf("instrument not canonical: %+v", m1.Instrument)
	}
	if _, err := svc.AddMember(ctx, alice, tech.ID, collections.Member{Instrument: collections.Instrument{Provider: "FINNHUB", Symbol: "NVDA"}}); err != nil {
		t.Fatalf("the same ticker from another provider is a separate member: %v", err)
	}
	if _, err := svc.AddMember(ctx, alice, tech.ID, collections.Member{Instrument: nvda}); !errors.Is(err, collections.ErrDuplicateMember) {
		t.Fatalf("duplicate member: %v", err)
	}
	if _, err := svc.AddMember(ctx, alice, med.ID, collections.Member{Instrument: nvda}); err != nil {
		t.Fatalf("one instrument in two collections: %v", err)
	}
	in, err := svc.Containing(ctx, alice, collections.Instrument{Provider: "YAHOO", Symbol: "NVDA"})
	if err != nil || len(in) != 2 {
		t.Fatalf("containing = %v, %v", in, err)
	}
	if in, _ := svc.Containing(ctx, bob, collections.Instrument{Provider: "YAHOO", Symbol: "NVDA"}); len(in) != 0 {
		t.Fatalf("bob must not see alice's collections: %v", in)
	}

	// Watch collections carry no weights.
	w := 10.0
	if _, err := svc.AddMember(ctx, alice, tech.ID, collections.Member{Instrument: collections.Instrument{Provider: "YAHOO", Symbol: "AMD"}, Weight: &w}); !errors.Is(err, collections.ErrInvalid) {
		t.Fatalf("a weight in a watch collection must be refused, got %v", err)
	}

	// Ownership: bob can neither read nor change alice's rows.
	if _, err := svc.Get(ctx, bob, tech.ID); !errors.Is(err, collections.ErrNotFound) {
		t.Fatalf("bob read alice's collection: %v", err)
	}
	if _, _, err := svc.Members(ctx, bob, tech.ID, collections.Page{}); !errors.Is(err, collections.ErrNotFound) {
		t.Fatalf("bob listed alice's members: %v", err)
	}
	name := "Hijacked"
	if _, err := svc.Update(ctx, bob, tech.ID, collections.Patch{Name: &name}); !errors.Is(err, collections.ErrNotFound) {
		t.Fatalf("bob renamed alice's collection: %v", err)
	}
	if err := svc.RemoveMember(ctx, bob, tech.ID, m1.ID); !errors.Is(err, collections.ErrNotFound) {
		t.Fatalf("bob removed alice's member: %v", err)
	}
	if err := svc.Delete(ctx, bob, tech.ID); !errors.Is(err, collections.ErrNotFound) {
		t.Fatalf("bob deleted alice's collection: %v", err)
	}
	bobs, _ := svc.List(ctx, bob, collections.Filter{})
	if _, err := svc.Transfer(ctx, bob, bobs[0].ID, m1.ID, bobs[0].ID+0, false, nil); err == nil {
		t.Fatal("bob transferred alice's member")
	}
	if _, err := svc.Transfer(ctx, alice, tech.ID, m1.ID, bobs[0].ID, false, nil); !errors.Is(err, collections.ErrNotFound) {
		t.Fatalf("alice copied into bob's collection: %v", err)
	}

	// Paging.
	members, total, err := svc.Members(ctx, alice, tech.ID, collections.Page{Limit: 1, Offset: 1})
	if err != nil || total != 2 || len(members) != 1 || members[0].Instrument.Provider != "FINNHUB" {
		t.Fatalf("page = %+v total %d err %v", members, total, err)
	}

	// Model portfolio: explicit weights that never exceed 100%.
	model, err := svc.Create(ctx, alice, collections.Collection{Name: "AI model", Kind: collections.KindModel})
	if err != nil {
		t.Fatal(err)
	}
	sixty, fifty := 60.0, 50.0
	copied, err := svc.Transfer(ctx, alice, tech.ID, m1.ID, model.ID, false, &sixty)
	if err != nil || copied.Weight == nil || *copied.Weight != 60 || len(copied.Tags) != 1 {
		t.Fatalf("copy with weight = %+v, %v", copied, err)
	}
	if _, err := svc.AddMember(ctx, alice, model.ID, collections.Member{Instrument: collections.Instrument{Provider: "YAHOO", Symbol: "AMD"}, Weight: &fifty}); !errors.Is(err, collections.ErrInvalid) {
		t.Fatalf("weights above 100%% must be refused, got %v", err)
	}
	forty := 40.0
	if _, err := svc.UpdateMember(ctx, alice, model.ID, copied.ID, collections.MemberPatch{Weight: &forty}); err != nil {
		t.Fatalf("lower a weight: %v", err)
	}
	if _, err := svc.AddMember(ctx, alice, model.ID, collections.Member{Instrument: collections.Instrument{Provider: "YAHOO", Symbol: "AMD"}, Weight: &sixty}); err != nil {
		t.Fatalf("weights adding up to exactly 100%%: %v", err)
	}
	if _, err := svc.Transfer(ctx, alice, model.ID, copied.ID, med.ID, false, nil); !errors.Is(err, collections.ErrDuplicateMember) {
		t.Fatalf("copy onto an existing member: %v", err)
	}

	// Move: gone from the source, present in the target, note and tags kept.
	note := "Watch the data center segment"
	if _, err := svc.UpdateMember(ctx, alice, tech.ID, m1.ID, collections.MemberPatch{Note: &note}); err != nil {
		t.Fatal(err)
	}
	bio, _ := svc.Create(ctx, alice, collections.Collection{Name: "Biotech"})
	moved, err := svc.Transfer(ctx, alice, tech.ID, m1.ID, bio.ID, true, nil)
	if err != nil || moved.Note != note || moved.Weight != nil {
		t.Fatalf("move = %+v, %v", moved, err)
	}
	if _, total, _ := svc.Members(ctx, alice, tech.ID, collections.Page{}); total != 1 {
		t.Fatalf("source still holds %d members after the move", total)
	}

	// Archive, filter and sort.
	archived := true
	if _, err := svc.Update(ctx, alice, bio.ID, collections.Patch{Archived: &archived}); err != nil {
		t.Fatal(err)
	}
	active, _ := svc.List(ctx, alice, collections.Filter{})
	if len(active) != 3 {
		t.Fatalf("active = %d", len(active))
	}
	only, _ := svc.List(ctx, alice, collections.Filter{Archive: collections.ArchiveOnly})
	if len(only) != 1 || only[0].Name != "Biotech" || only[0].MemberCount != 1 {
		t.Fatalf("archived = %+v", only)
	}
	byTag, _ := svc.List(ctx, alice, collections.Filter{Tag: "chips"})
	if len(byTag) != 1 || byTag[0].ID != tech.ID {
		t.Fatalf("by tag = %+v", byTag)
	}
	if got, _ := svc.List(ctx, alice, collections.Filter{Query: "theme"}); len(got) != 1 {
		t.Fatalf("query on category = %+v", got)
	}
	if got, _ := svc.List(ctx, alice, collections.Filter{Kind: collections.KindModel}); len(got) != 1 || got[0].MemberCount != 2 {
		t.Fatalf("by kind = %+v", got)
	}

	// Delete removes the collection only; the instrument stays elsewhere.
	if err := svc.Delete(ctx, alice, med.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, alice, med.ID); !errors.Is(err, collections.ErrNotFound) {
		t.Fatalf("deleted collection still readable: %v", err)
	}
	if in, _ := svc.Containing(ctx, alice, collections.Instrument{Provider: "YAHOO", Symbol: "NVDA"}); len(in) != 2 {
		t.Fatalf("NVDA should remain in the model and Biotech: %+v", in)
	}

	// Export and import round trip without touching the originals.
	doc, err := svc.Export(ctx, alice, []int64{model.ID}, tech.CreatedAt)
	if err != nil || len(doc.Collections) != 1 || len(doc.Collections[0].Members) != 2 {
		t.Fatalf("export = %+v, %v", doc, err)
	}
	res, err := svc.Import(ctx, alice, doc, collections.ConflictRename)
	if err != nil || len(res) != 1 || res[0].Status != "renamed" || res[0].SavedAs != "AI model (2)" || res[0].Members != 2 {
		t.Fatalf("import = %+v, %v", res, err)
	}
	if res, _ := svc.Import(ctx, alice, doc, collections.ConflictSkip); res[0].Status != "skipped" {
		t.Fatalf("skip = %+v", res)
	}
	if res, err := svc.Import(ctx, bob, doc, collections.ConflictRename); err != nil || res[0].Status != "created" {
		t.Fatalf("bob imports a file alice shared: %+v, %v", res, err)
	}
}
