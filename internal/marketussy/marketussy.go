// Package marketussy implements the stud marketplace and economy system
// for StallionUSSY. It manages stud listings, breeding transactions,
// the Sappho Scale quality rating, ELO matchmaking, and the 2% Cummies
// burn mechanism that keeps the in-game economy deflationary.
package marketussy

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mojomast/stallionussy/internal/models"
)

// ---------------------------------------------------------------------------
// Market — the in-memory stud marketplace
// ---------------------------------------------------------------------------

// Market is the central stud marketplace. All operations are thread-safe
// via a sync.RWMutex so concurrent HTTP handlers can read/write safely.
type Market struct {
	mu           sync.RWMutex
	listings     map[string]*models.StudListing
	transactions []*models.MarketTransaction
	totalBurned  int64 // running total of Cummies removed from the economy
}

// NewMarket creates an empty marketplace ready for listings.
func NewMarket() *Market {
	return &Market{
		listings:     make(map[string]*models.StudListing),
		transactions: make([]*models.MarketTransaction, 0),
		totalBurned:  0,
	}
}

// ---------------------------------------------------------------------------
// Import — load pre-existing data (for DB hydration on startup)
// ---------------------------------------------------------------------------

// ImportListing adds an existing listing (e.g. loaded from DB) directly into
// the in-memory registry. If a listing with the same ID already exists it is
// replaced.
func (m *Market) ImportListing(listing *models.StudListing) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listings[listing.ID] = listing
}

// ---------------------------------------------------------------------------
// Listing CRUD
// ---------------------------------------------------------------------------

// CreateListing advertises a horse on the stud market. The listing includes
// pedigree info (SireID, MareID, generation) and a Sappho Score computed
// from the horse's stats. Price is denominated in Cummies.
func (m *Market) CreateListing(horse *models.Horse, ownerID string, price int64) (*models.StudListing, error) {
	if horse == nil {
		return nil, fmt.Errorf("marketussy: cannot list a nil horse")
	}
	if ownerID == "" {
		return nil, fmt.Errorf("marketussy: ownerID must not be empty")
	}
	if price <= 0 {
		return nil, fmt.Errorf("marketussy: price must be positive, got %d", price)
	}

	sappho := CalcSapphoScore(horse)

	// Build a human-readable pedigree string.
	pedigree := buildPedigree(horse)

	listing := &models.StudListing{
		ID:          uuid.New().String(),
		HorseID:     horse.ID,
		HorseName:   horse.Name,
		OwnerID:     ownerID,
		Price:       price,
		Pedigree:    pedigree,
		SapphoScore: sappho,
		Active:      true,
		CreatedAt:   time.Now(),
	}

	m.mu.Lock()
	m.listings[listing.ID] = listing
	m.mu.Unlock()

	return listing, nil
}

// buildPedigree creates a short lineage summary for a horse.
// Gen-0 founders get "Founder (Gen 0)", bred horses get sire/mare IDs.
func buildPedigree(horse *models.Horse) string {
	if horse.SireID == "" && horse.MareID == "" {
		return fmt.Sprintf("Founder (Gen %d)", horse.Generation)
	}
	return fmt.Sprintf("Sire: %s | Mare: %s (Gen %d)", horse.SireID, horse.MareID, horse.Generation)
}

// GetListing returns a single listing by ID.
func (m *Market) GetListing(id string) (*models.StudListing, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	listing, ok := m.listings[id]
	if !ok {
		return nil, fmt.Errorf("marketussy: listing %q not found", id)
	}
	return listing, nil
}

// ListActiveListings returns all active stud listings sorted by SapphoScore
// descending (highest quality first). NaN-scored listings sort to the end.
func (m *Market) ListActiveListings() []*models.StudListing {
	m.mu.RLock()
	defer m.mu.RUnlock()

	active := make([]*models.StudListing, 0, len(m.listings))
	for _, l := range m.listings {
		if l.Active {
			active = append(active, l)
		}
	}

	sort.Slice(active, func(i, j int) bool {
		si, sj := active[i].SapphoScore, active[j].SapphoScore
		// NaN sorts to the bottom — it's unknowable, not worthless.
		if math.IsNaN(si) && math.IsNaN(sj) {
			return false
		}
		if math.IsNaN(si) {
			return false // i goes after j
		}
		if math.IsNaN(sj) {
			return true // i goes before j
		}
		return si > sj
	})

	return active
}

// ---------------------------------------------------------------------------
// Transactions
// ---------------------------------------------------------------------------

// PurchaseBreeding processes a stud-market purchase. It validates the listing
// is active, computes the 2% deflationary burn, and records a MarketTransaction.
//
// The actual breeding (foal creation via genussy.Breed) happens in the caller —
// this function only handles the economic side. The returned transaction's
// FoalID will be empty; the caller should fill it in after breeding.
func (m *Market) PurchaseBreeding(listingID, buyerID string) (*models.MarketTransaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	listing, ok := m.listings[listingID]
	if !ok {
		return nil, fmt.Errorf("marketussy: listing %q not found", listingID)
	}
	if !listing.Active {
		return nil, fmt.Errorf("marketussy: listing %q is no longer active", listingID)
	}
	if buyerID == "" {
		return nil, fmt.Errorf("marketussy: buyerID must not be empty")
	}
	if buyerID == listing.OwnerID {
		return nil, fmt.Errorf("marketussy: cannot purchase your own listing")
	}

	// 2% burn — the invisible hand of the Cummies economy.
	burnAmount := listing.Price * 2 / 100

	tx := &models.MarketTransaction{
		ID:         uuid.New().String(),
		ListingID:  listing.ID,
		BuyerID:    buyerID,
		SellerID:   listing.OwnerID,
		Price:      listing.Price,
		BurnAmount: burnAmount,
		FoalID:     "", // filled by caller after breeding
		CreatedAt:  time.Now(),
	}

	m.transactions = append(m.transactions, tx)
	m.totalBurned += burnAmount

	return tx, nil
}

