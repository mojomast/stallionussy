# StallionUSSY

Go monolith for a browser-based horse breeding, racing, trading, and chaos simulator.

## What Works

- Authenticated registration creates one stable per user.
- New authenticated stables are seeded with 2 starter horses so users can immediately race and breed.
- Existing empty user stables are backfilled with starter horses on server boot.
- First-time authenticated players are offered a skippable interactive tutorial that walks the main gameplay loop.
- Race replay links use `#replay/{raceID}` and recent replays are available via `/api/races/recent`.
- Stud-market breeding now requires an explicit owned mare.
- Quick races open a short betting window before the race starts.
- Bets and challenges now use authenticated ownership checks end to end.
- Challenge history and betting resolution now render correctly in the SPA.

## Current Rules

- Authenticated users get exactly one stable.
- Guests still use guest-mode client behavior and do not get persistent auth-backed onboarding, but they can use `GET /api/races/quick`.
- Shared race replays are public GET endpoints and are retained in cache/DB for about 7 days.
- Custom race purses are funded from the authenticated creator's stable balance and cannot be minted from arbitrary client input.
- Tutorial state is currently persisted in browser local storage and can be replayed from the in-app help button.

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
