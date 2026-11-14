package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"floweysales/internal/collections"
)

// Market collections (changeset 046) implement collections.Store. Every
// query is scoped to the owner: members are reached through a collection row
// whose user_id matches, never by member id alone.

const collectionColumns = "c.id,c.name,c.description,c.kind,c.category,c.tags,c.archived,c.created_at,c.updated_at," +
	"(SELECT COUNT(*) FROM market_collection_members m WHERE m.collection_id=c.id)"

const memberColumns = "id,collection_id,provider,symbol,name,asset_type,exchange,mic,isin,currency,country,note,tags,weight,added_at"

func scanCollection(r rowScanner) (collections.Collection, error) {
	var c collections.Collection
	var kind, tags string
	if err := r.Scan(&c.ID, &c.Name, &c.Description, &kind, &c.Category, &tags, &c.Archived, &c.CreatedAt, &c.UpdatedAt, &c.MemberCount); err != nil {
		return c, err
	}
	c.Kind = collections.Kind(kind)
	c.Tags = decodeTags(tags)
	return c, nil
}

func scanMember(r rowScanner) (collections.Member, error) {
	var m collections.Member
	var tags string
	var weight sql.NullFloat64
	in := &m.Instrument
	if err := r.Scan(&m.ID, &m.CollectionID, &in.Provider, &in.Symbol, &in.Name, &in.Type, &in.Exchange, &in.MIC, &in.ISIN, &in.Currency, &in.Country, &m.Note, &tags, &weight, &m.AddedAt); err != nil {
		return m, err
	}
	m.Tags = decodeTags(tags)
	if weight.Valid {
		w := weight.Float64
		m.Weight = &w
	}
	return m, nil
}

func decodeTags(raw string) []string {
	tags := []string{}
	_ = json.Unmarshal([]byte(raw), &tags)
	if tags == nil {
		tags = []string{}
	}
	return tags
}

func encodeTags(tags []string) string {
	if tags == nil {
		tags = []string{}
	}
	raw, _ := json.Marshal(tags)
	return string(raw)
}

func nullWeight(w *float64) any {
	if w == nil {
		return nil
	}
	return *w
}

type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// lockCollection checks ownership inside a transaction and holds the row, so
// concurrent member changes to one collection run one after the other.
func lockCollection(ctx context.Context, tx *sql.Tx, userID, id int64) error {
	var got int64
	err := tx.QueryRowContext(ctx, "SELECT id FROM market_collections WHERE id=? AND user_id=? FOR UPDATE", id, userID).Scan(&got)
	if errors.Is(err, sql.ErrNoRows) {
		return collections.ErrNotFound
	}
	return err
}

func collectionNameTaken(ctx context.Context, q querier, userID int64, name string, exceptID int64) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM market_collections WHERE user_id=? AND LOWER(name)=LOWER(?) AND id<>?", userID, name, exceptID).Scan(&n)
	return n > 0, err
}

