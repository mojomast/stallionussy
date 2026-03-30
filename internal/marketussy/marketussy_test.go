package marketussy

import (
	"math"
	"testing"

	"github.com/mojomast/stallionussy/internal/models"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makeTestHorse builds a baseline horse with known stats for deterministic
// Sappho Score calculations.
func makeTestHorse(id, name, ownerID string) *models.Horse {
	return &models.Horse{
		ID:             id,
		Name:           name,
		OwnerID:        ownerID,
		FitnessCeiling: 0.8,
		CurrentFitness: 0.5,
		ELO:            1200,
		Races:          10,
		Wins:           3,
		Generation:     0,
	}
}

// ---------------------------------------------------------------------------
// NewMarket
// ---------------------------------------------------------------------------

func TestNewMarket(t *testing.T) {
	m := NewMarket()
	if m == nil {
		t.Fatal("NewMarket returned nil")
	}
	if m.listings == nil {
		t.Error("listings map is nil")
	}
	if m.transactions == nil {
		t.Error("transactions slice is nil")
	}
	if m.totalBurned != 0 {
		t.Errorf("totalBurned = %d, want 0", m.totalBurned)
	}
}

// ---------------------------------------------------------------------------
// CreateListing
// ---------------------------------------------------------------------------

func TestCreateListing_Success(t *testing.T) {
	m := NewMarket()
	horse := makeTestHorse("h1", "TestHorse", "owner1")

	listing, err := m.CreateListing(horse, "owner1", 1000)
	if err != nil {
		t.Fatalf("CreateListing failed: %v", err)
	}
	if listing.ID == "" {
		t.Error("listing ID is empty")
	}
	if listing.HorseID != "h1" {
		t.Errorf("listing HorseID = %q, want h1", listing.HorseID)
	}
	if listing.HorseName != "TestHorse" {
		t.Errorf("listing HorseName = %q, want TestHorse", listing.HorseName)
	}
	if listing.OwnerID != "owner1" {
		t.Errorf("listing OwnerID = %q, want owner1", listing.OwnerID)
	}
	if listing.Price != 1000 {
		t.Errorf("listing Price = %d, want 1000", listing.Price)
	}
	if !listing.Active {
		t.Error("listing should be active")
	}
	if listing.SapphoScore <= 0 {
		t.Errorf("listing SapphoScore = %v, expected > 0", listing.SapphoScore)
	}
	if listing.Pedigree == "" {
		t.Error("listing Pedigree is empty")
	}
	if listing.CreatedAt.IsZero() {
		t.Error("listing CreatedAt is zero")
	}
}

func TestCreateListing_Founder_Pedigree(t *testing.T) {
	m := NewMarket()
	horse := makeTestHorse("h1", "FounderHorse", "owner1")
	// Gen-0 founder: no SireID/MareID

	listing, err := m.CreateListing(horse, "owner1", 500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if listing.Pedigree != "Founder (Gen 0)" {
		t.Errorf("pedigree = %q, want 'Founder (Gen 0)'", listing.Pedigree)
	}
}

func TestCreateListing_BredHorse_Pedigree(t *testing.T) {
	m := NewMarket()
	horse := makeTestHorse("h1", "BredHorse", "owner1")
	horse.SireID = "sire-1"
	horse.MareID = "mare-1"
	horse.Generation = 2

	listing, err := m.CreateListing(horse, "owner1", 500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "Sire: sire-1 | Mare: mare-1 (Gen 2)"
	if listing.Pedigree != expected {
		t.Errorf("pedigree = %q, want %q", listing.Pedigree, expected)
	}
}

func TestCreateListing_NilHorse(t *testing.T) {
	m := NewMarket()
	_, err := m.CreateListing(nil, "owner1", 1000)
	if err == nil {
		t.Fatal("expected error for nil horse")
	}
}

func TestCreateListing_EmptyOwnerID(t *testing.T) {
	m := NewMarket()
	horse := makeTestHorse("h1", "TestHorse", "owner1")
	_, err := m.CreateListing(horse, "", 1000)
	if err == nil {
		t.Fatal("expected error for empty ownerID")
	}
}

func TestCreateListing_ZeroPrice(t *testing.T) {
	m := NewMarket()
	horse := makeTestHorse("h1", "TestHorse", "owner1")
	_, err := m.CreateListing(horse, "owner1", 0)
	if err == nil {
		t.Fatal("expected error for zero price")
	}
}

func TestCreateListing_NegativePrice(t *testing.T) {
	m := NewMarket()
	horse := makeTestHorse("h1", "TestHorse", "owner1")
	_, err := m.CreateListing(horse, "owner1", -500)
	if err == nil {
		t.Fatal("expected error for negative price")
	}
}

// ---------------------------------------------------------------------------
// GetListing
// ---------------------------------------------------------------------------

func TestGetListing_Found(t *testing.T) {
	m := NewMarket()
	horse := makeTestHorse("h1", "TestHorse", "owner1")
	listing, _ := m.CreateListing(horse, "owner1", 1000)

	got, err := m.GetListing(listing.ID)
	if err != nil {
		t.Fatalf("GetListing failed: %v", err)
	}
	if got.ID != listing.ID {
		t.Errorf("got ID %q, want %q", got.ID, listing.ID)
	}
}

func TestGetListing_NotFound(t *testing.T) {
	m := NewMarket()
	_, err := m.GetListing("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent listing")
	}
}

// ---------------------------------------------------------------------------
// ListActiveListings
// ---------------------------------------------------------------------------

func TestListActiveListings_Empty(t *testing.T) {
	m := NewMarket()
	active := m.ListActiveListings()
	if len(active) != 0 {
		t.Errorf("expected 0 active listings, got %d", len(active))
	}
}

func TestListActiveListings_FiltersInactive(t *testing.T) {
	m := NewMarket()
	horse1 := makeTestHorse("h1", "Horse1", "owner1")
	horse2 := makeTestHorse("h2", "Horse2", "owner1")

	listing1, _ := m.CreateListing(horse1, "owner1", 1000)
	m.CreateListing(horse2, "owner1", 2000)

	// Deactivate listing1
	m.DelistStud(listing1.ID, "owner1")

	active := m.ListActiveListings()
	if len(active) != 1 {
		t.Fatalf("expected 1 active listing, got %d", len(active))
	}
	if active[0].HorseID != "h2" {
		t.Errorf("active listing HorseID = %q, want h2", active[0].HorseID)
	}
}

func TestListActiveListings_SortedBySapphoScoreDescending(t *testing.T) {
	m := NewMarket()

	// Horse with higher stats → higher Sappho Score
	highHorse := makeTestHorse("h-high", "HighScore", "owner1")
	highHorse.FitnessCeiling = 0.95
	highHorse.ELO = 2000
	highHorse.Races = 20
	highHorse.Wins = 15

	// Horse with lower stats
	lowHorse := makeTestHorse("h-low", "LowScore", "owner1")
	lowHorse.FitnessCeiling = 0.3
	lowHorse.ELO = 900
	lowHorse.Races = 20
	lowHorse.Wins = 1

	m.CreateListing(lowHorse, "owner1", 100)
	m.CreateListing(highHorse, "owner1", 200)

	active := m.ListActiveListings()
	if len(active) != 2 {
		t.Fatalf("expected 2 active listings, got %d", len(active))
	}
	if active[0].SapphoScore < active[1].SapphoScore {
		t.Errorf("listings not sorted by SapphoScore desc: [0]=%v, [1]=%v",
			active[0].SapphoScore, active[1].SapphoScore)
	}
}

// ---------------------------------------------------------------------------
// PurchaseBreeding
// ---------------------------------------------------------------------------

func TestPurchaseBreeding_Success(t *testing.T) {
	m := NewMarket()
	horse := makeTestHorse("h1", "StudHorse", "seller1")
	listing, _ := m.CreateListing(horse, "seller1", 1000)

	tx, err := m.PurchaseBreeding(listing.ID, "buyer1", 10000)
	if err != nil {
		t.Fatalf("PurchaseBreeding failed: %v", err)
	}
	if tx.ID == "" {
		t.Error("transaction ID is empty")
	}
	if tx.ListingID != listing.ID {
		t.Errorf("tx ListingID = %q, want %q", tx.ListingID, listing.ID)
	}
	if tx.BuyerID != "buyer1" {
		t.Errorf("tx BuyerID = %q, want buyer1", tx.BuyerID)
	}
	if tx.SellerID != "seller1" {
		t.Errorf("tx SellerID = %q, want seller1", tx.SellerID)
	}
	if tx.Price != 1000 {
		t.Errorf("tx Price = %d, want 1000", tx.Price)
	}
	if tx.FoalID != "" {
		t.Errorf("tx FoalID should be empty (filled by caller), got %q", tx.FoalID)
	}
	if tx.CreatedAt.IsZero() {
		t.Error("tx CreatedAt is zero")
	}
}

func TestPurchaseBreeding_BurnAmount(t *testing.T) {
	m := NewMarket()
	horse := makeTestHorse("h1", "StudHorse", "seller1")
	listing, _ := m.CreateListing(horse, "seller1", 1000)

	tx, _ := m.PurchaseBreeding(listing.ID, "buyer1", 10000)

	// 2% of 1000 = 20
	expectedBurn := int64(20)
	if tx.BurnAmount != expectedBurn {
		t.Errorf("BurnAmount = %d, want %d (2%% of %d)", tx.BurnAmount, expectedBurn, tx.Price)
	}
}

func TestPurchaseBreeding_BurnAmountSmallPrice(t *testing.T) {
	m := NewMarket()
	horse := makeTestHorse("h1", "StudHorse", "seller1")
	listing, _ := m.CreateListing(horse, "seller1", 50)

	tx, _ := m.PurchaseBreeding(listing.ID, "buyer1", 10000)

	// 2% of 50 = 1 (integer math: 50*2/100 = 1)
	expectedBurn := int64(1)
	if tx.BurnAmount != expectedBurn {
		t.Errorf("BurnAmount = %d, want %d", tx.BurnAmount, expectedBurn)
	}
}

func TestPurchaseBreeding_ListingPersistsAfterPurchase(t *testing.T) {
	m := NewMarket()
	horse := makeTestHorse("h1", "StudHorse", "seller1")
	listing, _ := m.CreateListing(horse, "seller1", 1000)

	m.PurchaseBreeding(listing.ID, "buyer1", 10000)

	got, _ := m.GetListing(listing.ID)
	if !got.Active {
		t.Error("listing should remain active after a single purchase (studs persist)")
	}
	if got.TimesUsed != 1 {
		t.Errorf("TimesUsed = %d, want 1", got.TimesUsed)
	}
}

func TestPurchaseBreeding_DeactivatesAfterMaxUses(t *testing.T) {
	m := NewMarket()
	horse := makeTestHorse("h1", "StudHorse", "seller1")
	listing, _ := m.CreateListing(horse, "seller1", 1000)

	// Manually set MaxUses to 2.
	m.mu.Lock()
	m.listings[listing.ID].MaxUses = 2
	m.mu.Unlock()

	m.PurchaseBreeding(listing.ID, "buyer1", 10000)
	got, _ := m.GetListing(listing.ID)
	if !got.Active {
		t.Error("listing should still be active after 1 of 2 max uses")
	}

	m.PurchaseBreeding(listing.ID, "buyer2", 10000)
	got, _ = m.GetListing(listing.ID)
	if got.Active {
		t.Error("listing should be deactivated after reaching MaxUses")
	}
}

func TestPurchaseBreeding_NotFound(t *testing.T) {
	m := NewMarket()
	_, err := m.PurchaseBreeding("nonexistent", "buyer1", 10000)
	if err == nil {
		t.Fatal("expected error for nonexistent listing")
	}
}

func TestPurchaseBreeding_EmptyBuyerID(t *testing.T) {
	m := NewMarket()
	horse := makeTestHorse("h1", "StudHorse", "seller1")
	listing, _ := m.CreateListing(horse, "seller1", 1000)

	_, err := m.PurchaseBreeding(listing.ID, "", 10000)
	if err == nil {
		t.Fatal("expected error for empty buyerID")
	}
}

func TestPurchaseBreeding_CantBuyOwnListing(t *testing.T) {
	m := NewMarket()
	horse := makeTestHorse("h1", "StudHorse", "owner1")
	listing, _ := m.CreateListing(horse, "owner1", 1000)

	_, err := m.PurchaseBreeding(listing.ID, "owner1", 10000)
	if err == nil {
		t.Fatal("expected error when buying own listing")
	}
}

// ---------------------------------------------------------------------------
// DelistStud
// ---------------------------------------------------------------------------

func TestDelistStud_Success(t *testing.T) {
	m := NewMarket()
	horse := makeTestHorse("h1", "TestHorse", "owner1")
	listing, _ := m.CreateListing(horse, "owner1", 1000)

	err := m.DelistStud(listing.ID, "owner1")
	if err != nil {
		t.Fatalf("DelistStud failed: %v", err)
	}

	got, _ := m.GetListing(listing.ID)
	if got.Active {
		t.Error("listing should be inactive after delisting")
	}
}

func TestDelistStud_NotFound(t *testing.T) {
	m := NewMarket()
	err := m.DelistStud("nonexistent", "owner1")
	if err == nil {
		t.Fatal("expected error for nonexistent listing")
	}
}

func TestDelistStud_WrongOwner(t *testing.T) {
	m := NewMarket()
	horse := makeTestHorse("h1", "TestHorse", "owner1")
	listing, _ := m.CreateListing(horse, "owner1", 1000)

	err := m.DelistStud(listing.ID, "intruder")
	if err == nil {
		t.Fatal("expected error when non-owner tries to delist")
	}
}

func TestDelistStud_AlreadyInactive(t *testing.T) {
	m := NewMarket()
	horse := makeTestHorse("h1", "TestHorse", "owner1")
	listing, _ := m.CreateListing(horse, "owner1", 1000)

	m.DelistStud(listing.ID, "owner1")
	err := m.DelistStud(listing.ID, "owner1")
	if err == nil {
		t.Fatal("expected error when delisting already-inactive listing")
	}
}

// ---------------------------------------------------------------------------
// GetTransactionHistory & GetTotalBurned
// ---------------------------------------------------------------------------

func TestGetTransactionHistory_Empty(t *testing.T) {
	m := NewMarket()
	h := m.GetTransactionHistory()
	if h == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(h) != 0 {
		t.Errorf("expected 0 transactions, got %d", len(h))
	}
}

func TestGetTransactionHistory_RecordsTransactions(t *testing.T) {
	m := NewMarket()
	horse := makeTestHorse("h1", "StudHorse", "seller1")
	listing, _ := m.CreateListing(horse, "seller1", 1000)
	m.PurchaseBreeding(listing.ID, "buyer1", 10000)

	history := m.GetTransactionHistory()
	if len(history) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(history))
	}
	if history[0].BuyerID != "buyer1" {
		t.Errorf("tx BuyerID = %q, want buyer1", history[0].BuyerID)
	}
}

