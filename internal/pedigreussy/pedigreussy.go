// Package pedigreussy implements pedigree visualization, dynasty scoring,
// horse trading, and bloodline bonus calculations for StallionUSSY.
// It provides ancestry tree construction, inbreeding analysis, ASCII pedigree
// rendering, stable dynasty ratings, and a concurrency-safe trade offer system.
package pedigreussy

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mojomast/stallionussy/internal/models"
)

// ---------------------------------------------------------------------------
// Pedigree Tree
// ---------------------------------------------------------------------------

// PedigreeNode represents a single node in a horse's family tree.
// It links to the sire (father) and mare (mother) recursively and tracks
// the generation depth relative to the root horse.
type PedigreeNode struct {
	Horse      *models.Horse   `json:"horse"`
	Sire       *PedigreeNode   `json:"sire,omitempty"`
	Mare       *PedigreeNode   `json:"mare,omitempty"`
	Children   []*PedigreeNode `json:"children,omitempty"`
	Generation int             `json:"generation"`
	Inbreeding float64         `json:"inbreeding_coefficient"`
}

// PedigreeEngine provides pedigree construction and analysis. It uses a
// caller-supplied horseLookup function to resolve horse IDs to Horse structs,
// decoupling the pedigree logic from the storage layer.
type PedigreeEngine struct {
	mu     sync.RWMutex
	horses func(id string) (*models.Horse, error) // lookup function
}

// NewPedigreeEngine creates a PedigreeEngine that resolves horse IDs via the
// provided lookup function. The lookup should be concurrency-safe if the
// engine will be used from multiple goroutines.
func NewPedigreeEngine(horseLookup func(id string) (*models.Horse, error)) *PedigreeEngine {
	return &PedigreeEngine{
		horses: horseLookup,
	}
}

// BuildPedigree recursively constructs a pedigree tree for the given horse,
// walking back through sire/mare links up to `depth` generations.
// The root horse is at generation 0, parents at 1, grandparents at 2, etc.
// Missing or unknown parents result in a nil branch — no error is returned
// for absent ancestors (only for a missing root horse).
func (pe *PedigreeEngine) BuildPedigree(horseID string, depth int) (*PedigreeNode, error) {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	return pe.buildNode(horseID, 0, depth)
}

// buildNode is the recursive workhorse (pun intended) for BuildPedigree.
// It creates a PedigreeNode for the given horse ID at the specified generation
// and recurses into sire/mare if we haven't exceeded the requested depth.
func (pe *PedigreeEngine) buildNode(horseID string, gen, maxDepth int) (*PedigreeNode, error) {
	if horseID == "" {
		return nil, nil
	}

	horse, err := pe.horses(horseID)
	if err != nil || horse == nil {
		// Parent not found — this branch ends here. Not an error for ancestry
		// lookups since founders have no parents in the database.
		return nil, nil
	}

	node := &PedigreeNode{
		Horse:      horse,
		Generation: gen,
	}

	// Recurse into parents if we haven't reached the depth limit.
	if gen < maxDepth {
		// Sire branch (father)
		if horse.SireID != "" {
			sireNode, _ := pe.buildNode(horse.SireID, gen+1, maxDepth)
			node.Sire = sireNode
		}
		// Mare branch (mother)
		if horse.MareID != "" {
			mareNode, _ := pe.buildNode(horse.MareID, gen+1, maxDepth)
			node.Mare = mareNode
		}
	}

	return node, nil
}

// ---------------------------------------------------------------------------
// Inbreeding Coefficient
// ---------------------------------------------------------------------------

