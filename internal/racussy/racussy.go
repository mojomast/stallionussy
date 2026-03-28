// Package racussy implements the race simulation engine for StallionUSSY.
// It handles physics-based tick simulation, track-specific modifiers, genetic
// influence on performance, weather effects, trait modifiers, and narrative
// generation for race events.
package racussy

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/mojomast/stallionussy/internal/models"
)

// ---------------------------------------------------------------------------
// Track distances (metres) — mirrors models.TrackDistance but kept local for
// use without calling into models every tick.
// ---------------------------------------------------------------------------

const (
	DistanceSprintussy  = 800
	DistanceGrindussy   = 3200
	DistanceMudussy     = 1600
	DistanceThunderussy = 2400
	DistanceFrostussy   = 1200
	DistanceHauntedussy = 666
)

// ---------------------------------------------------------------------------
// RaceConfig
// ---------------------------------------------------------------------------

// RaceConfig holds the parameters that define how a race is set up.
type RaceConfig struct {
	TrackType    models.TrackType
	Distance     int           // auto-populated from TrackType if zero
	TickInterval time.Duration // default 100ms game-time per tick
}

// DefaultConfig returns a RaceConfig with sensible defaults for the given track.
func DefaultConfig(trackType models.TrackType) RaceConfig {
	return RaceConfig{
		TrackType:    trackType,
		Distance:     models.TrackDistance(trackType),
		TickInterval: 100 * time.Millisecond,
	}
}

// ---------------------------------------------------------------------------
// Weather modifiers — applied to simulation parameters per-weather condition
// ---------------------------------------------------------------------------

// weatherMods holds the multipliers that weather applies to simulation params.
type weatherMods struct {
	speedMod   float64 // multiplied onto base speed each tick
	fatigueMod float64 // multiplied onto fatigue rate
	chaosMod   float64 // multiplied onto chaos sigma
	panicMod   float64 // multiplied onto panic chance
}

// weatherModifiers returns the modifier set for a given weather condition.
func weatherModifiers(w models.Weather) weatherMods {
	switch w {
	case models.WeatherRainy:
		return weatherMods{speedMod: 0.95, fatigueMod: 1.1, chaosMod: 1.3, panicMod: 1.5}
	case models.WeatherStormy:
		return weatherMods{speedMod: 0.85, fatigueMod: 1.3, chaosMod: 2.0, panicMod: 2.0}
	case models.WeatherFoggy:
		return weatherMods{speedMod: 0.90, fatigueMod: 1.0, chaosMod: 1.5, panicMod: 1.2}
	case models.WeatherScorching:
		return weatherMods{speedMod: 0.92, fatigueMod: 1.5, chaosMod: 0.8, panicMod: 0.8}
	case models.WeatherHaunted:
		return weatherMods{speedMod: 1.0, fatigueMod: 0.8, chaosMod: 3.0, panicMod: 3.0}
	default: // WeatherClear and any unknown
		return weatherMods{speedMod: 1.0, fatigueMod: 1.0, chaosMod: 1.0, panicMod: 1.0}
	}
}

// ---------------------------------------------------------------------------
// Track weight tables
// ---------------------------------------------------------------------------

// trackWeights holds the per-gene weights that affect base speed on a track.
type trackWeights struct {
	SPD float64 // Speed gene weight
	STM float64 // Stamina gene weight
	TMP float64 // Temper gene weight
}

func weightsFor(t models.TrackType) trackWeights {
	switch t {
	case models.TrackSprintussy:
		return trackWeights{SPD: 0.8, STM: 0.2, TMP: 0.0}
	case models.TrackGrindussy:
		return trackWeights{SPD: 0.1, STM: 0.9, TMP: 0.0}
	case models.TrackMudussy:
		return trackWeights{SPD: 0.4, STM: 0.3, TMP: 0.3}
	case models.TrackThunderussy:
		return trackWeights{SPD: 0.4, STM: 0.4, TMP: 0.2} // balanced endurance
	case models.TrackFrostussy:
		return trackWeights{SPD: 0.6, STM: 0.1, TMP: 0.3} // speed + temper on ice
	case models.TrackHauntedussy:
		return trackWeights{SPD: 0.3, STM: 0.2, TMP: 0.5} // temper-heavy, spooky
	default:
		return trackWeights{SPD: 0.4, STM: 0.3, TMP: 0.3}
	}
}

// trackModifier returns the global speed modifier for a track type.
func trackModifier(t models.TrackType) float64 {
	switch t {
	case models.TrackSprintussy:
		return 1.0
	case models.TrackGrindussy:
		return 0.7
	case models.TrackMudussy:
		return 0.6
	case models.TrackThunderussy:
		return 0.8
	case models.TrackFrostussy:
		return 0.9
	case models.TrackHauntedussy:
		return 0.7
	default:
		return 0.6
	}
}

