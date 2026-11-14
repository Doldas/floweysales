package storage

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"floweysales/internal/instruments"
)

// Instrument profiles and classification overrides (changeset 047) implement
// instruments.Store. Profiles are provider facts shared by every user;
// overrides are read and written only with the owner's user id.

// keyClause builds "(provider=? AND symbol=?) OR ..." for a list of keys.
func keyClause(keys []instruments.Key) (string, []any) {
	parts := make([]string, 0, len(keys))
	args := make([]any, 0, 2*len(keys))
	for _, k := range keys {
		parts = append(parts, "(provider=? AND symbol=?)")
		args = append(args, k.Provider, k.Symbol)
	}
	return strings.Join(parts, " OR "), args
}

// keyBatch bounds the number of keys per query.
const keyBatch = 200

func batches(keys []instruments.Key) [][]instruments.Key {
	var out [][]instruments.Key
	for len(keys) > 0 {
		n := min(len(keys), keyBatch)
		out = append(out, keys[:n])
		keys = keys[n:]
	}
	return out
}

func (d *Database) GetProfiles(ctx context.Context, keys []instruments.Key) (map[instruments.Key]instruments.CachedProfile, error) {
	out := map[instruments.Key]instruments.CachedProfile{}
	for _, batch := range batches(keys) {
		where, args := keyClause(batch)
		rows, err := d.db.QueryContext(ctx, "SELECT provider,symbol,status,profile,retrieved_at,expires_at FROM instrument_profiles WHERE "+where, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var c instruments.CachedProfile
			var raw string
			if err := rows.Scan(&c.Provider, &c.Symbol, &c.Status, &raw, &c.RetrievedAt, &c.ExpiresAt); err != nil {
				rows.Close()
				return nil, err
			}
			if c.Status == instruments.StatusOK && raw != "" {
				var p instruments.Profile
				if json.Unmarshal([]byte(raw), &p) == nil {
					c.Profile = &p
				}
			}
			c.RetrievedAt, c.ExpiresAt = c.RetrievedAt.UTC(), c.ExpiresAt.UTC()
			out[c.Key] = c
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (d *Database) PutProfile(ctx context.Context, c instruments.CachedProfile) error {
	raw := ""
	if c.Profile != nil {
		b, err := json.Marshal(c.Profile)
		if err != nil {
			return err
		}
		raw = string(b)
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO instrument_profiles (provider,symbol,status,profile,retrieved_at,expires_at) VALUES (?,?,?,?,?,?)
		ON DUPLICATE KEY UPDATE status=VALUES(status), profile=VALUES(profile), retrieved_at=VALUES(retrieved_at), expires_at=VALUES(expires_at)`,
		c.Provider, c.Symbol, c.Status, raw, c.RetrievedAt.UTC(), c.ExpiresAt.UTC())
	return err
}

func (d *Database) DeleteProfiles(ctx context.Context, provider string) (int, error) {
	query, args := "DELETE FROM instrument_profiles", []any{}
	if provider != "" {
		query, args = query+" WHERE provider=?", append(args, provider)
	}
	res, err := d.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (d *Database) GetOverrides(ctx context.Context, userID int64, keys []instruments.Key) (map[instruments.Key]instruments.Override, error) {
	out := map[instruments.Key]instruments.Override{}
	for _, batch := range batches(keys) {
		where, args := keyClause(batch)
		rows, err := d.db.QueryContext(ctx, "SELECT provider,symbol,asset_class,sector,industry,country,themes,updated_at FROM instrument_classifications WHERE user_id=? AND ("+where+")",
			append([]any{userID}, args...)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var o instruments.Override
			var themes string
			if err := rows.Scan(&o.Provider, &o.Symbol, &o.AssetClass, &o.Sector, &o.Industry, &o.Country, &themes, &o.UpdatedAt); err != nil {
				rows.Close()
				return nil, err
			}
			o.Themes = decodeTags(themes)
			o.UpdatedAt = o.UpdatedAt.UTC()
			out[o.Key] = o
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (d *Database) PutOverride(ctx context.Context, userID int64, o instruments.Override) error {
	if o.Empty() {
		return d.DeleteOverride(ctx, userID, o.Key)
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO instrument_classifications (user_id,provider,symbol,asset_class,sector,industry,country,themes,updated_at) VALUES (?,?,?,?,?,?,?,?,?)
		ON DUPLICATE KEY UPDATE asset_class=VALUES(asset_class), sector=VALUES(sector), industry=VALUES(industry), country=VALUES(country), themes=VALUES(themes), updated_at=VALUES(updated_at)`,
		userID, o.Provider, o.Symbol, o.AssetClass, o.Sector, o.Industry, o.Country, encodeTags(o.Themes), time.Now().UTC())
	return err
}

func (d *Database) DeleteOverride(ctx context.Context, userID int64, k instruments.Key) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM instrument_classifications WHERE user_id=? AND provider=? AND symbol=?", userID, k.Provider, k.Symbol)
	return err
}
