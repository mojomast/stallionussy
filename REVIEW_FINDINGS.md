# Deep Code Review: Gameplay & Business Logic Findings

**Scope:** Domain/logic packages only — `racussy`, `genussy`, `fightussy`, `trainussy`, `stableussy`, `marketussy`, `tournussy`, `pedigreussy`, `models`  
**Focus:** Concrete bugs, logic errors, and exploitable imbalances  
**Date:** 2026-03-30

---

## Finding 1: PurchaseBreeding does not deactivate listing — infinite purchase exploit

**Severity:** CRITICAL  
**Type:** Bug / Security / Economic exploit  
**Confidence:** Certain  

**File:** `internal/marketussy/marketussy.go` lines 157–193  

**Summary:** `PurchaseBreeding()` validates that `listing.Active == true` but never sets `listing.Active = false` after a successful purchase. The listing remains active indefinitely, allowing the same stud listing to be purchased an unlimited number of times by different buyers (or the same buyer).

**Impact:** Infinite foal generation from a single stud listing. Combined with the economic flow (buyer pays cummies per purchase), this is either a money sink (if the buyer loses cummies each time) or an exploit vector for flooding the horse population. At minimum, it lets one stud produce unlimited offspring, which the breeder collects fees on forever without needing to re-list.

**Evidence:**
```go
// Line 165-166: checks Active
if !listing.Active {
    return nil, fmt.Errorf("marketussy: listing %q is no longer active", listingID)
}
// ... creates transaction ...
// Lines 189-192: returns without setting listing.Active = false
m.transactions = append(m.transactions, tx)
m.totalBurned += burnAmount
return tx, nil
```

**Recommended fix:** Add `listing.Active = false` before the return, or add a configurable `MaxBreedings` counter that decrements on each purchase and deactivates when exhausted.

**Test needed:** Yes — test that a second purchase of the same listing returns an error.

---

## Finding 2: Legendary horses with FitnessCeilingOverride > 1.0 are absurdly overpowered

**Severity:** CRITICAL  
**Type:** Bug / Game balance  
**Confidence:** Certain  

**File:** `internal/genussy/genussy.go` lines 369–370, 608–617  
**File:** `internal/racussy/racussy.go` lines 169–178  

**Summary:** Legendary horses E-008's Chosen (lot 6) and STARDUSTUSSY's Prophecy (lot 11) have `FitnessCeilingOverride` values of 9.99 and 8.88 respectively. `CreateLegendary()` sets `CurrentFitness = ceiling`, so these horses start with `CurrentFitness` far exceeding the normal 0–1 range. The race engine's `CalcBaseSpeed()` multiplies directly by `horse.CurrentFitness`:

```go
return speedScale * horse.CurrentFitness * geneticFactor  // line 177
```

A normal horse with perfect genetics and CurrentFitness=1.0 gets ~18 m/tick. E-008 gets ~180 m/tick — **10x faster**. This isn't "legendary" — it's mathematically impossible for any non-legendary horse to compete.

**Impact:** Any race containing E-008 or STARDUSTUSSY is a foregone conclusion. These horses also dominate tournaments, break ELO calculations, and make any betting pool involving them trivial. Normal-bred horses cannot get CurrentFitness above 1.0 (clamped in `Breed()`), so no amount of breeding or training can close the gap.

**Evidence:**
```go
// genussy.go:369-370
e008Ceiling := 9.99
stardustCeiling := 8.88

// genussy.go:617
CurrentFitness: ceiling,  // legendaries start at full fitness

// racussy.go:177
return speedScale * horse.CurrentFitness * geneticFactor
```

**Recommended fix:** Either:
1. Cap `CurrentFitness` at 1.0 for all horses in `CalcBaseSpeed`, or
2. Change the legendary override to apply a separate bonus multiplier rather than inflating `CurrentFitness` into an entirely different numeric range, or
3. Scale the override values to be within [0, 1] (e.g., 0.999 and 0.888)

**Test needed:** Yes — test that legendary horses produce sane `CalcBaseSpeed` values relative to non-legendaries.

---

## Finding 3: fatigue_resist trait grants permanent fatigue immunity (broken comparison)

**Severity:** HIGH  
**Type:** Bug  
**Confidence:** Certain  

**File:** `internal/racussy/racussy.go` lines 385–396  