// ---------------------------------------------------------------------------
// Gene helpers — safe extraction from a Genome
// ---------------------------------------------------------------------------

// geneScore safely extracts the GeneScore for a gene type from a genome.
// Returns 0.3 (BB-equivalent) if the gene is missing.
func geneScore(g models.Genome, gt models.GeneType) float64 {
	if gene, ok := g[gt]; ok {
		return gene.GeneScore()
	}
	return 0.3
}

// geneExpress safely extracts the expression string ("AA"/"AB"/"BB") for a
// gene type. Returns "BB" if the gene is missing.
func geneExpress(g models.Genome, gt models.GeneType) string {
	if gene, ok := g[gt]; ok {
		return gene.Express()
	}
	return "BB"
}

// ---------------------------------------------------------------------------
// CalcBaseSpeed — exported so callers can inspect raw speed potential
// ---------------------------------------------------------------------------

// speedScale converts the 0-1 genetic/fitness values into a reasonable
// metres-per-tick figure. A top horse (fitness 1.0, all AA) should cover
// roughly 800m in ~80 ticks on a sprint (~10 m/tick). Even weak horses
// (~0.3 fitness) on long tracks should finish well within the 10k tick cap.
const speedScale = 18.0

// CalcBaseSpeed computes the base speed value for a horse on a given track.
//
//	BaseSpeed = speedScale * horse.CurrentFitness * (SPD_score*w_spd + STM_score*w_stm + TMP_score*w_tmp)
//
// The returned value is in metres-per-tick (before track modifier and fatigue).
func CalcBaseSpeed(horse *models.Horse, trackType models.TrackType) float64 {
	w := weightsFor(trackType)
	spdScore := geneScore(horse.Genome, models.GeneSPD)
	stmScore := geneScore(horse.Genome, models.GeneSTM)
	tmpScore := geneScore(horse.Genome, models.GeneTMP)

	geneticFactor := spdScore*w.SPD + stmScore*w.STM + tmpScore*w.TMP

	return speedScale * horse.CurrentFitness * geneticFactor
}

// ---------------------------------------------------------------------------
// Trait helpers — check if a horse has a specific trait effect
// ---------------------------------------------------------------------------