func TestGetTransactionHistory_ReturnsCopy(t *testing.T) {
	m := NewMarket()
	horse := makeTestHorse("h1", "StudHorse", "seller1")
	listing, _ := m.CreateListing(horse, "seller1", 1000)
	m.PurchaseBreeding(listing.ID, "buyer1", 10000)

	h1 := m.GetTransactionHistory()
	h2 := m.GetTransactionHistory()
	if &h1[0] == &h2[0] {
		t.Error("GetTransactionHistory should return independent copies")
	}
}

func TestGetTotalBurned_InitiallyZero(t *testing.T) {
	m := NewMarket()
	if m.GetTotalBurned() != 0 {
		t.Errorf("initial totalBurned = %d, want 0", m.GetTotalBurned())
	}
}

func TestGetTotalBurned_AccumulatesBurns(t *testing.T) {
	m := NewMarket()

	horse1 := makeTestHorse("h1", "Stud1", "seller1")
	listing1, _ := m.CreateListing(horse1, "seller1", 1000) // burn = 20
	m.PurchaseBreeding(listing1.ID, "buyer1", 10000)

	horse2 := makeTestHorse("h2", "Stud2", "seller2")
	listing2, _ := m.CreateListing(horse2, "seller2", 500) // burn = 10
	m.PurchaseBreeding(listing2.ID, "buyer2", 10000)

	expectedBurn := int64(20 + 10)
	if m.GetTotalBurned() != expectedBurn {
		t.Errorf("totalBurned = %d, want %d", m.GetTotalBurned(), expectedBurn)
	}
}

