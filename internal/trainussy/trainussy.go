// Package trainussy implements the training, trait, and aging systems for
// StallionUSSY. It handles workout XP/fitness gains, fatigue management,
// injury rolls, trait assignment at birth and milestones, horse aging with
// fitness ceiling decay, and retirement logic.
package trainussy

import (
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mojomast/stallionussy/internal/models"
)

// ---------------------------------------------------------------------------
// Trainer — the core training engine
// ---------------------------------------------------------------------------

// Trainer manages training sessions, trait pools, and session history.
// All methods are safe for concurrent use.
type Trainer struct {
	mu        sync.RWMutex
	sessions  map[string][]*models.TrainingSession // horseID -> sessions
	traitPool []models.Trait                       // all possible traits
}

// NewTrainer creates a Trainer with an initialised trait pool and empty
// session history.
func NewTrainer() *Trainer {
	return &Trainer{
		sessions:  make(map[string][]*models.TrainingSession),
		traitPool: InitTraitPool(),
	}
}

// ---------------------------------------------------------------------------
// Constants — XP bases, fatigue deltas, injury notes
// ---------------------------------------------------------------------------

// baseXP maps each workout type to its base XP reward.
var baseXP = map[models.WorkoutType]float64{
	models.WorkoutSprint:    10,
	models.WorkoutEndurance: 12,
	models.WorkoutMentalRep: 8,
	models.WorkoutMudRun:    15,
	models.WorkoutRecovery:  5, // RestDay
	models.WorkoutGeneral:   7,
}

// fatigueDelta maps each workout type to its fatigue change.
// Positive values add fatigue; negative values recover it.
var fatigueDelta = map[models.WorkoutType]float64{
	models.WorkoutSprint:    15,
	models.WorkoutEndurance: 20,
	models.WorkoutMentalRep: 8,
	models.WorkoutMudRun:    15,
	models.WorkoutRecovery:  -30, // RestDay heals
	models.WorkoutGeneral:   10,
}

// injuryNotes are randomly selected when a horse gets injured during training.
// Each one is funnier than the last.
var injuryNotes = []string{
	"Pulled a hamstring doing the Macarena",
	"Tripped over a suspicious yogurt container",
	"Strained fetlock while attempting a backflip",
	"Panic attack during motivational speech",
	"Stepped on a LEGO in the stable",
	"Allergic reaction to artisanal oat milk",
	"Existential crisis mid-sprint",
	"Got into a fight with a goose",
	"Slipped on Jason Derulo's spilled smoothie",
	"Dr. Mittens sat on them for 47 minutes",
}

// retirementMessages are selected when a horse retires.
var retirementMessages = []string{
	"Has discovered a passion for competitive dressage",
	"Started a hummus food truck",
	"Became Dr. Mittens' personal chauffeur",
	"Enrolled in online coding bootcamp",
	"Went full cottagecore",
	"Accepted a position at B.U.R.P.",
	"Opened an artisanal yogurt boutique",
	"Became a motivational speaker for anxious foals",
	"Launched a podcast about oat milk alternatives",
	"Retired to a flannel farm in Vermont",
	"Now consults for the Bureau of Equine Anomalies",
	"Became Jason Derulo's personal horse",
}

// ---------------------------------------------------------------------------
// Training System
// ---------------------------------------------------------------------------

