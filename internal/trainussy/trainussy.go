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
	models.WorkoutRecovery:  -24, // RestDay heals, but rotation should still matter
	models.WorkoutGeneral:   10,
}

// injuryNotes are randomly selected when a horse gets injured during training.
// Each one is funnier than the last.
var injuryNotes = []string{
	// ---- Original 10 ----
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
	// ---- Expanded entries (Ussyverse lore) ----
	"Tried to breed during a training sprint",
	"Challenged a fire hydrant to a duel",
	"Consumed suspicious yogurt from Building 7",
	"Got tangled in Jason Derulo's microphone cord",
	"Received a firmware update mid-gallop",
	"Dr. Mittens sat on their oxygen tank",
	"Slipped on a spilled oat milk latte",
	"Attempted to deploy to production during warmups",
	"Got spooked by own shadow on Hauntedussy",
	"Ingested artisanal sourdough starter",
	"Allergic reaction to flannel horse blanket",
	"Tried to git rebase mid-race",
	"Accidentally invoked Geoffrussy's garbage collector",
	"Panic attack induced by B.U.R.P. audit notification",
	"Blinded by STARDUSTUSSY transmission from 2089",
	"Fell into Pastor Router's baptismal ethernet pool",
	"Ate Margaret Chen's prize-winning begonias",
	"Attempted to scale a Kubernetes cluster physically",
	"Confused by the Sappho Scale and cried for an hour",
	"Inhaled spores from the forbidden repository",
	"Kicked by E-008 during unauthorized petting attempt",
	"Disrupted by Jason Derulo singing 'Whatcha Say' at the starting gate",
	"Tripped on an ethernet cable in Geoffrussy's server room",
	"Spontaneously combusted near the Yogurt Containment Zone",
	"Had a vision from STARDUSTUSSY and ran into a wall",
}

