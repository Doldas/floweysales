package server

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var routePattern = regexp.MustCompile(`HandleFunc\("([A-Z]+ /[^"]*)"`)

type routeSite struct {
	file   string // base name in this package
	region string // source text from the registration to the next one
}

// registeredRoutes reads every route the server registers from its source, so
// a route added without classification fails the test below.
func registeredRoutes(t *testing.T) map[string]routeSite {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]routeSite{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(raw)
		locs := routePattern.FindAllStringSubmatchIndex(src, -1)
		for i, loc := range locs {
			end := len(src)
			if i+1 < len(locs) {
				end = locs[i+1][0]
			}
			out[src[loc[2]:loc[3]]] = routeSite{file: f, region: src[loc[0]:end]}
		}
	}
	if len(out) < 150 {
		t.Fatalf("found only %d routes; the route scan is broken", len(out))
	}
	return out
}

func TestEveryRouteIsClassifiedForProvenance(t *testing.T) {
	var missing []string
	for route := range registeredRoutes(t) {
		r, ok := classifyRoute(route)
		if !ok {
			missing = append(missing, route)
			continue
		}
		if (r.State == provExempt || r.State == provPartial) && strings.TrimSpace(r.Note) == "" {
			t.Errorf("%s is %s without a reason", route, r.State)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("routes with no provenance classification (add each to provenanceRoutes in provenance_registry.go as covered, partial or exempt with a reason):\n  %s",
			strings.Join(missing, "\n  "))
	}
}

func TestProvenanceRegistryHasNoStaleEntries(t *testing.T) {
	routes := registeredRoutes(t)
	for route := range provenanceRoutes {
		if _, ok := routes[route]; !ok {
			t.Errorf("provenanceRoutes lists %q, which the server does not register", route)
		}
	}
}

// A route marked covered must emit provenance: its handler mentions it, or the
// files it delegates to do. Removing the field from a handler fails this.
func TestCoveredRoutesEmitProvenance(t *testing.T) {
	routes := registeredRoutes(t)
	covered := 0
	for route, entry := range provenanceRoutes {
		if entry.State != provCovered {
			continue
		}
		covered++
		site := routes[route]
		if strings.Contains(strings.ToLower(site.region), "provenance") {
			continue
		}
		if len(entry.Via) == 0 {
			t.Errorf("%s is covered but its handler does not mention provenance and names no delegate file", route)
			continue
		}
		for _, via := range entry.Via {
			raw, err := os.ReadFile(via)
			if err != nil {
				t.Errorf("%s: %v", route, err)
				continue
			}
			if !strings.Contains(strings.ToLower(string(raw)), "provenance") {
				t.Errorf("%s is covered via %s, which does not mention provenance", route, via)
			}
		}
	}
	if covered < 30 {
		t.Fatalf("only %d covered routes; the registry was emptied", covered)
	}
}

// Coverage is measured: the share of data-displaying routes (everything not
// exempt) that is fully covered must not fall. Raise the floor as partial
// routes are closed.
func TestProvenanceCoverageFloor(t *testing.T) {
	cover, partial := 0, 0
	for route := range registeredRoutes(t) {
		r, _ := classifyRoute(route)
		switch r.State {
		case provCovered:
			cover++
		case provPartial:
			partial++
		}
	}
	share := float64(cover) / float64(cover+partial)
	t.Logf("data routes: %d covered, %d partial (%.0f%% covered)", cover, partial, share*100)
	if share < 0.85 {
		t.Fatalf("provenance coverage fell to %.0f%% of data routes", share*100)
	}
}
