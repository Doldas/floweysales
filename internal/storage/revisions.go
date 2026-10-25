package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Revision is an immutable published snapshot of a flow or function draft.
type Revision struct {
	Revision    int             `json:"revision"`
	Name        string          `json:"name"`
	Graph       json.RawMessage `json:"graph,omitempty"`
	Note        string          `json:"note,omitempty"`
	PublishedBy int64           `json:"published_by"`
	PublishedAt string          `json:"published_at"`
	Active      bool            `json:"active"`
}

var (
	// ErrNoRevisionChanges means publishing would duplicate the latest revision.
	ErrNoRevisionChanges = errors.New("draft is identical to the latest published revision")
	// ErrDraftChanged means the draft was edited after it was validated for publishing.
	ErrDraftChanged = errors.New("draft changed while publishing; reload and try again")
)

// MaxRevisionNote bounds the optional publish note.
const MaxRevisionNote = 500

// revisionStore names the draft table and its revision table. Values are
// compile-time constants, never user input.
type revisionStore struct {
	parent, table, fk string
}

var (
	flowRevisions     = revisionStore{parent: "agent_flows", table: "agent_flow_revisions", fk: "flow_id"}
	functionRevisions = revisionStore{parent: "blueprint_functions", table: "blueprint_function_revisions", fk: "function_id"}
)

// publish snapshots the draft as the next revision and activates it. The
// caller passes the name and graph it validated; if the stored draft differs
// the publish is refused instead of freezing unvalidated content.
func (d *Database) publish(ctx context.Context, s revisionStore, id int64, name, graph, note string, admin int64) (int, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var draftName, draftGraph string
	err = tx.QueryRowContext(ctx, "SELECT name,graph_json FROM "+s.parent+" WHERE id=? FOR UPDATE", id).Scan(&draftName, &draftGraph)
	if err != nil {
		return 0, err
	}
	if draftName != name || draftGraph != graph {
		return 0, ErrDraftChanged
	}
	var latest int
	var latestName, latestGraph string
	err = tx.QueryRowContext(ctx, "SELECT revision,name,graph_json FROM "+s.table+" WHERE "+s.fk+"=? ORDER BY revision DESC LIMIT 1", id).
		Scan(&latest, &latestName, &latestGraph)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		latest = 0
	case err != nil:
		return 0, err
	case latestName == draftName && latestGraph == draftGraph:
		return 0, ErrNoRevisionChanges
	}
	next := latest + 1
	var storedNote any
	if note != "" {
		storedNote = note
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO "+s.table+"("+s.fk+",revision,name,graph_json,note,published_by) VALUES(?,?,?,?,?,?)",
		id, next, draftName, draftGraph, storedNote, admin); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE "+s.parent+" SET active_revision=? WHERE id=?", next, id); err != nil {
		return 0, err
	}
	return next, tx.Commit()
}

