package storage

import (
	"context"
	"database/sql"
	"errors"
)

// FlowNodeStateMaxBytes bounds the state one node keeps between runs.
const FlowNodeStateMaxBytes = 256 * 1024

// LoadFlowNodeState returns the state a node saved in an earlier run, or ""
// when it has none.
func (d *Database) LoadFlowNodeState(ctx context.Context, flowID int64, nodeID string) (string, error) {
	var state string
	err := d.db.QueryRowContext(ctx, "SELECT state FROM agent_flow_node_state WHERE flow_id=? AND node_id=?", flowID, nodeID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return state, err
}

// SaveFlowNodeState replaces a node's saved state.
func (d *Database) SaveFlowNodeState(ctx context.Context, flowID int64, nodeID, state string) error {
	if len(state) > FlowNodeStateMaxBytes {
		return errors.New("flow node state is too large")
	}
	_, err := d.db.ExecContext(ctx,
		"INSERT INTO agent_flow_node_state(flow_id,node_id,state) VALUES(?,?,?) ON DUPLICATE KEY UPDATE state=VALUES(state),updated_at=CURRENT_TIMESTAMP(6)",
		flowID, nodeID, state)
	return err
}

// ResetFlowNodeState forgets what the nodes of a flow remembered.
func (d *Database) ResetFlowNodeState(ctx context.Context, flowID int64) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM agent_flow_node_state WHERE flow_id=?", flowID)
	return err
}

// FlowOwnerEmail is the address of the account that owns a flow.
func (d *Database) FlowOwnerEmail(ctx context.Context, flowID int64) (string, error) {
	var email string
	err := d.db.QueryRowContext(ctx, "SELECT u.email FROM agent_flows f JOIN auth_users u ON u.id=f.created_by WHERE f.id=? AND u.active=TRUE", flowID).Scan(&email)
	return email, err
}
