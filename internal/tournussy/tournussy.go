// Package tournussy implements the tournament system, race history tracking,
// weather effects, and achievement definitions for StallionUSSY.
// It ties together the racing engine with persistent results, multi-round
// competitive events, and atmospheric chaos.
package tournussy

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/racussy"
)

// ===========================================================================
// Race History Manager
// ===========================================================================

// RaceHistory stores all race results with indexed lookups by horse and race.
// All methods are safe for concurrent use.
type RaceHistory struct {
	mu      sync.RWMutex
	results []*models.RaceResult            // all results, most recent first
	byHorse map[string][]*models.RaceResult // horseID -> results
	byRace  map[string][]*models.RaceResult // raceID -> results
}

// NewRaceHistory creates an empty race history store.
func NewRaceHistory() *RaceHistory {
	return &RaceHistory{
		results: make([]*models.RaceResult, 0),
		byHorse: make(map[string][]*models.RaceResult),
		byRace:  make(map[string][]*models.RaceResult),
	}
}

// RecordResult stores a race result, prepending it (most recent first) to
// the global list and both indexes.
func (rh *RaceHistory) RecordResult(result *models.RaceResult) {
	rh.mu.Lock()
	defer rh.mu.Unlock()

	// Prepend to global list — newest first.
	rh.results = append([]*models.RaceResult{result}, rh.results...)

	// Index by horse — also prepend for newest-first ordering.
	rh.byHorse[result.HorseID] = append(
		[]*models.RaceResult{result},
		rh.byHorse[result.HorseID]...,
	)

	// Index by race — order doesn't matter much here (all from same race),
	// but we prepend for consistency.
	rh.byRace[result.RaceID] = append(
		[]*models.RaceResult{result},
		rh.byRace[result.RaceID]...,
	)
}

// ImportResults bulk-loads pre-sorted race results (newest first) into the
// history store. This is used during startup to hydrate in-memory state from
// the database. Existing results are preserved; imported results are appended
// after any current entries.
func (rh *RaceHistory) ImportResults(results []*models.RaceResult) {
	rh.mu.Lock()
	defer rh.mu.Unlock()

	rh.results = append(rh.results, results...)

	for _, r := range results {
		rh.byHorse[r.HorseID] = append(rh.byHorse[r.HorseID], r)
		rh.byRace[r.RaceID] = append(rh.byRace[r.RaceID], r)
	}
}

// GetHorseHistory returns all results for a horse, most recent first.
// Returns nil if the horse has no recorded results.
func (rh *RaceHistory) GetHorseHistory(horseID string) []*models.RaceResult {
	rh.mu.RLock()
	defer rh.mu.RUnlock()

	results := rh.byHorse[horseID]
	if results == nil {
		return nil
	}

	// Return a copy to prevent external mutation.
	out := make([]*models.RaceResult, len(results))
	copy(out, results)
	return out
}

// GetRaceResults returns all results from a specific race.
// Returns nil if the race has no recorded results.
func (rh *RaceHistory) GetRaceResults(raceID string) []*models.RaceResult {
	rh.mu.RLock()
	defer rh.mu.RUnlock()

	results := rh.byRace[raceID]
	if results == nil {
		return nil
	}

	out := make([]*models.RaceResult, len(results))
	copy(out, results)
	return out
}

// GetRecentResults returns the N most recent results across all horses.
// If limit exceeds the total count, all results are returned.
func (rh *RaceHistory) GetRecentResults(limit int) []*models.RaceResult {
	rh.mu.RLock()
	defer rh.mu.RUnlock()

	if limit <= 0 {
		return nil
	}
	if limit > len(rh.results) {
		limit = len(rh.results)
	}

	out := make([]*models.RaceResult, limit)
	copy(out, rh.results[:limit])
	return out
}

// ---------------------------------------------------------------------------
// HorseStats — computed aggregate statistics for a single horse
// ---------------------------------------------------------------------------

// HorseStats holds computed performance statistics derived from race history.
type HorseStats struct {
	TotalRaces     int              `json:"total_races"`
	Wins           int              `json:"wins"`
	Places         int              `json:"places"`   // top 3 finishes
	WinRate        float64          `json:"win_rate"` // 0.0 - 1.0
	AvgFinishPlace float64          `json:"avg_place"`
	BestTime       time.Duration    `json:"best_time"`
	WorstTime      time.Duration    `json:"worst_time"`
	FavoriteTrack  models.TrackType `json:"favorite_track"` // track with the most wins
	TotalEarnings  int64            `json:"total_earnings"`
	CurrentStreak  int              `json:"current_streak"` // positive = win streak, negative = loss streak
	BestStreak     int              `json:"best_streak"`    // longest win streak ever
}

// GetHorseStats computes aggregate statistics for a horse from their full
// race history. Returns a zero-value HorseStats if the horse has no results.
func (rh *RaceHistory) GetHorseStats(horseID string) HorseStats {
	rh.mu.RLock()
	defer rh.mu.RUnlock()

	results := rh.byHorse[horseID]
	if len(results) == 0 {
		return HorseStats{}
	}

	stats := HorseStats{
		TotalRaces: len(results),
	}

	var totalPlace int
	trackWins := make(map[models.TrackType]int)

	// Track streaks. Results are newest-first, so we iterate in reverse
	// (chronological order) for streak calculation.
	streakCalcResults := make([]*models.RaceResult, len(results))
	copy(streakCalcResults, results)
	// Reverse to chronological order.
	for i, j := 0, len(streakCalcResults)-1; i < j; i, j = i+1, j-1 {
		streakCalcResults[i], streakCalcResults[j] = streakCalcResults[j], streakCalcResults[i]
	}

	currentStreak := 0
	bestStreak := 0

	for i, r := range streakCalcResults {
		// Win/place counts
		if r.FinishPlace == 1 {
			trackWins[r.TrackType]++
		}
		if r.FinishPlace <= 3 {
			stats.Places++
		}

		totalPlace += r.FinishPlace
		stats.TotalEarnings += r.Earnings

		// Best and worst times (skip zero-duration entries).
		if r.FinalTime > 0 {
			if stats.BestTime == 0 || r.FinalTime < stats.BestTime {
				stats.BestTime = r.FinalTime
			}
			if r.FinalTime > stats.WorstTime {
				stats.WorstTime = r.FinalTime
			}
		}

		// Streak tracking (chronological order).
		if r.FinishPlace == 1 {
			if i == 0 || currentStreak > 0 {
				currentStreak++
			} else {
				currentStreak = 1
			}
			if currentStreak > bestStreak {
				bestStreak = currentStreak
			}
		} else {
			if i == 0 || currentStreak < 0 {
				currentStreak--
			} else {
				currentStreak = -1
			}
		}
	}

	stats.Wins = trackWins[models.TrackSprintussy] +
		trackWins[models.TrackGrindussy] +
		trackWins[models.TrackMudussy] +
		trackWins[models.TrackThunderussy] +
		trackWins[models.TrackFrostussy] +
		trackWins[models.TrackHauntedussy]

	if stats.TotalRaces > 0 {
		stats.WinRate = float64(stats.Wins) / float64(stats.TotalRaces)
		stats.AvgFinishPlace = float64(totalPlace) / float64(stats.TotalRaces)
	}

	stats.CurrentStreak = currentStreak
	stats.BestStreak = bestStreak

	// Determine favorite track (track with most wins).
	bestTrackWins := 0
	for track, wins := range trackWins {
		if wins > bestTrackWins {
			bestTrackWins = wins
			stats.FavoriteTrack = track
		}
	}

	return stats
}

// ===========================================================================
// Weather System
// ===========================================================================

// weatherEntry pairs a weather type with its cumulative weight for selection.
type weatherEntry struct {
	weather models.Weather
	weight  int // cumulative weight out of 100
}

// weatherTable defines the weighted probability distribution for weather.
// Weights: Clear=35, Rainy=25, Stormy=15, Foggy=10, Scorching=10, Haunted=5.
var weatherTable = []weatherEntry{
	{models.WeatherClear, 35},
	{models.WeatherRainy, 60},     // 35 + 25
	{models.WeatherStormy, 75},    // 60 + 15
	{models.WeatherFoggy, 85},     // 75 + 10
	{models.WeatherScorching, 95}, // 85 + 10
	{models.WeatherHaunted, 100},  // 95 + 5
}

// RandomWeather returns a weighted-random weather condition.
// Haunted weather is only valid on Hauntedussy tracks — on other tracks,
// Haunted results are rerolled until a non-Haunted weather is selected.
func RandomWeather() models.Weather {
	return RandomWeatherForTrack("")
}

// RandomWeatherForTrack returns a weighted-random weather condition
// appropriate for the given track type. Haunted weather is only allowed
// on Hauntedussy; on other tracks it is rerolled. On Hauntedussy, the
// weather is always forced to Haunted.
func RandomWeatherForTrack(trackType models.TrackType) models.Weather {
	// Hauntedussy always gets Haunted weather — the spirits demand it.
	if trackType == models.TrackHauntedussy {
		return models.WeatherHaunted
	}

	// Roll weather with reroll on Haunted for non-Haunted tracks.
	for {
		roll := rand.IntN(100)
		for _, entry := range weatherTable {
			if roll < entry.weight {
				if entry.weather == models.WeatherHaunted {
					break // reroll — Haunted not allowed on this track
				}
				return entry.weather
			}
		}
		// If we broke out of the inner loop (Haunted reroll), continue outer loop.
	}
}

// ---------------------------------------------------------------------------
// Weather Modifiers — how each weather affects race physics
// ---------------------------------------------------------------------------

