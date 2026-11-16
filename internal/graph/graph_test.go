package graph

import (
	"fmt"
	"math/rand"
	"reflect"
	"slices"
	"testing"
)

func e(from, to string) Edge { return Edge{from, to} }

func TestSortOrdersNodesAfterEverythingThatLeadsToThem(t *testing.T) {
	for _, tc := range []struct {
		name     string
		ids      []string
		edges    []Edge
		order    []string
		roots    []string
		complete bool
	}{
		{"an empty graph", nil, nil, []string{}, nil, true},
		{"one node", []string{"a"}, nil, []string{"a"}, []string{"a"}, true},
		{"a line", []string{"a", "b", "c"}, []Edge{e("a", "b"), e("b", "c")}, []string{"a", "b", "c"}, []string{"a"}, true},
		{"a line saved backwards", []string{"c", "b", "a"}, []Edge{e("a", "b"), e("b", "c")}, []string{"a", "b", "c"}, []string{"a"}, true},
		{"nodes released by one node follow in the order their edges were saved", []string{"t", "a", "b"}, []Edge{e("t", "b"), e("t", "a")}, []string{"t", "b", "a"}, []string{"t"}, true},
		{"a join waits for its last input", []string{"t", "a", "b", "j"}, []Edge{e("t", "a"), e("t", "b"), e("a", "j"), e("b", "j")}, []string{"t", "a", "b", "j"}, []string{"t"}, true},
		{"a node that needs a later branch waits for it", []string{"t", "a", "b", "c"}, []Edge{e("t", "a"), e("a", "b"), e("t", "c"), e("c", "b")}, []string{"t", "a", "c", "b"}, []string{"t"}, true},
		{"several roots come first in node order", []string{"x", "t", "y"}, []Edge{e("t", "y")}, []string{"x", "t", "y"}, []string{"x", "t"}, true},
		{"a cycle leaves its nodes out", []string{"t", "a", "b"}, []Edge{e("t", "a"), e("a", "b"), e("b", "a")}, []string{"t"}, []string{"t"}, false},
		{"a cycle apart from the rest", []string{"t", "a", "b", "c"}, []Edge{e("t", "a"), e("b", "c"), e("c", "b")}, []string{"t", "a"}, []string{"t"}, false},
		{"everything behind a cycle is left out too", []string{"t", "a", "b", "z"}, []Edge{e("t", "a"), e("a", "b"), e("b", "a"), e("b", "z")}, []string{"t"}, []string{"t"}, false},
		{"a node wired to itself never runs", []string{"a"}, []Edge{e("a", "a")}, []string{}, nil, false},
		{"an edge saved twice counts twice and still runs", []string{"a", "b"}, []Edge{e("a", "b"), e("a", "b")}, []string{"a", "b"}, []string{"a"}, true},
		{"an edge to a node that is not there is ignored", []string{"a", "b"}, []Edge{e("a", "b"), e("b", "zz"), e("zz", "a")}, []string{"a", "b"}, []string{"a"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Sort(tc.ids, tc.edges)
			if !reflect.DeepEqual(s.Order, tc.order) {
				t.Errorf("order %v, want %v", s.Order, tc.order)
			}
			if !slices.Equal(s.Roots, tc.roots) {
				t.Errorf("roots %v, want %v", s.Roots, tc.roots)
			}
			if s.Complete(len(tc.ids)) != tc.complete {
				t.Errorf("complete = %v, want %v", s.Complete(len(tc.ids)), tc.complete)
			}
		})
	}
}

func TestInDegreeCountsEveryEdgeThatLeadsToANode(t *testing.T) {
	s := Sort([]string{"a", "b", "c"}, []Edge{e("a", "c"), e("b", "c"), e("a", "c"), e("c", "zz")})
	if !reflect.DeepEqual(s.InDegree, map[string]int{"c": 3}) {
		t.Fatalf("%v", s.InDegree)
	}
}

func TestSortDoesNotChangeItsInput(t *testing.T) {
	ids := []string{"t", "a", "b"}
	edges := []Edge{e("t", "b"), e("t", "a")}
	before := fmt.Sprint(ids, edges)
	Sort(ids, edges)
	if fmt.Sprint(ids, edges) != before {
		t.Fatal("the input changed")
	}
}

func TestJoinsNeedsTwoDifferentNodesThatExist(t *testing.T) {
	known := func(id string) bool { return id == "a" || id == "b" }
	for _, tc := range []struct {
		edge Edge
		want bool
	}{{e("a", "b"), true}, {e("b", "a"), true}, {e("a", "a"), false}, {e("a", "zz"), false}, {e("zz", "a"), false}, {e("zz", "yy"), false}} {
		if got := Joins(known, tc.edge); got != tc.want {
			t.Errorf("%v: %v, want %v", tc.edge, got, tc.want)
		}
	}
}

// hasCycle is an independent check by depth-first search, as the typed function validator used to do it.
func hasCycle(ids []string, edges []Edge) bool {
	adjacent := map[string][]string{}
	for _, edge := range edges {
		adjacent[edge.From] = append(adjacent[edge.From], edge.To)
	}
	color := map[string]int{}
	var visit func(string) bool
	visit = func(id string) bool {
		switch color[id] {
		case 1:
			return true
		case 2:
			return false
		}
		color[id] = 1
		for _, next := range adjacent[id] {
			if visit(next) {
				return true
			}
		}
		color[id] = 2
		return false
	}
	for _, id := range ids {
		if visit(id) {
			return true
		}
	}
	return false
}

func TestSortAgreesWithAnIndependentCycleCheckOnRandomGraphs(t *testing.T) {
	rng := rand.New(rand.NewSource(262))
	for round := 0; round < 3000; round++ {
		n := 1 + rng.Intn(9)
		ids := make([]string, n)
		for i := range ids {
			ids[i] = fmt.Sprintf("n%d", i)
		}
		var edges []Edge
		for i, count := 0, rng.Intn(2*n+1); i < count; i++ {
			from, to := rng.Intn(n), rng.Intn(n)
			if from == to && rng.Intn(4) != 0 {
				continue // mostly real edges; the odd self loop stays
			}
			edges = append(edges, e(ids[from], ids[to]))
		}
		s := Sort(ids, edges)
		if s.Complete(n) == hasCycle(ids, edges) {
			t.Fatalf("round %d: ids %v edges %v: complete=%v but cycle=%v", round, ids, edges, s.Complete(n), hasCycle(ids, edges))
		}
		position := map[string]int{}
		for i, id := range s.Order {
			if _, twice := position[id]; twice {
				t.Fatalf("round %d: %s is in the order twice: %v", round, id, s.Order)
			}
			position[id] = i
		}
		for _, edge := range edges {
			if edge.From == edge.To {
				continue
			}
			from, okFrom := position[edge.From]
			to, okTo := position[edge.To]
			if okTo && (!okFrom || from > to) {
				t.Fatalf("round %d: %v runs before what leads to it in %v", round, edge, s.Order)
			}
		}
		if again := Sort(ids, edges); !reflect.DeepEqual(again, s) {
			t.Fatalf("round %d: the same input gave another result", round)
		}
	}
}
