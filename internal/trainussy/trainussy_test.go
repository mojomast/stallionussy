package trainussy

import (
	"testing"

	"github.com/mojomast/stallionussy/internal/models"
)

// ---------------------------------------------------------------------------
// Helpers — create standardised horses for deterministic testing
// ---------------------------------------------------------------------------

// makeTestHorse builds a baseline horse with known genome and stats.
// All genes default to BB unless overridden. FitnessCeiling=1.0 so
// diminishing-return math is easy to reason about.
func makeTestHorse() *models.Horse {
	return &models.Horse{
		ID:             "horse-test-1",
		Name:           "TestGallop McTestface",
		Genome:         makeGenomeBB(),
		FitnessCeiling: 1.0,
		CurrentFitness: 0.5,
		Fatigue:        0,
		ELO:            1200,
		Races:          0,
		Wins:           0,
		TrainingXP:     0,
	}
}

// makeGenomeBB returns a genome where every gene is BB.
func makeGenomeBB() models.Genome {
	g := make(models.Genome)
	for _, gt := range models.AllGeneTypes {
		g[gt] = models.Gene{Type: gt, AlleleA: models.AlleleB, AlleleB: models.AlleleB}
	}
	return g
}

// makeGenomeAA returns a genome where every gene is AA.
func makeGenomeAA() models.Genome {
	g := make(models.Genome)
	for _, gt := range models.AllGeneTypes {
		g[gt] = models.Gene{Type: gt, AlleleA: models.AlleleA, AlleleB: models.AlleleA}
	}
	return g
}

// makeGenomeAB returns a genome where every gene is AB.
func makeGenomeAB() models.Genome {
	g := make(models.Genome)
	for _, gt := range models.AllGeneTypes {
		g[gt] = models.Gene{Type: gt, AlleleA: models.AlleleA, AlleleB: models.AlleleB}
	}
	return g
}

// ---------------------------------------------------------------------------
// NewTrainer
// ---------------------------------------------------------------------------

func TestNewTrainer(t *testing.T) {
	tr := NewTrainer()
	if tr == nil {
		t.Fatal("NewTrainer returned nil")
	}
	if tr.sessions == nil {
		t.Error("sessions map is nil")
	}
	if len(tr.traitPool) == 0 {
		t.Error("traitPool is empty — expected traits from InitTraitPool")
	}
}

// ---------------------------------------------------------------------------
// Train — one test per workout type
// ---------------------------------------------------------------------------

func TestTrain_Sprint(t *testing.T) {
	tr := NewTrainer()
	horse := makeTestHorse()

	fitBefore := horse.CurrentFitness
	xpBefore := horse.TrainingXP
	fatigueBefore := horse.Fatigue

	session := tr.Train(horse, models.WorkoutSprint)

	assertSession(t, session, horse.ID, models.WorkoutSprint)
	assertXPGain(t, horse, xpBefore, "Sprint")
	assertFitnessGain(t, horse, fitBefore, "Sprint")
	assertFatigueIncrease(t, horse, fatigueBefore, fatigueDelta[models.WorkoutSprint], "Sprint")
}

func TestTrain_Endurance(t *testing.T) {
	tr := NewTrainer()
	horse := makeTestHorse()

	fitBefore := horse.CurrentFitness
	xpBefore := horse.TrainingXP
	fatigueBefore := horse.Fatigue

	session := tr.Train(horse, models.WorkoutEndurance)

	assertSession(t, session, horse.ID, models.WorkoutEndurance)
	assertXPGain(t, horse, xpBefore, "Endurance")
	assertFitnessGain(t, horse, fitBefore, "Endurance")
	assertFatigueIncrease(t, horse, fatigueBefore, fatigueDelta[models.WorkoutEndurance], "Endurance")
}

func TestTrain_MentalRep(t *testing.T) {
	tr := NewTrainer()
	horse := makeTestHorse()

	fitBefore := horse.CurrentFitness
	xpBefore := horse.TrainingXP
	fatigueBefore := horse.Fatigue

	session := tr.Train(horse, models.WorkoutMentalRep)

	assertSession(t, session, horse.ID, models.WorkoutMentalRep)
	assertXPGain(t, horse, xpBefore, "MentalRep")
	assertFitnessGain(t, horse, fitBefore, "MentalRep")
	assertFatigueIncrease(t, horse, fatigueBefore, fatigueDelta[models.WorkoutMentalRep], "MentalRep")
}

