# FABLE FINDINGS

Codebase archaeology of **STALLIONRUN / StallionUSSY** — a comedy horse breeding/racing/genetics/casino simulator in Go + PostgreSQL with a single-file terminal-themed SPA. This document is the sole authoritative reference for a later bug-fixing pass. Every finding cites exact `file:line`. Line numbers are as of the audited tree (branch `master`, HEAD `5b46250`).

> Read-only audit. No code was modified.
>
> **Fix pass (branch `fable/full-improvement-pass`, 2026-07-16):** all CRITICAL and HIGH findings are fixed and annotated inline with `[FIXED: <sha>]`. Two related bugs discovered during fixing were folded into their parent findings: tournament registration double-counted every entry fee into the prize pool (fixed with C-2), and the betting house cut evaporated instead of going to the house (now banks into the House of USSY treasury, which funds the C-3 purses).
>
> **Phase 3 (same branch, 2026-07-16):** all MEDIUM findings are resolved and annotated inline (`[FIXED: <sha>]`, `[ALREADY-FIXED]`, or `[WONTFIX: reason]`), and a wiring & balance pass covered every gameplay action end-to-end — see the "Phase 3 — Wiring & Balance Pass" section at the bottom. LOW findings remain open.
>
> **Phase 4 (same branch, 2026-07-16):** full session & state persistence plus offline mode — see the "Phase 4 — Persistence & Offline Mode" section at the bottom. Nothing in the In-Memory State Inventory is lost on restart anymore; the server also runs fully self-contained on embedded SQLite (`make offline`).

## Severity Summary

| Severity | Count | Meaning |
|---|---|---|
| CRITICAL | 9 | Exploits, money duplication, data corruption, broken core loops |
| HIGH | 11 | Clear logic bugs affecting gameplay outcomes / persistence loss |
| MEDIUM | 14 | Balance issues, unclamped values, placeholder tuning, data races |
| LOW | 12 | Code smells, dead code, minor issues |

**Most urgent (fix first):**
1. **C-1** Poker hold'em action is an unlocked read-modify-write → concurrent requests double-settle the pot / double-cashout (chip printing).
2. **C-2** Tournament pays out its prize pool twice (per-round purses *and* final distribution) → ~2× cummies created from entry fees.
3. **C-3** Quick races + challenge races mint their purse from nothing (no counterparty is debited).
4. **C-4** DB-persisted trade acceptance always rolls back (`owner_id` column holds user IDs but the atomic query filters/writes stable IDs) → money "un-moves" across restart = duplication.
5. **C-5** Breeding cooldown is bypassable in the running server due to pointer/value divergence (`LastBredAt` written to an orphaned copy).

The single largest structural risk is that **in-memory maps are the source of truth and the DB is best-effort write-through with no `CHECK (cummies >= 0)` and only three atomic transactions (two of which are broken).** There is effectively no double-spend protection at the DB layer.

---

## Architecture Overview

### Packages

| Package | Responsibility |
|---|---|
| `internal/models` | All data types. `Horse`, `Stable` (holds `Cummies`/`CasinoChips`), `Genome`, `Race`, `StudListing`, `Tournament`, `Auction`, `Alliance`, `HorseFight`, `PokerTable`/`SlotSpin` (casino.go), etc. |
| `internal/genussy` | Genetics: genome generation, Punnett-cross `Breed`, mutation, `CalcFitnessCeiling`, 12 canonical legendary horses. |
| `internal/racussy` | Physics tick race simulator (`SimulateRaceWithWeather`), trait/weather/event application, narrative generation. |
| `internal/trainussy` | Training XP/fitness/fatigue, injury rolls, trait pool + assignment, aging, retirement, seasonal events. |
| `internal/fightussy` | Gladiatorial combat sim (`SimulateFight`): arenas, maces, rage/morale, KO/fatality. |
| `internal/marketussy` | In-memory stud marketplace, Sappho score, ELO update, 2% burn. |
| `internal/stableussy` | In-memory `StableManager`: stables + global horse registry, `TransferCummies`, `MoveHorse`, leaderboard. |
| `internal/tournussy` | `RaceHistory`, weather roll, `TournamentManager`, achievement definitions + `CheckAchievements`. |
| `internal/pedigreussy` | Pedigree tree, inbreeding coefficient, dynasty score, `TradeManager`. |
| `internal/authussy` | bcrypt + JWT auth, middleware, register/login/me/refresh handlers. |
| `internal/commussy` | WebSocket hub: chat, whispers, live race telemetry broadcasts. |
| `internal/nameussy` | Random horse/stable name generator. |
| `internal/server` | The monolith. `server.go` (10,364 lines) wires all managers + ~130 HTTP handlers; `casino.go` (2,461 lines) is slots + poker + chip exchange + departed-horse resurrection. |
| `internal/repository` + `internal/repository/postgres` | Repository interfaces and their sole Postgres implementations; `migrations.go` runs the schema on every startup; `atomictx.go` holds the only DB transactions. |
| `cmd/stallionussy/main.go` | `serve` mode (HTTP, requires DB) and `cli` mode (interactive, in-memory fallback). |

### Data flow

`main.go serve` → `connectDB` (runs migrations every start) → `server.NewServer(db)` wires repos + managers → `loadFromDB()` hydrates in-memory managers from Postgres → HTTP + WS serving. **All gameplay mutates in-memory manager state first; each handler then "write-through" persists** via `persistX` helpers that only log on error (server.go:6006-6122). The DB is a snapshot, not a transactional ledger.

### Persistence: what is a table vs RAM-only

**Postgres tables** (migrations.go): `users`, `stables`, `horses`, `race_results`, `stud_listings`, `tournaments`, `trade_offers`, `achievements`, `training_sessions`, `player_progress`, `seasons`, `poker_tables`, `slot_spins`, `departed_horses`, `market_transactions`, `auctions`, `race_replays`, `alliances`, `alliance_members`, `horse_fights`, `glue_factory`, `breeding_stallions`; Phase 2/3 added `casino_jackpot`; Phase 4 added `sessions`, `challenges`, `betting_pools`, `rivalries`, `app_config`. Every table also exists in the SQLite schema (`migrations_sqlite.go`) for offline mode.

**RAM-only (lost on restart)** — *(updated for Phase 4)* only genuinely ephemeral state remains RAM-only: the race-replay share cache (DB `race_replays` fallback exists), live WS connections, per-entity mutexes, and in-flight `Race`/`RaceEntry`/`TickEvent` sim objects (only derived `race_results` + `race_replays` persist, by design). Challenges, betting pools (with escrowed bets), rivalries, the jackpot pool, pending fights, market transaction history, and per-horse training history are all persisted and rehydrated now.

---

## Findings by Severity

### CRITICAL

#### C-1 [FIXED: fcb666e] — Poker action handler is an unlocked read-modify-write → pot double-settlement / double-cashout
`getPokerTable` returns a **copy** of the table (`internal/server/casino.go:996-1010`, `clone := *table`), the caller mutates it, and `savePokerTable` (`casino.go:1012-1025`) writes it back — with `pokerMu` released in between. Two concurrent `POST /api/casino/poker/{id}/action` (or a `draw` + `action`) can each observe a not-yet-settled table, both run settlement/cashout (`settleHoldemTable`/`holdemCashOutPlayers`, `casino.go:1187+`, `1927-1950`), and pay the pot / return stacks twice. In DB mode `getPokerTable` reads a fresh row each call, so there is **no shared in-memory guard at all**. Chip-printing race.
Fix: hold `pokerMu` (or a per-table lock) across the entire read→mutate→settle→save sequence, or make settlement idempotent (guard on a terminal status flag checked+set under lock). Note the clone is also *shallow* (`Seats`/`Log` slices are aliased), so mutations leak into shared state.

