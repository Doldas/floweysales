package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Bounds on one user's saved agent tasks.
const (
	AgentTaskMaxBytes = 4 << 20
	AgentTaskMaxCount = 100
)

var (
	ErrAgentTaskNotFound = errors.New("task not found")
	ErrAgentTaskLimit    = errors.New("too many saved tasks; delete one first")
)

// AgentTask is a research-agent task. Deliverables and Checkpoint are JSON
// owned by internal/ai; storage does not look inside them.
type AgentTask struct {
	ID           int64     `json:"id"`
	Title        string    `json:"title"`
	Status       string    `json:"status"`
	Deliverables string    `json:"-"`
	Checkpoint   string    `json:"-"`
	Steps        int       `json:"steps"`
	ToolCalls    int       `json:"tool_calls"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// CreateAgentTask starts a task and returns its id.
func (d *Database) CreateAgentTask(ctx context.Context, userID int64, title, deliverablesJSON string) (int64, error) {
	var count int
	if err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM agent_tasks WHERE user_id=?", userID).Scan(&count); err != nil {
		return 0, err
	}
	if count >= AgentTaskMaxCount {
		return 0, ErrAgentTaskLimit
	}
	res, err := d.db.ExecContext(ctx, "INSERT INTO agent_tasks(user_id,title,status,deliverables,checkpoint) VALUES(?,?,?,?,?)",
		userID, title, "running", deliverablesJSON, "{}")
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SaveAgentTaskCheckpoint stores the latest resumable state of a task.
func (d *Database) SaveAgentTaskCheckpoint(ctx context.Context, userID, id int64, status, deliverablesJSON, checkpointJSON string, steps, toolCalls int) error {
	if len(checkpointJSON) > AgentTaskMaxBytes {
		return errors.New("task checkpoint is too large")
	}
	res, err := d.db.ExecContext(ctx, "UPDATE agent_tasks SET status=?,deliverables=?,checkpoint=?,steps=?,tool_calls=?,updated_at=CURRENT_TIMESTAMP(6) WHERE user_id=? AND id=?",
		status, deliverablesJSON, checkpointJSON, steps, toolCalls, userID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_, err = d.GetAgentTask(ctx, userID, id)
	}
	return err
}

// SetAgentTaskStatus changes only the status, for example to "cancelled".
func (d *Database) SetAgentTaskStatus(ctx context.Context, userID, id int64, status string) error {
	_, err := d.db.ExecContext(ctx, "UPDATE agent_tasks SET status=?,updated_at=CURRENT_TIMESTAMP(6) WHERE user_id=? AND id=?", status, userID, id)
	return err
}

// GetAgentTask reads one of a user's tasks, checkpoint included.
func (d *Database) GetAgentTask(ctx context.Context, userID, id int64) (AgentTask, error) {
	var t AgentTask
	err := d.db.QueryRowContext(ctx, "SELECT id,title,status,deliverables,checkpoint,steps,tool_calls,created_at,updated_at FROM agent_tasks WHERE user_id=? AND id=?", userID, id).
		Scan(&t.ID, &t.Title, &t.Status, &t.Deliverables, &t.Checkpoint, &t.Steps, &t.ToolCalls, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrAgentTaskNotFound
	}
	return t, err
}

// ListAgentTasks returns a user's tasks, newest first, without checkpoints.
func (d *Database) ListAgentTasks(ctx context.Context, userID int64) ([]AgentTask, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT id,title,status,deliverables,steps,tool_calls,created_at,updated_at FROM agent_tasks WHERE user_id=? ORDER BY updated_at DESC, id DESC", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentTask{}
	for rows.Next() {
		var t AgentTask
		if err := rows.Scan(&t.ID, &t.Title, &t.Status, &t.Deliverables, &t.Steps, &t.ToolCalls, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteAgentTask removes one of a user's tasks.
func (d *Database) DeleteAgentTask(ctx context.Context, userID, id int64) error {
	res, err := d.db.ExecContext(ctx, "DELETE FROM agent_tasks WHERE user_id=? AND id=?", userID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAgentTaskNotFound
	}
	return nil
}