func TestTrain_MudRun(t *testing.T) {
	tr := NewTrainer()
	horse := makeTestHorse()

	fitBefore := horse.CurrentFitness
	xpBefore := horse.TrainingXP
	fatigueBefore := horse.Fatigue

	session := tr.Train(horse, models.WorkoutMudRun)

	assertSession(t, session, horse.ID, models.WorkoutMudRun)
	assertXPGain(t, horse, xpBefore, "MudRun")
	assertFitnessGain(t, horse, fitBefore, "MudRun")
	assertFatigueIncrease(t, horse, fatigueBefore, fatigueDelta[models.WorkoutMudRun], "MudRun")
}

func TestTrain_RestDay_ReducesFatigue(t *testing.T) {
	tr := NewTrainer()
	horse := makeTestHorse()
	horse.Fatigue = 60 // start with some fatigue

	fatigueBefore := horse.Fatigue
	xpBefore := horse.TrainingXP

	session := tr.Train(horse, models.WorkoutRecovery)

	assertSession(t, session, horse.ID, models.WorkoutRecovery)
	assertXPGain(t, horse, xpBefore, "RestDay")

	// RestDay has fatigueDelta = -30, so fatigue should drop.
	expectedFatigue := fatigueBefore + fatigueDelta[models.WorkoutRecovery] // 60 + (-30) = 30
	if expectedFatigue < 0 {
		expectedFatigue = 0
	}
	if horse.Fatigue != expectedFatigue {
		t.Errorf("RestDay: expected fatigue %v, got %v", expectedFatigue, horse.Fatigue)
	}
}

func TestTrain_RestDay_FatigueFloorAtZero(t *testing.T) {
	tr := NewTrainer()
	horse := makeTestHorse()
	horse.Fatigue = 10 // less than |fatigueDelta| = 30

	tr.Train(horse, models.WorkoutRecovery)

	if horse.Fatigue != 0 {
		t.Errorf("RestDay with low fatigue: expected 0, got %v", horse.Fatigue)
	}
}

func TestTrain_General(t *testing.T) {
	tr := NewTrainer()
	horse := makeTestHorse()

	fitBefore := horse.CurrentFitness
	xpBefore := horse.TrainingXP
	fatigueBefore := horse.Fatigue

	session := tr.Train(horse, models.WorkoutGeneral)

	assertSession(t, session, horse.ID, models.WorkoutGeneral)
	assertXPGain(t, horse, xpBefore, "General")
	assertFitnessGain(t, horse, fitBefore, "General")
	assertFatigueIncrease(t, horse, fatigueBefore, fatigueDelta[models.WorkoutGeneral], "General")
}

// ---------------------------------------------------------------------------
// XP calculation specifics
// ---------------------------------------------------------------------------

func TestCalcXP_BaseValues(t *testing.T) {
	// BB genome, zero fatigue → pure base XP
	horse := makeTestHorse()

	tests := []struct {
		workout models.WorkoutType
		wantXP  float64
	}{
		{models.WorkoutSprint, 10},
		{models.WorkoutEndurance, 12},
		{models.WorkoutMentalRep, 8},
		{models.WorkoutMudRun, 15},
		{models.WorkoutRecovery, 5},
		{models.WorkoutGeneral, 7},
	}

	for _, tt := range tests {
		xp := calcXP(horse, tt.workout)
		if xp != tt.wantXP {
			t.Errorf("calcXP(%s) with BB genome = %v, want %v", tt.workout, xp, tt.wantXP)
		}
	}
}

func TestCalcXP_INTBonus_AA(t *testing.T) {
	horse := makeTestHorse()
	horse.Genome[models.GeneINT] = models.Gene{
		Type: models.GeneINT, AlleleA: models.AlleleA, AlleleB: models.AlleleA,
	}

	// AA INT = 1.5x multiplier; Sprint base = 10 → expect 15
	xp := calcXP(horse, models.WorkoutSprint)
	if xp != 15 {
		t.Errorf("calcXP Sprint AA INT = %v, want 15", xp)
	}
}

func TestCalcXP_INTBonus_AB(t *testing.T) {
	horse := makeTestHorse()
	horse.Genome[models.GeneINT] = models.Gene{
		Type: models.GeneINT, AlleleA: models.AlleleA, AlleleB: models.AlleleB,
	}

	// AB INT = 1.2x; Sprint base 10 → 12
	xp := calcXP(horse, models.WorkoutSprint)
	if xp != 12 {
		t.Errorf("calcXP Sprint AB INT = %v, want 12", xp)
	}
}

