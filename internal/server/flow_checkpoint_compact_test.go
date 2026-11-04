package server

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"floweysales/internal/storage"
)

func bigCheckpoint() flowCheckpointState {
	big := strings.Repeat("row,", 100_000)
	return flowCheckpointState{
		NextNode: "c", Previous: big, LastNodeID: "b",
		NodeResults: map[string]string{"start": "in", "b": big},
		Variables:   map[string]string{},
		Results:     []map[string]string{{"node": "b", "output": big}, {"node": "x", "output": "small"}},
		V2: &flowV2State{
			Values: map[string]string{"b": big, "x": "small"}, Active: map[string]bool{"b": true},
			Steps: []flowV2Step{{Node: "b", Status: "SUCCESS", Output: big}},
		},
	}
}

func TestCheckpointKeepsEachLargeOutputOnce(t *testing.T) {
	state := bigCheckpoint()
	whole, _ := json.Marshal(state)
	compact, _ := json.Marshal(compactCheckpoint(state))
	if len(compact)*3 > len(whole) {
		t.Fatalf("compact form is %d bytes against %d: outputs are still repeated", len(compact), len(whole))
	}
	if state.Previous == "" || state.Results[0]["output"] == "" || state.V2.Steps[0].Output == "" {
		t.Fatal("compacting must not change the live state")
	}
}

func TestCheckpointRoundTripRestoresEveryCopy(t *testing.T) {
	state := bigCheckpoint()
	raw, _ := json.Marshal(compactCheckpoint(state))
	cp := &storage.FlowRunCheckpoint{NextIndex: 1, State: raw}
	got, err := restoreFlowCheckpoint(cp, []string{"a", "c"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Compact || !reflect.DeepEqual(got.NodeResults, state.NodeResults) || got.Previous != state.Previous ||
		!reflect.DeepEqual(got.Results, state.Results) || !reflect.DeepEqual(got.V2.Values, state.V2.Values) || !reflect.DeepEqual(got.V2.Steps, state.V2.Steps) {
		t.Fatal("restored state differs from the saved state")
	}
}

func TestCheckpointFromBeforeCompactionStillRestores(t *testing.T) {
	state := bigCheckpoint()
	raw, _ := json.Marshal(state) // the old shape: no compact flag
	got, err := restoreFlowCheckpoint(&storage.FlowRunCheckpoint{NextIndex: 1, State: raw}, []string{"a", "c"}, true)
	if err != nil || got.Previous != state.Previous {
		t.Fatalf("old checkpoint: %v", err)
	}
}

func TestCheckpointWithMissingReferenceIsRejected(t *testing.T) {
	state := compactCheckpoint(bigCheckpoint())
	delete(state.NodeResults, "b")
	raw, _ := json.Marshal(state)
	if _, err := restoreFlowCheckpoint(&storage.FlowRunCheckpoint{NextIndex: 1, State: raw}, []string{"a", "c"}, true); err == nil {
		t.Fatal("a reference with nothing behind it must not resume")
	}
}

func TestCheckpointOutputLookingLikeReferenceIsNotCompacted(t *testing.T) {
	state := bigCheckpoint()
	state.NodeResults["odd"] = checkpointRefPrefix + "b"
	if compactCheckpoint(state).Compact {
		t.Fatal("an output that looks like a reference must be saved whole")
	}
}
