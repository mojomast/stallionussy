package pedigreussy

import (
	"strings"
	"sync"
	"testing"

	"github.com/mojomast/stallionussy/internal/models"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makeHorse creates a minimal Horse with known genealogy for testing.
func makeHorse(id, name, sireID, mareID string, gen int) *models.Horse {
	g := make(models.Genome)
	for _, gt := range models.AllGeneTypes {
		g[gt] = models.Gene{Type: gt, AlleleA: models.AlleleA, AlleleB: models.AlleleB}
	}
	return &models.Horse{
		ID:         id,
		Name:       name,
		SireID:     sireID,
		MareID:     mareID,
		Generation: gen,
		ELO:        1200,
		Age:        3,
		Genome:     g,
	}
}

// makeGetHorse builds a horse lookup function from a map of ID -> Horse.
func makeGetHorse(horses map[string]*models.Horse) func(string) (*models.Horse, error) {
	return func(id string) (*models.Horse, error) {
		h, ok := horses[id]
		if !ok {
			return nil, nil
		}
		return h, nil
	}
}

// buildHorseMap creates a simple lineage:
//
//	foal (gen 2) -> sire (gen 1), mare (gen 1)
//	sire -> grandsire, grandmare
//	mare -> grandsire2, grandmare2
func buildSimpleLineage() (map[string]*models.Horse, *models.Horse) {
	grandsire := makeHorse("gs", "GrandSire", "", "", 0)
	grandmare := makeHorse("gm", "GrandMare", "", "", 0)
	grandsire2 := makeHorse("gs2", "GrandSire2", "", "", 0)
	grandmare2 := makeHorse("gm2", "GrandMare2", "", "", 0)
	sire := makeHorse("s", "Sire", "gs", "gm", 1)
	mare := makeHorse("m", "Mare", "gs2", "gm2", 1)
	foal := makeHorse("f", "Foal", "s", "m", 2)

	horses := map[string]*models.Horse{
		"gs": grandsire, "gm": grandmare,
		"gs2": grandsire2, "gm2": grandmare2,
		"s": sire, "m": mare,
		"f": foal,
	}
	return horses, foal
}

// buildInbredLineage creates a lineage with shared ancestor:
//
//	foal -> sireA, mareB
//	sireA -> sharedAncestor, mA
//	mareB -> sharedAncestor, mB
func buildInbredLineage() (map[string]*models.Horse, *models.Horse) {
	shared := makeHorse("shared", "SharedAncestor", "", "", 0)
	mA := makeHorse("mA", "MotherA", "", "", 0)
	mB := makeHorse("mB", "MotherB", "", "", 0)
	sireA := makeHorse("sA", "SireA", "shared", "mA", 1)
	mareB := makeHorse("mB1", "MareB", "shared", "mB", 1)
	foal := makeHorse("inbred-foal", "InbredFoal", "sA", "mB1", 2)

	horses := map[string]*models.Horse{
		"shared": shared, "mA": mA, "mB": mB,
		"sA": sireA, "mB1": mareB,
		"inbred-foal": foal,
	}
	return horses, foal
}

// ---------------------------------------------------------------------------
// PedigreeEngine creation
// ---------------------------------------------------------------------------

func TestNewPedigreeEngine(t *testing.T) {
	pe := NewPedigreeEngine(func(id string) (*models.Horse, error) { return nil, nil })
	if pe == nil {
		t.Fatal("NewPedigreeEngine returned nil")
	}
}

func TestNewPedigreeEngine_NilLookup(t *testing.T) {
	pe := NewPedigreeEngine(nil)
	if pe == nil {
		t.Fatal("NewPedigreeEngine returned nil with nil lookup")
	}
}

// ---------------------------------------------------------------------------
// BuildPedigree
// ---------------------------------------------------------------------------

func TestBuildPedigree_SimpleLineage(t *testing.T) {
	horses, foal := buildSimpleLineage()
	pe := NewPedigreeEngine(makeGetHorse(horses))

	tree, err := pe.BuildPedigree(foal.ID, 3)
	if err != nil {
		t.Fatalf("BuildPedigree error: %v", err)
	}
	if tree == nil {
		t.Fatal("tree is nil")
	}

	// Root should be the foal
	if tree.Horse.ID != "f" {
		t.Errorf("root horse ID = %q, want f", tree.Horse.ID)
	}
	if tree.Generation != 0 {
		t.Errorf("root generation = %d, want 0", tree.Generation)
	}

	// Sire branch
	if tree.Sire == nil {
		t.Fatal("sire is nil")
	}
	if tree.Sire.Horse.Name != "Sire" {
		t.Errorf("sire name = %q, want Sire", tree.Sire.Horse.Name)
	}
	if tree.Sire.Generation != 1 {
		t.Errorf("sire generation = %d, want 1", tree.Sire.Generation)
	}

	// Mare branch
	if tree.Mare == nil {
		t.Fatal("mare is nil")
	}
	if tree.Mare.Horse.Name != "Mare" {
		t.Errorf("mare name = %q, want Mare", tree.Mare.Horse.Name)
	}

	// Grandparent on sire side
	if tree.Sire.Sire == nil {
		t.Fatal("sire's sire is nil")
	}
	if tree.Sire.Sire.Horse.Name != "GrandSire" {
		t.Errorf("grandsire name = %q", tree.Sire.Sire.Horse.Name)
	}
}

func TestBuildPedigree_DepthLimit(t *testing.T) {
	horses, foal := buildSimpleLineage()
	pe := NewPedigreeEngine(makeGetHorse(horses))

	// Depth 1 should only show the root + parents (no grandparents)
	tree, err := pe.BuildPedigree(foal.ID, 1)
	if err != nil {
		t.Fatalf("BuildPedigree error: %v", err)
	}

	if tree.Sire == nil {
		t.Fatal("sire should exist at depth 1")
	}
	// At depth 1, sire's parents should be nil (not resolved)
	if tree.Sire.Sire != nil {
		t.Error("sire's sire should be nil at depth 1")
	}
	if tree.Sire.Mare != nil {
		t.Error("sire's mare should be nil at depth 1")
	}
}

func TestBuildPedigree_DepthZero(t *testing.T) {
	horses, foal := buildSimpleLineage()
	pe := NewPedigreeEngine(makeGetHorse(horses))

	tree, err := pe.BuildPedigree(foal.ID, 0)
	if err != nil {
		t.Fatalf("BuildPedigree error: %v", err)
	}
	if tree.Horse.ID != "f" {
		t.Errorf("root horse ID = %q, want f", tree.Horse.ID)
	}
	// No parents at depth 0
	if tree.Sire != nil {
		t.Error("sire should be nil at depth 0")
	}
	if tree.Mare != nil {
		t.Error("mare should be nil at depth 0")
	}
}

func TestBuildPedigree_EmptyHorseID(t *testing.T) {
	pe := NewPedigreeEngine(func(id string) (*models.Horse, error) { return nil, nil })

	tree, err := pe.BuildPedigree("", 3)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if tree != nil {
		t.Error("expected nil tree for empty horse ID")
	}
}

func TestBuildPedigree_NonexistentHorse(t *testing.T) {
	pe := NewPedigreeEngine(func(id string) (*models.Horse, error) { return nil, nil })

	tree, err := pe.BuildPedigree("doesnt-exist", 3)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if tree != nil {
		t.Error("expected nil tree for nonexistent horse")
	}
}

func TestBuildPedigree_OrphanHorse(t *testing.T) {
	orphan := makeHorse("orphan", "Orphan", "", "", 0)
	horses := map[string]*models.Horse{"orphan": orphan}
	pe := NewPedigreeEngine(makeGetHorse(horses))

	tree, err := pe.BuildPedigree("orphan", 5)
	if err != nil {
		t.Fatalf("BuildPedigree error: %v", err)
	}
	if tree == nil {
		t.Fatal("tree is nil")
	}
	if tree.Sire != nil {
		t.Error("orphan should have nil sire")
	}
	if tree.Mare != nil {
		t.Error("orphan should have nil mare")
	}
}

// ---------------------------------------------------------------------------
// CalcInbreedingCoefficient
// ---------------------------------------------------------------------------

func TestCalcInbreedingCoefficient_NoInbreeding(t *testing.T) {
	horses, foal := buildSimpleLineage()
	pe := NewPedigreeEngine(makeGetHorse(horses))

	coeff, err := pe.CalcInbreedingCoefficient(foal.ID)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if coeff != 0.0 {
		t.Errorf("expected 0 inbreeding, got %v", coeff)
	}
}

func TestCalcInbreedingCoefficient_WithSharedAncestor(t *testing.T) {
	horses, foal := buildInbredLineage()
	pe := NewPedigreeEngine(makeGetHorse(horses))

	coeff, err := pe.CalcInbreedingCoefficient(foal.ID)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if coeff <= 0.0 {
		t.Errorf("expected positive inbreeding coefficient, got %v", coeff)
	}
	if coeff > 1.0 {
		t.Errorf("inbreeding coefficient > 1.0: %v", coeff)
	}
}

func TestCalcInbreedingCoefficient_OrphanHorse(t *testing.T) {
	orphan := makeHorse("orphan", "Orphan", "", "", 0)
	horses := map[string]*models.Horse{"orphan": orphan}
	pe := NewPedigreeEngine(makeGetHorse(horses))

	coeff, err := pe.CalcInbreedingCoefficient("orphan")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if coeff != 0.0 {
		t.Errorf("orphan should have 0 inbreeding, got %v", coeff)
	}
}

func TestCalcInbreedingCoefficient_NonexistentHorse(t *testing.T) {
	pe := NewPedigreeEngine(func(id string) (*models.Horse, error) { return nil, nil })

	_, err := pe.CalcInbreedingCoefficient("nope")
	if err == nil {
		t.Error("expected error for nonexistent horse")
	}
}

// ---------------------------------------------------------------------------
// InbreedingPenalty
// ---------------------------------------------------------------------------

func TestInbreedingPenalty(t *testing.T) {
	tests := []struct {
		coeff float64
		want  float64
	}{
		{0.0, 1.0},
		{0.05, 1.0},
		{0.09, 1.0},
		{0.10, 0.95},
		{0.20, 0.95},
		{0.24, 0.95},
		{0.25, 0.85},
		{0.40, 0.85},
		{0.49, 0.85},
		{0.50, 0.70},
		{0.80, 0.70},
		{1.0, 0.70},
	}

	for _, tt := range tests {
		got := InbreedingPenalty(tt.coeff)
		if got != tt.want {
			t.Errorf("InbreedingPenalty(%v) = %v, want %v", tt.coeff, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// PedigreeToASCII
// ---------------------------------------------------------------------------

func TestPedigreeToASCII_NilNode(t *testing.T) {
	result := PedigreeToASCII(nil, 3)
	if result != "<empty pedigree>" {
		t.Errorf("expected <empty pedigree>, got %q", result)
	}
}

func TestPedigreeToASCII_NilHorse(t *testing.T) {
	node := &PedigreeNode{Horse: nil}
	result := PedigreeToASCII(node, 3)
	if result != "<empty pedigree>" {
		t.Errorf("expected <empty pedigree>, got %q", result)
	}
}

func TestPedigreeToASCII_SimpleTree(t *testing.T) {
	horses, foal := buildSimpleLineage()
	pe := NewPedigreeEngine(makeGetHorse(horses))

	tree, err := pe.BuildPedigree(foal.ID, 2)
	if err != nil {
		t.Fatalf("BuildPedigree error: %v", err)
	}

	ascii := PedigreeToASCII(tree, 2)
	if ascii == "" {
		t.Fatal("PedigreeToASCII returned empty string")
	}
	if ascii == "<empty pedigree>" {
		t.Fatal("PedigreeToASCII returned empty pedigree for valid tree")
	}

	// Should contain the foal's name
	if !strings.Contains(ascii, "Foal") {
		t.Errorf("ASCII tree doesn't contain root horse name: %s", ascii)
	}
	// Should contain sire and mare labels
	if !strings.Contains(ascii, "Sire") {
		t.Errorf("ASCII tree doesn't contain Sire: %s", ascii)
	}
	if !strings.Contains(ascii, "Mare") {
		t.Errorf("ASCII tree doesn't contain Mare: %s", ascii)
	}
}

func TestPedigreeToASCII_OrphanHorse(t *testing.T) {
	orphan := makeHorse("orphan", "LonelyOrphan", "", "", 0)
	node := &PedigreeNode{Horse: orphan, Generation: 0}

	ascii := PedigreeToASCII(node, 3)
	if !strings.Contains(ascii, "LonelyOrphan") {
		t.Errorf("expected orphan name in output: %s", ascii)
	}
}

// ---------------------------------------------------------------------------
// PedigreeToJSON
// ---------------------------------------------------------------------------

func TestPedigreeToJSON(t *testing.T) {
	horses, foal := buildSimpleLineage()
	pe := NewPedigreeEngine(makeGetHorse(horses))

	tree, _ := pe.BuildPedigree(foal.ID, 1)

	data, err := PedigreeToJSON(tree)
	if err != nil {
		t.Fatalf("PedigreeToJSON error: %v", err)
	}
	if len(data) == 0 {
		t.Error("PedigreeToJSON returned empty data")
	}
	// Should be valid JSON containing the horse name
	if !strings.Contains(string(data), "Foal") {
		t.Errorf("JSON doesn't contain horse name: %s", string(data))
	}
}

// ---------------------------------------------------------------------------
// CalcBloodlineBonus
// ---------------------------------------------------------------------------

func TestCalcBloodlineBonus_NoAncestors(t *testing.T) {
	horse := makeHorse("h1", "TestHorse", "", "", 0)
	horses := map[string]*models.Horse{"h1": horse}
	lookup := makeGetHorse(horses)

	bonus := CalcBloodlineBonus(horse, "", "", lookup)
	// No ancestors = base 1.0 (no legendary bonus, no inbreeding penalty)
	if bonus != 1.0 {
		t.Errorf("expected 1.0, got %v", bonus)
	}
}

func TestCalcBloodlineBonus_WithLegendaryAncestor(t *testing.T) {
	legendary := makeHorse("leg", "LegendaryHorse", "", "", 0)
	legendary.IsLegendary = true
	sire := makeHorse("s", "Sire", "leg", "", 1)
	horse := makeHorse("h", "TestHorse", "s", "", 2)

	horses := map[string]*models.Horse{
		"leg": legendary,
		"s":   sire,
		"h":   horse,
	}
	lookup := makeGetHorse(horses)

	bonus := CalcBloodlineBonus(horse, "s", "", lookup)
	// Should have a bonus > 1.0 from legendary ancestor
	if bonus <= 1.0 {
		t.Errorf("expected bonus > 1.0 with legendary ancestor, got %v", bonus)
	}
}

func TestCalcBloodlineBonus_WithTwoLegendaries(t *testing.T) {
	leg1 := makeHorse("leg1", "Legend1", "", "", 0)
	leg1.IsLegendary = true
	leg2 := makeHorse("leg2", "Legend2", "", "", 0)
	leg2.IsLegendary = true
	sire := makeHorse("s", "Sire", "leg1", "", 1)
	mare := makeHorse("m", "Mare", "leg2", "", 1)
	horse := makeHorse("h", "TestHorse", "s", "m", 2)

	horses := map[string]*models.Horse{
		"leg1": leg1, "leg2": leg2,
		"s": sire, "m": mare, "h": horse,
	}
	lookup := makeGetHorse(horses)

	bonus := CalcBloodlineBonus(horse, "s", "m", lookup)
	// 2 legendary = +2% each + 5% diversity = +9% = 1.09 (minus possible tiny inbreeding)
	if bonus < 1.05 {
		t.Errorf("expected bonus >= 1.05 with 2 legendaries, got %v", bonus)
	}
}

func TestCalcBloodlineBonus_NilLookup(t *testing.T) {
	horse := makeHorse("h", "TestHorse", "s", "m", 1)

	bonus := CalcBloodlineBonus(horse, "s", "m", nil)
	if bonus != 1.0 {
		t.Errorf("expected 1.0 with nil lookup, got %v", bonus)
	}
}

func TestCalcBloodlineBonus_NilHorse(t *testing.T) {
	horses := map[string]*models.Horse{}
	lookup := makeGetHorse(horses)

	bonus := CalcBloodlineBonus(nil, "", "", lookup)
	// Nil horse should not crash and return base value
	if bonus < 0.5 {
		t.Errorf("expected reasonable bonus, got %v", bonus)
	}
}

func TestCalcBloodlineBonus_FloorAt05(t *testing.T) {
	// Even with extreme conditions, bonus should not go below 0.5
	horse := makeHorse("h", "TestHorse", "", "", 0)
	horses := map[string]*models.Horse{"h": horse}
	lookup := makeGetHorse(horses)

	bonus := CalcBloodlineBonus(horse, "", "", lookup)
	if bonus < 0.5 {
		t.Errorf("bonus below floor: %v", bonus)
	}
}

// ---------------------------------------------------------------------------
// TradeManager
// ---------------------------------------------------------------------------

func TestNewTradeManager(t *testing.T) {
	tm := NewTradeManager()
	if tm == nil {
		t.Fatal("NewTradeManager returned nil")
	}
}

func TestTradeManager_CreateOffer(t *testing.T) {
	tm := NewTradeManager()

	offer := tm.CreateOffer("horse-1", "Lightning", "stable-A", "stable-B", 5000)
	if offer == nil {
		t.Fatal("CreateOffer returned nil")
	}
	if offer.ID == "" {
		t.Error("offer ID is empty")
	}
	if offer.HorseID != "horse-1" {
		t.Errorf("HorseID = %q, want horse-1", offer.HorseID)
	}
	if offer.HorseName != "Lightning" {
		t.Errorf("HorseName = %q, want Lightning", offer.HorseName)
	}
	if offer.FromStable != "stable-A" {
		t.Errorf("FromStable = %q, want stable-A", offer.FromStable)
	}
	if offer.ToStable != "stable-B" {
		t.Errorf("ToStable = %q, want stable-B", offer.ToStable)
	}
	if offer.Price != 5000 {
		t.Errorf("Price = %d, want 5000", offer.Price)
	}
	if offer.Status != "Pending" {
		t.Errorf("Status = %q, want Pending", offer.Status)
	}
	if offer.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
}

func TestTradeManager_AcceptOffer(t *testing.T) {
	tm := NewTradeManager()
	offer := tm.CreateOffer("h1", "Horse1", "sA", "sB", 1000)

	accepted, err := tm.AcceptOffer(offer.ID)
	if err != nil {
		t.Fatalf("AcceptOffer error: %v", err)
	}
	if accepted.Status != "Accepted" {
		t.Errorf("Status = %q, want Accepted", accepted.Status)
	}
}

func TestTradeManager_AcceptOffer_AlreadyAccepted(t *testing.T) {
	tm := NewTradeManager()
	offer := tm.CreateOffer("h1", "Horse1", "sA", "sB", 1000)
	tm.AcceptOffer(offer.ID)

	_, err := tm.AcceptOffer(offer.ID)
	if err == nil {
		t.Error("expected error when accepting already-accepted offer")
	}
}

func TestTradeManager_AcceptOffer_NotFound(t *testing.T) {
	tm := NewTradeManager()
	_, err := tm.AcceptOffer("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent offer")
	}
}

func TestTradeManager_RejectOffer(t *testing.T) {
	tm := NewTradeManager()
	offer := tm.CreateOffer("h1", "Horse1", "sA", "sB", 1000)

	err := tm.RejectOffer(offer.ID)
	if err != nil {
		t.Fatalf("RejectOffer error: %v", err)
	}

	retrieved, _ := tm.GetOffer(offer.ID)
	if retrieved.Status != "Rejected" {
		t.Errorf("Status = %q, want Rejected", retrieved.Status)
	}
}

func TestTradeManager_RejectOffer_AlreadyRejected(t *testing.T) {
	tm := NewTradeManager()
	offer := tm.CreateOffer("h1", "Horse1", "sA", "sB", 1000)
	tm.RejectOffer(offer.ID)

	err := tm.RejectOffer(offer.ID)
	if err == nil {
		t.Error("expected error when rejecting already-rejected offer")
	}
}

func TestTradeManager_RejectOffer_NotFound(t *testing.T) {
	tm := NewTradeManager()
	err := tm.RejectOffer("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent offer")
	}
}

func TestTradeManager_CancelOffer(t *testing.T) {
	tm := NewTradeManager()
	offer := tm.CreateOffer("h1", "Horse1", "sA", "sB", 1000)

	err := tm.CancelOffer(offer.ID)
	if err != nil {
		t.Fatalf("CancelOffer error: %v", err)
	}

	retrieved, _ := tm.GetOffer(offer.ID)
	if retrieved.Status != "Cancelled" {
		t.Errorf("Status = %q, want Cancelled", retrieved.Status)
	}
}

func TestTradeManager_CancelOffer_AlreadyCancelled(t *testing.T) {
	tm := NewTradeManager()
	offer := tm.CreateOffer("h1", "Horse1", "sA", "sB", 1000)
	tm.CancelOffer(offer.ID)

	err := tm.CancelOffer(offer.ID)
	if err == nil {
		t.Error("expected error when cancelling already-cancelled offer")
	}
}

func TestTradeManager_GetOffer(t *testing.T) {
	tm := NewTradeManager()
	offer := tm.CreateOffer("h1", "Horse1", "sA", "sB", 1000)

	retrieved, err := tm.GetOffer(offer.ID)
	if err != nil {
		t.Fatalf("GetOffer error: %v", err)
	}
	if retrieved.ID != offer.ID {
		t.Errorf("ID mismatch: %q vs %q", retrieved.ID, offer.ID)
	}
}

func TestTradeManager_GetOffer_NotFound(t *testing.T) {
	tm := NewTradeManager()
	_, err := tm.GetOffer("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent offer")
	}
}

func TestTradeManager_ListPendingOffers(t *testing.T) {
	tm := NewTradeManager()

	tm.CreateOffer("h1", "Horse1", "sA", "sB", 1000)
	tm.CreateOffer("h2", "Horse2", "sA", "sB", 2000)
	offer3 := tm.CreateOffer("h3", "Horse3", "sB", "sA", 3000) // Different direction
	tm.AcceptOffer(offer3.ID)                                  // Accept this one — should not appear

	pending := tm.ListPendingOffers("sB")
	if len(pending) != 2 {
		t.Errorf("expected 2 pending offers to sB, got %d", len(pending))
	}
}

func TestTradeManager_ListOutgoingOffers(t *testing.T) {
	tm := NewTradeManager()

	tm.CreateOffer("h1", "Horse1", "sA", "sB", 1000)
	tm.CreateOffer("h2", "Horse2", "sA", "sC", 2000)
	tm.CreateOffer("h3", "Horse3", "sB", "sA", 3000) // From sB, not sA

	outgoing := tm.ListOutgoingOffers("sA")
	if len(outgoing) != 2 {
		t.Errorf("expected 2 outgoing offers from sA, got %d", len(outgoing))
	}
}

func TestTradeManager_ListAllPending(t *testing.T) {
	tm := NewTradeManager()

	tm.CreateOffer("h1", "Horse1", "sA", "sB", 1000)
	tm.CreateOffer("h2", "Horse2", "sB", "sC", 2000)
	tm.CreateOffer("h3", "Horse3", "sC", "sD", 3000)

	// All pending
	all := tm.ListAllPending("")
	if len(all) != 3 {
		t.Errorf("expected 3 total pending, got %d", len(all))
	}

	// Filtered by sB (from or to)
	forSB := tm.ListAllPending("sB")
	if len(forSB) != 2 {
		t.Errorf("expected 2 pending involving sB, got %d", len(forSB))
	}
}

func TestTradeManager_ListPendingOffers_Empty(t *testing.T) {
	tm := NewTradeManager()
	pending := tm.ListPendingOffers("noone")
	if len(pending) != 0 {
		t.Errorf("expected 0 pending, got %d", len(pending))
	}
}

// ---------------------------------------------------------------------------
// TradeManager — concurrent access
// ---------------------------------------------------------------------------

func TestTradeManager_ConcurrentCreateAndList(t *testing.T) {
	tm := NewTradeManager()

	var wg sync.WaitGroup
	const n = 50

	// Create offers concurrently
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tm.CreateOffer("h", "Horse", "sA", "sB", int64(i*100))
		}(i)
	}
	wg.Wait()

	pending := tm.ListPendingOffers("sB")
	if len(pending) != n {
		t.Errorf("expected %d pending offers, got %d", n, len(pending))
	}
}

