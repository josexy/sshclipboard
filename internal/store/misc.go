package store

import (
	"context"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

// NamingRepo stores the filename template of each kind.
type NamingRepo struct{ s *Store }

func namingUpsert(n Naming) sqlite.Statement {
	return sqlite.Statement{
		SQL: `INSERT INTO naming (kind, template, lowercase, spaces_to_underscore) VALUES (?, ?, ?, ?)
			ON CONFLICT (kind) DO UPDATE SET template = excluded.template, lowercase = excluded.lowercase,
			spaces_to_underscore = excluded.spaces_to_underscore`,
		Args: []any{string(n.Kind), n.Template, n.Lowercase, n.SpacesToUnderscore},
	}
}

// All returns the templates of image, file, and text, in that order, with
// the defaults for any missing row.
func (r NamingRepo) All(ctx context.Context) ([]Naming, error) {
	rows, err := r.s.db.Query(ctx, "SELECT kind, template, lowercase, spaces_to_underscore FROM naming")
	if err != nil {
		return nil, err
	}
	out := DefaultNaming()
	for _, row := range rows.Values {
		for i := range out {
			if string(out[i].Kind) == str(row[0]) {
				out[i] = Naming{Kind: out[i].Kind, Template: str(row[1]), Lowercase: boolean(row[2]),
					SpacesToUnderscore: boolean(row[3])}
			}
		}
	}
	return out, nil
}

// Get returns the template of kind.
func (r NamingRepo) Get(ctx context.Context, kind Kind) (Naming, error) {
	all, err := r.All(ctx)
	if err != nil {
		return Naming{}, err
	}
	for _, n := range all {
		if n.Kind == kind {
			return n, nil
		}
	}
	return Naming{}, ErrNotFound
}

// Save stores n.
func (r NamingRepo) Save(ctx context.Context, n Naming) error {
	st := namingUpsert(n)
	_, err := r.s.db.Execute(ctx, st.SQL, st.Args...)
	return err
}

// KnownHostsRepo stores the host keys the user trusted in the app.
type KnownHostsRepo struct{ s *Store }

// Get returns the trusted key for host and port; ok is false when there is
// none.
func (r KnownHostsRepo) Get(ctx context.Context, host string, port int) (kh KnownHost, ok bool, err error) {
	rows, err := r.s.db.Query(ctx, `SELECT host, port, key_type, key_b64, fingerprint_sha256, added_at
		FROM known_hosts WHERE host = ? AND port = ?`, host, port)
	if err != nil || len(rows.Values) == 0 {
		return KnownHost{}, false, err
	}
	row := rows.Values[0]
	return KnownHost{Host: str(row[0]), Port: int(i64(row[1])), KeyType: str(row[2]), KeyBase64: str(row[3]),
		Fingerprint: str(row[4]), AddedAt: timeOf(row[5])}, true, nil
}

// Put trusts kh, replacing any key trusted for its host and port.
func (r KnownHostsRepo) Put(ctx context.Context, kh KnownHost) error {
	if kh.AddedAt.IsZero() {
		kh.AddedAt = time.Now()
	}
	_, err := r.s.db.Execute(ctx, `INSERT INTO known_hosts (host, port, key_type, key_b64, fingerprint_sha256, added_at)
		VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT (host, port) DO UPDATE SET key_type = excluded.key_type,
		key_b64 = excluded.key_b64, fingerprint_sha256 = excluded.fingerprint_sha256, added_at = excluded.added_at`,
		kh.Host, kh.Port, kh.KeyType, kh.KeyBase64, kh.Fingerprint, millis(kh.AddedAt))
	return err
}

// Delete forgets the key of host and port.
func (r KnownHostsRepo) Delete(ctx context.Context, host string, port int) error {
	_, err := r.s.db.Execute(ctx, "DELETE FROM known_hosts WHERE host = ? AND port = ?", host, port)
	return err
}

// CountersRepo keeps the per-profile, per-day {counter} sequences.
type CountersRepo struct{ s *Store }

// Day formats t's local date as counters key it.
func Day(t time.Time) string { return t.Format("2006-01-02") }

// Next increments the counter of profile on day and returns the new value,
// starting at 1.
func (r CountersRepo) Next(ctx context.Context, profileID int64, day string) (int, error) {
	rows, err := r.s.db.Query(ctx, `INSERT INTO counters (profile_id, day, value) VALUES (?, ?, 1)
		ON CONFLICT (profile_id, day) DO UPDATE SET value = value + 1 RETURNING value`, profileID, day)
	if err != nil {
		return 0, err
	}
	return int(i64(rows.Values[0][0])), nil
}

// Current returns the counter's last value, 0 when none was used that day.
// The Controller reads it, renders names with the values after it, and
// stores the last one used with the history rows (HistoryRepo.Insert), in
// one transaction.
func (r CountersRepo) Current(ctx context.Context, profileID int64, day string) (int, error) {
	rows, err := r.s.db.Query(ctx, "SELECT value FROM counters WHERE profile_id = ? AND day = ?", profileID, day)
	if err != nil || len(rows.Values) == 0 {
		return 0, err
	}
	return int(i64(rows.Values[0][0])), nil
}

// CounterValue is a counter's new value, stored with the history rows whose
// names used it.
type CounterValue struct {
	ProfileID int64
	Day       string
	Value     int
}

func (c CounterValue) statement() sqlite.Statement {
	// max() keeps a counter from going back if two writers ever raced.
	return sqlite.Statement{
		SQL: `INSERT INTO counters (profile_id, day, value) VALUES (?, ?, ?)
			ON CONFLICT (profile_id, day) DO UPDATE SET value = max(value, excluded.value)`,
		Args: []any{c.ProfileID, c.Day, c.Value},
	}
}