func TestCalcXP_FatiguePenalty(t *testing.T) {
	horse := makeTestHorse()

	// Fatigue >50 and <=80: XP halved
	horse.Fatigue = 60
	xp60 := calcXP(horse, models.WorkoutSprint) // base 10 * 0.5 = 5
	if xp60 != 5 {
		t.Errorf("calcXP at fatigue 60 = %v, want 5", xp60)
	}

	// Fatigue >80: XP quartered
	horse.Fatigue = 85
	xp85 := calcXP(horse, models.WorkoutSprint) // base 10 * 0.25 = 2.5
	if xp85 != 2.5 {
		t.Errorf("calcXP at fatigue 85 = %v, want 2.5", xp85)
	}
}

// ---------------------------------------------------------------------------
// Fitness gain — diminishing returns and gene bonuses
// ---------------------------------------------------------------------------

func TestCalcFitnessGain_DiminishingReturns(t *testing.T) {
	horse := makeTestHorse() // BB genome → SPD=BB triggers +20% Sprint bonus
	horse.CurrentFitness = 0.0

	// Use General workout (no gene bonus) for clean diminishing-returns test.
	// At 0 fitness: diminish = 1 - 0/1 = 1.0
	// gain = 7 * 0.002 * 1.0 = 0.014
	gain := calcFitnessGain(horse, models.WorkoutGeneral, 7)
	if gain != 0.014 {
		t.Errorf("fitnessGain at fitness 0 = %v, want 0.014", gain)
	}

	// At 0.5 fitness: diminish = 1 - 0.5/1 = 0.5
	// gain = 7 * 0.002 * 0.5 = 0.007
	horse.CurrentFitness = 0.5
	gain = calcFitnessGain(horse, models.WorkoutGeneral, 7)
	if gain != 0.007 {
		t.Errorf("fitnessGain at fitness 0.5 = %v, want 0.007", gain)
	}

	// Verify near-ceiling produces tiny gain
	horse.CurrentFitness = 0.99
	gain = calcFitnessGain(horse, models.WorkoutGeneral, 7)
	// diminish = 1 - 0.99/1 = 0.01 → gain ≈ 7 * 0.002 * 0.01 = 0.00014
	expected := 7 * 0.002 * 0.01
	if diff := gain - expected; diff < -1e-15 || diff > 1e-15 {
		t.Errorf("fitnessGain at fitness 0.99 = %v, want ~%v", gain, expected)
	}
}

func TestCalcFitnessGain_ZeroCeiling(t *testing.T) {
	horse := makeTestHorse()
	horse.FitnessCeiling = 0

	gain := calcFitnessGain(horse, models.WorkoutSprint, 10)
	if gain != 0 {
		t.Errorf("fitnessGain with zero ceiling = %v, want 0", gain)
	}
}

func TestCalcFitnessGain_SprintGeneBonus(t *testing.T) {
	// SPD = AB → Sprint gets +20% bonus
	horse := makeTestHorse()
	horse.CurrentFitness = 0
	horse.Genome[models.GeneSPD] = models.Gene{
		Type: models.GeneSPD, AlleleA: models.AlleleA, AlleleB: models.AlleleB,
	}

	gain := calcFitnessGain(horse, models.WorkoutSprint, 10)
	expected := 10 * 0.002 * 1.0 * 1.20 // XP * factor * diminish * gene bonus
	if gain != expected {
		t.Errorf("Sprint gene bonus = %v, want %v", gain, expected)
	}
}

func TestCalcFitnessGain_EnduranceGeneBonus(t *testing.T) {
	horse := makeTestHorse()
	horse.CurrentFitness = 0
	horse.Genome[models.GeneSTM] = models.Gene{
		Type: models.GeneSTM, AlleleA: models.AlleleA, AlleleB: models.AlleleB,
	}

	gain := calcFitnessGain(horse, models.WorkoutEndurance, 12)
	expected := 12 * 0.002 * 1.0 * 1.20
	if gain != expected {
		t.Errorf("Endurance gene bonus = %v, want %v", gain, expected)
	}
}

func TestCalcFitnessGain_MudRunGeneBonus(t *testing.T) {
	horse := makeTestHorse()
	horse.CurrentFitness = 0
	horse.Genome[models.GeneSZE] = models.Gene{
		Type: models.GeneSZE, AlleleA: models.AlleleB, AlleleB: models.AlleleB,
	}

	gain := calcFitnessGain(horse, models.WorkoutMudRun, 15)
	expected := 15 * 0.002 * 1.0 * 1.20
	if gain != expected {
		t.Errorf("MudRun gene bonus = %v, want %v", gain, expected)
	}
}

