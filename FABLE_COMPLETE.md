# FABLE COMPLETE

Final report for the full improvement pass on **STALLIONRUN / StallionUSSY** (branch `fable/full-improvement-pass`, 2026-07-16). Five phases: audit, critical fixes, wiring & balance, persistence & offline mode, and integration/smoke stabilization. Companion document: `FABLE_FINDINGS.md` (the full findings inventory with per-finding fix annotations).

---

## Phase 1 — Audit (FABLE_FINDINGS.md)

Read-only codebase archaeology. Produced the authoritative findings inventory: 9 CRITICAL, 11 HIGH, 14 MEDIUM, 12 LOW findings with exact `file:line` citations, plus an architecture overview, in-memory state inventory, unwired-systems list, economic loop analysis, and balance parameter inventory. No code changed.

## Phase 2 — CRITICAL & HIGH fixes (20/20 fixed)

Organized by system; finding IDs refer to FABLE_FINDINGS.md.

**Money integrity / economy**
- C-2, H-3, H-4: tournament prize pool no longer paid twice (single end-of-tournament distribution), the round counter only advances on recorded results, and running tournament rounds requires the organizer. Registration also no longer double-counts entry fees into the pool.
- C-3, H-5, C-8: quick-race, challenge, and bot-wager purses are funded from a new **House of USSY treasury** (a system stable seeded with 1,000,000 cummies) instead of minted; failed race creation refunds the purse on every error path. The betting house cut banks into the treasury rather than evaporating.
- C-6: self-purchase of your own stud listing and self-trades are rejected at the handler by stable identity.
- C-9: DB `CHECK (cummies >= 0)` / `CHECK (casino_chips >= 0)` constraints added.

**Persistence correctness**
- C-4, H-2: `AcceptTradeAtomically` uses the correct owner-ID column semantics, guards every statement with `WHERE status='Pending'` / RowsAffected checks, so DB trades no longer silently roll back (money-duplication-across-restart exploit closed).
- H-1: auction settlement debits the buyer and credits the seller in one transaction with RowsAffected guards.
- H-7, H-8, H-9, H-10: `last_bred_at`, `retired_champion`, stud `times_used`/`max_uses`, `market_transactions.seller_payout`, and the full Hold'em poker table state are persisted and reloaded.

**Concurrency / state aliasing**
- C-1: poker settlement runs under a per-table lock; the shallow table clone no longer aliases shared slices; settlement is idempotent behind a terminal-status guard.
- C-5: breeding cooldowns are written to the live registry horse (the orphaned-pointer divergence after slice reallocation is fixed); `syncHorseToStable` propagates all mutable fields.
- C-7: all reads/writes of `stable.Horses`/`Cummies`/`CasinoChips` standardized on the stable lock.
- H-6: betting-pool escrow and pending-fight fees are refunded on expiry (fully persisted later in Phase 4).

**CLI**
- H-11: CLI trade acceptance now moves the horse, not just the money.

## Phase 3 — MEDIUM fixes, wiring & balance (14/14 resolved)

**Casino** (M-1, M-2, M-3, M-8, M-9)
- The daily 40-chip grant is house-funded at cashout value (was 400 free cummies/day minted per user); a broke house grants nothing without consuming the day.
- The slot jackpot min-payout top-up and reseed are house-funded; the jackpot pool persists (`casino_jackpot` table).
- Exchange rates (buy 25 / cashout 10) are disclosed in the UI and API. Hold'em timeout auto-fold requires a seated caller. Jackpot/line-win stacking kept (WONTFIX: standard slot design, negligible RTP impact).

**Racing & training** (M-4, M-5, M-6, M-10)
- Race-day form: each horse rolls a whole-race multiplier N(1.0, 0.055) clamped [0.85, 1.15], giving meaningful upset rates (6% stat gap: 1%→28% underdog wins) without coin-flips; deterministic under seed.
- Distinct training modes: each focused workout builds a persisted discipline specialty consumed by the race sim (Sprint→SPD, Endurance→STM + slower in-race fatigue, MudRun→SZE on Mudussy, MentalRep→TMP/panic reduction), capped at 0.06/discipline.
- Fatigue trait modifiers compose multiplicatively in one place; seasonal ceiling boosts scale with remaining headroom (no ratchet to 1.0); CLI ELO is DNF-safe and order-independent (also fixed a latent bug that skipped ~half of all pairwise ELO exchanges).

**Schema consistency** (M-7, M-12, M-13, M-14)
- Trades: negative prices rejected, zero allowed as explicit gift, horse residence validated at creation, failed payments/moves compensate and reopen the offer.
- `race_results` deduped with a unique `(race_id, horse_id)` index + `ON CONFLICT DO NOTHING`; `GetStableByOwner` is deterministic (oldest-first); daily-limit defaults documented as 6/6.

