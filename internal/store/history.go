package store

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

// HistoryRepo stores captured entries and their outcomes.
type HistoryRepo struct{ s *Store }

const historyCols = `id, capture_id, captured_at, "trigger", kind, original_name, local_path, staged, thumb_path,
	size, width, height, sha256, profile_id, profile_name, rule_name, action, status, remote_dir, remote_name,
	remote_path, attempts, error, finished_at`

func scanHistory(row []any) HistoryEntry {
	return HistoryEntry{
		ID:           i64(row[0]),
		CaptureID:    str(row[1]),
		CapturedAt:   timeOf(row[2]),
		Trigger:      Trigger(str(row[3])),
		Kind:         Kind(str(row[4])),
		OriginalName: str(row[5]),
		LocalPath:    str(row[6]),
		Staged:       boolean(row[7]),
		ThumbPath:    str(row[8]),
		Size:         i64(row[9]),
		Width:        int(i64(row[10])),
		Height:       int(i64(row[11])),
		SHA256:       str(row[12]),
		ProfileID:    i64(row[13]),
		ProfileName:  str(row[14]),
		RuleName:     str(row[15]),
		Action:       Action(str(row[16])),
		Status:       Status(str(row[17])),
		RemoteDir:    str(row[18]),
		RemoteName:   str(row[19]),
		RemotePath:   str(row[20]),
		Attempts:     int(i64(row[21])),
		Error:        str(row[22]),
		FinishedAt:   timeOf(row[23]),
	}
}

func (r HistoryRepo) rows(ctx context.Context, sql string, args ...any) ([]HistoryEntry, error) {
	rows, err := r.s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	out := make([]HistoryEntry, 0, len(rows.Values))
	for _, row := range rows.Values {
		out = append(out, scanHistory(row))
	}
	return out, nil
}