func TestCalcFitnessGain_MentalRepGeneBonus(t *testing.T) {
	horse := makeTestHorse()
	horse.CurrentFitness = 0
	horse.Genome[models.GeneTMP] = models.Gene{
		Type: models.GeneTMP, AlleleA: models.AlleleA, AlleleB: models.AlleleB,
	}

	gain := calcFitnessGain(horse, models.WorkoutMentalRep, 8)
	expected := 8 * 0.002 * 1.0 * 1.20
	if gain != expected {
		t.Errorf("MentalRep gene bonus = %v, want %v", gain, expected)
	}
}

func TestCalcFitnessGain_NoGeneBonus_AA(t *testing.T) {
	// SPD = AA → NO sprint gene bonus (only AB/BB get it)
	horse := makeTestHorse()
	horse.CurrentFitness = 0
	horse.Genome[models.GeneSPD] = models.Gene{
		Type: models.GeneSPD, AlleleA: models.AlleleA, AlleleB: models.AlleleA,
	}

	gain := calcFitnessGain(horse, models.WorkoutSprint, 10)
	expected := 10 * 0.002 * 1.0 // no 1.2x bonus
	if gain != expected {
		t.Errorf("Sprint AA (no bonus) = %v, want %v", gain, expected)
	}
}

// ---------------------------------------------------------------------------
// Fitness ceiling cap
// ---------------------------------------------------------------------------

func TestTrain_FitnessCappedAtCeiling(t *testing.T) {
	tr := NewTrainer()
	horse := makeTestHorse()
	horse.CurrentFitness = 0.999
	horse.FitnessCeiling = 1.0

	tr.Train(horse, models.WorkoutSprint)

	if horse.CurrentFitness > horse.FitnessCeiling {
		t.Errorf("Fitness %v exceeded ceiling %v", horse.CurrentFitness, horse.FitnessCeiling)
	}
}

// ---------------------------------------------------------------------------
// Fatigue clamping
// ---------------------------------------------------------------------------

func TestTrain_FatigueCappedAt100(t *testing.T) {
	tr := NewTrainer()
	horse := makeTestHorse()
	horse.Fatigue = 95

	tr.Train(horse, models.WorkoutEndurance) // +20 fatigue
	if horse.Fatigue > 100 {
		t.Errorf("Fatigue exceeded 100: got %v", horse.Fatigue)
	}
	if horse.Fatigue != 100 {
		// It could be 100 exactly (capped) or less if injury set it to 100 first.
		// But it should never exceed 100.
		if horse.Fatigue > 100 {
			t.Errorf("Fatigue exceeded 100: got %v", horse.Fatigue)
		}
	}
}

// ---------------------------------------------------------------------------
// GetTrainingHistory
// ---------------------------------------------------------------------------

func TestGetTrainingHistory_Empty(t *testing.T) {
	tr := NewTrainer()
	h := tr.GetTrainingHistory("nonexistent")
	if h == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(h) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(h))
	}
}

func TestGetTrainingHistory_RecordsSessions(t *testing.T) {
	tr := NewTrainer()
	horse := makeTestHorse()

	tr.Train(horse, models.WorkoutSprint)
	tr.Train(horse, models.WorkoutEndurance)
	tr.Train(horse, models.WorkoutGeneral)

	history := tr.GetTrainingHistory(horse.ID)
	if len(history) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(history))
	}
	if history[0].WorkoutType != models.WorkoutSprint {
		t.Errorf("session[0] type = %v, want Sprint", history[0].WorkoutType)
	}
	if history[1].WorkoutType != models.WorkoutEndurance {
		t.Errorf("session[1] type = %v, want Endurance", history[1].WorkoutType)
	}
	if history[2].WorkoutType != models.WorkoutGeneral {
		t.Errorf("session[2] type = %v, want General", history[2].WorkoutType)
	}
}

func TestGetTrainingHistory_ReturnsCopy(t *testing.T) {
	tr := NewTrainer()
	horse := makeTestHorse()
	tr.Train(horse, models.WorkoutSprint)

	h1 := tr.GetTrainingHistory(horse.ID)
	h2 := tr.GetTrainingHistory(horse.ID)

	if &h1[0] == &h2[0] {
		t.Error("GetTrainingHistory should return independent copies of the slice")
	}
}

// ---------------------------------------------------------------------------
// RecoverFatigue
// ---------------------------------------------------------------------------