**Summary:** The `fatigue_resist` trait check compares `fatigue` (a tiny per-tick accumulator, typically ~0.001–0.01 per tick) against `0.8 * distance` (80% of the track distance in meters, e.g., 640m for an 800m track). Since fatigue will always be vastly less than this threshold (even after thousands of ticks, fatigue might reach ~10, vs. a threshold of ~640), the condition `fatigue < 0.8*distance` is **always true** for any horse with this trait.

**Impact:** Any horse with the `fatigue_resist` trait has **zero fatigue for the entire race**, every race, unconditionally. This is a massive competitive advantage that likely wasn't intended. The comment says "skip fatigue calculation entirely" — but the comparison was probably intended to check whether the horse is past 80% of the *race progress*, not compare fatigue against distance.

**Evidence:**
```go
// Line 378: fatigue is a small float
fatigue := float64(tick) * 0.001 * (1.0 / stmScore)
// Even at tick 10000, fatigue ≈ 10.0 / stmScore ≈ 10–30

// Line 389: comparison against 80% of distance (e.g., 800m → 640.0)
if trait.Effect == "fatigue_resist" && fatigue < 0.8*distance {
    skipFatigue = true  // ALWAYS TRUE
```

**Recommended fix:** The comparison should likely be `position < 0.8*distance` (check whether the horse has completed less than 80% of the race), or `fatigue < 0.8` (use a fixed fatigue threshold), or simply reduce the magnitude of the fatigue reduction instead of zeroing it entirely.

**Test needed:** Yes — test with a high tick count that fatigue_resist doesn't zero fatigue in late race stages.

---

## Finding 4: stamina_boost trait interaction with fatigue_resist is broken

**Severity:** MEDIUM  
**Type:** Bug  
**Confidence:** Certain  

**File:** `internal/racussy/racussy.go` lines 442–452  

**Summary:** The `stamina_boost` trait is gated by `!skipFatigue` (line 445), meaning it only triggers when `fatigue_resist` is NOT active. But because `fatigue_resist` effectively always fires (Finding 3), `stamina_boost` never activates for horses that have both traits. Even when `fatigue_resist` is not present, the stamina_boost recalculation has an issue: `oldFatigue` and `adjustedFatigue` differ, and the deltaP adjustment adds back `(oldFatigue - adjustedFatigue)` — but `oldFatigue` may have already been modified by `cursed_fatigue` (lines 400–404), making the adjustment interact unexpectedly with cursed_fatigue.

**Impact:** Horses with both `fatigue_resist` and `stamina_boost` get no benefit from `stamina_boost`. Additionally, the "retroactive adjustment" approach is fragile — it depends on the order of trait processing and interacts poorly with other fatigue modifiers.

**Evidence:**
```go
// Line 445: skipFatigue is true when fatigue_resist fired (always, per Finding 3)
if trait.Effect == "stamina_boost" && !skipFatigue {
    // This block never executes for horses with fatigue_resist
```

**Recommended fix:** Consolidate fatigue modification into a single fatigue multiplier computed from all relevant traits, applied once, instead of the current retroactive adjustment pattern.

**Test needed:** Yes — test that stamina_boost affects deltaP when combined with other fatigue traits.

---

## Finding 5: Mace malfunction permanently reduces attack (labeled as temporary)

**Severity:** MEDIUM  
**Type:** Bug  
**Confidence:** Certain  

**File:** `internal/fightussy/fightussy.go` lines 628–639  

**Summary:** When a mace malfunction occurs (2% chance per tick), the code sets `atk.Attack *= 0.80` and comments "Reduce attack for next tick (temporary)". But there is no restoration mechanism — the attack reduction persists for the entire fight. Multiple malfunctions compound multiplicatively (two malfunctions → 64% attack, three → 51.2% attack, etc.).

**Impact:** Over a full fight (up to 500+ ticks at 2% chance each), expected ~10 malfunctions per fighter. This means fighters who happen to be selected as attacker more often during malfunction events get progressively weaker. This creates a snowball effect and makes fights less about stats and more about RNG malfunction distribution. The E-008 stat-swap restoration mechanism (lines 507–515) proves the codebase has a pattern for temporary effects, but mace malfunction doesn't use it.

**Evidence:**
```go
// Line 636-638: permanent reduction, despite "temporary" comment
// Reduce attack for next tick (temporary)
atk.Attack *= 0.80
continue
```

Compare with stat-swap temporary effect restoration (lines 507–515):
```go
if eff.effectType == "stat_swap" {
    e[0].Attack = origAttack[0]  // restoration exists here
```

