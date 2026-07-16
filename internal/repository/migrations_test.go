package repository

import (
	"strings"
	"testing"
)

// TestSchemaContainsPersistenceFixColumns guards the schema additions for
// findings H-7, H-8, H-9, H-10, H-4 and the C-9 balance floors. These columns
// back fields that previously existed only in memory and silently reset on
// every restart (breeding cooldowns, stud use limits, Hold'em hand state...).
func TestSchemaContainsPersistenceFixColumns(t *testing.T) {
	required := map[string][]string{
		"H-7 horse breeding/champion state": {
			"ALTER TABLE horses ADD COLUMN IF NOT EXISTS retired_champion",
			"ALTER TABLE horses ADD COLUMN IF NOT EXISTS last_bred_at",
		},
		"H-8 stud listing use limits": {
			"ALTER TABLE stud_listings ADD COLUMN IF NOT EXISTS times_used",
			"ALTER TABLE stud_listings ADD COLUMN IF NOT EXISTS max_uses",
		},
		"H-9 market transaction seller payout": {
			"ALTER TABLE market_transactions ADD COLUMN IF NOT EXISTS seller_payout",
		},
		"H-10 hold'em poker state": {
			"ALTER TABLE poker_tables ADD COLUMN IF NOT EXISTS game_type",
			"ALTER TABLE poker_tables ADD COLUMN IF NOT EXISTS community_cards",
			"ALTER TABLE poker_tables ADD COLUMN IF NOT EXISTS small_blind",
			"ALTER TABLE poker_tables ADD COLUMN IF NOT EXISTS big_blind",
			"ALTER TABLE poker_tables ADD COLUMN IF NOT EXISTS current_bet",
			"ALTER TABLE poker_tables ADD COLUMN IF NOT EXISTS dealer_seat",
			"ALTER TABLE poker_tables ADD COLUMN IF NOT EXISTS action_seat",
			"ALTER TABLE poker_tables ADD COLUMN IF NOT EXISTS min_raise",
			"ALTER TABLE poker_tables ADD COLUMN IF NOT EXISTS side_pots",
			"ALTER TABLE poker_tables ADD COLUMN IF NOT EXISTS hand_round",
			"ALTER TABLE poker_tables ADD COLUMN IF NOT EXISTS action_deadline",
		},
		"H-4 tournament organizer": {
			"ALTER TABLE tournaments ADD COLUMN IF NOT EXISTS created_by",
		},
		"C-9 balance floors": {
			"CHECK (cummies >= 0)",
			"CHECK (casino_chips >= 0)",
			"UPDATE stables SET cummies = 0 WHERE cummies < 0",
			"UPDATE stables SET casino_chips = 0 WHERE casino_chips < 0",
		},
	}

	for finding, stmts := range required {
		for _, stmt := range stmts {
			if !strings.Contains(schemaSQL, stmt) {
				t.Errorf("%s: schema is missing %q", finding, stmt)
			}
		}
	}
}

// TestSchemaRetrofitsEveryLegacyColumn guards the schema-convergence block.
// CREATE TABLE IF NOT EXISTS is a no-op on databases where the table already
// exists, so every column of every legacy table must also appear as an
// ALTER TABLE ... ADD COLUMN IF NOT EXISTS retrofit or old production
// databases never receive it (this took the site down when the C-9 balance
// floor referenced stables.casino_chips, which production predated).
func TestSchemaRetrofitsEveryLegacyColumn(t *testing.T) {
	// Tables introduced by this improvement pass cannot predate their own
	// CREATE statement, so they need no retrofit.
	newTables := map[string]bool{
		"casino_jackpot": true,
		"sessions":       true,
		"app_config":     true,
		"challenges":     true,
		"betting_pools":  true,
		"rivalries":      true,
	}

	constraintKeywords := []string{"PRIMARY", "UNIQUE", "CHECK", "FOREIGN", "CONSTRAINT"}

	// Anchor to line starts so prose mentioning CREATE TABLE in SQL comments
	// is not parsed as a statement.
	const createStmt = "\nCREATE TABLE IF NOT EXISTS "
	rest := schemaSQL
	checked := 0
	for {
		idx := strings.Index(rest, createStmt)
		if idx < 0 {
			break
		}
		rest = rest[idx+len(createStmt):]
		open := strings.Index(rest, "(")
		table := strings.TrimSpace(rest[:open])
		end := strings.Index(rest, ");")
		body := rest[open+1 : end]
		rest = rest[end:]

		if newTables[table] {
			continue
		}

		// Columns named in a table-level PRIMARY KEY (...) are part of the
		// table's identity and cannot be retrofitted.
		compositePK := map[string]bool{}
		if pk := strings.Index(body, "PRIMARY KEY ("); pk >= 0 {
			inner := body[pk+len("PRIMARY KEY ("):]
			inner = inner[:strings.Index(inner, ")")]
			for _, col := range strings.Split(inner, ",") {
				compositePK[strings.TrimSpace(col)] = true
			}
		}

		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ","))
			if line == "" || strings.HasPrefix(line, "--") {
				continue
			}
			fields := strings.Fields(line)
			col := fields[0]
			isConstraint := false
			for _, kw := range constraintKeywords {
				if col == kw {
					isConstraint = true
				}
			}
			if isConstraint || compositePK[col] || strings.Contains(line, "PRIMARY KEY") {
				continue
			}
			retrofit := "ALTER TABLE IF EXISTS " + table + " ADD COLUMN IF NOT EXISTS " + col + " "
			if !strings.Contains(schemaRetrofitSQL, retrofit) {
				t.Errorf("legacy table %s: column %s has no ADD COLUMN IF NOT EXISTS retrofit", table, col)
			}
			checked++
		}
	}

	if checked < 200 {
		t.Fatalf("parsed only %d legacy columns from schemaSQL; the parser is likely broken", checked)
	}
}