// Train runs a single training session for the given horse and workout type.
// It calculates XP, applies fitness gains with diminishing returns, updates
// fatigue, and rolls for injuries. The horse is mutated in place. Returns
// the session record.
func (t *Trainer) Train(horse *models.Horse, workout models.WorkoutType) *models.TrainingSession {
	t.mu.Lock()
	defer t.mu.Unlock()

	fitnessBefore := horse.CurrentFitness

	// ---- Calculate XP ----
	xp := calcXP(horse, workout)

	// ---- Calculate fitness gain with diminishing returns ----
	fitnessGain := calcFitnessGain(horse, workout, xp)

	// Apply fitness gain, capped at ceiling
	horse.CurrentFitness += fitnessGain
	if horse.CurrentFitness > horse.FitnessCeiling {
		horse.CurrentFitness = horse.FitnessCeiling
	}

	// Accumulate lifetime XP
	horse.TrainingXP += xp

	// ---- Apply fatigue ----
	delta, ok := fatigueDelta[workout]
	if !ok {
		delta = 10 // fallback
	}
	horse.Fatigue += delta
	if horse.Fatigue < 0 {
		horse.Fatigue = 0
	}
	if horse.Fatigue > 100 {
		horse.Fatigue = 100
	}

	// ---- Injury check ----
	injured, injuryNote := rollInjury(horse)

	session := &models.TrainingSession{
		ID:            uuid.New().String(),
		HorseID:       horse.ID,
		WorkoutType:   workout,
		XPGained:      xp,
		FitnessBefore: fitnessBefore,
		FitnessAfter:  horse.CurrentFitness,
		Injury:        injured,
		InjuryNote:    injuryNote,
		CreatedAt:     time.Now(),
	}

	// Store session history
	t.sessions[horse.ID] = append(t.sessions[horse.ID], session)

	return session
}

// calcXP computes XP gained for a workout, factoring in INT gene and fatigue.
//
//	Base XP × INT multiplier × fatigue penalty
//	  INT: AA=1.5x, AB=1.2x, BB=1.0x
//	  Fatigue >50: XP halved. >80: XP quartered.
func calcXP(horse *models.Horse, workout models.WorkoutType) float64 {
	xp := baseXP[workout]
	if xp == 0 {
		xp = 7 // fallback to General
	}

	// INT gene bonus
	intExpr := geneExpress(horse.Genome, models.GeneINT)
	switch intExpr {
	case "AA":
		xp *= 1.5
	case "AB":
		xp *= 1.2
		// BB: no multiplier (x1.0)
	}

	// Fatigue penalty
	if horse.Fatigue > 80 {
		xp *= 0.25 // quartered
	} else if horse.Fatigue > 50 {
		xp *= 0.5 // halved
	}

	return xp
}

// calcFitnessGain computes the actual fitness gain from XP with diminishing
// returns near the ceiling, plus workout-specific bonuses for genes with
// room to grow (AB/BB).
//
//	fitnessGain = XP × 0.002 × (1 - currentFitness/ceiling)
//
// Specific bonuses:
//   - Sprint workouts: extra +20% if SPD is AB or BB (room to grow)
//   - Endurance workouts: extra +20% if STM is AB or BB
//   - MudRun workouts: extra +20% if SZE is AB or BB
//   - MentalRep workouts: extra +20% if TMP is AB or BB
func calcFitnessGain(horse *models.Horse, workout models.WorkoutType, xp float64) float64 {
	ceiling := horse.FitnessCeiling
	if ceiling <= 0 {
		return 0 // no ceiling, no gain possible
	}

	// Diminishing returns formula
	diminish := 1.0 - (horse.CurrentFitness / ceiling)
	if diminish < 0 {
		diminish = 0
	}

	gain := xp * 0.002 * diminish

	// Workout-specific gene bonus: if the relevant gene is AB or BB,
	// the horse has room to grow → extra 20% fitness from that workout.
	switch workout {
	case models.WorkoutSprint:
		if expr := geneExpress(horse.Genome, models.GeneSPD); expr == "AB" || expr == "BB" {
			gain *= 1.20
		}
	case models.WorkoutEndurance:
		if expr := geneExpress(horse.Genome, models.GeneSTM); expr == "AB" || expr == "BB" {
			gain *= 1.20
		}
	case models.WorkoutMudRun:
		if expr := geneExpress(horse.Genome, models.GeneSZE); expr == "AB" || expr == "BB" {
			gain *= 1.20
		}
	case models.WorkoutMentalRep:
		if expr := geneExpress(horse.Genome, models.GeneTMP); expr == "AB" || expr == "BB" {
			gain *= 1.20
		}
	}

	return gain
}