#### C-2 [FIXED: da00a63] — Tournament prize pool is paid out twice (money creation)
Each round distributes a per-round purse of `PrizePool/Rounds` to finishers via `applyPostRaceEffects` (`server.go:3126` `earnings = int64(float64(race.Purse) * share)`, credited at `server.go:3159`). `race.Purse` is `roundPurse = t.PrizePool / int64(t.Rounds)` (`internal/tournussy/tournussy.go:636`). Then on the final round `distributeTournamentPrizes` pays the **entire** `PrizePool` again (`server.go:3212-3248`). Net ≈ `PrizePool` (per-round purses summed) + `PrizePool` (final) ≈ **2× the collected entry fees**, created from nothing.
Fix: choose one distribution model — either per-round purses that sum to the pool, or a single end-of-tournament distribution — not both.

#### C-3 [FIXED: 9a7f1b5] — Quick races and challenge races mint their purse from nothing
`handleQuickRace` passes `quickRacePurse` (=200, `server.go:5576`) to `runRace` with **no debit from anyone** (`server.go:1379-1381`, `1422-1426`); `runRace` credits finishers via `addEarningsToStable` (`server.go:1610`). Bot challenges use `challengeBasePurse`=250 (`server.go:5577`) and accepted challenges `basePurse`=500 (`server.go:4729`) the same way. Contrast `handleCreateRace`, which *does* deduct the purse (`server.go:1302`). These are pure faucets bounded only by the daily race cap.
Fix: fund quick/challenge purses from a real source (entry fee, house sink budget) or set them to 0 and rely on betting.

#### C-4 [FIXED: 867a5be] — DB trade acceptance always rolls back → money duplication across restart
`AcceptTradeAtomically` moves the horse with `UPDATE horses SET owner_id = $1 WHERE id = $2 AND owner_id = $3` binding **stable IDs** (`internal/repository/postgres/atomictx.go:64-68`), but `horses.owner_id` stores **user IDs** (proven by hydration `ListHorsesByStable(ctx, stable.OwnerID)` at `server.go:6145` and auction settlement using `buyerStable.OwnerID` at `server.go:7562`). The horse UPDATE matches 0 rows, the guard at `atomictx.go:73-75` fires, and the **entire transaction (including the cummies transfer) rolls back**. The caller only logs it (`server.go:3456`) while the in-memory trade already completed. After a restart the DB shows the trade still "Pending", the buyer's cummies restored, and the horse unmoved — the buyer can accept again. Same column-semantics defect in `HorseRepo.MoveHorse` (`postgres/horses.go:339-340`), the non-DB fallback path.
Fix: make the atomic query use the user-ID convention (resolve stable→owner) or add and use a real `stable_id` column consistently.

#### C-5 [FIXED: f0a15c1] — Breeding cooldown bypassable via pointer/value divergence
`handleBreed` calls `AddHorseToStable(req.StableID, foal)` (`server.go:1177`) which does `stable.Horses = append(stable.Horses, *horse)` (`internal/stableussy/stableussy.go:163`), **reallocating** the slice and re-registering all pointers into the new array. The `sire`/`mare` pointers obtained earlier (`server.go:1127`, `1132`) now point into the *old* array. Cooldown is then written to those orphaned pointers: `sire.LastBredAt = time.Now()` (`server.go:1205-1206`). `syncHorseToStable` only forwards ELO (`server.go:1809-1812` → `stableussy.UpdateHorseStats`, which copies Wins/Losses/Races/ELO only, `stableussy.go:264-267`), so `LastBredAt` never reaches the live registry horse. The next breed's cooldown check `!sire.LastBredAt.IsZero()` (`server.go:1140`) sees zero → **cooldown skipped**. `persistHorse` writes the stale-but-correct pointer to DB, so behavior differs pre/post restart. Same pattern in `handleBuyListing` (`server.go:2579` then `2583-2584`) and `handleBreedWithStallion` (`server.go:10010` then `10014-10015`).
Fix: mutate through the live registry pointer (re-`GetHorse` after `AddHorseToStable`), or set cooldown *before* adding the foal, or have `syncHorseToStable` propagate all mutable fields.

#### C-6 [FIXED: cfd8a91] — Buying your own listing / self-trade drains only the burn
`handleBuyListing` never checks the buyer stable differs from the seller stable (`server.go:2432-2649`). A user who owns the listed stud pays `TransferCummies(buyer→seller)` to *themselves* (net cost = 2% burn) and still receives a foal. `PurchaseBreeding` rejects `buyerID == listing.OwnerID` (`marketussy.go:183`) but the server resolves the seller stable separately and can be the same stable under a different code path when a user owns multiple stables. Likewise `handleCreateTrade` has no `fromStable != toStable` guard (`server.go:3340-3407`).
Fix: reject self-purchase/self-trade at the handler using stable identity, not just owner ID.

#### C-7 [FIXED: cfd8a91] — Season-end soft-reset & other handlers read/write `stable.Horses` without the stable lock (data race + possible panic)
`handleEndSeason` ranges and mutates `stable.Horses[i].ELO` (`server.go:3988-3998`) and `handleGetLeaderboard` ranges every `stable.Horses` (`server.go:3790+`) **without** holding `stableMu`, while `AddHorseToStable`/breeding `append` (reallocate) the same slice concurrently under `stableMu`. Ranging a slice that another goroutine reallocates is a genuine data race and can panic. `maybeGrantDailyCasinoChips` mutates `stable.CasinoChips` under `progressMu` not `stableMu` (`casino.go:949`), racing slot/exchange balance ops.
Fix: standardize on `stableMu` for all reads/writes of `stable.Horses`/`Cummies`/`CasinoChips`, or snapshot under lock.

#### C-8 [FIXED: 9a7f1b5] — `handleCreateRace` deducts the purse then aborts without refund
Purse is deducted under lock (`server.go:1302`), but later failure paths `return` without refunding: invalid track (`server.go:1312`), unresolved horses (`~1318`), and hitting the daily race cap in `consumeDailyRace` (`~1325`). A user who supplies a bad track/horse or is at their daily cap permanently loses the deducted cummies.
Fix: validate everything (including daily-cap consumption) *before* debiting, or refund on every error path.

#### C-9 [FIXED: 867a5be] — No DB balance floor; absolute-value `UpdateStable` is the main money write
`stables.cummies BIGINT NOT NULL DEFAULT 0` has **no `CHECK (cummies >= 0)`** (`migrations.go:38`), and the primary persistence primitive writes the absolute in-memory value (`UPDATE stables SET ... cummies = $4 ...`, `postgres/stables.go:140-143`). Every debit relies on an in-process check then blasts the value to the DB; a single missed check, a lost update between load and persist, or two processes persist a negative/duplicated balance. Only the three `atomictx.go` methods use real transactions, and C-4/H-2 show two of the three are broken.
Fix: add `CHECK (cummies >= 0)` and `CHECK (casino_chips >= 0)`; route money movement through transactional `WHERE cummies >= amount` updates with RowsAffected checks.

---

### HIGH

#### H-1 [FIXED: 867a5be] — Auction settlement credits the seller but never debits the buyer in the same transaction
`SettleAuctionAtomically` does `UPDATE stables SET cummies = cummies + $1` for the seller (`atomictx.go:131-140`) and updates the horse owner (`atomictx.go:144-147`), but the buyer's escrow happened earlier in memory only (`bidderStable.Cummies -= req.Amount`, `server.go:7350`) with separate non-transactional persists. A crash between escrow-persist and settlement mints `sellerPayout` cummies from nothing. No `RowsAffected` checks on any of its three UPDATEs.
Fix: include the buyer debit inside the same transaction; add RowsAffected guards.

#### H-2 [FIXED: 867a5be] — Atomic trade transaction lacks guards on status/seller updates
In `AcceptTradeAtomically` the status UPDATE (`atomictx.go:30-35`) has no `WHERE status='Pending'` and no RowsAffected check, and the seller credit (`atomictx.go:54-61`) has no RowsAffected check. A non-existent/already-accepted trade or a bad seller stable still moves the buyer's money.
Fix: add `WHERE status='Pending'` and RowsAffected checks on all three statements.

