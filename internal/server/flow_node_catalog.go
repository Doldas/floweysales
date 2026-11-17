package server

// flowNodeMinVersion is the lowest graph version each of the node types a
// flow can hold is valid in. The investor data nodes (investorNodeTypes) are
// valid in every version. The editor lists the same types: the contract test
// in flow_node_catalog_test.go keeps the two together.
var flowNodeMinVersion = map[string]int{
	"trigger": 0, "agent": 0, "flow": 0, "constant": 0, "function": 0,
	"save_result": 0, "load_result": 0, "transform": 0,
	"workspace_read": 0, "workspace_write": 0, "output": 0, "http_get": 0,
	"loop": 2, "web_search": 2, "web_crawl": 2,
	"condition": 2, "merge": 2, "json_get": 2, "emit_event": 2,
}

// flowNodeAllowed reports whether a graph of this version may hold a node of
// this type.
func flowNodeAllowed(nodeType string, graphVersion int) bool {
	if investorNodeTypes[nodeType] {
		return true
	}
	min, ok := flowNodeMinVersion[nodeType]
	return ok && graphVersion >= min
}