// WeatherModifiers holds the multiplicative effects of weather on race physics.
type WeatherModifiers struct {
	SpeedMod    float64 // multiplier on base speed
	FatigueMod  float64 // multiplier on fatigue rate
	ChaosMod    float64 // multiplier on chaos sigma
	PanicMod    float64 // multiplier on panic chance
	Description string  // flavor text for the announcer
}

// WeatherEffects returns the race-physics modifiers for a given weather type.
func WeatherEffects(weather models.Weather) WeatherModifiers {
	switch weather {
	case models.WeatherClear:
		return WeatherModifiers{
			SpeedMod:    1.0,
			FatigueMod:  1.0,
			ChaosMod:    1.0,
			PanicMod:    1.0,
			Description: "Perfect racing conditions.",
		}
	case models.WeatherRainy:
		return WeatherModifiers{
			SpeedMod:    0.95,
			FatigueMod:  1.1,
			ChaosMod:    1.3,
			PanicMod:    1.5,
			Description: "The track is slick. Footing is treacherous.",
		}
	case models.WeatherStormy:
		return WeatherModifiers{
			SpeedMod:    0.85,
			FatigueMod:  1.3,
			ChaosMod:    2.0,
			PanicMod:    2.0,
			Description: "Thunder cracks! Lightning illuminates the track!",
		}
	case models.WeatherFoggy:
		return WeatherModifiers{
			SpeedMod:    0.90,
			FatigueMod:  1.0,
			ChaosMod:    1.5,
			PanicMod:    1.2,
			Description: "Visibility near zero. Horses are racing blind.",
		}
	case models.WeatherScorching:
		return WeatherModifiers{
			SpeedMod:    0.92,
			FatigueMod:  1.5,
			ChaosMod:    0.8,
			PanicMod:    0.8,
			Description: "Blistering heat. Stamina will be tested.",
		}
	case models.WeatherHaunted:
		return WeatherModifiers{
			SpeedMod:    1.0,
			FatigueMod:  0.8,
			ChaosMod:    3.0,
			PanicMod:    3.0,
			Description: "Something is watching from beyond the rail. The temperature drops 15 degrees.",
		}
	default:
		// Unknown weather defaults to clear — safety first, chaos second.
		return WeatherModifiers{
			SpeedMod:    1.0,
			FatigueMod:  1.0,
			ChaosMod:    1.0,
			PanicMod:    1.0,
			Description: "Conditions unknown. Proceed with caution.",
		}
	}
}

// ===========================================================================
// Tournament Manager
// ===========================================================================

// TournamentManager orchestrates multi-round competitive events with
// point-based standings and prize pools.
type TournamentManager struct {
	mu          sync.RWMutex
	tournaments map[string]*models.Tournament
	history     *RaceHistory
}

// NewTournamentManager creates a TournamentManager linked to the given
// race history store.
func NewTournamentManager(history *RaceHistory) *TournamentManager {
	return &TournamentManager{
		tournaments: make(map[string]*models.Tournament),
		history:     history,
	}
}

// ---------------------------------------------------------------------------
// Tournament Name Generation — because every cup needs a stupid name
// ---------------------------------------------------------------------------

var tournamentAdjectives = []string{
	// ---- Original 20 ----
	"Golden", "Midnight", "Sapphic", "Haunted", "Thunderous",
	"Forbidden", "Eternal", "Cosmic", "Cryogenic", "Volatile",
	"Iridescent", "Anomalous", "Sovereign", "Unholy", "Quantum",
	"Legendary", "Eldritch", "Magnificent", "Disastrous", "Suspicious",
	// ---- Expanded (Ussyverse lore) ----
	"Caffeinated", "Gluten-Free", "Decentralized", "Containerized",
	"Artisanal", "Sentient", "Prophetic", "Bioluminescent",
	"Thrice-Blessed", "Weaponized", "Ecclesiastical", "Yogurt-Adjacent",
	"Cloud-Native", "Zero-Downtime", "Uncontained", "Recursive",
	"Fermented", "Asymptotic", "Haunted-Adjacent", "Overclocked",
	"Sourdough-Infused", "Ethernet-Blessed", "Defragmented", "Unbothered",
	// ---- Batch 2: Even more adjectives (Ussyverse deep cuts) ----
	"Tax-Exempt", "Probiotic", "Load-Balanced", "Mildly-Haunted",
	"Dry-Aged", "Hyperthreaded", "Sapphically-Charged", "Unpatched",
	"Free-Range", "Turbo-Blessed", "Non-Euclidean", "Bluetooth-Enabled",
	"Triple-Fermented", "Ketogenic", "Bureaucratically-Approved",
	"Poorly-Documented", "Self-Replicating", "Microserviced",
	"Glitch-Hardened", "Oat-Milk-Powered",
}

var tournamentNouns = []string{
	// ---- Original 20 ----
	"Stallion", "Crown", "Legacy", "Chalice", "Yogurt",
	"Trophy", "Flannel", "Gauntlet", "Reckoning", "Convergence",
	"Meridian", "Tempest", "Horizon", "Catalyst", "Biscuit",
	"Uprising", "Phantom", "Prophecy", "Communion", "Pipeline",
	// ---- Expanded (Ussyverse lore) ----
	"Invitational", "Showdown", "Apocalypse", "Deployment", "Sermon",
	"Audit", "Containment", "Algorithm", "Thunderdome",
	"Absolution", "Compilation", "Sprint", "Marathon", "Odyssey",
	"Singularity", "Sacrament", "Firmware", "Stampede",
	"Inquisition", "Symposium", "Referendum", "Exorcism",
	"Cummification", "Rebuke",
	// ---- Batch 2: Even more nouns (Ussyverse deep cuts) ----
	"Baptism", "Rollback", "Stampede", "Confessional",
	"Calibration", "Recklessness", "Tribunal", "Defragmentation",
	"Transfiguration", "Benchmark", "Purification", "Shakedown",
	"Overflow", "Reconciliation", "Bloodmoon", "Pilgrimage",
	"Manifesto", "Thunderclap", "Incantation", "Firmware-Update",
}

var tournamentPlaces = []string{
	// ---- Original 14 ----
	"Lesbos", "Delaware", "Building 7", "the Ussyverse", "Sappho Valley",
	"the Forbidden Stable", "the Quantum Paddock", "Goroutine Gulch",
	"Haunted Meadows", "the Midnight Corral", "New Flannel City",
	"the Yogurt Wastes", "E-008's Domain", "the Anomaly Zone",
	// ---- Expanded (Ussyverse lore) ----
	"Pastor Router's Cathedral", "Dr. Mittens' Inspection Room",
	"Derulo's Living Room (unauthorized)", "Geoffrussy's Server Room",
	"Building 7 Subbasement", "Margaret Chen's Kentucky Estate",
	"the STARDUSTUSSY Timeline", "Agent Mothman's Observatory",
	"the Sourdough District", "the Flannel Coast",
	"the Kubernetes Cluster", "the Legacy Codebase",
	"the Forbidden Repository", "the Yogurt Containment Zone",
	"Sappho's Library", "the B.U.R.P. Regional Office",
	"the Ethernet Cathedral Annex", "the Oat Milk Dispensary",
	"the Haunted Server Closet", "the Cummies Vault",
	"the Grindussy Training Grounds", "the ISO 69420 Compliance Lab",
	"the Thunderussy Arena", "the Cottagecore Commune",
	"Margaret Chen's Secret Garden", "the Recursive Meadow",
	"the Anomalous Paddock Overflow", "the Sourdough Fermenting Room",
	// ---- Batch 2: Even more places (Ussyverse deep cuts) ----
	"Jason Derulo's Dressing Room (he said no)", "the Sentient Yogurt Vat",
	"the Decommissioned Goroutine Graveyard", "the Forbidden Flannel Archive",
	"the Sappho Scale Calibration Lab", "Agent Mothman's Rooftop Perch",
	"the Oat Milk Hot Springs", "the B.U.R.P. Evidence Locker",
	"STARDUSTUSSY's Temporal Lobby", "the Cummies Federal Reserve",
	"the Hauntedussy Gift Shop", "Margaret Chen's Forbidden Genome Vault",
	"the Ethernet Cathedral Crypt", "Pastor Router's Confession Booth",
	"the Quantum Hay Bale Storage", "the Docker Container Pasture",
	"the Yogurt-Stained Amphitheater", "E-008's Therapy Office",
	"the Cottagecore War Room", "the Sprintussy Victory Lap Lounge",
}

// generateTournamentName creates an absurd tournament name from templates.
func generateTournamentName() string {
	pickStr := func(list []string) string {
		return list[rand.IntN(len(list))]
	}

	patterns := []func() string{
		func() string {
			return fmt.Sprintf("The %s %s Cup", pickStr(tournamentAdjectives), pickStr(tournamentNouns))
		},
		func() string {
			return fmt.Sprintf("Championship of %s", pickStr(tournamentPlaces))
		},
		func() string {
			return fmt.Sprintf("The Grand %s", pickStr(tournamentNouns))
		},
		func() string {
			return fmt.Sprintf("The %s %s Invitational", pickStr(tournamentAdjectives), pickStr(tournamentNouns))
		},
		func() string {
			return fmt.Sprintf("%s %s Classic", pickStr(tournamentAdjectives), pickStr(tournamentNouns))
		},
		// ---- New patterns (Ussyverse lore) ----
		func() string {
			return fmt.Sprintf("The B.U.R.P. %s Investigation", pickStr(tournamentNouns))
		},
		func() string {
			return fmt.Sprintf("%s %s %s", pickStr(tournamentAdjectives), pickStr(tournamentAdjectives), pickStr(tournamentNouns))
		},
		func() string {
			return fmt.Sprintf("Pastor Router's %s %s Sermon", pickStr(tournamentAdjectives), pickStr(tournamentNouns))
		},
	}

	return patterns[rand.IntN(len(patterns))]()
}