#### H-3 [FIXED: da00a63] — Tournament round counter advances before results are confirmed
`RunNextRound` increments `t.CurrentRound++` (`tournussy.go:632`) *before* the race is simulated (`server.go:2977`) or recorded (`RecordRoundResults`, `server.go:2987`, whose error is only logged). Both `RunNextRound` (`tournussy.go:626`) and `RecordRoundResults` (`tournussy.go:703`) key "Finished" off `CurrentRound >= Rounds`, so a round that fails to record still consumed a round and the tournament can finish with fewer recorded results than rounds. (Matches head-start hint — **confirmed**.)
Fix: increment the round only after results are successfully recorded, or roll back on record failure.

#### H-4 [FIXED: da00a63] — `handleTournamentRace` has zero authorization
`server.go:2922-2926` explicitly notes "any authenticated user can trigger tournament races" and performs no ownership/organizer check. Any caller can advance any tournament's rounds and trigger prize distribution (which is double-paying, see C-2).
Fix: restrict to the tournament creator/admin.

#### H-5 [FIXED: 9a7f1b5] — `runBotChallenge` pays a 2× pot from an unfunded bot
Only the challenger's wager is escrowed (`server.go:4505`), but a win pays `payout := (wager*2) - 5%` (`server.go:4545-4546`). The bot never funds its half, so wins net ≈ +0.9·wager from nothing (a loss burns the wager). Asymmetric faucet.
Fix: fund the bot side from a house budget or make bot challenges non-monetary.

#### H-6 [FIXED: cfd8a91] — Betting escrow and pending-fight entry fees are lost on restart (no refund)
`bettingPools` is RAM-only (`server.go:100`); `placeBet` deducts cummies at `server.go:6461`. `pendingFights` is RAM-only (`server.go:122`); create/join deduct entry fees at `server.go:9193`/`9331`. On restart all open pools and pending fights vanish and the deducted cummies are never refunded. A fight that never gets a joiner also has no refund path even without a restart.
Fix: persist these (or refund on shutdown / on expiry sweeps).

#### H-7 [FIXED: 867a5be] — Missing DB columns silently drop `LastBredAt` and `RetiredChampion` on restart
`horseCols` and the schema omit `retired_champion` and `last_bred_at` (`postgres/horses.go:28-33`; `migrations.go:52-81`) even though `Horse.RetiredChampion`/`LastBredAt` exist (`models.go:134`,`138`). After a restart, breeding cooldowns reset to zero (compounding C-5) and champion flags (which drive the 5% foal bonus in `genussy.Breed`, `genussy.go:252-260`) are lost.
Fix: add the columns and include them in scan/insert/update.

#### H-8 [FIXED: 867a5be] — Stud `TimesUsed`/`MaxUses` never persisted → use-limits reset on restart
`stud_listings` has no `times_used`/`max_uses` columns (`migrations.go:109-122`) and `MarketRepo` never writes them (`postgres/market.go:27-31`,`118-122`), while `PurchaseBreeding` relies on them to deactivate a listing (`marketussy.go:213-216`). After restart, a maxed-out stud is active again with `TimesUsed=0`.
Fix: persist both fields.

#### H-9 [FIXED: 867a5be] — `market_transactions.seller_payout` not persisted
Schema and `SaveTransaction` omit `seller_payout` (`migrations.go:287-299`; `postgres/transactions.go:31-45`) though `MarketTransaction.SellerPayout` is set (`models.go:351`). Financial history is incomplete/unauditable.
Fix: add the column and write it.

#### H-10 [FIXED: 867a5be] — PokerTable Hold'em state truncated on every DB round-trip
`UpdatePokerTable` writes only 13 legacy columns (`postgres/casino.go:138-146`) and the schema has none of the Hold'em fields (no `game_type`, `community_cards`, blinds, `dealer_seat`, `action_seat`, `min_raise`, `side_pots`, `round`, `action_deadline`; `migrations.go:228-245`). After a restart an in-progress Hold'em hand loads as a legacy draw table with player chips already deducted — corrupted mid-hand state.
Fix: add columns + scan/write, or refuse to persist mid-hand and refund on shutdown.

#### H-11 [FIXED: 8120ab0] — CLI trade acceptance moves money but never the horse
`cmdAccept` (`cmd/stallionussy/main.go:1263-1293`) calls `AcceptOffer` (only flips status, `pedigreussy.go:479`) then `TransferCummies(offer.ToStableID, offer.FromStableID, offer.Price)` (`main.go:1276`) with **no `MoveHorse` call**. The buyer pays full price and the seller keeps the horse. (The HTTP handler does call `MoveHorse`, `server.go:3445` — CLI diverges.)
Fix: add `sm.MoveHorse(offer.HorseID, offer.FromStableID, offer.ToStableID)` to `cmdAccept`.

---

### MEDIUM

#### M-1 [FIXED: 06832d4] — Casino daily chip grant is a free 400-cummies/day faucet
`casinoDailyChipGrant` = 40 chips/day (`casino.go:23`), granted on casino overview/exchange (`casino.go:949`), cashable at 10 cummies/chip (`casino.go:205`) = 400 free cummies/day/user with zero play. Design faucet — flag for economy balance.
*Resolution:* the grant is now sponsored by the House of USSY treasury at cashout value (400 cummies/grant); a broke house grants nothing and the player's day is not consumed. Bounded by the treasury, which recoups from betting cuts and lost bot wagers.