**Recommended fix:** Either:
1. Save the original attack before malfunction and restore it at the start of the next tick, or
2. Use the existing `tempEffect` system to create a 1-tick attack debuff, or
3. Change the comment to match the actual behavior (permanent reduction) and accept it as a design choice

**Test needed:** Yes — verify attack is restored after mace malfunction.

---

## Finding 6: StableManager pointer/copy divergence causes stale data in server code

**Severity:** HIGH  
**Type:** Data integrity  
**Confidence:** Certain  

**File:** `internal/stableussy/stableussy.go` lines 162, 365  
**File:** `internal/server/server.go` lines 3886–3889, 4707–4708, 4960–4961  

**Summary:** `AddHorseToStable` appends `*horse` (a value copy) to `stable.Horses` but stores the original pointer `horse` in `sm.horses`. These are different objects. While `ListHorses()` is correctly implemented to return registry pointers (line 199), **multiple places in server.go access `stable.Horses[i]` directly**, reading from and writing to the stale copies instead of the registry.

Critical examples:
- **Season rollover ELO reset** (server.go:3887–3888): Writes new ELO to `stable.Horses[i]`, which modifies the copy but NOT the registry pointer. After rollover, `GetHorse()` returns pre-rollover ELO, `ListHorses()` returns pre-rollover ELO (from registry), but the *persisted* stable data has the post-rollover ELO. On next server restart (loading from DB), the rollover takes effect — but during the running session, ELOs are stale.
- **Horse lookup by name** (server.go:4707–4708, 4716–4717): Takes `&stable.Horses[i]` — a pointer to the copy. Any mutations through this pointer don't affect the registry.
- **Auto-pick horse** (server.go:4960–4961): Gets `&stable.Horses[i]` as a reference — same stale-copy problem.

**Impact:** Data inconsistency between the registry (source of truth for `GetHorse`/`ListHorses`) and direct stable-access patterns. The season ELO rollover is the most serious: during the session after rollover, all ELOs are inconsistent, which affects race matchmaking, betting odds, and leaderboards.

**Recommended fix:** Either:
1. Change all direct `stable.Horses[i]` accesses to use `sm.GetHorse(stable.Horses[i].ID)` instead, or
2. Store only horse IDs in the stable's roster (not full Horse values) and always look up from the registry, or
3. Call `syncHorseToStable` after every mutation (fragile but minimal change)

**Test needed:** Yes — test that season rollover ELO changes are visible via GetHorse.

---

## Finding 7: Training injury check uses post-training fatigue (higher than expected)

**Severity:** LOW  
**Type:** Bug / Documentation mismatch  
**Confidence:** Certain  

**File:** `internal/trainussy/trainussy.go` lines 192, 200–201  

**Summary:** The `Train()` function updates `horse.Fatigue` at line 192, then calls `rollInjury(horse)` at line 201. Since `rollInjury` reads `horse.Fatigue` to determine injury probability (>90 → 15%, >70 → 5%, else → 2%), the injury check uses the post-training fatigue — which is always higher than the pre-training fatigue. This means the effective injury rates are higher than a player might expect based on the pre-training fatigue displayed to them.

**Impact:** Players see their horse at 65% fatigue, choose to train, and the fatigue jumps to (say) 85%, triggering the 5% injury threshold instead of staying in the 2% bracket. The functional behavior is defensible as "training is risky when you're already tired" but it's not what the UI communicates.

**Evidence:**
```go
// Line 192: fatigue updated first
horse.Fatigue += delta

// Line 200-201: injury check uses new fatigue value
injured, injuryNote := rollInjury(horse)
```

**Recommended fix:** Either:
1. Call `rollInjury` before updating fatigue (using pre-training value), or
2. Document explicitly that injury risk is based on post-training fatigue, or
3. Pass the pre-training fatigue value to `rollInjury` as a parameter

**Test needed:** Yes — test injury probability thresholds relative to pre/post training fatigue.

---

## Finding 8: Season rollover can orphan tournament round data

**Severity:** LOW  
**Type:** Data integrity  
**Confidence:** Medium  

**File:** `internal/tournussy/tournussy.go` lines 610–616, 646–659  
**File:** `internal/server/server.go` lines 2832–2834  

**Summary:** `RunNextRound()` increments `t.CurrentRound` (line 616) before returning the race. `RecordRoundResults()` appends the race ID to `t.Races` (line 659). In the server, if `RecordRoundResults` errors, the round counter is already incremented but the race ID is not recorded:

```go
if err := s.tournaments.RecordRoundResults(tournamentID, race); err != nil {
    log.Printf("server: failed to record tournament round results: %v", err)
    // continues without recording — CurrentRound is already incremented
}
```

