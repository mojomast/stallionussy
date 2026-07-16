package server

// Phase 3 integration tests — training modes must produce DISTINCT stat
// effects (Part B.3). Exercises POST /api/horses/{id}/train end-to-end.

import (
	"context"
	"math"
	"net/http"
	"testing"

	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/racussy"
)

// TestHTTP_TrainingModesProduceDistinctSpecialties trains one horse in each
// focused mode over HTTP and asserts every mode builds a different specialty.
func TestHTTP_TrainingModesProduceDistinctSpecialties(t *testing.T) {
	s := NewServer(nil)
	stable, err := s.createOwnedStable(context.Background(), "Train Ranch", "user-train", true)
	if err != nil {
		t.Fatalf("createOwnedStable: %v", err)
	}
	horseID := stable.Horses[0].ID

	modes := []struct {
		workout string
		key     string
	}{
		{"Sprint", "SPD"},
		{"Endurance", "STM"},
		{"MentalRep", "TMP"},
		{"MudRun", "SZE"},
	}

	for _, m := range modes {
		rr := postJSON(t, s, "/api/horses/"+horseID+"/train",
			map[string]any{"workoutType": m.workout}, "user-train", "trainer")
		if rr.Code != http.StatusOK && rr.Code != http.StatusCreated {
			t.Fatalf("train %s: status = %d\nbody: %s", m.workout, rr.Code, rr.Body.String())
		}
		// Clear any comedy injury so the next session isn't blocked (the
		// specialty gain for this session already landed before the roll).
		if h, err := s.stables.GetHorse(horseID); err == nil {
			h.Injury = nil
		}
	}

	horse, err := s.stables.GetHorse(horseID)
	if err != nil {
		t.Fatalf("get horse: %v", err)
	}
	if horse.TrainingSpecialty == nil {
		t.Fatal("training left no specialties — modes are placeholder-identical")
	}
	for _, m := range modes {
		got := horse.SpecialtyOf(m.key)
		if math.Abs(got-0.004) > 1e-9 {
			t.Fatalf("%s specialty after one %s session = %v, want 0.004", m.key, m.workout, got)
		}
	}
	if len(horse.TrainingSpecialty) != 4 {
		t.Fatalf("expected 4 distinct specialties, got %d: %v", len(horse.TrainingSpecialty), horse.TrainingSpecialty)
	}
}

// TestTrainingSpecialtyAffectsRaceSpeed: the specialty must actually reach
// the race simulator — a Sprint-trained horse is faster than its untrained
// genetic twin.
func TestTrainingSpecialtyAffectsRaceSpeed(t *testing.T) {
	genome := models.Genome{}
	for _, gt := range []models.GeneType{models.GeneSPD, models.GeneSTM, models.GeneTMP, models.GeneSZE, models.GeneREC, models.GeneINT, models.GeneMUT} {
		genome[gt] = models.Gene{Type: gt, AlleleA: models.AlleleA, AlleleB: models.AlleleB}
	}
	untrained := &models.Horse{ID: "h1", Genome: genome, CurrentFitness: 0.8, FitnessCeiling: 1.0}
	trained := &models.Horse{ID: "h2", Genome: genome, CurrentFitness: 0.8, FitnessCeiling: 1.0,
		TrainingSpecialty: map[string]float64{"SPD": models.TrainingSpecialtyCap}}

	base := racussy.CalcBaseSpeed(untrained, models.TrackSprintussy)
	boosted := racussy.CalcBaseSpeed(trained, models.TrackSprintussy)
	if boosted <= base {
		t.Fatalf("SPD specialty did not increase base speed: %v <= %v", boosted, base)
	}
}

// TestTrainingSpecialtyIsCapped: grinding one mode forever cannot exceed the
// cap — no runaway stat inflation from training spam.
func TestTrainingSpecialtyIsCapped(t *testing.T) {
	s := NewServer(nil)
	stable, err := s.createOwnedStable(context.Background(), "Cap Ranch", "user-cap", true)
	if err != nil {
		t.Fatalf("createOwnedStable: %v", err)
	}
	horse, _ := s.stables.GetHorse(stable.Horses[0].ID)

	for i := 0; i < 50; i++ {
		horse.Fatigue = 0
		horse.Injury = nil
		if _, err := s.trainer.Train(horse, models.WorkoutSprint); err != nil {
			t.Fatalf("train %d: %v", i, err)
		}
	}
	if got := horse.SpecialtyOf("SPD"); got > models.TrainingSpecialtyCap+1e-9 {
		t.Fatalf("SPD specialty %v exceeds cap %v", got, models.TrainingSpecialtyCap)
	}
}