// ---------------------------------------------------------------------------
// CalcSapphoScore
// ---------------------------------------------------------------------------

func TestCalcSapphoScore_NormalHorse(t *testing.T) {
	horse := makeTestHorse("h1", "TestHorse", "owner1")
	// FitnessCeiling=0.8, Races=10, Wins=3 (winRate=0.3), ELO=1200

	score := CalcSapphoScore(horse)

	// fitComp = 0.8
	// winRate = 3/10 = 0.3
	// eloNorm = (1200-800)/(2400-800) = 400/1600 = 0.25
	// raw = 0.8*0.4 + 0.3*0.3 + 0.25*0.3 = 0.32 + 0.09 + 0.075 = 0.485
	// score = 0.485 * 12 = 5.82
	expected := 5.82
	if math.Abs(score-expected) > 0.01 {
		t.Errorf("SapphoScore = %v, want ~%v", score, expected)
	}
}

func TestCalcSapphoScore_PerfectHorse(t *testing.T) {
	horse := makeTestHorse("h1", "PerfectHorse", "owner1")
	horse.FitnessCeiling = 1.0
	horse.Races = 100
	horse.Wins = 100
	horse.ELO = 2400

	score := CalcSapphoScore(horse)

	// fitComp=1.0, winRate=1.0, eloNorm=1.0
	// raw = 1*0.4 + 1*0.3 + 1*0.3 = 1.0
	// score = 12.0
	if score != 12.0 {
		t.Errorf("perfect horse SapphoScore = %v, want 12.0", score)
	}
}