// CreateTournament initializes a new tournament. If name is empty, a glorious
// name is auto-generated. PrizePool = entryFee * 6 (estimated entries) * rounds.
func (tm *TournamentManager) CreateTournament(name string, trackType models.TrackType, rounds int, entryFee int64) *models.Tournament {
	if name == "" {
		name = generateTournamentName()
	}

	if rounds <= 0 {
		rounds = 1
	}

	// PrizePool = entryFee * estimated 6 entries * rounds
	prizePool := entryFee * 6 * int64(rounds)

	t := &models.Tournament{
		ID:           uuid.New().String(),
		Name:         name,
		TrackType:    trackType,
		Rounds:       rounds,
		CurrentRound: 0,
		EntryFee:     entryFee,
		PrizePool:    prizePool,
		Standings:    []models.TournamentEntry{},
		Races:        []string{},
		Status:       "Open",
		CreatedAt:    time.Now(),
	}

	tm.mu.Lock()
	tm.tournaments[t.ID] = t
	tm.mu.Unlock()

	return t
}

// RegisterHorse adds a horse to a tournament's standings.
// Returns an error if the tournament isn't Open or the horse is already registered.
func (tm *TournamentManager) RegisterHorse(tournamentID string, horse *models.Horse, stableID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	t, ok := tm.tournaments[tournamentID]
	if !ok {
		return fmt.Errorf("tournament %s not found", tournamentID)
	}

	if t.Status != "Open" {
		return fmt.Errorf("tournament %s is not open for registration (status: %s)", t.Name, t.Status)
	}

	// Check for duplicate entries.
	for _, entry := range t.Standings {
		if entry.HorseID == horse.ID {
			return fmt.Errorf("horse %s (%s) is already registered in tournament %s", horse.Name, horse.ID, t.Name)
		}
	}

	t.Standings = append(t.Standings, models.TournamentEntry{
		HorseID:   horse.ID,
		HorseName: horse.Name,
		StableID:  stableID,
		Points:    0,
		RacesRun:  0,
		BestPlace: 0,
	})

	return nil
}

// RunNextRound creates a race for the next round of a tournament.
// On the first call, the tournament auto-transitions from "Open" to "InProgress".
// Returns the race (un-simulated) for the caller to run via racussy.SimulateRace.
// If all rounds have been completed, the tournament is marked "Finished".
func (tm *TournamentManager) RunNextRound(tournamentID string, horses []*models.Horse) (*models.Race, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	t, ok := tm.tournaments[tournamentID]
	if !ok {
		return nil, fmt.Errorf("tournament %s not found", tournamentID)
	}

	// Validate status — must be Open or InProgress.
	if t.Status != "Open" && t.Status != "InProgress" {
		return nil, fmt.Errorf("tournament %s is %s, cannot run next round", t.Name, t.Status)
	}

	// Auto-start on first round.
	if t.Status == "Open" {
		t.Status = "InProgress"
	}

	// Check if all rounds are done.
	if t.CurrentRound >= t.Rounds {
		t.Status = "Finished"
		return nil, fmt.Errorf("tournament %s is finished (%d/%d rounds complete)", t.Name, t.CurrentRound, t.Rounds)
	}

	// Increment current round.
	t.CurrentRound++

	// Create the race using racussy. Purse is a fraction of the prize pool
	// distributed per round.
	roundPurse := t.PrizePool / int64(t.Rounds)
	race := racussy.NewRace(horses, t.TrackType, roundPurse)

	return race, nil
}

// pointsForPlace returns the tournament points awarded for a given finish position.
//
//	1st=10, 2nd=7, 3rd=5, 4th=3, 5th=2, others=1.
func pointsForPlace(place int) int {
	switch place {
	case 1:
		return 10
	case 2:
		return 7
	case 3:
		return 5
	case 4:
		return 3
	case 5:
		return 2
	default:
		return 1
	}
}

// RecordRoundResults updates a tournament's standings based on a completed race.
// Points are awarded per finish place: 1st=10, 2nd=7, 3rd=5, 4th=3, 5th=2, others=1.
// The race ID is appended to the tournament's Races slice.
func (tm *TournamentManager) RecordRoundResults(tournamentID string, race *models.Race) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	t, ok := tm.tournaments[tournamentID]
	if !ok {
		return fmt.Errorf("tournament %s not found", tournamentID)
	}

	// Append race ID to tournament's race list.
	t.Races = append(t.Races, race.ID)

	// Build a lookup from horseID -> RaceEntry for quick access.
	entryMap := make(map[string]*models.RaceEntry, len(race.Entries))
	for i := range race.Entries {
		entryMap[race.Entries[i].HorseID] = &race.Entries[i]
	}

	// Update each standing entry.
	for i := range t.Standings {
		standing := &t.Standings[i]
		raceEntry, ok := entryMap[standing.HorseID]
		if !ok {
			continue // horse wasn't in this round's race
		}

		standing.Points += pointsForPlace(raceEntry.FinishPlace)
		standing.RacesRun++

		// Update best place (lower is better, 0 means unset).
		if standing.BestPlace == 0 || raceEntry.FinishPlace < standing.BestPlace {
			standing.BestPlace = raceEntry.FinishPlace
		}
	}

	// Check if tournament is now finished.
	if t.CurrentRound >= t.Rounds {
		t.Status = "Finished"
	}

	return nil
}

// GetStandings returns the tournament standings sorted by points descending.
// Ties are broken by best place (lower is better), then by races run (more is better).
func (tm *TournamentManager) GetStandings(tournamentID string) []models.TournamentEntry {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	t, ok := tm.tournaments[tournamentID]
	if !ok {
		return nil
	}

	// Copy standings to avoid mutating the original.
	standings := make([]models.TournamentEntry, len(t.Standings))
	copy(standings, t.Standings)

	sort.Slice(standings, func(i, j int) bool {
		if standings[i].Points != standings[j].Points {
			return standings[i].Points > standings[j].Points
		}
		// Tiebreaker 1: better best place
		if standings[i].BestPlace != standings[j].BestPlace {
			// 0 means unset — treat as worse than any actual place.
			if standings[i].BestPlace == 0 {
				return false
			}
			if standings[j].BestPlace == 0 {
				return true
			}
			return standings[i].BestPlace < standings[j].BestPlace
		}
		// Tiebreaker 2: more races run
		return standings[i].RacesRun > standings[j].RacesRun
	})

	return standings
}

// ListTournaments returns all tournaments (copies of the pointers).
func (tm *TournamentManager) ListTournaments() []*models.Tournament {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	list := make([]*models.Tournament, 0, len(tm.tournaments))
	for _, t := range tm.tournaments {
		list = append(list, t)
	}

	// Sort by creation time, newest first.
	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.After(list[j].CreatedAt)
	})

	return list
}

// GetTournament returns a single tournament by ID.
func (tm *TournamentManager) GetTournament(id string) (*models.Tournament, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	t, ok := tm.tournaments[id]
	if !ok {
		return nil, fmt.Errorf("tournament %s not found", id)
	}
	return t, nil
}

// ImportTournament adds a previously-persisted tournament to the in-memory
// map. This is used during startup to hydrate state from the database.
// If a tournament with the same ID already exists, it is overwritten.
func (tm *TournamentManager) ImportTournament(t *models.Tournament) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.tournaments[t.ID] = t
}

// ===========================================================================
// Achievement Definitions
// ===========================================================================