func (d *Database) inTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ListCollections returns all of a user's collections with their member counts.
func (d *Database) ListCollections(ctx context.Context, userID int64) ([]collections.Collection, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT "+collectionColumns+" FROM market_collections c WHERE c.user_id=? ORDER BY c.id", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []collections.Collection{}
	for rows.Next() {
		c, err := scanCollection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCollection reads one of a user's collections.
func (d *Database) GetCollection(ctx context.Context, userID, id int64) (collections.Collection, error) {
	c, err := scanCollection(d.db.QueryRowContext(ctx, "SELECT "+collectionColumns+" FROM market_collections c WHERE c.user_id=? AND c.id=?", userID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, collections.ErrNotFound
	}
	return c, err
}

// CreateCollection saves a validated collection.
func (d *Database) CreateCollection(ctx context.Context, userID int64, c collections.Collection) (collections.Collection, error) {
	var id int64
	err := d.inTx(ctx, func(tx *sql.Tx) error {
		var count int
		// Locking the user's rows keeps two parallel creates under the limit.
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM market_collections WHERE user_id=? FOR UPDATE", userID).Scan(&count); err != nil {
			return err
		}
		if count >= collections.MaxCollectionsPerUser {
			return collections.ErrLimit
		}
		if taken, err := collectionNameTaken(ctx, tx, userID, c.Name, 0); err != nil {
			return err
		} else if taken {
			return collections.ErrNameTaken
		}
		res, err := tx.ExecContext(ctx, "INSERT INTO market_collections(user_id,name,description,kind,category,tags,archived) VALUES(?,?,?,?,?,?,?)",
			userID, c.Name, c.Description, string(c.Kind), c.Category, encodeTags(c.Tags), c.Archived)
		if isDuplicateKey(err) {
			return collections.ErrNameTaken
		}
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	if err != nil {
		return collections.Collection{}, err
	}
	return d.GetCollection(ctx, userID, id)
}

// UpdateCollection saves a collection's editable fields. The kind is not editable.
func (d *Database) UpdateCollection(ctx context.Context, userID int64, c collections.Collection) (collections.Collection, error) {
	if taken, err := collectionNameTaken(ctx, d.db, userID, c.Name, c.ID); err != nil {
		return c, err
	} else if taken {
		return c, collections.ErrNameTaken
	}
	_, err := d.db.ExecContext(ctx, "UPDATE market_collections SET name=?,description=?,category=?,tags=?,archived=?,updated_at=CURRENT_TIMESTAMP(6) WHERE user_id=? AND id=?",
		c.Name, c.Description, c.Category, encodeTags(c.Tags), c.Archived, userID, c.ID)
	if isDuplicateKey(err) {
		return c, collections.ErrNameTaken
	}
	if err != nil {
		return c, err
	}
	return d.GetCollection(ctx, userID, c.ID)
}

// DeleteCollection removes a collection; its members go with it (ON DELETE CASCADE).
func (d *Database) DeleteCollection(ctx context.Context, userID, id int64) error {
	res, err := d.db.ExecContext(ctx, "DELETE FROM market_collections WHERE user_id=? AND id=?", userID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return collections.ErrNotFound
	}
	return nil
}

func (d *Database) ownsCollection(ctx context.Context, userID, id int64) error {
	var got int64
	err := d.db.QueryRowContext(ctx, "SELECT id FROM market_collections WHERE id=? AND user_id=?", id, userID).Scan(&got)
	if errors.Is(err, sql.ErrNoRows) {
		return collections.ErrNotFound
	}
	return err
}

func (d *Database) queryMembers(ctx context.Context, query string, args ...any) ([]collections.Member, error) {
	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []collections.Member{}
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListMembers returns one page of a collection's members, oldest first, and the total.
func (d *Database) ListMembers(ctx context.Context, userID, collectionID int64, page collections.Page) ([]collections.Member, int, error) {
	if err := d.ownsCollection(ctx, userID, collectionID); err != nil {
		return nil, 0, err
	}
	page = page.Normalize()
	var total int
	if err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM market_collection_members WHERE collection_id=?", collectionID).Scan(&total); err != nil {
		return nil, 0, err
	}
	out, err := d.queryMembers(ctx, "SELECT "+memberColumns+" FROM market_collection_members WHERE collection_id=? ORDER BY id LIMIT ? OFFSET ?", collectionID, page.Limit, page.Offset)
	return out, total, err
}

// AllMembers returns every member of a collection (at most collections.MaxMembers).
func (d *Database) AllMembers(ctx context.Context, userID, collectionID int64) ([]collections.Member, error) {
	if err := d.ownsCollection(ctx, userID, collectionID); err != nil {
		return nil, err
	}
	return d.queryMembers(ctx, "SELECT "+memberColumns+" FROM market_collection_members WHERE collection_id=? ORDER BY id", collectionID)
}

func insertMember(ctx context.Context, tx *sql.Tx, collectionID int64, m collections.Member) (int64, error) {
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM market_collection_members WHERE collection_id=?", collectionID).Scan(&count); err != nil {
		return 0, err
	}
	if count >= collections.MaxMembers {
		return 0, collections.ErrLimit
	}
	in := m.Instrument
	res, err := tx.ExecContext(ctx, "INSERT INTO market_collection_members(collection_id,provider,symbol,name,asset_type,exchange,mic,isin,currency,country,note,tags,weight) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)",
		collectionID, in.Provider, in.Symbol, in.Name, in.Type, in.Exchange, in.MIC, in.ISIN, in.Currency, in.Country, m.Note, encodeTags(m.Tags), nullWeight(m.Weight))
	if isDuplicateKey(err) {
		return 0, collections.ErrDuplicateMember
	}
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE market_collections SET updated_at=CURRENT_TIMESTAMP(6) WHERE id=?", collectionID); err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *Database) getMember(ctx context.Context, collectionID, memberID int64) (collections.Member, error) {
	m, err := scanMember(d.db.QueryRowContext(ctx, "SELECT "+memberColumns+" FROM market_collection_members WHERE collection_id=? AND id=?", collectionID, memberID))
	if errors.Is(err, sql.ErrNoRows) {
		return m, collections.ErrMemberNotFound
	}
	return m, err
}

// AddMember inserts a validated member.
func (d *Database) AddMember(ctx context.Context, userID, collectionID int64, m collections.Member) (collections.Member, error) {
	var id int64
	err := d.inTx(ctx, func(tx *sql.Tx) error {
		if err := lockCollection(ctx, tx, userID, collectionID); err != nil {
			return err
		}
		var err error
		id, err = insertMember(ctx, tx, collectionID, m)
		return err
	})
	if err != nil {
		return collections.Member{}, err
	}
	return d.getMember(ctx, collectionID, id)
}

// UpdateMember saves a member's note, tags and weight.
func (d *Database) UpdateMember(ctx context.Context, userID int64, m collections.Member) (collections.Member, error) {
	if err := d.ownsCollection(ctx, userID, m.CollectionID); err != nil {
		return m, err
	}
	if _, err := d.getMember(ctx, m.CollectionID, m.ID); err != nil {
		return m, err
	}
	if _, err := d.db.ExecContext(ctx, "UPDATE market_collection_members SET note=?,tags=?,weight=? WHERE collection_id=? AND id=?",
		m.Note, encodeTags(m.Tags), nullWeight(m.Weight), m.CollectionID, m.ID); err != nil {
		return m, err
	}
	if _, err := d.db.ExecContext(ctx, "UPDATE market_collections SET updated_at=CURRENT_TIMESTAMP(6) WHERE id=?", m.CollectionID); err != nil {
		return m, err
	}
	return d.getMember(ctx, m.CollectionID, m.ID)
}

// RemoveMember takes one member out of one collection.
func (d *Database) RemoveMember(ctx context.Context, userID, collectionID, memberID int64) error {
	return d.inTx(ctx, func(tx *sql.Tx) error {
		if err := lockCollection(ctx, tx, userID, collectionID); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, "DELETE FROM market_collection_members WHERE collection_id=? AND id=?", collectionID, memberID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return collections.ErrMemberNotFound
		}
		_, err = tx.ExecContext(ctx, "UPDATE market_collections SET updated_at=CURRENT_TIMESTAMP(6) WHERE id=?", collectionID)
		return err
	})
}

// TransferMember copies a member into another collection, and with move
// deletes the original, in one transaction.
func (d *Database) TransferMember(ctx context.Context, userID, fromID, memberID, toID int64, move bool, weight *float64) (collections.Member, error) {
	var id int64
	err := d.inTx(ctx, func(tx *sql.Tx) error {
		// Lock in id order so two opposite transfers cannot deadlock.
		first, second := fromID, toID
		if second < first {
			first, second = second, first
		}
		for _, cid := range []int64{first, second} {
			if err := lockCollection(ctx, tx, userID, cid); err != nil {
				return err
			}
		}
		m, err := scanMember(tx.QueryRowContext(ctx, "SELECT "+memberColumns+" FROM market_collection_members WHERE collection_id=? AND id=?", fromID, memberID))
		if errors.Is(err, sql.ErrNoRows) {
			return collections.ErrMemberNotFound
		}
		if err != nil {
			return err
		}
		m.Weight = weight
		if id, err = insertMember(ctx, tx, toID, m); err != nil {
			return err
		}
		if move {
			if _, err := tx.ExecContext(ctx, "DELETE FROM market_collection_members WHERE collection_id=? AND id=?", fromID, memberID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE market_collections SET updated_at=CURRENT_TIMESTAMP(6) WHERE id=?", fromID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return collections.Member{}, err
	}
	return d.getMember(ctx, toID, id)
}

// CollectionsContaining lists the user's collections that hold provider:symbol.
func (d *Database) CollectionsContaining(ctx context.Context, userID int64, provider, symbol string) ([]collections.Collection, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT "+collectionColumns+" FROM market_collections c WHERE c.user_id=? AND EXISTS "+
		"(SELECT 1 FROM market_collection_members m WHERE m.collection_id=c.id AND m.provider=? AND m.symbol=?) ORDER BY c.id", userID, provider, symbol)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []collections.Collection{}
	for rows.Next() {
		c, err := scanCollection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

var _ collections.Store = (*Database)(nil)