**Impact:** The tournament's `CurrentRound` advances past a round whose results were never recorded. The tournament may finish with fewer recorded races than expected. Tournament standings and leaderboards would be incomplete.

**Recommended fix:** Either:
1. Roll back `CurrentRound` if `RecordRoundResults` fails, or
2. Move the `CurrentRound++` into `RecordRoundResults` so it only advances on success, or
3. Return an error from the handler and do not continue

**Test needed:** Yes — test tournament state when RecordRoundResults fails.

---

## Finding 9: FitnessCeiling drift from repeated seasonal events

**Severity:** MEDIUM  
**Type:** Data integrity / Economic exploit  
**Confidence:** Certain  

**File:** `internal/trainussy/trainussy.go` lines 1502–1605  

**Summary:** Multiple seasonal effects permanently increase `horse.FitnessCeiling` by multiplying (e.g., `*= 1.03`, `*= 1.02`, `*= 1.05`). These effects are applied via `ApplySeasonalEffect` and there is no cap on the resulting ceiling. Over many seasons, a horse's FitnessCeiling can exceed 1.0 through repeated application of ceiling-boosting events.

Normal horses have `FitnessCeiling` clamped to [0, 1] at creation (genussy.go:212–218), but seasonal effects have no corresponding clamp. A horse that receives `all_horses_chaos_boost` (+3%) 25 times over multiple seasons would have ceiling ≈ 2.1 — comparable to the legendary horse balance issue in Finding 2.

**Impact:** Long-lived horses in active stables gradually become overpowered through seasonal ceiling drift. Combined with `CalcBaseSpeed` multiplying by `CurrentFitness` (which is capped to `FitnessCeiling` by training), this creates an inflationary stat curve that advantages older accounts.

**Evidence:**
```go
case "all_horses_chaos_boost":
    horse.FitnessCeiling *= 1.03  // no upper bound check
```

**Recommended fix:** Add `if horse.FitnessCeiling > 1.0 { horse.FitnessCeiling = 1.0 }` after each ceiling-modifying seasonal effect, or add the clamp in the training update that sets CurrentFitness based on ceiling.

**Test needed:** Yes — test that seasonal effects don't push ceiling above 1.0.

---

## Finding 10: Inbreeding coefficient uses a naive metric instead of Wright's formula

**Severity:** LOW  
**Type:** Design / Accuracy  
**Confidence:** Certain  

**File:** `internal/pedigreussy/pedigreussy.go` lines 106–156  

**Summary:** The inbreeding coefficient is calculated as `shared ancestor duplicate appearances / total ancestor slots visited`. This is a custom heuristic, not Wright's standard inbreeding coefficient (which uses path-based analysis). The result is that the coefficient is strongly biased by tree depth — a 5-generation tree with one shared great-grandparent would produce a low coefficient due to the large denominator (62 total slots), even though the actual genetic overlap may be significant.

**Impact:** Inbreeding penalties are softer than intended. This primarily affects the pedigree/breeding quality analysis and `CalcBloodlineBonus`. Since the inbreeding penalty is already tier-based (0%, 5%, 15%, 30%) rather than continuous, the impact is somewhat mitigated.

**Recommended fix:** Either document this as an intentional simplification or implement a path-based coefficient calculation. Given this is a game, the current approach may be adequate if the tier thresholds are tuned to compensate.

**Test needed:** Existing tests are adequate for the current formula.

---

## Summary Table

| # | Severity | Type | Summary |
|---|----------|------|---------|
| 1 | CRITICAL | Bug/Security | PurchaseBreeding doesn't deactivate listing — infinite purchases |
| 2 | CRITICAL | Bug/Balance | Legendary horses with FitnessCeiling > 1.0 are 10x faster |
| 3 | HIGH | Bug | fatigue_resist always fires — permanent fatigue immunity |
| 4 | MEDIUM | Bug | stamina_boost never fires when combined with fatigue_resist |
| 5 | MEDIUM | Bug | Mace malfunction permanently reduces attack |
| 6 | HIGH | Data integrity | StableManager pointer/copy divergence in server code |
| 7 | LOW | Bug/Docs | Injury check uses post-training fatigue |
| 8 | LOW | Data integrity | Tournament round counter can advance without recording results |
| 9 | MEDIUM | Data integrity | FitnessCeiling drifts above 1.0 from seasonal events |
| 10 | LOW | Design | Inbreeding coefficient is naive heuristic, not Wright's formula |
