package tournussy

import (
	"testing"
	"time"

	"github.com/mojomast/stallionussy/internal/genussy"
	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/racussy"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makeTestHorse creates a test horse with a random genome and given ELO.
func makeTestHorse(id, name string, elo float64) *models.Horse {
	return &models.Horse{
		ID:             id,
		Name:           name,
		Genome:         genussy.RandomGenome(),
		ELO:            elo,
		Age:            4,
		FitnessCeiling: 0.9,
		CurrentFitness: 0.7,
		Fatigue:        0,
	}
}

// makeTestHorses creates N test horses for racing.
func makeTestHorses(n int) []*models.Horse {
	horses := make([]*models.Horse, n)
	for i := 0; i < n; i++ {
		horses[i] = makeTestHorse(
			"horse-"+string(rune('a'+i)),
			"Horse "+string(rune('A'+i)),
			1200+float64(i*50),
		)
	}
	return horses
}

// makeRaceResult creates a RaceResult for testing.
func makeRaceResult(raceID, horseID, horseName string, place int, trackType models.TrackType, weather string) *models.RaceResult {
	return &models.RaceResult{
		RaceID:      raceID,
		HorseID:     horseID,
		HorseName:   horseName,
		TrackType:   trackType,
		Distance:    models.TrackDistance(trackType),
		FinishPlace: place,
		TotalHorses: 6,
		FinalTime:   time.Duration(place) * 5 * time.Second,
		ELOBefore:   1200,
		ELOAfter:    1200 + float64(6-place)*10,
		Earnings:    int64((6 - place) * 1000),
		Weather:     weather,
		CreatedAt:   time.Now(),
	}
}

// ===========================================================================
// RaceHistory
// ===========================================================================

func TestNewRaceHistory(t *testing.T) {
	rh := NewRaceHistory()
	if rh == nil {
		t.Fatal("NewRaceHistory returned nil")
	}
}

func TestRaceHistory_RecordResult(t *testing.T) {
	rh := NewRaceHistory()
	result := makeRaceResult("race-1", "h1", "Horse1", 1, models.TrackSprintussy, "Clear")
	rh.RecordResult(result)

	results := rh.GetHorseHistory("h1")
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestRaceHistory_GetHorseHistory_Empty(t *testing.T) {
	rh := NewRaceHistory()
	results := rh.GetHorseHistory("nonexistent")
	if results != nil {
		t.Errorf("expected nil for unknown horse, got %v", results)
	}
}

func TestRaceHistory_GetHorseHistory_Ordering(t *testing.T) {
	rh := NewRaceHistory()

	r1 := makeRaceResult("race-1", "h1", "Horse1", 1, models.TrackSprintussy, "Clear")
	r2 := makeRaceResult("race-2", "h1", "Horse1", 3, models.TrackGrindussy, "Rainy")
	r3 := makeRaceResult("race-3", "h1", "Horse1", 2, models.TrackMudussy, "Stormy")

	rh.RecordResult(r1)
	rh.RecordResult(r2)
	rh.RecordResult(r3)

	results := rh.GetHorseHistory("h1")
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Most recent first
	if results[0].RaceID != "race-3" {
		t.Errorf("expected race-3 first (most recent), got %s", results[0].RaceID)
	}
}

func TestRaceHistory_GetRaceResults(t *testing.T) {
	rh := NewRaceHistory()

	r1 := makeRaceResult("race-1", "h1", "Horse1", 1, models.TrackSprintussy, "Clear")
	r2 := makeRaceResult("race-1", "h2", "Horse2", 2, models.TrackSprintussy, "Clear")
	r3 := makeRaceResult("race-2", "h1", "Horse1", 3, models.TrackGrindussy, "Rainy")

	rh.RecordResult(r1)
	rh.RecordResult(r2)
	rh.RecordResult(r3)

	results := rh.GetRaceResults("race-1")
	if len(results) != 2 {
		t.Errorf("expected 2 results for race-1, got %d", len(results))
	}

	// race-2 should have 1 result
	results2 := rh.GetRaceResults("race-2")
	if len(results2) != 1 {
		t.Errorf("expected 1 result for race-2, got %d", len(results2))
	}
}

func TestRaceHistory_GetRaceResults_NotFound(t *testing.T) {
	rh := NewRaceHistory()
	results := rh.GetRaceResults("nonexistent")
	if results != nil {
		t.Errorf("expected nil for unknown race, got %v", results)
	}
}

func TestRaceHistory_GetRecentResults(t *testing.T) {
	rh := NewRaceHistory()

	for i := 0; i < 10; i++ {
		r := makeRaceResult("race-"+string(rune('0'+i)), "h1", "Horse1", i%3+1, models.TrackSprintussy, "Clear")
		rh.RecordResult(r)
	}

	recent := rh.GetRecentResults(5)
	if len(recent) != 5 {
		t.Errorf("expected 5 recent results, got %d", len(recent))
	}

	// Requesting more than available
	all := rh.GetRecentResults(100)
	if len(all) != 10 {
		t.Errorf("expected 10 results, got %d", len(all))
	}
}

func TestRaceHistory_GetRecentResults_Zero(t *testing.T) {
	rh := NewRaceHistory()
	results := rh.GetRecentResults(0)
	if results != nil {
		t.Errorf("expected nil for limit 0, got %v", results)
	}
}

func TestRaceHistory_GetRecentResults_Negative(t *testing.T) {
	rh := NewRaceHistory()
	results := rh.GetRecentResults(-1)
	if results != nil {
		t.Errorf("expected nil for negative limit, got %v", results)
	}
}

func TestRaceHistory_ReturnsCopy(t *testing.T) {
	rh := NewRaceHistory()
	r := makeRaceResult("race-1", "h1", "Horse1", 1, models.TrackSprintussy, "Clear")
	rh.RecordResult(r)

	h1 := rh.GetHorseHistory("h1")
	h2 := rh.GetHorseHistory("h1")

	if &h1[0] == &h2[0] {
		t.Error("GetHorseHistory should return independent copies")
	}
}

// ---------------------------------------------------------------------------
// HorseStats
// ---------------------------------------------------------------------------

func TestGetHorseStats_Empty(t *testing.T) {
	rh := NewRaceHistory()
	stats := rh.GetHorseStats("nobody")
	if stats.TotalRaces != 0 {
		t.Errorf("expected 0 races, got %d", stats.TotalRaces)
	}
	if stats.Wins != 0 {
		t.Errorf("expected 0 wins, got %d", stats.Wins)
	}
}

func TestGetHorseStats_BasicCounts(t *testing.T) {
	rh := NewRaceHistory()

	// 3 races: 2 wins, 1 second place
	rh.RecordResult(makeRaceResult("r1", "h1", "Horse1", 1, models.TrackSprintussy, "Clear"))
	rh.RecordResult(makeRaceResult("r2", "h1", "Horse1", 2, models.TrackGrindussy, "Clear"))
	rh.RecordResult(makeRaceResult("r3", "h1", "Horse1", 1, models.TrackMudussy, "Clear"))

	stats := rh.GetHorseStats("h1")
	if stats.TotalRaces != 3 {
		t.Errorf("TotalRaces = %d, want 3", stats.TotalRaces)
	}
	if stats.Wins != 2 {
		t.Errorf("Wins = %d, want 2", stats.Wins)
	}
	if stats.Places != 3 {
		t.Errorf("Places = %d, want 3 (all top 3)", stats.Places)
	}
}

func TestGetHorseStats_WinRate(t *testing.T) {
	rh := NewRaceHistory()

	rh.RecordResult(makeRaceResult("r1", "h1", "Horse1", 1, models.TrackSprintussy, "Clear"))
	rh.RecordResult(makeRaceResult("r2", "h1", "Horse1", 4, models.TrackGrindussy, "Clear"))

	stats := rh.GetHorseStats("h1")
	if stats.WinRate != 0.5 {
		t.Errorf("WinRate = %v, want 0.5", stats.WinRate)
	}
}

func TestGetHorseStats_Streak(t *testing.T) {
	rh := NewRaceHistory()

	// Record 5 wins in a row (oldest first)
	for i := 0; i < 5; i++ {
		rh.RecordResult(makeRaceResult("r"+string(rune('a'+i)), "h1", "Horse1", 1, models.TrackSprintussy, "Clear"))
	}

	stats := rh.GetHorseStats("h1")
	if stats.BestStreak != 5 {
		t.Errorf("BestStreak = %d, want 5", stats.BestStreak)
	}
	if stats.CurrentStreak != 5 {
		t.Errorf("CurrentStreak = %d, want 5", stats.CurrentStreak)
	}
}

func TestGetHorseStats_LoseStreak(t *testing.T) {
	rh := NewRaceHistory()

	// Record 3 losses in a row
	for i := 0; i < 3; i++ {
		rh.RecordResult(makeRaceResult("r"+string(rune('a'+i)), "h1", "Horse1", 4, models.TrackSprintussy, "Clear"))
	}

	stats := rh.GetHorseStats("h1")
	if stats.CurrentStreak != -3 {
		t.Errorf("CurrentStreak = %d, want -3", stats.CurrentStreak)
	}
}

func TestGetHorseStats_Earnings(t *testing.T) {
	rh := NewRaceHistory()

	r1 := makeRaceResult("r1", "h1", "Horse1", 1, models.TrackSprintussy, "Clear")
	r1.Earnings = 5000
	r2 := makeRaceResult("r2", "h1", "Horse1", 2, models.TrackGrindussy, "Clear")
	r2.Earnings = 3000

	rh.RecordResult(r1)
	rh.RecordResult(r2)

	stats := rh.GetHorseStats("h1")
	if stats.TotalEarnings != 8000 {
		t.Errorf("TotalEarnings = %d, want 8000", stats.TotalEarnings)
	}
}

func TestGetHorseStats_BestAndWorstTime(t *testing.T) {
	rh := NewRaceHistory()

	r1 := makeRaceResult("r1", "h1", "Horse1", 1, models.TrackSprintussy, "Clear")
	r1.FinalTime = 8 * time.Second
	r2 := makeRaceResult("r2", "h1", "Horse1", 2, models.TrackSprintussy, "Clear")
	r2.FinalTime = 12 * time.Second

	rh.RecordResult(r1)
	rh.RecordResult(r2)

	stats := rh.GetHorseStats("h1")
	if stats.BestTime != 8*time.Second {
		t.Errorf("BestTime = %v, want 8s", stats.BestTime)
	}
	if stats.WorstTime != 12*time.Second {
		t.Errorf("WorstTime = %v, want 12s", stats.WorstTime)
	}
}

// ===========================================================================
// Weather System
// ===========================================================================

func TestRandomWeatherForTrack_Hauntedussy(t *testing.T) {
	// Hauntedussy should always return Haunted weather.
	for i := 0; i < 50; i++ {
		w := RandomWeatherForTrack(models.TrackHauntedussy)
		if w != models.WeatherHaunted {
			t.Fatalf("Hauntedussy weather = %q, want Haunted", w)
		}
	}
}

func TestRandomWeatherForTrack_NonHaunted(t *testing.T) {
	tracks := []models.TrackType{
		models.TrackSprintussy, models.TrackGrindussy, models.TrackMudussy,
		models.TrackThunderussy, models.TrackFrostussy,
	}

	for _, track := range tracks {
		for i := 0; i < 100; i++ {
			w := RandomWeatherForTrack(track)
			if w == models.WeatherHaunted {
				t.Fatalf("non-Hauntedussy track %s got Haunted weather", track)
			}
		}
	}
}

func TestRandomWeatherForTrack_ValidWeatherTypes(t *testing.T) {
	validWeathers := map[models.Weather]bool{
		models.WeatherClear:     true,
		models.WeatherRainy:     true,
		models.WeatherStormy:    true,
		models.WeatherFoggy:     true,
		models.WeatherScorching: true,
		models.WeatherHaunted:   true,
	}

	for i := 0; i < 200; i++ {
		w := RandomWeatherForTrack(models.TrackSprintussy)
		if !validWeathers[w] {
			t.Fatalf("got invalid weather type: %q", w)
		}
	}
}

func TestRandomWeather_ReturnsValidType(t *testing.T) {
	for i := 0; i < 100; i++ {
		w := RandomWeather()
		switch w {
		case models.WeatherClear, models.WeatherRainy, models.WeatherStormy,
			models.WeatherFoggy, models.WeatherScorching, models.WeatherHaunted:
			// valid
		default:
			t.Fatalf("RandomWeather returned invalid type: %q", w)
		}
	}
}

// ---------------------------------------------------------------------------
// WeatherEffects
// ---------------------------------------------------------------------------

func TestWeatherEffects_Clear(t *testing.T) {
	m := WeatherEffects(models.WeatherClear)
	if m.SpeedMod != 1.0 {
		t.Errorf("Clear SpeedMod = %v, want 1.0", m.SpeedMod)
	}
	if m.FatigueMod != 1.0 {
		t.Errorf("Clear FatigueMod = %v, want 1.0", m.FatigueMod)
	}
	if m.Description == "" {
		t.Error("Clear Description is empty")
	}
}

func TestWeatherEffects_Rainy(t *testing.T) {
	m := WeatherEffects(models.WeatherRainy)
	if m.SpeedMod >= 1.0 {
		t.Errorf("Rainy SpeedMod should be < 1.0, got %v", m.SpeedMod)
	}
	if m.FatigueMod <= 1.0 {
		t.Errorf("Rainy FatigueMod should be > 1.0, got %v", m.FatigueMod)
	}
}

func TestWeatherEffects_Stormy(t *testing.T) {
	m := WeatherEffects(models.WeatherStormy)
	if m.ChaosMod <= 1.0 {
		t.Errorf("Stormy ChaosMod should be > 1.0, got %v", m.ChaosMod)
	}
	if m.PanicMod <= 1.0 {
		t.Errorf("Stormy PanicMod should be > 1.0, got %v", m.PanicMod)
	}
}

func TestWeatherEffects_Haunted(t *testing.T) {
	m := WeatherEffects(models.WeatherHaunted)
	if m.ChaosMod <= 2.0 {
		t.Errorf("Haunted ChaosMod should be > 2.0, got %v", m.ChaosMod)
	}
	if m.PanicMod <= 2.0 {
		t.Errorf("Haunted PanicMod should be > 2.0, got %v", m.PanicMod)
	}
}

func TestWeatherEffects_Scorching(t *testing.T) {
	m := WeatherEffects(models.WeatherScorching)
	if m.FatigueMod <= 1.0 {
		t.Errorf("Scorching FatigueMod should be > 1.0, got %v", m.FatigueMod)
	}
}

func TestWeatherEffects_Foggy(t *testing.T) {
	m := WeatherEffects(models.WeatherFoggy)
	if m.SpeedMod >= 1.0 {
		t.Errorf("Foggy SpeedMod should be < 1.0, got %v", m.SpeedMod)
	}
}

func TestWeatherEffects_Unknown(t *testing.T) {
	m := WeatherEffects("UnknownWeather")
	if m.SpeedMod != 1.0 || m.FatigueMod != 1.0 || m.ChaosMod != 1.0 || m.PanicMod != 1.0 {
		t.Error("unknown weather should default to all 1.0 modifiers")
	}
}

func TestWeatherEffects_AllTypesHaveDescriptions(t *testing.T) {
	weathers := []models.Weather{
		models.WeatherClear, models.WeatherRainy, models.WeatherStormy,
		models.WeatherFoggy, models.WeatherScorching, models.WeatherHaunted,
	}

	for _, w := range weathers {
		m := WeatherEffects(w)
		if m.Description == "" {
			t.Errorf("weather %q has empty description", w)
		}
	}
}

// ===========================================================================
// Tournament Manager
// ===========================================================================

func TestNewTournamentManager(t *testing.T) {
	rh := NewRaceHistory()
	tm := NewTournamentManager(rh)
	if tm == nil {
		t.Fatal("NewTournamentManager returned nil")
	}
}

func TestCreateTournament(t *testing.T) {
	rh := NewRaceHistory()
	tm := NewTournamentManager(rh)

	tournament := tm.CreateTournament("Test Cup", models.TrackSprintussy, 3, 100)
	if tournament == nil {
		t.Fatal("CreateTournament returned nil")
	}
	if tournament.ID == "" {
		t.Error("tournament ID is empty")
	}
	if tournament.Name != "Test Cup" {
		t.Errorf("Name = %q, want Test Cup", tournament.Name)
	}
	if tournament.TrackType != models.TrackSprintussy {
		t.Errorf("TrackType = %q, want Sprintussy", tournament.TrackType)
	}
	if tournament.Rounds != 3 {
		t.Errorf("Rounds = %d, want 3", tournament.Rounds)
	}
	if tournament.CurrentRound != 0 {
		t.Errorf("CurrentRound = %d, want 0", tournament.CurrentRound)
	}
	if tournament.EntryFee != 100 {
		t.Errorf("EntryFee = %d, want 100", tournament.EntryFee)
	}
	if tournament.PrizePool != 0 {
		t.Errorf("PrizePool = %d, want 0", tournament.PrizePool)
	}
	if tournament.Status != "Open" {
		t.Errorf("Status = %q, want Open", tournament.Status)
	}
}

func TestCreateTournament_AutoName(t *testing.T) {
	rh := NewRaceHistory()
	tm := NewTournamentManager(rh)

	tournament := tm.CreateTournament("", models.TrackGrindussy, 2, 500)
	if tournament.Name == "" {
		t.Error("auto-generated name should not be empty")
	}
}

func TestCreateTournament_ZeroRounds(t *testing.T) {
	rh := NewRaceHistory()
	tm := NewTournamentManager(rh)

	tournament := tm.CreateTournament("Test", models.TrackSprintussy, 0, 100)
	if tournament.Rounds != 1 {
		t.Errorf("zero rounds should default to 1, got %d", tournament.Rounds)
	}
}

func TestCreateTournament_NegativeRounds(t *testing.T) {
	rh := NewRaceHistory()
	tm := NewTournamentManager(rh)

	tournament := tm.CreateTournament("Test", models.TrackSprintussy, -5, 100)
	if tournament.Rounds != 1 {
		t.Errorf("negative rounds should default to 1, got %d", tournament.Rounds)
	}
}

// ---------------------------------------------------------------------------
// RegisterHorse
// ---------------------------------------------------------------------------

func TestRegisterHorse(t *testing.T) {
	rh := NewRaceHistory()
	tm := NewTournamentManager(rh)

	tournament := tm.CreateTournament("Test Cup", models.TrackSprintussy, 3, 100)
	horse := makeTestHorse("h1", "Lightning", 1200)

	err := tm.RegisterHorse(tournament.ID, horse, "stable-1")
	if err != nil {
		t.Fatalf("RegisterHorse error: %v", err)
	}

	// Check standings
	standings := tm.GetStandings(tournament.ID)
	if len(standings) != 1 {
		t.Fatalf("expected 1 standing, got %d", len(standings))
	}
	if standings[0].HorseID != "h1" {
		t.Errorf("HorseID = %q, want h1", standings[0].HorseID)
	}
	if standings[0].Points != 0 {
		t.Errorf("Points = %d, want 0", standings[0].Points)
	}
}

func TestRegisterHorse_Duplicate(t *testing.T) {
	rh := NewRaceHistory()
	tm := NewTournamentManager(rh)

	tournament := tm.CreateTournament("Test Cup", models.TrackSprintussy, 3, 100)
	horse := makeTestHorse("h1", "Lightning", 1200)

	tm.RegisterHorse(tournament.ID, horse, "stable-1")
	err := tm.RegisterHorse(tournament.ID, horse, "stable-1")
	if err == nil {
		t.Error("expected error for duplicate registration")
	}
}

func TestRegisterHorse_TournamentNotFound(t *testing.T) {
	rh := NewRaceHistory()
	tm := NewTournamentManager(rh)

	horse := makeTestHorse("h1", "Lightning", 1200)
	err := tm.RegisterHorse("nonexistent", horse, "stable-1")
	if err == nil {
		t.Error("expected error for nonexistent tournament")
	}
}

func TestRegisterHorse_TournamentNotOpen(t *testing.T) {
	rh := NewRaceHistory()
	tm := NewTournamentManager(rh)

	tournament := tm.CreateTournament("Test", models.TrackSprintussy, 1, 100)
	horses := makeTestHorses(3)
	for _, h := range horses {
		tm.RegisterHorse(tournament.ID, h, "s1")
	}
	// Start the tournament by running a round
	tm.RunNextRound(tournament.ID, horses)

	// Now try to register — should fail since tournament is InProgress
	newHorse := makeTestHorse("new", "NewHorse", 1200)
	err := tm.RegisterHorse(tournament.ID, newHorse, "s2")
	if err == nil {
		t.Error("expected error registering in InProgress tournament")
	}
}

// ---------------------------------------------------------------------------
// RunNextRound
// ---------------------------------------------------------------------------

func TestRunNextRound(t *testing.T) {
	rh := NewRaceHistory()
	tm := NewTournamentManager(rh)

	tournament := tm.CreateTournament("Test Cup", models.TrackSprintussy, 3, 100)
	horses := makeTestHorses(4)
	for _, h := range horses {
		tm.RegisterHorse(tournament.ID, h, "s1")
	}

	race, err := tm.RunNextRound(tournament.ID, horses)
	if err != nil {
		t.Fatalf("RunNextRound error: %v", err)
	}
	if race == nil {
		t.Fatal("RunNextRound returned nil race")
	}
	if race.TrackType != models.TrackSprintussy {
		t.Errorf("race TrackType = %q, want Sprintussy", race.TrackType)
	}

	// Tournament should now be InProgress
	retrieved, _ := tm.GetTournament(tournament.ID)
	if retrieved.Status != "InProgress" {
		t.Errorf("Status = %q, want InProgress", retrieved.Status)
	}
	if retrieved.CurrentRound != 1 {
		t.Errorf("CurrentRound = %d, want 1", retrieved.CurrentRound)
	}
}

func TestRunNextRound_TournamentNotFound(t *testing.T) {
	rh := NewRaceHistory()
	tm := NewTournamentManager(rh)

	_, err := tm.RunNextRound("nonexistent", nil)
	if err == nil {
		t.Error("expected error for nonexistent tournament")
	}
}

func TestRunNextRound_AllRoundsComplete(t *testing.T) {
	rh := NewRaceHistory()
	tm := NewTournamentManager(rh)

	tournament := tm.CreateTournament("Quick Cup", models.TrackSprintussy, 1, 100)
	horses := makeTestHorses(3)
	for _, h := range horses {
		tm.RegisterHorse(tournament.ID, h, "s1")
	}

	// Run first (and only) round
	tm.RunNextRound(tournament.ID, horses)

	// Try to run another — should fail
	_, err := tm.RunNextRound(tournament.ID, horses)
	if err == nil {
		t.Error("expected error when all rounds are complete")
	}
}

// ---------------------------------------------------------------------------
// RecordRoundResults
// ---------------------------------------------------------------------------

func TestRecordRoundResults(t *testing.T) {
	rh := NewRaceHistory()
	tm := NewTournamentManager(rh)

	tournament := tm.CreateTournament("Test Cup", models.TrackSprintussy, 3, 100)
	horses := makeTestHorses(4)
	for _, h := range horses {
		tm.RegisterHorse(tournament.ID, h, "s1")
	}

	race, _ := tm.RunNextRound(tournament.ID, horses)
	// Simulate the race
	race = racussy.SimulateRace(race, horses)

	_, err := tm.RecordRoundResults(tournament.ID, race)
	if err != nil {
		t.Fatalf("RecordRoundResults error: %v", err)
	}

	standings := tm.GetStandings(tournament.ID)
	if len(standings) != 4 {
		t.Fatalf("expected 4 standings, got %d", len(standings))
	}

	// Leader should have the most points (10 for 1st place)
	if standings[0].Points < standings[len(standings)-1].Points {
		t.Error("standings not sorted by points")
	}
}

func TestRecordRoundResults_TournamentNotFound(t *testing.T) {
	rh := NewRaceHistory()
	tm := NewTournamentManager(rh)

	race := &models.Race{ID: "fake-race"}
	_, err := tm.RecordRoundResults("nonexistent", race)
	if err == nil {
		t.Error("expected error for nonexistent tournament")
	}
}

// ---------------------------------------------------------------------------
// GetStandings
// ---------------------------------------------------------------------------

func TestGetStandings_Empty(t *testing.T) {
	rh := NewRaceHistory()
	tm := NewTournamentManager(rh)

	tournament := tm.CreateTournament("Empty", models.TrackSprintussy, 1, 0)
	standings := tm.GetStandings(tournament.ID)
	if len(standings) != 0 {
		t.Errorf("expected 0 standings, got %d", len(standings))
	}
}

func TestGetStandings_SortedByPoints(t *testing.T) {
	rh := NewRaceHistory()
	tm := NewTournamentManager(rh)

	tournament := tm.CreateTournament("Test", models.TrackSprintussy, 3, 100)
	horses := makeTestHorses(6)
	for _, h := range horses {
		tm.RegisterHorse(tournament.ID, h, "s1")
	}

	// Run and record 2 rounds
	for i := 0; i < 2; i++ {
		race, _ := tm.RunNextRound(tournament.ID, horses)
		race = racussy.SimulateRace(race, horses)
		tm.RecordRoundResults(tournament.ID, race)
	}

	standings := tm.GetStandings(tournament.ID)
	for i := 1; i < len(standings); i++ {
		if standings[i].Points > standings[i-1].Points {
			t.Errorf("standings not sorted: position %d has %d points > position %d has %d points",
				i, standings[i].Points, i-1, standings[i-1].Points)
		}
	}
}

func TestGetStandings_NonexistentTournament(t *testing.T) {
	rh := NewRaceHistory()
	tm := NewTournamentManager(rh)

	standings := tm.GetStandings("nonexistent")
	if standings != nil {
		t.Error("expected nil for nonexistent tournament")
	}
}

// ---------------------------------------------------------------------------
// GetTournament
// ---------------------------------------------------------------------------

func TestGetTournament(t *testing.T) {
	rh := NewRaceHistory()
	tm := NewTournamentManager(rh)

	tournament := tm.CreateTournament("Test Cup", models.TrackSprintussy, 3, 100)
	retrieved, err := tm.GetTournament(tournament.ID)
	if err != nil {
		t.Fatalf("GetTournament error: %v", err)
	}
	if retrieved.ID != tournament.ID {
		t.Errorf("ID mismatch: %q vs %q", retrieved.ID, tournament.ID)
	}
}

func TestGetTournament_NotFound(t *testing.T) {
	rh := NewRaceHistory()
	tm := NewTournamentManager(rh)

	_, err := tm.GetTournament("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent tournament")
	}
}

// ---------------------------------------------------------------------------
// ListTournaments
// ---------------------------------------------------------------------------

func TestListTournaments(t *testing.T) {
	rh := NewRaceHistory()
	tm := NewTournamentManager(rh)

	tm.CreateTournament("Cup 1", models.TrackSprintussy, 1, 100)
	tm.CreateTournament("Cup 2", models.TrackGrindussy, 2, 200)
	tm.CreateTournament("Cup 3", models.TrackMudussy, 3, 300)

	list := tm.ListTournaments()
	if len(list) != 3 {
		t.Errorf("expected 3 tournaments, got %d", len(list))
	}
}

func TestListTournaments_Empty(t *testing.T) {
	rh := NewRaceHistory()
	tm := NewTournamentManager(rh)

	list := tm.ListTournaments()
	if len(list) != 0 {
		t.Errorf("expected 0 tournaments, got %d", len(list))
	}
}

// ---------------------------------------------------------------------------
// pointsForPlace
// ---------------------------------------------------------------------------

func TestPointsForPlace(t *testing.T) {
	tests := []struct {
		place int
		want  int
	}{
		{1, 10},
		{2, 7},
		{3, 5},
		{4, 3},
		{5, 2},
		{6, 1},
		{10, 1},
		{100, 1},
	}

	for _, tt := range tests {
		got := pointsForPlace(tt.place)
		if got != tt.want {
			t.Errorf("pointsForPlace(%d) = %d, want %d", tt.place, got, tt.want)
		}
	}
}

// ===========================================================================
// Achievements
// ===========================================================================

func TestAllAchievements_NotEmpty(t *testing.T) {
	if len(AllAchievements) == 0 {
		t.Error("AllAchievements is empty")
	}
}

func TestAllAchievements_HaveRequiredFields(t *testing.T) {
	for id, a := range AllAchievements {
		if a.ID != id {
			t.Errorf("achievement %q has mismatched ID field %q", id, a.ID)
		}
		if a.Name == "" {
			t.Errorf("achievement %q has empty Name", id)
		}
		if a.Description == "" {
			t.Errorf("achievement %q has empty Description", id)
		}
		if a.Icon == "" {
			t.Errorf("achievement %q has empty Icon", id)
		}
		if a.Rarity == "" {
			t.Errorf("achievement %q has empty Rarity", id)
		}
	}
}

func TestCheckAchievements_FirstBlood(t *testing.T) {
	rh := NewRaceHistory()
	horse := makeTestHorse("h1", "Horse1", 1200)
	stable := &models.Stable{ID: "s1", Achievements: nil}

	rh.RecordResult(makeRaceResult("r1", "h1", "Horse1", 1, models.TrackSprintussy, "Clear"))

	unlocked := CheckAchievements(horse, rh, stable)

	found := false
	for _, a := range unlocked {
		if a.ID == "first_blood" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected first_blood achievement to be unlocked")
	}
}

func TestCheckAchievements_FirstBlood_AlreadyUnlocked(t *testing.T) {
	rh := NewRaceHistory()
	horse := makeTestHorse("h1", "Horse1", 1200)
	stable := &models.Stable{
		ID: "s1",
		Achievements: []models.Achievement{
			{ID: "first_blood"},
		},
	}

	rh.RecordResult(makeRaceResult("r1", "h1", "Horse1", 1, models.TrackSprintussy, "Clear"))

	unlocked := CheckAchievements(horse, rh, stable)

	for _, a := range unlocked {
		if a.ID == "first_blood" {
			t.Error("first_blood should not be re-unlocked")
		}
	}
}

func TestCheckAchievements_Undefeated5(t *testing.T) {
	rh := NewRaceHistory()
	horse := makeTestHorse("h1", "Horse1", 1200)
	stable := &models.Stable{ID: "s1"}

	// Record 5 wins in a row
	for i := 0; i < 5; i++ {
		rh.RecordResult(makeRaceResult("r"+string(rune('a'+i)), "h1", "Horse1", 1, models.TrackSprintussy, "Clear"))
	}

	unlocked := CheckAchievements(horse, rh, stable)

	found := false
	for _, a := range unlocked {
		if a.ID == "undefeated_5" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected undefeated_5 achievement")
	}
}

func TestCheckAchievements_MudMaster(t *testing.T) {
	rh := NewRaceHistory()
	horse := makeTestHorse("h1", "Horse1", 1200)
	stable := &models.Stable{ID: "s1"}

	// Record 5 Mudussy wins
	for i := 0; i < 5; i++ {
		rh.RecordResult(makeRaceResult("r"+string(rune('a'+i)), "h1", "Horse1", 1, models.TrackMudussy, "Clear"))
	}

	unlocked := CheckAchievements(horse, rh, stable)

	found := false
	for _, a := range unlocked {
		if a.ID == "mud_master" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected mud_master achievement")
	}
}

func TestCheckAchievements_CenturyHorse(t *testing.T) {
	rh := NewRaceHistory()
	horse := makeTestHorse("h1", "Horse1", 1200)
	stable := &models.Stable{ID: "s1"}

	// Record 100 races
	for i := 0; i < 100; i++ {
		place := (i % 6) + 1
		rh.RecordResult(makeRaceResult("r"+string(rune(i)), "h1", "Horse1", place, models.TrackSprintussy, "Clear"))
	}

	unlocked := CheckAchievements(horse, rh, stable)

	found := false
	for _, a := range unlocked {
		if a.ID == "century_horse" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected century_horse achievement")
	}
}

func TestCheckAchievements_Millionaire(t *testing.T) {
	rh := NewRaceHistory()
	horse := makeTestHorse("h1", "Horse1", 1200)
	stable := &models.Stable{
		ID:            "s1",
		TotalEarnings: 1_000_000,
	}

	// Need at least one race result for stats
	rh.RecordResult(makeRaceResult("r1", "h1", "Horse1", 1, models.TrackSprintussy, "Clear"))

	unlocked := CheckAchievements(horse, rh, stable)

	found := false
	for _, a := range unlocked {
		if a.ID == "millionaire" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected millionaire achievement")
	}
}

func TestCheckAchievements_FrostKing(t *testing.T) {
	rh := NewRaceHistory()
	horse := makeTestHorse("h1", "Horse1", 1200)
	stable := &models.Stable{ID: "s1"}

	// Record 5 Frostussy wins
	for i := 0; i < 5; i++ {
		rh.RecordResult(makeRaceResult("r"+string(rune('a'+i)), "h1", "Horse1", 1, models.TrackFrostussy, "Clear"))
	}

	unlocked := CheckAchievements(horse, rh, stable)

	found := false
	for _, a := range unlocked {
		if a.ID == "frost_king" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected frost_king achievement")
	}
}

func TestCheckAchievements_ThunderGod(t *testing.T) {
	rh := NewRaceHistory()
	horse := makeTestHorse("h1", "Horse1", 1200)
	stable := &models.Stable{ID: "s1"}

	// Record 5 Thunderussy wins
	for i := 0; i < 5; i++ {
		rh.RecordResult(makeRaceResult("r"+string(rune('a'+i)), "h1", "Horse1", 1, models.TrackThunderussy, "Clear"))
	}

	unlocked := CheckAchievements(horse, rh, stable)

	found := false
	for _, a := range unlocked {
		if a.ID == "thunder_god" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected thunder_god achievement")
	}
}

func TestCheckAchievements_NoWins_NoAchievements(t *testing.T) {
	rh := NewRaceHistory()
	horse := makeTestHorse("h1", "Horse1", 1200)
	stable := &models.Stable{ID: "s1"}

	// Record a loss
	rh.RecordResult(makeRaceResult("r1", "h1", "Horse1", 5, models.TrackSprintussy, "Clear"))

	unlocked := CheckAchievements(horse, rh, stable)

	// Should not have first_blood (no wins)
	for _, a := range unlocked {
		if a.ID == "first_blood" {
			t.Error("first_blood should not unlock without a win")
		}
	}
}

func TestCheckAchievements_StormChaser(t *testing.T) {
	rh := NewRaceHistory()
	horse := makeTestHorse("h1", "Horse1", 1200)
	stable := &models.Stable{ID: "s1"}

	rh.RecordResult(makeRaceResult("r1", "h1", "Horse1", 1, models.TrackThunderussy, string(models.WeatherStormy)))

	unlocked := CheckAchievements(horse, rh, stable)

	found := false
	for _, a := range unlocked {
		if a.ID == "storm_chaser" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected storm_chaser achievement")
	}
}

func TestCheckAchievements_EloFloor(t *testing.T) {
	rh := NewRaceHistory()
	horse := makeTestHorse("h1", "Horse1", 750)
	stable := &models.Stable{
		ID:     "s1",
		Horses: []models.Horse{{ID: "h1", ELO: 750}},
	}

	rh.RecordResult(makeRaceResult("r1", "h1", "Horse1", 6, models.TrackSprintussy, "Clear"))

	unlocked := CheckAchievements(horse, rh, stable)

	found := false
	for _, a := range unlocked {
		if a.ID == "elo_floor" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected elo_floor achievement")
	}
}

func TestCheckAchievements_FullStable(t *testing.T) {
	rh := NewRaceHistory()
	horse := makeTestHorse("h1", "Horse1", 1200)

	// Create a stable with 20+ horses
	stableHorses := make([]models.Horse, 22)
	for i := 0; i < 22; i++ {
		stableHorses[i] = models.Horse{
			ID:   "sh" + string(rune('a'+i)),
			Name: "StableHorse",
			ELO:  1200,
		}
	}

	stable := &models.Stable{
		ID:     "s1",
		Horses: stableHorses,
	}

	rh.RecordResult(makeRaceResult("r1", "h1", "Horse1", 1, models.TrackSprintussy, "Clear"))

	unlocked := CheckAchievements(horse, rh, stable)

	found := false
	for _, a := range unlocked {
		if a.ID == "full_stable" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected full_stable achievement")
	}
}

func TestCheckAchievements_TheYogurtSees(t *testing.T) {
	rh := NewRaceHistory()
	horse := makeTestHorse("h1", "Horse1", 1200)
	stable := &models.Stable{ID: "s1"}

	// Race on Hauntedussy and finish
	rh.RecordResult(makeRaceResult("r1", "h1", "Horse1", 3, models.TrackHauntedussy, string(models.WeatherHaunted)))

	unlocked := CheckAchievements(horse, rh, stable)

	found := false
	for _, a := range unlocked {
		if a.ID == "the_yogurt_sees" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected the_yogurt_sees achievement")
	}
}

func TestCheckAchievements_ElderStatesman(t *testing.T) {
	rh := NewRaceHistory()
	horse := makeTestHorse("h1", "Horse1", 1200)
	horse.Age = 14

	stable := &models.Stable{ID: "s1"}

	rh.RecordResult(makeRaceResult("r1", "h1", "Horse1", 1, models.TrackSprintussy, "Clear"))

	unlocked := CheckAchievements(horse, rh, stable)

	found := false
	for _, a := range unlocked {
		if a.ID == "elder_statesman" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected elder_statesman achievement")
	}
}

func TestCheckAchievements_Dynasty(t *testing.T) {
	rh := NewRaceHistory()
	horse := makeTestHorse("h1", "Horse1", 1200)

	stable := &models.Stable{
		ID: "s1",
		Horses: []models.Horse{
			{ID: "g0", Generation: 0, Wins: 5, ELO: 1200},
			{ID: "g1", Generation: 1, Wins: 3, ELO: 1200},
			{ID: "g2", Generation: 2, Wins: 2, ELO: 1200},
		},
	}

	rh.RecordResult(makeRaceResult("r1", "h1", "Horse1", 1, models.TrackSprintussy, "Clear"))

	unlocked := CheckAchievements(horse, rh, stable)

	found := false
	for _, a := range unlocked {
		if a.ID == "dynasty" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected dynasty achievement")
	}
}

// ---------------------------------------------------------------------------
// hasAchievement helper
// ---------------------------------------------------------------------------

func TestHasAchievement(t *testing.T) {
	achievements := []models.Achievement{
		{ID: "first_blood"},
		{ID: "mud_master"},
	}

	if !hasAchievement(achievements, "first_blood") {
		t.Error("expected to find first_blood")
	}
	if !hasAchievement(achievements, "mud_master") {
		t.Error("expected to find mud_master")
	}
	if hasAchievement(achievements, "nonexistent") {
		t.Error("should not find nonexistent")
	}
	if hasAchievement(nil, "first_blood") {
		t.Error("should not find in nil slice")
	}
}

// ---------------------------------------------------------------------------
// Full tournament simulation
// ---------------------------------------------------------------------------

func TestFullTournamentSimulation(t *testing.T) {
	rh := NewRaceHistory()
	tm := NewTournamentManager(rh)

	tournament := tm.CreateTournament("Grand Prix", models.TrackMudussy, 3, 500)
	horses := makeTestHorses(6)

	for _, h := range horses {
		err := tm.RegisterHorse(tournament.ID, h, "s1")
		if err != nil {
			t.Fatalf("RegisterHorse error: %v", err)
		}
	}

	// Run all 3 rounds
	for i := 0; i < 3; i++ {
		race, err := tm.RunNextRound(tournament.ID, horses)
		if err != nil {
			t.Fatalf("RunNextRound %d error: %v", i+1, err)
		}

		race = racussy.SimulateRace(race, horses)

		_, err = tm.RecordRoundResults(tournament.ID, race)
		if err != nil {
			t.Fatalf("RecordRoundResults %d error: %v", i+1, err)
		}
	}

	// Tournament should be finished
	final, err := tm.GetTournament(tournament.ID)
	if err != nil {
		t.Fatalf("GetTournament error: %v", err)
	}
	if final.Status != "Finished" {
		t.Errorf("Status = %q, want Finished", final.Status)
	}
	if final.CurrentRound != 3 {
		t.Errorf("CurrentRound = %d, want 3", final.CurrentRound)
	}

	// Should have standings for all 6 horses
	standings := tm.GetStandings(tournament.ID)
	if len(standings) != 6 {
		t.Errorf("expected 6 standings, got %d", len(standings))
	}

	// Leader should have run 3 races
	for _, s := range standings {
		if s.RacesRun != 3 {
			t.Errorf("horse %q ran %d races, want 3", s.HorseName, s.RacesRun)
		}
	}
}

// ---------------------------------------------------------------------------
// generateTournamentName
// ---------------------------------------------------------------------------

func TestGenerateTournamentName(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		name := generateTournamentName()
		if name == "" {
			t.Fatal("generateTournamentName returned empty string")
		}
		seen[name] = true
	}
	// Should produce at least a few different names in 50 tries
	if len(seen) < 3 {
		t.Errorf("expected variety in names, got only %d unique in 50 tries", len(seen))
	}
}
