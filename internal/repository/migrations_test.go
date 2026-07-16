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