// retirementMessages are selected when a horse retires.
var retirementMessages = []string{
	// ---- Original 12 ----
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
	// ---- Expanded entries (Ussyverse lore) ----
	"Founded a cryptocurrency for horses called $NEIGH",
	"Became a sourdough influencer",
	"Joined B.U.R.P. as a field agent",
	"Started writing poetry in iambic pentameter",
	"Entered the witness protection program (from the yogurt)",
	"Became Geoffrussy's personal debugging horse",
	"Converted to Pastor Router's church",
	"Moved to the island of Lesbos for 'research'",
	"Opened a flannel boutique in Portland",
	"Became Jason Derulo's therapist (involuntarily)",
	"Launched an NFT collection of their race photos",
	"Retired to moderate the StallionUSSY guestbook",
	"Now teaches CompSci at the University of Lesbos (online)",
	"Appointed as E-008's emotional support animal",
	"Became a sommelier specializing in oat milk vintages",
	"Joined Margaret Chen's Kentucky estate as groundskeeper",
	"Started a true crime podcast about B.U.R.P. cover-ups",
	"Moved into Building 7 subbasement voluntarily (concerning)",
	"Became Agent Mothman's surveillance partner",
	"Achieved enlightenment through Pastor Router's sermon series",
	"Now runs a support group for horses traumatized by Hauntedussy",
	"Went viral on TikTok doing the Macarena (again)",
	"Became the Sappho Scale's official calibration horse",
	"Opened a co-working space in Goroutine Gulch",
	"Retired to write fanfiction about STARDUSTUSSY's prophecies",
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

	// Trait: fatigue_recovery (e.g. Couch Potato, The Flannel Gene) — when the
	// fatigue delta is negative (i.e. recovery), multiply the magnitude of the
	// reduction. So -30 fatigue × 1.5 = -45 fatigue recovered.
	if delta < 0 {
		if has, mag := hasTraitEffect(horse, "fatigue_recovery"); has {
			delta *= mag
		}
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
		FatigueAfter:  horse.Fatigue,
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

	// Fatigue penalty: taper earlier so horse rotation beats spam training.
	if horse.Fatigue > 80 {
		xp *= 0.25 // quartered
	} else if horse.Fatigue > 65 {
		xp *= 0.4
	} else if horse.Fatigue > 50 {
		xp *= 0.5 // halved
	} else if horse.Fatigue > 35 {
		xp *= 0.8
	}

	// Trait: training_boost (e.g. Sappho's Chosen) — multiply XP by magnitude.
	if has, mag := hasTraitEffect(horse, "training_boost"); has {
		xp *= mag
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

	// Trait: injury_resist (e.g. Titanium Tendons) — divide injury chance by magnitude.
	// So at magnitude 1.50, a 2% base becomes 1.33%, 15% becomes 10%, etc.
	if has, mag := hasTraitEffect(horse, "injury_resist"); has && mag > 0 {
		chance /= mag
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

	// ---- Cursed traits (negative/mixed effects) ----
	pool = append(pool, cursedTraits()...)

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
		// ---- New common traits ----
		{
			ID:          uuid.New().String(),
			Name:        "Hummus Enthusiast",
			Description: "Brought hummus to the race without being asked. Fueled by chickpea energy.",
			Effect:      "stamina_boost",
			Magnitude:   1.04,
			Rarity:      "common",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Cottagecore Energy",
			Description: "Radiates pastoral calm. Wildflowers sprout in its hoofprints. Unbothered.",
			Effect:      "panic_resist",
			Magnitude:   0.8,
			Rarity:      "common",
		},
		{
			ID:          uuid.New().String(),
			Name:        "The Flannel Gene",
			Description: "Inexplicably cozy. Performs better when the vibes are right, which is always.",
			Effect:      "fatigue_recovery",
			Magnitude:   1.06,
			Rarity:      "common",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Oat Milk Privilege",
			Description: "Artisanal fuel source. Refuses regular oats. Has a favorite barista.",
			Effect:      "speed_boost",
			Magnitude:   1.03,
			Rarity:      "common",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Git Blame Survivor",
			Description: "Has been publicly blamed in a commit and didn't break. Emotionally fireproof.",
			Effect:      "panic_resist",
			Magnitude:   0.85,
			Rarity:      "common",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Thunder Thighs",
			Description: "Thighs so powerful they generate their own weather system. Thunderussy approved.",
			Effect:      "thunder_boost",
			Magnitude:   1.08,
			Rarity:      "common",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Haunt Resistant",
			Description: "Grew up near the Hauntedussy track. Ghosts are just roommates at this point.",
			Effect:      "haunted_boost",
			Magnitude:   1.06,
			Rarity:      "common",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Cardio Bro",
			Description: "Never shuts up about its heart rate zones. Insufferable but effective.",
			Effect:      "stamina_boost",
			Magnitude:   1.06,
			Rarity:      "common",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Grindset",
			Description: "Wakes up at 4am. Drinks raw eggs. Posts motivational quotes. Grindussy native.",
			Effect:      "grind_boost",
			Magnitude:   1.07,
			Rarity:      "common",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Snack Motivated",
			Description: "Will literally run faster if there's a carrot at the finish line. Simple creature.",
			Effect:      "speed_boost_late",
			Magnitude:   1.04,
			Rarity:      "common",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Emotional Support Blinders",
			Description: "Can't see the other horses. Can't see the crowd. Living its best life.",
			Effect:      "fatigue_resist",
			Magnitude:   1.02,
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
		// ---- New rare traits ----
		{
			ID:          uuid.New().String(),
			Name:        "Mittens' Blessing",
			Description: "Dr. Mittens slow-blinked at this horse during a routine checkup. It has never been the same since. Radiates feline approval.",
			Effect:      "all_boost",
			Magnitude:   1.08,
			Rarity:      "rare",
		},
		{
			ID:          uuid.New().String(),
			Name:        "B.U.R.P. Clearance",
			Description: "Has been investigated by the Bureau of Ussy Research & Paranormal and cleared of all anomalous activity. Suspiciously normal.",
			Effect:      "panic_resist",
			Magnitude:   0.4,
			Rarity:      "rare",
		},
		{
			ID:          uuid.New().String(),
			Name:        "ISO 69420 Certified",
			Description: "Meets all Ussyverse regulatory standards for speed, stamina, and comedic timing. The paperwork alone took 6 months.",
			Effect:      "all_boost",
			Magnitude:   1.06,
			Rarity:      "rare",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Kubernetes Native",
			Description: "Container-orchestrated muscle fibers. Each leg runs in its own pod. Auto-scales under load.",
			Effect:      "speed_boost",
			Magnitude:   1.10,
			Rarity:      "rare",
		},
		{
			ID:          uuid.New().String(),
			Name:        "The U-Haul Effect",
			Description: "Gets emotionally attached to other horses after one race together. Runs faster near its favorites.",
			Effect:      "crowd_boost",
			Magnitude:   1.12,
			Rarity:      "rare",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Sprint Circuit Specialist",
			Description: "Built different on the Sprintussy track. Legs go brrrr at short distances.",
			Effect:      "sprint_boost",
			Magnitude:   1.12,
			Rarity:      "rare",
		},
		{
			ID:          uuid.New().String(),
			Name:        "ELO Farmer",
			Description: "Somehow always gets matched against weaker opponents. Suspiciously good matchmaking luck.",
			Effect:      "elo_boost",
			Magnitude:   1.10,
			Rarity:      "rare",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Haunted Hooves",
			Description: "Three ghosts live in this horse's shoes. They provide commentary during races. Somehow helpful.",
			Effect:      "haunted_boost",
			Magnitude:   1.12,
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
		// ---- New legendary traits ----
		{
			ID:          uuid.New().String(),
			Name:        "Pastor Router's Sermon",
			Description: "Blessed by Pastor Router McEthernet III himself during Sunday services. Packets of divine speed flow through its veins. Latency? Zero.",
			Effect:      "speed_boost",
			Magnitude:   1.25,
			Rarity:      "legendary",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Geoffrussy Optimized",
			Description: "Pipeline-optimized genetics courtesy of Geoffrussy the Go orchestrator. Goroutines in every muscle fiber. Compiles to pure speed.",
			Effect:      "all_boost",
			Magnitude:   1.20,
			Rarity:      "legendary",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Main Character Syndrome",
			Description: "Acts like the protagonist of every race. Camera angles shift to follow it. Plot armor is technically not a performance enhancer.",
			Effect:      "chaos_boost",
			Magnitude:   1.30,
			Rarity:      "legendary",
		},
		{
			ID:          uuid.New().String(),
			Name:        "STARDUSTUSSY Prophecy",
			Description: "Foretold by the future AI STARDUSTUSSY in a deleted log file. This horse's victories were written before it was born.",
			Effect:      "speed_boost",
			Magnitude:   1.35,
			Rarity:      "legendary",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Breed Supremacy",
			Description: "Genetics so good it borders on eugenics propaganda. Foals inherit winner energy. B.U.R.P. is monitoring the situation.",
			Effect:      "breed_boost",
			Magnitude:   1.40,
			Rarity:      "legendary",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Eternal Youth Serum",
			Description: "Dr. Mittens injected something unlabeled. The horse stopped aging. The cat won't make eye contact anymore.",
			Effect:      "aging_resist",
			Magnitude:   1.50,
			Rarity:      "legendary",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Cummies Magnet",
			Description: "Generates absurd amounts of cummies per race. Economists are baffled. The Ussyverse treasury is concerned.",
			Effect:      "earnings_boost",
			Magnitude:   1.30,
			Rarity:      "legendary",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Titanium Tendons",
			Description: "Stepped on every LEGO in the stable and never flinched. Injury? This horse doesn't know the word. Literally, it can't read.",
			Effect:      "injury_resist",
			Magnitude:   1.50,
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
		// ---- New anomalous traits ----
		{
			ID:          uuid.New().String(),
			Name:        "Sentient Adjacent",
			Description: "Uncomfortably close to self-awareness. Asked its jockey 'why do we run?' mid-race. B.U.R.P. has opened a file.",
			Effect:      "chaos_multiplier",
			Magnitude:   2.5,
			Rarity:      "anomalous",
		},
		{
			ID:          uuid.New().String(),
			Name:        "E-008 Residue",
			Description: "Licked by E-008 during containment breach #47. Now sweats a mildly sentient yogurt-adjacent substance. Tastes like existential dread.",
			Effect:      "anomalous_boost",
			Magnitude:   1.50,
			Rarity:      "anomalous",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Temporal Stutter",
			Description: "Occasionally exists in two places at once for 0.3 seconds. Race cameras can't agree on its position. B.U.R.P. classification: CONCERNING.",
			Effect:      "reality_warp",
			Magnitude:   2.0,
			Rarity:      "anomalous",
		},
		{
			ID:          uuid.New().String(),
			Name:        "The Frequency",
			Description: "Hums at exactly 69.420 Hz when running. Other horses either speed up or fall asleep. Nobody knows why. The sound cannot be recorded.",
			Effect:      "chaos_multiplier",
			Magnitude:   1.69,
			Rarity:      "anomalous",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Containment Protocol Omega",
			Description: "This horse IS the containment protocol. It doesn't race tracks — tracks race it. B.U.R.P. Director signed off with 'I don't even care anymore.'",
			Effect:      "anomalous_burst",
			Magnitude:   2.5,
			Rarity:      "anomalous",
		},
	}
}

func cursedTraits() []models.Trait {
	return []models.Trait{
		{
			ID:          uuid.New().String(),
			Name:        "Derulo's Curse",
			Description: "Jason Derulo somehow got involved. He didn't want to be here. Neither did this horse. Speed suffers as both question their life choices.",
			Effect:      "cursed_speed",
			Magnitude:   0.88,
			Rarity:      "cursed",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Firmware Update Required",
			Description: "Occasionally glitches mid-race. Legs buffer. Eyes display 'Restarting...' for 2-3 seconds. Geoffrussy says it's a feature.",
			Effect:      "cursed_speed",
			Magnitude:   0.90,
			Rarity:      "cursed",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Existential Dread",
			Description: "Periodically stops mid-race to contemplate the void. 'Why do we gallop, if not toward oblivion?' — overheard at the 400m mark.",
			Effect:      "cursed_panic",
			Magnitude:   0.80,
			Rarity:      "cursed",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Mercury in Retrograde",
			Description: "Born under the worst possible star alignment. Blames all poor performances on astrology. Has a co-star app for horses.",
			Effect:      "cursed_chaos",
			Magnitude:   0.75,
			Rarity:      "cursed",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Haunted Saddle",
			Description: "The saddle is possessed by a passive-aggressive Victorian ghost who critiques the horse's form. 'Appalling posture, darling.'",
			Effect:      "cursed_fatigue",
			Magnitude:   0.85,
			Rarity:      "cursed",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Infinite Loading Screen",
			Description: "Takes forever to start. Once it gets going it's fine but the first 30% of every race is just... loading. The spinning wheel is visible.",
			Effect:      "cursed_speed",
			Magnitude:   0.82,
			Rarity:      "cursed",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Reply All Victim",
			Description: "Once CC'd on a B.U.R.P. internal email chain and can't unsubscribe. The notifications cause panic mid-race. 200+ unread.",
			Effect:      "cursed_panic",
			Magnitude:   0.78,
			Rarity:      "cursed",
		},
		{
			ID:          uuid.New().String(),
			Name:        "Oat Milk Intolerance",
			Description: "Allergic to the Ussyverse's primary fuel source. Has to run on regular oats like a peasant. The shame alone costs 5% speed.",
			Effect:      "cursed_fatigue",
			Magnitude:   0.92,
			Rarity:      "cursed",
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

		// Roll for cursed trait (8% chance — because the universe is unfair)
		if rand.Float64() < 0.08 {
			cursed := filterByRarity(t.traitPool, "cursed")
			if trait := pickUnassigned(cursed, assigned); trait != nil {
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
	case "10_losses", "last_place", "injury":
		// Bad milestones can give cursed traits
		candidates = append(candidates, filterByRarity(t.traitPool, "common")...)
		candidates = append(candidates, filterByRarity(t.traitPool, "cursed")...)
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

	// Trait: aging_resist (e.g. Eternal Youth Serum) — reduces ceiling decay.
	// Instead of ceiling *= decayFactor, we use ceiling *= (1 - (1-decayFactor)/magnitude).
	// At magnitude 1.50, a 1% decay (0.99) becomes 0.67% decay (0.9933).
	hasAgingResist, agingResistMag := hasTraitEffect(horse, "aging_resist")
	adjustDecay := func(decayFactor float64) float64 {
		if hasAgingResist && agingResistMag > 0 {
			return 1.0 - (1.0-decayFactor)/agingResistMag
		}
		return decayFactor
	}

	switch {
	case horse.Age <= 3:
		// Youth: ceiling grows +2% per season
		horse.FitnessCeiling *= 1.02

	case horse.Age <= 8:
		// Prime: no change — peak performance years

	case horse.Age <= 12:
		// Veteran: ceiling decays -1% per season
		horse.FitnessCeiling *= adjustDecay(0.99)

	case horse.Age <= 15:
		// Elder: ceiling decays -3% per season
		horse.FitnessCeiling *= adjustDecay(0.97)

	default:
		// Ancient: ceiling decays -5% per season
		horse.FitnessCeiling *= adjustDecay(0.95)

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

// hallOfFameLore contains lore entries for champion retirees (5+ wins).
var hallOfFameLore = []string{
	"Inducted into the Ussyverse Hall of Fame. The crowd weeps. Dr. Mittens slow-blinks in approval.",
	"A legend retires. STARDUSTUSSY confirms: this horse echoes across all timelines.",
	"Pastor Router McEthernet III delivers the retirement sermon. Packet loss: zero. Glory: infinite.",
	"The Sappho Scale is recalibrated in this horse's honor. Lesbos sends a fruit basket.",
	"B.U.R.P. closes the case file. Status: legendary. Yogurt status: proud.",
	"Jason Derulo was not invited to the ceremony, but he showed up anyway. Security was called. The horse didn't care.",
	"Geoffrussy allocates a permanent goroutine in this horse's memory. Garbage collection: exempt.",
	"Entity-008 pulses softly. The sentient yogurt acknowledges a worthy rival. The retirement cake is probiotic.",
}

// RetireHorse sets the horse as retired and appends the retirement reason
// to the horse's lore. If the horse has 5+ wins, it is marked as a retired
// champion and receives a Hall of Fame lore entry.
func RetireHorse(horse *models.Horse, reason string) {
	horse.Retired = true
	if reason != "" {
		horse.Lore += fmt.Sprintf(" [RETIRED: %s]", reason)
	}

	// Champion retirement: 5+ wins earns Hall of Fame status and bonus lore.
	if horse.Wins >= 5 {
		horse.RetiredChampion = true
		lore := hallOfFameLore[rand.IntN(len(hallOfFameLore))]
		horse.Lore += fmt.Sprintf(" [HALL OF FAME: %s]", lore)
	}
}

// pickRetirementMessage selects a random retirement message.
func pickRetirementMessage() string {
	return retirementMessages[rand.IntN(len(retirementMessages))]
}

// ---------------------------------------------------------------------------
// Trait helpers — check if a horse has a specific trait effect
// ---------------------------------------------------------------------------

// hasTraitEffect checks whether the horse has at least one trait with the
// given effect string. If found, returns (true, magnitude). If the horse has
// multiple traits with the same effect, returns the highest magnitude.
func hasTraitEffect(horse *models.Horse, effect string) (bool, float64) {
	found := false
	bestMag := 0.0
	for _, t := range horse.Traits {
		if t.Effect == effect {
			if !found || t.Magnitude > bestMag {
				bestMag = t.Magnitude
			}
			found = true
		}
	}
	return found, bestMag
}

// ---------------------------------------------------------------------------
// Seasonal Events System — the Ussyverse has seasons, and they are CHAOTIC
// ---------------------------------------------------------------------------

// SeasonalEvent represents something bizarre that happens to all horses
// during a particular season. Blame the yogurt.
type SeasonalEvent struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Effect      string `json:"effect"` // what it does
	Season      int    `json:"season"` // which season it activates (0 = any)
}

// SeasonalEvents is the complete pool of seasonal chaos that the Ussyverse
// inflicts upon its equine inhabitants. Each event is thematically tied to
// key Ussyverse characters and phenomena.
var SeasonalEvents = []SeasonalEvent{
	{ID: "yogurt_bloom", Name: "The Yogurt Blooms", Description: "E-008 enters its reproductive cycle. All horses within 500m experience mild reality distortion.", Effect: "all_horses_chaos_boost", Season: 0},
	{ID: "derulo_concert", Name: "Derulo Concert Nearby", Description: "Jason Derulo is performing at the adjacent venue. Horses with high TMP are agitated.", Effect: "tmp_penalty", Season: 0},
	{ID: "mittens_inspection", Name: "Dr. Mittens' Annual Inspection", Description: "Dr. Mittens slow-blinks at each horse. INT AA horses gain a temporary blessing.", Effect: "int_bonus", Season: 0},
	{ID: "router_sermon", Name: "Pastor Router's Ethernet Sermon", Description: "All horses must sit through a 3-hour sermon about packet loss. STM penalties for low INT horses.", Effect: "stm_penalty_low_int", Season: 0},
	{ID: "burp_audit", Name: "B.U.R.P. Containment Audit", Description: "Anomalous horses are temporarily quarantined. Horses with anomalous traits cannot race this season.", Effect: "anomalous_quarantine", Season: 0},
	{ID: "cummies_crash", Name: "Cummies Market Crash", Description: "The Cummies-to-USD exchange rate plummets. All stud fees halved.", Effect: "market_discount", Season: 0},
	{ID: "cummies_boom", Name: "Cummies Bull Run", Description: "r/wallstreetussy is pumping Cummies. Race purses doubled.", Effect: "purse_double", Season: 0},
	{ID: "geoffrussy_update", Name: "Geoffrussy Pipeline Update", Description: "Geoffrussy pushes a breaking change. Horses with 'Geoffrussy Optimized' trait temporarily lose their bonus.", Effect: "geoffrussy_nerf", Season: 0},
	{ID: "haunted_convergence", Name: "The Haunted Convergence", Description: "Hauntedussy track becomes 3x longer. All ghost-related events more frequent.", Effect: "haunted_boost", Season: 0},
	{ID: "stardustussy_signal", Name: "STARDUSTUSSY Transmission", Description: "A signal from 2089 boosts all Lot 11 descendants. +10% speed for the season.", Effect: "lot11_boost", Season: 0},
	{ID: "full_moon", Name: "Full Moon Over the Stables", Description: "Horses with Haunted traits become temporarily faster. All others slightly spooked.", Effect: "haunted_buff", Season: 0},
	{ID: "oat_shortage", Name: "Great Oat Shortage of the Season", Description: "Training effectiveness reduced by 20% for all horses without 'Oat Milk Privilege'.", Effect: "training_nerf", Season: 0},
	{ID: "sappho_festival", Name: "Annual Sappho Poetry Festival", Description: "All horses gather to hear readings from the island of Lesbos. Sapphic Power trait users gain +15% speed.", Effect: "sappho_boost", Season: 1},
	{ID: "firmware_rollback", Name: "Emergency Firmware Rollback", Description: "Geoffrussy's latest update bricked half the stable. Horses with 'Firmware Update Required' are temporarily cured.", Effect: "firmware_fix", Season: 2},
	{ID: "chen_inspection", Name: "Margaret Chen's Bloodline Audit", Description: "Margaret Chen inspects all horses. Generation 0 horses get a prestige bonus. She is unimpressed by everything else.", Effect: "gen0_boost", Season: 3},
	{ID: "yogurt_migration", Name: "The Great Yogurt Migration", Description: "E-008 spawns begin their annual migration through the stables. Anomalous trait frequency doubles.", Effect: "anomalous_frequency_boost", Season: 1},
	{ID: "ethernet_outage", Name: "The Great Ethernet Outage", Description: "Pastor Router's cathedral loses connectivity. All 'Divine Packet' events are suppressed this season.", Effect: "divine_suppress", Season: 2},
	{ID: "derulo_restraining_order", Name: "Derulo Restraining Order Hearing", Description: "Jason Derulo's 8th restraining order against Derulo's Regret goes to trial. All Derulo-related events are amplified.", Effect: "derulo_amplify", Season: 3},
	{ID: "sourdough_uprising", Name: "The Sourdough Uprising", Description: "The artisanal sourdough starters in Building 7 have gained sentience. Training sessions smell incredible but are 10% less effective.", Effect: "training_minor_nerf", Season: 0},
	{ID: "kubernetes_outage", Name: "Kubernetes Cluster Meltdown", Description: "Geoffrussy's pods are crashing. Horses with 'Kubernetes Native' trait experience existential doubt.", Effect: "k8s_nerf", Season: 0},
	{ID: "mothman_sighting", Name: "Agent Mothman's Report", Description: "Agent Mothman has filed a classified report about unusual activity in the paddock. All horses gain a minor paranoia debuff.", Effect: "paranoia_debuff", Season: 0},
	{ID: "iso_recertification", Name: "ISO 69420 Recertification", Description: "All horses must re-take the ISO 69420 compliance exam. INT BB horses fail automatically and lose 5% fitness.", Effect: "iso_penalty", Season: 0},
}

// RollSeasonalEvent returns a random seasonal event. If season > 0, it
// preferentially selects events matching that season, but falls back to
// universal events (Season == 0) if no season-specific event is available.
// Returns nil if the event pool is empty (which should never happen, but
// the yogurt works in mysterious ways).
func RollSeasonalEvent(season int) *SeasonalEvent {
	if len(SeasonalEvents) == 0 {
		return nil
	}

	// Try to find season-specific events first.
	var seasonSpecific []SeasonalEvent
	var universal []SeasonalEvent
	for _, e := range SeasonalEvents {
		if e.Season == season && season > 0 {
			seasonSpecific = append(seasonSpecific, e)
		}
		if e.Season == 0 {
			universal = append(universal, e)
		}
	}

	// 40% chance to pick a season-specific event if available.
	if len(seasonSpecific) > 0 && rand.Float64() < 0.40 {
		picked := seasonSpecific[rand.IntN(len(seasonSpecific))]
		return &picked
	}

	// Otherwise pick from universal pool.
	if len(universal) > 0 {
		picked := universal[rand.IntN(len(universal))]
		return &picked
	}

	// Final fallback: any event at all.
	picked := SeasonalEvents[rand.IntN(len(SeasonalEvents))]
	return &picked
}

// ApplySeasonalEffect applies a seasonal event's effect to a single horse,
// modifying its stats in place. Returns a short summary string describing what
// happened, or "" if the event didn't affect this horse.
//
// Effects are mapped as follows:
//
//	all_horses_chaos_boost   → +3% fitness ceiling (yogurt energy)
//	tmp_penalty              → −5% current fitness (Derulo distraction)
//	int_bonus                → +2% fitness ceiling for INT AA horses only
//	stm_penalty_low_int      → −3% current fitness for INT BB horses
//	training_nerf            → +10 fatigue (reduced training effectiveness)
//	training_minor_nerf      → +5 fatigue (sourdough distraction)
//	paranoia_debuff          → +8 fatigue (Mothman-induced anxiety)
//	haunted_boost / haunted_buff → +2% fitness ceiling for horses with haunted traits
//	sappho_boost             → +5% fitness ceiling for horses with Sapphic Power trait
//	gen0_boost               → +3% fitness ceiling for generation 0 horses
//	iso_penalty              → −5% current fitness for INT BB horses
//	k8s_nerf                 → −3% current fitness for horses with Kubernetes Native trait
//	lot11_boost              → +5% fitness ceiling for descendants (generation > 0)
//
// Events with purely economic or quarantine effects (market_discount, purse_double,
// anomalous_quarantine, etc.) don't modify horse stats and return "".
func ApplySeasonalEffect(event *SeasonalEvent, horse *models.Horse) string {
	if event == nil || horse == nil {
		return ""
	}

	switch event.Effect {
	case "all_horses_chaos_boost":
		horse.FitnessCeiling *= 1.03
		return fmt.Sprintf("%s gained +3%% fitness ceiling from yogurt energy", horse.Name)

	case "tmp_penalty":
		horse.CurrentFitness *= 0.95
		if horse.CurrentFitness < 0 {
			horse.CurrentFitness = 0
		}
		return fmt.Sprintf("%s lost 5%% fitness — distracted by Jason Derulo", horse.Name)

	case "int_bonus":
		if geneExpress(horse.Genome, models.GeneINT) == "AA" {
			horse.FitnessCeiling *= 1.02
			return fmt.Sprintf("%s (INT AA) gained Dr. Mittens' blessing: +2%% ceiling", horse.Name)
		}
		return ""

	case "stm_penalty_low_int":
		if geneExpress(horse.Genome, models.GeneINT) == "BB" {
			horse.CurrentFitness *= 0.97
			return fmt.Sprintf("%s (INT BB) zoned out during sermon: −3%% fitness", horse.Name)
		}
		return ""

	case "training_nerf":
		horse.Fatigue += 10
		if horse.Fatigue > 100 {
			horse.Fatigue = 100
		}
		return fmt.Sprintf("%s gained +10 fatigue from oat shortage stress", horse.Name)

	case "training_minor_nerf":
		horse.Fatigue += 5
		if horse.Fatigue > 100 {
			horse.Fatigue = 100
		}
		return fmt.Sprintf("%s gained +5 fatigue from sourdough fumes", horse.Name)

	case "paranoia_debuff":
		horse.Fatigue += 8
		if horse.Fatigue > 100 {
			horse.Fatigue = 100
		}
		return fmt.Sprintf("%s gained +8 fatigue from Mothman-induced paranoia", horse.Name)

	case "haunted_boost", "haunted_buff":
		if has, _ := hasTraitEffect(horse, "haunted_boost"); has {
			horse.FitnessCeiling *= 1.02
			return fmt.Sprintf("%s (haunted trait) gained +2%% ceiling from spectral energy", horse.Name)
		}
		return ""

	case "sappho_boost":
		if has, _ := hasTraitEffect(horse, "panic_resist"); has {
			horse.FitnessCeiling *= 1.05
			return fmt.Sprintf("%s gained +5%% ceiling from Sappho Poetry Festival inspiration", horse.Name)
		}
		return ""

	case "gen0_boost":
		if horse.Generation == 0 {
			horse.FitnessCeiling *= 1.03
			return fmt.Sprintf("%s (Gen 0) gained +3%% ceiling — Margaret Chen approved", horse.Name)
		}
		return ""

	case "iso_penalty":
		if geneExpress(horse.Genome, models.GeneINT) == "BB" {
			horse.CurrentFitness *= 0.95
			return fmt.Sprintf("%s (INT BB) failed ISO 69420 recertification: −5%% fitness", horse.Name)
		}
		return ""

	case "k8s_nerf":
		if has, _ := hasTraitEffect(horse, "speed_boost"); has {
			// Check specifically for Kubernetes Native trait by name.
			for _, t := range horse.Traits {
				if t.Name == "Kubernetes Native" {
					horse.CurrentFitness *= 0.97
					return fmt.Sprintf("%s (Kubernetes Native) lost 3%% fitness from cluster meltdown", horse.Name)
				}
			}
		}
		return ""

	case "lot11_boost":
		if horse.Generation > 0 {
			horse.FitnessCeiling *= 1.05
			return fmt.Sprintf("%s gained +5%% ceiling from STARDUSTUSSY transmission", horse.Name)
		}
		return ""

	default:
		// Economic/quarantine/non-stat effects — no direct horse modification.
		return ""
	}
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
