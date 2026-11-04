package server

import "strings"

// A node's output is held in several places in a checkpoint: Previous,
// NodeResults, Results, V2.Values and V2.Steps. Saved as is, one 1 MiB output
// takes about five times that and the 8 MiB checkpoint cap is reached after
// a few nodes. The saved form keeps each large output once, in NodeResults,
// and refers to it by node id everywhere else.

const (
	checkpointRefPrefix = "\x00ref:"
	// Below this a reference saves nothing worth the indirection.
	checkpointRefMin = 1 << 10
)

// compactCheckpoint returns a copy that stores each large node output once.
// The copy has Compact set, which restoreFlowCheckpoint expands. The live
// state is not changed. If a value already looks like a reference it returns
// the state unchanged rather than risk a wrong expansion.
func compactCheckpoint(s flowCheckpointState) flowCheckpointState {
	byValue := map[string]string{}
	for id, v := range s.NodeResults {
		if len(v) < checkpointRefMin {
			continue
		}
		if first, ok := byValue[v]; !ok || id < first {
			byValue[v] = id
		}
	}
	if len(byValue) == 0 {
		return s
	}
	ref := func(v string) string {
		if id, ok := byValue[v]; ok {
			return checkpointRefPrefix + id
		}
		return v
	}
	for _, v := range s.NodeResults {
		if strings.HasPrefix(v, checkpointRefPrefix) {
			return s
		}
	}
	out := s
	out.Compact = true
	out.Previous = ref(s.Previous)
	out.Results = make([]map[string]string, len(s.Results))
	for i, step := range s.Results {
		cp := make(map[string]string, len(step))
		for k, v := range step {
			if k == "output" {
				v = ref(v)
			}
			cp[k] = v
		}
		out.Results[i] = cp
	}
	if s.V2 != nil {
		v2 := *s.V2
		v2.Values = make(map[string]string, len(s.V2.Values))
		for k, v := range s.V2.Values {
			v2.Values[k] = ref(v)
		}
		v2.Steps = make([]flowV2Step, len(s.V2.Steps))
		for i, step := range s.V2.Steps {
			step.Output = ref(step.Output)
			v2.Steps[i] = step
		}
		out.V2 = &v2
	}
	return out
}

// expandCheckpoint undoes compactCheckpoint. A reference to an output that is
// not in NodeResults is an error: the checkpoint is corrupt.
func expandCheckpoint(s *flowCheckpointState) bool {
	if !s.Compact {
		return true
	}
	ok := true
	deref := func(v string) string {
		id, isRef := strings.CutPrefix(v, checkpointRefPrefix)
		if !isRef {
			return v
		}
		full, found := s.NodeResults[id]
		if !found {
			ok = false
		}
		return full
	}
	s.Previous = deref(s.Previous)
	for _, step := range s.Results {
		if v, has := step["output"]; has {
			step["output"] = deref(v)
		}
	}
	if s.V2 != nil {
		for k, v := range s.V2.Values {
			s.V2.Values[k] = deref(v)
		}
		for i := range s.V2.Steps {
			s.V2.Steps[i].Output = deref(s.V2.Steps[i].Output)
		}
	}
	s.Compact = false
	return ok
}
