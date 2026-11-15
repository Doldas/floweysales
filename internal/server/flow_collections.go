package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"floweysales/internal/collections"
	"floweysales/internal/instruments"
	"floweysales/internal/market"
	"floweysales/internal/storage"
)

// Flow Studio nodes for Market Collections (#441 phase 5). Like the wealth
// plan node they read the flow owner's data, here one of the owner's
// collections, and only read: no node changes a collection.

const (
	// flowCollectionDefaultLimit is what one data node accepts.
	flowCollectionDefaultLimit = flowMaxSymbols
	flowCollectionNewsDefault  = 20
)

// flowCollection resolves the node's collection among the flow owner's.
func (e *Engine) flowCollection(ctx context.Context, f storage.AgentFlow, n flowNode) (int64, collections.Collection, []collections.Member, instruments.Store, error) {
	owner, err := e.flowOwnerID(ctx, f.ID)
	if err != nil {
		return 0, collections.Collection{}, nil, nil, fmt.Errorf("find the flow owner: %w", err)
	}
	svc, ok := e.collectionService()
	if !ok {
		return 0, collections.Collection{}, nil, nil, errors.New("collections need the database")
	}
	store, ok := e.instrumentStoreFor()
	if !ok {
		return 0, collections.Collection{}, nil, nil, errors.New("instrument profiles need the database")
	}
	c, err := resolveCollection(ctx, svc, owner, n.Collection)
	if err != nil {
		if errors.Is(err, errNoCollection) {
			err = fmt.Errorf("the flow owner has %w", err)
		}
		return 0, c, nil, nil, err
	}
	members, err := svc.Store.AllMembers(ctx, owner, c.ID)
	if err != nil {
		return 0, c, nil, nil, err
	}
	return owner, c, members, store, nil
}

// runCollectionNode passes on the collection's instruments as rows that the
// data nodes read: each row names its provider, so a quotes or news node
// asks the instrument's own provider. Classes come from stored profiles and
// the owner's classification; no provider is contacted.
func (e *Engine) runCollectionNode(ctx context.Context, f storage.AgentFlow, n flowNode) (output, note string, err error) {
	owner, c, members, store, err := e.flowCollection(ctx, f, n)
	if err != nil {
		return "", "", err
	}
	if len(members) == 0 {
		return "[]", fmt.Sprintf("%s has no instruments.", c.Name), nil
	}
	_, intel, err := e.collectionIntelligence(ctx, owner, c, members, store)
	if err != nil {
		return "", "", err
	}
	byID := map[int64]collections.Member{}
	for _, m := range members {
		byID[m.ID] = m
	}
	limit := n.Limit
	if limit == 0 {
		limit = flowCollectionDefaultLimit
	}
	rows := []map[string]any{}
	var unreadable []string
	readable := 0
	for _, mi := range intel {
		symbol, err := market.CanonicalSymbol(mi.Key.Symbol)
		if err != nil {
			unreadable = append(unreadable, mi.Key.Symbol)
			continue
		}
		if readable++; len(rows) >= limit {
			continue
		}
		rows = append(rows, collectionMemberRow(c, byID[mi.MemberID], mi, symbol))
	}
	var notes []string
	if readable > len(rows) {
		notes = append(notes, fmt.Sprintf("%d of %d instruments passed on; raise the limit (at most 100) for more, but a data node reads at most %d.", len(rows), readable, flowMaxSymbols))
	}
	if len(unreadable) > 0 {
		notes = append(notes, fmt.Sprintf("Left out %s: flow data nodes cannot read index or futures symbols.", strings.Join(firstN(unreadable, 5), ", ")))
	}
	output, err = flowRowsJSON(rows)
	return output, strings.Join(notes, " "), err
}

func collectionMemberRow(c collections.Collection, m collections.Member, mi memberIntelligence, symbol string) map[string]any {
	row := map[string]any{"symbol": symbol, "provider": mi.Key.Provider, "collection": c.Name, "profile_status": mi.ProfileStatus}
	set := func(key, value string) {
		if value != "" {
			row[key] = value
		}
	}
	in := m.Instrument
	set("name", mi.Name)
	set("type", in.Type)
	set("exchange", in.Exchange)
	set("currency", in.Currency)
	set("isin", in.ISIN)
	set("note", m.Note)
	set("tags", strings.Join(m.Tags, ", "))
	cl := mi.Classification
	set("asset_class", cl.AssetClass.Value)
	set("sector", cl.Sector.Value)
	set("industry", cl.Industry.Value)
	set("country", cl.Country.Value)
	set("region", cl.Region.Value)
	set("themes", strings.Join(cl.Themes, ", "))
	if c.Kind == collections.KindModel && mi.Weight != nil {
		row["weight_pct"] = *mi.Weight
	}
	return row
}

// runCollectionNewsNode is the collection's news as rows: each headline from
// its instrument's own provider, with its relevance and the reasons.
func (e *Engine) runCollectionNewsNode(ctx context.Context, f storage.AgentFlow, n flowNode) (output, note string, err error) {
	owner, c, members, store, err := e.flowCollection(ctx, f, n)
	if err != nil {
		return "", "", err
	}
	limit := n.Limit
	if limit == 0 {
		limit = flowCollectionNewsDefault
	}
	out, err := e.collectionNews(ctx, owner, c, members, store, limit)
	if err != nil {
		return "", "", err
	}
	items, _ := out["items"].([]collectionNewsItem)
	retrieved, _ := out["retrieved_at"].(string)
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row := map[string]any{"id": item.URL, "symbol": item.Symbol, "provider": item.Provider, "title": trimSearchField(item.Title, 200),
			"url": item.URL, "relevance": item.Relevance, "retrieved_at": retrieved, "collection": c.Name}
		if item.Source != "" {
			row["source"] = trimSearchField(item.Source, 60)
		}
		if item.Published > 0 {
			row["published"] = time.Unix(item.Published, 0).UTC().Format(time.RFC3339)
			row["date_status"] = "published"
		} else {
			row["date_status"] = "unavailable"
		}
		if item.Summary != "" {
			row["summary"] = trimSearchField(item.Summary, 280)
		}
		reasons := make([]string, 0, len(item.Matches))
		for _, m := range item.Matches {
			reasons = append(reasons, m.Reason)
		}
		row["reasons"] = strings.Join(reasons, "; ")
		rows = append(rows, row)
	}
	output, err = flowRowsJSON(rows)
	return output, collectionNewsNote(out["coverage"]), err
}

// collectionNewsNote names what the run could not cover, for the trace.
func collectionNewsNote(raw any) string {
	cov, _ := raw.(map[string]int)
	var parts []string
	if v := cov["no_source"]; v > 0 {
		parts = append(parts, fmt.Sprintf("%d on a provider without news", v))
	}
	if v := cov["failed"]; v > 0 {
		parts = append(parts, fmt.Sprintf("%d could not be reached", v))
	}
	if v := cov["skipped"]; v > 0 {
		parts = append(parts, fmt.Sprintf("%d not asked (at most %d per run)", v, collectionNewsMembers))
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("Headlines from %d of %d instruments; %s.", cov["fetched"]+cov["cached"], cov["members"], strings.Join(parts, ", "))
}

func firstN(list []string, n int) []string {
	if len(list) > n {
		return append(list[:n:n], fmt.Sprintf("%d more", len(list)-n))
	}
	return list
}
