# SOLO FINDINGS

Read-only diagnostic pass on **STALLIONRUN / StallionUSSY**, branch `master` @ `56da38e`, 2026-07-16.
**Fix pass 2026-07-16**: every finding below is annotated `[FIXED: sha]` / `[OPS]` / `[PARTIALLY ADDRESSED]` inline.
Scope: (1) "sometimes horses don't race", (2) solo-player progression dead ends, (3) remaining NaN/Inf JSON sources.
All line numbers are against the tree at `56da38e`. Reproduction was done against a freshly built binary in offline mode (`STALLION_OFFLINE=true`, SQLite scratch DB, port 8299) plus read-only inspection of production logs (`journalctl -u stallionussy`).

Legend for each finding: **[bug]** = incorrect behavior, **[feedback]** = correct behavior with missing/misleading UX, **[feature]** = capability that does not exist. Fix size: **S** (≤1 h, localized), **M** (half-day, one subsystem), **L** (multi-day / design decision).

---

## (a) Executive summary

1. **"Horses don't race" has three concrete causes, all verified.**
   - **R-1 (top):** The tournament detail view in the SPA reads the wrong response shape (`GET /api/tournaments/{id}` returns `{tournament, standings}` but the SPA reads fields off the top level), so the REGISTER form and the "▶ RUN NEXT ROUND" button **never render**. Tournaments can be created and listed from the UI but can never be raced. Present since the initial commit.
   - **R-2:** Every authenticated quick race broadcasts `betting_pool_opened` over WS **before** the race simulates; the SPA responds by opening a **full-screen betting modal (z-index 9000, 80% black overlay)** on *every connected client, including the racer* — directly covering the race animation. The pool closes microseconds later, so any bet placed in that modal is rejected with "betting is closed", and because zero-bet pools are deleted without a `betting_resolved` broadcast, **nothing ever closes the modal**. To the player it looks like "I clicked QUICK RACE and got a broken betting popup instead of a race."
   - **R-3:** The Phase-3 exhibition-betting flow (60 s window, then a spectator race) is **unreachable from the SPA** (no UI calls `POST /api/betting/pools`), and even over the raw API the resulting race is **invisible**: no countdown/deadline field in the pool JSON, no race replay broadcast, no `raceCache` entry (`GET /api/races/{id}` → 404). Only a one-line WS chat message announces the result.
   - Supporting paper cuts: a PvP challenge issued at the daily race cap returns 429 **but is still created and broadcast** (R-4); PvP challenges expire after 5 minutes so asynchronous PvP never happens (R-6); tournament round races are never rendered by the tournaments page even when run via API (R-7).

2. **Solo progression is real but demoralizing and capped.** A fresh solo player's starter horses (fitness ≈ 0.42–0.44) are matched by `pickBotOpponents` **on ELO only** against clones of the 12 seeded legendary house horses (fitness 0.65–1.00, all pinned at ELO 1200). Measured live: **6/6 quick races lost to "E-008's Chosen" clones, total race income ₵40 on ₵1,200 of house-funded purses**. The daily cap of 6 races + 6 trainings exhausts the entire gameplay loop in ~10 minutes. Market, trades, auctions, fights (retired-only AND needs a human joiner), alliances, PvP challenges, seasonal rewards (admin-only), and betting are all dead ends solo. What *does* work solo: training, breeding (4 h cooldown), solo tournaments **via API only** (blocked in UI by R-1), CPU-arena challenges, casino, glue factory, daily reward, prestige XP.