// CalcInbreedingCoefficient calculates a simple inbreeding coefficient for the
// given horse by building a 5-generation pedigree and scanning for ancestors
// that appear more than once.
//
// Coefficient = shared ancestor appearances / total ancestor slots in tree.
// 0.0 = no inbreeding, 1.0 = maximum inbreeding.
func (pe *PedigreeEngine) CalcInbreedingCoefficient(horseID string) (float64, error) {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	tree, err := pe.buildNode(horseID, 0, 5)
	if err != nil {
		return 0.0, err
	}
	if tree == nil {
		return 0.0, fmt.Errorf("horse not found: %s", horseID)
	}

	// Collect all ancestor IDs (excluding the root horse itself).
	ancestors := make(map[string]int) // ID -> count of appearances
	totalSlots := 0
	pe.collectAncestors(tree.Sire, ancestors, &totalSlots)
	pe.collectAncestors(tree.Mare, ancestors, &totalSlots)

	if totalSlots == 0 {
		return 0.0, nil // no ancestors — no inbreeding possible
	}

	// Count how many ancestor appearances are duplicates.
	sharedAppearances := 0
	for _, count := range ancestors {
		if count > 1 {
			sharedAppearances += count - 1 // extra appearances beyond the first
		}
	}

	uniqueAncestors := len(ancestors)
	if uniqueAncestors == 0 {
		return 0.0, nil
	}

	// Coefficient: shared (duplicate) appearances / total slots visited.
	coefficient := float64(sharedAppearances) / float64(totalSlots)

	// Clamp to [0, 1].
	if coefficient > 1.0 {
		coefficient = 1.0
	}

	return coefficient, nil
}

// collectAncestors recursively walks a pedigree subtree, counting how many
// times each ancestor ID appears. totalSlots tracks the total number of
// ancestor positions visited (including duplicates).
func (pe *PedigreeEngine) collectAncestors(node *PedigreeNode, seen map[string]int, totalSlots *int) {
	if node == nil || node.Horse == nil {
		return
	}

	seen[node.Horse.ID]++
	*totalSlots++

	pe.collectAncestors(node.Sire, seen, totalSlots)
	pe.collectAncestors(node.Mare, seen, totalSlots)
}

// InbreedingPenalty converts an inbreeding coefficient into a fitness ceiling
// multiplier. Higher inbreeding = lower multiplier = weaker offspring.
//
//	0.00 - 0.10: no penalty     (1.00 multiplier)
//	0.10 - 0.25: mild penalty   (0.95 multiplier)
//	0.25 - 0.50: moderate        (0.85 multiplier)
//	0.50+:       severe          (0.70 multiplier) — "The gene pool is more of a gene puddle."
func InbreedingPenalty(coefficient float64) float64 {
	switch {
	case coefficient < 0.1:
		return 1.0
	case coefficient < 0.25:
		return 0.95
	case coefficient < 0.5:
		return 0.85
	default:
		return 0.70 // gene puddle territory
	}
}

// ---------------------------------------------------------------------------
// Pedigree Visualization
// ---------------------------------------------------------------------------

// PedigreeToASCII renders a text-based family tree for the given pedigree node.
// The depth parameter controls how many generations to render (0 = root only).
//
// Example output:
//
//	HORSE_NAME (Gen 0, ELO: 1234)
//	+-- Sire: SIRE_NAME (Gen 1, ELO: 1100)
//	|   +-- Sire: GRANDSIRE_NAME (Gen 2)
//	|   +-- Mare: GRANDMARE_NAME (Gen 2)
//	+-- Mare: MARE_NAME (Gen 1, ELO: 1050)
//	    +-- Sire: GRANDSIRE2_NAME (Gen 2)
//	    +-- Mare: GRANDMARE2_NAME (Gen 2)
func PedigreeToASCII(node *PedigreeNode, depth int) string {
	if node == nil || node.Horse == nil {
		return "<empty pedigree>"
	}

	var sb strings.Builder
	// Root line
	sb.WriteString(formatNodeLabel(node, ""))
	sb.WriteString("\n")

	// Render children (sire, then mare) with tree connectors.
	renderSubtree(&sb, node, depth, 0, "")

	return sb.String()
}

