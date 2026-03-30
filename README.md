# StallionUSSY

Go monolith for a browser-based horse breeding, racing, trading, and chaos simulator.

## What Works

- Authenticated registration creates one stable per user.
- New authenticated stables are seeded with 2 starter horses so users can immediately race and breed.
- Existing empty user stables are backfilled with starter horses on server boot.
- Authenticated users who end up with an empty stable can claim a one-time emergency starter pair from the stable page.
- Owned empty stables now show an explicit recovery panel instead of sending zero-horse players into breeding dead ends.
- First-time authenticated players are offered a skippable interactive tutorial that walks the main gameplay loop.
- Race replay links use `#replay/{raceID}` and recent replays are available via `/api/races/recent`.
- Stud-market breeding now requires an explicit owned mare.
- Quick races open a short betting window before the race starts.
- Bets and challenges now use authenticated ownership checks end to end.
- Challenge history and betting resolution now render correctly in the SPA.
- The SPA now includes an in-world lore codex page plus inline lore tooltips on core racing, genetics, progression, market, achievement, challenge, and betting surfaces.
- Progression now has a real daily action loop for authenticated players: training and race entries are limited per day and surfaced in the dashboard.
- Authenticated quick races now guarantee solo progression by auto-filling the field with matched computer-controlled opponents when needed.
- The challenge page now supports a `CPU Arena` fallback for immediate 1v1 progression when no other player is available.
- The SPA now uses a fixed desktop app shell: the left chat column stays pinned while the right content pane scrolls independently.
- Quick Race now auto-selects your strongest active horse instead of the first available one.
- The SPA now includes a `CASINO` page with ring-fenced casino chips, async five-card draw poker tables, and slot machines.
- Horses destroyed in fatal fights or sent to the glue factory now enter a departed-horse ledger and can trigger rare return omens with altered traits and lore.

## Current Rules

- Authenticated users get exactly one stable.
- Each stable gets its initial starter pair plus at most one manual emergency starter recovery grant.
- Guests still use guest-mode client behavior and do not get persistent auth-backed onboarding, but they can use `GET /api/races/quick`.
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
- Slot spins accept the main authenticated `POST` path and a compatibility `GET` fallback on the same endpoint to avoid dead-end method mismatches in the SPA.
- Async poker is lightweight five-card draw: one buy-in, one draw phase, no live side pots, no websocket-dependent turn engine.
- Casino chip exchange enforces a protected cummies floor so gambling cannot bankrupt a stable below basic operating capital.
- Departed horses do not freely revive. They enter a dormant ledger, may surface a rare omen later, and return permanently altered with reduced efficiency and anomalous traits.

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