// rollInjury checks whether the horse sustains an injury during training.
//
//	Base: 2% chance
//	Fatigue >70: 5% chance
//	Fatigue >90: 15% chance
//
// On injury: fatigue is set to 100 and a random funny injury note is picked.
func rollInjury(horse *models.Horse) (bool, string) {
	chance := 0.02
	if horse.Fatigue > 90 {
		chance = 0.15
	} else if horse.Fatigue > 70 {
		chance = 0.05
	}

	if rand.Float64() < chance {
		horse.Fatigue = 100
		note := injuryNotes[rand.IntN(len(injuryNotes))]
		return true, note
	}

	return false, ""
}

// GetTrainingHistory returns all training sessions for a horse, ordered
// chronologically (oldest first).
func (t *Trainer) GetTrainingHistory(horseID string) []*models.TrainingSession {
	t.mu.RLock()
	defer t.mu.RUnlock()

	sessions := t.sessions[horseID]
	if sessions == nil {
		return []*models.TrainingSession{}
	}

	// Return a copy to avoid external mutation
	out := make([]*models.TrainingSession, len(sessions))
	copy(out, sessions)
	return out
}

// RecoverFatigue reduces a horse's fatigue by the given amount (min 0).
func (t *Trainer) RecoverFatigue(horse *models.Horse, amount float64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	horse.Fatigue -= amount
	if horse.Fatigue < 0 {
		horse.Fatigue = 0
	}
}

// ---------------------------------------------------------------------------
// Trait System
// ---------------------------------------------------------------------------

// InitTraitPool builds and returns the complete pool of all possible traits.
// Traits are grouped by rarity: common, rare, legendary, and anomalous.
func InitTraitPool() []models.Trait {
	pool := []models.Trait{}

	// ---- Common traits (magnitude ~1.02-1.10) ----
	pool = append(pool, commonTraits()...)

	// ---- Rare traits (magnitude ~1.06-1.15) ----
	pool = append(pool, rareTraits()...)

	// ---- Legendary traits (magnitude ~1.15-1.50) ----
	pool = append(pool, legendaryTraits()...)

	// ---- Anomalous traits (E-008 lineage only) ----
	pool = append(pool, anomalousTraits()...)

	return pool
}

func commonTraits() []models.Trait {
	return []models.Trait{
		{
			ID:          uuid.New().String(),
			Name:        "Early Bird",
			Description: "Faster starts in the first 20% of a race",
			Effect:      "speed_boost_early",
			Magnitude:   1.05,
			Rarity:      "common",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Night Owl",
			Description: "Stronger finish in the last 20%",
			Effect:      "speed_boost_late",
			Magnitude:   1.05,
			Rarity:      "common",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Mud Lover",
			Description: "Thrives in Mudussy conditions",
			Effect:      "mud_boost",
			Magnitude:   1.08,
			Rarity:      "common",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Couch Potato",
			Description: "Recovers fatigue faster",
			Effect:      "fatigue_recovery",
			Magnitude:   1.5,
			Rarity:      "common",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Social Butterfly",
			Description: "Performs better with more competitors",
			Effect:      "crowd_boost",
			Magnitude:   1.03,
			Rarity:      "common",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Lone Wolf",
			Description: "Performs better with fewer competitors",
			Effect:      "small_field_boost",
			Magnitude:   1.05,
			Rarity:      "common",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Stubborn",
			Description: "Resists panic but slower to train",
			Effect:      "panic_resist",
			Magnitude:   0.5, // halves panic chance
			Rarity:      "common",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Glutton",
			Description: "High stamina but slower recovery",
			Effect:      "stamina_boost",
			Magnitude:   1.05,
			Rarity:      "common",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Lightweight",
			Description: "Better on Frostussy, worse on Mudussy",
			Effect:      "frost_boost",
			Magnitude:   1.10,
			Rarity:      "common",
		},
	}
}