// renderSubtree recursively appends ASCII tree lines for a node's sire and mare.
func renderSubtree(sb *strings.Builder, node *PedigreeNode, maxDepth, currentDepth int, prefix string) {
	if node == nil || currentDepth >= maxDepth {
		return
	}

	// Determine which branches exist so we can pick the right connector.
	hasSire := node.Sire != nil && node.Sire.Horse != nil
	hasMare := node.Mare != nil && node.Mare.Horse != nil

	// Sire branch
	if hasSire {
		connector := "\u251c\u2500\u2500 " // "├── "
		childPrefix := "\u2502   "         // "│   "
		if !hasMare {
			connector = "\u2514\u2500\u2500 " // "└── "
			childPrefix = "    "
		}
		sb.WriteString(prefix + connector + formatNodeLabel(node.Sire, "Sire") + "\n")
		renderSubtree(sb, node.Sire, maxDepth, currentDepth+1, prefix+childPrefix)
	}

	// Mare branch
	if hasMare {
		connector := "\u2514\u2500\u2500 " // "└── "
		childPrefix := "    "
		sb.WriteString(prefix + connector + formatNodeLabel(node.Mare, "Mare") + "\n")
		renderSubtree(sb, node.Mare, maxDepth, currentDepth+1, prefix+childPrefix)
	}
}

// formatNodeLabel produces a display string for one pedigree node.
// If role is non-empty (e.g. "Sire", "Mare"), it's prefixed.
func formatNodeLabel(node *PedigreeNode, role string) string {
	if node == nil || node.Horse == nil {
		return "Unknown"
	}

	label := node.Horse.Name
	if role != "" {
		label = role + ": " + label
	}

	// Always show generation. Show ELO for generations 0 and 1 where it's
	// most interesting; deeper generations just show the gen number.
	if node.Generation <= 1 {
		return fmt.Sprintf("%s (Gen %d, ELO: %.0f)", label, node.Generation, node.Horse.ELO)
	}
	return fmt.Sprintf("%s (Gen %d)", label, node.Generation)
}

// PedigreeToJSON serializes a PedigreeNode tree to JSON bytes.
func PedigreeToJSON(node *PedigreeNode) ([]byte, error) {
	return json.MarshalIndent(node, "", "  ")
}

// ---------------------------------------------------------------------------
// Dynasty System
// ---------------------------------------------------------------------------

// DynastyInfo aggregates bloodline and breeding statistics for a stable,
// producing a composite dynasty rating.
type DynastyInfo struct {
	TotalHorses       int      `json:"total_horses"`
	TotalGenerations  int      `json:"total_generations"`
	OldestLineage     int      `json:"oldest_lineage"` // deepest generation number
	LegendaryCount    int      `json:"legendary_count"`
	AverageELO        float64  `json:"average_elo"`
	BestHorse         string   `json:"best_horse"`         // highest ELO horse name
	DynastyRating     string   `json:"dynasty_rating"`     // computed tier
	BloodlineStrength float64  `json:"bloodline_strength"` // 0-1 composite score
	FamousAncestors   []string `json:"famous_ancestors"`   // names of legendary horses in lineage
}