// AllAchievements defines every unlockable achievement in StallionUSSY.
// Keys are stable achievement IDs. Achievements are checked dynamically
// against race history and horse/stable state.
var AllAchievements = map[string]models.Achievement{
	// -----------------------------------------------------------------------
	// Horse Achievements
	// -----------------------------------------------------------------------
	"first_blood": {
		ID:          "first_blood",
		Name:        "First Blood",
		Description: "Win your first race",
		Icon:        "\U0001F3C6", // 🏆
		Rarity:      "common",
	},
	"triple_crown": {
		ID:          "triple_crown",
		Name:        "Triple Crown",
		Description: "Win on all 3 original track types",
		Icon:        "\U0001F451", // 👑
		Rarity:      "epic",
	},
	"undefeated_5": {
		ID:          "undefeated_5",
		Name:        "Untouchable",
		Description: "Win 5 races in a row",
		Icon:        "\U0001F525", // 🔥
		Rarity:      "rare",
	},
	"undefeated_10": {
		ID:          "undefeated_10",
		Name:        "Legendary Streak",
		Description: "Win 10 races in a row",
		Icon:        "\u26A1", // ⚡
		Rarity:      "legendary",
	},
	"underdog": {
		ID:          "underdog",
		Name:        "Underdog Victory",
		Description: "Win a race with the lowest ELO entry",
		Icon:        "\U0001F434", // 🐴
		Rarity:      "rare",
	},
	"mud_master": {
		ID:          "mud_master",
		Name:        "Mud Master",
		Description: "Win 5 Mudussy races",
		Icon:        "\U0001F40A", // 🐊
		Rarity:      "rare",
	},
	"speed_demon": {
		ID:          "speed_demon",
		Name:        "Speed Demon",
		Description: "Win a Sprintussy in under 10 seconds",
		Icon:        "\U0001F4A8", // 💨
		Rarity:      "epic",
	},
	"marathon_king": {
		ID:          "marathon_king",
		Name:        "Marathon King",
		Description: "Win a Grindussy without panic",
		Icon:        "\U0001F3CB\uFE0F", // 🏋️
		Rarity:      "rare",
	},
	"photo_finish": {
		ID:          "photo_finish",
		Name:        "By a Nose",
		Description: "Win by less than 0.5m",
		Icon:        "\U0001F4F8", // 📸
		Rarity:      "rare",
	},
	"the_yogurt_sees": {
		ID:          "the_yogurt_sees",
		Name:        "The Yogurt Sees All",
		Description: "Race against E-008's Chosen and survive",
		Icon:        "\U0001F300", // 🌀
		Rarity:      "legendary",
	},
	"century_horse": {
		ID:          "century_horse",
		Name:        "Century Horse",
		Description: "Complete 100 races",
		Icon:        "\U0001F4AF", // 💯
		Rarity:      "epic",
	},
	"comeback_kid": {
		ID:          "comeback_kid",
		Name:        "Comeback Kid",
		Description: "Win after being in last place at 75% mark",
		Icon:        "\U0001F504", // 🔄
		Rarity:      "epic",
	},
	"elder_statesman": {
		ID:          "elder_statesman",
		Name:        "Elder Statesman",
		Description: "Win a race at age 12+",
		Icon:        "\U0001F9D3", // 🧓
		Rarity:      "legendary",
	},

	// -----------------------------------------------------------------------
	// Stable Achievements
	// -----------------------------------------------------------------------
	"first_foal": {
		ID:          "first_foal",
		Name:        "First Foal",
		Description: "Breed your first horse",
		Icon:        "\U0001F9EC", // 🧬
		Rarity:      "common",
	},
	"breeder_10": {
		ID:          "breeder_10",
		Name:        "Prolific Breeder",
		Description: "Breed 10 horses",
		Icon:        "\U0001F40E", // 🐎
		Rarity:      "rare",
	},
	"millionaire": {
		ID:          "millionaire",
		Name:        "Cummy Millionaire",
		Description: "Accumulate 1,000,000 Cummies",
		Icon:        "\U0001F911", // 🤑
		Rarity:      "epic",
	},
	"dynasty": {
		ID:          "dynasty",
		Name:        "Dynasty",
		Description: "Have 3 generations of winners",
		Icon:        "\U0001F3DB\uFE0F", // 🏛️
		Rarity:      "legendary",
	},
	"tournament_winner": {
		ID:          "tournament_winner",
		Name:        "Tournament Champion",
		Description: "Win a tournament",
		Icon:        "\U0001F3C5", // 🏅
		Rarity:      "epic",
	},
	"full_stable": {
		ID:          "full_stable",
		Name:        "Full House",
		Description: "Own 20+ horses",
		Icon:        "\U0001F3E0", // 🏠
		Rarity:      "rare",
	},
	"legendary_bloodline": {
		ID:          "legendary_bloodline",
		Name:        "Legendary Bloodline",
		Description: "Breed a foal from two legendary parents",
		Icon:        "\u2728", // ✨
		Rarity:      "legendary",
	},

	// -----------------------------------------------------------------------
	// Track Mastery Achievements
	// -----------------------------------------------------------------------
	"frost_king": {
		ID:          "frost_king",
		Name:        "Ice Royalty",
		Description: "Win 5 Frostussy races. The frost bows to you. Dr. Mittens prescribes a warm blanket and a crown.",
		Icon:        "\u2744\uFE0F", // ❄️
		Rarity:      "rare",
	},
	"thunder_god": {
		ID:          "thunder_god",
		Name:        "Thunder God",
		Description: "Win 5 Thunderussy races. Zeus called — he wants his horse back. The storm is yours now.",
		Icon:        "\U0001F329\uFE0F", // 🌩️
		Rarity:      "rare",
	},
	"haunted_survivor": {
		ID:          "haunted_survivor",
		Name:        "Ghost Whisperer",
		Description: "Win 3 Hauntedussy races. B.U.R.P. would like a word. The spirits are filing a formal complaint.",
		Icon:        "\U0001F47B", // 👻
		Rarity:      "epic",
	},
	"sprint_master": {
		ID:          "sprint_master",
		Name:        "Flash",
		Description: "Win 10 Sprintussy races. You're not fast — you're a temporal anomaly. STARDUSTUSSY confirms this from 2089.",
		Icon:        "\u26A1", // ⚡
		Rarity:      "epic",
	},
	"grind_master": {
		ID:          "grind_master",
		Name:        "The Mountain",
		Description: "Win 10 Grindussy races. 3200 meters of pure suffering, ten times over. Pastor Router blesses your knees.",
		Icon:        "\U0001F3D4\uFE0F", // 🏔️
		Rarity:      "epic",
	},
	"all_tracks": {
		ID:          "all_tracks",
		Name:        "Track Omnivore",
		Description: "Win on all 6 track types. Sprintussy, Grindussy, Mudussy, Thunderussy, Frostussy, Hauntedussy — you devoured them all. Geoffrussy rates your pipeline: flawless.",
		Icon:        "\U0001F30D", // 🌍
		Rarity:      "legendary",
	},

	// -----------------------------------------------------------------------
	// Weather Achievements
	// -----------------------------------------------------------------------
	"storm_chaser": {
		ID:          "storm_chaser",
		Name:        "Storm Chaser",
		Description: "Win a race in Stormy weather. Lightning struck twice — once on the track, once on the podium. B.U.R.P. is monitoring the situation.",
		Icon:        "\U0001F4A8", // 💨
		Rarity:      "rare",
	},
	"fog_runner": {
		ID:          "fog_runner",
		Name:        "Blind Speed",
		Description: "Win in Foggy weather. Your horse can't see the finish line, but the finish line can see your horse. Sappho Scale rating: vibes-based navigation.",
		Icon:        "\U0001F32B\uFE0F", // 🌫️
		Rarity:      "rare",
	},
	"heat_stroke": {
		ID:          "heat_stroke",
		Name:        "Heatstroke Hero",
		Description: "Win in Scorching weather. 140°F on the track. Dr. Mittens advises hydration. Your horse advises victory.",
		Icon:        "\U0001F525", // 🔥
		Rarity:      "rare",
	},
	"weather_master": {
		ID:          "weather_master",
		Name:        "Weatherproof",
		Description: "Win in all 6 weather types. Clear, Rainy, Stormy, Foggy, Scorching, Haunted — nothing stops you. STARDUSTUSSY's 2089 forecast: perpetual dominance.",
		Icon:        "\U0001F308", // 🌈
		Rarity:      "legendary",
	},

	// -----------------------------------------------------------------------
	// Lore Achievements — the deep Ussyverse cuts
	// -----------------------------------------------------------------------
	"mittens_approved": {
		ID:          "mittens_approved",
		Name:        "Dr. Mittens Approves",
		Description: "Win 3+ races with a horse that has INT AA. Dr. Mittens (DVM, board-certified in equine cognition and slow-blinks) has reviewed the neural pathways and issued a rare nod of approval.",
		Icon:        "\U0001F431", // 🐱
		Rarity:      "epic",
	},
	"derulo_moment": {
		ID:          "derulo_moment",
		Name:        "Jason Derulo Moment",
		Description: "Horse panics 3+ times in a single race and still finishes. Jason Derulo (unwilling affiliate) knows this feeling. You stumble, you fall, you keep going. He is not endorsing this.",
		Icon:        "\U0001F635", // 😵
		Rarity:      "rare",
	},
	"burp_investigated": {
		ID:          "burp_investigated",
		Name:        "Under Investigation",
		Description: "Race on Hauntedussy with E-008's Chosen present. The Bureau of Ussyverse Rogue Phenomena has opened a case file. Agent status: concerned. Yogurt status: watching.",
		Icon:        "\U0001F50D", // 🔍
		Rarity:      "epic",
	},
	"pastor_blessing": {
		ID:          "pastor_blessing",
		Name:        "Blessed Connection",
		Description: "Win a race with 0 panic events. Pastor Router McEthernet III has blessed your connection. Packet loss: 0%. Spiritual latency: negligible. Amen.",
		Icon:        "\U0001F54A\uFE0F", // 🕊️
		Rarity:      "rare",
	},
	"geoffrussy_certified": {
		ID:          "geoffrussy_certified",
		Name:        "Geoffrussy Certified",
		Description: "Complete a race in under 100 ticks. Your goroutine completed before the garbage collector even woke up. Geoffrussy (the Go orchestrator) stamps your binary: OPTIMIZED.",
		Icon:        "\U0001F4BB", // 💻
		Rarity:      "epic",
	},
	"sappho_perfect": {
		ID:          "sappho_perfect",
		Name:        "Sappho 12.0",
		Description: "Win a race with a horse that has all AA genome. A perfect specimen. The Sappho Scale has been recalibrated. Lesbos weeps with joy. The poets write your name in the stars.",
		Icon:        "\U0001F31F", // 🌟
		Rarity:      "legendary",
	},
	"yogurt_whisperer": {
		ID:          "yogurt_whisperer",
		Name:        "Yogurt Whisperer",
		Description: "Breed a foal from E-008's Chosen (Lot 6). The sentient yogurt has deemed your bloodline worthy. Entity-008 pulses with approval. The foal smells faintly of probiotics.",
		Icon:        "\U0001F9C0", // 🧀
		Rarity:      "legendary",
	},
	"stardustussy_vision": {
		ID:          "stardustussy_vision",
		Name:        "2089 Vision",
		Description: "Win 3 races with a Lot 11 descendant. STARDUSTUSSY (the AI from 2089) confirms: this bloodline persists across timelines. The prophecy is not a metaphor. It never was.",
		Icon:        "\U0001F52E", // 🔮
		Rarity:      "epic",
	},

	// -----------------------------------------------------------------------
	// Breeding Achievements
	// -----------------------------------------------------------------------
	"genetic_disaster": {
		ID:          "genetic_disaster",
		Name:        "Genetic Disaster",
		Description: "Breed a foal with all BB genes. Every allele chose violence. Dr. Mittens has declined to comment. This horse is a monument to recessive chaos.",
		Icon:        "\U0001F9E8", // 🧨
		Rarity:      "rare",
	},
	"super_foal": {
		ID:          "super_foal",
		Name:        "Super Foal",
		Description: "Breed a foal with fitness ceiling > 0.95. The genetic lottery has spoken and your ticket won. Geoffrussy's pipeline is jealous of this optimization.",
		Icon:        "\U0001F4AA", // 💪
		Rarity:      "epic",
	},
	"inbreeding_moment": {
		ID:          "inbreeding_moment",
		Name:        "Family Reunion",
		Description: "Breed horses with a common grandparent. The family tree is more of a family wreath. Pastor Router McEthernet III is praying for your stable. Jason Derulo has left the chat.",
		Icon:        "\U0001F3A1", // 🎡
		Rarity:      "rare",
	},
	"generation_5": {
		ID:          "generation_5",
		Name:        "Fifth Generation",
		Description: "Have a generation 5+ horse. Five generations of Ussyverse horses. That's a dynasty the Habsburg jaw couldn't dream of. STARDUSTUSSY confirms: the bloodline echoes in 2089.",
		Icon:        "\U0001F332", // 🌲
		Rarity:      "epic",
	},
	"mutation_witnessed": {
		ID:          "mutation_witnessed",
		Name:        "Mutation Observed",
		Description: "Breed a foal that gets a mutation. Something changed in the genome. B.U.R.P. has been notified. E-008 stirs. The yogurt remembers. This was not in the original codebase.",
		Icon:        "\U0001F9EC", // 🧬
		Rarity:      "rare",
	},

	// -----------------------------------------------------------------------
	// Racing Records
	// -----------------------------------------------------------------------
	"total_wins_50": {
		ID:          "total_wins_50",
		Name:        "Hall of Fame",
		Description: "50 total wins across all horses. Your stable doesn't race — it conquers. The Ussyverse Hall of Fame has reserved a plaque. It's next to Jason Derulo's restraining order.",
		Icon:        "\U0001F3C6", // 🏆
		Rarity:      "epic",
	},
	"total_races_200": {
		ID:          "total_races_200",
		Name:        "Race Addict",
		Description: "200 total races. You can stop any time you want. You just don't want to. Dr. Mittens recommends an intervention. Geoffrussy recommends more goroutines.",
		Icon:        "\U0001F3C7", // 🏇
		Rarity:      "epic",
	},
	"elo_2000": {
		ID:          "elo_2000",
		Name:        "Grand Master",
		Description: "Any horse reaches 2000 ELO. Your horse has transcended mortal rankings. The Sappho Scale bends. STARDUSTUSSY cross-references 2089 records: this horse exists in the prophecy.",
		Icon:        "\U0001F9E0", // 🧠
		Rarity:      "legendary",
	},
	"elo_floor": {
		ID:          "elo_floor",
		Name:        "Rock Bottom",
		Description: "Any horse drops below 800 ELO. The only way from here is up. Or further down. Pastor Router McEthernet III offers a prayer. Connection status: unstable.",
		Icon:        "\U0001F4C9", // 📉
		Rarity:      "common",
	},
	"losing_streak_10": {
		ID:          "losing_streak_10",
		Name:        "The Cursed",
		Description: "10 losses in a row. This isn't bad luck — this is a haunting. B.U.R.P. has classified your stable as a paranormal event. E-008 is laughing. The yogurt is laughing.",
		Icon:        "\U0001F480", // 💀
		Rarity:      "rare",
	},

	// -----------------------------------------------------------------------
	// Economy Achievements
	// -----------------------------------------------------------------------
	"big_spender": {
		ID:          "big_spender",
		Name:        "Big Spender",
		Description: "Spend 100,000 cummies on stud fees. The cummies flow like water. Your accountant (Dr. Mittens) is concerned. The market doesn't care. The market is hungry.",
		Icon:        "\U0001F4B8", // 💸
		Rarity:      "rare",
	},
	"first_sale": {
		ID:          "first_sale",
		Name:        "Open for Business",
		Description: "List your first horse on the stud market. Your stallion's genes are now a publicly traded commodity. Pastor Router blesses the transaction. Packet sent.",
		Icon:        "\U0001F3EA", // 🏪
		Rarity:      "common",
	},
	"cummies_earned_100k": {
		ID:          "cummies_earned_100k",
		Name:        "Six Figures",
		Description: "Earn 100,000 total cummies from races. Six figures in cummies. That's approximately $0.00 in real money but priceless in the Ussyverse. Geoffrussy's pipeline approves this throughput.",
		Icon:        "\U0001F4B0", // 💰
		Rarity:      "rare",
	},
	"market_mogul": {
		ID:          "market_mogul",
		Name:        "Market Mogul",
		Description: "Complete 10 stud market transactions. You're not a horse breeder — you're a venture capitalist. The Ussyverse economy trembles. Jason Derulo is somehow losing money from this.",
		Icon:        "\U0001F4C8", // 📈
		Rarity:      "epic",
	},

	// -----------------------------------------------------------------------
	// Breeding Achievements (additional)
	// -----------------------------------------------------------------------
	"dynasty_builder": {
		ID:          "dynasty_builder",
		Name:        "Dynasty Builder",
		Description: "Have 4 generations of horses in a single bloodline. The family tree is a redwood. STARDUSTUSSY sees this dynasty persisting into 2089 and beyond.",
		Icon:        "\U0001F3F0", // 🏰
		Rarity:      "epic",
	},
	"genetic_lottery": {
		ID:          "genetic_lottery",
		Name:        "Genetic Lottery",
		Description: "Breed a foal with fitness ceiling > 0.90 from parents both below 0.70. The alleles aligned. Dr. Mittens calls it a miracle. Geoffrussy calls it a statistical anomaly.",
		Icon:        "\U0001F3B0", // 🎰
		Rarity:      "epic",
	},
	"champion_bloodline": {
		ID:          "champion_bloodline",
		Name:        "Champion Bloodline",
		Description: "Breed a foal from a retired champion (5+ win retiree). The legacy continues. Hall of Fame blood flows through this foal's veins.",
		Icon:        "\U0001F3C6", // 🏆
		Rarity:      "rare",
	},

	// -----------------------------------------------------------------------
	// Trading Achievements
	// -----------------------------------------------------------------------
	"first_trade": {
		ID:          "first_trade",
		Name:        "First Trade",
		Description: "Complete your first horse trade. The market opens its arms. Pastor Router blesses the handshake. Packet delivered.",
		Icon:        "\U0001F91D", // 🤝
		Rarity:      "common",
	},
	"cummies_burned": {
		ID:          "cummies_burned",
		Name:        "Cummies Burned",
		Description: "Spend 500,000 total cummies. The cummies are gone. All of them. Dr. Mittens is writing a concerned letter. The yogurt absorbs your financial distress.",
		Icon:        "\U0001F4B8", // 💸
		Rarity:      "epic",
	},

	// -----------------------------------------------------------------------
	// Social / Engagement Achievements
	// -----------------------------------------------------------------------
	"first_challenge": {
		ID:          "first_challenge",
		Name:        "Challenger Approaching",
		Description: "Issue or accept your first head-to-head challenge. The gauntlet is thrown. Jason Derulo felt the tremor from across dimensions.",
		Icon:        "\u2694\uFE0F", // ⚔️
		Rarity:      "common",
	},
	"betting_winner": {
		ID:          "betting_winner",
		Name:        "Lucky Bettor",
		Description: "Win a bet on a race. The odds were in your favor. The cummies flow. STARDUSTUSSY's 2089 ledger confirms: you're on the right timeline.",
		Icon:        "\U0001F911", // 🤑
		Rarity:      "common",
	},
	"streak_7": {
		ID:          "streak_7",
		Name:        "Weekly Warrior",
		Description: "Maintain a 7-day login streak. Seven days of dedication. Pastor Router McEthernet III notes your unwavering connection. Uptime: impressive.",
		Icon:        "\U0001F4C5", // 📅
		Rarity:      "rare",
	},
}