// hasTraitEffect returns true if the horse has at least one trait with the
// given effect string.
func hasTraitEffect(horse *models.Horse, effect string) bool {
	for _, t := range horse.Traits {
		if t.Effect == effect {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// NewRace — creates a models.Race ready for simulation
// ---------------------------------------------------------------------------

// NewRace builds a new Race with entries for every horse, the correct track
// type, distance, and purse. The race starts in "Pending" status.
func NewRace(horses []*models.Horse, trackType models.TrackType, purse int64) *models.Race {
	entries := make([]models.RaceEntry, len(horses))
	for i, h := range horses {
		entries[i] = models.RaceEntry{
			HorseID:   h.ID,
			HorseName: h.Name,
			Position:  0,
			Finished:  false,
			TickLog:   []models.TickEvent{},
		}
	}

	return &models.Race{
		ID:        uuid.New().String(),
		TrackType: trackType,
		Distance:  models.TrackDistance(trackType),
		Entries:   entries,
		Status:    models.RaceStatusPending,
		Purse:     purse,
		CreatedAt: time.Now(),
	}
}

// ---------------------------------------------------------------------------
// SimulateRace — backward-compatible entry point (uses WeatherClear)
// ---------------------------------------------------------------------------

// SimulateRace runs the full race simulation synchronously and returns the
// mutated Race with final results. An optional seed can be provided to make
// the simulation deterministic; pass nothing (or omit) for random seeding.
//
// This is a convenience wrapper around SimulateRaceWithWeather using Clear weather.
func SimulateRace(race *models.Race, horses []*models.Horse, seed ...uint64) *models.Race {
	return SimulateRaceWithWeather(race, horses, models.WeatherClear, seed...)
}

// ---------------------------------------------------------------------------
// SimulateRaceWithWeather — the main simulation loop with weather support
// ---------------------------------------------------------------------------

// SimulateRaceWithWeather runs the full race simulation with weather modifiers
// applied to speed, fatigue, chaos, and panic calculations.
//
// Physics tick formula (every 100ms game-time):
//
//	deltaP = (BaseSpeed * TrackModifier * WeatherSpeedMod) - Fatigue*(1/STM_score)*WeatherFatigueMod + Chaos*WeatherChaosMod
//
// Weather modifiers:
//   - Clear:     speed=1.0, fatigue=1.0, chaos=1.0, panic=1.0
//   - Rainy:     speed=0.95, fatigue=1.1, chaos=1.3, panic=1.5
//   - Stormy:    speed=0.85, fatigue=1.3, chaos=2.0, panic=2.0
//   - Foggy:     speed=0.90, fatigue=1.0, chaos=1.5, panic=1.2
//   - Scorching: speed=0.92, fatigue=1.5, chaos=0.8, panic=0.8
//   - Haunted:   speed=1.0, fatigue=0.8, chaos=3.0, panic=3.0
//
// Trait effects are applied per-tick per-horse after base deltaP calculation.
// Hauntedussy track has additional special events (ghost sightings, light flickers).
func SimulateRaceWithWeather(race *models.Race, horses []*models.Horse, weather models.Weather, seed ...uint64) *models.Race {
	// -----------------------------------------------------------------------
	// Set up RNG — deterministic if seed provided, otherwise random.
	// -----------------------------------------------------------------------
	var rng *rand.Rand
	if len(seed) > 0 {
		rng = rand.New(rand.NewPCG(seed[0], seed[0]^0xDEADBEEF_CAFEBABE))
	} else {
		rng = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	}

	// Build a lookup map: horseID -> *Horse for quick genome access.
	horseMap := make(map[string]*models.Horse, len(horses))
	for _, h := range horses {
		horseMap[h.ID] = h
	}

	race.Status = models.RaceStatusRunning
	trackType := race.TrackType
	distance := float64(race.Distance)
	tmod := trackModifier(trackType)

	// Resolve weather modifiers once before the loop.
	wMods := weatherModifiers(weather)

	finishedCount := 0
	totalEntries := len(race.Entries)
	nextPlace := 1

	tick := 0
	tickInterval := 100 * time.Millisecond

	// -----------------------------------------------------------------------
	// Simulation loop — runs until every horse crosses the finish line
	// Safety cap at 10000 ticks to prevent infinite races.
	// -----------------------------------------------------------------------
	const maxTicks = 10000
	for finishedCount < totalEntries && tick < maxTicks {
		tick++
		gameTime := time.Duration(tick) * tickInterval

		// ------------------------------------------------------------------
		// Hauntedussy global event: "THE LIGHTS FLICKER" — 0.5% per tick
		// affects ALL horses this tick with chaos * 3.
		// ------------------------------------------------------------------
		lightsFlicker := false
		if trackType == models.TrackHauntedussy && rng.Float64() < 0.005 {
			lightsFlicker = true
		}

		for i := range race.Entries {
			entry := &race.Entries[i]

			// Already finished — skip.
			if entry.Finished {
				continue
			}

			horse, ok := horseMap[entry.HorseID]
			if !ok {
				// Safety net: if horse data is missing, DNF at current time.
				entry.Finished = true
				entry.FinalTime = gameTime
				entry.FinishPlace = nextPlace
				nextPlace++
				finishedCount++
				continue
			}

			// ------ Determine if this horse is weather-immune ------
			isWeatherImmune := hasTraitEffect(horse, "weather_immune")

			// ------ Gene scores ------
			stmScore := geneScore(horse.Genome, models.GeneSTM)
			szeExpr := geneExpress(horse.Genome, models.GeneSZE)
			tmpExpr := geneExpress(horse.Genome, models.GeneTMP)

			// ------ Base speed (metres-per-tick before modifiers) ------
			baseSpeed := CalcBaseSpeed(horse, trackType)

			// Apply weather speed modifier (unless immune).
			if !isWeatherImmune {
				baseSpeed *= wMods.speedMod
			}

			// ------ Effective track modifier (with SZE adjustment on mud) ------
			effectiveTmod := tmod
			if trackType == models.TrackMudussy {
				switch szeExpr {
				case "AA": // Power build — pushes through mud
					effectiveTmod *= 1.10
				case "BB": // Lean build — slips in mud
					effectiveTmod *= 0.95
					// "AB" is neutral — no adjustment
				}
			}

			// ------ Fatigue (increases every tick, penalised by low STM) ------
			fatigue := float64(tick) * 0.001 * (1.0 / stmScore)

			// Apply weather fatigue modifier (unless immune).
			if !isWeatherImmune {
				fatigue *= wMods.fatigueMod
			}

			// ------ Apply fatigue_resist trait ------
			// If fatigue < 80% of distance, skip fatigue calculation entirely.
			skipFatigue := false
			for _, trait := range horse.Traits {
				if trait.Effect == "fatigue_resist" && fatigue < 0.8*distance {
					skipFatigue = true
					break
				}
			}
			if skipFatigue {
				fatigue = 0
			}

			// ------ Chaos (random jitter per tick) ------
			var chaosSigma float64
			if trackType == models.TrackMudussy {
				chaosSigma = 0.8 // N(0, 0.8)
			} else {
				chaosSigma = 0.3 // N(0, 0.3)
			}

			// Apply weather chaos modifier.
			chaosSigma *= wMods.chaosMod

			// ------ Apply chaos_multiplier trait ------
			for _, trait := range horse.Traits {
				if trait.Effect == "chaos_multiplier" {
					chaosSigma *= trait.Magnitude
				}
			}

			chaos := rng.NormFloat64() * chaosSigma

			// If lights flicker on Hauntedussy, chaos triples for this tick.
			if lightsFlicker {
				chaos *= 3
			}

			// ------ Compute raw delta-position ------
			deltaP := (baseSpeed * effectiveTmod) - fatigue + chaos

			// ------ Apply stamina_boost trait (reduces fatigue growth) ------
			// This retroactively adjusts deltaP by modifying the fatigue component.
			for _, trait := range horse.Traits {
				if trait.Effect == "stamina_boost" && !skipFatigue {
					// Recalculate: reduce fatigue by trait magnitude factor.
					// fatigue *= (2 - magnitude), so we need to adjust deltaP.
					oldFatigue := fatigue
					adjustedFatigue := fatigue * (2 - trait.Magnitude)
					deltaP += (oldFatigue - adjustedFatigue) // add back the fatigue savings
				}
			}

			// ------ Event tracking ------
			eventText := ""

			// ------ Event: PANIC (TMP-dependent, modified by weather) ------
			panicChance := 0.0
			switch tmpExpr {
			case "BB":
				panicChance = 0.03 // 3% per tick base
			case "AB":
				panicChance = 0.01 // 1% per tick base
			}

			// On non-Mudussy tracks, panic only triggers if there's weather/track chaos.
			// On Mudussy it always applies (original behavior).
			if trackType != models.TrackMudussy {
				// Scale panic chance by track temper weight — high TMP weight = more panic.
				w := weightsFor(trackType)
				panicChance *= w.TMP / 0.3 // normalize so Mudussy-level (0.3) = 1.0x
				if w.TMP == 0 {
					panicChance = 0
				}
			}

			// Apply weather panic modifier.
			panicChance *= wMods.panicMod

			// Apply panic_resist traits.
			for _, trait := range horse.Traits {
				if trait.Effect == "panic_resist" {
					panicChance *= trait.Magnitude
				}
			}

			if panicChance > 0 && rng.Float64() < panicChance {
				deltaP = 0
				eventText = "PANIC"
			}

			// ------ Event: E-008 ANOMALOUS ACCELERATION (LotNumber == 6) ------
			e008Chance := 0.005 // 0.5% base
			// On Hauntedussy with Haunted weather, E-008 chance doubles.
			if trackType == models.TrackHauntedussy && weather == models.WeatherHaunted {
				e008Chance = 0.01
			}
			if horse.LotNumber == 6 && rng.Float64() < e008Chance {
				deltaP *= 2
				eventText = "ANOMALOUS ACCELERATION"
			}

			// ------ Hauntedussy special: GHOST SIGHTING (2% per tick) ------
			if trackType == models.TrackHauntedussy && rng.Float64() < 0.02 {
				deltaP *= 0.5 // horse loses 50% speed for this tick
				eventText = "GHOST SIGHTING"
			}

			// ------ Hauntedussy global: THE LIGHTS FLICKER (already applied above via chaos) ------
			if lightsFlicker && eventText == "" {
				eventText = "THE LIGHTS FLICKER"
			}

			// ------ Trait effects (applied after base deltaP calculation) ------
			for _, trait := range horse.Traits {
				switch trait.Effect {
				case "speed_boost", "all_boost":
					deltaP *= trait.Magnitude

				case "speed_boost_early":
					// Boost if position < 20% of distance.
					if entry.Position < 0.2*distance {
						deltaP *= trait.Magnitude
					}

				case "speed_boost_late":
					// Boost if position > 80% of distance.
					if entry.Position > 0.8*distance {
						deltaP *= trait.Magnitude
					}

				case "mud_boost":
					if trackType == models.TrackMudussy {
						deltaP *= trait.Magnitude
					}

				case "frost_boost":
					if trackType == models.TrackFrostussy {
						deltaP *= trait.Magnitude
					}

				case "crowd_boost":
					// Boost if 6 or more entries in the race.
					if totalEntries >= 6 {
						deltaP *= trait.Magnitude
					}

				case "small_field_boost":
					// Boost if 3 or fewer entries.
					if totalEntries <= 3 {
						deltaP *= trait.Magnitude
					}

				case "chaos_boost":
					// 50% chance of boost, 50% chance of inverse.
					if rng.Float64() < 0.5 {
						deltaP *= trait.Magnitude
					} else {
						deltaP *= (2 - trait.Magnitude)
					}

				case "anomalous_boost":
					deltaP *= trait.Magnitude
					// 1% chance of generating ANOMALOUS RESONANCE event.
					if rng.Float64() < 0.01 {
						eventText = "ANOMALOUS RESONANCE"
					}

				case "anomalous_burst":
					// 0.1% chance per tick of massive burst.
					if rng.Float64() < 0.001 {
						deltaP *= trait.Magnitude
						eventText = "THE YOGURT ERUPTS"
					}

				case "reality_warp":
					// 0.5% chance per tick of teleporting forward.
					if rng.Float64() < 0.005 {
						// Teleport to random position between current and finish.
						newPos := entry.Position + rng.Float64()*(distance-entry.Position)
						entry.Position = newPos
						eventText = "REALITY FRACTURE"
					}

					// stamina_boost, fatigue_resist, panic_resist, weather_immune,
					// chaos_multiplier are handled above in their respective sections.
				}
			}

			// Clamp: a horse should never go backwards.
			if deltaP < 0 {
				deltaP = 0
			}

			// ------ Apply movement ------
			entry.Position += deltaP

			// ------ Record tick snapshot ------
			entry.TickLog = append(entry.TickLog, models.TickEvent{
				Tick:     tick,
				Position: entry.Position,
				Speed:    deltaP,
				Event:    eventText,
			})

			// ------ Check if horse crossed the finish line ------
			if entry.Position >= distance {
				entry.Finished = true
				entry.FinishPlace = nextPlace

				// Interpolate the exact crossing time within this tick
				// by computing how far past the line the horse went.
				overshoot := entry.Position - distance
				var fraction float64
				if deltaP > 0 {
					fraction = overshoot / deltaP
				}
				entry.FinalTime = gameTime - time.Duration(fraction*float64(tickInterval))
				nextPlace++
				finishedCount++
			}
		}
	}

	// DNF any horses that didn't finish within the tick cap.
	if finishedCount < totalEntries {
		gameTime := time.Duration(tick) * tickInterval
		for i := range race.Entries {
			if !race.Entries[i].Finished {
				race.Entries[i].Finished = true
				race.Entries[i].FinishPlace = nextPlace
				race.Entries[i].FinalTime = gameTime
				nextPlace++
				finishedCount++
			}
		}
	}

	race.Status = models.RaceStatusFinished
	return race
}

// ---------------------------------------------------------------------------
// CalcPostRaceFatigue — compute fatigue accrued from a race
// ---------------------------------------------------------------------------

// CalcPostRaceFatigue calculates the fatigue a horse should accumulate after
// completing a race, based on distance, finish place, and weather.
//
//   - Base fatigue: distance / 100 (e.g., 3200m race = 32 fatigue)
//   - Winner bonus: +5 (victory lap exhaustion)
//   - Last place: +10 (humiliation exhaustion)
//   - Scorching weather: +50%
//   - Stormy weather: +30%
func CalcPostRaceFatigue(horse *models.Horse, race *models.Race, finishPlace int, weather models.Weather) float64 {
	// Base fatigue from race distance.
	baseFatigue := float64(race.Distance) / 100.0

	// Placement modifier.
	switch {
	case finishPlace == 1:
		baseFatigue += 5.0 // victory lap
	case finishPlace == len(race.Entries):
		baseFatigue += 10.0 // humiliation exhaustion
	}

	// Weather modifier.
	switch weather {
	case models.WeatherScorching:
		baseFatigue *= 1.5
	case models.WeatherStormy:
		baseFatigue *= 1.3
	}

	return baseFatigue
}

// ---------------------------------------------------------------------------
// GenerateRaceNarrative — produce flavour text from tick logs
// ---------------------------------------------------------------------------

// GenerateRaceNarrative scans a finished race's tick logs and produces a slice
// of dramatic commentary strings suitable for display or broadcast.
//
// Events covered:
//   - Weather announcement at start
//   - Lead changes: "[TICK] HORSE_NAME surges ahead!"
//   - Panic:        "[TICK] HORSE_NAME: PANIC! Spooked by the mud!"
//   - E-008:        "[TICK] E-008's Chosen: ANOMALOUS ACCELERATION. The yogurt remembers."
//   - Ghost sighting (Hauntedussy): "[TICK] HORSE_NAME: GHOST SIGHTING!"
//   - Light flicker (Hauntedussy): "[TICK] THE LIGHTS FLICKER — all horses shudder!"
//   - Frostussy slip: "[TICK] HORSE_NAME: SLIPPING ON ICE!"
//   - Thunderussy lightning: "[TICK] LIGHTNING STRIKES the track!"
//   - Anomalous events: ANOMALOUS RESONANCE, THE YOGURT ERUPTS, REALITY FRACTURE
//   - Photo finish: "[TICK] PHOTO FINISH between X and Y!"
//   - Final results with dramatic flair
func GenerateRaceNarrative(race *models.Race) []string {
	return GenerateRaceNarrativeWithWeather(race, models.WeatherClear)
}

// GenerateRaceNarrativeWithWeather produces narrative text including weather context.
func GenerateRaceNarrativeWithWeather(race *models.Race, weather models.Weather) []string {
	if len(race.Entries) == 0 {
		return nil
	}

	narrative := []string{}

	// ------ Weather announcement ------
	weatherDesc := weatherDescription(weather)
	narrative = append(narrative, fmt.Sprintf("[0.0s] WEATHER: %s -- %s", string(weather), weatherDesc))

	// Track-specific opening flavor.
	switch race.TrackType {
	case models.TrackHauntedussy:
		narrative = append(narrative, "[0.0s] The shadows lengthen on the Hauntedussy track... something stirs in the fog.")
	case models.TrackFrostussy:
		narrative = append(narrative, "[0.0s] Ice crystals glitter under the floodlights. The Frostussy surface is treacherous today.")
	case models.TrackThunderussy:
		narrative = append(narrative, "[0.0s] Thunder rumbles in the distance. The Thunderussy endurance gauntlet begins.")
	}

	// Determine the max tick across all entries.
	maxTick := 0
	for _, e := range race.Entries {
		if n := len(e.TickLog); n > 0 {
			if last := e.TickLog[n-1].Tick; last > maxTick {
				maxTick = last
			}
		}
	}

	// Build a tick-index per entry for O(1) lookups.
	type tickRef struct {
		pos   float64
		speed float64
		event string
	}
	tickData := make([]map[int]tickRef, len(race.Entries))
	for i, e := range race.Entries {
		m := make(map[int]tickRef, len(e.TickLog))
		for _, te := range e.TickLog {
			m[te.Tick] = tickRef{pos: te.Position, speed: te.Speed, event: te.Event}
		}
		tickData[i] = m
	}

	prevLeaderIdx := -1
	lastLeadChangeTick := 0
	distance := float64(race.Distance)

	// Report race progress at key percentage checkpoints plus events.
	checkpoints := map[int]bool{}
	for _, pct := range []int{25, 50, 75} {
		checkpoints[int(distance*float64(pct)/100)] = true
	}
	reportedCheckpoints := map[int]bool{}

	// Track Thunderussy lightning intervals — every ~50 ticks with some randomness.
	lastLightningTick := 0

	// --- Event throttling to prevent narrative spam ---
	// Per-horse event counts: eventCounts[horseIdx][eventType] = count
	eventCounts := make([]map[string]int, len(race.Entries))
	for i := range eventCounts {
		eventCounts[i] = map[string]int{}
	}
	// Per-horse last event tick to enforce minimum gap between same events
	eventLastTick := make([]map[string]int, len(race.Entries))
	for i := range eventLastTick {
		eventLastTick[i] = map[string]int{}
	}
	// Global event counts for track-wide events (lights flicker, etc.)
	globalEventCount := map[string]int{}

	// Throttle limits: max times an event can appear per horse in narrative
	eventMaxPerHorse := map[string]int{
		"PANIC": 3, "GHOST SIGHTING": 2, "ICE SLIP": 2,
		"ANOMALOUS ACCELERATION": 4, "ANOMALOUS RESONANCE": 2,
		"THE YOGURT ERUPTS": 2, "REALITY FRACTURE": 2,
	}
	// Minimum tick gap between reports of same event for same horse
	eventMinGap := map[string]int{
		"PANIC": 15, "GHOST SIGHTING": 30, "ICE SLIP": 20,
		"ANOMALOUS ACCELERATION": 10,
	}
	// Escalation messages when a horse hits the cap for an event type
	eventCapMsg := map[string]string{
		"PANIC":          "%s has entered a permanent state of existential dread.",
		"GHOST SIGHTING": "%s refuses to look anywhere but straight ahead now.",
		"ICE SLIP":       "%s has given up on traction entirely.",
	}
	// Global event caps
	globalEventMax := map[string]int{
		"THE LIGHTS FLICKER": 3,
	}

	for t := 1; t <= maxTick; t++ {
		// Gather positions at this tick for every entry that has data.
		type posAt struct {
			idx  int
			pos  float64
			name string
		}
		var positions []posAt
		for i, e := range race.Entries {
			if ref, ok := tickData[i][t]; ok {
				positions = append(positions, posAt{idx: i, pos: ref.pos, name: e.HorseName})
			}
		}
		if len(positions) == 0 {
			continue
		}

		// Sort by position descending to find the leader.
		sort.Slice(positions, func(a, b int) bool {
			return positions[a].pos > positions[b].pos
		})

		leader := positions[0]

		// --- Lead change (throttled: min 10 ticks between reports) ---
		if leader.idx != prevLeaderIdx {
			if prevLeaderIdx == -1 || (t-lastLeadChangeTick) >= 10 {
				narrative = append(narrative, fmt.Sprintf("[%.1fs] %s surges ahead!", float64(t)*0.1, leader.name))
				lastLeadChangeTick = t
			}
		}
		prevLeaderIdx = leader.idx

		// --- Checkpoint position reports ---
		for cp := range checkpoints {
			if reportedCheckpoints[cp] {
				continue
			}
			if leader.pos >= float64(cp) {
				pct := int(float64(cp) / distance * 100)
				// Build compact standings
				standings := ""
				for rank, p := range positions {
					if rank > 0 {
						standings += ", "
					}
					standings += fmt.Sprintf("%d.%s", rank+1, p.name)
					if rank >= 3 {
						break
					}
				}
				narrative = append(narrative, fmt.Sprintf("[%.1fs] === %d%% MARK === %s", float64(t)*0.1, pct, standings))
				reportedCheckpoints[cp] = true
			}
		}

		// --- Special events this tick (throttled to prevent narrative spam) ---
		for i := range race.Entries {
			ref, ok := tickData[i][t]
			if !ok {
				continue
			}
			ts := float64(t) * 0.1
			name := race.Entries[i].HorseName

			// Helper: check if a per-horse event should be narrated.
			// Returns true if under cap and past minimum gap; increments counters.
			canReport := func(evtKey string) bool {
				maxN := eventMaxPerHorse[evtKey]
				if maxN == 0 {
					maxN = 3 // default
				}
				count := eventCounts[i][evtKey]
				if count > maxN {
					return false // already past cap (cap message already sent)
				}
				if count == maxN {
					// Emit escalation message exactly once at the cap.
					if msg, ok := eventCapMsg[evtKey]; ok {
						narrative = append(narrative,
							fmt.Sprintf("[%.1fs] %s", ts, fmt.Sprintf(msg, name)))
					}
					eventCounts[i][evtKey]++
					return false
				}
				minGap := eventMinGap[evtKey]
				if minGap > 0 {
					if lastTick, ok := eventLastTick[i][evtKey]; ok && (t-lastTick) < minGap {
						return false
					}
				}
				eventCounts[i][evtKey]++
				eventLastTick[i][evtKey] = t
				return true
			}

			// Helper: check if a global (track-wide) event should be narrated.
			canReportGlobal := func(evtKey string) bool {
				maxN := globalEventMax[evtKey]
				if maxN == 0 {
					maxN = 3
				}
				if globalEventCount[evtKey] >= maxN {
					return false
				}
				globalEventCount[evtKey]++
				return true
			}

			switch ref.event {
			case "PANIC":
				if canReport("PANIC") {
					if race.TrackType == models.TrackMudussy {
						narrative = append(narrative,
							fmt.Sprintf("[%.1fs] %s: PANIC! Spooked by the mud!", ts, name))
					} else if race.TrackType == models.TrackHauntedussy {
						narrative = append(narrative,
							fmt.Sprintf("[%.1fs] %s: PANIC! Something unseen brushes past!", ts, name))
					} else {
						narrative = append(narrative,
							fmt.Sprintf("[%.1fs] %s: PANIC! Lost composure!", ts, name))
					}
				}
			case "ANOMALOUS ACCELERATION":
				if canReport("ANOMALOUS ACCELERATION") {
					narrative = append(narrative,
						fmt.Sprintf("[%.1fs] E-008's Chosen: ANOMALOUS ACCELERATION. The yogurt remembers.", ts))
				}
			case "GHOST SIGHTING":
				if canReport("GHOST SIGHTING") {
					narrative = append(narrative,
						fmt.Sprintf("[%.1fs] %s: GHOST SIGHTING! A spectral figure drifts across the track!", ts, name))
				}
			case "THE LIGHTS FLICKER":
				if canReportGlobal("THE LIGHTS FLICKER") {
					narrative = append(narrative,
						fmt.Sprintf("[%.1fs] THE LIGHTS FLICKER -- all horses shudder as darkness pulses through the arena!", ts))
				}
			case "ANOMALOUS RESONANCE":
				if canReport("ANOMALOUS RESONANCE") {
					narrative = append(narrative,
						fmt.Sprintf("[%.1fs] %s: ANOMALOUS RESONANCE. The air hums with forbidden frequency.", ts, name))
				}
			case "THE YOGURT ERUPTS":
				if canReport("THE YOGURT ERUPTS") {
					narrative = append(narrative,
						fmt.Sprintf("[%.1fs] %s: THE YOGURT ERUPTS! An impossible surge of dairy-powered energy!", ts, name))
				}
			case "REALITY FRACTURE":
				if canReport("REALITY FRACTURE") {
					narrative = append(narrative,
						fmt.Sprintf("[%.1fs] %s: REALITY FRACTURE! Space folds and the horse blinks forward!", ts, name))
				}
			}

			// Frostussy: detect ice slipping when chaos is very negative (speed near 0).
			if race.TrackType == models.TrackFrostussy && ref.speed < 0.5 && ref.event == "" {
				if ref.speed > 0 && ref.speed < 0.3 {
					if canReport("ICE SLIP") {
						narrative = append(narrative,
							fmt.Sprintf("[%.1fs] %s: SLIPPING ON ICE! Hooves scramble for grip!", ts, name))
					}
				}
			}
		}

		// --- Thunderussy: LIGHTNING STRIKES at pseudo-random intervals ---
		if race.TrackType == models.TrackThunderussy && (t-lastLightningTick) >= 40 {
			// Check roughly every 50 ticks with jitter.
			if t%50 < 5 || (t > 100 && t%73 < 3) {
				narrative = append(narrative,
					fmt.Sprintf("[%.1fs] LIGHTNING STRIKES the Thunderussy track! The ground trembles!", float64(t)*0.1))
				lastLightningTick = t
			}
		}

		// --- Photo finish detection ---
		// Check if 2+ horses cross the line this tick within 0.5m of each other.
		var finishersThisTick []posAt
		for _, p := range positions {
			if p.pos >= distance {
				finishersThisTick = append(finishersThisTick, p)
			}
		}
		if len(finishersThisTick) >= 2 {
			if math.Abs(finishersThisTick[0].pos-finishersThisTick[1].pos) <= 0.5 {
				narrative = append(narrative,
					fmt.Sprintf("[%.1fs] PHOTO FINISH between %s and %s!",
						float64(t)*0.1, finishersThisTick[0].name, finishersThisTick[1].name))
			}
		}
	}

	// --- Final results ---
	narrative = append(narrative, "")
	narrative = append(narrative, "=== FINAL RESULTS ===")

	// Sort entries by finish place.
	sorted := make([]models.RaceEntry, len(race.Entries))
	copy(sorted, race.Entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].FinishPlace < sorted[j].FinishPlace
	})

	for _, e := range sorted {
		placeStr := ordinal(e.FinishPlace)
		timeStr := e.FinalTime.Truncate(time.Millisecond).String()

		var flair string
		switch e.FinishPlace {
		case 1:
			flair = " -- CHAMPION! Absolute unit."
		case 2:
			flair = " -- So close. The crowd weeps."
		case 3:
			flair = " -- Podium secured. Respectable."
		default:
			flair = " -- Ran their heart out."
		}

		narrative = append(narrative,
			fmt.Sprintf("  %s: %s (%s)%s", placeStr, e.HorseName, timeStr, flair))
	}

	return narrative
}

// ---------------------------------------------------------------------------
// weatherDescription — human-readable flavor text for weather conditions
// ---------------------------------------------------------------------------

func weatherDescription(w models.Weather) string {
	switch w {
	case models.WeatherClear:
		return "Clear skies. Perfect racing conditions."
	case models.WeatherRainy:
		return "Rain lashes the track. Footing is treacherous."
	case models.WeatherStormy:
		return "A violent storm rages! Visibility near zero, chaos reigns."
	case models.WeatherFoggy:
		return "Dense fog rolls across the course. Horses vanish and reappear like ghosts."
	case models.WeatherScorching:
		return "The sun beats down mercilessly. Heat shimmers rise from the track."
	case models.WeatherHaunted:
		return "The air tastes of static and old milk. E-008 containment levels: UNSTABLE."
	default:
		return "Conditions nominal."
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// ordinal converts a placement number to its ordinal string (1st, 2nd, ...).
func ordinal(n int) string {
	suffix := "th"
	switch n % 100 {
	case 11, 12, 13:
		// Special cases: 11th, 12th, 13th always end in "th".
	default:
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%d%s", n, suffix)
}