// CalcDynastyScore computes the dynasty rating for a stable by analyzing all
// of its horses' ELO, generation depth, legendary status, and win rate.
//
// The stableManager parameter must implement ListHorses(stableID) to return
// the stable's roster. This loose coupling avoids a direct import cycle with
// the stableussy package.
func CalcDynastyScore(stableID string, stableManager interface {
	ListHorses(string) []*models.Horse
}) DynastyInfo {
	horses := stableManager.ListHorses(stableID)

	info := DynastyInfo{
		TotalHorses:     len(horses),
		FamousAncestors: []string{},
	}

	if len(horses) == 0 {
		info.DynastyRating = "Backyard Breeders"
		return info
	}

	var (
		totalELO     float64
		totalWins    int
		totalRaces   int
		bestELO      float64
		bestName     string
		maxGen       int
		gensSeen     = make(map[int]bool)
		legendarySet = make(map[string]bool) // track unique legendary names
	)

	for _, h := range horses {
		totalELO += h.ELO
		totalWins += h.Wins
		totalRaces += h.Races

		if h.ELO > bestELO {
			bestELO = h.ELO
			bestName = h.Name
		}

		if h.Generation > maxGen {
			maxGen = h.Generation
		}
		gensSeen[h.Generation] = true

		if h.IsLegendary && !legendarySet[h.Name] {
			legendarySet[h.Name] = true
			info.FamousAncestors = append(info.FamousAncestors, h.Name)
			info.LegendaryCount++
		}
	}

	info.AverageELO = totalELO / float64(len(horses))
	info.BestHorse = bestName
	info.OldestLineage = maxGen
	info.TotalGenerations = len(gensSeen)

	// Win rate: 0 if no races have been run.
	winRate := 0.0
	if totalRaces > 0 {
		winRate = float64(totalWins) / float64(totalRaces)
	}

	// BloodlineStrength composite: 4 weighted factors.
	// (avgELO - 1000) / 500 * 0.3  — ELO contribution (centered at 1000, scales by 500)
	// (legendaryCount / totalHorses) * 0.3  — legendary density
	// (oldestLineage / 10.0) * 0.2  — generational depth
	// winRate * 0.2  — competitive success
	eloComponent := (info.AverageELO - 1000.0) / 500.0 * 0.3
	legendaryComponent := float64(info.LegendaryCount) / float64(info.TotalHorses) * 0.3
	lineageComponent := float64(info.OldestLineage) / 10.0 * 0.2
	winRateComponent := winRate * 0.2

	strength := eloComponent + legendaryComponent + lineageComponent + winRateComponent

	// Clamp to [0, 1].
	if strength < 0.0 {
		strength = 0.0
	}
	if strength > 1.0 {
		strength = 1.0
	}

	info.BloodlineStrength = strength
	info.DynastyRating = dynastyTier(strength)

	return info
}

// dynastyTier maps a BloodlineStrength score to a human-readable tier.
func dynastyTier(strength float64) string {
	switch {
	case strength >= 0.95:
		return "The Yogurt's Chosen"
	case strength >= 0.8:
		return "Legendary Dynasty"
	case strength >= 0.6:
		return "Elite Bloodline"
	case strength >= 0.4:
		return "Distinguished Stable"
	case strength >= 0.2:
		return "Respectable Ranch"
	default:
		return "Backyard Breeders"
	}
}

// ---------------------------------------------------------------------------
// Horse Trading
// ---------------------------------------------------------------------------

// TradeOffer is an alias for models.TradeOffer — the canonical trade type
// used by the repository layer. Previously pedigreussy had its own copy with
// different field names (FromStable/ToStable vs FromStableID/ToStableID),
// which caused persistence mismatches. Now unified.
type TradeOffer = models.TradeOffer

// TradeManager provides concurrency-safe trade offer management.
type TradeManager struct {
	mu     sync.RWMutex
	trades map[string]*TradeOffer
}

// NewTradeManager creates an empty TradeManager ready for use.
func NewTradeManager() *TradeManager {
	return &TradeManager{
		trades: make(map[string]*TradeOffer),
	}
}

