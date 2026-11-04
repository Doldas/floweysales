package storage

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Bounds on what an administrator can prepare and a user can save.
const (
	MaxModelProfiles = 20
	MaxSavedTeams    = 20
)

var (
	ErrProfileNotFound  = errors.New("model profile not found")
	ErrProfileNameTaken = errors.New("a model profile with this name already exists")
	ErrProfileLimit     = errors.New("too many model profiles; delete one first")
	ErrTeamNotFound     = errors.New("team not found")
	ErrTeamNameTaken    = errors.New("you already have a team with this name")
	ErrTeamLimit        = errors.New("too many saved teams; delete one first")
)

// ModelProfile is a model choice an administrator prepares. Unknown
// capabilities and costs stay nil, and the interface says "unknown".
type ModelProfile struct {
	ID                int64    `json:"id"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Model             string   `json:"model"`
	FallbackModel     string   `json:"fallback_model,omitempty"`
	SupportsTools     *bool    `json:"supports_tools"`
	ContextWindow     *int     `json:"context_window"`
	CostInputPerMTok  *float64 `json:"cost_input_per_mtok"`
	CostOutputPerMTok *float64 `json:"cost_output_per_mtok"`
	MaxSteps          *int     `json:"max_steps"`
	MaxToolCalls      *int     `json:"max_tool_calls"`
	Enabled           bool     `json:"enabled"`
	IsDefault         bool     `json:"is_default"`
	Position          int      `json:"position"`
}

// ValidAIModelName reports whether s is a well-formed model identifier.
func ValidAIModelName(s string) bool { return allowedAIModel.MatchString(s) }

const profileColumns = "id,name,description,model_name,COALESCE(fallback_model,''),supports_tools,context_window,cost_input_per_mtok,cost_output_per_mtok,max_steps,max_tool_calls,enabled,is_default,position"

func scanProfile(r rowScanner) (ModelProfile, error) {
	var p ModelProfile
	var tools sql.NullBool
	var window, steps, calls sql.NullInt64
	var costIn, costOut sql.NullFloat64
	if err := r.Scan(&p.ID, &p.Name, &p.Description, &p.Model, &p.FallbackModel, &tools, &window, &costIn, &costOut, &steps, &calls, &p.Enabled, &p.IsDefault, &p.Position); err != nil {
		return p, err
	}
	if tools.Valid {
		p.SupportsTools = &tools.Bool
	}
	if window.Valid {
		v := int(window.Int64)
		p.ContextWindow = &v
	}
	if steps.Valid {
		v := int(steps.Int64)
		p.MaxSteps = &v
	}
	if calls.Valid {
		v := int(calls.Int64)
		p.MaxToolCalls = &v
	}
	if costIn.Valid {
		p.CostInputPerMTok = &costIn.Float64
	}
	if costOut.Valid {
		p.CostOutputPerMTok = &costOut.Float64
	}
	return p, nil
}

// ListModelProfiles returns the profiles in display order; onlyEnabled hides the disabled ones.
func (d *Database) ListModelProfiles(ctx context.Context, onlyEnabled bool) ([]ModelProfile, error) {
	q := "SELECT " + profileColumns + " FROM ai_model_profiles"
	if onlyEnabled {
		q += " WHERE enabled=1"
	}
	rows, err := d.db.QueryContext(ctx, q+" ORDER BY position,name,id LIMIT 50")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ModelProfile{}
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (d *Database) GetModelProfile(ctx context.Context, id int64) (ModelProfile, error) {
	p, err := scanProfile(d.db.QueryRowContext(ctx, "SELECT "+profileColumns+" FROM ai_model_profiles WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrProfileNotFound
	}
	return p, err
}

// SaveModelProfile creates the profile (ID 0) or updates it, and keeps at
// most one default. It returns the saved id.
func (d *Database) SaveModelProfile(ctx context.Context, p ModelProfile) (int64, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if p.ID == 0 {
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM ai_model_profiles").Scan(&n); err != nil {
			return 0, err
		}
		if n >= MaxModelProfiles {
			return 0, ErrProfileLimit
		}
		res, err := tx.ExecContext(ctx, "INSERT INTO ai_model_profiles(name,description,model_name,fallback_model,supports_tools,context_window,cost_input_per_mtok,cost_output_per_mtok,max_steps,max_tool_calls,enabled,is_default,position) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)",
			p.Name, p.Description, p.Model, nullString(p.FallbackModel), p.SupportsTools, p.ContextWindow, p.CostInputPerMTok, p.CostOutputPerMTok, p.MaxSteps, p.MaxToolCalls, p.Enabled, p.IsDefault, p.Position)
		if err != nil {
			return 0, nameTaken(err, ErrProfileNameTaken)
		}
		if p.ID, err = res.LastInsertId(); err != nil {
			return 0, err
		}
	} else {
		res, err := tx.ExecContext(ctx, "UPDATE ai_model_profiles SET name=?,description=?,model_name=?,fallback_model=?,supports_tools=?,context_window=?,cost_input_per_mtok=?,cost_output_per_mtok=?,max_steps=?,max_tool_calls=?,enabled=?,is_default=?,position=?,updated_at=CURRENT_TIMESTAMP(6) WHERE id=?",
			p.Name, p.Description, p.Model, nullString(p.FallbackModel), p.SupportsTools, p.ContextWindow, p.CostInputPerMTok, p.CostOutputPerMTok, p.MaxSteps, p.MaxToolCalls, p.Enabled, p.IsDefault, p.Position, p.ID)
		if err != nil {
			return 0, nameTaken(err, ErrProfileNameTaken)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			var exists int
			if tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM ai_model_profiles WHERE id=?", p.ID).Scan(&exists) != nil || exists == 0 {
				return 0, ErrProfileNotFound
			}
		}
	}
	if p.IsDefault {
		if _, err := tx.ExecContext(ctx, "UPDATE ai_model_profiles SET is_default=0 WHERE id<>?", p.ID); err != nil {
			return 0, err
		}
	}
	return p.ID, tx.Commit()
}

// DeleteModelProfile removes a profile; teams that used it fall back to the default.
func (d *Database) DeleteModelProfile(ctx context.Context, id int64) error {
	res, err := d.db.ExecContext(ctx, "DELETE FROM ai_model_profiles WHERE id=?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrProfileNotFound
	}
	return nil
}

func nullString(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

// nameTaken turns MySQL's duplicate-key error (1062) into the given error.
func nameTaken(err, taken error) error {
	if strings.Contains(err.Error(), "Error 1062") || strings.Contains(strings.ToLower(err.Error()), "duplicate entry") {
		return taken
	}
	return err
}

// SavedTeam is a user's saved agent team.
type SavedTeam struct {
	ID        int64             `json:"id"`
	Name      string            `json:"name"`
	Chair     bool              `json:"chair"`
	Members   []SavedTeamMember `json:"members"`
	UpdatedAt string            `json:"updated_at,omitempty"`
}

// SavedTeamMember is one character of a team and the model profile it uses
// (nil means the default profile).
type SavedTeamMember struct {
	Persona   string `json:"persona"`
	ProfileID *int64 `json:"profile_id"`
}

func (d *Database) ListSavedTeams(ctx context.Context, userID int64) ([]SavedTeam, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT id,name,chair,DATE_FORMAT(updated_at,'%Y-%m-%dT%H:%i:%sZ') FROM agent_teams WHERE user_id=? ORDER BY name,id", userID)
	if err != nil {
		return nil, err
	}
	teams := []SavedTeam{}
	for rows.Next() {
		var t SavedTeam
		if err := rows.Scan(&t.ID, &t.Name, &t.Chair, &t.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		teams = append(teams, t)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i := range teams {
		if teams[i].Members, err = d.teamMembers(ctx, teams[i].ID); err != nil {
			return nil, err
		}
	}
	return teams, nil
}

func (d *Database) teamMembers(ctx context.Context, teamID int64) ([]SavedTeamMember, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT persona_id,profile_id FROM agent_team_members WHERE team_id=? ORDER BY position", teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SavedTeamMember{}
	for rows.Next() {
		var m SavedTeamMember
		var profile sql.NullInt64
		if err := rows.Scan(&m.Persona, &profile); err != nil {
			return nil, err
		}
		if profile.Valid {
			m.ProfileID = &profile.Int64
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (d *Database) GetSavedTeam(ctx context.Context, userID, id int64) (SavedTeam, error) {
	var t SavedTeam
	err := d.db.QueryRowContext(ctx, "SELECT id,name,chair,DATE_FORMAT(updated_at,'%Y-%m-%dT%H:%i:%sZ') FROM agent_teams WHERE user_id=? AND id=?", userID, id).Scan(&t.ID, &t.Name, &t.Chair, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrTeamNotFound
	}
	if err != nil {
		return t, err
	}
	t.Members, err = d.teamMembers(ctx, t.ID)
	return t, err
}

// SaveSavedTeam creates the team (ID 0) or replaces one of the user's own.
func (d *Database) SaveSavedTeam(ctx context.Context, userID int64, t SavedTeam) (int64, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if t.ID == 0 {
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM agent_teams WHERE user_id=?", userID).Scan(&n); err != nil {
			return 0, err
		}
		if n >= MaxSavedTeams {
			return 0, ErrTeamLimit
		}
		res, err := tx.ExecContext(ctx, "INSERT INTO agent_teams(user_id,name,chair) VALUES(?,?,?)", userID, t.Name, t.Chair)
		if err != nil {
			return 0, nameTaken(err, ErrTeamNameTaken)
		}
		if t.ID, err = res.LastInsertId(); err != nil {
			return 0, err
		}
	} else {
		res, err := tx.ExecContext(ctx, "UPDATE agent_teams SET name=?,chair=?,updated_at=CURRENT_TIMESTAMP(6) WHERE user_id=? AND id=?", t.Name, t.Chair, userID, t.ID)
		if err != nil {
			return 0, nameTaken(err, ErrTeamNameTaken)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			var own int
			if tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM agent_teams WHERE user_id=? AND id=?", userID, t.ID).Scan(&own) != nil || own == 0 {
				return 0, ErrTeamNotFound
			}
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM agent_team_members WHERE team_id=?", t.ID); err != nil {
			return 0, err
		}
	}
	for i, m := range t.Members {
		if _, err := tx.ExecContext(ctx, "INSERT INTO agent_team_members(team_id,position,persona_id,profile_id) VALUES(?,?,?,?)", t.ID, i, m.Persona, m.ProfileID); err != nil {
			return 0, err
		}
	}
	return t.ID, tx.Commit()
}

func (d *Database) DeleteSavedTeam(ctx context.Context, userID, id int64) error {
	res, err := d.db.ExecContext(ctx, "DELETE FROM agent_teams WHERE user_id=? AND id=?", userID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTeamNotFound
	}
	return nil
}