func (d *Database) listRevisions(ctx context.Context, s revisionStore, id int64) ([]Revision, error) {
	var exists int
	if err := d.db.QueryRowContext(ctx, "SELECT 1 FROM "+s.parent+" WHERE id=?", id).Scan(&exists); err != nil {
		return nil, err
	}
	rows, err := d.db.QueryContext(ctx, `SELECT r.revision,r.name,COALESCE(r.note,''),r.published_by,
		DATE_FORMAT(r.published_at,'%Y-%m-%dT%H:%i:%s.%fZ'),COALESCE(p.active_revision=r.revision,FALSE)
		FROM `+s.table+` r JOIN `+s.parent+` p ON p.id=r.`+s.fk+`
		WHERE r.`+s.fk+`=? ORDER BY r.revision DESC LIMIT 100`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Revision{}
	for rows.Next() {
		var r Revision
		if err := rows.Scan(&r.Revision, &r.Name, &r.Note, &r.PublishedBy, &r.PublishedAt, &r.Active); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *Database) getRevision(ctx context.Context, s revisionStore, id int64, revision int) (Revision, error) {
	var r Revision
	var graph string
	err := d.db.QueryRowContext(ctx, `SELECT r.revision,r.name,r.graph_json,COALESCE(r.note,''),r.published_by,
		DATE_FORMAT(r.published_at,'%Y-%m-%dT%H:%i:%s.%fZ'),COALESCE(p.active_revision=r.revision,FALSE)
		FROM `+s.table+` r JOIN `+s.parent+` p ON p.id=r.`+s.fk+`
		WHERE r.`+s.fk+`=? AND r.revision=?`, id, revision).
		Scan(&r.Revision, &r.Name, &graph, &r.Note, &r.PublishedBy, &r.PublishedAt, &r.Active)
	r.Graph = json.RawMessage(graph)
	return r, err
}

// activate points runs and unpinned calls at an existing revision; this is
// how a rollback is performed. It returns sql.ErrNoRows for unknown revisions.
func (d *Database) activate(ctx context.Context, s revisionStore, id int64, revision int) error {
	if _, err := d.getRevision(ctx, s, id, revision); err != nil {
		return err
	}
	_, err := d.db.ExecContext(ctx, "UPDATE "+s.parent+" SET active_revision=? WHERE id=?", revision, id)
	return err
}

// restore copies a revision's name and graph into the draft. It does not
// change which revision is active.
func (d *Database) restore(ctx context.Context, s revisionStore, id int64, revision int) (Revision, error) {
	r, err := d.getRevision(ctx, s, id, revision)
	if err != nil {
		return r, err
	}
	_, err = d.db.ExecContext(ctx, "UPDATE "+s.parent+" SET name=?,graph_json=? WHERE id=?", r.Name, string(r.Graph), id)
	return r, err
}

// resolve returns the name and graph a call or run should execute: a pinned
// revision when pinned > 0, otherwise the active revision, otherwise the
// draft of a never-published definition. revision is nil for the draft.
func (d *Database) resolve(ctx context.Context, s revisionStore, id int64, pinned int) (name, graph string, revision *int, err error) {
	if pinned > 0 {
		r, err := d.getRevision(ctx, s, id, pinned)
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", nil, fmt.Errorf("revision %d does not exist: %w", pinned, sql.ErrNoRows)
		}
		if err != nil {
			return "", "", nil, err
		}
		return r.Name, string(r.Graph), &r.Revision, nil
	}
	var active sql.NullInt64
	var revName, revGraph sql.NullString
	err = d.db.QueryRowContext(ctx, `SELECT p.name,p.graph_json,p.active_revision,r.name,r.graph_json FROM `+s.parent+` p
		LEFT JOIN `+s.table+` r ON r.`+s.fk+`=p.id AND r.revision=p.active_revision WHERE p.id=?`, id).
		Scan(&name, &graph, &active, &revName, &revGraph)
	if err != nil {
		return "", "", nil, err
	}
	if !active.Valid {
		return name, graph, nil, nil
	}
	if !revGraph.Valid {
		return "", "", nil, fmt.Errorf("active revision %d is missing", active.Int64)
	}
	rev := int(active.Int64)
	return revName.String, revGraph.String, &rev, nil
}

// revisionColumns adds publish state to a draft listing whose table alias is
// p and whose active revision is joined as r.
func revisionColumns(s revisionStore) string {
	return `,p.active_revision,(SELECT COALESCE(MAX(x.revision),0) FROM ` + s.table + ` x WHERE x.` + s.fk + `=p.id),
		(p.active_revision IS NOT NULL AND (r.graph_json IS NULL OR CAST(r.graph_json AS BINARY)<>CAST(p.graph_json AS BINARY) OR CAST(r.name AS BINARY)<>CAST(p.name AS BINARY)))`
}

func revisionJoin(s revisionStore) string {
	return ` p LEFT JOIN ` + s.table + ` r ON r.` + s.fk + `=p.id AND r.revision=p.active_revision`
}

// PublishState is reported with every flow and function draft.
type PublishState struct {
	ActiveRevision        *int `json:"active_revision"`
	LatestRevision        int  `json:"latest_revision"`
	HasUnpublishedChanges bool `json:"has_unpublished_changes"`
}

func (p *PublishState) scanTargets() []any {
	return []any{&p.ActiveRevision, &p.LatestRevision, &p.HasUnpublishedChanges}
}

func (d *Database) PublishAgentFlow(ctx context.Context, id int64, name, graph, note string, admin int64) (int, error) {
	return d.publish(ctx, flowRevisions, id, name, graph, note, admin)
}
func (d *Database) ListAgentFlowRevisions(ctx context.Context, id int64) ([]Revision, error) {
	return d.listRevisions(ctx, flowRevisions, id)
}
func (d *Database) GetAgentFlowRevision(ctx context.Context, id int64, revision int) (Revision, error) {
	return d.getRevision(ctx, flowRevisions, id, revision)
}
func (d *Database) ActivateAgentFlowRevision(ctx context.Context, id int64, revision int) error {
	return d.activate(ctx, flowRevisions, id, revision)
}
func (d *Database) RestoreAgentFlowRevision(ctx context.Context, id int64, revision int) (Revision, error) {
	return d.restore(ctx, flowRevisions, id, revision)
}

// ResolveAgentFlow returns the flow with the graph that should execute; see resolve.
func (d *Database) ResolveAgentFlow(ctx context.Context, id int64, pinned int) (AgentFlow, *int, error) {
	f, err := d.GetAgentFlow(ctx, id)
	if err != nil {
		return f, nil, err
	}
	name, graph, revision, err := d.resolve(ctx, flowRevisions, id, pinned)
	if err != nil {
		return f, nil, err
	}
	f.Name, f.Graph = name, json.RawMessage(graph)
	return f, revision, nil
}

func (d *Database) PublishBlueprintFunction(ctx context.Context, id int64, name, graph, note string, admin int64) (int, error) {
	return d.publish(ctx, functionRevisions, id, name, graph, note, admin)
}
func (d *Database) ListBlueprintFunctionRevisions(ctx context.Context, id int64) ([]Revision, error) {
	return d.listRevisions(ctx, functionRevisions, id)
}
func (d *Database) GetBlueprintFunctionRevision(ctx context.Context, id int64, revision int) (Revision, error) {
	return d.getRevision(ctx, functionRevisions, id, revision)
}
func (d *Database) ActivateBlueprintFunctionRevision(ctx context.Context, id int64, revision int) error {
	return d.activate(ctx, functionRevisions, id, revision)
}
func (d *Database) RestoreBlueprintFunctionRevision(ctx context.Context, id int64, revision int) (Revision, error) {
	return d.restore(ctx, functionRevisions, id, revision)
}

// ResolveBlueprintFunction returns the function definition a call should evaluate; see resolve.
func (d *Database) ResolveBlueprintFunction(ctx context.Context, id int64, pinned int) (BlueprintFunction, *int, error) {
	name, graph, revision, err := d.resolve(ctx, functionRevisions, id, pinned)
	if err != nil {
		return BlueprintFunction{}, nil, err
	}
	return BlueprintFunction{ID: id, Name: name, Graph: json.RawMessage(graph)}, revision, nil
}
