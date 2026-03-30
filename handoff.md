# Handoff

## Completed: Texas Hold'em Poker Engine (Backend)

## What Was Done
Upgraded the casino poker system from basic 5-card draw only to support both 5-card draw AND full Texas Hold'em alongside each other. All existing draw functionality preserved with full backward compatibility.

### Models Updated (`internal/models/casino.go`)
- **PokerSeat**: Added `ChipStack`, `CurrentBet`, `Folded`, `AllIn`, `LastAction` fields
- **PokerTable**: Added `GameType`, `CommunityCards`, `SmallBlind`, `BigBlind`, `CurrentBet`, `DealerSeat`, `ActionSeat`, `MinRaise`, `SidePots`, `Round`, `ActionDeadline` fields
- **New type**: `SidePot` struct for side pot tracking
- **New constants**: `PokerTablePreFlop`, `PokerTableFlop`, `PokerTableTurn`, `PokerTableRiver`, `PokerTableShowdown`, `PokerGameDraw`, `PokerGameHoldem`

### Game Engine (`internal/server/casino.go`)
- **`startHoldemHand`**: Posts blinds, deals 2 hole cards, sets action seat
- **`handleHoldemAction`**: HTTP handler for check/call/raise/fold/allin actions with timeout enforcement
- **`holdemValidateAction`**: Validates action legality (check when no bet, call amount, min raise, etc.)
- **`holdemApplyAction`**: Applies action to table state with fun log messages
- **`advanceHoldemRound`**: Progresses preflop->flop->turn->river->showdown, dealing community cards
- **`settleHoldemTable`**: Evaluates best 5-of-7 hands, distributes pots including side pots
- **`settleHoldemSingleWinner`**: Awards pot when everyone folds
- **`holdemCashOutPlayers`**: Returns chip stacks to player stables at settlement
- **`holdemBuildPots`**: Builds main pot + side pots for all-in scenarios
- **`evaluateBestHoldemHand`**: Tries all C(7,5)=21 combinations to find best 5-card hand
- **`handKickers`**: Kicker-based tie breaking for all hand types
- **`combinations`**: Generic C(n,k) combinatorial generator
- **Timeout mechanism**: 60-second ActionDeadline per action, auto-fold on expiry

### Routes Updated (`internal/server/server.go`)
- `POST /api/casino/poker/{id}/action` - New holdem action endpoint
- `POST /api/casino/poker` - Now accepts `gameType` param ("draw" or "holdem", default "holdem")
- All existing draw routes unchanged

### Key Design Decisions
- Hold'em uses per-player chip stacks (set to buy-in at table join)
- Blinds: SmallBlind = buyIn/20, BigBlind = buyIn/10
- Max 6 players for holdem (vs 4 for draw)
- Side pots built from contribution levels when players go all-in
- Settlement cashes out all remaining chip stacks to player stables
- Draw tables still use the legacy flat pot/payout system

### Additional Fixes
- Fixed pre-existing build error: `slotSymbolPool` -> `slotWeightedPool` reference
- Removed old stub implementations of `startHoldemHand` and `handleHoldemAction`

## Files Modified
- `internal/models/casino.go` - New fields on PokerSeat/PokerTable, SidePot type, Hold'em constants
- `internal/server/casino.go` - Full Hold'em engine (~850 lines of new code)
- `internal/server/server.go` - New route for `/api/casino/poker/{id}/action`

## Verification
- `go build ./...` - passes
- `go vet ./...` - passes
- `go test -race ./internal/server/` - 61/61 PASS
- `go test -race ./...` - all project tests pass

## Next Task: Check devplan.md for next pending task