// CreateOffer creates a new pending trade offer from one stable to another.
// Returns the newly created offer with a unique ID and "Pending" status.
func (tm *TradeManager) CreateOffer(horseID, horseName, fromStable, toStable string, price int64) *TradeOffer {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	now := time.Now()
	offer := &TradeOffer{
		ID:           uuid.New().String(),
		HorseID:      horseID,
		HorseName:    horseName,
		FromStableID: fromStable,
		ToStableID:   toStable,
		Price:        price,
		Status:       "Pending",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	tm.trades[offer.ID] = offer
	return offer
}

// AcceptOffer marks a pending trade offer as accepted. Returns the offer so
// the caller can execute the actual horse transfer and currency exchange.
// Returns an error if the offer is not found or is not in "Pending" status.
func (tm *TradeManager) AcceptOffer(offerID string) (*TradeOffer, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	offer, ok := tm.trades[offerID]
	if !ok {
		return nil, fmt.Errorf("trade offer not found: %s", offerID)
	}

	if offer.Status != "Pending" {
		return nil, fmt.Errorf("trade offer %s is not pending (status: %s)", offerID, offer.Status)
	}

	offer.Status = "Accepted"
	return offer, nil
}

// RejectOffer marks a pending trade offer as rejected.
// Returns an error if the offer is not found or is not in "Pending" status.
func (tm *TradeManager) RejectOffer(offerID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	offer, ok := tm.trades[offerID]
	if !ok {
		return fmt.Errorf("trade offer not found: %s", offerID)
	}

	if offer.Status != "Pending" {
		return fmt.Errorf("trade offer %s is not pending (status: %s)", offerID, offer.Status)
	}

	offer.Status = "Rejected"
	return nil
}

// CancelOffer marks a pending trade offer as cancelled (typically by the sender).
// Returns an error if the offer is not found or is not in "Pending" status.
func (tm *TradeManager) CancelOffer(offerID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	offer, ok := tm.trades[offerID]
	if !ok {
		return fmt.Errorf("trade offer not found: %s", offerID)
	}

	if offer.Status != "Pending" {
		return fmt.Errorf("trade offer %s is not pending (status: %s)", offerID, offer.Status)
	}

	offer.Status = "Cancelled"
	return nil
}

// ListPendingOffers returns all pending trade offers directed TO the given
// stable (i.e., offers this stable can accept or reject).
func (tm *TradeManager) ListPendingOffers(stableID string) []*TradeOffer {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	var result []*TradeOffer
	for _, offer := range tm.trades {
		if offer.ToStableID == stableID && offer.Status == "Pending" {
			result = append(result, offer)
		}
	}
	return result
}

// ListOutgoingOffers returns all pending trade offers sent FROM the given
// stable (i.e., offers this stable is waiting on).
func (tm *TradeManager) ListOutgoingOffers(stableID string) []*TradeOffer {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	var result []*TradeOffer
	for _, offer := range tm.trades {
		if offer.FromStableID == stableID && offer.Status == "Pending" {
			result = append(result, offer)
		}
	}
	return result
}

// ListAllPending returns all pending trade offers, optionally filtered by
// stableID (either from or to). If stableID is empty, all pending are returned.
func (tm *TradeManager) ListAllPending(stableID string) []*TradeOffer {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	var result []*TradeOffer
	for _, offer := range tm.trades {
		if offer.Status != "Pending" {
			continue
		}
		if stableID == "" || offer.FromStableID == stableID || offer.ToStableID == stableID {
			result = append(result, offer)
		}
	}
	return result
}

// ImportOffer adds an existing trade offer (e.g. loaded from DB) directly
// into the in-memory registry. If an offer with the same ID already exists
// it is replaced. Used during startup to hydrate state from the database.
func (tm *TradeManager) ImportOffer(offer *TradeOffer) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.trades[offer.ID] = offer
}

// GetOffer retrieves a trade offer by its ID regardless of status.
// Returns an error if the offer is not found.
func (tm *TradeManager) GetOffer(id string) (*TradeOffer, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	offer, ok := tm.trades[id]
	if !ok {
		return nil, fmt.Errorf("trade offer not found: %s", id)
	}
	return offer, nil
}

// ---------------------------------------------------------------------------
// Bloodline Bonus Calculator
// ---------------------------------------------------------------------------

