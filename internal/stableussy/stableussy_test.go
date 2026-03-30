package stableussy

import (
	"testing"

	"github.com/google/uuid"
	"github.com/mojomast/stallionussy/internal/models"
)

// ---------------------------------------------------------------------------
// Helper: create a test horse with sensible defaults
// ---------------------------------------------------------------------------

func makeTestHorse(name string, elo float64) *models.Horse {
	return &models.Horse{
		ID:   uuid.New().String(),
		Name: name,
		ELO:  elo,
	}
}

// ---------------------------------------------------------------------------
// NewStableManager
// ---------------------------------------------------------------------------

func TestNewStableManager(t *testing.T) {
	sm := NewStableManager()
	if sm == nil {
		t.Fatal("NewStableManager returned nil")
	}
	if sm.stables == nil {
		t.Fatal("stables map should be initialised")
	}
	if sm.horses == nil {
		t.Fatal("horses map should be initialised")
	}
	if len(sm.stables) != 0 {
		t.Fatalf("expected 0 stables, got %d", len(sm.stables))
	}
	if len(sm.horses) != 0 {
		t.Fatalf("expected 0 horses, got %d", len(sm.horses))
	}
}

// ---------------------------------------------------------------------------
// CreateStable
// ---------------------------------------------------------------------------

func TestCreateStable(t *testing.T) {
	sm := NewStableManager()

	stable := sm.CreateStable("Test Ranch", "owner-1")

	if stable.Name != "Test Ranch" {
		t.Errorf("expected name %q, got %q", "Test Ranch", stable.Name)
	}
	if stable.OwnerID != "owner-1" {
		t.Errorf("expected ownerID %q, got %q", "owner-1", stable.OwnerID)
	}
	if stable.Cummies != 5000 {
		t.Errorf("expected 5000 starting cummies, got %d", stable.Cummies)
	}
	if stable.ID == "" {
		t.Error("expected non-empty ID")
	}
	if len(stable.Horses) != 0 {
		t.Errorf("expected 0 horses, got %d", len(stable.Horses))
	}
	if stable.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}

	// Verify stable is stored internally
	if _, ok := sm.stables[stable.ID]; !ok {
		t.Error("stable not found in internal map after creation")
	}
}

func TestCreateStableMultiple(t *testing.T) {
	sm := NewStableManager()

	s1 := sm.CreateStable("Ranch A", "owner-a")
	s2 := sm.CreateStable("Ranch B", "owner-b")

	if s1.ID == s2.ID {
		t.Error("two stables should have different IDs")
	}
	if len(sm.stables) != 2 {
		t.Errorf("expected 2 stables, got %d", len(sm.stables))
	}
}

// ---------------------------------------------------------------------------
// GetStable
// ---------------------------------------------------------------------------

func TestGetStable(t *testing.T) {
	sm := NewStableManager()
	created := sm.CreateStable("Lookup Ranch", "owner-1")

	got, err := sm.GetStable(created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("expected ID %q, got %q", created.ID, got.ID)
	}
	if got.Name != "Lookup Ranch" {
		t.Errorf("expected name %q, got %q", "Lookup Ranch", got.Name)
	}
}

func TestGetStable_NotFound(t *testing.T) {
	sm := NewStableManager()

	_, err := sm.GetStable("does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing stable, got nil")
	}
}

// ---------------------------------------------------------------------------
// ListStables
// ---------------------------------------------------------------------------

func TestListStables_Empty(t *testing.T) {
	sm := NewStableManager()
	list := sm.ListStables()
	if len(list) != 0 {
		t.Errorf("expected 0 stables, got %d", len(list))
	}
}

func TestListStables(t *testing.T) {
	sm := NewStableManager()
	sm.CreateStable("A", "o1")
	sm.CreateStable("B", "o2")
	sm.CreateStable("C", "o3")

	list := sm.ListStables()
	if len(list) != 3 {
		t.Errorf("expected 3 stables, got %d", len(list))
	}
}

// ---------------------------------------------------------------------------
// AddHorseToStable
// ---------------------------------------------------------------------------