**Wiring gaps closed**
- **Betting wired end-to-end**: user-opened pools schedule a real spectator exhibition race after a 60s window (physics sim, zero stat/ELO/earnings side effects, pari-mutuel settlement, 10% house cut); tournament rounds use deterministic race IDs so each round's pool is genuinely bettable between organizer actions; pool-clobbering (which vaporized escrow) is blocked; opening a pool requires auth.
- Unobtainable achievements (`tournament_winner`, `first_sale`, `market_mogul`) are granted by their handlers.
- Breeder-program stud fee refunded when breeding fails after payment; tournament podium shares with <3 finishers roll up to the champion instead of vanishing.
- Stud-market burn actually burns (buyer pays full price; exactly 2% leaves the economy); glue factory bounded and house-funded (was an infinite breed→glue pump of ~620 minted cummies per foal).

## Phase 4 — Persistence & offline mode

**Everything in the in-memory state inventory now survives a restart**: challenges, betting pools (with escrowed bets; exhibition timers reschedule across downtime), rivalries, market transaction history + burn total, per-horse training history — on top of everything already persisted (stables, horses incl. cooldowns/specialties/injuries, listings, results, tournaments, trades, achievements, progress, seasons, auctions, alliances, poker, departures, fights, jackpot, treasury).

**Sessions**: new `sessions` table stores the SHA-256 of each issued JWT with expiry and last-seen. Middleware honors a JWT only while its session row is live (single `UPDATE ... WHERE expires_at > now` doubles as validation and refresh). `STALLION_SESSION_TTL` (default `168h`) drives both JWT and session lifetime. Players survive server restarts without re-login.

**Offline mode**: a dialect shim over the single shared SQL repository (the runtime SQL was already portable; only the DDL diverges — `migrations_sqlite.go`). `stallionussy serve --offline` (or `STALLION_OFFLINE=true`) runs the whole stack on embedded SQLite via modernc.org/sqlite (pure Go, WAL, single-writer pinned connection), DB at `STALLION_DB_PATH` (default `./stallionussy.db`), zero external dependencies, auto-generated JWT secret persisted in `app_config`. `make offline` target. New unauthenticated `GET /api/status` reports `{app, status, mode, storage, uptimeSeconds}`; the SPA shows an amber `◈ OFFLINE MODE` badge.

## Phase 5 — Integration & smoke stabilization (this phase)

Three bugs found and fixed while running the full suite under `-race` repeatedly and smoke-testing the real binary:

1. **Random purse events minted money** (`internal/server/server.go`). The mid-race random events `evt_sponsor_bonus` (purse ×2), `evt_treasure_chest` (+1000), fired on ~15% of races with no funding source — a C-3-style faucet the earlier passes missed (it surfaced as a flaky `TestGuestQuickRace_NoMinting` failure). Purse increases are now funded by the House of USSY treasury (partial/none if the house is short), the Geoffrussey tax garnish is banked into the treasury instead of evaporating, and zero-purse (guest/exhibition) races stay money-free for every event. New deterministic regression test `TestRandomPurseEvents_HouseFundedAndConserved`; `TestQuickRace_PurseFundedByHouse` relaxed from exact-debit to a ≥70%-of-purse bound to accommodate legitimate event-driven house movement.
2. **Horses could be injured by a rest day** (`internal/trainussy/trainussy.go`). The 2% training injury roll ran even for `WorkoutRecovery`, randomly spiking fatigue to 100 and failing the (pre-existing, deterministic) RestDay tests. Recovery workouts no longer roll injuries — rest is the safe option by design.
3. **Same-second login failed with a 500** (`internal/repository/postgres/sessions.go`). JWTs carry second-granular `iat`, so register + login (or two logins) within the same second mint byte-identical tokens; the second session INSERT hit the `token_hash` primary key and failed the request. `CreateSession` is now an upsert (`ON CONFLICT (token_hash) DO UPDATE` — valid on both PostgreSQL and SQLite); the session test that had encoded duplicate-insert-must-fail was corrected to assert upsert semantics.

Also: gofmt drift in three files from earlier phases fixed (whitespace only).

### Final verification results

- `go build ./...` — clean.
- `go vet ./...` — clean.
- `go test -race -count=1 ./...` — all 15 packages pass; the server package was additionally run 9× under `-race` with no failures or race reports after the fixes above.

### Offline smoke test (real binary, embedded SQLite, no external dependencies)