func TestCalcSapphoScore_TerribleHorse(t *testing.T) {
	horse := makeTestHorse("h1", "TerribleHorse", "owner1")
	horse.FitnessCeiling = 0.0
	horse.Races = 50
	horse.Wins = 0
	horse.ELO = 800

	score := CalcSapphoScore(horse)
	if score != 0 {
		t.Errorf("terrible horse SapphoScore = %v, want 0", score)
	}
}

func TestCalcSapphoScore_NoRaces(t *testing.T) {
	horse := makeTestHorse("h1", "NewbornHorse", "owner1")
	horse.Races = 0
	horse.Wins = 0

	score := CalcSapphoScore(horse)
	// winRate = 0, eloNorm = (1200-800)/1600 = 0.25
	// raw = 0.8*0.4 + 0*0.3 + 0.25*0.3 = 0.32 + 0 + 0.075 = 0.395
	// score = 0.395 * 12 = 4.74
	expected := 4.74
	if math.Abs(score-expected) > 0.01 {
		t.Errorf("no-races SapphoScore = %v, want ~%v", score, expected)
	}
}

func TestCalcSapphoScore_E008_ReturnsNaN(t *testing.T) {
	horse := makeTestHorse("h1", "E-008", "owner1")
	horse.LotNumber = 6

	score := CalcSapphoScore(horse)
	if !math.IsNaN(score) {
		t.Errorf("E-008 SapphoScore = %v, want NaN", score)
	}
}

