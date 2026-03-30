# Handoff

## Current State

This repo was re-audited and tightened around the main product-breakers.

## Fixed In This Pass

- New authenticated users now get a stable with 2 starter horses.
- Existing empty authenticated stables are backfilled with starter horses on boot.
- Authenticated users with an empty stable can now claim a one-time emergency starter recovery grant from the stable page.
- Authenticated users are restricted to a single stable, matching current server ownership assumptions.
- First-time authenticated players now get a skippable interactive tutorial in the SPA covering stable, horse detail, training, racing, breeding, market, advanced competition, and replay/share.
- Tutorial can be replayed from the in-app help button and persists completion/skip state in browser local storage.
- Race history/replay/share flow was repaired:
  - removed broken `apiFetch` usage
  - fixed replay visualizer tick wiring
  - reduced duplicate local-vs-WS race playback for the initiating client
- Challenge and betting flows were tightened further:
  - challenge list endpoint now returns history states instead of only pending challenges
  - challenge completion broadcasts now include winner name for the SPA
  - challenge acceptance can auto-pick a horse server-side and now surfaces the returned race in the SPA
  - betting resolution payload now matches the SPA expectation
  - SPA betting now sends `horseID` correctly
- Added a first-pass lore/help system to the SPA:
  - new `#lore` codex page in `web/index.html`
  - lore/help data centralized in `LORE_HELP` and `LORE_CODEX`
  - bottom-right help area now links to both tutorial replay and lore codex
  - inline lore tooltips added on race, horse detail, prestige, market, achievements, challenges, and betting surfaces
  - routing/export wiring added for `openLore()` and `loadLorePage()`
- Lore terminology was normalized to reduce drift:
  - `Sappho Score` = numeric bloodline quality signal shown in UI
  - `Sappho Scale` = broader social/institutional ideology around elite bloodstock
  - `Geoffrussy` = platform governance/compliance authority, not a random one-off joke label
  - `B.U.R.P.` = Bureau of Unexplained Racing Phenomena, responsible for anomaly incident framing
- Progression and pacing were reworked to make solo play viable from onboarding through mid-game:
  - authenticated players now consume real daily train and race actions instead of dead placeholder counters
  - `/api/progress` and `/api/prestige` now return SPA-usable progression fields
  - daily login rewards were flattened into a predictable 7-day loop instead of compounding exponentially
  - prestige thresholds were pulled forward for earlier account growth and horse-cap expansion
  - race prestige XP was normalized around placement instead of only win-heavy spikes
- Async fallback opponents were added without a new subsystem:
  - authenticated quick races now auto-fill with matched synthetic CPU entrants
  - underfilled authenticated custom races are padded with CPU entrants to keep progression moving
  - challenge creation now supports a `CPU Arena` opponent for immediate 1v1 resolution when concurrency is low
  - synthetic opponents are simulation-only and do not persist, earn cummies, gain prestige, or pollute race history/leaderboards
- Breeding progression guardrails were unified:
  - stud-market breeding now enforces prestige stable-cap and breeding cooldown rules
  - breeder-stallion breeding now enforces the same cap/cooldown checks before charging fees
- Player progression and season state now persist server-side:
  - `PlayerProgress` daily limits, login streaks, prestige XP, and related counters are written through to Postgres
  - seasons are loaded from Postgres on boot and season rollover now updates the archived and active season records
- The SPA shell and chat layout were tightened:
  - desktop now uses a fixed-height two-pane layout with a persistent left chat rail and a dedicated scrolling main-content pane
  - mobile chat now opens as a clearer drawer with close/backdrop handling instead of relying on page scroll behavior
- Gameplay balance and QoL were tightened in a follow-up pass:
  - active horse slots now exclude retired horses, reducing cap pressure on breeding, retirement, and breeder assignments
  - quick race auto-picks the best active owned horse rather than the first one in roster order
  - race purse distribution is flatter and streak multipliers are less top-heavy, so deeper placements still move a roster forward
  - training fatigue penalties start earlier and rest recovery is less extreme, which pushes healthier horse rotation
  - destructive loops like glue now protect the last active horse from irreversible account-bricking mistakes
- Added a first-pass casino subsystem designed around async-safe social gambling rather than a real-time minigame stack:
  - stables now track `casino_chips` separately from cummies
  - players can exchange cummies into chips with a protected cummies floor to avoid hard-bricking core progression
  - a new `#casino` SPA page exposes chip exchange, slots, and async draw poker
  - poker is simplified to persistent five-card draw tables with one buy-in, one draw phase, and server-side showdown resolution
  - slots use server-authoritative outcomes and persist recent spin history
- Added a departed-horse / rare return system tied directly to destructive outcomes:
  - horses lost in fatal arena fights or the glue factory are now written to a departed-horse ledger
  - ledger records persist in Postgres as horse snapshots plus omen state
  - rare omens can surface later and be claimed from the glue/ledger UI
  - returned horses come back as altered roster members with reduced efficiency, changed lore, and permanent anomalous traits instead of a full undo
- Stud-market breeding now requires an explicit owned mare on both client and server.
- Tournament registration now validates horse/stable ownership before charging and only deducts entry fees after successful registration.
- Multiple frontend selectors were restricted to owned horses/stables for action-taking pages.
- Guest quick race now works under auth middleware, matching the SPA guidance.
- Auth-backed users with auto-created stables no longer see an actionable create-stable dead end in the stable page UI.
- Added backend tests for starter-horse seeding and one-stable-per-user behavior.

## Validation

- `go test ./...`
- `go vet ./...`
- `go build ./...`

All passed after the fixes in this pass.

## Important Product Rules

- Authenticated user: one stable only.
- New stable: seeded with 2 starter horses.
- Empty owned stable: may claim one emergency replacement starter pair.
- Retired horses do not count against active prestige horse-slot limits.
- Casino chips are the preferred gambling currency; cummies can be staked only through the explicit casino exchange / table rules.
- Rare return events are exceptions, not a reusable revive economy: they come from the departed ledger and permanently change the horse.
- First-time authenticated session: tutorial is offered once by default, can be skipped, and can be replayed later.
- Replay share links: `#replay/{raceID}`.
- Guest quick race is allowed.
- Server startup requires Postgres.

## Remaining Caveats

- Frontend still has no automated browser/API integration coverage.
- Tutorial persistence is client-side only; it is not yet stored server-side per account.
- Lore/codex content is currently SPA-local data, not server-backed content.
- Challenges and betting pools are still in-memory and do not yet survive server restart.
- The poker implementation is intentionally shallow: no hold'em, no side pots, no per-action timers, and no collusion detection beyond the ring-fenced economy design.
- `docker-compose.yml` still does not provide Postgres.
- `devplan.md` is now historical, not an accurate live delivery tracker.

## Follow-up Fix

- The post-casino SPA regression that caused a black screen was fixed by removing a stale `window.SU.claimStarterRecovery` export that no longer had a matching function implementation.
- Routing now falls back to `#home` on unknown hashes instead of leaving the app with no active page.
- Stable detail is now route-addressable as `#stable/{id}`, which lets the owned empty-stable recovery panel survive refresh/navigation.
- Owned empty stables once again surface the emergency starter recovery CTA instead of incorrectly pushing players toward breeding with zero horses.
- Slot spins now support the normal `POST` path plus a compatibility `GET` variant on the same route, reducing 405 dead ends while keeping auth intact.
- Casino/departed follow-ups were tightened during the repair:
  - daily casino chip grants are now applied consistently from the casino overview path
  - poker table payloads now redact opponent hands until settlement
  - departed-horse omens now expire and returned horses respect active-capacity limits before re-entry
