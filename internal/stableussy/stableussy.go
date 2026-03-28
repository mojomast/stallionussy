// Package stableussy implements the in-memory stable management system for
// StallionUSSY. It provides CRUD operations for stables and horses, currency
// transfers, stat tracking, and leaderboard queries — all concurrency-safe
// via sync.RWMutex.
package stableussy

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mojomast/stallionussy/internal/genussy"
	"github.com/mojomast/stallionussy/internal/models"
)

// ---------------------------------------------------------------------------
// StableManager — the central in-memory registry
// ---------------------------------------------------------------------------

// StableManager holds all stables and horses in memory, providing
// thread-safe access via a read-write mutex.
type StableManager struct {
	mu      sync.RWMutex
	stables map[string]*models.Stable // keyed by stable ID
	horses  map[string]*models.Horse  // global horse registry, keyed by horse ID
}

// NewStableManager creates and returns an empty StableManager ready for use.
func NewStableManager() *StableManager {
	return &StableManager{
		stables: make(map[string]*models.Stable),
		horses:  make(map[string]*models.Horse),
	}
}

// ---------------------------------------------------------------------------
// Stable CRUD
// ---------------------------------------------------------------------------

// CreateStable creates a new stable with the given name and owner, seeded with
// 5000 starting Cummies. Returns the newly created Stable.
func (sm *StableManager) CreateStable(name, ownerID string) *models.Stable {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	stable := &models.Stable{
		ID:        uuid.New().String(),
		Name:      name,
		OwnerID:   ownerID,
		Cummies:   5000,
		Horses:    []models.Horse{},
		CreatedAt: time.Now(),
	}

	sm.stables[stable.ID] = stable
	return stable
}

// GetStable retrieves a stable by its ID. Returns an error if not found.
func (sm *StableManager) GetStable(id string) (*models.Stable, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stable, ok := sm.stables[id]
	if !ok {
		return nil, fmt.Errorf("stable not found: %s", id)
	}
	return stable, nil
}

// ListStables returns all stables in no guaranteed order.
func (sm *StableManager) ListStables() []*models.Stable {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make([]*models.Stable, 0, len(sm.stables))
	for _, s := range sm.stables {
		result = append(result, s)
	}
	return result
}

// ---------------------------------------------------------------------------
// Horse management
// ---------------------------------------------------------------------------

// AddHorseToStable adds a horse to the specified stable and registers it in
// the global horse registry. Returns an error if the stable is not found or
// if a horse with the same ID already exists.
func (sm *StableManager) AddHorseToStable(stableID string, horse *models.Horse) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	stable, ok := sm.stables[stableID]
	if !ok {
		return fmt.Errorf("stable not found: %s", stableID)
	}

	if _, exists := sm.horses[horse.ID]; exists {
		return fmt.Errorf("horse already registered: %s", horse.ID)
	}

	// Set the horse's owner to the stable's owner.
	horse.OwnerID = stable.OwnerID

	// Append to the stable's horse roster.
	stable.Horses = append(stable.Horses, *horse)

	// Register in the global registry (pointer to the copy inside the stable's slice).
	// We store a pointer to the horse separately so global lookups stay O(1).
	sm.horses[horse.ID] = horse

	return nil
}

// GetHorse retrieves a horse by ID from the global registry.
// Returns an error if the horse is not found.
func (sm *StableManager) GetHorse(id string) (*models.Horse, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	horse, ok := sm.horses[id]
	if !ok {
		return nil, fmt.Errorf("horse not found: %s", id)
	}
	return horse, nil
}