func TestCalcSapphoScore_HighCeilingClamped(t *testing.T) {
	horse := makeTestHorse("h1", "LegendaryHorse", "owner1")
	horse.FitnessCeiling = 9.99 // legendary override > 1.0 → clamped to 1.0
	horse.Races = 10
	horse.Wins = 10
	horse.ELO = 2400

	score := CalcSapphoScore(horse)

	// fitComp clamped to 1.0
	// raw = 1*0.4 + 1*0.3 + 1*0.3 = 1.0
	// score = 12.0
	if score != 12.0 {
		t.Errorf("high-ceiling SapphoScore = %v, want 12.0 (clamped)", score)
	}
}

func TestCalcSapphoScore_CappedAt12(t *testing.T) {
	// Even with extreme stats, score should not exceed 12.0
	horse := makeTestHorse("h1", "SuperHorse", "owner1")
	horse.FitnessCeiling = 5.0
	horse.Races = 100
	horse.Wins = 100
	horse.ELO = 5000

	score := CalcSapphoScore(horse)
	if score > 12.0 {
		t.Errorf("SapphoScore = %v, should be capped at 12.0", score)
	}
}

func TestCalcSapphoScore_LowELO(t *testing.T) {
	horse := makeTestHorse("h1", "LowELO", "owner1")
	horse.ELO = 500 // below eloFloor of 800 → eloNorm clamped to 0.0

	score := CalcSapphoScore(horse)

	// eloNorm = (500-800)/(2400-800) = -300/1600 < 0 → clamped to 0.0
	// fitComp=0.8, winRate=0.3
	// raw = 0.8*0.4 + 0.3*0.3 + 0*0.3 = 0.32 + 0.09 = 0.41
	// score = 0.41 * 12 = 4.92
	expected := 4.92
	if math.Abs(score-expected) > 0.01 {
		t.Errorf("lowELO SapphoScore = %v, want ~%v", score, expected)
	}
}