func rareTraits() []models.Trait {
	return []models.Trait{
		{
			ID:          uuid.New().String(),
			Name:        "Thunderblood",
			Description: "Descendant of Thundercock. Born fast.",
			Effect:      "speed_boost",
			Magnitude:   1.08,
			Rarity:      "rare",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Flannel Energy",
			Description: "Radiates cozy determination",
			Effect:      "stamina_boost",
			Magnitude:   1.10,
			Rarity:      "rare",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Deploy on Friday",
			Description: "Unpredictable: 50% chance of +15% speed or -15%",
			Effect:      "chaos_boost",
			Magnitude:   1.15,
			Rarity:      "rare",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Mediterranean Diet",
			Description: "Impossibly smooth running form",
			Effect:      "all_boost",
			Magnitude:   1.04,
			Rarity:      "rare",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Sapphic Power",
			Description: "Poetic grace under pressure",
			Effect:      "panic_resist",
			Magnitude:   0.3, // 70% reduction in panic chance
			Rarity:      "rare",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Ice Veins",
			Description: "Immune to weather penalties",
			Effect:      "weather_immune",
			Magnitude:   1.0,
			Rarity:      "rare",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Git Push Force",
			Description: "Ignores fatigue below 80%",
			Effect:      "fatigue_resist",
			Magnitude:   0.5,
			Rarity:      "rare",
		},
	}
}

func legendaryTraits() []models.Trait {
	return []models.Trait{
		{
			ID:          uuid.New().String(),
			Name:        "Yogurt Blessed",
			Description: "E-008 has noticed this horse",
			Effect:      "anomalous_boost",
			Magnitude:   1.20,
			Rarity:      "legendary",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Triple Crown Gene",
			Description: "Born champion material",
			Effect:      "all_boost",
			Magnitude:   1.10,
			Rarity:      "legendary",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Sappho's Chosen",
			Description: "Perfect score on the Sappho Scale",
			Effect:      "training_boost",
			Magnitude:   1.50,
			Rarity:      "legendary",
		},
		{
			ID:          uuid.New().String(),
			Name:        "The Haunting",
			Description: "Strange things happen when this horse races",
			Effect:      "chaos_multiplier",
			Magnitude:   2.0,
			Rarity:      "legendary",
		},
	}
}

func anomalousTraits() []models.Trait {
	return []models.Trait{
		{
			ID:          uuid.New().String(),
			Name:        "The Yogurt Remembers",
			Description: "Reality bends around this horse",
			Effect:      "reality_warp",
			Magnitude:   1.0, // special handling by race engine
			Rarity:      "anomalous",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Warm at -196\u00B0C",
			Description: "Defies thermodynamics",
			Effect:      "all_boost",
			Magnitude:   1.15,
			Rarity:      "anomalous",
		},
		{
			ID:          uuid.New().String(),
			Name:        "DO NOT OPEN",
			Description: "Something is inside",
			Effect:      "anomalous_burst",
			Magnitude:   3.0,
			Rarity:      "anomalous",
		},
	}
}

// ---------------------------------------------------------------------------
// Trait Assignment
// ---------------------------------------------------------------------------

