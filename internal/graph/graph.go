// Package graph is what every saved graph of the app shares (issue #262): what an edge may join,
// the order the nodes run in, and what counts as a cycle. Flows, untyped functions and typed
// functions are different graphs with different node types and rules, and each keeps its own; they
// all answer these questions here, so the answer cannot differ between them.
//
// The package knows nothing about node types, ports or settings, and it does not change what it
// is given.
package graph

// Edge joins two nodes by id.
type Edge struct{ From, To string }

// Joins reports whether an edge joins two different nodes that exist. known says whether an id is
// a node. A node wired to itself, or to a node that is not there, is not an edge of any graph.
func Joins(known func(id string) bool, e Edge) bool {
	return e.From != e.To && known(e.From) && known(e.To)
}

// Sorted is the result of Sort.
type Sorted struct {
	// InDegree counts the edges that lead to each node. An edge saved twice counts twice.
	InDegree map[string]int
	// Roots lists the nodes nothing leads to, in node order.
	Roots []string
	// Order is an order the nodes can run in: every node after all nodes that lead to it. It starts
	// with the roots in node order, and a node follows when its last input has run, so nodes
	// released by one node come in the order their edges were saved. The result is the same for the
	// same input. Nodes in a cycle, or behind one, never run and are missing from it.
	Order []string
}

// Complete reports whether every one of n nodes has a place in the order, which is to say that the
// graph has no cycle.
func (s Sorted) Complete(n int) bool { return len(s.Order) == n }

// Sort orders the nodes ids (in the order they were saved) by the edges (in the order they were
// saved). An edge that does not join two of the ids is ignored; use Joins to refuse such an edge.
func Sort(ids []string, edges []Edge) Sorted {
	known := make(map[string]bool, len(ids))
	for _, id := range ids {
		known[id] = true
	}
	inDegree := make(map[string]int, len(ids))
	next := make(map[string][]string, len(ids))
	for _, e := range edges {
		if !known[e.From] || !known[e.To] {
			continue
		}
		inDegree[e.To]++
		next[e.From] = append(next[e.From], e.To)
	}
	// Counted per node, not per edge, so a name that is in ids twice counts as one.
	remaining := make(map[string]int, len(inDegree))
	for id, n := range inDegree {
		remaining[id] = n
	}
	var roots, queue []string
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if inDegree[id] == 0 {
			roots = append(roots, id)
			queue = append(queue, id)
		}
	}
	order := make([]string, 0, len(ids))
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		order = append(order, id)
		for _, to := range next[id] {
			if remaining[to]--; remaining[to] == 0 {
				queue = append(queue, to)
			}
		}
	}
	return Sorted{InDegree: inDegree, Roots: roots, Order: order}
}
