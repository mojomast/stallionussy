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

	// Defensive clamp: FitnessCeiling (and thus CurrentFitness) should never
	// exceed 1.0, but clamp here to prevent any upstream drift from making
	// a horse disproportionately fast.
	fitness := horse.CurrentFitness
	if fitness > 1.0 {
		fitness = 1.0
	}

	return speedScale * fitness * geneticFactor
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
//
// Post-trait events (Ussyverse expansion):
//   - SECOND WIND: 1% when past 70% and struggling, deltaP *= 1.5
//   - CAFFEINE KICK: INT AA only, 0.5%, deltaP *= 1.3
//   - FROSTUSSY SLIP: Frostussy + SZE BB, 3%, deltaP *= 0.3
//   - THUNDERUSSY LIGHTNING STRIKE: Thunderussy global, 1%, deltaP *= 0.6 all
//   - MUDUSSY SPLATTER: Mudussy, 2%, deltaP *= 0.7
//   - CROWD SURGE: 1st place past 80%, 0.5%, deltaP *= 1.2
//   - DERULO SIGHTING: any, 0.1%, deltaP = 0
//   - MITTENS NAP: any, 0.2%, deltaP *= 0.4
//   - DIVINE PACKET: Thunderussy, 0.3%, deltaP *= 1.4
//   - GEOFFRUSSY OPTIMIZATION: INT AA/AB, final 20%, 0.3%, deltaP *= 1.25
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

		// ------------------------------------------------------------------
		// Thunderussy global event: "THUNDERUSSY LIGHTNING STRIKE" — 1%
		// per tick, affects ALL unfinished horses (deltaP *= 0.6).
		// ------------------------------------------------------------------
		thunderStrike := false
		if trackType == models.TrackThunderussy && rng.Float64() < 0.01 {
			thunderStrike = true
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
			intExpr := geneExpress(horse.Genome, models.GeneINT)

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
			// BUG FIX: fatigue_resist now provides a 50% fatigue reduction
			// instead of complete immunity. The old logic compared fatigue
			// (a tiny per-tick value) against 0.8 which was almost always
			// true, granting permanent zero fatigue.
			for _, trait := range horse.Traits {
				if trait.Effect == "fatigue_resist" && fatigue < 0.8*distance {
					fatigue *= 0.5 // 50% fatigue reduction, not full immunity
					break
				}
			}

			// Apply cursed_fatigue traits (increases fatigue).
			// Magnitude is ~0.8, so (2.0 - 0.8) = 1.2x fatigue multiplier.
			for _, trait := range horse.Traits {
				if trait.Effect == "cursed_fatigue" {
					fatigue *= (2.0 - trait.Magnitude)
				}
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

			// ------ Apply cursed_chaos trait (increases chaos) ------
			// Magnitude is ~0.85, so (2.0 - 0.85) = 1.15x chaos multiplier.
			for _, trait := range horse.Traits {
				if trait.Effect == "cursed_chaos" {
					chaosSigma *= (2.0 - trait.Magnitude)
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
				if trait.Effect == "stamina_boost" {
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

			// Apply cursed_panic traits (increases panic chance).
			// Magnitude is 0.85-0.95, so (2.0 - mag) yields 1.05-1.15x multiplier.
			for _, trait := range horse.Traits {
				if trait.Effect == "cursed_panic" {
					panicChance *= (2.0 - trait.Magnitude)
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

				case "thunder_boost":
					if trackType == models.TrackThunderussy {
						deltaP *= trait.Magnitude
					}

				case "haunted_boost":
					if trackType == models.TrackHauntedussy {
						deltaP *= trait.Magnitude
					}

				case "grind_boost":
					if trackType == models.TrackGrindussy {
						deltaP *= trait.Magnitude
					}

				case "sprint_boost":
					if trackType == models.TrackSprintussy {
						deltaP *= trait.Magnitude
					}

				case "cursed_speed":
					// Speed penalty — magnitude is 0.85-0.95, reducing deltaP.
					deltaP *= trait.Magnitude

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

				// elo_boost and earnings_boost are handled outside the race engine
				// (in ELO/earnings calculations). No-ops here to avoid default.
				case "elo_boost", "earnings_boost":
					// No race-time effect — handled in post-race processing.

					// stamina_boost, fatigue_resist, panic_resist, weather_immune,
					// chaos_multiplier, cursed_panic, cursed_fatigue, cursed_chaos
					// are handled above in their respective sections.
				}
			}

			// ------------------------------------------------------------------
			// NEW EVENTS (post-trait) — the Ussyverse expands
			// ------------------------------------------------------------------

			// ------ Event: SECOND WIND ------
			// Any horse past 70% of the distance AND struggling (speed < 50% base).
			// 1% per tick. The crowd gasps as the horse finds one last gear.
			if entry.Position > 0.7*distance && deltaP < baseSpeed*0.5 {
				if rng.Float64() < 0.01 {
					deltaP *= 1.5
					if eventText == "" {
						eventText = "SECOND WIND"
					}
				}
			}

			// ------ Event: CAFFEINE KICK ------
			// INT AA horses only. 0.5% per tick. They found a discarded oat milk
			// latte on the track. Dr. Mittens left it there. Allegedly.
			if intExpr == "AA" && rng.Float64() < 0.005 {
				deltaP *= 1.3
				if eventText == "" {
					eventText = "CAFFEINE KICK"
				}
			}

			// ------ Event: FROSTUSSY SLIP ------
			// Frostussy track only, SZE BB (lean build = no grip), 3% per tick.
			// The horse's hooves betray it on the ice. Devastating.
			if trackType == models.TrackFrostussy && szeExpr == "BB" {
				if rng.Float64() < 0.03 {
					deltaP *= 0.3
					if eventText == "" {
						eventText = "FROSTUSSY SLIP"
					}
				}
			}

			// ------ Event: THUNDERUSSY LIGHTNING STRIKE (global) ------
			// Applied to ALL unfinished horses when thunderStrike fires.
			// deltaP *= 0.6 — the lightning terrifies everyone.
			if thunderStrike {
				deltaP *= 0.6
				if eventText == "" {
					eventText = "THUNDERUSSY LIGHTNING STRIKE"
				}
			}

			// ------ Event: MUDUSSY SPLATTER ------
			// Mudussy track, 2% per tick. Face full of mud. Glorious.
			if trackType == models.TrackMudussy && rng.Float64() < 0.02 {
				deltaP *= 0.7
				if eventText == "" {
					eventText = "MUDUSSY SPLATTER"
				}
			}

			// ------ Event: CROWD SURGE ------
			// Horse in 1st place, past 80%, 0.5% per tick.
			// The crowd's energy is PALPABLE. It propels the leader forward.
			if entry.Position > 0.8*distance {
				// Determine if this horse is currently in 1st (highest position).
				isLeader := true
				for j := range race.Entries {
					if j != i && !race.Entries[j].Finished && race.Entries[j].Position > entry.Position {
						isLeader = false
						break
					}
				}
				if isLeader && rng.Float64() < 0.005 {
					deltaP *= 1.2
					if eventText == "" {
						eventText = "CROWD SURGE"
					}
				}
			}

			// ------ Event: DERULO SIGHTING ------
			// Any track, 0.1% per tick. Jason Derulo is spotted in the crowd.
			// The horse STOPS. Dead. Just stares. He insists he's not here.
			if rng.Float64() < 0.001 {
				deltaP = 0
				if eventText == "" {
					eventText = "DERULO SIGHTING"
				}
			}

			// ------ Event: MITTENS NAP ------
			// Any horse, 0.2% per tick. Dr. Mittens has materialized on the
			// horse's back and fallen asleep. The horse dare not disturb her.
			if rng.Float64() < 0.002 {
				deltaP *= 0.4
				if eventText == "" {
					eventText = "MITTENS NAP"
				}
			}

			// ------ Event: DIVINE PACKET ------
			// Thunderussy only, 0.3% per tick. Pastor Router McEthernet III
			// sends a divine network packet. The blessing finds its target.
			if trackType == models.TrackThunderussy && rng.Float64() < 0.003 {
				deltaP *= 1.4
				if eventText == "" {
					eventText = "DIVINE PACKET"
				}
			}

			// ------ Event: GEOFFRUSSY OPTIMIZATION ------
			// INT AA or AB, final 20% of distance, 0.3% per tick.
			// Geoffrussy's pipeline optimizes the horse's route to the finish.
			if (intExpr == "AA" || intExpr == "AB") && entry.Position > 0.8*distance {
				if rng.Float64() < 0.003 {
					deltaP *= 1.25
					if eventText == "" {
						eventText = "GEOFFRUSSY OPTIMIZATION"
					}
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
//   - Ussyverse events: SECOND WIND, CAFFEINE KICK, FROSTUSSY SLIP,
//     THUNDERUSSY LIGHTNING STRIKE, MUDUSSY SPLATTER, CROWD SURGE,
//     DERULO SIGHTING, MITTENS NAP, DIVINE PACKET, GEOFFRUSSY OPTIMIZATION
//   - Photo finish: "[TICK] PHOTO FINISH between X and Y!"
//   - Final results with dramatic flair
func GenerateRaceNarrative(race *models.Race) []string {
	return GenerateRaceNarrativeWithWeather(race, models.WeatherClear)
}

// GenerateRaceNarrativeWithWeather produces narrative text including weather context.
// This is the backward-compatible wrapper that returns plain strings.
// Internally delegates to GenerateRaceNarrativeIndexed and strips tick/class info.
func GenerateRaceNarrativeWithWeather(race *models.Race, weather models.Weather) []string {
	indexed := GenerateRaceNarrativeIndexed(race, weather)
	result := make([]string, len(indexed))
	for i, nl := range indexed {
		result[i] = nl.Text
	}
	return result
}

// ---------------------------------------------------------------------------
// NarrativeLine — a single narrative line tagged with its tick number
// ---------------------------------------------------------------------------

// NarrativeLine represents a single piece of race commentary tied to a
// specific simulation tick. This enables real-time interleaving of narration
// with tick-by-tick race replay — like a real derby broadcast.
type NarrativeLine struct {
	Tick  int    `json:"tick"`  // simulation tick this line corresponds to (0 = pre-race)
	Text  string `json:"text"`  // the narrative text
	Class string `json:"class"` // CSS class hint for frontend styling
}

// ---------------------------------------------------------------------------
// GenerateRaceNarrativeIndexed — tick-tagged narrative for real-time replay
// ---------------------------------------------------------------------------

// GenerateRaceNarrativeIndexed produces narrative lines tagged by tick,
// enabling real-time interleaving with the tick-by-tick race replay.
func GenerateRaceNarrativeIndexed(race *models.Race, weather models.Weather) []NarrativeLine {
	if len(race.Entries) == 0 {
		return nil
	}

	var lines []NarrativeLine

	// Helper to add a line.
	add := func(tick int, text, class string) {
		lines = append(lines, NarrativeLine{Tick: tick, Text: text, Class: class})
	}

	// --- Pre-race announcements (tick 0) ---
	weatherDesc := weatherDescription(weather)
	add(0, fmt.Sprintf("🎙️ LADIES AND GENTLEMEN... Welcome to the %s! %dm of pure equine chaos!", string(race.TrackType), race.Distance), "event-announcer")
	add(0, fmt.Sprintf("☁️ CONDITIONS: %s — %s", string(weather), weatherDesc), "event-weather")

	// Track flavor.
	switch race.TrackType {
	case models.TrackHauntedussy:
		add(0, "👻 The shadows lengthen on the Hauntedussy track... something stirs in the fog. The crowd falls silent.", "event-ghost")
	case models.TrackFrostussy:
		add(0, "❄️ Ice crystals glitter under the floodlights. The Frostussy surface is TREACHEROUS today, folks!", "event-weather")
	case models.TrackThunderussy:
		add(0, "⚡ Thunder rumbles in the distance. This is the Thunderussy endurance gauntlet — only the strong survive.", "event-weather")
	case models.TrackMudussy:
		add(0, "🟤 The Mudussy track is an absolute swamp today. This is going to get UGLY.", "event-weather")
	case models.TrackGrindussy:
		add(0, "🏔️ 3200 metres of pure Grindussy ahead. This is a war of attrition.", "event-announcer")
	case models.TrackSprintussy:
		add(0, "💨 Sprintussy! 800 metres, blink and you'll miss it. RAW SPEED is all that matters.", "event-burst")
	}

	// Entrant introductions.
	for i, e := range race.Entries {
		laneStr := ordinal(i + 1)
		add(0, fmt.Sprintf("   🐎 Lane %s: %s", laneStr, e.HorseName), "event-normal")
	}
	add(0, "", "event-normal")
	add(0, "🔔 AND THEY'RE OFF! The gates fly open!", "event-burst")

	// --- Build tick data index ---
	maxTick := 0
	for _, e := range race.Entries {
		if n := len(e.TickLog); n > 0 {
			if last := e.TickLog[n-1].Tick; last > maxTick {
				maxTick = last
			}
		}
	}

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

	// Checkpoints for standings reports.
	checkpointPcts := []int{25, 50, 75, 90}
	reportedCheckpoints := map[int]bool{}

	// Event throttling (same as original).
	eventCounts := make([]map[string]int, len(race.Entries))
	for i := range eventCounts {
		eventCounts[i] = map[string]int{}
	}
	eventLastTick := make([]map[string]int, len(race.Entries))
	for i := range eventLastTick {
		eventLastTick[i] = map[string]int{}
	}
	globalEventCount := map[string]int{}

	eventMaxPerHorse := map[string]int{
		"PANIC": 3, "GHOST SIGHTING": 2, "ICE SLIP": 2,
		"ANOMALOUS ACCELERATION": 4, "ANOMALOUS RESONANCE": 2,
		"THE YOGURT ERUPTS": 2, "REALITY FRACTURE": 2,
		"SECOND WIND": 2, "CAFFEINE KICK": 2, "FROSTUSSY SLIP": 3,
		"MUDUSSY SPLATTER": 3, "CROWD SURGE": 2, "DERULO SIGHTING": 2,
		"MITTENS NAP": 2, "DIVINE PACKET": 2, "GEOFFRUSSY OPTIMIZATION": 2,
	}
	eventMinGap := map[string]int{
		"PANIC": 15, "GHOST SIGHTING": 30, "ICE SLIP": 20,
		"ANOMALOUS ACCELERATION": 10,
		"SECOND WIND":            20, "CAFFEINE KICK": 25, "FROSTUSSY SLIP": 15,
		"MUDUSSY SPLATTER": 15, "CROWD SURGE": 20, "DERULO SIGHTING": 50,
		"MITTENS NAP": 30, "DIVINE PACKET": 25, "GEOFFRUSSY OPTIMIZATION": 20,
	}
	eventCapMsg := map[string]string{
		"PANIC":            "💀 %s has entered a permanent state of existential dread. They've stopped responding to stimuli.",
		"GHOST SIGHTING":   "😨 %s refuses to look anywhere but straight ahead now. Pure survival mode.",
		"ICE SLIP":         "🧊 %s has given up on traction entirely and is basically just sliding.",
		"FROSTUSSY SLIP":   "🧊 %s has accepted their fate as a horse-shaped ice cube. All four hooves are decorative at this point.",
		"MUDUSSY SPLATTER": "🟤 %s is now 60%% mud by volume. Scientists are debating if this still qualifies as a horse.",
		"MITTENS NAP":      "😺 Dr. Mittens has taken PERMANENT residence on %s's back. The horse has accepted its new overlord.",
		"DERULO SIGHTING":  "🎤 %s can no longer function in Jason Derulo's presence. The star power is simply too much.",
	}
	globalEventMax := map[string]int{
		"THE LIGHTS FLICKER":           3,
		"THUNDERUSSY LIGHTNING STRIKE": 3,
	}

	// Track last time we gave a general "pack update"
	lastPackUpdateTick := 0

	// Track which horses have been announced as finished
	announcedFinish := map[int]bool{}

	lastLightningTick := 0

	for t := 1; t <= maxTick; t++ {
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

		// Sort by position descending.
		sort.Slice(positions, func(a, b int) bool {
			return positions[a].pos > positions[b].pos
		})
		leader := positions[0]
		ts := float64(t) * 0.1

		// --- Lead change ---
		if leader.idx != prevLeaderIdx && prevLeaderIdx != -1 {
			if (t - lastLeadChangeTick) >= 8 {
				gap := 0.0
				if len(positions) > 1 {
					gap = leader.pos - positions[1].pos
				}
				if gap > 5 {
					add(t, fmt.Sprintf("🔥 [%.1fs] %s SURGES to the front and opens up a BIG lead! %.0fm ahead!", ts, leader.name, gap), "event-burst")
				} else {
					add(t, fmt.Sprintf("↗️ [%.1fs] LEAD CHANGE! %s takes the lead from %s!", ts, leader.name, race.Entries[prevLeaderIdx].HorseName), "event-burst")
				}
				lastLeadChangeTick = t
			}
		} else if prevLeaderIdx == -1 {
			add(t, fmt.Sprintf("🏇 [%.1fs] %s breaks fast and takes the early lead!", ts, leader.name), "event-burst")
			lastLeadChangeTick = t
		}
		prevLeaderIdx = leader.idx

		// --- Checkpoint standings ---
		for _, pct := range checkpointPcts {
			cpDist := int(distance * float64(pct) / 100)
			if reportedCheckpoints[pct] {
				continue
			}
			if leader.pos >= float64(cpDist) {
				standings := ""
				for rank, p := range positions {
					if rank > 0 {
						standings += ", "
					}
					medal := ""
					switch rank {
					case 0:
						medal = "🥇"
					case 1:
						medal = "🥈"
					case 2:
						medal = "🥉"
					}
					standings += fmt.Sprintf("%s%d.%s", medal, rank+1, p.name)
					if rank >= 3 {
						standings += "..."
						break
					}
				}

				marker := "═══"
				if pct == 90 {
					add(t, fmt.Sprintf("🚨 [%.1fs] %s FINAL STRETCH! %s %s %s STANDINGS: %s", ts, marker, marker, marker, marker, standings), "event-burst")
				} else if pct == 50 {
					add(t, fmt.Sprintf("📍 [%.1fs] %s HALFWAY POINT %s STANDINGS: %s", ts, marker, marker, standings), "event-announcer")
				} else {
					add(t, fmt.Sprintf("📍 [%.1fs] %s %d%% MARK %s %s", ts, marker, pct, marker, standings), "event-announcer")
				}
				reportedCheckpoints[pct] = true
			}
		}

		// --- Pack proximity commentary ---
		if len(positions) >= 3 && (t-lastPackUpdateTick) >= 30 && t > 10 {
			frontGap := positions[0].pos - positions[len(positions)-1].pos
			if frontGap < 20 && frontGap > 0 {
				add(t, fmt.Sprintf("🏇 [%.1fs] The pack is TIGHT! Only %.0fm separating first from last! Anyone's race!", ts, frontGap), "event-announcer")
				lastPackUpdateTick = t
			} else if len(positions) >= 2 {
				gapToSecond := positions[0].pos - positions[1].pos
				if gapToSecond > 50 {
					add(t, fmt.Sprintf("🔭 [%.1fs] %s is RUNNING AWAY with it! %.0fm lead — the others can barely see them!", ts, positions[0].name, gapToSecond), "event-burst")
					lastPackUpdateTick = t
				}
			}
		}

		// --- Horse events ---
		for i := range race.Entries {
			ref, ok := tickData[i][t]
			if !ok {
				continue
			}
			name := race.Entries[i].HorseName

			canReport := func(evtKey string) bool {
				maxN := eventMaxPerHorse[evtKey]
				if maxN == 0 {
					maxN = 3
				}
				count := eventCounts[i][evtKey]
				if count > maxN {
					return false
				}
				if count == maxN {
					if msg, ok := eventCapMsg[evtKey]; ok {
						add(t, fmt.Sprintf("[%.1fs] %s", ts, fmt.Sprintf(msg, name)), "event-panic")
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
					msgs := []string{
						fmt.Sprintf("😱 [%.1fs] %s: PANIC! They've completely lost it! Dead stop on the track!", ts, name),
						fmt.Sprintf("🫣 [%.1fs] OH NO! %s has SPOOKED! They're frozen in place! The crowd gasps!", ts, name),
						fmt.Sprintf("💥 [%.1fs] %s: TOTAL MELTDOWN! Legs locked, eyes wide — that horse is NOT moving!", ts, name),
						fmt.Sprintf("😱 [%.1fs] %s just remembered its browser history isn't deleted! FULL STOP!", ts, name),
						fmt.Sprintf("🫣 [%.1fs] Someone played a Derulo song near the track — %s has entered a catatonic state!", ts, name),
						fmt.Sprintf("💥 [%.1fs] %s heard a B.U.R.P. siren and FROZE! Containment trauma is REAL!", ts, name),
					}
					add(t, msgs[eventCounts[i]["PANIC"]%len(msgs)], "event-panic")
				}
			case "ANOMALOUS ACCELERATION":
				if canReport("ANOMALOUS ACCELERATION") {
					msgs := []string{
						fmt.Sprintf("🧪 [%.1fs] E-008's Chosen: ANOMALOUS ACCELERATION! The yogurt REMEMBERS! %s rockets forward with impossible speed!", ts, name),
						fmt.Sprintf("🧪 [%.1fs] %s: The yogurt containment field FLUCTUATES! E-008 sends its REGARDS! B.U.R.P. sirens blare from the observation deck!", ts, name),
					}
					add(t, msgs[eventCounts[i]["ANOMALOUS ACCELERATION"]%len(msgs)], "event-e008")
				}
			case "GHOST SIGHTING":
				if canReport("GHOST SIGHTING") {
					msgs := []string{
						fmt.Sprintf("👻 [%.1fs] %s: GHOST SIGHTING! A spectral figure drifts across the track! The horse RECOILS in terror!", ts, name),
						fmt.Sprintf("👻 [%.1fs] %s sees something that ISN'T THERE! Or IS it?! B.U.R.P. agents are taking notes from the stands!", ts, name),
						fmt.Sprintf("👻 [%.1fs] The ghost of a Victorian-era jockey appears before %s! It offers unsolicited form advice! The horse is NOT having it!", ts, name),
					}
					add(t, msgs[eventCounts[i]["GHOST SIGHTING"]%len(msgs)], "event-ghost")
				}
			case "THE LIGHTS FLICKER":
				if canReportGlobal("THE LIGHTS FLICKER") {
					add(t, fmt.Sprintf("⚫ [%.1fs] THE LIGHTS FLICKER — darkness pulses through the arena! Every horse shudders! The crowd SCREAMS!", ts), "event-ghost")
				}
			case "ANOMALOUS RESONANCE":
				if canReport("ANOMALOUS RESONANCE") {
					add(t, fmt.Sprintf("🌀 [%.1fs] %s: ANOMALOUS RESONANCE! The air hums with forbidden frequency! Reality bends around the horse!", ts, name), "event-e008")
				}
			case "THE YOGURT ERUPTS":
				if canReport("THE YOGURT ERUPTS") {
					add(t, fmt.Sprintf("🥛 [%.1fs] %s: THE YOGURT ERUPTS!! A geyser of sentient dairy launches the horse forward! E-008 CONTAINMENT BREACH!!", ts, name), "event-e008")
				}
			case "REALITY FRACTURE":
				if canReport("REALITY FRACTURE") {
					add(t, fmt.Sprintf("🌌 [%.1fs] %s: REALITY FRACTURE!! Space FOLDS — the horse BLINKS forward through a rip in spacetime! WHAT DID WE JUST WITNESS?!", ts, name), "event-e008")
				}

			case "SECOND WIND":
				if canReport("SECOND WIND") {
					msgs := []string{
						fmt.Sprintf("💨 [%.1fs] %s: SECOND WIND! From the depths of exhaustion — a SURGE! The crowd is on their FEET!", ts, name),
						fmt.Sprintf("🔥 [%.1fs] %s REFUSES TO DIE! Somehow they've found another gear! Where was THIS energy hiding?!", ts, name),
						fmt.Sprintf("💨 [%.1fs] This horse runs like it owes the yogurt money! %s EXPLODES from nowhere!", ts, name),
						fmt.Sprintf("🔥 [%.1fs] %s just channeled pure Grindussy energy! The comeback is ON!", ts, name),
					}
					add(t, msgs[eventCounts[i]["SECOND WIND"]%len(msgs)], "event-burst")
				}

			case "CAFFEINE KICK":
				if canReport("CAFFEINE KICK") {
					msgs := []string{
						fmt.Sprintf("☕ [%.1fs] %s: CAFFEINE KICK! They found a discarded oat milk latte on the track! INSTANT ENERGY! Dr. Mittens left it there. Allegedly.", ts, name),
						fmt.Sprintf("☕ [%.1fs] %s just inhaled someone's abandoned cold brew! Their eyes are ENORMOUS! They're VIBRATING!", ts, name),
						fmt.Sprintf("☕ [%.1fs] %s found a triple-shot espresso from the Oat Milk Dispensary! They're running at 2x speed and TWITCHING!", ts, name),
					}
					add(t, msgs[eventCounts[i]["CAFFEINE KICK"]%len(msgs)], "event-burst")
				}

			case "FROSTUSSY SLIP":
				if canReport("FROSTUSSY SLIP") {
					msgs := []string{
						fmt.Sprintf("🧊 [%.1fs] %s: FROSTUSSY SLIP! All four hooves go out from under them! They're SKATING not racing!", ts, name),
						fmt.Sprintf("❄️ [%.1fs] %s hits a patch of black ice and goes FULL BAMBI! Legs everywhere! The crowd winces!", ts, name),
						fmt.Sprintf("🧊 [%.1fs] %s's hooves BETRAY them on the ice! That lean build has ZERO grip! They're pinwheeling!", ts, name),
					}
					add(t, msgs[eventCounts[i]["FROSTUSSY SLIP"]%len(msgs)], "event-weather")
				}

			case "THUNDERUSSY LIGHTNING STRIKE":
				if canReportGlobal("THUNDERUSSY LIGHTNING STRIKE") {
					add(t, fmt.Sprintf("⚡ [%.1fs] THUNDERUSSY LIGHTNING STRIKE!! A bolt from the heavens SLAMS the track! Every horse RECOILS! Pastor Router's grid trembles! ALL horses lose speed!", ts), "event-weather")
				}

			case "MUDUSSY SPLATTER":
				if canReport("MUDUSSY SPLATTER") {
					msgs := []string{
						fmt.Sprintf("🟤 [%.1fs] %s: MUDUSSY SPLATTER! A wall of mud EXPLODES into their face! They can't see! They can't BREATHE!", ts, name),
						fmt.Sprintf("🟤 [%.1fs] %s just ate a TIDAL WAVE of Mudussy sludge! That horse is now 40%% mud by volume!", ts, name),
						fmt.Sprintf("💩 [%.1fs] %s gets OBLITERATED by a mud geyser from the track! The crowd is ALSO covered! Nobody is happy!", ts, name),
					}
					add(t, msgs[eventCounts[i]["MUDUSSY SPLATTER"]%len(msgs)], "event-weather")
				}

			case "CROWD SURGE":
				if canReport("CROWD SURGE") {
					msgs := []string{
						fmt.Sprintf("📣 [%.1fs] %s: CROWD SURGE! The fans ROAR! The energy is ELECTRIC! The leader feeds off the adulation!", ts, name),
						fmt.Sprintf("🎉 [%.1fs] The crowd is going ABSOLUTELY FERAL for %s! The noise is DEAFENING! It's propelling them forward!", ts, name),
						fmt.Sprintf("📣 [%.1fs] Dr. Mittens would approve — a purrfect run! %s is riding the crowd's energy to GLORY!", ts, name),
					}
					add(t, msgs[eventCounts[i]["CROWD SURGE"]%len(msgs)], "event-burst")
				}

			case "DERULO SIGHTING":
				if canReport("DERULO SIGHTING") {
					msgs := []string{
						fmt.Sprintf("🎤 [%.1fs] %s: DERULO SIGHTING! Jason Derulo is in the crowd! The horse STOPS DEAD to stare! He insists he's not here! \"Jason Derulo!\" he whispers, unable to help himself.", ts, name),
						fmt.Sprintf("🎤 [%.1fs] %s has FROZEN! Is that... JASON DERULO?! In the VIP box?! He's wearing a disguise but it's CLEARLY him! The horse is MESMERIZED!", ts, name),
						fmt.Sprintf("🎤 [%.1fs] %s: Jason Derulo just dropped his phone from the stands! The horse STOPS to return it! Derulo is MORTIFIED! 'I'm not even supposed to BE here!'", ts, name),
						fmt.Sprintf("🎤 [%.1fs] DERULO ALERT! %s hears 'Whatcha Say' from the PA system and LOCKS UP! Someone in the sound booth is getting fired!", ts, name),
					}
					add(t, msgs[eventCounts[i]["DERULO SIGHTING"]%len(msgs)], "event-panic")
				}

			case "MITTENS NAP":
				if canReport("MITTENS NAP") {
					msgs := []string{
						fmt.Sprintf("😺 [%.1fs] %s: MITTENS NAP! Dr. Mittens has materialized on the horse's back and FALLEN ASLEEP! The horse dare not disturb her! Speed PLUMMETS out of respect!", ts, name),
						fmt.Sprintf("🐱 [%.1fs] A soft *poof* and Dr. Mittens APPEARS on %s's hindquarters, curling into a perfect circle! The horse slows to a reverent tippy-tap! You do NOT wake the doctor!", ts, name),
						fmt.Sprintf("😺 [%.1fs] Dr. Mittens would approve — a purrfect nap spot! She's chosen %s as her bed and that is now LEGALLY BINDING! The horse creeps forward at minimum speed!", ts, name),
					}
					add(t, msgs[eventCounts[i]["MITTENS NAP"]%len(msgs)], "event-ghost")
				}

			case "DIVINE PACKET":
				if canReport("DIVINE PACKET") {
					msgs := []string{
						fmt.Sprintf("📡 [%.1fs] %s: DIVINE PACKET! A golden beam of pure 802.11ax descends from the clouds! Pastor Router McEthernet III has sent his BLESSING! Speed BOOST!", ts, name),
						fmt.Sprintf("🙏 [%.1fs] %s receives a DIVINE PACKET from Pastor Router! The horse's latency drops to ZERO! They're running on GOD'S OWN BANDWIDTH!", ts, name),
						fmt.Sprintf("📡 [%.1fs] Pastor Router blesses this performance from the commentary booth! %s's TCP handshake with victory is COMPLETE! Zero packet loss!", ts, name),
					}
					add(t, msgs[eventCounts[i]["DIVINE PACKET"]%len(msgs)], "event-burst")
				}

			case "GEOFFRUSSY OPTIMIZATION":
				if canReport("GEOFFRUSSY OPTIMIZATION") {
					msgs := []string{
						fmt.Sprintf("⚙️ [%.1fs] %s: GEOFFRUSSY OPTIMIZATION! The Go orchestrator has optimized their race line! Goroutines DEPLOYED! The path to the finish is CALCULATED!", ts, name),
						fmt.Sprintf("🔧 [%.1fs] Geoffrussy's pipeline kicks in for %s! runtime.GOMAXPROCS set to MAXIMUM! The horse's route is now O(1) to the finish line!", ts, name),
						fmt.Sprintf("⚙️ [%.1fs] Geoffrussy's pipeline just deployed a hotfix mid-race! %s's legs have been RECOMPILED with optimizations! -O3 galloping engaged!", ts, name),
						fmt.Sprintf("🔧 [%.1fs] B.U.R.P. agents are monitoring %s closely... but Geoffrussy already pushed a patch! The horse's CI/CD pipeline is PRISTINE!", ts, name),
					}
					add(t, msgs[eventCounts[i]["GEOFFRUSSY OPTIMIZATION"]%len(msgs)], "event-burst")
				}
			}

			// Frostussy ice slip detection.
			if race.TrackType == models.TrackFrostussy && ref.speed < 0.5 && ref.event == "" {
				if ref.speed > 0 && ref.speed < 0.3 {
					if canReport("ICE SLIP") {
						add(t, fmt.Sprintf("🧊 [%.1fs] %s: SLIPPING ON ICE! Hooves scramble desperately for grip! They're losing ground!", ts, name), "event-weather")
					}
				}
			}
		}

		// --- Thunderussy lightning ---
		if race.TrackType == models.TrackThunderussy && (t-lastLightningTick) >= 40 {
			if t%50 < 5 || (t > 100 && t%73 < 3) {
				add(t, fmt.Sprintf("⚡ [%.1fs] LIGHTNING STRIKES the Thunderussy track! The ground TREMBLES! Horses scatter!", ts), "event-weather")
				lastLightningTick = t
			}
		}

		// --- Finish announcements ---
		for _, p := range positions {
			if p.pos >= distance && !announcedFinish[p.idx] {
				place := 0
				for _, e := range race.Entries {
					if e.HorseID == race.Entries[p.idx].HorseID {
						place = e.FinishPlace
						break
					}
				}
				switch place {
				case 1:
					add(t, fmt.Sprintf("🏆 [%.1fs] %s CROSSES THE LINE FIRST!! WHAT A RACE!! THE CROWD ERUPTS!!", ts, p.name), "event-burst")
				case 2:
					add(t, fmt.Sprintf("🥈 [%.1fs] %s finishes 2nd! SO close! The crowd groans!", ts, p.name), "event-announcer")
				case 3:
					add(t, fmt.Sprintf("🥉 [%.1fs] %s claims the final podium spot in 3rd!", ts, p.name), "event-announcer")
				default:
					if place > 0 {
						add(t, fmt.Sprintf("🏁 [%.1fs] %s crosses the line in %s place.", ts, p.name, ordinal(place)), "event-normal")
					}
				}
				announcedFinish[p.idx] = true
			}
		}

		// --- Photo finish detection ---
		var finishersThisTick []posAt
		for _, p := range positions {
			if p.pos >= distance {
				finishersThisTick = append(finishersThisTick, p)
			}
		}
		if len(finishersThisTick) >= 2 {
			if math.Abs(finishersThisTick[0].pos-finishersThisTick[1].pos) <= 0.5 {
				add(t, fmt.Sprintf("📸 [%.1fs] PHOTO FINISH between %s and %s!! Separated by MILLIMETRES!!", ts, finishersThisTick[0].name, finishersThisTick[1].name), "event-burst")
			}
		}
	}

	// --- Final results ---
	add(maxTick+1, "", "event-normal")
	add(maxTick+1, "═══════════════════════════════════", "event-announcer")
	add(maxTick+1, "🏁  F I N A L   R E S U L T S  🏁", "event-announcer")
	add(maxTick+1, "═══════════════════════════════════", "event-announcer")

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
			flairs := []string{
				" — 🏆 CHAMPION! ABSOLUTE UNIT!",
				" — 🏆 Dr. Mittens would approve — a purrfect victory!",
				" — 🏆 Geoffrussy's pipeline rates this run: FLAWLESS!",
				" — 🏆 CHAMPION! The Ussyverse BOWS before this horse!",
				" — 🏆 Pastor Router blesses this performance! Amen!",
			}
			flair = flairs[rand.IntN(len(flairs))]
		case 2:
			flairs := []string{
				" — 🥈 So close. The crowd weeps.",
				" — 🥈 Almost had it. Jason Derulo sends sympathies (unwillingly).",
				" — 🥈 Silver is just gold with commitment issues.",
			}
			flair = flairs[rand.IntN(len(flairs))]
		case 3:
			flairs := []string{
				" — 🥉 Podium secured. Respectable.",
				" — 🥉 Bronze! B.U.R.P. has no complaints (for once).",
				" — 🥉 Third place! The yogurt acknowledges your effort.",
			}
			flair = flairs[rand.IntN(len(flairs))]
		default:
			flairs := []string{
				" — Ran their heart out.",
				" — An honest effort. Margaret Chen nods stoically.",
				" — Finished upright. That's more than some can say.",
				" — The Sappho Scale rates this run: 'participated.'",
			}
			flair = flairs[rand.IntN(len(flairs))]
		}

		add(maxTick+2, fmt.Sprintf("  %s: %s (%s)%s", placeStr, e.HorseName, timeStr, flair), "event-announcer")
	}

	return lines
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