func TestAddHorseToStable(t *testing.T) {
	sm := NewStableManager()
	stable := sm.CreateStable("Horse Farm", "owner-1")

	horse := makeTestHorse("Thunder", 1200)
	err := sm.AddHorseToStable(stable.ID, horse)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Horse should be in the stable's roster
	if len(stable.Horses) != 1 {
		t.Fatalf("expected 1 horse in stable, got %d", len(stable.Horses))
	}
	if stable.Horses[0].Name != "Thunder" {
		t.Errorf("expected horse name %q, got %q", "Thunder", stable.Horses[0].Name)
	}

	// Horse's OwnerID should be set to the stable's owner
	if horse.OwnerID != "owner-1" {
		t.Errorf("expected horse ownerID %q, got %q", "owner-1", horse.OwnerID)
	}

	// Horse should be in the global registry
	if _, ok := sm.horses[horse.ID]; !ok {
		t.Error("horse not found in global registry")
	}
}

func TestAddHorseToStable_StableNotFound(t *testing.T) {
	sm := NewStableManager()
	horse := makeTestHorse("Ghost", 1200)

	err := sm.AddHorseToStable("no-such-stable", horse)
	if err == nil {
		t.Fatal("expected error for missing stable, got nil")
	}
}

func TestAddHorseToStable_DuplicateHorse(t *testing.T) {
	sm := NewStableManager()
	stable := sm.CreateStable("Dupe Farm", "owner-1")

	horse := makeTestHorse("Clone", 1200)
	if err := sm.AddHorseToStable(stable.ID, horse); err != nil {
		t.Fatalf("first add failed: %v", err)
	}

	err := sm.AddHorseToStable(stable.ID, horse)
	if err == nil {
		t.Fatal("expected error for duplicate horse, got nil")
	}
}

func TestAddMultipleHorsesToStable(t *testing.T) {
	sm := NewStableManager()
	stable := sm.CreateStable("Big Ranch", "owner-1")

	for i := 0; i < 5; i++ {
		h := makeTestHorse("Horse", 1200)
		if err := sm.AddHorseToStable(stable.ID, h); err != nil {
			t.Fatalf("failed to add horse %d: %v", i, err)
		}
	}

	if len(stable.Horses) != 5 {
		t.Errorf("expected 5 horses, got %d", len(stable.Horses))
	}
	if len(sm.horses) != 5 {
		t.Errorf("expected 5 horses in global registry, got %d", len(sm.horses))
	}
}

// ---------------------------------------------------------------------------
// GetHorse
// ---------------------------------------------------------------------------