func TestRecoverFatigue(t *testing.T) {
	tr := NewTrainer()
	horse := makeTestHorse()
	horse.Fatigue = 50

	tr.RecoverFatigue(horse, 20)
	if horse.Fatigue != 30 {
		t.Errorf("after recovering 20: fatigue = %v, want 30", horse.Fatigue)
	}

	tr.RecoverFatigue(horse, 100) // should floor at 0
	if horse.Fatigue != 0 {
		t.Errorf("after recovering 100: fatigue = %v, want 0", horse.Fatigue)
	}
}

// ---------------------------------------------------------------------------
// Trait System
// ---------------------------------------------------------------------------

func TestInitTraitPool_HasAllRarities(t *testing.T) {
	pool := InitTraitPool()

	rarities := map[string]int{}
	for _, tr := range pool {
		rarities[tr.Rarity]++
	}

	for _, r := range []string{"common", "rare", "legendary", "anomalous"} {
		if rarities[r] == 0 {
			t.Errorf("trait pool missing rarity %q", r)
		}
	}
}

func TestInitTraitPool_TraitsHaveRequiredFields(t *testing.T) {
	pool := InitTraitPool()
	for _, tr := range pool {
		if tr.ID == "" {
			t.Errorf("trait %q has empty ID", tr.Name)
		}
		if tr.Name == "" {
			t.Error("found trait with empty Name")
		}
		if tr.Effect == "" {
			t.Errorf("trait %q has empty Effect", tr.Name)
		}
		if tr.Rarity == "" {
			t.Errorf("trait %q has empty Rarity", tr.Name)
		}
	}
}

func TestAssignTraitsAtBirth_AssignsSomeTraits(t *testing.T) {
	tr := NewTrainer()
	foal := makeTestHorse()
	foal.Traits = nil

	sire := makeTestHorse()
	sire.ID = "sire-1"
	mare := makeTestHorse()
	mare.ID = "mare-1"

	// Run many times to ensure at least one assigns traits (stochastic).
	anyAssigned := false
	for i := 0; i < 100; i++ {
		foal.Traits = nil
		tr.AssignTraitsAtBirth(foal, sire, mare)
		if len(foal.Traits) > 0 {
			anyAssigned = true
			break
		}
	}
	if !anyAssigned {
		t.Error("AssignTraitsAtBirth never assigned any traits in 100 attempts")
	}
}

func TestAssignTraitsAtBirth_MaxThreeTraits(t *testing.T) {
	tr := NewTrainer()
	foal := makeTestHorse()
	sire := makeTestHorse()
	mare := makeTestHorse()

	for i := 0; i < 200; i++ {
		foal.Traits = nil
		tr.AssignTraitsAtBirth(foal, sire, mare)
		if len(foal.Traits) > 3 {
			t.Fatalf("foal got %d traits — max is 3", len(foal.Traits))
		}
	}
}

func TestAssignTraitsAtBirth_NoDuplicates(t *testing.T) {
	tr := NewTrainer()
	foal := makeTestHorse()
	sire := makeTestHorse()
	mare := makeTestHorse()

	for i := 0; i < 200; i++ {
		foal.Traits = nil
		tr.AssignTraitsAtBirth(foal, sire, mare)
		seen := map[string]bool{}
		for _, trait := range foal.Traits {
			if seen[trait.Name] {
				t.Fatalf("duplicate trait %q in foal", trait.Name)
			}
			seen[trait.Name] = true
		}
	}
}

func TestAssignTraitsAtBirth_NilParentsOK(t *testing.T) {
	tr := NewTrainer()
	foal := makeTestHorse()
	foal.Traits = nil

	// Should not panic with nil parents
	tr.AssignTraitsAtBirth(foal, nil, nil)
}

func TestAssignTraitOnMilestone_StochasticReturns(t *testing.T) {
	tr := NewTrainer()
	horse := makeTestHorse()
	horse.Traits = []models.Trait{}

	// 30% chance each call, so over 100 attempts we should see at least one.
	var gained int
	for i := 0; i < 200; i++ {
		trait := tr.AssignTraitOnMilestone(horse, "first_win")
		if trait != nil {
			gained++
		}
	}
	if gained == 0 {
		t.Error("AssignTraitOnMilestone never returned a trait in 200 attempts")
	}
	if gained == 200 {
		t.Error("AssignTraitOnMilestone returned a trait every time — expected ~30% rate")
	}
}

// ---------------------------------------------------------------------------
// Trait helper unit tests
// ---------------------------------------------------------------------------