| Step | Result |
|---|---|
| Boot `serve --offline` with scratch DB; `GET /api/status` → `mode:"offline", storage:"sqlite"` | PASS |
| Register + immediate login + `GET /api/auth/me` (same-second token regression) | PASS (after fix 3) |
| Stable auto-created with 5000 cummies and starter horses | PASS |
| Sprint training raises SPD specialty only; Endurance raises STM only (distinct modes) | PASS |
| Breeding produces a foal that joins the stable | PASS |
| Authenticated quick race: runs, purse house-funded, total economy does not grow, no negative balances | PASS |
| Stud market listing created and visible in `GET /api/market` | PASS |
| Exhibition betting: pool opened, 50-cummie bet escrowed, window elapsed, pool `resolved`, payout + 10% house cut exactly conserve the pre-bet total | PASS |
| Second exhibition round (losing bet): pool resolves, exact conservation, no negatives | PASS |
| Tournament: create (2 rounds, 100 fee) → register 2 horses (fees collected once) → organizer runs both rounds → `Finished`, prize paid with exact 5% burn (200 pool → 190 paid, total economy −10) | PASS |
| Hard `kill -9`, restart on the same DB file: old session token still authenticates; stable, horses (fitness/ELO/specialties), balances, tournament state, and market listing byte-identical | PASS |
| House of USSY treasury topped back up to 1,000,000 on boot | Expected (documented once-per-boot design faucet from the C-3 funding model, not data loss) |
| SPA served from `web/` (459 KB self-contained single file), contains the `api('/status')` fetch and the `◈ OFFLINE MODE` badge; no external assets to break | PASS |
| `make offline` — boots the full offline stack, `/api/status` healthy, clean shutdown | PASS |
| `make dev` — parses and starts; fails only at Postgres authentication (no correctly-credentialed Postgres in this environment) | PASS (environmental) |
| Docker-dependent targets (`make up`, `docker-run`) | Not run — would conflict with an existing Postgres on :5432 in this environment; Dockerfile unchanged by all phases except none |

Note: the SPA is served from a `web/` directory resolved relative to the process working directory, so the binary must run from the repo root (as `make offline`/`make serve` do) for the frontend to appear; from elsewhere the API still works and `GET /` returns a JSON status.

## Tests added across the pass

- **Phase 2** (regression tests for every C/H fix, `internal/server` + repos + CLI): tournament single-payout/round-counter/authz, house-funded quick/challenge/bot purses, purse refund on failed creation, poker double-settlement lock, breeding cooldown through the live registry, self-purchase/self-trade guards, atomic trade/auction guards, escrow refunds, persisted cooldown/champion/stud-use fields, CLI trade horse movement.
- **Phase 3** (`internal/server/phase3_*.go`, `internal/racussy`): exchange round-trip with disclosed rates; house-funded daily grant incl. broke house; trade create→accept path incl. compensation; M-9 unseated auto-fold guard; 4 distinct training specialties over HTTP + specialty reaching `CalcBaseSpeed` + cap; exhibition bet-and-settle with conservation and no stat mutation; full tournament lifecycle with burn-exact conservation; pool-clobber escrow guard; stud-purchase burn conservation + `first_sale`; house-funded glue exact payouts; `tournament_winner`; race variance statistical bounds + same-seed determinism.
- **Phase 4**: `sqlite_test.go` (first real DB-backed repo tests: full horse JSON round-trip, M-13 dedupe, H-8 use limits, jackpot upsert, C-9 balance floor, H-2 double-accept guard), `sessions_test.go` (CRUD, touch-validate-refresh, expiry purge, per-player delete), authussy middleware session enforcement, `rehydration_test.go` (boot server on temp SQLite → create state over HTTP → reboot → assert identical state + token survival), `status_test.go`.
- **Phase 5**: `TestRandomPurseEvents_HouseFundedAndConserved` (purse events conserve money and are inert on zero-purse races); sessions upsert-on-same-token assertion.

## Remaining known issues / limitations (honest list)

- **LOW findings L-1–L-12 remain open** (see FABLE_FINDINGS.md): dead casino helpers, stubbed comments, Dockerfile ldflag no-op and Go toolchain mismatch, hardcoded default DB password in `main.go`/compose, `==` instead of `errors.Is` for `sql.ErrNoRows`, naive inbreeding coefficient, CLI double ceiling-cap and weak ID generator, dead `horses.stable_id` column, WebSocket `CheckOrigin` allows all origins (CSWSH risk in production). One update: L-5's nginx port change (`deploy/nginx-horse.ussyco.de.conf`, 8080→4200) is **no longer uncommitted** — it was committed by Phase 3 in `06832d4`.
- **Design wishlists unimplemented**: CASINO_DESIGN_SPEC.md (20 paylines, bonus rounds, SNG poker, rake, daily caps) and MULTIPLAYER_ENGAGEMENT_RESEARCH.md features remain future work by scope decision.
- **M-3 / M-11 WONTFIX** stand: jackpot/line-win stacking is intended; winner-take-all win/loss accounting is shared server/CLI semantics and changing it is a product decision.
- The **SQLite schema is fresh-start-only**: future column additions need explicit `ALTER TABLE` handling for existing offline DBs.
- **Token refresh does not revoke** the previous token's session (it ages out at its original expiry); both belong to the same player.
- **Postgres-backed integration tests still don't run in CI** (no server available); the SQLite suite exercises the shared SQL implementation, which differs only in DDL.
- The **exhibition betting window is a fixed 60s** at runtime (overridable in tests only); the 15-minute stale-pool sweep refunds any tournament round pool left unrun that long (safe, just no wagers).
- The **house treasury top-up** (to 1,000,000 once per process start) is a deliberate bounded faucet replacing the old unbounded minting; a long-lived process can drain it (sponsored purses/grants then shrink to whatever the house recoups) and a restart refills it.
- Race sim entropy uses the global PRNG; race outcomes are reproducible under seed in tests but not replayable from production logs.
