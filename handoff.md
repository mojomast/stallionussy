# Handoff

## Completed: 13 Bug Fixes in Market + Tournament Systems (marketussy, tournussy)

## What Was Done
Fixed all 13 remaining bugs across `internal/marketussy/marketussy.go` and `internal/tournussy/tournussy.go`, updated tests in both packages, and ensured all three test suites pass.

### Bug Fixes Summary

**marketussy.go:**
1. **BUG 1** - Burn amount floor: `max(int64(1), listing.Price*2/100)` so prices < 50 still burn at least 1.
2. **BUG 2** - Stud listings persist: Added `TimesUsed`/`MaxUses` fields, listings only deactivate when `MaxUses > 0 && TimesUsed >= MaxUses`.
3. **BUG 3** - Buyer balance validation: Added `buyerBalance int64` param, check `buyerBalance < listing.Price`, compute `SellerPayout`.
4. **BUG 4** - Duplicate horse listing check: Iterates existing listings for same active HorseID before creating.
5. **BUG 5** - ELO floor: Loser ELO can't go below 100.
6. **BUG 6** - GetListing returns copy: Returns `clone := *listing; &clone` to prevent mutation.

**tournussy.go:**
7. **BUG 7** - RecordResult O(n) prepend: Now appends chronologically, GetHorseHistory/GetRecentResults reverse for newest-first.
8. **BUG 8** - Prize pool: PrizePool starts at 0, grows from `RegisterHorse` entry fees. `RecordRoundResults` returns `(*PrizeDistribution, error)` with 50/30/20 split when tournament finishes.
9. **BUG 9** - Min horses: `RunNextRound` rejects with `< 2` horses registered.
10. **BUG 10** - Dynamic win counting: Loops over `trackWins` map instead of hardcoded track types.
11. **BUG 11** - `the_yogurt_sees` description updated to reflect actual behavior (Hauntedussy race completion) with limitation comment.
12. **BUG 12** - `derulo_moment` description updated to reflect actual behavior (TMP BB horse finishing race) with limitation comment.
13. **BUG 13** - Removed duplicate "Stampede" from `tournamentNouns`.

### Test Updates
- `marketussy_test.go`: All `PurchaseBreeding` calls updated to 3-arg signature (added `buyerBalance`). Replaced `TestPurchaseBreeding_DeactivatesListing` with `TestPurchaseBreeding_ListingPersistsAfterPurchase` and `TestPurchaseBreeding_DeactivatesAfterMaxUses`.
- `tournussy_test.go`: All `RecordRoundResults` calls updated to handle `(*PrizeDistribution, error)` return type.

## Verification
- `go build ./...` — passes
- `go test ./internal/marketussy/ -v` — 44/44 PASS
- `go test ./internal/tournussy/ -v` — 75/75 PASS
- `go test ./internal/server/ -v` — 55/55 PASS

## Files Modified
- `internal/marketussy/marketussy.go` — BUGs 1-6
- `internal/marketussy/marketussy_test.go` — Updated signatures, new tests
- `internal/tournussy/tournussy.go` — BUGs 7-13
- `internal/tournussy/tournussy_test.go` — Updated return value handling
- `internal/models/models.go` — Added `TimesUsed`, `MaxUses` to `StudListing`; `SellerPayout` to `MarketTransaction`
- `internal/server/server.go` — Updated `PurchaseBreeding` call site
- `cmd/stallionussy/main.go` — Updated `RecordRoundResults` return handling

## Previous Work (by earlier Ralphs)
- Fixed 10 bugs in `internal/fightussy/fightussy.go` (fight engine)
- Fixed 8 bugs in `internal/racussy/racussy.go` (racing engine)

## Next Task: Check devplan.md for next pending task (devplan.md not found — all known bugs fixed)