// DelistStud deactivates a listing. Only the listing's owner can delist.
func (m *Market) DelistStud(listingID, ownerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	listing, ok := m.listings[listingID]
	if !ok {
		return fmt.Errorf("marketussy: listing %q not found", listingID)
	}
	if listing.OwnerID != ownerID {
		return fmt.Errorf("marketussy: only the owner can delist (expected %q, got %q)", listing.OwnerID, ownerID)
	}
	if !listing.Active {
		return fmt.Errorf("marketussy: listing %q is already inactive", listingID)
	}

	listing.Active = false
	return nil
}

// ---------------------------------------------------------------------------
// Transaction history & burn tracking
// ---------------------------------------------------------------------------

// GetTransactionHistory returns all recorded transactions in chronological order.
func (m *Market) GetTransactionHistory() []*models.MarketTransaction {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy of the slice so callers can't corrupt our internal state.
	out := make([]*models.MarketTransaction, len(m.transactions))
	copy(out, m.transactions)
	return out
}

// GetTotalBurned returns the cumulative Cummies burned across all transactions.
func (m *Market) GetTotalBurned() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.totalBurned
}

// ---------------------------------------------------------------------------
// The Sappho Scale — quality rating from 0 to 12
// ---------------------------------------------------------------------------

// CalcSapphoScore computes the Sappho Scale rating for a horse.
//
// The score is a weighted composite on a 0–12 scale:
//
//	FitnessCeiling  (weight 0.4) — raw genetic potential
//	Win rate        (weight 0.3) — proven track performance
//	Normalized ELO  (weight 0.3) — competitive standing
//
// A score of 12.0 is a perfect horse. Scores are capped at 12.0.
//
// Special case: E-008's Chosen (LotNumber 6) always returns NaN because
// "the pricing algorithm cannot calculate a value" for an anomalous entity.
func CalcSapphoScore(horse *models.Horse) float64 {
	// E-008 is beyond the Sappho Scale. The algorithm refuses to classify it.
	if horse.LotNumber == 6 {
		return math.NaN()
	}

	// --- Component 1: FitnessCeiling (0–1 normalized) ---
	// FitnessCeiling is already in [0, 1] for normal horses.
	// Legendary horses with overrides (e.g. 9.99) get clamped.
	fitComp := horse.FitnessCeiling
	if fitComp > 1.0 {
		fitComp = 1.0
	}
	if fitComp < 0.0 {
		fitComp = 0.0
	}

	// --- Component 2: Win rate (0–1) ---
	var winRate float64
	if horse.Races > 0 {
		winRate = float64(horse.Wins) / float64(horse.Races)
	}

	// --- Component 3: ELO normalized to 0–1 ---
	// ELO typically ranges 800–2400. We normalize with 800 as floor, 2400 as ceiling.
	const eloFloor = 800.0
	const eloCeiling = 2400.0
	eloNorm := (horse.ELO - eloFloor) / (eloCeiling - eloFloor)
	if eloNorm > 1.0 {
		eloNorm = 1.0
	}
	if eloNorm < 0.0 {
		eloNorm = 0.0
	}

	// --- Weighted composite ---
	raw := fitComp*0.4 + winRate*0.3 + eloNorm*0.3

	// Scale to 0–12.
	score := raw * 12.0

	// Cap at 12.0 (the Sappho ceiling — perfection).
	if score > 12.0 {
		score = 12.0
	}

	return score
}

// ---------------------------------------------------------------------------
// ELO — standard Elo rating update
// ---------------------------------------------------------------------------

// ELOUpdate computes new Elo ratings after a head-to-head result.
// Uses the standard formula with K-factor 32:
//
//	Expected(self) = 1 / (1 + 10^((opponent - self) / 400))
//	NewRating = OldRating + K * (result - expected)
//
// The winner gets result=1.0, the loser gets result=0.0.
// Returns (newWinnerELO, newLoserELO).
//
// This function is pure — it does NOT mutate the Horse structs.
// The caller is responsible for writing the new values back.
func ELOUpdate(winner, loser *models.Horse) (float64, float64) {
	const k = 32.0

	// Expected score for winner.
	expWinner := 1.0 / (1.0 + math.Pow(10.0, (loser.ELO-winner.ELO)/400.0))
	// Expected score for loser.
	expLoser := 1.0 / (1.0 + math.Pow(10.0, (winner.ELO-loser.ELO)/400.0))

	newWinner := winner.ELO + k*(1.0-expWinner)
	newLoser := loser.ELO + k*(0.0-expLoser)

	return newWinner, newLoser
}
