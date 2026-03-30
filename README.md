# StallionUSSY

Go monolith for a browser-based horse breeding, racing, trading, and chaos simulator.

## What Works

- Authenticated registration creates one stable per user.
- New authenticated stables are seeded with 2 starter horses so users can immediately race and breed.
- Existing empty user stables are backfilled with starter horses on server boot.
- Authenticated users who end up with an empty stable can claim a one-time emergency starter pair from the stable page.
- Owned empty stables now show an explicit recovery panel instead of sending zero-horse players into breeding dead ends.
- First-time authenticated players are offered a skippable interactive tutorial that walks the main gameplay loop.
- The interactive tutorial now covers the broader playable spine: stable, empty-stable recovery, horse detail, training/recovery, quick race, custom race, breeding, market, challenges, fights, glue/departed systems, studs, progression, replay/share, and help surfaces.
- Race replay links use `#replay/{raceID}` and recent replays are available via `/api/races/recent`.
- Stud-market breeding now requires an explicit owned mare.
- Quick races open a short betting window before the race starts.
- Bets and challenges now use authenticated ownership checks end to end.
- Challenge history and betting resolution now render correctly in the SPA.
- The SPA now includes an in-world lore codex page plus inline lore tooltips on core racing, genetics, progression, market, achievement, challenge, and betting surfaces.
- The SPA now also includes a persistent page-help panel that gives current-screen guidance and recovery-oriented checklists outside the tutorial.
- Progression now has a real daily action loop for authenticated players: training and race entries are limited per day and surfaced in the dashboard.
- Authenticated quick races now guarantee solo progression by auto-filling the field with matched computer-controlled opponents when needed.
- The challenge page now supports a `CPU Arena` fallback for immediate 1v1 progression when no other player is available.
- The SPA now uses a fixed desktop app shell: the left chat column stays pinned while the right content pane scrolls independently.
- Quick Race now auto-selects your strongest active horse instead of the first available one.
- The SPA now includes a `CASINO` page with ring-fenced casino chips, Texas Hold'em and five-card draw poker tables, and a 5-reel video slot machine.
- Horses destroyed in fatal fights or sent to the glue factory now enter a departed-horse ledger and can trigger rare return omens with altered traits and lore.
- The casino slot machine is a 5-reel, 9-payline video slot with 12 horse-themed weighted symbols, wild/scatter mechanics, free-spin bonus rounds, and a server-wide progressive jackpot (~94% RTP).
- Texas Hold'em poker supports 2-6 players with full betting rounds (pre-flop, flop, turn, river), blinds, check/call/raise/fold/all-in actions, side pot computation, best-5-of-7 hand evaluation with kicker tie-breaking, and a 60-second action timeout.

## Current Rules

- Authenticated users get exactly one stable.
- Each stable gets its initial starter pair plus at most one manual emergency starter recovery grant.
- Guests still use guest-mode client behavior and do not get persistent auth-backed onboarding, but they can use `POST /api/races/quick`.
- Shared race replays are public GET endpoints and are retained in cache/DB for about 7 days.
- Custom race purses are funded from the authenticated creator's stable balance and cannot be minted from arbitrary client input.
- Tutorial state is currently persisted in browser local storage and can be replayed from the in-app help button.
- Lore help follows a progressive disclosure model: practical UI text first, short mechanic-plus-fiction tooltip second, full codex entry third.
- Lore wording is now normalized around these terms: `Sappho Score` is the numeric rating, `Sappho Scale` is the broader institutional doctrine; `Geoffrussy` is the platform governance authority; `B.U.R.P.` is the anomaly-response bureau.
- Prestige thresholds were pulled forward for better early and mid-game pacing, while higher tiers still ramp sharply.
- Stud-market and breeder-stallion breeding now respect stable-cap and breeding-cooldown rules just like direct breeding.
- Stable-cap checks now count active racing horses, so retired horses do not consume your competitive slots.
- Race purse distribution is flatter and win-streak multipliers are lower, so non-winning finishes still produce some forward progress.
- Training fatigue penalties now taper earlier and rest recovers less, which makes rotating horses more valuable than cycling one favorite endlessly.
- Irreversible destruction loops now protect your last active horse from being deleted.
- Casino gambling uses a separate `casino chips` wallet by default so poker and slots do not directly destroy core stable progression.
- Slot machines are 5-reel, 9-payline video slots with weighted symbols (WILD_MARE, GOLDEN_STALLION, CHAMPION_TROPHY, RACING_SADDLE, LUCKY_HORSESHOE, SUGAR_CUBE, CARROT, OATS, BELL, CHERRY, YOGURT scatter, SKULL). Wild substitutes for all non-scatter symbols. 3+ YOGURT scatters anywhere trigger 5/10/15 free spins at 2x multiplier. A progressive jackpot (2% wager contribution) triggers on 5x GOLDEN_STALLION on the middle payline.
- Slot spins accept the main authenticated `POST` path and a compatibility `GET` fallback on the same endpoint to avoid dead-end method mismatches in the SPA.
- Texas Hold'em poker runs full betting rounds: pre-flop, flop, turn, river, showdown. Blinds are derived from buy-in (buyIn/20 small, buyIn/10 big). All standard actions are supported: check, call, raise (minimum 2x), fold, and all-in. Side pots are computed automatically for multi-way all-in scenarios. Hand evaluation picks the best 5 of 7 cards (C(7,5)=21 combinations) with kicker tie-breaking. Players have 60 seconds to act before auto-fold.
- Async five-card draw poker is still available as a lighter alternative: one buy-in, one draw phase, no live betting rounds.
- Casino chip exchange enforces a protected cummies floor so gambling cannot bankrupt a stable below basic operating capital.
- The casino frontend renders a 3x5 reel grid with spin animations, winning payline highlights, scatter/wild cell styling, progressive jackpot display, bonus messages, and scrollable spin history. Poker tables show visual card elements with suit symbols, clickable card selection for draw discard, Hold'em action buttons, community card display, seat status indicators, and pot/side-pot tracking.
- Departed horses do not freely revive. They enter a dormant ledger, may surface a rare omen later, and return permanently altered with reduced efficiency and anomalous traits.
- Breeding market listings are deactivated after purchase to prevent double-buy exploits.
- Legendary horse FitnessCeiling is clamped to 1.0 to prevent disproportionate race speed.
- The `fatigue_resist` trait provides 50% fatigue reduction rather than full immunity.
- Mace malfunctions in combat are now temporary effects that restore after 3 ticks.
- Seasonal event bonuses clamp FitnessCeiling to 1.0 to prevent stat drift.
- ELO updates operate on canonical horse pointers to prevent stale data from copy divergence.