3. **The 03:39:08 production NaN is NOT a new source.** It is the already-fixed `/api/market` SapphoScore NaN (E-008's Chosen), misattributed to `/api/prestige` by log-line ordering: `writeJSON` logs the encode error *inside* the handler and `loggingMiddleware` logs the request line *after* it, so the error line at 03:39:08 belongs to the `GET /api/market 200 54µs` logged on the very next line — `/api/prestige` was a separate concurrent request. The running production process (PID 4000359) started **03:23:45**, the fix commit `56da38e` landed **03:37:16** (the on-disk binary was rebuilt at 03:37 but never restarted into). **Action: restart/redeploy the service.** A systematic audit found every other float division in JSON-reachable code guarded; one latent offline-mode bug remains (N-2: SQLite cannot persist the NaN score — NOT NULL constraint — so an E-008 listing silently vanishes on restart).

---

## (b) Findings

## Area 1 — "Sometimes horses don't race"

### R-1 — Tournament detail view reads the wrong response shape → register/run-round UI never renders — **[bug] Severity: HIGH — Size: S** — [FIXED: 07f5e05]

- Server: `internal/server/server.go:3149-3152` — `handleGetTournament` responds `{"tournament": {...}, "standings": [...]}`.
- SPA: `web/index.html:8830` (`const t = await api('/tournaments/${id}')`), then reads `t.status` (8838-8839), `t.name` (8836), `t.track_type`/`t.rounds`/`t.prize_pool`/`t.entry_fee` (8843-8846) — **all undefined** because they live under `t.tournament`. `isOpen`/`isInProgress` are therefore always `false`, so the REGISTER HORSE form (guarded at 8871) and the "▶ RUN NEXT ROUND" button (guarded at 8901) **never render**. Only standings display (luckily `t.standings` is top-level).
- The list view is fine (`GET /api/tournaments` returns a bare array; `renderTournamentList` at 8769 works), so players see tournaments, click in, and hit a dead end — no way to register or race. This exactly matches "the horses don't race."
- Verified: `git log -S` shows both sides unchanged since the initial commit — this has never worked from the UI. (My solo tournament repro below worked only because I called the API directly.)
- **Fix direction (S):** in `viewTournament`, unwrap `const t = data.tournament || data; const standings = data.standings || t.standings || []`. Do not change the server response (other consumers/tests may rely on it). While in there, hide the RUN button when `t.created_by` isn't the current user (see R-8).

### R-2 — Quick races pop a stale, un-dismissable full-screen betting modal over the race — **[bug + feedback] Severity: HIGH — Size: S/M** — [FIXED: cdf82fe]

Sequence (all verified in code and by live pool behavior):
1. `handleQuickRace` opens a pool and broadcasts `betting_pool_opened` **before** simulating: `internal/server/server.go:1591` (auth path) and `:1638` (guest path); broadcast inside `openBettingPool` at `server.go:7233-7243`.
2. `runRace` closes the pool before simulation in the same synchronous request: `server.go:1682` → `closeBettingPool` (7277) broadcasts `betting_pool_closed`.
3. SPA: `betting_pool_opened` → `onBettingPoolOpened` (`web/index.html:5185-5187`, `9683-9697`) sets `#betting-modal` (`index.html:3471`) to `display:flex`; `.modal-overlay` is `position:fixed; inset:0; rgba(0,0,0,0.8); z-index:9000` (`index.html:1673`) — it **covers the race animation** that `simulateLocalRace` starts when the HTTP response lands. This fires on **every connected client** for **every quick race by anyone**.
4. Any bet in that modal fails: pool already closed → `placeBet` returns "betting is closed…" (rejection path inside `placeBet`, `server.go:7312+`).
5. Nothing closes the modal: `betting_pool_closed` only shows a toast (`index.html:5192-5194`); a zero-bet pool is **deleted without a `betting_resolved` broadcast** (`server.go:7431-7435`), and `onBettingResolved` (the only handler that repurposes the modal) never fires.

Net effect: click QUICK RACE → screen goes dark with a "PLACE YOUR BETS" modal whose bets always error, race runs invisibly behind it. This is the most likely origin of the live report.
- **Fix direction (S/M):** stop opening zero-window pools for synchronous races (drop `openBettingPool` from `handleQuickRace`, or give quick races a real pre-race window like exhibitions); on the SPA, never auto-open the modal for pools the current player didn't ask to bet on (make `betting_pool_opened` a toast + a "BET" button), and close/disable the modal on `betting_pool_closed`.

### R-3 — Exhibition betting (the 60 s window feature) is unreachable from the SPA, and its race is invisible even via API — **[feature gap + feedback] Severity: HIGH — Size: M** — [FIXED: cdf82fe]

- The SPA **never calls** `POST /api/betting/pools` — grep of `web/index.html` finds only `POST /betting/pools/{raceID}/bet` (`index.html:9785`). There is no "open a pool"/exhibition UI anywhere, so the entire Phase-3 exhibition loop (`internal/server/server.go:7616-7673`) is dead code for UI players.
- Even through the API (reproduced end-to-end): pool opened at 03:56:04, bet ₵100 escrowed, pool auto-resolved at ~03:57:04 with payout ₵90 / house cut ₵10. But:
  - The pool JSON carries **no deadline/countdown field** — `models.BettingPool` (`internal/models/models.go:598-608`) has only `openedAt` (and a zero `closedAt` while open). A client cannot know when the race will run. The 60 s constant lives server-side only (`defaultBettingWindow`, `server.go:7677`).
  - `resolveExhibitionPool` (`server.go:7683-7744`) runs the sim but emits only a **one-line `chat_system` WS message** (7737-7742). It never calls `broadcastRaceReplay` and never calls `cacheRaceResult`, so `GET /api/races/{raceID}` → **404 "race not found or expired"** (verified). The race that decided your bet literally cannot be watched or fetched.
  - The `betting_resolved` broadcast from `resolveBets` reaches only clients that happen to be connected at that moment.
- **Fix direction (M):** add `closesAt` (openedAt + window) to the pool JSON; in `resolveExhibitionPool`, generate the indexed narrative, `cacheRaceResult`, and `go broadcastRaceReplay(...)` like tournament rounds do (`server.go:3414-3422`); add a minimal SPA surface (an "OPEN EXHIBITION POOL" button on the betting/race page listing `GET /api/betting/active` with countdowns).

### R-4 — PvP challenge is created and broadcast even when the 429 daily-cap error is returned — **[bug] Severity: MEDIUM — Size: S** — [FIXED: 1d65cc7]

`handleCreateChallenge` calls `s.createChallenge(...)` (which registers, persists, and broadcasts the pending challenge — `internal/server/server.go:4734`, storage/broadcast inside `createChallenge` at 4951-4971) **before** `consumeDailyRace` (`server.go:4740-4743`). At the cap the caller gets `429 no daily race entries left` and believes it failed, but:
- the defender sees a live incoming challenge (verified: created challenge `9e03df5a…` while at 0 races left, HTTP 429 returned, challenge listed as `pending`);
- the defender can accept it and the race **runs anyway** (verified — accept path has no daily-race check at all, `server.go:5092+`), so the cap is bypassable;
- a retry by the challenger hits "you already have a pending challenge against X" (4925-4933), compounding confusion.
**Fix direction (S):** consume the daily race **before** `createChallenge` (mirroring quick race), refunding it if creation fails; decide whether accepting should consume the defender's daily race (currently free — document if intentional).

### R-5 — `first_challenge` achievement is never granted for CPU-arena challenges — **[bug] Severity: LOW — Size: S** — [FIXED: 1d65cc7]

The bot path returns at `internal/server/server.go:4745-4752`, before the `first_challenge` grant at 4756-4758. Solo players (whose only challenge option is CPU Arena) can never earn it — on a truly empty server it is unobtainable. **Fix (S):** grant before the bot branch or inside `runBotChallenge`.

### R-6 — PvP challenges expire after 5 minutes → asynchronous PvP never happens — **[design bug for low-pop servers] Severity: MEDIUM — Size: S** — [FIXED: 1d65cc7 — TTL now 24h (`challengeTTL`), SPA shows expiry countdown]

`ExpiresAt: now.Add(5 * time.Minute)` (`internal/server/server.go:4948`); a sweep marks pending challenges expired every 30 s (`challengeExpiryLoop`, 5470+), and acceptance of an expired challenge returns "challenge has expired" (5105-5110). With one or two players who are rarely online simultaneously (production has exactly one, 'bees'), every PvP challenge dies unanswered — the challenger later sees it silently gone from "outgoing". **Fix (S):** raise expiry to hours/days (it's persisted now, Phase 4), and surface expiry countdown in the SPA cards (`index.html:9527-9557`).

### R-7 — Tournament round races are never shown, even when a round runs — **[feedback] Severity: MEDIUM — Size: S** — [FIXED: 07f5e05]

`runTournamentRound` (`web/index.html:8943-8963`) discards the response — which contains the full `race`, `narrative`, `narrative_indexed`, `weather` (`server.go:3452-3458`) — and prints only "Round complete!". The replay *is* broadcast tick-by-tick over WS (`server.go:3414`, 50 ms/tick), but that renders on the **#race page**, which the user is not on. Result: the organizer runs a round and no horses visibly race. **Fix (S):** after a round, navigate to `#race` and call `simulateLocalRace(...)` with the returned payload, exactly like the CPU-challenge flow does (`index.html:9613-9622`).

### R-8 — "RUN NEXT ROUND" is offered to everyone but is organizer-only — **[feedback] Severity: LOW — Size: S** — [FIXED: 07f5e05]

Server enforces organizer-or-`mojo` (`internal/server/server.go:3317-3327`, 403); the SPA button renders for any viewer (`index.html:8901-8905`, currently unreachable due to R-1 anyway). After fixing R-1, hide the button unless `tournament.created_by === currentUser.id`, else non-organizers get "only the tournament organizer can run tournament rounds" and read it as "races are broken."

### R-9 — Injured horses can race and train but cannot rest — **[bug] Severity: MEDIUM — Size: S** — [FIXED: 447b9b7 — rest always works and heals (1 rest day = 1 recovery unit), injured horses cannot race/train, daily turns only spend on success]

- No race path checks `Injury` or `Fatigue`: `resolveHorses` blocks only `Retired` (`server.go:1652-1654`); quick race picks `firstBestActiveHorse` ignoring injury/fatigue (2184-2202). Injured horses run (slower) and their injury heals by racing (`RacesLeft--`, 1848-1855). So injuries never make horses "not race" — but see next point.
- `handleRestHorse` (`server.go:1162`) delegates to `trainussy.Train(horse, WorkoutRecovery)`, and `Train` rejects injured horses outright (`internal/trainussy/trainussy.go:161-163`, "cannot train an injured horse"). So the *safe* option is denied to injured horses while the risky ones are allowed.
- Worse: `handleTrainHorse` consumes the daily training **before** validating anything (`server.go:1002` precedes the injured 50 % worsen branch at 1009, body validation at 1055-1072, and `Train`'s own rejections at 1074-1078). Training an injured horse is: 50 % → injury worsens (turn consumed), 50 % → `400 cannot train an injured horse` **and the daily train is still consumed**. Invalid `workoutType` also burns a turn.
**Fix (S):** validate body + injury before `consumeDailyTrain` (or refund on error, the C-8 pattern); let `WorkoutRecovery` bypass the injury rejection (resting an injured horse is sensible); consider a fatigue warning (not a block) in the SPA pre-race.

### R-10 — Complete race-gate matrix (for reference) — [reference only; note: injury now gates every player race path per R-9, and the House of USSY is excluded from the SPA challenge dropdown]

| Trigger (SPA → endpoint) | Gates, in order | Status/body | Does the SPA show it? |
|---|---|---|---|
| QUICK RACE btn → `POST /api/races/quick` (auth) | daily cap (`server.go:1563`); stable w/ ≥1 active horse (1568-1572); best-horse resolution (1574-1578) | 429 `no daily race entries left; check back after reset`; 400 messages | `alert()` with raw `HTTP 429: {json}` (`index.html:8112`) — visible but ugly |
| QUICK RACE (guest) | ≥2 non-retired horses in world (1606-1608) | 400 | alert |
| START RACE btn → `POST /api/races` | ≥2 horses (1456); purse ≥ 0 (1460); own ≥1 entrant (1479, **403 via `http.Error`, plain text**); guest+purse (1483); valid track (1493); all horses exist & non-retired (1499-1503, 404); sufficient cummies (1516-1519); daily cap w/ refund (1526-1537) | 4xx JSON (except 403 plain text) | inline `#race-create-status`, good (`index.html:8077-8083`) |
| Challenge form → `POST /api/challenges` | auth; fields; self-challenge; ownership; retired; defender exists ("player \"X\" not found" — note: picking **House of USSY** from the dropdown always fails this way, `index.html:9462` doesn't exclude system stables); wager ≤ balance; no dup pending; **then** daily cap (R-4) | 400/429 | inline msgEl, good |
| Accept challenge → `POST /api/challenges/{id}/accept` | pending; not expired (5 min, R-6); is defender; owns horse; not retired; both can cover wager | 400 | toast, good |
| Tournament ▶ → `POST /api/tournaments/{id}/race` | exists; organizer (403); ≥2 registered; ≥2 non-retired; not finished | 4xx | **unreachable — R-1** |
| Betting `POST /api/betting/pools` | auth; raceID + ≥2 horses; no existing pool (409) | — | **no UI — R-3** |
| Bet `POST /api/betting/pools/{id}/bet` | auth; amount 10–100,000; pool exists & open; ≤3 bets/user/race; balance | 400 | modal msg, good (but pool is always already closed for quick races — R-2) |

No race path gates on fatigue, injury, age, or breeding cooldown — those never block a race.

---

## Area 2 — Solo-player progression

### Reproduced solo session (fresh user `solo1`, offline mode, ~20 min)

| Step | Result |
|---|---|
| Register | Stable + ₵5,000 + 2 starter horses (fitness 0.44 / 0.42, ELO 1200). World contains 12 system legendary horses (fitness 0.65–1.00) in the House of USSY stable |
| Quick race ×6 | **Lost all 6** (placed 3rd once, else 4th–6th/last). Bot fields were clones of the legendaries; "E-008's Chosen" (fitness 1.00) won **all six races**. Income: **₵40 total**; house treasury debited ₵1,200 |
| Quick race #7 | 429 `no daily race entries left` — day's racing over in ~3 minutes |
| Bot challenge (wager 100) | 429 — challenges share the same daily race pool |
| PvP challenge at cap | 429 **but challenge created**; second account accepted it and the race ran (R-4) |
| Train (Sprint) | Works; fatigue 80 → 84.7 (no gate); +0.005 fitness (diminishing returns) |
| Breed | Works, free, produced foal; second attempt: 400 `on breeding cooldown (235 minutes remaining)` |
| Market / breeders / auctions / fights / trades | **All empty arrays.** Nothing to buy, no counterparty |
| Create fight | 400 `only retired horses can enter the arena` (also matches production: bees got `POST /api/fights 400` at 03:37:46) |
| Solo tournament (API only) | Created 2-round tournament, registered both own horses (per-stable multi-entry allowed, `tournussy.go:554-587`), ran both rounds, prize paid with 5 % burn: fees ₵200 → payout ₵190, **net −₵10**. Works — but only via curl (R-1) |
| Exhibition bet on own race (API only) | Sole bettor: bet ₵100 → payout ₵90. Solo betting is a guaranteed −10 % (or −100 %) sink |
| Casino | Daily grant 40 chips (house-funded, = ₵400 at cashout rate 10); slots spin works (wager is per-line ×9 — error `insufficient casino chips` for wager 5 with 40 chips gives no hint of that, `internal/server/casino.go` cost calc) |
| Prestige | 65 XP from 6 losing races + challenge (tier 1 at 250 XP → ~3 days of full dailies) |
| End balance | ₵5,315 after everything (+₵315 net, of which ₵295 came from the human-vs-human challenge that a real solo player cannot have) |

### S-1 — Matchmaking pits fresh players against fitness-1.0 legendary clones forever — **[bug/balance] Severity: HIGH — Size: M** — [FIXED: 1bf439a — time-trial handicapped clones (0.82–0.97 pace band), own horses excluded, clone ELO pinned to player; measured 43–60% fresh-starter win rate, 6/10 over HTTP]

`pickBotOpponents` (`internal/server/server.go:6216-6244`) selects opponents purely by **ELO distance**, and `cloneBotHorse` (6183-6200) blends the clone's ELO toward the player (`0.7·own + 0.3·player`) but copies **fitness, genome, and traits untouched**. On a fresh server every horse sits at ELO 1200, so the "nearest" opponents are the 12 seeded legendaries (fitness up to 1.00, some with strong traits) — a ~2.3× base-speed advantage over a 0.43-fitness starter that race-day form variance (±15 %, clamped) essentially never overcomes. The legendaries' registry entries never race themselves (only clones do), so they stay pinned at 1200 while the player's ELO sinks — the gap in *stats* never narrows. Measured: 0 wins in 6 races; E-008's Chosen won 6/6.
Also: the player's *other* horse is cloned as an opponent (only `playerHorse.ID` is excluded, 6223), which reads as "I'm racing against my own horse."
**Fix direction (M):** handicap clones toward the player's effective strength (scale `CurrentFitness`/specialties toward the player's, or matchmake on `CalcBaseSpeed`/fitness ceiling rather than ELO); exclude all horses owned by the requesting user from the template pool; optionally seed a tier of deliberately weak "local circuit" bot templates for sub-1200 players.

### S-2 — The whole daily loop is ~10 minutes; caps gate every race mode — **[design] Severity: MEDIUM — Size: M (product decision)** — [FIXED: 1bf439a — caps raised 6→10 races and 6→10 trains; tournaments (now UI-reachable per R-1) and exhibition betting remain uncapped]

6 races + 6 trainings per UTC day (`defaultDailyTrains/Races`, `server.go:6095-6096`; reset at UTC midnight, 6137-6144). Quick races, custom races, and challenge *creation* all draw from the same 6 (`server.go:1526`, `1563`, `4740`). After that the only remaining verbs are: rest (free, uncapped, `handleRestHorse` never calls `consumeDailyTrain` — note the asymmetry), breed (every 4 h), casino, glue, daily reward. Racing is also the only prestige-XP faucet reachable solo (10–40 XP/race, `server.go:1826-1838`; +15 for a bot-challenge win, 5066). Combined with S-1 (you lose everything anyway) the felt experience is "log in, lose 6 races in 3 minutes, nothing else to do." Tournament rounds notably do **not** consume daily races (`handleTournamentRace` never calls `consumeDailyRace`) — once R-1 is fixed, solo tournaments become the actual grind loop (2 entrants minimum, self-funded prize pool minus 5 % burn).
**Fix direction:** raise/regenerate the caps (e.g. +1 race per 2 h), or exempt house-funded bot content from the cap while keeping it on purse-carrying custom races.

### S-3 — Solo income is dominated by login/casino stipends, not gameplay — **[balance] Severity: MEDIUM — Size: M** — [FIXED: 1bf439a — bot purse shares recycle to the house treasury (leak closed), and the House of USSY keeps a rotating 2-stud legendary catalogue listed (Sappho-priced, 3 covers, hourly re-stock) so cummies buy better genomes solo]

Verified daily faucets for a losing solo player: daily reward ₵150–700 escalating with streak (`server.go:6072-6080`), casino grant 40 chips ≈ ₵400 at cashout (house-funded), race placements ≈ ₵0–100. Sinks: heal costs, exchange haircut, betting cut, tournament burn. A solo player *accumulates* cummies slowly but has **nothing to spend them on** (market/auctions empty — S-4), so the money loop is inert: you can't buy a better horse, and races don't yield one. Progression therefore reduces to breed-every-4h + train-6/day toward better genomes — days of real time before a competitive horse exists, with zero wins in between (S-1).
Also an economy note: quick-race purses debited from the house (`server.go:1589`) are **destroyed** when bot clones place ( `if earnings > 0 && !isBotHorse(horse)` at 1822 skips crediting, and nothing refunds the house) — with mostly-losing players ~₵160–200 of every ₵200 purse evaporates; ~₵1,200/player/day of pure deletion from the treasury. Consider crediting unclaimed shares back to the house (S).

### S-4 — Systems that are hard-blocked solo (and what bot/house support exists) — [PARTIALLY ADDRESSED: house stud catalogue (1bf439a) unlocks the market row; CPU Arena has a dedicated race-page button; betting pools are SPA-reachable with countdowns (cdf82fe). Trades/auctions/fights/seasons/alliances house-fill-ins remain future work — each is an M-sized design decision beyond this pass]

| System | Why it's dead solo | Bot/house fill-in available? |
|---|---|---|
| Stud market (`/api/market`) | Empty; self-purchase correctly blocked (C-6) | **House of USSY owns 12 legendaries and never lists them.** Easiest unlock: have the house auto-list a rotating stud (house already has treasury plumbing). The seeded breeder program (`/api/breeders`) is also empty — no system stallions are registered at boot (`handleListBreeders` returned `[]`) |
| Trades | Needs a counterparty; nothing lists | none |
| Auctions | Can create, nobody bids; expiry returns the horse | House sniping/min-bid buyout would be M work |
| Horse fights | Requires **retired** horses (`server.go:10210`, `10338`) *and* a second player to join within 30 min or the fee refunds (`pendingFightMaxAge`, 7053). Production 'bees' hit exactly this 400 | A house-fielded retired gladiator on join-timeout would be M work |
| PvP challenges | Nobody to accept within the 5-minute expiry (R-6) | **CPU Arena exists and is discoverable** in the challenge dropdown (`index.html:9461`) — the one well-wired bot system (house-funded purse ₵250 + wager matching). Still subject to daily cap and S-1 difficulty |
| Betting pools | SPA can't open them (R-3); quick-race pools close in 0 ms (R-2); solo pari-mutuel betting is self-defeating (−10 % guaranteed) | Needs house bettors or fixed odds to mean anything solo |
| Seasons | `POST /api/seasons/end` and `/api/advance-season` are admin-`mojo`-only (`server.go:4116-4122`, `4454-4458`). No timer advances seasons; on a mojo-less server they never end → season rewards unreachable | Auto-advance on wall-clock (M) |
| Alliances | Creatable (₵500) but pointless alone | — |
| Achievements | Race/breed ones reachable; `first_challenge` broken solo (R-5); `first_trade`/`market_mogul` need counterparties; `betting_winner` reachable only via the API exhibition self-bet; `tournament_winner` blocked by R-1 | — |
| ELO ladder / leaderboard | Player sinks below the permanently-1200 house legendaries (their registry entries never race, S-1) — leaderboard perpetually headed by the house | Have clones write back results to templates, or decay house ELO |

Note the log-spam confirmation of S-1's clone design: every quick race logs `server: failed to update stats for horse bot:<uuid>: not found` from `server.go:1774-1776` — harmless but noisy; guard with `isBotHorse` (S).

### S-5 — Production corroboration ('bees', 2026-07-16 03:35–03:40 UTC) — [observational; the walls bees hit are addressed by R-9 (injury/rest), S-1/S-2 (races), and the fights gate is unchanged by design (retired-only)]

journalctl shows bees hitting exactly these walls: `POST /api/breed 400` (03:36:09 — breeding cooldown wording is only in the response body), `POST /api/fights 400` (03:37:46 — retired-only gate), long stretches of read-only browsing (`/api/trades` polled 12× in one second at 03:36:36 — the trades page has nothing to show a solo player and appears to be re-fetching in a loop; worth a look at `loadTrades` callers, possible S), then daily reward claim at 03:39:21. No races run in that window — consistent with cap exhaustion or R-2 confusion.

---

## Area 3 — NaN/Inf sources reaching JSON

### N-1 — The 03:39:08 production NaN is the already-fixed `/api/market` SapphoScore, on a stale binary — **[ops, not code] Severity: HIGH (until restarted) — Size: S** — [OPS: no code change needed; a freshly built ./stallionussy binary (including all fixes in this pass) is in place — restart the service to pick it up. The running process was deliberately not touched]

Evidence chain:
1. `writeJSON` logs `failed to encode JSON response` **inside** the handler (`internal/server/server.go:5784-5790`); `loggingMiddleware` logs the `METHOD path status` line **after** the handler returns (5829-5837). Therefore the encode-error line immediately precedes the request line of the **same** request.
2. journalctl 03:39:08: `server: failed to encode JSON response: json: unsupported value: NaN` → next line `GET /api/market 200 54µs` → *then* `GET /api/prestige 200 35µs`. Same pattern at 03:36:32 (also followed by `GET /api/market 200`). The NaN belongs to `/api/market`; `/api/prestige` was a coincidental neighbor. `/api/prestige` contains no computed floats at all — only constant tier fields and integer XP (`server.go:6463-6506`, tiers 6083+).
3. The NaN source is `CalcSapphoScore` returning `math.NaN()` for E-008's Chosen / LotNumber 6 (`internal/marketussy/marketussy.go:289-293`) — deliberately, per lore. Fixed in `56da38e` by marshaling `models.SapphoScore` NaN as JSON `null` (`internal/models/models.go:351-369`).
4. Production process 4000359 started **03:23:45**; commit/build of the fix happened **03:37** — the fixed binary is on disk (`/home/mojo/projects/stallionussy/stallionussy`, mtime Jul 16 03:37) but the process was never restarted. Every `/api/market` request while an E-008 listing is active returns truncated JSON (header 200 already sent), which the SPA surfaces as a market page that fails to load.
**Fix: `systemctl restart stallionussy`.** No further code change needed for this incident. (Also noted at 03:23:03-03:23:40: a four-restart crash loop on `pq: column "casino_chips" does not exist` from a pre-`bb7e0d2` binary — the retrofit commit fixed it; mentioning for timeline completeness.)

### N-2 — Offline/SQLite cannot persist the NaN Sappho score: listing silently lost on restart — **[bug, latent] Severity: LOW — Size: S** — [FIXED: 5e7215f — SapphoScore implements Valuer/Scanner with a -1 sentinel for NaN; no schema change needed, works on existing offline DBs]

Empirically verified with the repo's driver version (modernc.org/sqlite v1.53.0): **binding `math.NaN()` stores NULL**, and the SQLite schema declares `sappho_score … NOT NULL` (`internal/repository/migrations_sqlite.go:129`), so `CreateListing`/`UpdateListing` (`internal/repository/postgres/market.go:39`, `:137`) fail with `NOT NULL constraint failed` for an E-008's Chosen listing. `persistListing` only logs (`server.go:6585-6592`), so the listing works in memory and **vanishes on restart**. Even if the column were nullable, the scan `&l.SapphoScore` (`postgres/market.go:66`, `:105`) errors on NULL (`converting NULL to float64` — `models.SapphoScore` has `UnmarshalJSON` but no `sql.Scanner`). Postgres is unaffected (float8 stores NaN natively).
**Fix (S):** give `models.SapphoScore` `Value()/Scan()` methods mapping NaN↔NULL, and drop NOT NULL on the SQLite column (fresh-start schema, per Phase-4 notes) or store a sentinel.

### N-3 — Systematic audit of every other float reaching JSON: all guarded (verified list) — [no action needed]

| Site | Division / risk | Guard |
|---|---|---|
| `tournussy.go:229-232` (`/api/horses/{id}/stats`) | winRate, avgPlace over TotalRaces | `if stats.TotalRaces > 0`; zero-race horse verified live → all zeros |
| `server.go:4307-4315` (`/api/leaderboard`) | avgELO over len(horses), winRate over totalRaces | both guarded; empty-stable verified 200 |
| `server.go:4383-4386` (`/api/leaderboard/horses`) | winRate over `h.Races` | guarded |
| `server.go:4490-4493` (season end) | avgELO | guarded |
| `server.go:7553-7568` (`calcOddsLocked`, pool JSON) | odds = TotalPool/TotalBet | zero-pool and zero-bet both short-circuit to 0 |
| `casino.go:704-706` (slot spin) | multiplier = payout/cost | `if totalCost > 0` |
| `marketussy.go:307-310` (Sappho winRate) | wins/races | guarded (the NaN is the deliberate LotNumber-6 branch only) |
| `marketussy.go:353-364` (`ELOUpdate`) | `1/(1+10^x)` | cannot NaN/Inf-divide for finite ELO; no NaN ELO source exists (retrofit defaults are 0/1200, not NaN) |
| `pedigreussy.go:320-323, 358, 375` (`/api/horses/{id}/dynasty`) | AverageELO, legendary density over TotalHorses | `len(horses)==0` early-returns at 320-323 |
| `pedigreussy.go:135-137, 148-150, 153` (inbreeding) | shared/totalSlots | zero-slot early returns |
| `trainussy.go:366-369` (fitness gain) | fitness/ceiling | `ceiling <= 0` returns 0 |
| `racussy.go:406` (`1.0/stmScore`) | missing gene | `geneScore` defaults 0.3 for absent genes (`racussy.go:119-124`), never 0 |
| `racussy.go:802` (`overshoot/deltaP`) | finish interpolation | only reached when `deltaP > 0` (position advanced past the line) |
| Prestige/progress/daily endpoints | integers + constant tier floats only | n/a — verified clean live |

No `math.Log/Sqrt/Pow`-based NaN paths exist outside `ELOUpdate`. Conclusion: after a production restart onto `56da38e`, **no known NaN/Inf can reach `json.Encoder`**; the one residual is the offline persistence gap (N-2).

---

## Appendix — reproduction environment

- Binary built from `56da38e` (`go build ./cmd/stallionussy`), run as `STALLION_OFFLINE=true STALLION_DB_PATH=<scratch>/diag.db ./stallionussy-diag serve --port 8299` from the repo root; killed after the session (port verified closed).
- Accounts: `solo1` (primary), `bees2` (only to prove R-4's cross-player visibility). All API calls via curl with Bearer tokens.
- Production inspection was read-only `journalctl -u stallionussy` plus `ps`/binary mtime; no production state was touched.
