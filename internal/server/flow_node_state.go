package server

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

// flowNodeStateStore keeps what a node remembers between runs.
type flowNodeStateStore interface {
	LoadFlowNodeState(ctx context.Context, flowID int64, nodeID string) (string, error)
	SaveFlowNodeState(ctx context.Context, flowID int64, nodeID, state string) error
}

const (
	onlyNewMaxKeys   = 2000
	onlyNewRetention = 180 * 24 * time.Hour
)

func (e *Engine) nodeStateStore() flowNodeStateStore {
	if e.flowState != nil {
		return e.flowState
	}
	if e.db != nil {
		return e.db
	}
	return nil
}

// onlyNewState maps an item key to when it was last passed on (unix seconds).
type onlyNewState struct {
	Seen map[string]int64 `json:"seen"`
}

// onlyNewKey identifies a row: the configured fields, or else its id, url or
// symbol, or else the whole row.
func onlyNewKey(row map[string]any, fields []string) string {
	if len(fields) == 0 {
		for _, f := range []string{"id", "url", "symbol"} {
			if v, ok := rowField(row, f); ok {
				return f + "=" + textOf(v)
			}
		}
		raw, _ := json.Marshal(row)
		return string(raw)
	}
	parts := make([]string, len(fields))
	for i, f := range fields {
		v, _ := rowField(row, f)
		parts[i] = textOf(v)
	}
	key := strings.Join(parts, "|")
	if len(key) > 300 {
		key = key[:300]
	}
	return key
}

// filterOnlyNew passes on the rows not passed on before. With a cooldown a
// row may pass again once that many hours have gone by since it last did,
// which suits conditions that persist, such as "RSI below 30". The state is
// saved before the rows go on, so a run that is resumed after a crash does
// not repeat them.
func (e *Engine) filterOnlyNew(ctx context.Context, flowID int64, n flowNode, input string, now time.Time) (string, int, error) {
	rows, err := flowRows(input)
	if err != nil {
		return "", 0, err
	}
	store := e.nodeStateStore()
	if store == nil || flowID <= 0 {
		return "", 0, errors.New("only new needs a saved flow and storage")
	}
	raw, err := store.LoadFlowNodeState(ctx, flowID, n.ID)
	if err != nil {
		return "", 0, err
	}
	state := onlyNewState{Seen: map[string]int64{}}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &state); err != nil || state.Seen == nil {
			state = onlyNewState{Seen: map[string]int64{}}
		}
	}
	fields := splitFields(n.KeyFields)
	cooldown := int64(n.CooldownHours) * 3600
	nowUnix := now.Unix()
	fresh := []map[string]any{}
	inRun := map[string]bool{}
	for _, row := range rows {
		if _, failed := row["error"]; failed {
			continue
		}
		key := onlyNewKey(row, fields)
		if inRun[key] {
			continue
		}
		inRun[key] = true
		last, seen := state.Seen[key]
		if seen && (cooldown == 0 || nowUnix-last < cooldown) {
			continue
		}
		state.Seen[key] = nowUnix
		fresh = append(fresh, row)
	}
	// Forget the oldest keys beyond the retention window and the cap.
	cutoff := now.Add(-onlyNewRetention).Unix()
	for k, t := range state.Seen {
		if t < cutoff {
			delete(state.Seen, k)
		}
	}
	if len(state.Seen) > onlyNewMaxKeys {
		keys := make([]string, 0, len(state.Seen))
		for k := range state.Seen {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return state.Seen[keys[i]] < state.Seen[keys[j]] })
		for _, k := range keys[:len(keys)-onlyNewMaxKeys] {
			delete(state.Seen, k)
		}
	}
	saved, err := json.Marshal(state)
	if err != nil {
		return "", 0, err
	}
	if err := store.SaveFlowNodeState(ctx, flowID, n.ID, string(saved)); err != nil {
		return "", 0, err
	}
	out, err := flowRowsJSON(fresh)
	return out, len(rows) - len(fresh), err
}