## v2 Comprehensive Overhaul

### Racing Engine (8 fixes)
- `fatigue_resist` trait condition fix: now correctly checks for the trait instead of always granting immunity.
- Weather effects apply to all weather types (Haunted, Scorching, etc.) — no more silent immunity gaps.
- Race events use append-slice instead of single-value overwriting, so multiple events per tick are preserved.
- Crowd-surge bonus now verifies the horse is actually in the lead before applying the boost.
- Race narrative RNG uses deterministic seeding per-race for reproducible results.
- Removed dead/unused `RaceConfig` struct that was shadowed by the actual configuration.
- Rebalanced Haunted and Scorching weather modifiers for fairer race outcomes.
- Fitness floor prevents horses from going below 0.0 fitness during extreme fatigue.

### Combat Engine (10 fixes)
- Hit/dodge resolution uses separate RNG rolls instead of a single roll that made dodging overpowered.
- Standardized format strings for `desperate_lunge` and dodge narrative messages.
- Derulo rage mechanic now has a cap preventing infinite rage stacking.
- Morale system uses actual damage dealt (not raw attack) for more accurate morale shifts.
- Stat-swap stacking guard prevents repeated stat swaps from compounding beyond intended limits.
- Combat RNG uses deterministic seeding per-fight for reproducible results.
- `chaosMode` flag is now actually implemented and triggers enhanced randomness when active.
- `haunted_mace` narrative text is correctly rendered during malfunction events.
- TMP gene now influences rage generation rate as originally designed.

### Training & Genetics (11 fixes)
- Gene rarity distribution corrected: common/uncommon/rare/legendary odds match design spec.
- Youth ceiling clamp prevents young horses from exceeding maximum stat thresholds during growth.
- `sappho_boost` trait correctly checked before applying Sappho Score bonuses.
- `lot11_boost` scope fix: bonus now applies only to the intended training context.
- `Train()` rejects retired and injured horses with proper error messages instead of silently training them.
- `RecoverFatigue()` guards against negative fatigue values from over-recovery.
- Injury system properly connected: injuries from fights/races now affect training eligibility.
- Trait cap enforced at 6 maximum traits per horse to prevent unbounded trait accumulation.
- `Breed()` guards against nil parent and self-breed attempts.

### Market & Tournaments (13 fixes)
- Stud burn mechanic has a floor price preventing listings from being burned to zero value.
- Stud listings now persist with `MaxUses` tracking instead of disappearing after one purchase.
- Buyer balance check enforced before stud purchase to prevent negative-balance exploits.
- Duplicate listing guard prevents the same horse from being listed twice simultaneously.
- ELO floor prevents horses from dropping below a minimum rating.
- Market listings use copy-on-read to prevent callers from mutating shared state.
- Tournament bracket building uses append instead of O(n) slice prepend for better performance.
- Tournament prize pools are now collected from entrants and distributed to winners correctly.
- Minimum horse check prevents tournaments from starting with too few entries.
- Dynamic track counting replaces hardcoded track count for future track additions.
- Achievement description text fixes for accuracy.
- Removed duplicate "Stampede" achievement entry.