// ---------------------------------------------------------------------------
// ELOUpdate
// ---------------------------------------------------------------------------

func TestELOUpdate_EqualRatings(t *testing.T) {
	winner := makeTestHorse("w", "Winner", "o1")
	loser := makeTestHorse("l", "Loser", "o2")
	winner.ELO = 1200
	loser.ELO = 1200

	newW, newL := ELOUpdate(winner, loser)

	// Equal ratings: expected=0.5, so winner gains K*0.5=16, loser loses 16
	expectedW := 1200.0 + 16.0
	expectedL := 1200.0 - 16.0
	if math.Abs(newW-expectedW) > 0.01 {
		t.Errorf("winner ELO = %v, want ~%v", newW, expectedW)
	}
	if math.Abs(newL-expectedL) > 0.01 {
		t.Errorf("loser ELO = %v, want ~%v", newL, expectedL)
	}
}

func TestELOUpdate_DoesNotMutate(t *testing.T) {
	winner := makeTestHorse("w", "Winner", "o1")
	loser := makeTestHorse("l", "Loser", "o2")
	winner.ELO = 1200
	loser.ELO = 1200

	ELOUpdate(winner, loser)

	if winner.ELO != 1200 {
		t.Error("ELOUpdate mutated winner's ELO")
	}
	if loser.ELO != 1200 {
		t.Error("ELOUpdate mutated loser's ELO")
	}
}

func TestELOUpdate_UpsetWin(t *testing.T) {
	winner := makeTestHorse("w", "Underdog", "o1")
	loser := makeTestHorse("l", "Favorite", "o2")
	winner.ELO = 1000
	loser.ELO = 1400

	newW, newL := ELOUpdate(winner, loser)

	// Big upset → winner gains more, loser loses more
	winGain := newW - winner.ELO
	loseLoss := loser.ELO - newL

	if winGain <= 16 {
		t.Errorf("upset win gain = %v, expected > 16", winGain)
	}
	if loseLoss <= 16 {
		t.Errorf("upset loss = %v, expected > 16", loseLoss)
	}
}

func TestELOUpdate_ExpectedWin(t *testing.T) {
	winner := makeTestHorse("w", "Favorite", "o1")
	loser := makeTestHorse("l", "Underdog", "o2")
	winner.ELO = 1800
	loser.ELO = 1200

	newW, newL := ELOUpdate(winner, loser)

	// Expected win → winner gains little
	winGain := newW - winner.ELO
	if winGain >= 16 {
		t.Errorf("expected win gain = %v, expected < 16", winGain)
	}
	if newL >= loser.ELO {
		t.Errorf("loser ELO should decrease: before=%v, after=%v", loser.ELO, newL)
	}
}

func TestELOUpdate_Symmetry(t *testing.T) {
	// Total ELO change should sum to zero (zero-sum game)
	winner := makeTestHorse("w", "W", "o1")
	loser := makeTestHorse("l", "L", "o2")
	winner.ELO = 1300
	loser.ELO = 1100

	newW, newL := ELOUpdate(winner, loser)
	winDelta := newW - winner.ELO
	loseDelta := newL - loser.ELO

	// winDelta + loseDelta should ≈ 0
	if math.Abs(winDelta+loseDelta) > 0.01 {
		t.Errorf("ELO not zero-sum: winDelta=%v, loseDelta=%v, sum=%v",
			winDelta, loseDelta, winDelta+loseDelta)
	}
}