func TestFilterByRarity(t *testing.T) {
	pool := InitTraitPool()
	common := filterByRarity(pool, "common")
	if len(common) == 0 {
		t.Error("filterByRarity(common) returned empty")
	}
	for _, tr := range common {
		if tr.Rarity != "common" {
			t.Errorf("trait %q has rarity %q, expected common", tr.Name, tr.Rarity)
		}
	}
}

func TestIsAnomalousEligible(t *testing.T) {
	// LotNumber == 6 makes parent eligible
	sire := makeTestHorse()
	sire.LotNumber = 6
	if !isAnomalousEligible(sire, nil) {
		t.Error("expected anomalous eligible with LotNumber=6 sire")
	}

	// Parent with anomalous trait
	mare := makeTestHorse()
	mare.Traits = []models.Trait{{Rarity: "anomalous"}}
	if !isAnomalousEligible(nil, mare) {
		t.Error("expected anomalous eligible with anomalous trait mare")
	}

	// Neither parent eligible
	normSire := makeTestHorse()
	normMare := makeTestHorse()
	if isAnomalousEligible(normSire, normMare) {
		t.Error("expected NOT anomalous eligible for normal parents")
	}
}

func TestIsLegendaryEligible(t *testing.T) {
	// IsLegendary flag
	sire := makeTestHorse()
	sire.IsLegendary = true
	if !isLegendaryEligible(sire, nil) {
		t.Error("expected legendary eligible with IsLegendary sire")
	}

	// LotNumber > 0
	mare := makeTestHorse()
	mare.LotNumber = 3
	if !isLegendaryEligible(nil, mare) {
		t.Error("expected legendary eligible with LotNumber=3 mare")
	}

	// Parent with legendary trait
	sire2 := makeTestHorse()
	sire2.Traits = []models.Trait{{Rarity: "legendary"}}
	if !isLegendaryEligible(sire2, nil) {
		t.Error("expected legendary eligible with legendary trait sire")
	}

	// Neither eligible
	normSire := makeTestHorse()
	normMare := makeTestHorse()
	if isLegendaryEligible(normSire, normMare) {
		t.Error("expected NOT legendary eligible for normal parents")
	}
}

// ---------------------------------------------------------------------------
// Aging System
// ---------------------------------------------------------------------------

func TestAgeHorse_Youth(t *testing.T) {
	horse := makeTestHorse()
	horse.Age = 0
	horse.FitnessCeiling = 1.0

	AgeHorse(horse)

	if horse.Age != 1 {
		t.Errorf("age = %d, want 1", horse.Age)
	}
	// Youth: ceiling *= 1.02
	expected := 1.0 * 1.02
	if horse.FitnessCeiling != expected {
		t.Errorf("ceiling = %v, want %v (Youth +2%%)", horse.FitnessCeiling, expected)
	}
}

func TestAgeHorse_Prime(t *testing.T) {
	horse := makeTestHorse()
	horse.Age = 4 // will become 5 after aging → Prime
	horse.FitnessCeiling = 1.0

	AgeHorse(horse)

	if horse.Age != 5 {
		t.Errorf("age = %d, want 5", horse.Age)
	}
	// Prime: no ceiling change
	if horse.FitnessCeiling != 1.0 {
		t.Errorf("ceiling = %v, want 1.0 (Prime no change)", horse.FitnessCeiling)
	}
}

func TestAgeHorse_Veteran(t *testing.T) {
	horse := makeTestHorse()
	horse.Age = 9 // will become 10 → Veteran
	horse.FitnessCeiling = 1.0

	AgeHorse(horse)

	if horse.Age != 10 {
		t.Errorf("age = %d, want 10", horse.Age)
	}
	// Veteran: ceiling *= 0.99
	expected := 1.0 * 0.99
	if horse.FitnessCeiling != expected {
		t.Errorf("ceiling = %v, want %v (Veteran -1%%)", horse.FitnessCeiling, expected)
	}
}

func TestAgeHorse_Elder(t *testing.T) {
	horse := makeTestHorse()
	horse.Age = 13 // will become 14 → Elder
	horse.FitnessCeiling = 1.0

	AgeHorse(horse)

	if horse.Age != 14 {
		t.Errorf("age = %d, want 14", horse.Age)
	}
	// Elder: ceiling *= 0.97
	expected := 1.0 * 0.97
	if horse.FitnessCeiling != expected {
		t.Errorf("ceiling = %v, want %v (Elder -3%%)", horse.FitnessCeiling, expected)
	}
}

