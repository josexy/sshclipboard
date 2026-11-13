package store

import (
	"encoding/json"
	"slices"

	"github.com/egoist/mygo/plugins/sqlite"
)

// MB is a megabyte as the app counts sizes: 1024 × 1024 bytes, as
// Windows Explorer shows them.
const MB = 1024 * 1024

// DefaultNaming returns the first-run filename templates.
func DefaultNaming() []Naming {
	return []Naming{
		{Kind: KindImage, Template: "screenshot_{datetime}.{ext}"},
		{Kind: KindFile, Template: "{name}.{ext}"},
		{Kind: KindText, Template: "clip_{datetime}.{ext}"},
	}
}

// DefaultRules returns the first-run rules, in order.
func DefaultRules() []Rule {
	minSize := int64(100 * MB)
	return []Rule{
		{Position: 1, Name: "Skip text", Enabled: true, Action: ActionSkip,
			Conditions: Conditions{Kinds: []Kind{KindText}}},
		{Position: 2, Name: "Skip large files", Enabled: true, Action: ActionSkip,
			Conditions: Conditions{Kinds: []Kind{KindFile}, MinSize: &minSize}},
	}
}

// seedStatements fill a new database with the first-run defaults: the
// settings, the naming templates, and the default rules. There are no
// profiles.
func seedStatements() []sqlite.Statement {
	stmts, err := settingsStatements(DefaultSettings())
	if err != nil {
		panic(err) // DefaultSettings always encodes
	}
	for _, n := range DefaultNaming() {
		stmts = append(stmts, namingUpsert(n))
	}
	for _, r := range DefaultRules() {
		stmts = append(stmts, ruleInsert(r))
	}
	return stmts
}

func conditionsJSON(c Conditions) string {
	b, err := json.Marshal(c)
	if err != nil {
		panic(err) // Conditions always encodes
	}
	return string(b)
}

func sortStrings(s []string) { slices.Sort(s) }