func profileArg(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// Insert records entries, and the counter value their names used when
// counter is not nil, in one transaction. It returns the new IDs in order.
func (r HistoryRepo) Insert(ctx context.Context, entries []HistoryEntry, counter *CounterValue) ([]int64, error) {
	stmts := make([]sqlite.Statement, 0, len(entries)+1)
	if counter != nil {
		stmts = append(stmts, counter.statement())
	}
	for _, e := range entries {
		stmts = append(stmts, sqlite.Statement{
			SQL: `INSERT INTO history (capture_id, captured_at, "trigger", kind, original_name, local_path, staged,
				thumb_path, size, width, height, sha256, profile_id, profile_name, rule_name, action, status,
				remote_dir, remote_name, remote_path, attempts, error, finished_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			Args: []any{e.CaptureID, millis(e.CapturedAt), string(e.Trigger), string(e.Kind), e.OriginalName,
				e.LocalPath, e.Staged, e.ThumbPath, e.Size, e.Width, e.Height, e.SHA256, profileArg(e.ProfileID),
				e.ProfileName, e.RuleName, string(e.Action), string(e.Status), e.RemoteDir, e.RemoteName,
				e.RemotePath, e.Attempts, e.Error, millis(e.FinishedAt)},
		})
	}
	results, err := r.s.db.Transaction(ctx, stmts)
	if err != nil {
		return nil, err
	}
	if counter != nil {
		results = results[1:]
	}
	ids := make([]int64, len(results))
	for i, res := range results {
		ids[i] = res.LastInsertID
	}
	return ids, nil
}

// Get returns the entry with id, or ErrNotFound.
func (r HistoryRepo) Get(ctx context.Context, id int64) (HistoryEntry, error) {
	out, err := r.rows(ctx, "SELECT "+historyCols+" FROM history WHERE id = ?", id)
	if err != nil {
		return HistoryEntry{}, err
	}
	if len(out) == 0 {
		return HistoryEntry{}, ErrNotFound
	}
	return out[0], nil
}

// ByCapture returns the entries of one capture, in insertion order.
func (r HistoryRepo) ByCapture(ctx context.Context, captureID string) ([]HistoryEntry, error) {
	return r.rows(ctx, "SELECT "+historyCols+" FROM history WHERE capture_id = ? ORDER BY id", captureID)
}

// Update describes a status transition. Zero fields other than Status
// keep their stored values, except that Error is always replaced, so a
// transition clears a previous error unless it sets one.
type Update struct {
	Status     Status
	Attempts   int // > 0 to set
	Error      string
	RemotePath string    // non-empty to set
	FinishedAt time.Time // non-zero to set; cleared for queued and uploading
}

// SetStatus applies u to the entry with id.
func (r HistoryRepo) SetStatus(ctx context.Context, id int64, u Update) error {
	var finished any = millis(u.FinishedAt)
	if u.Status.Active() {
		finished = nil
	}
	res, err := r.s.db.Execute(ctx, `UPDATE history SET status = ?,
		attempts = CASE WHEN ? > 0 THEN ? ELSE attempts END,
		error = ?,
		remote_path = CASE WHEN ? <> '' THEN ? ELSE remote_path END,
		finished_at = ?
		WHERE id = ?`,
		string(u.Status), u.Attempts, u.Attempts, u.Error, u.RemotePath, u.RemotePath, finished, id)
	if err != nil {
		return err
	}
	if res.Changes == 0 {
		return ErrNotFound
	}
	return nil
}

// Requeue describes queuing an entry again: Retry keeps its profile and
// name; "Upload anyway" sets the active profile, a manual trigger, and
// names rendered with the default templates.
type Requeue struct {
	Trigger     Trigger
	ProfileID   int64
	ProfileName string
	RuleName    string
	RemoteDir   string
	RemoteName  string
}

// Requeue queues the entry with id again, resetting its attempts, error,
// remote path, and finish time.
func (r HistoryRepo) Requeue(ctx context.Context, id int64, q Requeue) error {
	res, err := r.s.db.Execute(ctx, `UPDATE history SET status = 'queued', action = 'upload', "trigger" = ?,
		profile_id = ?, profile_name = ?, rule_name = ?, remote_dir = ?, remote_name = ?, remote_path = '',
		attempts = 0, error = '', finished_at = NULL WHERE id = ?`,
		string(q.Trigger), profileArg(q.ProfileID), q.ProfileName, q.RuleName, q.RemoteDir, q.RemoteName, id)
	if err != nil {
		return err
	}
	if res.Changes == 0 {
		return ErrNotFound
	}
	return nil
}

// Filter selects history entries for the Activity view.
type Filter struct {
	Statuses    []Status // any of; empty: all
	Kinds       []Kind   // any of; empty: all
	HideSkipped bool
	// Search matches original names and remote paths, case-insensitive.
	Search string
	Limit  int // 0: no limit
	Offset int
}

// Query returns a page of entries matching f, newest first, and how many
// match in all.
func (r HistoryRepo) Query(ctx context.Context, f Filter) ([]HistoryEntry, int, error) {
	var (
		where []string
		args  []any
	)
	if len(f.Statuses) > 0 {
		where = append(where, "status IN ("+placeholders(len(f.Statuses))+")")
		for _, s := range f.Statuses {
			args = append(args, string(s))
		}
	}
	if len(f.Kinds) > 0 {
		where = append(where, "kind IN ("+placeholders(len(f.Kinds))+")")
		for _, k := range f.Kinds {
			args = append(args, string(k))
		}
	}
	if f.HideSkipped {
		where = append(where, "status <> 'skipped'")
	}
	if q := strings.TrimSpace(f.Search); q != "" {
		// instr on lower() avoids LIKE's wildcard escaping.
		where = append(where, "(instr(lower(original_name), lower(?)) > 0 OR instr(lower(remote_path), lower(?)) > 0)")
		args = append(args, q, q)
	}
	cond := ""
	if len(where) > 0 {
		cond = " WHERE " + strings.Join(where, " AND ")
	}
	count, err := r.s.db.Query(ctx, "SELECT count(*) FROM history"+cond, args...)
	if err != nil {
		return nil, 0, err
	}
	total := int(i64(count.Values[0][0]))
	sql := "SELECT " + historyCols + " FROM history" + cond + " ORDER BY captured_at DESC, id DESC"
	if f.Limit > 0 {
		sql += " LIMIT ? OFFSET ?"
		args = append(args, f.Limit, f.Offset)
	}
	out, err := r.rows(ctx, sql, args...)
	return out, total, err
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

// Summary is the Activity view's numbers for one day.
type Summary struct {
	Uploaded int
	Bytes    int64
	Failed   int
}

// Summary counts entries uploaded and failed whose transfer finished in
// [from, to), and the bytes uploaded.
func (r HistoryRepo) Summary(ctx context.Context, from, to time.Time) (Summary, error) {
	rows, err := r.s.db.Query(ctx, `SELECT
		coalesce(sum(status = 'uploaded'), 0),
		coalesce(sum(CASE WHEN status = 'uploaded' THEN size ELSE 0 END), 0),
		coalesce(sum(status = 'failed'), 0)
		FROM history WHERE finished_at >= ? AND finished_at < ?`, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return Summary{}, err
	}
	row := rows.Values[0]
	return Summary{Uploaded: int(i64(row[0])), Bytes: i64(row[1]), Failed: int(i64(row[2]))}, nil
}

// Prune removes finished entries older than maxDays and, beyond maxCount
// entries in all, the oldest finished ones. It never removes queued or
// uploading entries. It returns the removed entries so their staged files
// can be deleted. A limit <= 0 is off.
func (r HistoryRepo) Prune(ctx context.Context, maxCount, maxDays int, now time.Time) ([]HistoryEntry, error) {
	var removed []HistoryEntry
	if maxDays > 0 {
		cutoff := now.Add(-time.Duration(maxDays) * 24 * time.Hour)
		out, err := r.rows(ctx, `DELETE FROM history WHERE status NOT IN ('queued', 'uploading')
			AND captured_at < ? RETURNING `+historyCols, cutoff.UnixMilli())
		if err != nil {
			return nil, err
		}
		removed = append(removed, out...)
	}
	if maxCount > 0 {
		out, err := r.rows(ctx, `DELETE FROM history WHERE id IN (
			SELECT id FROM history WHERE status NOT IN ('queued', 'uploading')
			ORDER BY captured_at, id
			LIMIT max(0, (SELECT count(*) FROM history) - ?)) RETURNING `+historyCols, maxCount)
		if err != nil {
			return nil, err
		}
		removed = append(removed, out...)
	}
	return removed, nil
}

// MarkInterrupted fails every queued or uploading entry with
// ReasonInterrupted, as on startup after the app stopped mid-transfer, and
// returns how many it marked.
func (r HistoryRepo) MarkInterrupted(ctx context.Context, now time.Time) (int, error) {
	res, err := r.s.db.Execute(ctx, `UPDATE history SET status = 'failed', error = ?, finished_at = ?
		WHERE status IN ('queued', 'uploading')`, ReasonInterrupted, now.UnixMilli())
	if err != nil {
		return 0, err
	}
	return int(res.Changes), nil
}

// Delete removes the entry with id and returns it.
func (r HistoryRepo) Delete(ctx context.Context, id int64) (HistoryEntry, error) {
	out, err := r.rows(ctx, "DELETE FROM history WHERE id = ? RETURNING "+historyCols, id)
	if err != nil {
		return HistoryEntry{}, err
	}
	if len(out) == 0 {
		return HistoryEntry{}, ErrNotFound
	}
	return out[0], nil
}

// Clear removes every entry that is not queued or uploading and returns
// them.
func (r HistoryRepo) Clear(ctx context.Context) ([]HistoryEntry, error) {
	return r.rows(ctx, "DELETE FROM history WHERE status NOT IN ('queued', 'uploading') RETURNING "+historyCols)
}

// StagedFiles returns every staged file and thumbnail that a history entry
// refers to.
func (r HistoryRepo) StagedFiles(ctx context.Context) (map[string]bool, error) {
	rows, err := r.s.db.Query(ctx, "SELECT local_path, thumb_path FROM history WHERE staged = 1")
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, 2*len(rows.Values))
	for _, row := range rows.Values {
		for _, v := range row {
			if p := str(v); p != "" {
				out[p] = true
			}
		}
	}
	return out, nil
}