// ---------------------------------------------------------------------------
// Dynasty System — CalcDynastyScore
// ---------------------------------------------------------------------------

// mockStableManager implements the ListHorses interface for CalcDynastyScore.
type mockStableManager struct {
	horses map[string][]*models.Horse
}

func (m *mockStableManager) ListHorses(stableID string) []*models.Horse {
	return m.horses[stableID]
}

func TestCalcDynastyScore_EmptyStable(t *testing.T) {
	mgr := &mockStableManager{horses: map[string][]*models.Horse{
		"empty": {},
	}}

	info := CalcDynastyScore("empty", mgr)
	if info.TotalHorses != 0 {
		t.Errorf("TotalHorses = %d, want 0", info.TotalHorses)
	}
	if info.DynastyRating != "Backyard Breeders" {
		t.Errorf("DynastyRating = %q, want Backyard Breeders", info.DynastyRating)
	}
}

func TestCalcDynastyScore_SingleHorse(t *testing.T) {
	horse := makeHorse("h1", "SingleHorse", "", "", 0)
	horse.ELO = 1200
	horse.Wins = 5
	horse.Races = 10

	mgr := &mockStableManager{horses: map[string][]*models.Horse{
		"s1": {horse},
	}}

	info := CalcDynastyScore("s1", mgr)
	if info.TotalHorses != 1 {
		t.Errorf("TotalHorses = %d, want 1", info.TotalHorses)
	}
	if info.AverageELO != 1200 {
		t.Errorf("AverageELO = %v, want 1200", info.AverageELO)
	}
	if info.BestHorse != "SingleHorse" {
		t.Errorf("BestHorse = %q, want SingleHorse", info.BestHorse)
	}
}