// ListHorses returns all horses belonging to the specified stable.
// Returns nil if the stable is not found.
func (sm *StableManager) ListHorses(stableID string) []*models.Horse {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stable, ok := sm.stables[stableID]
	if !ok {
		return nil
	}

	result := make([]*models.Horse, 0, len(stable.Horses))
	for i := range stable.Horses {
		// Look up the canonical pointer from the global registry so callers
		// get live references (useful for stat updates).
		if h, exists := sm.horses[stable.Horses[i].ID]; exists {
			result = append(result, h)
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Economy
// ---------------------------------------------------------------------------

// TransferCummies moves the specified amount of Cummies from one stable to
// another. Returns an error if either stable is not found, the amount is
// non-positive, or the source stable has insufficient funds.
func (sm *StableManager) TransferCummies(fromStableID, toStableID string, amount int64) error {
	if amount <= 0 {
		return fmt.Errorf("transfer amount must be positive, got %d", amount)
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	from, ok := sm.stables[fromStableID]
	if !ok {
		return fmt.Errorf("source stable not found: %s", fromStableID)
	}

	to, ok := sm.stables[toStableID]
	if !ok {
		return fmt.Errorf("destination stable not found: %s", toStableID)
	}

	if from.Cummies < amount {
		return fmt.Errorf("insufficient cummies: have %d, need %d", from.Cummies, amount)
	}

	from.Cummies -= amount
	to.Cummies += amount

	return nil
}

// ---------------------------------------------------------------------------
// Race stat tracking
// ---------------------------------------------------------------------------

// UpdateHorseStats updates a horse's post-race statistics. It adds the
// provided wins, losses, and races counts, and sets the ELO to the new value.
// Returns an error if the horse is not found.
func (sm *StableManager) UpdateHorseStats(horseID string, wins, losses, races int, elo float64) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	horse, ok := sm.horses[horseID]
	if !ok {
		return fmt.Errorf("horse not found: %s", horseID)
	}

	horse.Wins += wins
	horse.Losses += losses
	horse.Races += races
	horse.ELO = elo

	// Also update the copy embedded in the stable's Horses slice so that
	// stable-level views stay consistent.
	sm.syncHorseToStable(horse)

	return nil
}

// syncHorseToStable propagates a horse's current state back into its parent
// stable's Horses slice. Must be called while holding sm.mu (write lock).
func (sm *StableManager) syncHorseToStable(horse *models.Horse) {
	for _, stable := range sm.stables {
		for i := range stable.Horses {
			if stable.Horses[i].ID == horse.ID {
				stable.Horses[i] = *horse
				return
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Legendary seeding
// ---------------------------------------------------------------------------

// SeedLegendaries creates all 12 canonical legendary horses (lots 1-12) using
// genussy.CreateLegendary and adds each to the specified stable. Horses are
// also registered in the global registry.
func (sm *StableManager) SeedLegendaries(stableID string) {
	for lot := 1; lot <= 12; lot++ {
		horse := genussy.CreateLegendary(lot)
		if horse == nil {
			continue
		}
		// AddHorseToStable acquires its own lock, so we call it directly.
		_ = sm.AddHorseToStable(stableID, horse)
	}
}

// ---------------------------------------------------------------------------
// Horse transfer between stables (for trading)
// ---------------------------------------------------------------------------

// MoveHorse transfers a horse from one stable to another. The horse is removed
// from the source stable's roster and added to the destination stable's roster.
// The horse's OwnerID is updated to the destination stable's owner.
// Returns an error if either stable or the horse is not found, or if the horse
// is not in the source stable.
func (sm *StableManager) MoveHorse(horseID, fromStableID, toStableID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	fromStable, ok := sm.stables[fromStableID]
	if !ok {
		return fmt.Errorf("source stable not found: %s", fromStableID)
	}

	toStable, ok := sm.stables[toStableID]
	if !ok {
		return fmt.Errorf("destination stable not found: %s", toStableID)
	}

	horse, ok := sm.horses[horseID]
	if !ok {
		return fmt.Errorf("horse not found: %s", horseID)
	}

	// Remove from source stable's roster.
	found := false
	for i := range fromStable.Horses {
		if fromStable.Horses[i].ID == horseID {
			fromStable.Horses = append(fromStable.Horses[:i], fromStable.Horses[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("horse %s not found in stable %s", horseID, fromStableID)
	}

	// Update ownership.
	horse.OwnerID = toStable.OwnerID

	// Add to destination stable's roster.
	toStable.Horses = append(toStable.Horses, *horse)

	return nil
}

// ---------------------------------------------------------------------------
// Leaderboard
// ---------------------------------------------------------------------------

// GetLeaderboard returns all registered horses sorted by ELO in descending
// order. Ties are broken by win count (descending), then by name (ascending).
func (sm *StableManager) GetLeaderboard() []*models.Horse {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	board := make([]*models.Horse, 0, len(sm.horses))
	for _, h := range sm.horses {
		board = append(board, h)
	}

	sort.Slice(board, func(i, j int) bool {
		if board[i].ELO != board[j].ELO {
			return board[i].ELO > board[j].ELO
		}
		if board[i].Wins != board[j].Wins {
			return board[i].Wins > board[j].Wins
		}
		return board[i].Name < board[j].Name
	})

	return board
}