// AssignTraitsAtBirth gives a newborn foal 1-3 traits based on rarity rolls
// and parental inheritance. The horse's Traits slice is mutated in place.
//
// Rarity distribution per slot:
//
//	60% common
//	25% rare
//	10% legendary (requires parent with legendary lineage or LotNumber > 0)
//	 5% dud (no trait for this slot)
//
// Anomalous traits: 10% chance if either parent is E-008's Chosen
// (LotNumber == 6) or already carries an anomalous trait.
//
// Each parent's traits have a 20% chance of being inherited directly.
// Duplicate traits are never assigned.
func (t *Trainer) AssignTraitsAtBirth(horse *models.Horse, sire, mare *models.Horse) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Determine how many trait slots (1-3)
	numSlots := 1 + rand.IntN(3) // 1, 2, or 3

	// Check if anomalous traits are eligible
	anomalousEligible := isAnomalousEligible(sire, mare)

	// Check if legendary traits are eligible
	legendaryEligible := isLegendaryEligible(sire, mare)

	// Collect parent traits for inheritance rolls
	parentTraits := collectParentTraits(sire, mare)

	// Track assigned trait names to prevent duplicates
	assigned := make(map[string]bool)
	if horse.Traits == nil {
		horse.Traits = []models.Trait{}
	}
	for _, tr := range horse.Traits {
		assigned[tr.Name] = true
	}

	// Pool categorisation
	common := filterByRarity(t.traitPool, "common")
	rare := filterByRarity(t.traitPool, "rare")
	legendary := filterByRarity(t.traitPool, "legendary")
	anomalous := filterByRarity(t.traitPool, "anomalous")

	for i := 0; i < numSlots; i++ {
		// First, try parental inheritance (20% per parent trait)
		if trait := tryParentInheritance(parentTraits, assigned); trait != nil {
			horse.Traits = append(horse.Traits, *trait)
			assigned[trait.Name] = true
			continue
		}

		// Roll for anomalous if eligible (10% chance)
		if anomalousEligible && rand.Float64() < 0.10 {
			if trait := pickUnassigned(anomalous, assigned); trait != nil {
				horse.Traits = append(horse.Traits, *trait)
				assigned[trait.Name] = true
				continue
			}
		}

		// Standard rarity roll
		roll := rand.Float64()
		switch {
		case roll < 0.05:
			// 5% dud — no trait this slot
			continue

		case roll < 0.15 && legendaryEligible:
			// 10% legendary (only if eligible)
			if trait := pickUnassigned(legendary, assigned); trait != nil {
				horse.Traits = append(horse.Traits, *trait)
				assigned[trait.Name] = true
			}

		case roll < 0.40:
			// 25% rare
			if trait := pickUnassigned(rare, assigned); trait != nil {
				horse.Traits = append(horse.Traits, *trait)
				assigned[trait.Name] = true
			}

		default:
			// 60% common
			if trait := pickUnassigned(common, assigned); trait != nil {
				horse.Traits = append(horse.Traits, *trait)
				assigned[trait.Name] = true
			}
		}
	}
}

// AssignTraitOnMilestone is called when a horse hits a milestone (e.g. first
// win, 10 races, etc.). There's a 30% chance the horse gains a new trait.
// Returns the new trait or nil.
func (t *Trainer) AssignTraitOnMilestone(horse *models.Horse, milestone string) *models.Trait {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 30% chance of gaining a trait
	if rand.Float64() >= 0.30 {
		return nil
	}

	// Build set of already-owned trait names
	owned := make(map[string]bool)
	for _, tr := range horse.Traits {
		owned[tr.Name] = true
	}

	// Milestone-based rarity weighting:
	// Generally common/rare — legendary only from major milestones.
	var candidates []models.Trait
	switch milestone {
	case "first_win", "10_races", "triple_crown":
		// Higher chance of rare/legendary for major milestones
		candidates = append(candidates, filterByRarity(t.traitPool, "common")...)
		candidates = append(candidates, filterByRarity(t.traitPool, "rare")...)
		if milestone == "triple_crown" {
			candidates = append(candidates, filterByRarity(t.traitPool, "legendary")...)
		}
	default:
		// Minor milestones: mostly common
		candidates = append(candidates, filterByRarity(t.traitPool, "common")...)
		// Small chance of rare
		if rand.Float64() < 0.25 {
			candidates = append(candidates, filterByRarity(t.traitPool, "rare")...)
		}
	}

	trait := pickUnassigned(candidates, owned)
	if trait == nil {
		return nil
	}

	// Give the trait a unique ID for this horse's instance
	newTrait := *trait
	newTrait.ID = uuid.New().String()

	if horse.Traits == nil {
		horse.Traits = []models.Trait{}
	}
	horse.Traits = append(horse.Traits, newTrait)

	return &newTrait
}

