# Handoff

## Current State

This repo was re-audited and tightened around the main product-breakers.

## Fixed In This Pass

- New authenticated users now get a stable with 2 starter horses.
- Existing empty authenticated stables are backfilled with starter horses on boot.
- Authenticated users are restricted to a single stable, matching current server ownership assumptions.
- Race history/replay/share flow was repaired:
  - removed broken `apiFetch` usage
  - fixed replay visualizer tick wiring
  - reduced duplicate local-vs-WS race playback for the initiating client
- Stud-market breeding now requires an explicit owned mare on both client and server.
- Tournament registration now validates horse/stable ownership before charging and only deducts entry fees after successful registration.
- Multiple frontend selectors were restricted to owned horses/stables for action-taking pages.
- Added backend tests for starter-horse seeding and one-stable-per-user behavior.

## Validation

- `go test ./...`
- `go vet ./...`
- `go build ./...`

All passed after the fixes in this pass.

## Important Product Rules

- Authenticated user: one stable only.
- New stable: seeded with 2 starter horses.
- Replay share links: `#replay/{raceID}`.
- Server startup requires Postgres.

## Remaining Caveats

- Frontend still has no automated browser/API integration coverage.
- `docker-compose.yml` still does not provide Postgres.
- `devplan.md` is now historical, not an accurate live delivery tracker.
