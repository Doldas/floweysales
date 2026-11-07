package server

import (
	"context"
	"strings"

	"floweysales/internal/websearch"
)

// trimSearchField makes provider text single-line, control-character free and bounded.
func trimSearchField(v string, max int) string {
	v = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == '\t' {
			return ' '
		}
		if r < 32 {
			return -1
		}
		return r
	}, v))
	if len(v) > max {
		v = strings.ToValidUTF8(v[:max], "")
	}
	return v
}

// acceptSearchURL lets a search result's address through only when it is a
// public https page. Nothing is fetched here, and no credential is ever sent
// to a result URL.
func acceptSearchURL(raw string) (string, bool) {
	target, err := flowPublicURL(raw)
	if err != nil || target.Scheme != "https" {
		return "", false
	}
	return target.String(), true
}

// runWebSearch is the one web search path of the research agent, agent teams,
// the world view and the Flow Studio web search node: they all search by the
// provider policy an administrator saved (order, budgets, fallback).
func (e *Engine) runWebSearch(ctx context.Context, query string) (string, error) {
	outcome, err := e.webSearchRouter().Search(ctx, query)
	if err != nil {
		return "", err
	}
	return websearch.Render(outcome, acceptSearchURL)
}
