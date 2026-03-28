// Package tournussy implements the tournament system, race history tracking,
// weather effects, and achievement definitions for StallionUSSY.
// It ties together the racing engine with persistent results, multi-round
// competitive events, and atmospheric chaos.
package tournussy

import (
	"fmt"
	"math/rand/v2"
	"sort"
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
	"Golden", "Midnight", "Sapphic", "Haunted", "Thunderous",
	"Forbidden", "Eternal", "Cosmic", "Cryogenic", "Volatile",
	"Iridescent", "Anomalous", "Sovereign", "Unholy", "Quantum",
	"Legendary", "Eldritch", "Magnificent", "Disastrous", "Suspicious",
}

var tournamentNouns = []string{
	"Stallion", "Crown", "Legacy", "Chalice", "Yogurt",
	"Trophy", "Flannel", "Gauntlet", "Reckoning", "Convergence",
	"Meridian", "Tempest", "Horizon", "Catalyst", "Biscuit",
	"Uprising", "Phantom", "Prophecy", "Communion", "Pipeline",
}

var tournamentPlaces = []string{
	"Lesbos", "Delaware", "Building 7", "the Ussyverse", "Sappho Valley",
	"the Forbidden Stable", "the Quantum Paddock", "Goroutine Gulch",
	"Haunted Meadows", "the Midnight Corral", "New Flannel City",
	"the Yogurt Wastes", "E-008's Domain", "the Anomaly Zone",
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
	// This requires tick log analysis which RaceResult doesn't carry.
	// The caller should check this separately using race tick logs and call
	// unlockAchievement directly. We leave the definition in AllAchievements
	// but can't auto-detect it here without tick data.
	// Placeholder: skip automated detection.

	// elder_statesman — Win a race at age 12+
	if !hasAchievement(existing, "elder_statesman") && horse.Age >= 12 && stats.Wins >= 1 {
		// Verify there's a win while the horse was at current age (or older).
		// Since RaceResult doesn't store horse age at race time, we check
		// current age. This is a reasonable approximation — if the horse is
		// currently 12+ and has wins, at least one win happened at 12+.
		unlocked = append(unlocked, unlockAchievement("elder_statesman"))
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

		// tournament_winner — Win a tournament (checked externally, but we can
		// see if any horse in the stable has a tournament win — for now this
		// is best checked by the caller after tournament completion).
		// Placeholder: we check if any horse has enough wins/earnings to suggest
		// tournament victory. Callers should explicitly grant this.

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
	}

	return unlocked
}