// ---------------------------------------------------------------------------
// Trait helpers
// ---------------------------------------------------------------------------

// isAnomalousEligible returns true if either parent is E-008's Chosen
// (LotNumber == 6) or carries an anomalous trait.
func isAnomalousEligible(sire, mare *models.Horse) bool {
	if sire != nil && sire.LotNumber == 6 {
		return true
	}
	if mare != nil && mare.LotNumber == 6 {
		return true
	}
	for _, parent := range []*models.Horse{sire, mare} {
		if parent == nil {
			continue
		}
		for _, tr := range parent.Traits {
			if tr.Rarity == "anomalous" {
				return true
			}
		}
	}
	return false
}

// isLegendaryEligible returns true if either parent has legendary lineage
// (IsLegendary, LotNumber > 0, or carries a legendary trait).
func isLegendaryEligible(sire, mare *models.Horse) bool {
	for _, parent := range []*models.Horse{sire, mare} {
		if parent == nil {
			continue
		}
		if parent.IsLegendary || parent.LotNumber > 0 {
			return true
		}
		for _, tr := range parent.Traits {
			if tr.Rarity == "legendary" {
				return true
			}
		}
	}
	return false
}

// collectParentTraits gathers all traits from both parents.
func collectParentTraits(sire, mare *models.Horse) []models.Trait {
	var traits []models.Trait
	if sire != nil {
		traits = append(traits, sire.Traits...)
	}
	if mare != nil {
		traits = append(traits, mare.Traits...)
	}
	return traits
}

// tryParentInheritance rolls a 20% chance for each parent trait (in random
// order) and returns the first one that passes and hasn't been assigned.
func tryParentInheritance(parentTraits []models.Trait, assigned map[string]bool) *models.Trait {
	if len(parentTraits) == 0 {
		return nil
	}

	// Shuffle to avoid bias toward sire's traits
	shuffled := make([]models.Trait, len(parentTraits))
	copy(shuffled, parentTraits)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	for _, tr := range shuffled {
		if assigned[tr.Name] {
			continue
		}
		if rand.Float64() < 0.20 {
			// Create a new instance with a fresh ID
			inherited := tr
			inherited.ID = uuid.New().String()
			return &inherited
		}
	}
	return nil
}

// filterByRarity returns all traits in the pool matching the given rarity.
func filterByRarity(pool []models.Trait, rarity string) []models.Trait {
	var out []models.Trait
	for _, tr := range pool {
		if tr.Rarity == rarity {
			out = append(out, tr)
		}
	}
	return out
}