// CalcBloodlineBonus computes a fitness ceiling multiplier based on the
// horse's ancestry. Legendary ancestors within 3 generations provide bonuses,
// while inbreeding provides a penalty.
//
// Bonus rules:
//   - Each legendary ancestor within 3 generations: +2% ceiling bonus
//   - Two or more different legendary ancestors: +5% diversity bonus
//   - E-008 (Lot 6) in lineage: random +/- 10% (anomalous energy)
//   - Inbreeding penalty is subtracted
//
// Returns a multiplier (e.g. 1.04 = 4% bonus, 0.96 = 4% penalty).
func CalcBloodlineBonus(horse *models.Horse, sireID, mareID string, horseLookup func(string) (*models.Horse, error)) float64 {
	bonus := 1.0

	if horseLookup == nil {
		return bonus
	}

	// Build a mini-engine just for this calculation.
	pe := &PedigreeEngine{horses: horseLookup}

	// Collect unique legendary ancestors within 3 generations of the parents.
	// We check the sire and mare lines separately (the horse itself is gen 0).
	legendaryIDs := make(map[string]bool)
	hasE008 := false

	// Scan sire lineage (up to 3 generations back from the horse).
	if sireID != "" {
		sireTree, _ := pe.buildNode(sireID, 1, 3)
		scanForLegendaries(sireTree, legendaryIDs, &hasE008)
	}

	// Scan mare lineage (up to 3 generations back from the horse).
	if mareID != "" {
		mareTree, _ := pe.buildNode(mareID, 1, 3)
		scanForLegendaries(mareTree, legendaryIDs, &hasE008)
	}

	// +2% per legendary ancestor within 3 generations.
	legendaryCount := len(legendaryIDs)
	bonus += float64(legendaryCount) * 0.02

	// +5% diversity bonus for 2+ different legendary ancestors.
	if legendaryCount >= 2 {
		bonus += 0.05
	}

	// E-008 anomalous energy: random +/- 10%.
	// The yogurt is unpredictable.
	if hasE008 {
		anomaly := (rand.Float64() * 0.20) - 0.10 // range [-0.10, +0.10)
		bonus += anomaly
	}

	// Subtract inbreeding penalty.
	// Build the inbreeding coefficient from the horse's own ID if available,
	// otherwise approximate from sire/mare overlap.
	if horse != nil && horse.ID != "" {
		coeff, err := pe.calcInbreedingInternal(horse.ID)
		if err == nil && coeff > 0 {
			penalty := InbreedingPenalty(coeff)
			// penalty is a multiplier (e.g. 0.95), so the loss is 1.0 - penalty.
			bonus -= (1.0 - penalty)
		}
	}

	// Floor at a reasonable minimum — even the most inbred horse with bad
	// luck shouldn't go negative.
	if bonus < 0.5 {
		bonus = 0.5
	}

	// Round to 4 decimal places to avoid floating point noise.
	bonus = math.Round(bonus*10000) / 10000

	return bonus
}

// calcInbreedingInternal is an unlocked version of CalcInbreedingCoefficient
// for internal use (avoids double-locking when called from CalcBloodlineBonus).
func (pe *PedigreeEngine) calcInbreedingInternal(horseID string) (float64, error) {
	tree, err := pe.buildNode(horseID, 0, 5)
	if err != nil {
		return 0.0, err
	}
	if tree == nil {
		return 0.0, fmt.Errorf("horse not found: %s", horseID)
	}

	ancestors := make(map[string]int)
	totalSlots := 0
	pe.collectAncestors(tree.Sire, ancestors, &totalSlots)
	pe.collectAncestors(tree.Mare, ancestors, &totalSlots)

	if totalSlots == 0 {
		return 0.0, nil
	}

	sharedAppearances := 0
	for _, count := range ancestors {
		if count > 1 {
			sharedAppearances += count - 1
		}
	}

	coefficient := float64(sharedAppearances) / float64(totalSlots)
	if coefficient > 1.0 {
		coefficient = 1.0
	}

	return coefficient, nil
}

// scanForLegendaries recursively scans a pedigree subtree, recording the IDs
// of any legendary horses found and flagging the presence of E-008's Chosen
// (Lot 6, the anomalous horse whose MUT locus reads BB in impossible patterns).
func scanForLegendaries(node *PedigreeNode, legendaryIDs map[string]bool, hasE008 *bool) {
	if node == nil || node.Horse == nil {
		return
	}

	if node.Horse.IsLegendary {
		legendaryIDs[node.Horse.ID] = true
		// E-008's Chosen is lot 6 — the anomalous yogurt horse.
		if node.Horse.LotNumber == 6 {
			*hasE008 = true
		}
	}

	scanForLegendaries(node.Sire, legendaryIDs, hasE008)
	scanForLegendaries(node.Mare, legendaryIDs, hasE008)
}
