package server

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"floweysales/internal/ai"
	"floweysales/internal/collections"
	"floweysales/internal/identity"
	"floweysales/internal/instruments"
)

// These research tools read the collections of the user the run acts for.

var _ ai.CollectionSource = engineSource{}

// errNoCollection is wrapped by resolveCollection when nothing matches.
var errNoCollection = errors.New("no collection")

const collectionToolNotice = "The user's own collection. A model portfolio represents hypothetical allocations."

func (s engineSource) collectionCaller(ctx context.Context) (identity.User, collections.Service, error) {
	u, ok := s.e.agentUser(ctx)
	if !ok {
		return u, collections.Service{}, errNoAgentUser
	}
	svc, ok := s.e.collectionService()
	if !ok {
		return u, svc, errors.New("collections need the database")
	}
	return u, svc, nil
}

// resolveCollection finds one of the user's collections by id or by name
// (ignoring case). Another user's collection is not found, like a missing one;
// the error wraps errNoCollection, so callers can say whose.
func resolveCollection(ctx context.Context, svc collections.Service, userID int64, ref string) (collections.Collection, error) {
	ref = strings.TrimSpace(ref)
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil && id > 0 {
		c, err := svc.Get(ctx, userID, id)
		if errors.Is(err, collections.ErrNotFound) {
			return c, fmt.Errorf("%w with id %d", errNoCollection, id)
		}
		return c, err
	}
	all, err := svc.List(ctx, userID, collections.Filter{Archive: collections.ArchiveInclude})
	if err != nil {
		return collections.Collection{}, err
	}
	for _, c := range all {
		if strings.EqualFold(c.Name, ref) {
			return c, nil
		}
	}
	return collections.Collection{}, fmt.Errorf("%w named %q", errNoCollection, ref)
}

func (s engineSource) Collections(ctx context.Context, includeArchived bool) (any, error) {
	u, svc, err := s.collectionCaller(ctx)
	if err != nil {
		return nil, err
	}
	f := collections.Filter{}
	if includeArchived {
		f.Archive = collections.ArchiveInclude
	}
	list, err := svc.List(ctx, u.ID, f)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(list))
	for _, c := range list {
		row := map[string]any{"id": c.ID, "name": c.Name, "kind": c.Kind, "instruments": c.MemberCount,
			"updated_at": c.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z")}
		if c.Category != "" {
			row["category"] = c.Category
		}
		if len(c.Tags) > 0 {
			row["tags"] = c.Tags
		}
		if c.Description != "" {
			row["description"] = c.Description
		}
		if c.Archived {
			row["archived"] = true
		}
		rows = append(rows, row)
	}
	return map[string]any{"collections": rows, "notice": collectionToolNotice}, nil
}

// collectionToolMembers reads one collection with its members, for the caller only.
func (s engineSource) collectionToolMembers(ctx context.Context, ref string) (int64, collections.Collection, []collections.Member, instruments.Store, error) {
	u, svc, err := s.collectionCaller(ctx)
	if err != nil {
		return 0, collections.Collection{}, nil, nil, err
	}
	store, ok := s.e.instrumentStoreFor()
	if !ok {
		return 0, collections.Collection{}, nil, nil, errors.New("instrument profiles need the database")
	}
	c, err := resolveCollection(ctx, svc, u.ID, ref)
	if err != nil {
		if errors.Is(err, errNoCollection) {
			err = fmt.Errorf("you have %w; list_collections shows yours", err)
		}
		return 0, c, nil, nil, err
	}
	members, err := svc.Store.AllMembers(ctx, u.ID, c.ID)
	if err != nil {
		return 0, c, nil, nil, err
	}
	return u.ID, c, members, store, nil
}

func (s engineSource) Collection(ctx context.Context, ref string, limit int) (any, error) {
	userID, c, members, store, err := s.collectionToolMembers(ctx, ref)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 200 {
		limit = 100
	}
	intel, rows, err := s.e.collectionIntelligence(ctx, userID, c, members, store)
	if err != nil {
		return nil, err
	}
	byID := map[int64]collections.Member{}
	for _, m := range members {
		byID[m.ID] = m
	}
	out := make([]map[string]any, 0, min(limit, len(rows)))
	for _, mi := range rows {
		if len(out) >= limit {
			break
		}
		m := byID[mi.MemberID]
		row := map[string]any{"member_id": mi.MemberID, "provider": mi.Key.Provider, "symbol": mi.Key.Symbol,
			"profile_status": mi.ProfileStatus, "classification": mi.Classification}
		if mi.Name != "" {
			row["name"] = mi.Name
		}
		if mi.Weight != nil {
			row["weight_pct"] = *mi.Weight
		}
		if m.Note != "" {
			row["note"] = m.Note
		}
		if len(m.Tags) > 0 {
			row["tags"] = m.Tags
		}
		if mi.Stale {
			row["profile_stale"] = true
		}
		out = append(out, row)
	}
	result := map[string]any{
		"collection": map[string]any{"id": c.ID, "name": c.Name, "kind": c.Kind, "category": c.Category, "tags": c.Tags, "description": c.Description},
		"members":    out, "total_members": len(rows),
		"coverage": intel["coverage"], "composition": intel["composition"], "links": intel["links"],
		"notice": collectionToolNotice,
	}
	if len(rows) > len(out) {
		result["truncated"] = fmt.Sprintf("%d of %d instruments shown; raise limit (at most 200) for more", len(out), len(rows))
	}
	return result, nil
}

func (s engineSource) CollectionNews(ctx context.Context, ref string, limit int) (any, error) {
	userID, c, members, store, err := s.collectionToolMembers(ctx, ref)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > collectionNewsMax {
		limit = 20
	}
	out, err := s.e.collectionNews(ctx, userID, c, members, store, limit)
	if err != nil {
		return nil, err
	}
	out["collection"] = map[string]any{"id": c.ID, "name": c.Name}
	out["notice"] = "Headlines as each instrument's own provider listed them. Relevance is a text match with its reason, not a judgment of the story; cite the headline's URL."
	return out, nil
}