#### M-2 [FIXED: 06832d4] — Slot jackpot pays at least 1000 chips from a pool seeded at 500 (can exceed contributions)
`slotJackpotMinPayout`=1000 while `slotJackpotSeed`=500 (`casino.go:109`,`112`); the jackpot win path forces `pool = max(pool, 1000)` (`casino.go:653-656`). Immediately after a restart (pool=500) a jackpot mints 1000 from nothing. Also the jackpot pool is RAM-only (see In-Memory Inventory) so all accumulated 2% contributions are lost on restart.
*Resolution:* the min-payout top-up and the post-win reseed are now funded from the house treasury (partial if it's short), and the pool persists in a new `casino_jackpot` table, hydrated on startup.

#### M-3 [WONTFIX: intended stacking] — Slot jackpot line win stacks with the jackpot on the same spin
When the middle row is all `GOLDEN_STALLION`, payline #1 (index 0 = middle row, `casino.go:83`) also scores a 5-of-a-kind (100×wager) in the payline loop (`casino.go:601-620`) *and* the jackpot is added (`casino.go:648-662`); both accumulate into `totalPayout`. Worth confirming this stacking is intended for RTP.
*Resolution (WONTFIX):* stacking a progressive jackpot on top of the natural 5-of-a-kind line pay is standard slot design; the hit probability is (2/60)^5 ≈ 4×10⁻⁸ so RTP impact is negligible, and with M-2 fixed the jackpot portion is house-funded, so the stack can no longer mint.

#### M-4 [FIXED: d285848] — Trait `stamina_boost` uses post-modification fatigue and adds it back fragilely
In racussy the `stamina_boost` effect (`racussy.go:434-442`) computes `adjustedFatigue := fatigue*(2-magnitude)` and does `deltaP += (oldFatigue - adjustedFatigue)`, but `fatigue` here has *already* been multiplied by `fatigue_resist` (×0.5, `racussy.go:379-384`) and `cursed_fatigue` (`racussy.go:388-392`). The interaction is order-dependent and fragile, though the trait **does** fire (contrary to the older REVIEW_FINDINGS claim that it's gated behind an early-exit — that gating is **not present** in current code).
*Resolution:* all fatigue modifiers (fatigue_resist, cursed_fatigue, stamina_boost) now compose multiplicatively in one place before deltaP — arithmetically identical, no longer order-dependent.

#### M-5 [FIXED: 7d98ebc] — Seasonal effects multiply `FitnessCeiling` but the clamp only caps the top, allowing slow ratchet within [x,1.0]
`ApplySeasonalEffect` multiplies `FitnessCeiling` by 1.02–1.05 for several events and clamps to 1.0 (`trainussy.go:1556-1560`, applied in each case). The clamp **is** present (contrary to the head-start hint about "no clamping") so the ceiling can't exceed 1.0 — but repeated favorable events still ratchet ceilings toward 1.0 for many horses, flattening genetic variance over a long-lived server. Balance concern, not an overflow.
*Resolution:* seasonal ceiling boosts now scale with remaining headroom (`gain = pct × (1 − ceiling)`), so gains shrink asymptotically and the ceiling approaches but never reaches 1.0 — relative genetic ordering is preserved.

#### M-6 [ALREADY-FIXED] — Aging "Youth" growth clamps ceiling but there is no clamp on `int_bonus`/`gen0_boost` stacking beyond 1.0 elsewhere
`AgeHorse` Youth branch multiplies ceiling ×1.02 and clamps to 1.0 (`trainussy.go:1294-1300`) — correctly clamped. All seasonal ceiling boosts also clamp. Verified: no unclamped ceiling path remains in trainussy. (Documented to close the head-start hint: **not present**.) *Resolution:* verification note only — nothing to fix.

#### M-7 [FIXED: f3ff747] — Trade price is never validated (zero/negative accepted)
`handleCreateTrade` (`server.go:3340-3407`) does not validate `req.Price`. On accept, `if offer.Price > 0` (`server.go:3438`) skips the transfer, giving a free horse hand-off for price ≤ 0. Not money creation but an unvalidated economic input (most other endpoints validate: listing `marketussy.go:67`, bid `server.go:7292`, wager `casino.go:512`, exchange `casino.go:173`).
*Resolution:* negative prices are rejected (400); zero remains allowed as an explicit gift (the accept path already skips the transfer). Creation also now validates the horse actually lives in the source stable, and acceptance compensates failed payments/moves (see Phase 3 section).

#### M-8 [FIXED: 06832d4] — Casino chip exchange asymmetry is a large hidden haircut with no UI disclosure
Buy = 25 cummies/chip, cashout = 10 cummies/chip (`casino.go:192`,`205`) → 60% loss round-trip. Intended house edge, but the SPA shows no rate before the click (frontend §3). Also note this asymmetry means chips are a one-way sink except for winnings.
*Resolution:* both rates now render next to the BUY/CASH OUT controls (live from the API) and the overview/exchange responses expose an additive `cashoutRate` field.

#### M-9 [FIXED: f3ff747] — Hold'em auto-fold on timeout acts before verifying the caller
`handleHoldemAction` auto-folds the current action seat when the deadline passed (`casino.go:1506-1509`) *before* the seat/identity check (later at `casino.go:1533`). Any request hitting the endpoint after a timeout forces the waiting player's fold; combined with C-1's lack of locking, timing races corrupt hand state.
*Resolution:* the seat-membership check now runs before any table mutation — only seated players can trigger the timeout auto-fold (C-1's per-table lock already serialized it).

#### M-10 [FIXED: 31e7b86] — DNF horses win ELO in the CLI race path
`RaceEntry.FinishPlace` is 0 until finished (`models.go:301`). The CLI pairwise ELO loop treats lower place as winner: `if entries[i].entry.FinishPlace < entries[j].entry.FinishPlace` (`main.go:1798`); an unfinished horse (place 0) satisfies `0 < anything` and gains ELO against every finisher (`main.go:1810`). (Server path sorts by FinishPlace where DNF horses are assigned real places at `racussy.go:759-770`, so the server is safe — CLI-only bug.)
*Resolution:* place 0 now ranks worst-possible in the CLI pairwise loop (defensive — racussy currently always assigns places). The same fix also repaired a worse latent bug: pairs where the later slice entry finished better were silently skipped, halving CLI ELO movement.

#### M-11 [WONTFIX: matches server semantics] — CLI records every non-winner as a loss
`main.go:1835-1839`: `if FinishPlace == 1 { wins=1 } else { losses=1 }` — 2nd place in an 8-horse field counts as a loss, inflating Losses and skewing win-rate.
*Resolution (WONTFIX):* the HTTP server applies identical winner-take-all semantics (`applyPostRaceEffects`/`runRace`), and the `Wins+Losses == Races` invariant is relied on by leaderboards and achievements — changing only the CLI would diverge from the server; changing both is a product decision, not a bug fix.

#### M-12 [FIXED: 7fdb38b] — `player_progress` DB defaults contradict the model comments
Schema defaults `daily_trains_left`/`daily_races_left` to **6/6** (`migrations.go:202-203`) while the model comments say **5/10** (`models.go:637-638`). The runtime reset logic sets its own values, but a freshly-inserted row from a non-standard path uses 6/6.
*Resolution:* the runtime constants (`defaultDailyTrains/Races`) are 6/6, matching the DB — the model comments were the outlier and now document the real values. No gameplay change.

#### M-13 [FIXED: 7fdb38b] — `race_results` allows duplicate rows (double-counted history/earnings)
No unique index on `(race_id, horse_id)` (`migrations.go:86-104`); `RecordResult` is a plain INSERT (`postgres/races.go:66-72`). Re-running result recording double-counts earnings/history.
*Resolution:* migration dedupes legacy rows (keeping the earliest), adds a unique index on `(race_id, horse_id)`, and `RecordResult` uses `ON CONFLICT DO NOTHING`.

#### M-14 [FIXED: 7fdb38b] — No `UNIQUE` on `stables.owner_id`; `GetStableByOwner` returns an arbitrary row
`stables` has no unique constraint on `owner_id` (`migrations.go:34-45`); `GetStableByOwner` uses `QueryRowContext` with no `LIMIT/ORDER BY` (`postgres/stables.go:77-81`). Multi-stable users get nondeterministic resolution, and duplicate-stable creation is unguarded at the schema level. The server only ever uses the first stable (`getStableForUser`, `server.go:1816-1822`).
*Resolution:* `GetStableByOwner` now orders `created_at ASC, id ASC LIMIT 1` — the same oldest-first rule as the in-memory `getStablesForUser` — so DB and RAM agree deterministically. (A UNIQUE constraint was not added: legacy multi-stable owners may exist, and duplicate creation is already guarded at the handler.)

---

### LOW

#### L-1 — Dead code in casino.go
`grantReturnLoreTrait` (`casino.go:2456`), `describeSpin` (`casino.go:2288`), `slotMultiplier`/`containsSymbol` (`casino.go:2254`,`2279`) — legacy 3-reel slot helpers and an abandoned lore-trait grant, never called by the live video-slot path (only by tests).

#### L-2 — Stubbed / future-work comments (not literal TODOs)
Tournament organizer auth "will be added in a future update" (`server.go:2925-2926`); tournament betting window "future async implementation" — pool opens and immediately closes so nobody can actually bet on tournament rounds (`server.go:2964-2967`); race-cache eviction "trim randomly … you'd want an LRU" (`server.go:2096-2105`).

#### L-3 — `-X main.version=docker` ldflag targets a nonexistent variable
Dockerfile sets `-X main.version=docker` (`Dockerfile:17`) but package `main` has no `version` variable — the ldflag is a silent no-op.

#### L-4 — Docker Go toolchain mismatch
`go.mod` declares `go 1.25.0` (`go.mod:3`) but the builder is `golang:1.24-alpine` (`Dockerfile:5`); with `GOTOOLCHAIN=local` the build fails, with `auto` it downloads 1.25 at build time.

#### L-5 — systemd/nginx port contract is fragile
`deploy/stallionussy.service:11-15` relies on `/etc/stallionussy/env` setting `STALLIONUSSY_PORT=4200`; nginx proxies `127.0.0.1:4200` (`deploy/nginx-horse.ussyco.de.conf:19`, just changed from 8080 per the uncommitted git diff). If the env file lacks the port the binary listens on 8080 → 502. `EnvironmentFile` has no `-` prefix, so a missing file blocks unit start.

#### L-6 — Hardcoded DB password committed
`defaultDatabaseURL` embeds `h0rs3ussy420` (`main.go:156`), matching `docker-compose.yml:13`/`32`; Postgres is also host-exposed on `5432:5432` (`docker-compose.yml:16-17`).

#### L-7 — `sql.ErrNoRows` compared with `==` instead of `errors.Is`
Throughout postgres/ (e.g. `users.go:60`, `horses.go:196-199`, `auctions.go:132`). Works today because errors aren't wrapped before the compare, but `scanHorse` wraps JSON errors, so any future wrap of the Scan error breaks not-found detection.

#### L-8 — Inbreeding coefficient is a naive duplicate-count heuristic
`CalcInbreedingCoefficient` counts duplicate ancestor appearances / total slots (`pedigreussy.go:117-161`), not Wright's coefficient. Functional but genetically inaccurate; drives the `InbreedingPenalty` ladder (`pedigreussy.go:185-196`).

#### L-9 — CLI applies two contradictory fitness-ceiling caps
`cmdBreed` first caps to 1.0 *only for non-legendary parents* (`main.go:716-718`) then unconditionally caps to 1.0 fifty lines later (`main.go:767-770`), nullifying the legendary exemption. CLI-only.

#### L-10 — Weak CLI ID generator (collision-prone)
`fmt.Sprintf("%x-%04x", time.Now().UnixNano(), rand.IntN(0xFFFF))` (`main.go:1945-1947`); same-nanosecond horses have a 1/65536 collision on a PRIMARY KEY. CLI-only.

#### L-11 — `horses.stable_id` is a dead column
Created and indexed (`migrations.go:55`,`80`) but never in `horseCols`, INSERT, or UPDATE (`postgres/horses.go`) — always `''`. Ownership is tracked only via `owner_id` (user ID), which is the root of C-4/H-2.

#### L-12 — WebSocket `CheckOrigin` allows all origins
`commussy.go:698-704` returns `true` for every origin ("Allow all origins during development"). CSWSH risk in production; the code comments acknowledge it.

---

## In-Memory State Inventory

State held only in RAM on the `Server` struct (`server.go:48-145`) and elsewhere. "Persisted" means write-through to Postgres exists and `loadFromDB` rehydrates it; "RAM-only" means it is lost on restart.

| Field / location | Type | Holds | On restart |
|---|---|---|---|
| `stables` (`server.go:50`) | `*stableussy.StableManager` | All stables + global horse registry (`stableussy.go:77-81`) | Rehydrated from `stables`/`horses` — **but** `LastBredAt`/`RetiredChampion` are dropped (H-7). |
| `market` (`server.go:51`) | `*marketussy.Market` | Stud listings, transactions, `totalBurned` | Listings rehydrated; `TimesUsed`/`MaxUses` fixed in Phase 2 (H-8); **[FIXED: Phase 4]** transaction history + `totalBurned` also rehydrate now (`Market.ImportTransaction`). |
| `raceCache` (`server.go:61`) | `map[string]*raceResult` | Replay cache, cap 200 | Lost; DB `race_replays` fallback exists. |
| `challenges` (`server.go:92`) | `map[string]*models.Challenge` | Head-to-head challenges + wager escrow state | ~~Lost~~ **[FIXED: Phase 4]** Persisted (`challenges` table, write-through on every status change); pending challenges rehydrate on boot. |
| `auctions` (`server.go:96`) | `map[string]*models.Auction` | Live auctions + bid history | Persisted (auctionRepo). |
| `bettingPools` (`server.go:100`) | `map[string]*models.BettingPool` | Open/closed pools + escrowed bets | ~~Lost~~ **[FIXED: Phase 4]** Persisted (`betting_pools` table, upserted on every mutation); unresolved pools rehydrate with escrow intact, exhibition timers reschedule, resolved pools remain as payout records. |
| `currentSeason`/`pastSeasons` (`server.go:104-105`) | `*Season` / `[]Season` | Season state | Persisted (seasonRepo). |
| `progress` (`server.go:109`) | `map[string]*PlayerProgress` | Daily limits, streaks, prestige | Persisted (progressRepo). |
| `rivalries` (`server.go:114`) | `map[string]map[string]int` | Head-to-head meeting counts | ~~Lost~~ **[FIXED: Phase 4]** Persisted (`rivalries` table, upsert-increment write-through); full matrix rehydrates on boot. |
| `alliances` (`server.go:118`) | `map[string]*Alliance` | Guilds + treasury | Persisted (allianceRepo). |
| `pendingFights` (`server.go:122`) | `map[string]*HorseFight` | Fights awaiting a joiner | **Lost — deducted entry fees never refunded (H-6).** Finished fights persisted. |
| `pokerTables` (`server.go:126`) | `map[string]*PokerTable` | Poker/hold'em tables | Persisted, but Hold'em fields truncated (H-10). |
| `departures` (`server.go:128`) | `map[string]*DepartureRecord` | Glue/fight departures + omen state | Persisted (departureRepo). |
| `jackpotPool` / `jackpotLastWinner` / `jackpotLastAmount` (`server.go:132-134`) | `int64`/`string`/`int64` | Progressive slot jackpot | **Lost — resets to seed 500 (M-2); all 2%-of-wager contributions gone.** |
| `stableMus` (`server.go:139`) | `map[string]*sync.Mutex` | Per-stable locks | Lost (fine). |
| `trainer.sessions` (`trainussy.go:25`) | `map[string][]*TrainingSession` | Per-horse training history | Persisted via `persistTrainingSession`; **[FIXED: Phase 4]** now also rehydrated on boot (`Trainer.ImportSession`). |
| `raceHistory` (`tournussy.go:26-31`) | slices + maps | All race results (indexed) | Rehydrated from `race_results`. |
| `trades` (`pedigreussy.go:424-427`) | `map[string]*TradeOffer` | Trade offers | Rehydrated from `trade_offers`. |
| `tournaments` (`tournussy.go:384-388`) | `map[string]*Tournament` | Tournaments + standings | Rehydrated from `tournaments`. |
| `hub.clients` (`commussy.go:29`) | `map[*Client]bool` | WS connections | Lost (correct). |
| All `Race`/`RaceEntry`/`TickEvent` | — | Live race sim objects | No `races` table; only derived `race_results` + `race_replays` persist. |

---

## Unwired / Stubbed Systems

Systems spec'd in docs but not reachable / not real via HTTP:

- **Tournament round betting** — the pool is opened and immediately closed before simulation (`server.go:2964-2974`), so no one can bet on tournament rounds. Comment admits "future async implementation."
- **`async_draw_poker` / `slot_machine` capabilities** — `TestHandleCapabilities` (`server_test.go:343-345`) asserts these are *not* advertised; they are intentionally unexposed.
- **CASINO_DESIGN_SPEC.md vs shipped slots** — spec specifies **20 fixed paylines**, a YOGURT/GOLDEN-HORSESHOE/SEVEN symbol set, Yogurt Vault + Glue Factory Roulette + Photo Finish bonus rounds, SNG poker tournaments, 5% poker rake, daily spin/hand caps, "Touch Grass" wellness screen, table tiers, Wild-Stallion variant, and mascot perks. **None of these ship.** The live game is 9 paylines with a different symbol set (`casino.go:33-106`), free-spins only, no rake, no daily caps.
- **MULTIPLAYER_ENGAGEMENT_RESEARCH.md** proposals not implemented: rivalries UI (map exists but no HTTP surface), spectator reactions, sponsorships, insurance, breeding contracts w/ escrow, daily-challenge templates, weekly seasons/championships, limited-time events, bounties, gifting/lending, sabotage items, mythic horses, dynasty-tier rewards, trait fusion, topic-based WS subscriptions.
- **Dead handlers/helpers**: `grantReturnLoreTrait`, `describeSpin`, `slotMultiplier`, `containsSymbol` (L-1); `drawPoker()` frontend stub; `loreTooltip`/`loreText` frontend functions never invoked.
- **Achievements meant to be granted by direct call are never granted**: `CheckAchievements` (`tournussy.go:1321+`) documents that `tournament_winner`, `first_sale`, `market_mogul`, `streak_7`, `first_trade`, `first_challenge` must be granted directly by handlers — searching server.go shows only `betting_winner` (`server.go:6562`) is granted this way; the others are effectively unobtainable unless the corresponding handler calls `grantAchievementToStable`.

---

## Economic Loop Analysis

**Currency:** `cummies` (`Stable.Cummies`) is the primary currency; `casino_chips` is a secondary, largely one-way sink. Starting balance is 5000 cummies (`stableussy.go:105`).

### Sources (create cummies)
| Source | Where | Funded by |
|---|---|---|
| Custom race purse payout | `server.go:1610`,`3159` | The purse-funding stable (debited `server.go:1302`) — **balanced**. |
| **Quick / challenge race purse** | `server.go:1381`,`4524`,`4762` | **Nobody — C-3, money from nothing.** |
| **Tournament prizes (double)** | `server.go:3159` + `server.go:3248` | Entry fees, but paid ~2× — **C-2, net creation.** |
| **Bot challenge 2× payout** | `server.go:4546` | Only challenger funds half — **H-5, net creation.** |
| Season-end rewards | `server.go:4029` | House (design faucet). |
| Daily login reward | `server.go:5910` | House (design faucet). |
| Glue factory | `server.go:9610` (`glueProduced*10 + eloBonus`) | House (design sink-offset). |
| **Daily casino chips → cashout** | `casino.go:949` → `casino.go:205` | House — 400 free cummies/day/user (**M-1**). |
| Bet payout | `server.go:6557` | Betting pool (pari-mutuel, 10% house cut, **balanced**). |
| Auction seller payout | `server.go:7540` | Buyer escrow — but **H-1** can mint if crash-timed. |
| Fight purse | `server.go:9211`,`9393` (`EntryFee*2`) | Both fighters' fees — balanced, except unrefunded orphan pending fights (H-6). |
| Alliance disband treasury split | `server.go:8427` | Prior donations — balanced. |

### Sinks (burn / remove cummies)
- Market 2% burn (`marketussy.go:192`), min 1.
- Auction 5% Geoffrussy tax (`models.go:713`).
- Tournament ~5% burn on prize distribution (`server.go:3215`).
- Betting 10% house cut (`server.go:6525`).
- Casino chip exchange 60% round-trip haircut (`casino.go:192`,`205`).
- Slots/poker house edge.
- Injury heal costs (`models.go:178-191`), alliance creation 500 (`server.go:8058`).

### Where the loop breaks
1. **Net inflation**: C-2 (tournament double), C-3 (unfunded quick/challenge purses), H-5 (bot 2×), plus C-1 poker double-settle and H-1 auction mint — these outpace the sinks and let a player farm cummies via the 6/day race cap and casino grinding.
2. **Money duplication across restart**: C-4 (trade rollback restores buyer cummies while keeping the horse), H-6 (escrowed bets/fees lost but the in-memory game already paid out), M-2 (jackpot mint).
3. **Negative balances possible**: C-9 (no DB floor + absolute-value writes + unlocked reads).
4. **Free value leaks**: M-1 (400 free cummies/day), M-7 (free horse via ≤0-price trade), C-6 (self-purchase for only the burn), C-8 (purse lost, a *deflationary* leak that still frustrates players).

---

## Balance Parameter Inventory

| Parameter | Value | file:line | Assessment |
|---|---|---|---|
| Starting stable cummies | 5000 | `stableussy.go:105` | Reasonable. |
| Quick race purse | 200 | `server.go:5576` | **Unfunded (C-3).** |
| Challenge base purse (bot) | 250 | `server.go:5577` | **Unfunded (C-3/H-5).** |
| Challenge base purse (PvP) | 500 | `server.go:4729` | Unfunded (C-3). |
| Breeding cooldown | 4 h | `server.go:5573` | Bypassable (C-5). |
| Daily trains / races | 5 / 10 (model), 6 / 6 (DB) | `models.go:637-638` / `migrations.go:202-203` | **Mismatch (M-12).** |
| Casino chip buy rate | 25 cummies/chip | `casino.go:21` | House edge. |
| Casino chip cashout rate | 10 cummies/chip | `casino.go:205` | 60% haircut (M-8). |
| Casino protected floor | 500 cummies | `casino.go:22` | Prevents casino bankruptcy. |
| Daily casino chip grant | 40 chips | `casino.go:23` | 400 free cummies/day (M-1). |
| Slot jackpot seed / min payout | 500 / 1000 | `casino.go:109`,`112` | **Min > seed → mint (M-2).** |
| Slot jackpot contribution | 2% of wager | `casino.go:580` | RAM-only (M-2). |
| Slot symbol weights | 60-stop reel, WILD×2…CHERRY×9 | `casino.go:56-72` | Targets ~94% RTP; unverified by Monte Carlo. |
| Slot paylines | 9 (spec wants 20) | `casino.go:82-92` | Divergence from spec. |
| Betting house cut | 10% | `server.go:6229` | Sink. |
| Bet min / max / per-race cap | 10 / 100000 / 3 | `server.go:6407-6447` | Server-enforced (good). |
| Market burn | 2%, min 1 | `marketussy.go:192` | Sink. |
| Auction Geoffrussy tax | 5% | `models.go:713` | Sink. |
| Tournament prize split | 60/25/10, ~5% burn | `server.go:3212-3215` | **Double-paid (C-2).** |
| ELO K-factor / floor | 32 / 100 | `marketussy.go:344`,`355` | Standard. |
| Gene scores | AA 1.0 / AB 0.65 / BB 0.30 | `models.go:89-98` | Tuned. |
| Fitness-ceiling gene weights | SPD/STM 0.25, TMP 0.15, SZE/REC/INT 0.10, MUT 0.05 | `genussy.go:33-41` | Tuned, sums to 1.0. |
| Breed ceiling jitter | ±5% | `genussy.go:218` | Clamped to [0,1]. |
| Legendary ceiling override | 9.99 / 8.88 → clamped to 1.0 | `genussy.go:384`,`619-627` | **Clamp present — see hint verification.** |
| speedScale (race) | 18.0 | `racussy.go:143` | Tuned; `CalcBaseSpeed` also clamps fitness to ≤1.0 (`racussy.go:161-166`). |
| Track modifiers | 0.6–1.0 | `racussy.go:94-111` | Tuned per track. |
| Training XP bases | Sprint 10 … MudRun 15 | `trainussy.go:43-50` | Tuned. |
| Fatigue deltas | Sprint 15 … RestDay −24 | `trainussy.go:54-61` | Tuned. |
| Base injury chance | 2% / 5% (>70) / 15% (>90) | `trainussy.go:341-346` | Tuned. |
| Fight base HP/ATK/DEF | 150 / 20 / 10 × gene×fitness | `fightussy.go:149-151` | Tuned. |
| Fight crit / dodge / mace-malfunction | 5% / 15% base / 2% | `fightussy.go:745`,`736`,`659` | Tuned. |
| Glue payout | `50 + age*3 + races*2 + wins*5`, ×10 + ELO/10 | `server.go:9578-9582` | Uncapped house faucet. |

---

## Head-Start Hint Verification

| Hint | Status | Evidence |
|---|---|---|
| marketussy purchase flow may not deactivate its listing | **Partially confirmed / by design.** `PurchaseBreeding` only deactivates when `MaxUses>0 && TimesUsed>=MaxUses` (`marketussy.go:213-216`); default `MaxUses=0` = unlimited, so a listing stays active after purchase (intentional per handoff.md BUG 2). **But** `TimesUsed`/`MaxUses` are never persisted (**H-8**), so any cap resets on restart. The server also sets `listing.Active=false` after a buy in DB write-through (`server.go:2595`) — inconsistent with the in-memory "persist" model. |
| genussy/racussy legendary fitness outside 0–1 | **Not present (already fixed).** `CreateLegendary` clamps the 9.99/8.88 overrides to 1.0 (`genussy.go:619-627`); `CalcBaseSpeed` also clamps `CurrentFitness` to ≤1.0 (`racussy.go:161-166`). |
| racussy trait comparison always true (fatigue vs distance in metres) | **Not present (already fixed).** `fatigue_resist` now compares `entry.Position < 0.8*distance` (both metres, `racussy.go:380`) — no fatigue-vs-distance comparison remains (grep confirmed). |
| racussy trait never fires behind another trait's early-exit | **Not present (already fixed).** `stamina_boost` runs in its own loop (`racussy.go:434-442`), not gated by any `skipFatigue`/early-exit (grep confirmed). See **M-4** for the remaining fragility. |
| fightussy "temporary" debuff with no restoration | **Not present (already fixed).** Mace malfunction now schedules restoration via a `tempEffect` after 3 ticks (`fightussy.go:659-673`, restored at `fightussy.go:529-532`). |
| stableussy/server.go pointer-vs-value copy divergence | **CONFIRMED — C-5** (and root-caused in H-7). `LastBredAt` (and any non-ELO field) mutated on a GetHorse pointer after `AddHorseToStable` reallocates the slice is written to orphaned memory; `syncHorseToStable` only propagates ELO. |
| trainussy seasonal effects push a stat ceiling above max without clamping | **Not present (already fixed).** `ApplySeasonalEffect` clamps ceiling to 1.0 in every boosting case (`trainussy.go:1556-1668`); `AgeHorse` Youth also clamps (`trainussy.go:1298-1300`). See **M-5/M-6** for the residual ratchet-toward-1.0 balance note. |
| tournussy round counter advances before results confirmed | **CONFIRMED — H-3.** `t.CurrentRound++` at `tournussy.go:632` precedes simulation/`RecordRoundResults`; record errors are only logged (`server.go:2987-2989`). |

Net: of the eight hints, **two are confirmed as live bugs** (pointer divergence, tournament round counter), and **six describe issues that a prior fix pass already addressed** — but several of those fixes are undermined by persistence gaps (H-7, H-8) and the newly-found economic exploits (C-1 through C-3, C-6, H-5) that the hint list did not cover.

---

## Phase 3 — Wiring & Balance Pass

Branch `fable/full-improvement-pass`, 2026-07-16. Beyond the M-* fixes annotated above, every gameplay action (race, breed, train, fight, bet, buy, sell, trade, tournament, casino) was traced HTTP handler → domain → repository. Changes:

### Wiring gaps found & fixed

- **Betting was functionally unwired over HTTP** `[1115c35]`. Races simulate synchronously with server-generated UUID race IDs, so a user-opened pool (`POST /api/betting/pools`) could never attach to a real race — every escrowed bet just waited for the stale-pool refund sweep. Tournament rounds opened and closed their pool in the same request (the L-2 stub), so nobody could ever bet on anything. Now: user-opened pools schedule a spectator **exhibition race** after a 60s window (real physics sim, zero stat/ELO/fatigue/earnings effects, pari-mutuel settlement, 10% cut to the House of USSY; pool refunded if the field collapses); tournament round races use deterministic IDs (`<tid>-round-<n>`) so the round-1 pool opens when the field reaches 2 horses and each finished round opens the next round's pool — the betting window is the gap between organizer actions. `openBettingPool` no longer clobbers an existing pool (that vaporized escrowed bets); opening a pool now requires auth. Settlement remains timing-safe: pools always close before simulation, `resolveBets` is idempotent. Note: the H-6 stale-pool sweep still refunds pools older than 15 minutes, so a tournament round left unrun for >15 min refunds its bets (safe, just no wagers).
- **Trade acceptance charged the buyer before moving the horse** `[f3ff747]`. `handleCreateTrade` never verified the horse lived in the source stable; a failing `MoveHorse` on accept left the buyer paid-up with no horse and no refund. Creation now validates horse residence, acceptance re-validates before payment (cancelling stale offers), failed payments reopen the offer, failed moves refund and reopen (new `TradeManager.ReopenOffer`).
- **Unobtainable achievements** `[23acda4]`. `tournament_winner`, `first_sale`, and `market_mogul` were defined but never granted by any handler (`first_trade`, `first_challenge`, `streak_7`, `betting_winner` were already wired). Now granted at prize distribution, listing creation, and the 10th stud-market transaction respectively.
- **Breeder-program fee lost on failure** `[b91a1ad]`. `handleBreedWithStallion` paid the stallion owner before breeding; a nil foal or failed `AddHorseToStable` ate the fee. Both paths now reverse the payment.
- **Tournament podium leak** `[1115c35]`. With fewer than 3 finishers, `distributeTournamentPrizes` silently dropped the unpaid podium shares — a 2-player tournament destroyed 10% of its pool beyond the declared 5% burn. Unclaimed shares now roll up to the champion.
- **CLI pairwise ELO half-skip** `[31e7b86]`. Found while fixing M-10: the CLI ELO loop only updated pairs where the *earlier* slice entry finished better, silently skipping ~half of all pairwise exchanges.

### Balance changes (before → after)

- **Race outcomes now have meaningful variance** `[d285848]`. Per-tick chaos (σ≈0.3) grows as √ticks while stat gaps compound linearly, so outcomes were near-deterministic: a 5% weaker horse won <0.1% of head-to-heads. Each horse now rolls a whole-race "race-day form" multiplier N(1.0, 0.055) clamped [0.85, 1.15] (deterministic under seed; extreme rolls surface as GOOD OATS DAY / WOKE UP HAUNTED BY MONDAYS tick-1 events). Measured underdog win rates over 1000 head-to-heads: 6% gap 1%→28%, 11% gap 0%→9%, 22% gap stays ~0%. Upsets possible, coin-flips not.
- **Training modes produce distinct effects** `[7d98ebc]`. All workouts previously fed one scalar (`CurrentFitness`) — Sprint- and Endurance-trained horses were identical at the ceiling. Each focused mode now builds a persisted discipline specialty consumed by the race sim (Sprint→SPD, Endurance→STM + slower in-race fatigue, MudRun→SZE on Mudussy, MentalRep→TMP + up to ~18% panic reduction; General a 35% sliver of each, RestDay none), +0.004/session (halved above 65 fatigue), capped 0.06/discipline. New `horses.training_specialty` JSONB column.
- **Casino faucets closed** `[06832d4]`. Daily 40-chip grant: minted from nothing (400 cummies/day/user) → House-of-USSY-funded at cashout value, withheld (without consuming the day) if the house is broke. Jackpot: min-payout top-up and reseed minted → house-funded; pool now persisted (`casino_jackpot` table).
- **Stud-market burn actually burns** `[23acda4]`. Buyer paid `price − burn` and seller received `price − burn` — the "2% deflationary burn" was a buyer discount; effective burn was 0. Buyer now pays full price; exactly 2% (min 1) leaves the economy.
- **Glue factory bounded** `[23acda4]`. Fresh foal payout ~620 cummies minted from nothing (infinite breed→glue pump on a 4h cooldown) → 150 cummies, house-funded: under-age-2 horses render to 15 glue, the ELO bonus only counts rating proven above the 1200 start, and all payouts debit the house treasury.
- **Seasonal ceiling ratchet removed** `[7d98ebc]` (M-5, see annotation).

### Systems verified as already sound (no change)

- Quick/challenge/bot races: purses house-funded (Phase 2 C-3/H-5), conservation verified by existing tests.
- Tournament lifecycle core: create → register (fees counted once) → rounds (counter advances only on recorded results) → single final payout (Phase 2 C-2/H-3/H-4); Phase 3 added the full-lifecycle HTTP integration test with a burn-exact conservation assertion.
- Fights: escrow, self-join guard, expiry refunds (Phase 2 C-6/H-6). Auctions: atomic settlement, escrowed bids, real 5% burn (Phase 2 H-1).
- Bet settlement determinism: pools close before simulation everywhere, `resolveBets` idempotent, race sims reproducible under seed (new test).

### Remaining known gaps (out of scope, LOW)

- L-1…L-12 as documented above; the CASINO_DESIGN_SPEC/MULTIPLAYER_ENGAGEMENT_RESEARCH feature wishlists; ~~market transaction history is not rehydrated from DB; rivalries/challenges remain RAM-only~~ *(all three closed in Phase 4)*.

### Phase 3 integration tests added (internal/server)

`phase3_econ_test.go` (exchange round-trip with disclosed rates; house-funded daily grant incl. broke-house; house-bounded chip funding), `phase3_trade_test.go` (trade create→accept full path; negative price; foreign horse; failed-payment compensation; M-9 unseated auto-fold guard), `phase3_training_test.go` (4 distinct specialties over HTTP; specialty reaches `CalcBaseSpeed`; cap), `phase3_betting_test.go` (exhibition bet-and-settle loop with conservation and no-stat-mutation; full tournament lifecycle with burn-exact conservation; pool-clobber escrow guard), `phase3_market_test.go` (burn conservation on stud purchase + `first_sale`; house-funded glue with exact veteran/foal payouts; `tournament_winner`), plus `racussy` statistical variance bounds and same-seed determinism tests.

---

## Phase 4 — Persistence & Offline Mode

Branch `fable/full-improvement-pass`, 2026-07-16. Commits: `83b37bf` (SQLite dialect layer), `a6f49c1` (sessions), `3fefca8` (state persistence), `f8c8b54` (offline mode).

### What is now persisted (and how rehydration works)

**Everything in the In-Memory State Inventory survives a restart.** The write-through model is unchanged (in-memory managers mutate first, `persistX` helpers mirror to the DB); Phase 4 closed the last gaps:

- **Challenges** — new `challenges` table; upserted at creation, accept-time expiry, bot/PvP completion, decline, and the 30s expiry sweep. `loadFromDB` restores the most recent 500. A challenge caught mid-accept by a crash reloads as `pending` (wager escrow only persists at settlement, so this matches the persisted balances).
- **Betting pools** — new `betting_pools` table storing the full pool (horses, escrowed bets, totals, status, and a new `Kind`: race/exhibition/tournament), upserted on open, add-horse, every bet, close, resolve, and refund. Unresolved pools rehydrate with their escrow intact; exhibition pools reschedule the remainder of their betting window (or resolve immediately if it elapsed during downtime); resolved/refunded pools remain in the table as permanent payout records. The H-6 "refund everything at shutdown" path was removed — an open pool now simply resumes. The 15-minute stale-pool sweep still guards abandonment.
- **Rivalries** — new `rivalries (winner_id, loser_id, wins)` table with upsert-increment write-through from the post-race update; the full matrix rehydrates.
- **Market transaction history + burn total** and **per-horse training history** — were persisted but never reloaded; `loadFromDB` now imports both (oldest-first) via new `Market.ImportTransaction` / `Trainer.ImportSession`.
- Already persisted before Phase 4 (verified): stables/horses (incl. `last_bred_at`, `retired_champion`, `training_specialty`, injury), market listings with use limits, race results, tournaments (bracket/standings/round/prize pool), trades, achievements, player progress, seasons, auctions, alliances, poker tables (full Hold'em state), departures, pending fights, glue ledger, breeding stallions, the jackpot pool, and the House of USSY treasury (a normal stable row).

### Sessions design

New `sessions` table: `token_hash` (PK), `player_id`, `created_at`, `expires_at`, `last_seen` — only the SHA-256 of the JWT is stored, never the raw token. Wiring follows the existing `GetTokenVersion` injection pattern: `authussy.AuthService` gained optional `CreateSession`/`ValidateSession` callbacks that the server implements against `SessionRepository`.

- Login/register/refresh insert a session row for every issued token (failing closed with a 500 if the store errors).
- The middleware honors a structurally valid JWT only while its session row exists and is unexpired; validation is a single `UPDATE ... SET last_seen = now WHERE token_hash = ? AND expires_at > now` (zero rows ⇒ 401), which doubles as the `last_seen` refresh.
- Expired rows are purged at startup and hourly; `DeleteSessionsForPlayer` exists for future password-change revocation.
- `STALLION_SESSION_TTL` (Go duration, default `168h`) sets both the JWT expiry and the session `expires_at`, so they age out together. Because sessions live in the DB and the signing secret is stable (env in online mode, persisted `app_config` secret in offline mode), players resume their exact state after a restart with no re-login.

### Offline mode architecture

**Decision: a dialect shim over the single shared SQL repository implementation — not a second repository.** The audit showed the runtime SQL was already 95% portable: `$N` placeholders, basic `ON CONFLICT`, partial/expression indexes, and inline CHECKs all work in SQLite; there was no `RETURNING`, `FOR UPDATE`, `INTERVAL`, `ILIKE`, casts, or lib/pq API usage anywhere. Only three queries used `NOW()` (now bound as parameters — portable), leaving the schema DDL as the single true divergence. Duplicating 20+ repo files for that would have been a maintenance disaster.

- `postgres.NewSQLite(path)` opens modernc.org/sqlite (pure Go, no CGO) with WAL, foreign keys, busy timeout, `_time_format=sqlite`, and a single-connection pool (SQLite has one writer; a pinned connection sidesteps `SQLITE_BUSY` and makes `:memory:` safe). `DB.Dialect()` reports the engine.
- `repository.RunMigrationsFor(db, dialect)` selects the schema: the existing Postgres DDL, or `migrations_sqlite.go` — a fresh-schema rendering with all retrofit `ALTER TABLE`s baked into `CREATE TABLE`, `SERIAL`→`AUTOINCREMENT`, `NOW()`→`CURRENT_TIMESTAMP`, `JSONB`→`TEXT`, and the `DO $$` constraint block as inline CHECKs.
- `cmd`: `serve --offline` or `STALLION_OFFLINE=true`; DB file `./stallionussy.db` (override `STALLION_DB_PATH`); `make offline` runs the full stack with zero external dependencies.
- Zero-config auth: offline mode auto-generates a 256-bit JWT secret on first run and persists it in the new `app_config` table.
- New unauthenticated `GET /api/status` → `{app, status, mode: "offline"|"online", storage, uptimeSeconds}` (additive). The SPA fetches it at boot and shows an amber `◈ OFFLINE MODE` badge in the terminal header.

### New environment variables

| Variable | Default | Purpose |
|---|---|---|
| `STALLION_SESSION_TTL` | `168h` | Session + JWT lifetime |
| `STALLION_OFFLINE` | `false` | Run on embedded SQLite (same as `--offline`) |
| `STALLION_DB_PATH` | `./stallionussy.db` | SQLite file location |

### Tests added (all CI-runnable — SQLite needs no server)

- `internal/repository/postgres/sqlite_test.go` — the repo's first real DB-backed tests: users/stables/horses (full JSON round-trip incl. genome, traits, injury, specialty, `last_bred_at`), race-result dedupe (M-13), stud use limits (H-8), jackpot upsert, the C-9 balance-floor CHECK, and `AcceptTradeAtomically` incl. the H-2 double-accept guard.
- `internal/repository/postgres/sessions_test.go` — session CRUD, touch-validates-and-refreshes (expired sessions cannot be resurrected), expiry purge, per-player delete.
- `internal/authussy` — middleware session enforcement (live vs dead session), fail-closed session creation on login/register.
- `internal/server/rehydration_test.go` — full round-trip: boot a real server on a temp SQLite file, create state through real code paths (HTTP registration, training, challenge, escrowed bet, rivalry, market tx, jackpot), boot a second server on the same file, assert identical state including that the pre-restart token still authenticates; plus expired-session-stays-dead.
- `internal/server/status_test.go` — `/api/status` mode reporting per backend; zero-config offline secret survives restarts.

Also verified manually end-to-end with the compiled binary: offline boot with no env vars, register/login over HTTP, hard process restart, old token still authenticates, `/api/status` reports offline.

### Known limitations

- The SQLite schema is fresh-start-only: future column additions need explicit `ALTER TABLE` handling for existing offline DBs (SQLite lacks `ADD COLUMN IF NOT EXISTS`).
- Refresh does not revoke the previous token's session (it ages out at its original expiry) — acceptable, both belong to the same player.
- Postgres-backed integration tests still don't run in CI (no server available); the SQLite suite covers the shared SQL implementation, which is identical except for the DDL.