// pickUnassigned selects a random trait from candidates that isn't already in
// the assigned set. Returns nil if no valid candidates remain.
func pickUnassigned(candidates []models.Trait, assigned map[string]bool) *models.Trait {
	if len(candidates) == 0 {
		return nil
	}

	// Shuffle and pick the first unassigned
	shuffled := make([]models.Trait, len(candidates))
	copy(shuffled, candidates)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	for _, tr := range shuffled {
		if !assigned[tr.Name] {
			picked := tr
			picked.ID = uuid.New().String()
			return &picked
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Aging System
// ---------------------------------------------------------------------------

// AgeHorse increments the horse's age by one season and adjusts the fitness
// ceiling based on the horse's life stage.
//
// Life stages:
//
//	Age 0-3  "Youth"   — ceiling increases +2% per season (still growing)
//	Age 4-8  "Prime"   — no change (peak years)
//	Age 9-12 "Veteran" — ceiling decreases -1% per season
//	Age 13-15 "Elder"  — ceiling decreases -3% per season
//	Age 16+  "Ancient" — ceiling decreases -5% per season, 10% forced retirement
//
// E-008's Chosen (LotNumber == 6) does not age. The yogurt is eternal.
func AgeHorse(horse *models.Horse) {
	// E-008's Chosen transcends time.
	if horse.LotNumber == 6 {
		return
	}

	horse.Age++

	switch {
	case horse.Age <= 3:
		// Youth: ceiling grows +2% per season
		horse.FitnessCeiling *= 1.02

	case horse.Age <= 8:
		// Prime: no change — peak performance years

	case horse.Age <= 12:
		// Veteran: ceiling decays -1% per season
		horse.FitnessCeiling *= 0.99

	case horse.Age <= 15:
		// Elder: ceiling decays -3% per season
		horse.FitnessCeiling *= 0.97

	default:
		// Ancient: ceiling decays -5% per season
		horse.FitnessCeiling *= 0.95

		// 10% chance of forced retirement
		if rand.Float64() < 0.10 {
			horse.Retired = true
			horse.Lore += fmt.Sprintf(" [Retired at age %d — the years finally caught up.]", horse.Age)
		}
	}

	// Cap current fitness if ceiling dropped below it
	if horse.CurrentFitness > horse.FitnessCeiling {
		horse.CurrentFitness = horse.FitnessCeiling
	}
}

// LifeStage returns a human-readable life stage string for the horse's age.
func LifeStage(horse *models.Horse) string {
	if horse.LotNumber == 6 {
		return "Eternal"
	}
	switch {
	case horse.Age <= 3:
		return "Youth"
	case horse.Age <= 8:
		return "Prime"
	case horse.Age <= 12:
		return "Veteran"
	case horse.Age <= 15:
		return "Elder"
	default:
		return "Ancient"
	}
}

// ---------------------------------------------------------------------------
// Retirement System
// ---------------------------------------------------------------------------

// ShouldRetire evaluates whether a horse should retire and returns true with
// a funny reason if so. Checks:
//
//   - Age >= 16 and 10% random roll
//   - Fitness ceiling dropped below 0.2
//   - 50+ races and ELO below 900
//
// Returns (false, "") if the horse should keep racing.
func ShouldRetire(horse *models.Horse) (bool, string) {
	// E-008's Chosen never retires. The yogurt sustains.
	if horse.LotNumber == 6 {
		return false, ""
	}

	// Already retired
	if horse.Retired {
		return true, "Already retired"
	}

	// Age >= 16 with 10% chance
	if horse.Age >= 16 && rand.Float64() < 0.10 {
		return true, pickRetirementMessage()
	}

	// Fitness ceiling too low
	if horse.FitnessCeiling < 0.2 {
		return true, pickRetirementMessage()
	}

	// 50+ races and ELO below 900 — time to hang up the horseshoes
	if horse.Races >= 50 && horse.ELO < 900 {
		return true, pickRetirementMessage()
	}

	return false, ""
}

// RetireHorse sets the horse as retired and appends the retirement reason
// to the horse's lore.
func RetireHorse(horse *models.Horse, reason string) {
	horse.Retired = true
	if reason != "" {
		horse.Lore += fmt.Sprintf(" [RETIRED: %s]", reason)
	}
}

// pickRetirementMessage selects a random retirement message.
func pickRetirementMessage() string {
	return retirementMessages[rand.IntN(len(retirementMessages))]
}

// ---------------------------------------------------------------------------
// Gene helpers — safe extraction from a Genome
// ---------------------------------------------------------------------------

// geneExpress safely returns the expression string ("AA", "AB", "BB") for a
// gene type. Returns "BB" if the gene is missing from the genome.
func geneExpress(g models.Genome, gt models.GeneType) string {
	if g == nil {
		return "BB"
	}
	if gene, ok := g[gt]; ok {
		return gene.Express()
	}
	return "BB"
}