func TestCalcDynastyScore_MultipleGenerations(t *testing.T) {
	h0 := makeHorse("h0", "Founder", "", "", 0)
	h0.ELO = 1300
	h0.Wins = 10
	h0.Races = 20

	h1 := makeHorse("h1", "Child", "h0", "", 1)
	h1.ELO = 1400
	h1.Wins = 15
	h1.Races = 25

	h2 := makeHorse("h2", "Grandchild", "h1", "", 2)
	h2.ELO = 1500
	h2.Wins = 20
	h2.Races = 30

	mgr := &mockStableManager{horses: map[string][]*models.Horse{
		"s1": {h0, h1, h2},
	}}

	info := CalcDynastyScore("s1", mgr)
	if info.TotalHorses != 3 {
		t.Errorf("TotalHorses = %d, want 3", info.TotalHorses)
	}
	if info.TotalGenerations != 3 {
		t.Errorf("TotalGenerations = %d, want 3", info.TotalGenerations)
	}
	if info.OldestLineage != 2 {
		t.Errorf("OldestLineage = %d, want 2", info.OldestLineage)
	}
	if info.BestHorse != "Grandchild" {
		t.Errorf("BestHorse = %q, want Grandchild", info.BestHorse)
	}
	if info.BloodlineStrength < 0 || info.BloodlineStrength > 1.0 {
		t.Errorf("BloodlineStrength out of range: %v", info.BloodlineStrength)
	}
	if info.DynastyRating == "" {
		t.Error("DynastyRating is empty")
	}
}

