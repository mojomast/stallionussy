package racussy

import (
	"math"
	"strings"
	"testing"

	"github.com/mojomast/stallionussy/internal/genussy"
	"github.com/mojomast/stallionussy/internal/models"
)

// floatClose returns true if a and b are within epsilon of each other.
func floatClose(a, b, epsilon float64) bool {
	return math.Abs(a-b) < epsilon
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// makeTestHorse creates a horse with a random genome and the given fitness.
func makeTestHorse(name string, fitness float64) *models.Horse {
	g := genussy.RandomGenome()
	return &models.Horse{
		ID:             "horse-" + name,
		Name:           name,
		Genome:         g,
		CurrentFitness: fitness,
		FitnessCeiling: 1.0,
		ELO:            1200,
		Traits:         []models.Trait{},
	}
}

// makeEliteHorse creates a horse with all-AA genes and the given fitness.
func makeEliteHorse(name string, fitness float64) *models.Horse {
	g := models.Genome{
		models.GeneSPD: {Type: models.GeneSPD, AlleleA: models.AlleleA, AlleleB: models.AlleleA},
		models.GeneSTM: {Type: models.GeneSTM, AlleleA: models.AlleleA, AlleleB: models.AlleleA},
		models.GeneTMP: {Type: models.GeneTMP, AlleleA: models.AlleleA, AlleleB: models.AlleleA},
		models.GeneSZE: {Type: models.GeneSZE, AlleleA: models.AlleleA, AlleleB: models.AlleleA},
		models.GeneREC: {Type: models.GeneREC, AlleleA: models.AlleleA, AlleleB: models.AlleleA},
		models.GeneINT: {Type: models.GeneINT, AlleleA: models.AlleleA, AlleleB: models.AlleleA},
		models.GeneMUT: {Type: models.GeneMUT, AlleleA: models.AlleleA, AlleleB: models.AlleleA},
	}
	return &models.Horse{
		ID:             "horse-" + name,
		Name:           name,
		Genome:         g,
		CurrentFitness: fitness,
		FitnessCeiling: 1.0,
		ELO:            1200,
		Traits:         []models.Trait{},
	}
}

// makeWeakHorse creates a horse with all-BB genes and the given fitness.
func makeWeakHorse(name string, fitness float64) *models.Horse {
	g := models.Genome{
		models.GeneSPD: {Type: models.GeneSPD, AlleleA: models.AlleleB, AlleleB: models.AlleleB},
		models.GeneSTM: {Type: models.GeneSTM, AlleleA: models.AlleleB, AlleleB: models.AlleleB},
		models.GeneTMP: {Type: models.GeneTMP, AlleleA: models.AlleleB, AlleleB: models.AlleleB},
		models.GeneSZE: {Type: models.GeneSZE, AlleleA: models.AlleleB, AlleleB: models.AlleleB},
		models.GeneREC: {Type: models.GeneREC, AlleleA: models.AlleleB, AlleleB: models.AlleleB},
		models.GeneINT: {Type: models.GeneINT, AlleleA: models.AlleleB, AlleleB: models.AlleleB},
		models.GeneMUT: {Type: models.GeneMUT, AlleleA: models.AlleleB, AlleleB: models.AlleleB},
	}
	return &models.Horse{
		ID:             "horse-" + name,
		Name:           name,
		Genome:         g,
		CurrentFitness: fitness,
		FitnessCeiling: fitness,
		ELO:            1200,
		Traits:         []models.Trait{},
	}
}

// makeHorseField builds a small field of test horses.
func makeHorseField(count int) []*models.Horse {
	names := []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot", "Golf", "Hotel"}
	horses := make([]*models.Horse, count)
	for i := 0; i < count; i++ {
		name := names[i%len(names)]
		horses[i] = makeTestHorse(name, 0.5+float64(i)*0.05)
	}
	return horses
}

// allTrackTypes returns the 6 canonical track types.
func allTrackTypes() []models.TrackType {
	return []models.TrackType{
		models.TrackSprintussy,
		models.TrackGrindussy,
		models.TrackMudussy,
		models.TrackThunderussy,
		models.TrackFrostussy,
		models.TrackHauntedussy,
	}
}

// ---------------------------------------------------------------------------
// 1. TestNewRace — verify race creation
// ---------------------------------------------------------------------------

func TestNewRace(t *testing.T) {
	horses := makeHorseField(4)
	purse := int64(5000)

	for _, tt := range allTrackTypes() {
		t.Run(string(tt), func(t *testing.T) {
			race := NewRace(horses, tt, purse)

			// ID should be a non-empty UUID string.
			if race.ID == "" {
				t.Error("race ID is empty")
			}

			// Track type.
			if race.TrackType != tt {
				t.Errorf("expected TrackType=%s, got %s", tt, race.TrackType)
			}

			// Distance should match the canonical distance for the track type.
			expectedDist := models.TrackDistance(tt)
			if race.Distance != expectedDist {
				t.Errorf("expected Distance=%d for %s, got %d", expectedDist, tt, race.Distance)
			}

			// Purse.
			if race.Purse != purse {
				t.Errorf("expected Purse=%d, got %d", purse, race.Purse)
			}

			// Status should be Pending.
			if race.Status != models.RaceStatusPending {
				t.Errorf("expected Status=Pending, got %s", race.Status)
			}

			// Entries count.
			if len(race.Entries) != len(horses) {
				t.Errorf("expected %d entries, got %d", len(horses), len(race.Entries))
			}

			// Each entry should reference a horse and start at position 0.
			for i, entry := range race.Entries {
				if entry.HorseID != horses[i].ID {
					t.Errorf("entry[%d] HorseID=%s, want %s", i, entry.HorseID, horses[i].ID)
				}
				if entry.HorseName != horses[i].Name {
					t.Errorf("entry[%d] HorseName=%s, want %s", i, entry.HorseName, horses[i].Name)
				}
				if entry.Position != 0 {
					t.Errorf("entry[%d] Position=%f, want 0", i, entry.Position)
				}
				if entry.Finished {
					t.Errorf("entry[%d] should not be finished", i)
				}
				if entry.TickLog == nil {
					t.Errorf("entry[%d] TickLog should be initialized (not nil)", i)
				}
			}

			// CreatedAt should be recent (non-zero).
			if race.CreatedAt.IsZero() {
				t.Error("CreatedAt should not be zero")
			}
		})
	}
}

func TestNewRaceEmptyField(t *testing.T) {
	race := NewRace([]*models.Horse{}, models.TrackSprintussy, 1000)
	if len(race.Entries) != 0 {
		t.Errorf("expected 0 entries for empty field, got %d", len(race.Entries))
	}
	if race.Status != models.RaceStatusPending {
		t.Errorf("expected Pending status, got %s", race.Status)
	}
}

// ---------------------------------------------------------------------------
// 2. TestSimulateRace — verify race completes, entries have tick logs, places
// ---------------------------------------------------------------------------

func TestSimulateRace(t *testing.T) {
	horses := makeHorseField(4)
	race := NewRace(horses, models.TrackSprintussy, 5000)

	// Use a deterministic seed for reproducibility.
	result := SimulateRace(race, horses, 42)

	// Status should be Finished.
	if result.Status != models.RaceStatusFinished {
		t.Errorf("expected Finished status, got %s", result.Status)
	}

	// All entries should have finished.
	for i, entry := range result.Entries {
		if !entry.Finished {
			t.Errorf("entry[%d] %s did not finish", i, entry.HorseName)
		}
	}

	// Each entry should have tick log entries.
	for i, entry := range result.Entries {
		if len(entry.TickLog) == 0 {
			t.Errorf("entry[%d] %s has empty tick log", i, entry.HorseName)
		}
	}

	// Finish places should be assigned: 1 through N with no duplicates.
	places := make(map[int]bool)
	for i, entry := range result.Entries {
		if entry.FinishPlace < 1 || entry.FinishPlace > len(horses) {
			t.Errorf("entry[%d] FinishPlace=%d out of range [1,%d]", i, entry.FinishPlace, len(horses))
		}
		if places[entry.FinishPlace] {
			t.Errorf("duplicate finish place %d", entry.FinishPlace)
		}
		places[entry.FinishPlace] = true
	}

	// FinalTime should be positive for all entries.
	for i, entry := range result.Entries {
		if entry.FinalTime <= 0 {
			t.Errorf("entry[%d] FinalTime=%v should be positive", i, entry.FinalTime)
		}
	}
}

func TestSimulateRaceDeterministic(t *testing.T) {
	horses := makeHorseField(3)

	// Two runs with the same seed should produce identical results.
	race1 := NewRace(horses, models.TrackMudussy, 3000)
	result1 := SimulateRace(race1, horses, 99)

	race2 := NewRace(horses, models.TrackMudussy, 3000)
	result2 := SimulateRace(race2, horses, 99)

	for i := range result1.Entries {
		if result1.Entries[i].FinishPlace != result2.Entries[i].FinishPlace {
			t.Errorf("entry[%d] places differ: %d vs %d",
				i, result1.Entries[i].FinishPlace, result2.Entries[i].FinishPlace)
		}
		if result1.Entries[i].FinalTime != result2.Entries[i].FinalTime {
			t.Errorf("entry[%d] times differ: %v vs %v",
				i, result1.Entries[i].FinalTime, result2.Entries[i].FinalTime)
		}
	}
}

// ---------------------------------------------------------------------------
// 3. TestSimulateRaceWithWeather — verify weather affects the race
// ---------------------------------------------------------------------------

func TestSimulateRaceWithWeather(t *testing.T) {
	weathers := []models.Weather{
		models.WeatherClear,
		models.WeatherRainy,
		models.WeatherStormy,
		models.WeatherFoggy,
		models.WeatherScorching,
		models.WeatherHaunted,
	}

	for _, w := range weathers {
		t.Run(string(w), func(t *testing.T) {
			horses := makeHorseField(3)
			race := NewRace(horses, models.TrackSprintussy, 2000)
			result := SimulateRaceWithWeather(race, horses, w, 42)

			if result.Status != models.RaceStatusFinished {
				t.Errorf("race did not finish under weather %s", w)
			}

			for i, entry := range result.Entries {
				if !entry.Finished {
					t.Errorf("entry[%d] did not finish under weather %s", i, w)
				}
				if len(entry.TickLog) == 0 {
					t.Errorf("entry[%d] has empty tick log under weather %s", i, w)
				}
			}
		})
	}
}

func TestWeatherAffectsSpeed(t *testing.T) {
	// Stormy weather should generally make a race take more ticks than clear.
	horses := []*models.Horse{makeEliteHorse("SpeedDemon", 0.9)}
	seed := uint64(123)

	raceClear := NewRace(horses, models.TrackSprintussy, 1000)
	resultClear := SimulateRaceWithWeather(raceClear, horses, models.WeatherClear, seed)

	raceStormy := NewRace(horses, models.TrackSprintussy, 1000)
	resultStormy := SimulateRaceWithWeather(raceStormy, horses, models.WeatherStormy, seed)

	clearTicks := len(resultClear.Entries[0].TickLog)
	stormyTicks := len(resultStormy.Entries[0].TickLog)

	// Stormy has speedMod=0.85 + fatigueMod=1.3, so it should take more ticks.
	if stormyTicks <= clearTicks {
		t.Logf("clear ticks=%d, stormy ticks=%d", clearTicks, stormyTicks)
		t.Error("expected stormy race to take more ticks than clear race")
	}
}

// ---------------------------------------------------------------------------
// 4. TestGenerateRaceNarrative — verify it produces narrative lines
// ---------------------------------------------------------------------------

func TestGenerateRaceNarrative(t *testing.T) {
	horses := makeHorseField(4)
	race := NewRace(horses, models.TrackSprintussy, 5000)
	result := SimulateRace(race, horses, 42)

	narrative := GenerateRaceNarrative(result)

	if len(narrative) == 0 {
		t.Fatal("narrative should not be empty")
	}

	// Should contain weather line.
	foundWeather := false
	for _, line := range narrative {
		if strings.Contains(line, "WEATHER:") {
			foundWeather = true
			break
		}
	}
	if !foundWeather {
		t.Error("narrative should contain a WEATHER line")
	}

	// Should contain final results section.
	foundResults := false
	for _, line := range narrative {
		if strings.Contains(line, "FINAL RESULTS") {
			foundResults = true
			break
		}
	}
	if !foundResults {
		t.Error("narrative should contain FINAL RESULTS")
	}

	// Should contain at least one horse name.
	foundHorseName := false
	for _, line := range narrative {
		if strings.Contains(line, horses[0].Name) {
			foundHorseName = true
			break
		}
	}
	if !foundHorseName {
		t.Errorf("narrative should mention horse name %q", horses[0].Name)
	}

	// Should contain ordinal place strings (1st, 2nd, etc.).
	foundOrdinal := false
	for _, line := range narrative {
		if strings.Contains(line, "1st") || strings.Contains(line, "2nd") ||
			strings.Contains(line, "3rd") || strings.Contains(line, "4th") {
			foundOrdinal = true
			break
		}
	}
	if !foundOrdinal {
		t.Error("narrative should contain ordinal place strings")
	}
}

func TestGenerateRaceNarrativeEmpty(t *testing.T) {
	race := &models.Race{
		Entries: []models.RaceEntry{},
	}
	narrative := GenerateRaceNarrative(race)
	if narrative != nil {
		t.Errorf("expected nil narrative for empty entries, got %v", narrative)
	}
}

// ---------------------------------------------------------------------------
// 5. TestGenerateRaceNarrativeWithWeather — weather info, PANIC throttling
// ---------------------------------------------------------------------------

func TestGenerateRaceNarrativeWithWeather(t *testing.T) {
	weathers := []struct {
		w    models.Weather
		desc string // substring expected in the weather line
	}{
		{models.WeatherClear, "Clear skies"},
		{models.WeatherRainy, "Rain lashes"},
		{models.WeatherStormy, "violent storm"},
		{models.WeatherFoggy, "Dense fog"},
		{models.WeatherScorching, "sun beats"},
		{models.WeatherHaunted, "E-008"},
	}

	for _, tc := range weathers {
		t.Run(string(tc.w), func(t *testing.T) {
			horses := makeHorseField(3)
			race := NewRace(horses, models.TrackMudussy, 2000)
			result := SimulateRaceWithWeather(race, horses, tc.w, 42)

			narrative := GenerateRaceNarrativeWithWeather(result, tc.w)
			if len(narrative) == 0 {
				t.Fatal("narrative should not be empty")
			}

			// First line should contain the weather type.
			if !strings.Contains(narrative[0], string(tc.w)) {
				t.Errorf("first line should mention weather %q, got %q", tc.w, narrative[0])
			}

			// Weather description should be present.
			if !strings.Contains(narrative[0], tc.desc) {
				t.Errorf("first line should contain %q, got %q", tc.desc, narrative[0])
			}
		})
	}
}

func TestPanicThrottling(t *testing.T) {
	// Create a horse with all BB temper (highly panic-prone) and run on Mudussy.
	horse := makeWeakHorse("PanicProne", 0.5)
	horses := []*models.Horse{horse}
	race := NewRace(horses, models.TrackMudussy, 1000)
	result := SimulateRaceWithWeather(race, horses, models.WeatherStormy, 77)

	narrative := GenerateRaceNarrativeWithWeather(result, models.WeatherStormy)

	// Count PANIC lines in narrative.
	panicCount := 0
	for _, line := range narrative {
		if strings.Contains(line, "PANIC") && !strings.Contains(line, "existential dread") {
			panicCount++
		}
	}

	// The throttle limit is 3 per horse. There should be at most 3 PANIC event lines
	// (the "existential dread" escalation message doesn't count as a PANIC event line).
	if panicCount > 3 {
		t.Errorf("expected at most 3 PANIC narrative lines per horse (throttled), got %d", panicCount)
	}
}

// ---------------------------------------------------------------------------
// 6. TestAllTrackTypes — verify all 6 track types complete a race
// ---------------------------------------------------------------------------

func TestAllTrackTypes(t *testing.T) {
	trackDistances := map[models.TrackType]int{
		models.TrackSprintussy:  800,
		models.TrackGrindussy:   3200,
		models.TrackMudussy:     1600,
		models.TrackThunderussy: 2400,
		models.TrackFrostussy:   1200,
		models.TrackHauntedussy: 666,
	}

	for tt, expectedDist := range trackDistances {
		t.Run(string(tt), func(t *testing.T) {
			horses := makeHorseField(4)
			race := NewRace(horses, tt, 3000)

			// Verify distance.
			if race.Distance != expectedDist {
				t.Errorf("distance for %s: got %d, want %d", tt, race.Distance, expectedDist)
			}

			// Simulate and verify completion.
			result := SimulateRace(race, horses, 42)
			if result.Status != models.RaceStatusFinished {
				t.Errorf("%s race did not finish", tt)
			}

			for i, entry := range result.Entries {
				if !entry.Finished {
					t.Errorf("%s entry[%d] did not finish", tt, i)
				}
				if entry.FinishPlace < 1 {
					t.Errorf("%s entry[%d] FinishPlace=%d, expected >= 1", tt, i, entry.FinishPlace)
				}
			}
		})
	}
}

func TestTrackDistanceConstants(t *testing.T) {
	// Verify the local constants match models.TrackDistance.
	checks := []struct {
		constant int
		track    models.TrackType
	}{
		{DistanceSprintussy, models.TrackSprintussy},
		{DistanceGrindussy, models.TrackGrindussy},
		{DistanceMudussy, models.TrackMudussy},
		{DistanceThunderussy, models.TrackThunderussy},
		{DistanceFrostussy, models.TrackFrostussy},
		{DistanceHauntedussy, models.TrackHauntedussy},
	}
	for _, c := range checks {
		d := models.TrackDistance(c.track)
		if c.constant != d {
			t.Errorf("local constant for %s = %d, models.TrackDistance = %d", c.track, c.constant, d)
		}
	}
}

// ---------------------------------------------------------------------------
// 7. TestTickLogEntriesValid — verify positions and speeds
// ---------------------------------------------------------------------------

func TestTickLogEntriesValid(t *testing.T) {
	horses := makeHorseField(3)
	race := NewRace(horses, models.TrackSprintussy, 2000)
	result := SimulateRace(race, horses, 42)

	for i, entry := range result.Entries {
		if len(entry.TickLog) == 0 {
			t.Errorf("entry[%d] has no tick log", i)
			continue
		}

		for j, te := range entry.TickLog {
			// Position should be non-negative and monotonically non-decreasing.
			if te.Position < 0 {
				t.Errorf("entry[%d] tick[%d]: negative position %f", i, j, te.Position)
			}

			// Speed (deltaP) should be >= 0 (clamped in simulation).
			if te.Speed < 0 {
				t.Errorf("entry[%d] tick[%d]: negative speed %f", i, j, te.Speed)
			}

			// Tick numbers should be sequential starting from 1.
			if te.Tick != j+1 {
				t.Errorf("entry[%d] tick[%d]: expected Tick=%d, got %d", i, j, j+1, te.Tick)
			}

			// Position should be monotonically non-decreasing.
			if j > 0 && te.Position < entry.TickLog[j-1].Position {
				t.Errorf("entry[%d] tick[%d]: position decreased from %f to %f",
					i, j, entry.TickLog[j-1].Position, te.Position)
			}
		}

		// The final position should be at or past the finish line.
		lastPos := entry.TickLog[len(entry.TickLog)-1].Position
		if lastPos < float64(result.Distance) {
			t.Errorf("entry[%d] final position %f < distance %d (should have finished)",
				i, lastPos, result.Distance)
		}
	}
}

func TestTickLogSpeedReasonable(t *testing.T) {
	// A single elite horse on sprint should have reasonable speed values.
	horse := makeEliteHorse("SpeedTest", 0.9)
	horses := []*models.Horse{horse}
	race := NewRace(horses, models.TrackSprintussy, 1000)
	result := SimulateRace(race, horses, 42)

	entry := result.Entries[0]
	for _, te := range entry.TickLog {
		// Maximum theoretical speed: speedScale(18) * fitness(0.9) * geneScore(1.0) * trackMod(1.0)
		// = 16.2 m/tick. With chaos that could be higher, but shouldn't be absurd.
		// Allow up to 50 m/tick as a sanity limit.
		if te.Speed > 50 {
			t.Errorf("tick %d: speed %f seems unreasonably high", te.Tick, te.Speed)
		}
	}
}

// ---------------------------------------------------------------------------
// 8. TestOrdinal — test the ordinal() helper
// ---------------------------------------------------------------------------

func TestOrdinal(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{1, "1st"},
		{2, "2nd"},
		{3, "3rd"},
		{4, "4th"},
		{5, "5th"},
		{9, "9th"},
		{10, "10th"},
		{11, "11th"},
		{12, "12th"},
		{13, "13th"},
		{14, "14th"},
		{20, "20th"},
		{21, "21st"},
		{22, "22nd"},
		{23, "23rd"},
		{24, "24th"},
		{100, "100th"},
		{101, "101st"},
		{111, "111th"},
		{112, "112th"},
		{113, "113th"},
		{121, "121st"},
		{122, "122nd"},
		{123, "123rd"},
		{200, "200th"},
		{1011, "1011th"},
		{1012, "1012th"},
		{1013, "1013th"},
	}
	for _, tc := range cases {
		got := ordinal(tc.n)
		if got != tc.want {
			t.Errorf("ordinal(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Additional: CalcBaseSpeed
// ---------------------------------------------------------------------------

func TestCalcBaseSpeed(t *testing.T) {
	// Elite horse (all AA, fitness 1.0) on sprint track:
	// weights: SPD=0.8, STM=0.2, TMP=0.0
	// geneScore for AA = 1.0
	// geneticFactor = 1.0*0.8 + 1.0*0.2 + 1.0*0.0 = 1.0
	// BaseSpeed = 18.0 * 1.0 * 1.0 = 18.0
	elite := makeEliteHorse("Elite", 1.0)
	speed := CalcBaseSpeed(elite, models.TrackSprintussy)
	if speed != 18.0 {
		t.Errorf("CalcBaseSpeed for elite on sprint: got %f, want 18.0", speed)
	}

	// Weak horse (all BB, fitness 0.3) on sprint track:
	// geneScore for BB = 0.3
	// geneticFactor = 0.3*0.8 + 0.3*0.2 + 0.3*0.0 = 0.3
	// BaseSpeed = 18.0 * 0.3 * 0.3 = 1.62
	weak := makeWeakHorse("Weak", 0.3)
	speed = CalcBaseSpeed(weak, models.TrackSprintussy)
	expected := 1.62
	if !floatClose(speed, expected, 0.001) {
		t.Errorf("CalcBaseSpeed for weak on sprint: got %f, want ~%f", speed, expected)
	}

	// Speed should always be non-negative.
	for _, tt := range allTrackTypes() {
		s := CalcBaseSpeed(elite, tt)
		if s < 0 {
			t.Errorf("CalcBaseSpeed returned negative speed %f for track %s", s, tt)
		}
	}
}

// ---------------------------------------------------------------------------
// Additional: CalcPostRaceFatigue
// ---------------------------------------------------------------------------

func TestCalcPostRaceFatigue(t *testing.T) {
	horse := makeTestHorse("Tired", 0.5)
	race := NewRace([]*models.Horse{horse}, models.TrackGrindussy, 1000)

	// Base fatigue = 3200 / 100 = 32.
	// Winner bonus: +5 → 37.
	fatigue := CalcPostRaceFatigue(horse, race, 1, models.WeatherClear)
	if fatigue != 37.0 {
		t.Errorf("expected fatigue=37.0 for winner on Grindussy/Clear, got %f", fatigue)
	}

	// Last place (1 entry): +10 → 42.
	fatigue = CalcPostRaceFatigue(horse, race, 1, models.WeatherClear)
	// With 1 entry, place 1 is both winner and last → winner bonus applies.
	// The code checks finishPlace==1 first (switch case), so +5 → 37.
	if fatigue != 37.0 {
		t.Errorf("expected fatigue=37.0 for single-horse winner, got %f", fatigue)
	}

	// Multi-entry race: last place.
	horses3 := makeHorseField(3)
	race3 := NewRace(horses3, models.TrackGrindussy, 1000)
	fatigue = CalcPostRaceFatigue(horses3[0], race3, 3, models.WeatherClear)
	// Base=32, last place=+10 → 42.
	if fatigue != 42.0 {
		t.Errorf("expected fatigue=42.0 for last place on Grindussy/Clear, got %f", fatigue)
	}

	// Scorching weather: base × 1.5.
	fatigue = CalcPostRaceFatigue(horses3[0], race3, 2, models.WeatherScorching)
	// Base=32, middle place=no bonus, × 1.5 = 48.
	if fatigue != 48.0 {
		t.Errorf("expected fatigue=48.0 for middle on Grindussy/Scorching, got %f", fatigue)
	}

	// Stormy weather: winner → (32+5) × 1.3 = 48.1.
	fatigue = CalcPostRaceFatigue(horses3[0], race3, 1, models.WeatherStormy)
	if fatigue != 48.1 {
		t.Errorf("expected fatigue=48.1 for winner on Grindussy/Stormy, got %f", fatigue)
	}
}

// ---------------------------------------------------------------------------
// Additional: DefaultConfig
// ---------------------------------------------------------------------------

func TestDefaultConfig(t *testing.T) {
	for _, tt := range allTrackTypes() {
		cfg := DefaultConfig(tt)
		if cfg.TrackType != tt {
			t.Errorf("DefaultConfig(%s).TrackType = %s", tt, cfg.TrackType)
		}
		if cfg.Distance != models.TrackDistance(tt) {
			t.Errorf("DefaultConfig(%s).Distance = %d, want %d", tt, cfg.Distance, models.TrackDistance(tt))
		}
		if cfg.TickInterval <= 0 {
			t.Errorf("DefaultConfig(%s).TickInterval = %v, expected positive", tt, cfg.TickInterval)
		}
	}
}

// ---------------------------------------------------------------------------
// Additional: Hauntedussy-specific events
// ---------------------------------------------------------------------------

func TestHauntedussyTrack(t *testing.T) {
	horses := makeHorseField(4)
	race := NewRace(horses, models.TrackHauntedussy, 2000)
	result := SimulateRaceWithWeather(race, horses, models.WeatherHaunted, 42)

	narrative := GenerateRaceNarrativeWithWeather(result, models.WeatherHaunted)

	// Should have the Hauntedussy opening flavor text.
	foundHauntedOpening := false
	for _, line := range narrative {
		if strings.Contains(line, "shadows lengthen") || strings.Contains(line, "Hauntedussy") {
			foundHauntedOpening = true
			break
		}
	}
	if !foundHauntedOpening {
		t.Error("expected Hauntedussy-specific opening in narrative")
	}

	// Should have FINAL RESULTS.
	foundResults := false
	for _, line := range narrative {
		if strings.Contains(line, "FINAL RESULTS") {
			foundResults = true
			break
		}
	}
	if !foundResults {
		t.Error("narrative should contain FINAL RESULTS")
	}
}

// ---------------------------------------------------------------------------
// Additional: Frostussy and Thunderussy narrative
// ---------------------------------------------------------------------------

func TestFrostussyNarrative(t *testing.T) {
	horses := makeHorseField(3)
	race := NewRace(horses, models.TrackFrostussy, 2000)
	result := SimulateRace(race, horses, 42)

	narrative := GenerateRaceNarrativeWithWeather(result, models.WeatherClear)

	// Should have the Frostussy opening.
	foundFrostOpening := false
	for _, line := range narrative {
		if strings.Contains(line, "Ice crystals") || strings.Contains(line, "Frostussy") {
			foundFrostOpening = true
			break
		}
	}
	if !foundFrostOpening {
		t.Error("expected Frostussy-specific opening in narrative")
	}
}

func TestThunderussyNarrative(t *testing.T) {
	horses := makeHorseField(3)
	race := NewRace(horses, models.TrackThunderussy, 2000)
	result := SimulateRace(race, horses, 42)

	narrative := GenerateRaceNarrativeWithWeather(result, models.WeatherClear)

	// Should have the Thunderussy opening.
	foundThunderOpening := false
	for _, line := range narrative {
		if strings.Contains(line, "Thunder rumbles") || strings.Contains(line, "Thunderussy") {
			foundThunderOpening = true
			break
		}
	}
	if !foundThunderOpening {
		t.Error("expected Thunderussy-specific opening in narrative")
	}
}

// ---------------------------------------------------------------------------
// Additional: Large field stress test
// ---------------------------------------------------------------------------

func TestLargeField(t *testing.T) {
	horses := makeHorseField(8)
	race := NewRace(horses, models.TrackGrindussy, 10000)
	result := SimulateRace(race, horses, 42)

	if result.Status != models.RaceStatusFinished {
		t.Error("large field race did not finish")
	}

	// All unique finish places.
	places := make(map[int]bool)
	for _, entry := range result.Entries {
		if places[entry.FinishPlace] {
			t.Errorf("duplicate finish place %d", entry.FinishPlace)
		}
		places[entry.FinishPlace] = true
	}

	// Should have places 1-8.
	for p := 1; p <= 8; p++ {
		if !places[p] {
			t.Errorf("missing finish place %d", p)
		}
	}
}

// ---------------------------------------------------------------------------
// Additional: Race with traits
// ---------------------------------------------------------------------------

func TestRaceWithTraits(t *testing.T) {
	horse := makeEliteHorse("Boosted", 0.9)
	horse.Traits = []models.Trait{
		{
			ID:        "trait-speed",
			Name:      "Lightning Hooves",
			Effect:    "speed_boost",
			Magnitude: 1.10,
			Rarity:    "rare",
		},
	}
	horses := []*models.Horse{horse, makeTestHorse("Normal", 0.9)}

	race := NewRace(horses, models.TrackSprintussy, 2000)
	result := SimulateRace(race, horses, 42)

	if result.Status != models.RaceStatusFinished {
		t.Error("race with traits did not finish")
	}

	// Both should have finished.
	for i, entry := range result.Entries {
		if !entry.Finished {
			t.Errorf("entry[%d] did not finish", i)
		}
	}
}

func TestWeatherImmuneTraitIgnoresWeather(t *testing.T) {
	// An immune horse should be less affected by stormy weather.
	immune := makeEliteHorse("Immune", 0.9)
	immune.Traits = []models.Trait{
		{
			ID:        "trait-immune",
			Name:      "Weather Shield",
			Effect:    "weather_immune",
			Magnitude: 1.0,
			Rarity:    "legendary",
		},
	}

	normal := makeEliteHorse("Normal", 0.9)
	normal.ID = "horse-Normal-unique" // unique ID to avoid conflict

	horses := []*models.Horse{immune, normal}

	race := NewRace(horses, models.TrackSprintussy, 2000)
	result := SimulateRaceWithWeather(race, horses, models.WeatherStormy, 42)

	// Both should finish. The immune horse should generally finish faster
	// in stormy weather, but we just verify completion here.
	for i, entry := range result.Entries {
		if !entry.Finished {
			t.Errorf("entry[%d] did not finish", i)
		}
	}
}
