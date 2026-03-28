# Stable Alliances, Horse Aging/Injury, & Random Events — Dev Plan

## Feature 1: Stable Alliances / Guild System

- [✅] Step 1: Models + Migration + Repo Interface — Add Alliance, AllianceMember, Injury, RandomEvent structs to models.go; add alliances table + injury column to migrations.go; add AllianceRepository + Injury field to Horse in repository.go
- [✅] Step 2: Postgres Alliances Repo — Create alliances.go with full CRUD implementation
- [✅] Step 3: Postgres Horses Repo — Add injury JSON column handling to horses.go (scan/create/update)
- [✅] Step 4: Server Alliance Handlers — Add allianceRepo field, 8 HTTP handlers, route registration, loadFromDB for alliances, persistAlliance helper
- [✅] Step 5: Alliance Chat Commands — Add /alliance create/join/leave/donate commands to OnChatCommand switch

## Feature 2: Horse Aging, Injury, and Retirement Lifecycle

- [✅] Step 6: Injury + Age Handlers — Add POST /api/horses/{id}/heal, GET /api/horses/{id}/age-info endpoints; injury logic after races; training-while-injured logic

## Feature 3: Random Events System

- [✅] Step 7: Random Events — Add rollRandomEvent() with 20 events, integrate into runRace, WebSocket broadcast

## Integration & Frontend

- [✅] Step 8: Frontend — Alliances nav tab + list/detail/create views, age badges, injury display, heal button, random event toasts
- [✅] Step 9: Build Verification — Run go build ./... and fix any compilation errors