func TestCalcDynastyScore_WithLegendaries(t *testing.T) {
	h1 := makeHorse("h1", "LegendaryHorse", "", "", 0)
	h1.ELO = 1800
	h1.IsLegendary = true
	h1.Wins = 30
	h1.Races = 40

	h2 := makeHorse("h2", "NormalHorse", "", "", 0)
	h2.ELO = 1100
	h2.Wins = 5
	h2.Races = 20

	mgr := &mockStableManager{horses: map[string][]*models.Horse{
		"s1": {h1, h2},
	}}

	info := CalcDynastyScore("s1", mgr)
	if info.LegendaryCount != 1 {
		t.Errorf("LegendaryCount = %d, want 1", info.LegendaryCount)
	}
	if len(info.FamousAncestors) != 1 {
		t.Errorf("FamousAncestors count = %d, want 1", len(info.FamousAncestors))
	}
	if info.FamousAncestors[0] != "LegendaryHorse" {
		t.Errorf("FamousAncestors[0] = %q, want LegendaryHorse", info.FamousAncestors[0])
	}
}

func TestCalcDynastyScore_BloodlineStrengthClamped(t *testing.T) {
	// Very low ELO horses — strength should be clamped to 0.
	h := makeHorse("h", "BadHorse", "", "", 0)
	h.ELO = 500
	h.Wins = 0
	h.Races = 50

	mgr := &mockStableManager{horses: map[string][]*models.Horse{
		"s1": {h},
	}}

	info := CalcDynastyScore("s1", mgr)
	if info.BloodlineStrength < 0 {
		t.Errorf("BloodlineStrength should be >= 0, got %v", info.BloodlineStrength)
	}
}

