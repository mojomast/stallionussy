package server

import (
	"context"
	"testing"
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