// ---------------------------------------------------------------------------
// Achievement Checking
// ---------------------------------------------------------------------------

// hasAchievement checks if an achievement ID is already present in a slice.
func hasAchievement(achievements []models.Achievement, id string) bool {
	for _, a := range achievements {
		if a.ID == id {
			return true
		}
	}
	return false
}

// unlockAchievement creates a copy of the achievement definition with an
// unlocked timestamp.
func unlockAchievement(id string) models.Achievement {
	a := AllAchievements[id]
	a.UnlockedAt = time.Now()
	return a
}

// CheckAchievements evaluates all achievement conditions against the given
// horse, their race history, and the owning stable. Returns any achievements
// that are newly unlocked (not already present on the horse or stable).
//
// The caller is responsible for persisting returned achievements to the
// horse/stable. This function is intentionally side-effect-free.
func CheckAchievements(horse *models.Horse, history *RaceHistory, stable *models.Stable) []models.Achievement {
	var unlocked []models.Achievement

	// Merge horse traits (if any) and stable achievements for "already have" checks.
	// We consider both stable.Achievements and what we've unlocked so far.
	existing := stable.Achievements

	// Get horse stats and history for condition evaluation.
	stats := history.GetHorseStats(horse.ID)
	horseResults := history.GetHorseHistory(horse.ID)

	// -----------------------------------------------------------------------
	// Horse Achievements
	// -----------------------------------------------------------------------

	// first_blood — Win your first race
	if !hasAchievement(existing, "first_blood") && stats.Wins >= 1 {
		unlocked = append(unlocked, unlockAchievement("first_blood"))
	}

	// triple_crown — Win on all 3 original track types (Sprintussy, Grindussy, Mudussy)
	if !hasAchievement(existing, "triple_crown") {
		trackWins := make(map[models.TrackType]bool)
		for _, r := range horseResults {
			if r.FinishPlace == 1 {
				trackWins[r.TrackType] = true
			}
		}
		if trackWins[models.TrackSprintussy] && trackWins[models.TrackGrindussy] && trackWins[models.TrackMudussy] {
			unlocked = append(unlocked, unlockAchievement("triple_crown"))
		}
	}

	// undefeated_5 — Win 5 races in a row
	if !hasAchievement(existing, "undefeated_5") && stats.BestStreak >= 5 {
		unlocked = append(unlocked, unlockAchievement("undefeated_5"))
	}

	// undefeated_10 — Win 10 races in a row
	if !hasAchievement(existing, "undefeated_10") && stats.BestStreak >= 10 {
		unlocked = append(unlocked, unlockAchievement("undefeated_10"))
	}

	// underdog — Win a race with the lowest ELO entry
	// We check the most recent race: if the horse won and had the lowest ELO
	// among all entries in that race.
	if !hasAchievement(existing, "underdog") && len(horseResults) > 0 {
		latest := horseResults[0]
		if latest.FinishPlace == 1 {
			raceResults := history.GetRaceResults(latest.RaceID)
			if len(raceResults) > 1 {
				isLowestELO := true
				for _, rr := range raceResults {
					if rr.HorseID != horse.ID && rr.ELOBefore < latest.ELOBefore {
						isLowestELO = false
						break
					}
				}
				if isLowestELO {
					unlocked = append(unlocked, unlockAchievement("underdog"))
				}
			}
		}
	}

	// mud_master — Win 5 Mudussy races
	if !hasAchievement(existing, "mud_master") {
		mudWins := 0
		for _, r := range horseResults {
			if r.TrackType == models.TrackMudussy && r.FinishPlace == 1 {
				mudWins++
			}
		}
		if mudWins >= 5 {
			unlocked = append(unlocked, unlockAchievement("mud_master"))
		}
	}

	// speed_demon — Win a Sprintussy in under 10 seconds
	if !hasAchievement(existing, "speed_demon") {
		for _, r := range horseResults {
			if r.TrackType == models.TrackSprintussy && r.FinishPlace == 1 && r.FinalTime > 0 && r.FinalTime < 10*time.Second {
				unlocked = append(unlocked, unlockAchievement("speed_demon"))
				break
			}
		}
	}

	// marathon_king — Win a Grindussy without panic
	// We check if there's a winning Grindussy result. Panic detection would
	// normally require tick log inspection, but from RaceResult we approximate:
	// a Grindussy win with a time that doesn't suggest any panic delays qualifies.
	// Since we can't see tick logs from RaceResult alone, we check the horse's
	// temper gene — AA temper horses can't panic, so any Grindussy win counts.
	if !hasAchievement(existing, "marathon_king") {
		tmpExpr := "BB"
		if gene, ok := horse.Genome[models.GeneTMP]; ok {
			tmpExpr = gene.Express()
		}
		// AA temper = 0% panic chance, so any Grindussy win is panic-free.
		if tmpExpr == "AA" {
			for _, r := range horseResults {
				if r.TrackType == models.TrackGrindussy && r.FinishPlace == 1 {
					unlocked = append(unlocked, unlockAchievement("marathon_king"))
					break
				}
			}
		}
	}

	// photo_finish — Win by less than 0.5m (checked from race results context).
	// We look at races where this horse won and the 2nd place horse's time
	// is within 50ms (approximation of 0.5m at typical speeds).
	if !hasAchievement(existing, "photo_finish") {
		for _, r := range horseResults {
			if r.FinishPlace == 1 {
				raceResults := history.GetRaceResults(r.RaceID)
				for _, rr := range raceResults {
					if rr.FinishPlace == 2 && rr.FinalTime > 0 && r.FinalTime > 0 {
						// If the time difference is under 50ms, that's about 0.5m
						// at ~10 m/s race speed. Close enough for government work.
						timeDiff := rr.FinalTime - r.FinalTime
						if timeDiff >= 0 && timeDiff < 50*time.Millisecond {
							unlocked = append(unlocked, unlockAchievement("photo_finish"))
							break
						}
					}
				}
				if hasAchievement(unlocked, "photo_finish") {
					break
				}
			}
		}
	}

	// the_yogurt_sees — Race against E-008's Chosen and survive
	// Check if any race the horse participated in also had an E-008 horse (LotNumber 6).
	// We can't determine LotNumber from RaceResult alone, so we check if the horse
	// has completed any race — and the caller should check this after E-008 races.
	// For a more robust check, we look at the race weather for Haunted or the
	// horse name patterns. Since we can't be perfect, we flag this if the horse
	// has raced on Hauntedussy and finished (survived).
	if !hasAchievement(existing, "the_yogurt_sees") {
		for _, r := range horseResults {
			if r.TrackType == models.TrackHauntedussy && r.FinishPlace > 0 {
				unlocked = append(unlocked, unlockAchievement("the_yogurt_sees"))
				break
			}
		}
	}

	// century_horse — Complete 100 races
	if !hasAchievement(existing, "century_horse") && stats.TotalRaces >= 100 {
		unlocked = append(unlocked, unlockAchievement("century_horse"))
	}

	// comeback_kid — Win after being in last place at 75% mark.
	// This requires tick-log analysis which RaceResult doesn't carry.
	// The race simulation engine should grant this directly when it detects
	// a horse was in last place at the 75% distance mark and went on to win.
	// NOTE: Cannot auto-detect from RaceResult alone — needs tick data.

	// elder_statesman — Win a race at age 12+
	if !hasAchievement(existing, "elder_statesman") && horse.Age >= 12 && stats.Wins >= 1 {
		// Verify there's a win while the horse was at current age (or older).
		// Since RaceResult doesn't store horse age at race time, we check
		// current age. This is a reasonable approximation — if the horse is
		// currently 12+ and has wins, at least one win happened at 12+.
		unlocked = append(unlocked, unlockAchievement("elder_statesman"))
	}

	// -----------------------------------------------------------------------
	// Track Mastery Achievements
	// -----------------------------------------------------------------------

	// frost_king — Win 5 Frostussy races
	if !hasAchievement(existing, "frost_king") {
		frostWins := 0
		for _, r := range horseResults {
			if r.TrackType == models.TrackFrostussy && r.FinishPlace == 1 {
				frostWins++
			}
		}
		if frostWins >= 5 {
			unlocked = append(unlocked, unlockAchievement("frost_king"))
		}
	}

	// thunder_god — Win 5 Thunderussy races
	if !hasAchievement(existing, "thunder_god") {
		thunderWins := 0
		for _, r := range horseResults {
			if r.TrackType == models.TrackThunderussy && r.FinishPlace == 1 {
				thunderWins++
			}
		}
		if thunderWins >= 5 {
			unlocked = append(unlocked, unlockAchievement("thunder_god"))
		}
	}

	// haunted_survivor — Win 3 Hauntedussy races
	if !hasAchievement(existing, "haunted_survivor") {
		hauntedWins := 0
		for _, r := range horseResults {
			if r.TrackType == models.TrackHauntedussy && r.FinishPlace == 1 {
				hauntedWins++
			}
		}
		if hauntedWins >= 3 {
			unlocked = append(unlocked, unlockAchievement("haunted_survivor"))
		}
	}

	// sprint_master — Win 10 Sprintussy races
	if !hasAchievement(existing, "sprint_master") {
		sprintWins := 0
		for _, r := range horseResults {
			if r.TrackType == models.TrackSprintussy && r.FinishPlace == 1 {
				sprintWins++
			}
		}
		if sprintWins >= 10 {
			unlocked = append(unlocked, unlockAchievement("sprint_master"))
		}
	}

	// grind_master — Win 10 Grindussy races
	if !hasAchievement(existing, "grind_master") {
		grindWins := 0
		for _, r := range horseResults {
			if r.TrackType == models.TrackGrindussy && r.FinishPlace == 1 {
				grindWins++
			}
		}
		if grindWins >= 10 {
			unlocked = append(unlocked, unlockAchievement("grind_master"))
		}
	}

	// all_tracks — Win on all 6 track types
	if !hasAchievement(existing, "all_tracks") {
		trackWinsAll := make(map[models.TrackType]bool)
		for _, r := range horseResults {
			if r.FinishPlace == 1 {
				trackWinsAll[r.TrackType] = true
			}
		}
		if trackWinsAll[models.TrackSprintussy] && trackWinsAll[models.TrackGrindussy] &&
			trackWinsAll[models.TrackMudussy] && trackWinsAll[models.TrackThunderussy] &&
			trackWinsAll[models.TrackFrostussy] && trackWinsAll[models.TrackHauntedussy] {
			unlocked = append(unlocked, unlockAchievement("all_tracks"))
		}
	}

	// -----------------------------------------------------------------------
	// Weather Achievements
	// -----------------------------------------------------------------------

	// storm_chaser — Win a race in Stormy weather
	if !hasAchievement(existing, "storm_chaser") {
		for _, r := range horseResults {
			if r.FinishPlace == 1 && r.Weather == string(models.WeatherStormy) {
				unlocked = append(unlocked, unlockAchievement("storm_chaser"))
				break
			}
		}
	}

	// fog_runner — Win in Foggy weather
	if !hasAchievement(existing, "fog_runner") {
		for _, r := range horseResults {
			if r.FinishPlace == 1 && r.Weather == string(models.WeatherFoggy) {
				unlocked = append(unlocked, unlockAchievement("fog_runner"))
				break
			}
		}
	}

	// heat_stroke — Win in Scorching weather
	if !hasAchievement(existing, "heat_stroke") {
		for _, r := range horseResults {
			if r.FinishPlace == 1 && r.Weather == string(models.WeatherScorching) {
				unlocked = append(unlocked, unlockAchievement("heat_stroke"))
				break
			}
		}
	}

	// weather_master — Win in all 6 weather types
	if !hasAchievement(existing, "weather_master") {
		weatherWins := make(map[string]bool)
		for _, r := range horseResults {
			if r.FinishPlace == 1 {
				weatherWins[r.Weather] = true
			}
		}
		if weatherWins[string(models.WeatherClear)] && weatherWins[string(models.WeatherRainy)] &&
			weatherWins[string(models.WeatherStormy)] && weatherWins[string(models.WeatherFoggy)] &&
			weatherWins[string(models.WeatherScorching)] && weatherWins[string(models.WeatherHaunted)] {
			unlocked = append(unlocked, unlockAchievement("weather_master"))
		}
	}

	// -----------------------------------------------------------------------
	// Lore Achievements
	// -----------------------------------------------------------------------

	// mittens_approved — Win 3+ races with a horse that has INT AA
	if !hasAchievement(existing, "mittens_approved") && stats.Wins >= 3 {
		if gene, ok := horse.Genome[models.GeneINT]; ok && gene.Express() == "AA" {
			unlocked = append(unlocked, unlockAchievement("mittens_approved"))
		}
	}

	// derulo_moment — Horse panics 3+ times in a single race and still finishes.
	// Approximation: horse has TMP BB (highly volatile / panic-prone) and has
	// finished at least one race. TMP BB horses have maximum panic chance, so
	// surviving a race with multiple panics is expected for them.
	if !hasAchievement(existing, "derulo_moment") && len(horseResults) > 0 {
		if gene, ok := horse.Genome[models.GeneTMP]; ok && gene.Express() == "BB" {
			unlocked = append(unlocked, unlockAchievement("derulo_moment"))
		}
	}

	// burp_investigated — Race on Hauntedussy with E-008's Chosen present.
	// Approximation: check if horse has raced on Hauntedussy and any other
	// entry in that race has "E-008" or "Yogurt" in their name.
	if !hasAchievement(existing, "burp_investigated") {
		for _, r := range horseResults {
			if r.TrackType == models.TrackHauntedussy {
				raceResults := history.GetRaceResults(r.RaceID)
				for _, rr := range raceResults {
					if rr.HorseID != horse.ID {
						nameLower := strings.ToLower(rr.HorseName)
						if strings.Contains(nameLower, "e-008") || strings.Contains(nameLower, "yogurt") {
							unlocked = append(unlocked, unlockAchievement("burp_investigated"))
							break
						}
					}
				}
				if hasAchievement(unlocked, "burp_investigated") {
					break
				}
			}
		}
	}

	// pastor_blessing — Win a race with 0 panic events.
	// Approximation: TMP AA (calm temperament = 0% panic) and won a race.
	if !hasAchievement(existing, "pastor_blessing") && stats.Wins >= 1 {
		if gene, ok := horse.Genome[models.GeneTMP]; ok && gene.Express() == "AA" {
			unlocked = append(unlocked, unlockAchievement("pastor_blessing"))
		}
	}

	// geoffrussy_certified — Complete a race in under 100 ticks.
	// Approximation: fast race time relative to distance. We use a heuristic
	// of FinalTime < (distance_meters * 3ms) as a proxy for sub-100-tick
	// completion, since each tick represents ~distance/100 progress at speed.
	// For Sprintussy (800m): < 2.4s, Hauntedussy (666m): < 2.0s, etc.
	if !hasAchievement(existing, "geoffrussy_certified") {
		for _, r := range horseResults {
			if r.FinalTime > 0 && r.Distance > 0 {
				// Threshold: approximately 3ms per meter of distance.
				threshold := time.Duration(r.Distance) * 3 * time.Millisecond
				if r.FinalTime < threshold {
					unlocked = append(unlocked, unlockAchievement("geoffrussy_certified"))
					break
				}
			}
		}
	}

	// sappho_perfect — Win a race with a horse that has all AA genome
	if !hasAchievement(existing, "sappho_perfect") && stats.Wins >= 1 {
		allAA := true
		for _, geneType := range models.AllGeneTypes {
			if gene, ok := horse.Genome[geneType]; !ok || gene.Express() != "AA" {
				allAA = false
				break
			}
		}
		if allAA {
			unlocked = append(unlocked, unlockAchievement("sappho_perfect"))
		}
	}

	// yogurt_whisperer — Breed a foal from E-008's Chosen (Lot 6).
	// Check stable for any horse whose SireID or MareID matches a LotNumber 6 horse.
	if !hasAchievement(existing, "yogurt_whisperer") && stable != nil {
		// Build a set of IDs for Lot 6 horses.
		lot6IDs := make(map[string]bool)
		for _, h := range stable.Horses {
			if h.LotNumber == 6 {
				lot6IDs[h.ID] = true
			}
		}
		if len(lot6IDs) > 0 {
			for _, h := range stable.Horses {
				if h.Generation > 0 && (lot6IDs[h.SireID] || lot6IDs[h.MareID]) {
					unlocked = append(unlocked, unlockAchievement("yogurt_whisperer"))
					break
				}
			}
		}
	}

	// stardustussy_vision — Win 3 races with a Lot 11 descendant.
	// Check if the current horse has LotNumber 11 or descends from a Lot 11
	// horse (SireID or MareID matches a Lot 11 horse in the stable), and has 3+ wins.
	if !hasAchievement(existing, "stardustussy_vision") && stats.Wins >= 3 && stable != nil {
		isLot11Descendant := horse.LotNumber == 11
		if !isLot11Descendant {
			for _, h := range stable.Horses {
				if h.LotNumber == 11 && (h.ID == horse.SireID || h.ID == horse.MareID) {
					isLot11Descendant = true
					break
				}
			}
		}
		if isLot11Descendant {
			unlocked = append(unlocked, unlockAchievement("stardustussy_vision"))
		}
	}

	// -----------------------------------------------------------------------
	// Stable Achievements
	// -----------------------------------------------------------------------

	if stable != nil {
		// first_foal — Breed your first horse (stable has any horses with generation > 0)
		if !hasAchievement(existing, "first_foal") {
			for _, h := range stable.Horses {
				if h.Generation > 0 {
					unlocked = append(unlocked, unlockAchievement("first_foal"))
					break
				}
			}
		}

		// breeder_10 — Breed 10 horses
		if !hasAchievement(existing, "breeder_10") {
			bred := 0
			for _, h := range stable.Horses {
				if h.Generation > 0 {
					bred++
				}
			}
			if bred >= 10 {
				unlocked = append(unlocked, unlockAchievement("breeder_10"))
			}
		}

		// millionaire — Accumulate 1,000,000 Cummies (current + spent = total earnings)
		if !hasAchievement(existing, "millionaire") && stable.TotalEarnings >= 1_000_000 {
			unlocked = append(unlocked, unlockAchievement("millionaire"))
		}

		// dynasty — Have 3 generations of winners
		if !hasAchievement(existing, "dynasty") {
			generationsWithWins := make(map[int]bool)
			for _, h := range stable.Horses {
				if h.Wins > 0 {
					generationsWithWins[h.Generation] = true
				}
			}
			// Check if there are 3 consecutive or any 3 distinct generations with wins.
			if len(generationsWithWins) >= 3 {
				unlocked = append(unlocked, unlockAchievement("dynasty"))
			}
		}

		// tournament_winner — Win a tournament.
		// This achievement should be granted directly by the tournament system
		// (TournamentManager) when a tournament finishes and the winner is
		// determined. Cannot be auto-detected from RaceResult/HorseStats alone
		// because tournament standings are computed externally.
		// NOTE: Grant via direct call to unlockAchievement("tournament_winner")
		// in the tournament completion handler.

		// full_stable — Own 20+ horses
		if !hasAchievement(existing, "full_stable") && len(stable.Horses) >= 20 {
			unlocked = append(unlocked, unlockAchievement("full_stable"))
		}

		// legendary_bloodline — Breed a foal from two legendary parents
		if !hasAchievement(existing, "legendary_bloodline") {
			// Build a set of legendary horse IDs in the stable.
			legendaryIDs := make(map[string]bool)
			for _, h := range stable.Horses {
				if h.IsLegendary {
					legendaryIDs[h.ID] = true
				}
			}
			// Check if any horse has both parents as legendary.
			for _, h := range stable.Horses {
				if h.SireID != "" && h.MareID != "" &&
					legendaryIDs[h.SireID] && legendaryIDs[h.MareID] {
					unlocked = append(unlocked, unlockAchievement("legendary_bloodline"))
					break
				}
			}
		}

		// -------------------------------------------------------------------
		// Breeding Achievements
		// -------------------------------------------------------------------

		// genetic_disaster — Breed a foal with all BB genes
		if !hasAchievement(existing, "genetic_disaster") {
			for _, h := range stable.Horses {
				if h.Generation > 0 {
					allBB := true
					for _, geneType := range models.AllGeneTypes {
						if gene, ok := h.Genome[geneType]; !ok || gene.Express() != "BB" {
							allBB = false
							break
						}
					}
					if allBB {
						unlocked = append(unlocked, unlockAchievement("genetic_disaster"))
						break
					}
				}
			}
		}

		// super_foal — Breed a foal with fitness ceiling > 0.95
		if !hasAchievement(existing, "super_foal") {
			for _, h := range stable.Horses {
				if h.Generation > 0 && h.FitnessCeiling > 0.95 {
					unlocked = append(unlocked, unlockAchievement("super_foal"))
					break
				}
			}
		}

		// inbreeding_moment — Breed horses with a common grandparent.
		// Check if any horse in the stable has a sire and mare that share a
		// parent (i.e., the foal's paternal and maternal grandparents overlap).
		if !hasAchievement(existing, "inbreeding_moment") {
			// Build parent lookup: horseID -> (sireID, mareID)
			parentMap := make(map[string][2]string) // horseID -> [sireID, mareID]
			for _, h := range stable.Horses {
				if h.SireID != "" || h.MareID != "" {
					parentMap[h.ID] = [2]string{h.SireID, h.MareID}
				}
			}
			for _, h := range stable.Horses {
				if h.Generation > 0 && h.SireID != "" && h.MareID != "" {
					sireParents := parentMap[h.SireID]
					mareParents := parentMap[h.MareID]
					// Collect all grandparent IDs from the sire side.
					sireGrandparents := make(map[string]bool)
					if sireParents[0] != "" {
						sireGrandparents[sireParents[0]] = true
					}
					if sireParents[1] != "" {
						sireGrandparents[sireParents[1]] = true
					}
					// Check if any mare-side grandparent overlaps.
					if len(sireGrandparents) > 0 {
						if (mareParents[0] != "" && sireGrandparents[mareParents[0]]) ||
							(mareParents[1] != "" && sireGrandparents[mareParents[1]]) {
							unlocked = append(unlocked, unlockAchievement("inbreeding_moment"))
							break
						}
					}
				}
			}
		}

		// generation_5 — Have a generation 5+ horse
		if !hasAchievement(existing, "generation_5") {
			for _, h := range stable.Horses {
				if h.Generation >= 5 {
					unlocked = append(unlocked, unlockAchievement("generation_5"))
					break
				}
			}
		}

		// mutation_witnessed — Breed a foal that gets a mutation.
		// Check for any bred horse (Generation > 0) with MUT gene expressing AA.
		if !hasAchievement(existing, "mutation_witnessed") {
			for _, h := range stable.Horses {
				if h.Generation > 0 {
					if gene, ok := h.Genome[models.GeneMUT]; ok && gene.Express() == "AA" {
						unlocked = append(unlocked, unlockAchievement("mutation_witnessed"))
						break
					}
				}
			}
		}

		// -------------------------------------------------------------------
		// Racing Records
		// -------------------------------------------------------------------

		// total_wins_50 — 50 total wins across all horses in the stable
		if !hasAchievement(existing, "total_wins_50") {
			totalStableWins := 0
			for _, h := range stable.Horses {
				totalStableWins += h.Wins
			}
			if totalStableWins >= 50 {
				unlocked = append(unlocked, unlockAchievement("total_wins_50"))
			}
		}

		// total_races_200 — 200 total races across the stable
		if !hasAchievement(existing, "total_races_200") && stable.TotalRaces >= 200 {
			unlocked = append(unlocked, unlockAchievement("total_races_200"))
		}

		// elo_2000 — Any horse reaches 2000 ELO
		if !hasAchievement(existing, "elo_2000") {
			for _, h := range stable.Horses {
				if h.ELO >= 2000 || h.PeakELO >= 2000 {
					unlocked = append(unlocked, unlockAchievement("elo_2000"))
					break
				}
			}
		}

		// elo_floor — Any horse drops below 800 ELO
		if !hasAchievement(existing, "elo_floor") {
			for _, h := range stable.Horses {
				if h.ELO < 800 {
					unlocked = append(unlocked, unlockAchievement("elo_floor"))
					break
				}
			}
		}

		// losing_streak_10 — 10 losses in a row (any horse in the stable)
		if !hasAchievement(existing, "losing_streak_10") {
			for _, h := range stable.Horses {
				hStats := history.GetHorseStats(h.ID)
				if hStats.CurrentStreak <= -10 {
					unlocked = append(unlocked, unlockAchievement("losing_streak_10"))
					break
				}
			}
		}

		// -------------------------------------------------------------------
		// Economy Achievements
		// -------------------------------------------------------------------

		// big_spender — Spend 100,000 cummies on stud fees.
		// Approximation: TotalEarnings - current Cummies > 100,000 implies
		// at least that much was spent. This is imperfect (earnings can be
		// spent on other things) but is the best heuristic without dedicated
		// spending tracking.
		if !hasAchievement(existing, "big_spender") {
			spent := stable.TotalEarnings - stable.Cummies
			if spent >= 100_000 {
				unlocked = append(unlocked, unlockAchievement("big_spender"))
			}
		}

		// first_sale — List your first horse on the stud market.
		// This achievement cannot be detected from horse/stable state alone.
		// It should be granted directly by the server code that handles stud
		// market listing creation (e.g., in marketussy or the API handler).
		// NOTE: Grant via direct call to unlockAchievement("first_sale") when
		// a player creates their first StudListing.

		// cummies_earned_100k — Earn 100,000 total cummies from races
		if !hasAchievement(existing, "cummies_earned_100k") && stable.TotalEarnings >= 100_000 {
			unlocked = append(unlocked, unlockAchievement("cummies_earned_100k"))
		}

		// market_mogul — Complete 10 stud market transactions.
		// This achievement cannot be detected from horse/stable state alone.
		// It should be granted directly by the server code that handles stud
		// market transaction completion (e.g., in marketussy or the API handler).
		// NOTE: Grant via direct call to unlockAchievement("market_mogul") when
		// a player completes their 10th MarketTransaction.

		// -------------------------------------------------------------------
		// Additional Breeding Achievements
		// -------------------------------------------------------------------

		// dynasty_builder — Have 4 generations of horses in a single bloodline.
		if !hasAchievement(existing, "dynasty_builder") {
			maxGen := 0
			for _, h := range stable.Horses {
				if h.Generation > maxGen {
					maxGen = h.Generation
				}
			}
			if maxGen >= 4 {
				unlocked = append(unlocked, unlockAchievement("dynasty_builder"))
			}
		}

		// genetic_lottery — Breed a foal with fitness ceiling > 0.90 from
		// parents both below 0.70.
		// We approximate: check for any bred horse (gen > 0) with ceiling > 0.90
		// whose parents (if found in stable) both have ceiling < 0.70.
		if !hasAchievement(existing, "genetic_lottery") {
			horseByID := make(map[string]*models.Horse, len(stable.Horses))
			for i := range stable.Horses {
				horseByID[stable.Horses[i].ID] = &stable.Horses[i]
			}
			for _, h := range stable.Horses {
				if h.Generation > 0 && h.FitnessCeiling > 0.90 {
					sireH := horseByID[h.SireID]
					mareH := horseByID[h.MareID]
					if sireH != nil && mareH != nil &&
						sireH.FitnessCeiling < 0.70 && mareH.FitnessCeiling < 0.70 {
						unlocked = append(unlocked, unlockAchievement("genetic_lottery"))
						break
					}
				}
			}
		}

		// champion_bloodline — Breed a foal from a retired champion.
		if !hasAchievement(existing, "champion_bloodline") {
			championIDs := make(map[string]bool)
			for _, h := range stable.Horses {
				if h.RetiredChampion {
					championIDs[h.ID] = true
				}
			}
			if len(championIDs) > 0 {
				for _, h := range stable.Horses {
					if h.Generation > 0 && (championIDs[h.SireID] || championIDs[h.MareID]) {
						unlocked = append(unlocked, unlockAchievement("champion_bloodline"))
						break
					}
				}
			}
		}

		// cummies_burned — Spend 500,000 total cummies.
		if !hasAchievement(existing, "cummies_burned") {
			spent := stable.TotalEarnings - stable.Cummies
			if spent >= 500_000 {
				unlocked = append(unlocked, unlockAchievement("cummies_burned"))
			}
		}

		// streak_7 — 7-day login streak.
		// This achievement cannot be detected from horse/stable state alone.
		// It should be granted directly by the server code that tracks login
		// streaks (engagement system).
		// NOTE: Grant via direct call to unlockAchievement("streak_7") in
		// the daily reward handler when streak >= 7.

		// first_trade — Complete your first horse trade.
		// This achievement cannot be detected from horse/stable state alone.
		// It should be granted directly by the trade acceptance handler.
		// NOTE: Grant via direct call in handleAcceptTrade.

		// first_challenge — Issue or accept your first challenge.
		// This achievement cannot be detected from horse/stable state alone.
		// It should be granted directly by the challenge handlers.
		// NOTE: Grant via direct call in handleCreateChallenge / handleAcceptChallenge.

		// betting_winner — Win a bet on a race.
		// This achievement cannot be detected from horse/stable state alone.
		// It should be granted directly by the bet resolution code.
		// NOTE: Grant via direct call in resolveBets.
	}

	return unlocked
}