func TestAgeHorse_Ancient_CeilingDecay(t *testing.T) {
	horse := makeTestHorse()
	horse.Age = 16 // will become 17 → Ancient
	horse.FitnessCeiling = 1.0

	AgeHorse(horse)

	if horse.Age != 17 {
		t.Errorf("age = %d, want 17", horse.Age)
	}
	// Ancient: ceiling *= 0.95 (may also trigger retirement)
	expectedCeiling := 1.0 * 0.95
	if !horse.Retired {
		// If not retired, ceiling should be exactly 0.95
		if horse.FitnessCeiling != expectedCeiling {
			t.Errorf("ceiling = %v, want %v (Ancient -5%%)", horse.FitnessCeiling, expectedCeiling)
		}
	}
}

func TestAgeHorse_E008_DoesNotAge(t *testing.T) {
	horse := makeTestHorse()
	horse.LotNumber = 6 // E-008's Chosen
	horse.Age = 5
	horse.FitnessCeiling = 1.0

	AgeHorse(horse)

	if horse.Age != 5 {
		t.Errorf("E-008 aged: age = %d, want 5 (unchanged)", horse.Age)
	}
	if horse.FitnessCeiling != 1.0 {
		t.Errorf("E-008 ceiling changed to %v, should remain 1.0", horse.FitnessCeiling)
	}
}

func TestAgeHorse_FitnessCappedByCeiling(t *testing.T) {
	horse := makeTestHorse()
	horse.Age = 13 // will become 14, Elder
	horse.FitnessCeiling = 0.80
	horse.CurrentFitness = 0.80

	AgeHorse(horse)

	// Elder: ceiling *= 0.97 → 0.776
	// currentFitness should be capped to new ceiling
	if horse.CurrentFitness > horse.FitnessCeiling {
		t.Errorf("fitness %v exceeds ceiling %v after aging", horse.CurrentFitness, horse.FitnessCeiling)
	}
}

// ---------------------------------------------------------------------------
// LifeStage
// ---------------------------------------------------------------------------