### Infrastructure Fixes
- **stableussy**: `RemoveHorse` outer loop now breaks after finding the horse, preventing unnecessary iteration. `ListHorses` returns empty slice instead of nil. `SeedLegendaries` logs errors instead of silently discarding them.
- **pedigreussy**: `AcceptOffer`, `RejectOffer`, `CancelOffer` now update `UpdatedAt` timestamps. Removed unused `Children` and `Inbreeding` fields from `PedigreeNode`. `buildNode` logs ancestor lookup errors instead of silently discarding them.
- **commussy**: `WritePump` sends each queued message as its own WebSocket frame instead of concatenating multiple JSON objects (which broke `JSON.parse` on the client). Rate limiting extended to `chat_emote` and `whisper` message types. Self-whisper check uses `UserID` comparison instead of case-insensitive username match. Text truncation is UTF-8 safe (rune-based instead of byte-based).
- **authussy**: `HandleRegister` and `HandleLogin` enforce 1MB request body size limits. Username lookup errors are logged instead of silently ignored.
- **nameussy**: Removed duplicate "Unhinged" adjective. Double-adjective and double-noun dedup uses a loop instead of single re-roll to guarantee uniqueness.

### Frontend Presentation Overhaul
- **Racing**: Race cards with hover/active states, track-type color badges (Sprintussy red, Grindussy green, Frostussy blue, Mudussy brown, Thunderdome amber, Hauntedussy purple), position items with medal colors (gold/silver/bronze), progress bars with glow effects, weather badges, pulse animation for active races.
- **Horse Detail**: Two-column profile grid, stat grid with color-coded values (low/mid/high), gene badges colored by zygosity (AA green, AB amber, BB red), trait list with rarity-colored left borders (common/rare/legendary/anomalous), fitness bars with condition-based coloring, Sappho Score pip meter visualization.
- **Combat**: Fight result cards with fatality variant styling, versus display with HP bars, arena badges, round log with alternating row colors and critical-hit highlighting.
- **Breeding**: Breeding pair display with connector line, gene compatibility matrix with colored cells, offspring preview cards with stat range bars, cooldown badge styling.
- **Market**: Listing cards with price/ELO/Sappho display, stud listing grid, burn badge for depleted listings, uses remaining bar, buy/cancel action buttons with hover states.
- **Tournaments**: Tournament cards with status badges (open/active/finished), bracket display styling, round headers, match cards with winner highlighting, prize pool display.
- **Leaderboard**: Table styling with rank medal colors, stat columns with bar indicators, alternating row colors with hover highlighting, section headers for different ranking categories.
- **Achievements**: Achievement cards with locked/unlocked states, rarity border colors, progress bars for incremental achievements, unlock animation with glow effect.
- **Stable Overview**: Stable header with balance display, horse roster cards with inline stat bars, quick-action buttons, empty-state styling.

## Requirements

- Go 1.25+
- PostgreSQL

The HTTP server requires Postgres at startup. By default it uses:

```text
postgres://stallionussy:h0rs3ussy420@localhost/stallionussy?sslmode=disable
```

Override with `DATABASE_URL`.

## Run

```bash
createdb stallionussy
DATABASE_URL='postgres://stallionussy:h0rs3ussy420@localhost/stallionussy?sslmode=disable' make serve
```

Or:

```bash
DATABASE_URL='postgres://stallionussy:h0rs3ussy420@localhost/stallionussy?sslmode=disable' go run ./cmd/stallionussy serve --port 8080
```

CLI mode can run without DB persistence if Postgres is unavailable:

```bash
go run ./cmd/stallionussy cli
```

## Validation

```bash
go test ./...
go vet ./...
go build ./...
```

## Notes

- `docker-compose.yml` does not currently provision Postgres, so `docker-compose up` is not sufficient by itself for the server path.
- Frontend is a single-file SPA at `web/index.html`.
- The first-session tutorial intentionally focuses on the core loop first: stable -> horse -> training -> race -> results, then previews breeding, market, competition, and replay/share.
- The lore codex is routed at `#lore` and is also reachable from the bottom-right help area.
- Authenticated player progression and season state now persist in Postgres and survive server restarts.
- Casino tables, slot spin history, and departed-horse omens also persist in Postgres.
- Challenges and betting pools are still in-memory and reset on server restart.
- On mobile, chat remains a drawer instead of a persistent side column.
