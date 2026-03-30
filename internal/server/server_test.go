package server

import (
	"context"
	"testing"

	"github.com/mojomast/stallionussy/internal/models"
)

func TestCreateOwnedStableSeedsStarterHorses(t *testing.T) {
	s := NewServer(nil)

	stable, err := s.createOwnedStable(context.Background(), "Starter Ranch", "user-1", true)
	if err != nil {
		t.Fatalf("createOwnedStable failed: %v", err)
	}
	if stable == nil {
		t.Fatal("expected stable")
	}
	if len(stable.Horses) != starterHorseCount {
		t.Fatalf("expected %d starter horses, got %d", starterHorseCount, len(stable.Horses))
	}
	if stable.StarterGrants != 1 {
		t.Fatalf("starter grants = %d, want 1", stable.StarterGrants)
	}
	for _, horse := range stable.Horses {
		if horse.OwnerID != "user-1" {
			t.Fatalf("starter horse owner = %q, want user-1", horse.OwnerID)
		}
		if horse.Generation != 0 {
			t.Fatalf("starter horse generation = %d, want 0", horse.Generation)
		}
		if horse.Name == "" {
			t.Fatal("starter horse name should not be empty")
		}
	}
}

func TestEnsureStarterHorsesDoesNotReseedGrantedStable(t *testing.T) {
	s := NewServer(nil)

	stable, err := s.createOwnedStable(context.Background(), "Starter Ranch", "user-1", true)
	if err != nil {
		t.Fatalf("createOwnedStable failed: %v", err)
	}

	for _, horse := range append([]models.Horse(nil), stable.Horses...) {
		if err := s.stables.RemoveHorse(horse.ID); err != nil {
			t.Fatalf("RemoveHorse failed: %v", err)
		}
	}

	stable.Horses = nil
	if err := s.ensureStarterHorses(context.Background(), stable); err != nil {
		t.Fatalf("ensureStarterHorses failed: %v", err)
	}
	if len(stable.Horses) != 0 {
		t.Fatalf("expected no reseed after initial grant, got %d horses", len(stable.Horses))
	}
	if stable.StarterGrants != 1 {
		t.Fatalf("starter grants = %d, want 1", stable.StarterGrants)
	}
}

func TestGrantStarterHorsesAllowsOneRecovery(t *testing.T) {
	s := NewServer(nil)

	stable, err := s.createOwnedStable(context.Background(), "Starter Ranch", "user-1", true)
	if err != nil {
		t.Fatalf("createOwnedStable failed: %v", err)
	}

	for _, horse := range append([]models.Horse(nil), stable.Horses...) {
		if err := s.stables.RemoveHorse(horse.ID); err != nil {
			t.Fatalf("RemoveHorse failed: %v", err)
		}
	}
	stable.Horses = nil

	if err := s.grantStarterHorses(context.Background(), stable, true); err != nil {
		t.Fatalf("grantStarterHorses recovery failed: %v", err)
	}
	if len(stable.Horses) != starterHorseCount {
		t.Fatalf("expected %d recovery horses, got %d", starterHorseCount, len(stable.Horses))
	}
	if stable.StarterGrants != 2 {
		t.Fatalf("starter grants = %d, want 2", stable.StarterGrants)
	}

	for _, horse := range append([]models.Horse(nil), stable.Horses...) {
		if err := s.stables.RemoveHorse(horse.ID); err != nil {
			t.Fatalf("RemoveHorse failed: %v", err)
		}
	}
	stable.Horses = nil

	if err := s.grantStarterHorses(context.Background(), stable, true); err == nil {
		t.Fatal("expected second recovery to fail")
	}
}

func TestCreateOwnedStableRejectsSecondStableForUser(t *testing.T) {
	s := NewServer(nil)

	if _, err := s.createOwnedStable(context.Background(), "First", "user-1", true); err != nil {
		t.Fatalf("initial createOwnedStable failed: %v", err)
	}
	if _, err := s.createOwnedStable(context.Background(), "Second", "user-1", true); err == nil {
		t.Fatal("expected second stable creation to fail")
	}
}

func TestUserOwnsHorseAcrossOwnedStable(t *testing.T) {
	s := NewServer(nil)
	stable, err := s.createOwnedStable(context.Background(), "Starter Ranch", "user-1", true)
	if err != nil {
		t.Fatalf("createOwnedStable failed: %v", err)
	}
	if len(stable.Horses) == 0 {
		t.Fatal("expected starter horses")
	}

	horseID := stable.Horses[0].ID
	if !s.userOwnsHorse("user-1", horseID) {
		t.Fatal("expected owner to own starter horse")
	}
	if s.userOwnsHorse("user-2", horseID) {
		t.Fatal("unexpected ownership match for different user")
	}
}

func TestCountActiveStableHorsesIgnoresRetired(t *testing.T) {
	s := NewServer(nil)
	stable, err := s.createOwnedStable(context.Background(), "Starter Ranch", "user-1", true)
	if err != nil {
		t.Fatalf("createOwnedStable failed: %v", err)
	}
	if len(stable.Horses) < 2 {
		t.Fatalf("expected at least 2 horses, got %d", len(stable.Horses))
	}
	stable.Horses[0].Retired = true
	if got := countActiveStableHorses(stable); got != len(stable.Horses)-1 {
		t.Fatalf("countActiveStableHorses = %d, want %d", got, len(stable.Horses)-1)
	}
}

func TestLastActiveHorseWarningBlocksDestructiveAction(t *testing.T) {
	s := NewServer(nil)
	stable, err := s.createOwnedStable(context.Background(), "Starter Ranch", "user-1", true)
	if err != nil {
		t.Fatalf("createOwnedStable failed: %v", err)
	}
	if len(stable.Horses) < 2 {
		t.Fatalf("expected at least 2 horses, got %d", len(stable.Horses))
	}
	stable.Horses[1].Retired = true
	if err := lastActiveHorseWarning(&stable.Horses[0], stable, "glue"); err == nil {
		t.Fatal("expected last active horse warning")
	}
}

func TestFirstBestActiveHorseChoosesHighestELO(t *testing.T) {
	s := NewServer(nil)
	stable, err := s.createOwnedStable(context.Background(), "Starter Ranch", "user-1", true)
	if err != nil {
		t.Fatalf("createOwnedStable failed: %v", err)
	}
	if len(stable.Horses) < 2 {
		t.Fatalf("expected at least 2 horses, got %d", len(stable.Horses))
	}
	stable.Horses[0].ELO = 1200
	stable.Horses[1].ELO = 1350
	best := s.firstBestActiveHorse(stable)
	if best == nil {
		t.Fatal("expected best active horse")
	}
	if best.ID != stable.Horses[1].ID {
		t.Fatalf("best horse = %s, want %s", best.ID, stable.Horses[1].ID)
	}
}