// ---------------------------------------------------------------------------
// dynastyTier
// ---------------------------------------------------------------------------

func TestDynastyTier(t *testing.T) {
	tests := []struct {
		strength float64
		want     string
	}{
		{0.0, "Backyard Breeders"},
		{0.19, "Backyard Breeders"},
		{0.20, "Respectable Ranch"},
		{0.39, "Respectable Ranch"},
		{0.40, "Distinguished Stable"},
		{0.59, "Distinguished Stable"},
		{0.60, "Elite Bloodline"},
		{0.79, "Elite Bloodline"},
		{0.80, "Legendary Dynasty"},
		{0.94, "Legendary Dynasty"},
		{0.95, "The Yogurt's Chosen"},
		{1.0, "The Yogurt's Chosen"},
	}

	for _, tt := range tests {
		got := dynastyTier(tt.strength)
		if got != tt.want {
			t.Errorf("dynastyTier(%v) = %q, want %q", tt.strength, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// formatNodeLabel
// ---------------------------------------------------------------------------

func TestFormatNodeLabel_NilNode(t *testing.T) {
	got := formatNodeLabel(nil, "Sire")
	if got != "Unknown" {
		t.Errorf("formatNodeLabel(nil) = %q, want Unknown", got)
	}
}

func TestFormatNodeLabel_NilHorse(t *testing.T) {
	node := &PedigreeNode{Horse: nil}
	got := formatNodeLabel(node, "Mare")
	if got != "Unknown" {
		t.Errorf("formatNodeLabel(nil horse) = %q, want Unknown", got)
	}
}

func TestFormatNodeLabel_WithRole(t *testing.T) {
	horse := makeHorse("h", "Lightning", "", "", 0)
	horse.ELO = 1500
	node := &PedigreeNode{Horse: horse, Generation: 0}

	got := formatNodeLabel(node, "Sire")
	if !strings.Contains(got, "Sire: Lightning") {
		t.Errorf("expected 'Sire: Lightning' in %q", got)
	}
	if !strings.Contains(got, "ELO") {
		t.Errorf("expected ELO in gen 0 label: %q", got)
	}
}

func TestFormatNodeLabel_NoRole(t *testing.T) {
	horse := makeHorse("h", "Lightning", "", "", 0)
	node := &PedigreeNode{Horse: horse, Generation: 0}

	got := formatNodeLabel(node, "")
	if strings.Contains(got, ":") && strings.Contains(got, "Sire") {
		t.Errorf("should not contain role prefix: %q", got)
	}
	if !strings.Contains(got, "Lightning") {
		t.Errorf("expected horse name in %q", got)
	}
}

func TestFormatNodeLabel_DeepGeneration(t *testing.T) {
	horse := makeHorse("h", "DeepHorse", "", "", 0)
	node := &PedigreeNode{Horse: horse, Generation: 3}

	got := formatNodeLabel(node, "Sire")
	// Gen > 1 should show gen number but not ELO
	if !strings.Contains(got, "Gen 3") {
		t.Errorf("expected 'Gen 3' in %q", got)
	}
	if strings.Contains(got, "ELO") {
		t.Errorf("deep generation should not show ELO: %q", got)
	}
}