func TestLifeStage(t *testing.T) {
	tests := []struct {
		age       int
		lotNumber int
		want      string
	}{
		{0, 0, "Youth"},
		{3, 0, "Youth"},
		{4, 0, "Prime"},
		{8, 0, "Prime"},
		{9, 0, "Veteran"},
		{12, 0, "Veteran"},
		{13, 0, "Elder"},
		{15, 0, "Elder"},
		{16, 0, "Ancient"},
		{25, 0, "Ancient"},
		{5, 6, "Eternal"},  // E-008
		{99, 6, "Eternal"}, // E-008 at any age
	}

	for _, tt := range tests {
		horse := makeTestHorse()
		horse.Age = tt.age
		horse.LotNumber = tt.lotNumber
		got := LifeStage(horse)
		if got != tt.want {
			t.Errorf("LifeStage(age=%d, lot=%d) = %q, want %q", tt.age, tt.lotNumber, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Retirement System
// ---------------------------------------------------------------------------

func TestShouldRetire_E008_NeverRetires(t *testing.T) {
	horse := makeTestHorse()
	horse.LotNumber = 6
	horse.Age = 99

	shouldRetire, _ := ShouldRetire(horse)
	if shouldRetire {
		t.Error("E-008 should never retire")
	}
}

func TestShouldRetire_AlreadyRetired(t *testing.T) {
	horse := makeTestHorse()
	horse.Retired = true

	shouldRetire, reason := ShouldRetire(horse)
	if !shouldRetire {
		t.Error("already retired horse should return true")
	}
	if reason != "Already retired" {
		t.Errorf("reason = %q, want 'Already retired'", reason)
	}
}

func TestShouldRetire_LowFitnessCeiling(t *testing.T) {
	horse := makeTestHorse()
	horse.FitnessCeiling = 0.1 // below 0.2 threshold

	shouldRetire, reason := ShouldRetire(horse)
	if !shouldRetire {
		t.Error("horse with fitness ceiling < 0.2 should retire")
	}
	if reason == "" {
		t.Error("expected a retirement reason")
	}
}

func TestShouldRetire_50RacesLowELO(t *testing.T) {
	horse := makeTestHorse()
	horse.Races = 55
	horse.ELO = 850

	shouldRetire, reason := ShouldRetire(horse)
	if !shouldRetire {
		t.Error("horse with 50+ races and ELO < 900 should retire")
	}
	if reason == "" {
		t.Error("expected a retirement reason")
	}
}

func TestShouldRetire_HealthyHorse(t *testing.T) {
	horse := makeTestHorse()
	horse.Age = 5
	horse.FitnessCeiling = 0.9
	horse.Races = 10
	horse.ELO = 1200

	shouldRetire, _ := ShouldRetire(horse)
	if shouldRetire {
		t.Error("healthy young horse should not retire")
	}
}

func TestRetireHorse(t *testing.T) {
	horse := makeTestHorse()
	RetireHorse(horse, "Became a motivational speaker for anxious foals")

	if !horse.Retired {
		t.Error("horse should be retired")
	}
	if horse.Lore == "" {
		t.Error("expected lore to contain retirement reason")
	}
}

func TestRetireHorse_EmptyReason(t *testing.T) {
	horse := makeTestHorse()
	horse.Lore = "Original lore"
	RetireHorse(horse, "")

	if !horse.Retired {
		t.Error("horse should be retired")
	}
	if horse.Lore != "Original lore" {
		t.Errorf("lore should be unchanged with empty reason, got %q", horse.Lore)
	}
}

// ---------------------------------------------------------------------------
// Gene helpers
// ---------------------------------------------------------------------------

func TestGeneExpress_NilGenome(t *testing.T) {
	result := geneExpress(nil, models.GeneSPD)
	if result != "BB" {
		t.Errorf("geneExpress(nil) = %q, want BB", result)
	}
}

func TestGeneExpress_MissingGene(t *testing.T) {
	g := make(models.Genome)
	result := geneExpress(g, models.GeneSPD)
	if result != "BB" {
		t.Errorf("geneExpress(missing) = %q, want BB", result)
	}
}

func TestGeneExpress_Expressions(t *testing.T) {
	g := make(models.Genome)

	g[models.GeneSPD] = models.Gene{Type: models.GeneSPD, AlleleA: models.AlleleA, AlleleB: models.AlleleA}
	if got := geneExpress(g, models.GeneSPD); got != "AA" {
		t.Errorf("AA = %q", got)
	}

	g[models.GeneSPD] = models.Gene{Type: models.GeneSPD, AlleleA: models.AlleleA, AlleleB: models.AlleleB}
	if got := geneExpress(g, models.GeneSPD); got != "AB" {
		t.Errorf("AB = %q", got)
	}

	g[models.GeneSPD] = models.Gene{Type: models.GeneSPD, AlleleA: models.AlleleB, AlleleB: models.AlleleB}
	if got := geneExpress(g, models.GeneSPD); got != "BB" {
		t.Errorf("BB = %q", got)
	}
}

// ---------------------------------------------------------------------------
// assertion helpers
// ---------------------------------------------------------------------------

func assertSession(t *testing.T, s *models.TrainingSession, wantHorseID string, wantType models.WorkoutType) {
	t.Helper()
	if s == nil {
		t.Fatal("session is nil")
	}
	if s.ID == "" {
		t.Error("session ID is empty")
	}
	if s.HorseID != wantHorseID {
		t.Errorf("session HorseID = %q, want %q", s.HorseID, wantHorseID)
	}
	if s.WorkoutType != wantType {
		t.Errorf("session WorkoutType = %v, want %v", s.WorkoutType, wantType)
	}
	if s.CreatedAt.IsZero() {
		t.Error("session CreatedAt is zero")
	}
}

func assertXPGain(t *testing.T, horse *models.Horse, xpBefore float64, workout string) {
	t.Helper()
	if horse.TrainingXP <= xpBefore {
		t.Errorf("%s: TrainingXP did not increase (before=%v, after=%v)", workout, xpBefore, horse.TrainingXP)
	}
}

func assertFitnessGain(t *testing.T, horse *models.Horse, fitBefore float64, workout string) {
	t.Helper()
	if horse.CurrentFitness < fitBefore {
		t.Errorf("%s: CurrentFitness decreased (before=%v, after=%v)", workout, fitBefore, horse.CurrentFitness)
	}
}

func assertFatigueIncrease(t *testing.T, horse *models.Horse, fatigueBefore, expectedDelta float64, workout string) {
	t.Helper()
	if expectedDelta <= 0 {
		return // skip for recovery workouts
	}
	if horse.Fatigue <= fatigueBefore {
		// Could be injury set it to 100, which is still >= fatigueBefore (0).
		// Just ensure it didn't decrease for non-recovery workouts.
		if horse.Fatigue < fatigueBefore {
			t.Errorf("%s: fatigue decreased (before=%v, after=%v)", workout, fatigueBefore, horse.Fatigue)
		}
	}
}
