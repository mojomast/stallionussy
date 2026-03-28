# Horse Retirement Paths — Dev Plan
# (Gladiatorial Combat, Glue Factory, Dedicated Breeding Stallions)

## Step 1: Fight Engine + Models + Migrations + Repository Interfaces + Postgres Implementations
- [✅] Create internal/fightussy/fightussy.go — full gladiatorial combat simulation engine
- [✅] Add HorseFight, GlueFactoryResult, BreedingStallion models to models.go
- [✅] Add horse_fights, glue_factory, breeding_stallions tables to migrations.go
- [✅] Add HorseFightRepository, GlueFactoryRepository, BreedingStallionRepository interfaces to repository.go
- [✅] Create internal/repository/postgres/fights.go (fight repo implementation)
- [✅] Create internal/repository/postgres/glue.go (glue factory repo implementation)
- [✅] Create internal/repository/postgres/breeders.go (breeding stallion repo implementation)
- [✅] Wire up NewFightRepo, NewGlueFactoryRepo, NewBreedingStallionRepo constructors
- [✅] Verified: go build ./... and go vet ./... pass cleanly

## Step 2: StableManager RemoveHorse + Server Backend
- [✅] Add RemoveHorse method to stableussy.go for permanent horse deletion
- [✅] Add fight/glue/breeder repo fields to Server struct, wire in NewServer, add pending fights map
- [✅] Add ~14 routes for fights/glue/breeders endpoints
- [✅] Implement ~12 handler functions (createFight, joinFight, getFight, listFights, sendToGlue, listGlueHistory, assignBreeder, listBreeders, getBreeder, breedWithStallion, retireBreeder, getFightReplay)
- [✅] Add /fight and /joinfight chat commands

## Step 3: Frontend (Fights, Glue Factory, Breeding Stallions)
- [✅] Add 3 new nav tabs (FIGHTS, GLUE, STUDS) to web/index.html
- [✅] Implement fight page with create/join/replay UI
- [✅] Implement glue factory page with horror aesthetic
- [✅] Implement breeders section with stud listing
- [✅] Add WebSocket event handlers for fight/glue/breeder events
- [✅] Add themed CSS for all three sections
- [✅] Add JavaScript functions: loadFights, createFight, joinFight, viewFightReplay, loadGluePage, sendToGlue, loadStuds, assignBreeder, selectStallion, breedWithStallion, retireBreeder
- [✅] Wire navigate() switch cases for fights/glue/studs pages
- [✅] Add bindEvents() button handlers for fight/glue/breeder buttons
- [✅] Expose functions in window.SU for inline onclick handlers

## Step 4: Build Verification
- [✅] Run go build ./... and go vet ./... to verify compilation
- [✅] All clean — no errors
