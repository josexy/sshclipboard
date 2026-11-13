package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/egoist/mygo/plugins/sqlite"
)

// RulesRepo stores the ordered rule list.
type RulesRepo struct{ s *Store }

const ruleCols = "id, position, name, enabled, action, conditions, dir_override, template_override"

func scanRule(row []any) (Rule, error) {
	r := Rule{
		ID:               i64(row[0]),
		Position:         int(i64(row[1])),
		Name:             str(row[2]),
		Enabled:          boolean(row[3]),
		Action:           Action(str(row[4])),
		DirOverride:      str(row[6]),
		TemplateOverride: str(row[7]),
	}
	if err := json.Unmarshal([]byte(str(row[5])), &r.Conditions); err != nil {
		return Rule{}, fmt.Errorf("rule %d conditions: %w", r.ID, err)
	}
	return r, nil
}

func ruleInsert(r Rule) sqlite.Statement {
	return sqlite.Statement{
		SQL: `INSERT INTO rules (position, name, enabled, action, conditions, dir_override, template_override)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
		Args: []any{r.Position, r.Name, r.Enabled, string(r.Action), conditionsJSON(r.Conditions),
			nullable(r.DirOverride), nullable(r.TemplateOverride)},
	}
}

// List returns the rules in evaluation order.
func (r RulesRepo) List(ctx context.Context) ([]Rule, error) {
	rows, err := r.s.db.Query(ctx, "SELECT "+ruleCols+" FROM rules ORDER BY position, id")
	if err != nil {
		return nil, err
	}
	out := make([]Rule, 0, len(rows.Values))
	for _, row := range rows.Values {
		rule, err := scanRule(row)
		if err != nil {
			return nil, err
		}
		out = append(out, rule)
	}
	return out, nil
}

// Get returns the rule with id, or ErrNotFound.
func (r RulesRepo) Get(ctx context.Context, id int64) (Rule, error) {
	rows, err := r.s.db.Query(ctx, "SELECT "+ruleCols+" FROM rules WHERE id = ?", id)
	if err != nil {
		return Rule{}, err
	}
	if len(rows.Values) == 0 {
		return Rule{}, ErrNotFound
	}
	return scanRule(rows.Values[0])
}

// Create appends rule at the end of the list and returns it with its ID
// and position.
func (r RulesRepo) Create(ctx context.Context, rule Rule) (Rule, error) {
	rows, err := r.s.db.Query(ctx, "SELECT coalesce(max(position), 0) FROM rules")
	if err != nil {
		return Rule{}, err
	}
	rule.Position = int(i64(rows.Values[0][0])) + 1
	st := ruleInsert(rule)
	res, err := r.s.db.Execute(ctx, st.SQL, st.Args...)
	if err != nil {
		return Rule{}, err
	}
	rule.ID = res.LastInsertID
	return rule, nil
}

// Update saves rule's fields, but not its position (see Reorder).
func (r RulesRepo) Update(ctx context.Context, rule Rule) error {
	res, err := r.s.db.Execute(ctx, `UPDATE rules SET name = ?, enabled = ?, action = ?, conditions = ?,
		dir_override = ?, template_override = ? WHERE id = ?`,
		rule.Name, rule.Enabled, string(rule.Action), conditionsJSON(rule.Conditions),
		nullable(rule.DirOverride), nullable(rule.TemplateOverride), rule.ID)
	if err != nil {
		return err
	}
	if res.Changes == 0 {
		return ErrNotFound
	}
	return nil
}

// SetEnabled turns a rule on or off.
func (r RulesRepo) SetEnabled(ctx context.Context, id int64, enabled bool) error {
	res, err := r.s.db.Execute(ctx, "UPDATE rules SET enabled = ? WHERE id = ?", enabled, id)
	if err != nil {
		return err
	}
	if res.Changes == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes a rule.
func (r RulesRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.s.db.Execute(ctx, "DELETE FROM rules WHERE id = ?", id)
	return err
}

// Reorder sets the order of the rules to ids, which must list every rule
// exactly once, in one transaction.
func (r RulesRepo) Reorder(ctx context.Context, ids []int64) error {
	current, err := r.List(ctx)
	if err != nil {
		return err
	}
	if len(ids) != len(current) {
		return fmt.Errorf("reorder: got %d ids for %d rules", len(ids), len(current))
	}
	known := make(map[int64]bool, len(current))
	for _, rule := range current {
		known[rule.ID] = true
	}
	stmts := make([]sqlite.Statement, 0, len(ids))
	for i, id := range ids {
		if !known[id] {
			return fmt.Errorf("reorder: unknown or repeated rule %d", id)
		}
		delete(known, id)
		stmts = append(stmts, sqlite.Statement{SQL: "UPDATE rules SET position = ? WHERE id = ?", Args: []any{i + 1, id}})
	}
	_, err = r.s.db.Transaction(ctx, stmts)
	return err
}

// Move moves the rule with id by delta places (negative: up), clamped to
// the list.
func (r RulesRepo) Move(ctx context.Context, id int64, delta int) error {
	current, err := r.List(ctx)
	if err != nil {
		return err
	}
	ids := make([]int64, len(current))
	from := -1
	for i, rule := range current {
		ids[i] = rule.ID
		if rule.ID == id {
			from = i
		}
	}
	if from < 0 {
		return ErrNotFound
	}
	to := max(0, min(len(ids)-1, from+delta))
	if to == from {
		return nil
	}
	moved := ids[from]
	ids = append(ids[:from], ids[from+1:]...)
	ids = append(ids[:to], append([]int64{moved}, ids[to:]...)...)
	return r.Reorder(ctx, ids)
}