func TestGetHorse(t *testing.T) {
	sm := NewStableManager()
	stable := sm.CreateStable("Horse Farm", "owner-1")

	horse := makeTestHorse("Finder", 1500)
	_ = sm.AddHorseToStable(stable.ID, horse)

	got, err := sm.GetHorse(horse.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "Finder" {
		t.Errorf("expected name %q, got %q", "Finder", got.Name)
	}
	if got.ELO != 1500 {
		t.Errorf("expected ELO 1500, got %f", got.ELO)
	}
}

func TestGetHorse_NotFound(t *testing.T) {
	sm := NewStableManager()

	_, err := sm.GetHorse("ghost-horse")
	if err == nil {
		t.Fatal("expected error for missing horse, got nil")
	}
}

func TestGetHorse_AcrossStables(t *testing.T) {
	sm := NewStableManager()
	s1 := sm.CreateStable("Stable A", "o1")
	s2 := sm.CreateStable("Stable B", "o2")

	h1 := makeTestHorse("Alpha", 1200)
	h2 := makeTestHorse("Beta", 1300)
	_ = sm.AddHorseToStable(s1.ID, h1)
	_ = sm.AddHorseToStable(s2.ID, h2)

	// Both horses should be findable from the global registry
	got1, err := sm.GetHorse(h1.ID)
	if err != nil {
		t.Fatalf("unexpected error finding h1: %v", err)
	}
	if got1.Name != "Alpha" {
		t.Errorf("expected %q, got %q", "Alpha", got1.Name)
	}

	got2, err := sm.GetHorse(h2.ID)
	if err != nil {
		t.Fatalf("unexpected error finding h2: %v", err)
	}
	if got2.Name != "Beta" {
		t.Errorf("expected %q, got %q", "Beta", got2.Name)
	}
}

// ---------------------------------------------------------------------------
// ListHorses
// ---------------------------------------------------------------------------

func TestListHorses(t *testing.T) {
	sm := NewStableManager()
	stable := sm.CreateStable("List Farm", "owner-1")

	h1 := makeTestHorse("A", 1200)
	h2 := makeTestHorse("B", 1300)
	_ = sm.AddHorseToStable(stable.ID, h1)
	_ = sm.AddHorseToStable(stable.ID, h2)

	horses := sm.ListHorses(stable.ID)
	if len(horses) != 2 {
		t.Fatalf("expected 2 horses, got %d", len(horses))
	}
}

func TestListHorses_StableNotFound(t *testing.T) {
	sm := NewStableManager()
	horses := sm.ListHorses("nonexistent")
	if horses != nil {
		t.Errorf("expected nil for missing stable, got %v", horses)
	}
}

func TestListHorses_EmptyStable(t *testing.T) {
	sm := NewStableManager()
	stable := sm.CreateStable("Empty Ranch", "owner-1")

	horses := sm.ListHorses(stable.ID)
	if len(horses) != 0 {
		t.Errorf("expected 0 horses, got %d", len(horses))
	}
}

func TestListHorses_OnlyThisStable(t *testing.T) {
	sm := NewStableManager()
	s1 := sm.CreateStable("Stable 1", "o1")
	s2 := sm.CreateStable("Stable 2", "o2")

	_ = sm.AddHorseToStable(s1.ID, makeTestHorse("H1", 1200))
	_ = sm.AddHorseToStable(s1.ID, makeTestHorse("H2", 1200))
	_ = sm.AddHorseToStable(s2.ID, makeTestHorse("H3", 1200))

	list1 := sm.ListHorses(s1.ID)
	list2 := sm.ListHorses(s2.ID)

	if len(list1) != 2 {
		t.Errorf("expected 2 horses in s1, got %d", len(list1))
	}
	if len(list2) != 1 {
		t.Errorf("expected 1 horse in s2, got %d", len(list2))
	}
}

// ---------------------------------------------------------------------------
// GetLeaderboard
// ---------------------------------------------------------------------------

func TestGetLeaderboard_SortedByELO(t *testing.T) {
	sm := NewStableManager()
	stable := sm.CreateStable("ELO Farm", "owner-1")

	// Add horses with different ELOs
	h1 := makeTestHorse("Low", 1000)
	h2 := makeTestHorse("High", 1500)
	h3 := makeTestHorse("Mid", 1250)
	_ = sm.AddHorseToStable(stable.ID, h1)
	_ = sm.AddHorseToStable(stable.ID, h2)
	_ = sm.AddHorseToStable(stable.ID, h3)

	board := sm.GetLeaderboard()
	if len(board) != 3 {
		t.Fatalf("expected 3 horses on leaderboard, got %d", len(board))
	}

	// Should be sorted descending by ELO: High (1500), Mid (1250), Low (1000)
	if board[0].Name != "High" {
		t.Errorf("expected rank 1 = High, got %q (ELO=%.0f)", board[0].Name, board[0].ELO)
	}
	if board[1].Name != "Mid" {
		t.Errorf("expected rank 2 = Mid, got %q (ELO=%.0f)", board[1].Name, board[1].ELO)
	}
	if board[2].Name != "Low" {
		t.Errorf("expected rank 3 = Low, got %q (ELO=%.0f)", board[2].Name, board[2].ELO)
	}
}

func TestGetLeaderboard_TiebreakerByWins(t *testing.T) {
	sm := NewStableManager()
	stable := sm.CreateStable("Tie Farm", "owner-1")

	h1 := makeTestHorse("Loser", 1200)
	h1.Wins = 2
	h2 := makeTestHorse("Winner", 1200)
	h2.Wins = 10

	_ = sm.AddHorseToStable(stable.ID, h1)
	_ = sm.AddHorseToStable(stable.ID, h2)

	board := sm.GetLeaderboard()
	if len(board) != 2 {
		t.Fatalf("expected 2 horses, got %d", len(board))
	}
	// Same ELO — tiebreaker is wins descending
	if board[0].Name != "Winner" {
		t.Errorf("expected rank 1 = Winner, got %q", board[0].Name)
	}
	if board[1].Name != "Loser" {
		t.Errorf("expected rank 2 = Loser, got %q", board[1].Name)
	}
}

func TestGetLeaderboard_TiebreakerByName(t *testing.T) {
	sm := NewStableManager()
	stable := sm.CreateStable("Name Farm", "owner-1")

	h1 := makeTestHorse("Zebra", 1200)
	h1.Wins = 5
	h2 := makeTestHorse("Alpha", 1200)
	h2.Wins = 5

	_ = sm.AddHorseToStable(stable.ID, h1)
	_ = sm.AddHorseToStable(stable.ID, h2)

	board := sm.GetLeaderboard()
	if len(board) != 2 {
		t.Fatalf("expected 2 horses, got %d", len(board))
	}
	// Same ELO, same wins — tiebreaker is name ascending
	if board[0].Name != "Alpha" {
		t.Errorf("expected rank 1 = Alpha, got %q", board[0].Name)
	}
	if board[1].Name != "Zebra" {
		t.Errorf("expected rank 2 = Zebra, got %q", board[1].Name)
	}
}

func TestGetLeaderboard_Empty(t *testing.T) {
	sm := NewStableManager()
	board := sm.GetLeaderboard()
	if len(board) != 0 {
		t.Errorf("expected 0 horses on empty leaderboard, got %d", len(board))
	}
}

// ---------------------------------------------------------------------------
// MoveHorse
// ---------------------------------------------------------------------------

func TestMoveHorse(t *testing.T) {
	sm := NewStableManager()
	from := sm.CreateStable("Source", "owner-from")
	to := sm.CreateStable("Dest", "owner-to")

	horse := makeTestHorse("Traveler", 1200)
	_ = sm.AddHorseToStable(from.ID, horse)

	err := sm.MoveHorse(horse.ID, from.ID, to.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Horse should be removed from source
	if len(from.Horses) != 0 {
		t.Errorf("expected 0 horses in source, got %d", len(from.Horses))
	}

	// Horse should be added to destination
	if len(to.Horses) != 1 {
		t.Fatalf("expected 1 horse in dest, got %d", len(to.Horses))
	}
	if to.Horses[0].Name != "Traveler" {
		t.Errorf("expected %q in dest, got %q", "Traveler", to.Horses[0].Name)
	}

	// Horse's owner should be updated — use GetHorse to get the canonical
	// pointer from the global registry (the caller's original pointer is
	// stale after re-registration into the slice).
	got, err := sm.GetHorse(horse.ID)
	if err != nil {
		t.Fatalf("horse not found after move: %v", err)
	}
	if got.OwnerID != "owner-to" {
		t.Errorf("expected ownerID %q, got %q", "owner-to", got.OwnerID)
	}

	// Horse should still be findable in global registry
	if got.OwnerID != "owner-to" {
		t.Errorf("global registry ownerID mismatch: expected %q, got %q", "owner-to", got.OwnerID)
	}
}

func TestMoveHorse_SourceNotFound(t *testing.T) {
	sm := NewStableManager()
	to := sm.CreateStable("Dest", "o2")
	horse := makeTestHorse("X", 1200)
	_ = sm.AddHorseToStable(to.ID, horse)

	err := sm.MoveHorse(horse.ID, "nonexistent", to.ID)
	if err == nil {
		t.Fatal("expected error for missing source stable")
	}
}

func TestMoveHorse_DestNotFound(t *testing.T) {
	sm := NewStableManager()
	from := sm.CreateStable("Source", "o1")
	horse := makeTestHorse("X", 1200)
	_ = sm.AddHorseToStable(from.ID, horse)

	err := sm.MoveHorse(horse.ID, from.ID, "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing destination stable")
	}
}

func TestMoveHorse_HorseNotFound(t *testing.T) {
	sm := NewStableManager()
	from := sm.CreateStable("Source", "o1")
	to := sm.CreateStable("Dest", "o2")

	err := sm.MoveHorse("no-such-horse", from.ID, to.ID)
	if err == nil {
		t.Fatal("expected error for missing horse")
	}
}

func TestMoveHorse_HorseNotInSourceStable(t *testing.T) {
	sm := NewStableManager()
	from := sm.CreateStable("Source", "o1")
	to := sm.CreateStable("Dest", "o2")
	other := sm.CreateStable("Other", "o3")

	horse := makeTestHorse("Misplaced", 1200)
	// Add horse to 'other', not 'from'
	_ = sm.AddHorseToStable(other.ID, horse)

	err := sm.MoveHorse(horse.ID, from.ID, to.ID)
	if err == nil {
		t.Fatal("expected error when horse is not in the source stable")
	}
}

// ---------------------------------------------------------------------------
// TransferCummies
// ---------------------------------------------------------------------------

func TestTransferCummies(t *testing.T) {
	sm := NewStableManager()
	s1 := sm.CreateStable("Rich", "o1")
	s2 := sm.CreateStable("Poor", "o2")

	err := sm.TransferCummies(s1.ID, s2.ID, 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s1.Cummies != 4000 {
		t.Errorf("expected source to have 4000, got %d", s1.Cummies)
	}
	if s2.Cummies != 6000 {
		t.Errorf("expected dest to have 6000, got %d", s2.Cummies)
	}
}

func TestTransferCummies_InsufficientFunds(t *testing.T) {
	sm := NewStableManager()
	s1 := sm.CreateStable("Broke", "o1")
	s2 := sm.CreateStable("Target", "o2")

	err := sm.TransferCummies(s1.ID, s2.ID, 10000)
	if err == nil {
		t.Fatal("expected error for insufficient cummies")
	}
}

func TestTransferCummies_NonPositiveAmount(t *testing.T) {
	sm := NewStableManager()
	s1 := sm.CreateStable("A", "o1")
	s2 := sm.CreateStable("B", "o2")

	if err := sm.TransferCummies(s1.ID, s2.ID, 0); err == nil {
		t.Error("expected error for zero amount")
	}
	if err := sm.TransferCummies(s1.ID, s2.ID, -100); err == nil {
		t.Error("expected error for negative amount")
	}
}

func TestTransferCummies_SourceNotFound(t *testing.T) {
	sm := NewStableManager()
	s2 := sm.CreateStable("B", "o2")

	err := sm.TransferCummies("nope", s2.ID, 100)
	if err == nil {
		t.Fatal("expected error for missing source stable")
	}
}

func TestTransferCummies_DestNotFound(t *testing.T) {
	sm := NewStableManager()
	s1 := sm.CreateStable("A", "o1")

	err := sm.TransferCummies(s1.ID, "nope", 100)
	if err == nil {
		t.Fatal("expected error for missing dest stable")
	}
}

// ---------------------------------------------------------------------------
// UpdateHorseStats
// ---------------------------------------------------------------------------

func TestUpdateHorseStats(t *testing.T) {
	sm := NewStableManager()
	stable := sm.CreateStable("Stats Farm", "owner-1")
	horse := makeTestHorse("Racer", 1200)
	_ = sm.AddHorseToStable(stable.ID, horse)

	err := sm.UpdateHorseStats(horse.ID, 1, 0, 1, 1250)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Fetch the canonical pointer from the registry (after AddHorseToStable,
	// the global pointer points into the stable's slice, not the caller's
	// original pointer).
	h, _ := sm.GetHorse(horse.ID)

	if h.Wins != 1 {
		t.Errorf("expected 1 win, got %d", h.Wins)
	}
	if h.Races != 1 {
		t.Errorf("expected 1 race, got %d", h.Races)
	}
	if h.ELO != 1250 {
		t.Errorf("expected ELO 1250, got %f", h.ELO)
	}

	// Update again
	err = sm.UpdateHorseStats(horse.ID, 0, 1, 1, 1230)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	h, _ = sm.GetHorse(horse.ID)
	if h.Wins != 1 {
		t.Errorf("expected 1 win after second update, got %d", h.Wins)
	}
	if h.Losses != 1 {
		t.Errorf("expected 1 loss, got %d", h.Losses)
	}
	if h.Races != 2 {
		t.Errorf("expected 2 races, got %d", h.Races)
	}
}

func TestUpdateHorseStats_NotFound(t *testing.T) {
	sm := NewStableManager()
	err := sm.UpdateHorseStats("ghost", 1, 0, 1, 1300)
	if err == nil {
		t.Fatal("expected error for missing horse")
	}
}
