// Package fightussy implements the gladiatorial horse combat simulation engine
// for StallionUSSY. Retired horses get maces welded to their hooves and fight
// in arenas ranging from the noble Colosseum to the chaotic E-008 Containment
// Zone. Two horses enter, one horse leaves (usually).
package fightussy

import (
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
	"github.com/mojomast/stallionussy/internal/models"
)

// ---------------------------------------------------------------------------
// Arena types — where the carnage happens
// ---------------------------------------------------------------------------

const (
	ArenaColosseum   = "Colosseum"
	ArenaThunderdome = "Thunderdome"
	ArenaPit         = "The Pit"
	ArenaDrMittens   = "Dr. Mittens' Operating Theater"
	ArenaE008        = "E-008's Containment Zone"
)

// ---------------------------------------------------------------------------
// Mace types — the instruments of percussive persuasion
// ---------------------------------------------------------------------------

const (
	MaceStandard    = "Standard Mace"
	MaceMorningstar = "Spiked Morningstar"
	MaceHoly        = "Holy Mace of Pastor Router"
	MaceE008        = "E-008 Infused Mace"
	MaceGeoffrussy  = "The Geoffrussy's Gavel"
)

var allMaceTypes = []string{
	MaceStandard,
	MaceMorningstar,
	MaceHoly,
	MaceE008,
	MaceGeoffrussy,
}

// ---------------------------------------------------------------------------
// FightConfig — how the fight is set up
// ---------------------------------------------------------------------------

// FightConfig holds the parameters that define how a fight is set up.
type FightConfig struct {
	ArenaType string // "Colosseum", "Thunderdome", "The Pit", "Dr. Mittens' Operating Theater", "E-008's Containment Zone"
	MaxRounds int    // default 10
	Purse     int64  // winner's purse
	IsToDeath bool   // true = loser dies permanently. false = loser just gets injured
	Seed      uint64 // BUG 7 FIX: if non-zero, use as deterministic RNG seed for fight replay
}

// defaultMaxRounds is used when MaxRounds is zero.
const defaultMaxRounds = 10

// ---------------------------------------------------------------------------
// FightEntry — a combatant's state during a fight
// ---------------------------------------------------------------------------

// FightEntry represents one horse's combat state in a fight.
type FightEntry struct {
	HorseID   string
	HorseName string
	HP        float64 // calculated from STM gene + fitness: baseHP(150) * stmScore * fitness
	MaxHP     float64
	Attack    float64 // from SZE gene: baseAtk(20) * szeScore * fitness
	Defense   float64 // from STM + SZE: baseDef(10) * (stm+sze)/2 * fitness
	Speed     float64 // from SPD: determines who strikes first, dodge chance
	Rage      float64 // from TMP: starts at 0, builds per round, at 100 = berserk mode (2x dmg, 0 def)
	Morale    float64 // starts at 100, drops when hit hard, at 0 = surrender (if not to-death)
	MaceType  string  // random: one of the five sacred maces
}

// ---------------------------------------------------------------------------
// FightResult — the full outcome of a gladiatorial bout
// ---------------------------------------------------------------------------

// FightResult encodes the complete outcome of a horse fight.
type FightResult struct {
	ID         string
	ArenaType  string
	Entries    [2]FightEntry
	Rounds     []FightRound
	WinnerID   string // horse ID (empty if mutual destruction)
	LoserID    string
	WinnerName string
	LoserName  string
	IsFatality bool // true if loser died
	KORound    int  // round where fight ended
	TotalTicks int
	Purse      int64
	Narrative  []string // blow-by-blow commentary
	CreatedAt  time.Time
}

// FightRound records the state of one round of combat.
type FightRound struct {
	Round    int
	Events   []FightEvent
	HP1After float64
	HP2After float64
	Rage1    float64
	Rage2    float64
}

// FightEvent records a single tick-level event within a round.
type FightEvent struct {
	Tick       int
	AttackerID string
	Event      string // "hit", "critical", "dodge", "mace_malfunction", etc.
	Damage     float64
	Text       string // narrative text
}

// ---------------------------------------------------------------------------
// Gene helpers — safe extraction from a Genome
// ---------------------------------------------------------------------------

func geneScore(g models.Genome, gt models.GeneType) float64 {
	if gene, ok := g[gt]; ok {
		return gene.GeneScore()
	}
	return 0.3
}

// ---------------------------------------------------------------------------
// buildEntry — construct a FightEntry from a Horse
// ---------------------------------------------------------------------------

func buildEntry(horse *models.Horse, rng *rand.Rand) FightEntry {
	stm := geneScore(horse.Genome, models.GeneSTM)
	sze := geneScore(horse.Genome, models.GeneSZE)
	spd := geneScore(horse.Genome, models.GeneSPD)
	tmp := geneScore(horse.Genome, models.GeneTMP) // BUG 10 FIX: use TMP gene
	fit := horse.CurrentFitness
	if fit < 0.1 {
		fit = 0.1 // minimum floor so horses aren't literally dead on arrival
	}

	hp := 150.0 * stm * fit
	atk := 20.0 * sze * fit
	def := 10.0 * ((stm + sze) / 2.0) * fit
	speed := spd * fit

	mace := allMaceTypes[rng.IntN(len(allMaceTypes))]

	// BUG 10 FIX: TMP gene influences starting rage — hot-tempered horses start angrier
	startingRage := tmp * 30.0

	return FightEntry{
		HorseID:   horse.ID,
		HorseName: horse.Name,
		HP:        hp,
		MaxHP:     hp,
		Attack:    atk,
		Defense:   def,
		Speed:     speed,
		Rage:      startingRage,
		Morale:    100,
		MaceType:  mace,
	}
}

// ---------------------------------------------------------------------------
// Arena modifiers
// ---------------------------------------------------------------------------

type arenaModifiers struct {
	damageMultiplier float64
	passiveDamage    float64 // HP lost per round by BOTH combatants
	szeBonus         float64 // extra damage for SZE-heavy horses
	spdPenalty       float64 // multiplied onto speed
	intBonus         float64 // multiplied onto INT-based effects
	chaosMode        bool    // random stat flips
	yogurtMutChance  float64 // per-round chance of yogurt-based mutation
	healEventChance  float64 // per-round chance of random healing
}

func getArenaModifiers(arenaType string) arenaModifiers {
	switch arenaType {
	case ArenaThunderdome:
		return arenaModifiers{
			damageMultiplier: 1.20,
			passiveDamage:    5.0,
		}
	case ArenaPit:
		return arenaModifiers{
			damageMultiplier: 1.0,
			szeBonus:         0.15,
			spdPenalty:       0.85, // 15% speed penalty
		}
	case ArenaDrMittens:
		return arenaModifiers{
			damageMultiplier: 1.0,
			intBonus:         1.20,
			healEventChance:  0.10,
		}
	case ArenaE008:
		return arenaModifiers{
			damageMultiplier: 1.0,
			chaosMode:        true,
			yogurtMutChance:  0.05,
		}
	default: // Colosseum — standard
		return arenaModifiers{
			damageMultiplier: 1.0,
		}
	}
}

// ---------------------------------------------------------------------------
// Mace modifiers
// ---------------------------------------------------------------------------

type maceModifiers struct {
	damageMultiplier float64
	selfInjuryChance float64 // chance of hurting yourself per swing
	moralePerRound   float64 // morale bonus to wielder per round
	randomDamage     bool    // 50-150% random damage
	healOpponentPct  float64 // chance of healing opponent instead
	speedPenalty     float64 // multiplied onto speed
}

func getMaceModifiers(maceType string) maceModifiers {
	switch maceType {
	case MaceMorningstar:
		return maceModifiers{damageMultiplier: 1.15, selfInjuryChance: 0.05}
	case MaceHoly:
		return maceModifiers{damageMultiplier: 1.10, moralePerRound: 10.0}
	case MaceE008:
		return maceModifiers{damageMultiplier: 1.0, randomDamage: true, healOpponentPct: 0.03}
	case MaceGeoffrussy:
		return maceModifiers{damageMultiplier: 1.25, speedPenalty: 0.80}
	default: // Standard Mace
		return maceModifiers{damageMultiplier: 1.0}
	}
}

// ---------------------------------------------------------------------------
// Narrative templates — the heart and soul of the commentary
// ---------------------------------------------------------------------------

var hitNarratives = []string{
	"%s swings the %s with reckless abandon! %s takes %.0f damage to the flank! The crowd goes absolutely unhinged!",
	"%s connects with a THUNDEROUS blow from the %s! %.0f damage to %s! Teeth are optional after that one!",
	"The %s in %s's hoof CRUNCHES into %s's ribcage! %.0f damage! You can hear it from the cheap seats!",
	"%s rears up and SLAMS the %s down onto %s! %.0f damage! That's gonna leave a mark... and a dent... and a crater!",
	"A devastating horizontal sweep from %s! The %s catches %s right in the withers! %.0f damage! OUCH!",
	"%s does a full 360 spin and THWACKS %s with the %s! %.0f damage! Style points through the roof!",
	"WHAM! %s brings the %s down like a HAMMER! %s staggers! %.0f damage! The arena SHAKES!",
	"%s charges forward and SMASHES the %s into %s's shoulder! %.0f damage! BONE CRUNCHING NOISES!",
}

var criticalNarratives = []string{
	"CRITICAL HIT! %s's %s connects with DIVINE FURY! 'MAY YOUR PACKETS NEVER DROP!' screams the crowd! %.0f damage to %s!",
	"CRITICAL HIT! The %s IGNITES as %s brings it down on %s! %.0f DAMAGE! The mace is literally ON FIRE! HOW?!",
	"CRITICAL HIT! %s finds the weak spot! The %s strikes %s with surgical precision! %.0f DAMAGE! DR. MITTENS WOULD BE IMPRESSED!",
	"OH MY GOD! CRITICAL HIT! %s's %s DETONATES on contact with %s! %.0f DAMAGE! The shockwave knocks over a hot dog vendor!",
	"CRITICAL HIT! %s channels the spirit of every retired horse and ANNIHILATES %s with the %s! %.0f DAMAGE! THE CROWD LOSES ITS COLLECTIVE MIND!",
}

// BUG 3 FIX: All templates standardized to 3 args: (defenderName, maceType, attackerName)
var dodgeNarratives = []string{
	"%s DODGES the %s with the grace of a caffeinated gazelle! %s stumbles face-first into the arena wall!",
	"%s sidesteps the %s like a GHOST! %s swings at nothing but air and regret!",
	"MISSED! %s ducks under the %s with impossible timing! %s overcommits and eats dirt!",
	"%s performs a MATRIX-LEVEL dodge of the %s! It whistles past their ear! %s can't believe it!",
	"DODGED! %s weaves past the %s like a drunken ballerina! %s hits nothing but hopes and dreams!",
	"%s saw that %s coming from a MILE away! They leap sideways as %s's swing crashes into the ground!",
}

var maceMalfunctionNarratives = []string{
	"MACE MALFUNCTION! %s's %s flies off their hoof and into the crowd! A spectator catches it and refuses to give it back!",
	"DISASTER! The %s DETACHES from %s's hoof mid-swing! It clatters across the arena! %s spends this tick looking embarrassed!",
	"OH NO! %s's %s falls apart! The duct tape holding it on has finally given up! Someone get the arena blacksmith!",
}

var rageExplosionNarratives = []string{
	"RAGE EXPLOSION! %s's eyes turn BLOOD RED! They go ABSOLUTELY BERSERK! %.0f DAMAGE to %s but %s hurts themselves for %.0f!",
	"BERSERKER MODE ENGAGED! %s SCREAMS and brings the %s down with the force of a THOUSAND SUNS! %.0f to %s! Self-damage: %.0f! WORTH IT!",
	"%s HAS LOST ALL CONTROL! Pure RAGE fuels a devastating strike! %.0f damage to %s! %s takes %.0f recoil! THE CROWD IS TERRIFIED AND DELIGHTED!",
}

// BUG 2 FIX: All templates standardized to 4 args: (attackerName, maceType, damage, defenderName)
var desperateLungeHitNarratives = []string{
	"DESPERATE LUNGE! %s swings the %s wildly — nearly dead but REFUSES TO QUIT! %.0f MASSIVE DAMAGE to %s!",
	"WITH NOTHING LEFT TO LOSE, %s makes one final DESPERATE charge with the %s! IT CONNECTS! %.0f DAMAGE to %s! WHAT A MOMENT!",
	"LAST STAND! %s summons every remaining ounce of energy and hurls the %s for a LEGENDARY lunge! %.0f to %s! THE ARENA ERUPTS!",
}

var desperateLungeMissNarratives = []string{
	"DESPERATE LUNGE! %s goes all-in... and MISSES! They faceplant into the arena floor! The crowd winces sympathetically!",
	"%s attempts a last-ditch charge and TRIPS over their own hooves! The %s goes flying! This is just sad now!",
	"A final desperate swing from %s... and they FALL FLAT! The crowd offers a polite golf clap of pity!",
}

var crowdOatsNarratives = []string{
	"THE CROWD THROWS OATS! Both fighters pause to munch appreciatively! +10 HP each! The referee allows it because honestly, who's going to stop them?",
	"OATS FROM THE SKY! A generous spectator dumps a bucket of premium oats into the arena! Both horses heal 10 HP! Sportsmanship!",
	"THE OATS RAIN DOWN! Both %s and %s take a snack break mid-combat! +10 HP each! The crowd cheers for EVERYONE!",
}

var derulNarratives = []string{
	"JASON DERULO FALLS INTO THE ARENA! He stumbles through a railing and lands face-first in the sand! Both %s and %s STOP to stare! 'Jason Derulo!' he announces from the ground. Rage resets!",
	"IS THAT... JASON DERULO?! He's somehow tumbled out of the VIP box DIRECTLY into the fighting pit! Both horses freeze! He does a small wave! RAGE RESETS TO 50!",
	"OH MY GOD JASON DERULO HAS ENTERED THE ARENA! Not intentionally — he tripped! Both %s and %s are too confused to fight! He gets up, brushes himself off, and says his own name! Classic Derulo!",
}

var drMittensNarratives = []string{
	"DR. MITTENS RUNS INTO THE ARENA WITH A TINY STETHOSCOPE! 'This is HIGHLY irregular!' she hisses, healing %s for 15 HP!",
	"A small orange blur streaks across the arena — it's DR. MITTENS! She does a quick vitals check on %s and applies 15 HP of healing! 'You're welcome,' she meows judgmentally!",
	"DR. MITTENS INTERVENES! The cat DVM leaps onto %s and performs emergency hoof-CPR! +15 HP! She then vanishes in a puff of orange fur!",
}

var e008SentienceNarratives = []string{
	"E-008 SENTIENCE SURGE! The arena trembles! A wave of yogurt-energy SWAPS RANDOM STATS between %s and %s for the next 3 ticks! Reality is a SUGGESTION!",
	"THE YOGURT REMEMBERS! E-008 sends a pulse through the arena! %s and %s feel their very essence SHIFT! Stats swapped for 3 ticks!",
}

var hauntedMaceNarratives = []string{
	"HAUNTED MACE! %s's %s begins GLOWING with spectral energy! It starts swinging ON ITS OWN! 1.5x damage for 5 ticks! THE HORSE ISN'T EVEN TRYING!",
	"%s's %s is POSSESSED! A ghostly hoof grips the handle from beyond the grave! 1.5x damage for 5 ticks! This is technically a 2v1 now!",
}

var mutualRespectNarratives = []string{
	"MUTUAL RESPECT MOMENT! Both %s and %s lock eyes and give a solemn nod. +20 morale each, -10 rage. The crowd sheds a single collective tear.",
	"A MOMENT OF HONOR! %s and %s touch maces gently — a gladiatorial salute! +20 morale, -10 rage. Even the referee wipes away a tear!",
}

var mutualDestructionNarratives = []string{
	"MUTUAL DESTRUCTION! Both horses collapse simultaneously! The referee — a cat in a tiny striped shirt — looks deeply disappointed.",
	"DOUBLE KNOCKOUT! %s and %s hit each other at the EXACT same moment and BOTH go down! The crowd doesn't know whether to cheer or cry! They do both!",
	"IT'S A DRAW BY DEATH! Both combatants fall in a heap of mace and regret! Nobody wins! Everyone loses! E-008 absorbs their essence probably!",
}

var surrenderNarratives = []string{
	"%s's morale has COLLAPSED! They drop the %s and raise their hooves in surrender! %s wins by MORAL VICTORY!",
	"%s has had ENOUGH! They trot to the corner of the arena and refuse to continue! %s takes the win! The crowd boos the quitter mercilessly!",
	"WHITE FLAG! %s waves a tiny white handkerchief with their mace hoof! It's pathetic and adorable! %s wins!",
}

var koNarratives = []string{
	"%s COLLAPSES! Their HP has hit ZERO! %s stands victorious, mace raised high, probably covered in questionable fluids!",
	"%s goes DOWN! The arena medics (two cats in lab coats) rush in! %s IS YOUR CHAMPION!",
	"IT'S OVER! %s crumples like a wet newspaper! %s bellows in triumph! THE CROWD GOES NUCLEAR!",
}

var fatalityNarratives = []string{
	"FATALITY! %s has been PERMANENTLY RETIRED by %s's %s! They will be remembered... probably... maybe... who are they again?",
	"DEATH IN THE ARENA! %s falls for the last time! %s shows no mercy! The crowd is equal parts horrified and entertained!",
	"%s has shuffled off this mortal coil via %s's %s! They're with the glue factory in the sky now!",
}

var roundStartNarratives = []string{
	"ROUND %d! DING DING DING! The combatants circle each other! Tension is UNBEARABLE!",
	"ROUND %d BEGINS! The crowd stamps their feet! The arena VIBRATES with anticipation!",
	"ROUND %d! Both fighters emerge from their corners! The maces GLEAM under the floodlights!",
}

var roundRecoveryNarratives = []string{
	"Between rounds: %s recovers %.0f HP from sheer determination (and possibly illegal yogurt supplements)!",
	"Rest period: %s heals %.0f HP! Their corner team (a gopher and a confused pigeon) works frantically!",
}

var yogurtMutationNarratives = []string{
	"E-008 YOGURT MUTATION! A wave of sentient dairy washes through the arena! %s's stats shift unpredictably! B.U.R.P. agents are SCRIBBLING NOTES!",
	"THE YOGURT EVOLVES! A tendril of E-008 touches %s! Their genes WOBBLE! Is this even legal?! (No. No it is not.)",
}

// ---------------------------------------------------------------------------
// SimulateFight — the main fight simulation function
// ---------------------------------------------------------------------------

// SimulateFight runs a complete gladiatorial combat between two horses and
// returns the full fight result with blow-by-blow narrative.
func SimulateFight(horse1, horse2 *models.Horse, config FightConfig) *FightResult {
	// BUG 7 FIX: Use deterministic seed if provided, otherwise random
	var rng *rand.Rand
	if config.Seed != 0 {
		rng = rand.New(rand.NewPCG(config.Seed, config.Seed))
	} else {
		rng = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	}

	if config.MaxRounds <= 0 {
		config.MaxRounds = defaultMaxRounds
	}
	if config.ArenaType == "" {
		config.ArenaType = ArenaColosseum
	}

	// Build entries
	entry1 := buildEntry(horse1, rng)
	entry2 := buildEntry(horse2, rng)

	// Apply mace speed penalties
	mace1Mods := getMaceModifiers(entry1.MaceType)
	mace2Mods := getMaceModifiers(entry2.MaceType)
	if mace1Mods.speedPenalty > 0 {
		entry1.Speed *= mace1Mods.speedPenalty
	}
	if mace2Mods.speedPenalty > 0 {
		entry2.Speed *= mace2Mods.speedPenalty
	}

	// Apply arena speed penalty (The Pit)
	arenaMods := getArenaModifiers(config.ArenaType)
	if arenaMods.spdPenalty > 0 {
		entry1.Speed *= arenaMods.spdPenalty
		entry2.Speed *= arenaMods.spdPenalty
	}

	result := &FightResult{
		ID:        uuid.New().String(),
		ArenaType: config.ArenaType,
		Entries:   [2]FightEntry{entry1, entry2},
		Purse:     config.Purse,
		CreatedAt: time.Now(),
	}

	// Arena intro narrative
	result.Narrative = append(result.Narrative,
		fmt.Sprintf("WELCOME TO THE %s! Tonight's gladiatorial combat will be LEGENDARY!", config.ArenaType),
		fmt.Sprintf("In the RED corner: %s! Armed with a %s! HP: %.0f, ATK: %.0f, DEF: %.0f!",
			entry1.HorseName, entry1.MaceType, entry1.HP, entry1.Attack, entry1.Defense),
		fmt.Sprintf("In the BLUE corner: %s! Armed with a %s! HP: %.0f, ATK: %.0f, DEF: %.0f!",
			entry2.HorseName, entry2.MaceType, entry2.HP, entry2.Attack, entry2.Defense),
		"The maces have been welded. The crowd is feral. LET THE CARNAGE BEGIN!",
	)

	totalTicks := 0
	fightOver := false

	// Track temporary effects
	type tempEffect struct {
		ticksLeft  int
		effectType string  // "stat_swap", "haunted_mace", "mace_malfunction"
		targetIdx  int     // 0 or 1
		savedValue float64 // original stat value for restoration (mace_malfunction)
	}
	var activeEffects []tempEffect

	// Store original stats for stat-swap restoration
	origAttack := [2]float64{entry1.Attack, entry2.Attack}
	origDefense := [2]float64{entry1.Defense, entry2.Defense}
	origSpeed := [2]float64{entry1.Speed, entry2.Speed}

	// We work with local pointers for the fight state
	e := [2]*FightEntry{&entry1, &entry2}

	for round := 1; round <= config.MaxRounds && !fightOver; round++ {
		// Round start narration
		result.Narrative = append(result.Narrative,
			fmt.Sprintf(roundStartNarratives[rng.IntN(len(roundStartNarratives))], round))

		// Apply per-round mace morale bonuses
		for idx := 0; idx < 2; idx++ {
			maceMods := getMaceModifiers(e[idx].MaceType)
			if maceMods.moralePerRound > 0 {
				e[idx].Morale = math.Min(100, e[idx].Morale+maceMods.moralePerRound)
			}
		}

		// Apply arena passive damage (Thunderdome)
		if arenaMods.passiveDamage > 0 {
			for idx := 0; idx < 2; idx++ {
				e[idx].HP -= arenaMods.passiveDamage
				if e[idx].HP < 0 {
					e[idx].HP = 0
				}
			}
			result.Narrative = append(result.Narrative,
				fmt.Sprintf("The ELECTRIC FLOOR crackles! Both fighters take %.0f passive damage! Welcome to the Thunderdome!", arenaMods.passiveDamage))
		}

		// Yogurt mutation chance (E-008's Containment Zone)
		if arenaMods.yogurtMutChance > 0 && rng.Float64() < arenaMods.yogurtMutChance {
			targetIdx := rng.IntN(2)
			// Randomly shift attack or defense by +-20%
			shift := 0.8 + rng.Float64()*0.4
			e[targetIdx].Attack *= shift
			e[targetIdx].Defense *= (2.0 - shift) // inverse shift
			result.Narrative = append(result.Narrative,
				fmt.Sprintf(yogurtMutationNarratives[rng.IntN(len(yogurtMutationNarratives))], e[targetIdx].HorseName))
		}

		// Dr. Mittens healing event (Dr. Mittens' Operating Theater)
		if arenaMods.healEventChance > 0 && rng.Float64() < arenaMods.healEventChance {
			// Heal the lower-HP fighter
			targetIdx := 0
			if e[1].HP < e[0].HP {
				targetIdx = 1
			}
			healAmt := 15.0
			e[targetIdx].HP = math.Min(e[targetIdx].MaxHP, e[targetIdx].HP+healAmt)
			result.Narrative = append(result.Narrative,
				fmt.Sprintf(drMittensNarratives[rng.IntN(len(drMittensNarratives))], e[targetIdx].HorseName))
		}

		fightRound := FightRound{Round: round}
		maxTicksPerRound := 50

		for tick := 1; tick <= maxTicksPerRound && !fightOver; tick++ {
			totalTicks++

			// Process active temporary effects
			for i := len(activeEffects) - 1; i >= 0; i-- {
				eff := &activeEffects[i]
				eff.ticksLeft--
				if eff.ticksLeft <= 0 {
					// Restore stats
					if eff.effectType == "stat_swap" {
						e[0].Attack = origAttack[0]
						e[0].Defense = origDefense[0]
						e[0].Speed = origSpeed[0]
						e[1].Attack = origAttack[1]
						e[1].Defense = origDefense[1]
						e[1].Speed = origSpeed[1]
					}
					// BUG FIX: Restore attack after mace malfunction expires.
					if eff.effectType == "mace_malfunction" {
						e[eff.targetIdx].Attack = eff.savedValue
					}
					activeEffects = append(activeEffects[:i], activeEffects[i+1:]...)
				}
			}

			// Determine attacker order — higher speed goes first with jitter
			speed1 := e[0].Speed + rng.Float64()*0.2 - 0.1
			speed2 := e[1].Speed + rng.Float64()*0.2 - 0.1
			attackerIdx := 0
			defenderIdx := 1
			if speed2 > speed1 {
				attackerIdx = 1
				defenderIdx = 0
			}

			atk := e[attackerIdx]
			def := e[defenderIdx]

			// === SPECIAL EVENTS BEFORE ATTACK ===

			// "THE CROWD THROWS OATS" (1%)
			if rng.Float64() < 0.01 {
				e[0].HP = math.Min(e[0].MaxHP, e[0].HP+10)
				e[1].HP = math.Min(e[1].MaxHP, e[1].HP+10)
				narr := crowdOatsNarratives[rng.IntN(len(crowdOatsNarratives))]
				if len(narr) > 0 && narr[0] == 'T' {
					// Templates with horse names
					narr = fmt.Sprintf(crowdOatsNarratives[2], e[0].HorseName, e[1].HorseName)
				}
				fightRound.Events = append(fightRound.Events, FightEvent{
					Tick: tick, AttackerID: "", Event: "crowd_oats", Damage: -10, Text: narr,
				})
				result.Narrative = append(result.Narrative, narr)
				continue
			}

			// "JASON DERULO FALLS INTO ARENA" (0.5%)
			if rng.Float64() < 0.005 {
				// BUG 4 FIX: Cap rage at 50 instead of setting to 50 (only reduces, never increases)
				e[0].Rage = math.Min(e[0].Rage, 50)
				e[1].Rage = math.Min(e[1].Rage, 50)
				narr := fmt.Sprintf(derulNarratives[rng.IntN(len(derulNarratives))], e[0].HorseName, e[1].HorseName)
				fightRound.Events = append(fightRound.Events, FightEvent{
					Tick: tick, AttackerID: "", Event: "derulo", Damage: 0, Text: narr,
				})
				result.Narrative = append(result.Narrative, narr)
				continue
			}

			// "DR. MITTENS INTERVENES" (1%) — per-tick version
			if rng.Float64() < 0.01 {
				targetIdx := 0
				if e[1].HP < e[0].HP {
					targetIdx = 1
				}
				e[targetIdx].HP = math.Min(e[targetIdx].MaxHP, e[targetIdx].HP+15)
				narr := fmt.Sprintf(drMittensNarratives[rng.IntN(len(drMittensNarratives))], e[targetIdx].HorseName)
				fightRound.Events = append(fightRound.Events, FightEvent{
					Tick: tick, AttackerID: "", Event: "dr_mittens", Damage: -15, Text: narr,
				})
				result.Narrative = append(result.Narrative, narr)
				continue
			}

			// "E-008 SENTIENCE SURGE" (1%) — stat swap for 3 ticks
			if rng.Float64() < 0.01 {
				// BUG 6 FIX: Skip if a stat_swap is already active to prevent corruption
				alreadySwapped := false
				for _, eff := range activeEffects {
					if eff.effectType == "stat_swap" {
						alreadySwapped = true
						break
					}
				}
				if alreadySwapped {
					// Skip — stat swap already in progress
				} else {
					// Save current stats as originals before swap
					origAttack = [2]float64{e[0].Attack, e[1].Attack}
					origDefense = [2]float64{e[0].Defense, e[1].Defense}
					origSpeed = [2]float64{e[0].Speed, e[1].Speed}

					// Swap
					e[0].Attack, e[1].Attack = e[1].Attack, e[0].Attack
					e[0].Defense, e[1].Defense = e[1].Defense, e[0].Defense
					e[0].Speed, e[1].Speed = e[1].Speed, e[0].Speed

					activeEffects = append(activeEffects, tempEffect{
						ticksLeft: 3, effectType: "stat_swap",
					})
					narr := fmt.Sprintf(e008SentienceNarratives[rng.IntN(len(e008SentienceNarratives))], e[0].HorseName, e[1].HorseName)
					fightRound.Events = append(fightRound.Events, FightEvent{
						Tick: tick, AttackerID: "", Event: "e008_sentience", Damage: 0, Text: narr,
					})
					result.Narrative = append(result.Narrative, narr)
				}
				continue
			}

			// "HAUNTED MACE" (0.5%)
			if rng.Float64() < 0.005 {
				targetIdx := rng.IntN(2)
				activeEffects = append(activeEffects, tempEffect{
					ticksLeft: 5, effectType: "haunted_mace", targetIdx: targetIdx,
				})
				narr := fmt.Sprintf(hauntedMaceNarratives[rng.IntN(len(hauntedMaceNarratives))], e[targetIdx].HorseName, e[targetIdx].MaceType)
				fightRound.Events = append(fightRound.Events, FightEvent{
					Tick: tick, AttackerID: e[targetIdx].HorseID, Event: "haunted_mace", Damage: 0, Text: narr,
				})
				result.Narrative = append(result.Narrative, narr)
			}

			// "MUTUAL RESPECT MOMENT" (0.3%)
			if rng.Float64() < 0.003 {
				e[0].Morale = math.Min(100, e[0].Morale+20)
				e[1].Morale = math.Min(100, e[1].Morale+20)
				e[0].Rage = math.Max(0, e[0].Rage-10)
				e[1].Rage = math.Max(0, e[1].Rage-10)
				narr := fmt.Sprintf(mutualRespectNarratives[rng.IntN(len(mutualRespectNarratives))], e[0].HorseName, e[1].HorseName)
				fightRound.Events = append(fightRound.Events, FightEvent{
					Tick: tick, AttackerID: "", Event: "mutual_respect", Damage: 0, Text: narr,
				})
				result.Narrative = append(result.Narrative, narr)
				continue
			}

			// === MACE MALFUNCTION (2%) ===
			if rng.Float64() < 0.02 {
				narr := fmt.Sprintf(maceMalfunctionNarratives[rng.IntN(len(maceMalfunctionNarratives))],
					atk.HorseName, atk.MaceType, atk.HorseName)
				fightRound.Events = append(fightRound.Events, FightEvent{
					Tick: tick, AttackerID: atk.HorseID, Event: "mace_malfunction", Damage: 0, Text: narr,
				})
				result.Narrative = append(result.Narrative, narr)
				// BUG FIX: Store original attack and schedule restoration after 3 ticks
				// instead of permanently reducing the base stat.
				savedAtk := atk.Attack
				atk.Attack *= 0.80
				activeEffects = append(activeEffects, tempEffect{
					ticksLeft: 3, effectType: "mace_malfunction", targetIdx: attackerIdx, savedValue: savedAtk,
				})
				continue
			}

			// === DESPERATE LUNGE (when HP < 20%) ===
			if atk.HP < atk.MaxHP*0.20 && rng.Float64() < 0.3 {
				if rng.Float64() < 0.5 {
					// HIT — 4x damage
					dmg := atk.Attack * 4.0
					atkMaceMods := getMaceModifiers(atk.MaceType)
					dmg *= atkMaceMods.damageMultiplier * arenaMods.damageMultiplier
					dmg -= def.Defense * 0.3 // reduced defense effectiveness
					if dmg < 1 {
						dmg = 1
					}
					def.HP -= dmg
					narr := fmt.Sprintf(desperateLungeHitNarratives[rng.IntN(len(desperateLungeHitNarratives))],
						atk.HorseName, atk.MaceType, dmg, def.HorseName)
					fightRound.Events = append(fightRound.Events, FightEvent{
						Tick: tick, AttackerID: atk.HorseID, Event: "desperate_lunge_hit", Damage: dmg, Text: narr,
					})
					result.Narrative = append(result.Narrative, narr)
				} else {
					// MISS + fall
					narr := fmt.Sprintf(desperateLungeMissNarratives[rng.IntN(len(desperateLungeMissNarratives))],
						atk.HorseName, atk.MaceType, atk.HorseName)
					fightRound.Events = append(fightRound.Events, FightEvent{
						Tick: tick, AttackerID: atk.HorseID, Event: "desperate_lunge_miss", Damage: 0, Text: narr,
					})
					result.Narrative = append(result.Narrative, narr)
					atk.Morale -= 15
				}
				goto checkDeath
			}

			// === RAGE EXPLOSION (when rage >= 80) ===
			if atk.Rage >= 80 && rng.Float64() < 0.4 {
				dmg := atk.Attack * 3.0
				atkMaceMods := getMaceModifiers(atk.MaceType)
				dmg *= atkMaceMods.damageMultiplier * arenaMods.damageMultiplier
				dmg -= def.Defense * 0.2
				if dmg < 1 {
					dmg = 1
				}
				selfDmg := atk.MaxHP * 0.20
				def.HP -= dmg
				atk.HP -= selfDmg
				narr := fmt.Sprintf(rageExplosionNarratives[rng.IntN(len(rageExplosionNarratives))],
					atk.HorseName, atk.MaceType, dmg, def.HorseName, atk.HorseName, selfDmg)
				fightRound.Events = append(fightRound.Events, FightEvent{
					Tick: tick, AttackerID: atk.HorseID, Event: "rage_explosion", Damage: dmg, Text: narr,
				})
				result.Narrative = append(result.Narrative, narr)
				atk.Rage = 30 // reset rage after explosion
				goto checkDeath
			}

			{
				// === NORMAL ATTACK RESOLUTION ===
				// Calculate hit chance
				hitChance := 0.70 + (atk.Speed-def.Speed)*0.1
				hitChance = math.Max(0.20, math.Min(0.95, hitChance))

				// Dodge check (15% base, modified by SPD)
				dodgeChance := 0.15 + (def.Speed-atk.Speed)*0.05
				dodgeChance = math.Max(0.05, math.Min(0.40, dodgeChance))

				// BUG 1 FIX: Clamp combined thresholds so dodge+hit doesn't exceed 1.0
				if dodgeChance+hitChance > 1.0 {
					hitChance = 1.0 - dodgeChance
				}

				// CRITICAL HIT (5%)
				isCrit := rng.Float64() < 0.05

				// BUG 8 FIX: chaosMode adds per-tick +-10% random variance to attack and defense
				atkAttack := atk.Attack
				defDefense := def.Defense
				if arenaMods.chaosMode {
					atkAttack *= (0.9 + rng.Float64()*0.2)  // +-10%
					defDefense *= (0.9 + rng.Float64()*0.2) // +-10%
				}

				// BUG 1 FIX: Single roll with combined thresholds
				roll := rng.Float64()

				if roll < dodgeChance {
					// DODGE!
					// BUG 3 FIX: All dodge templates now take 3 args: (defenderName, maceType, attackerName)
					narr := fmt.Sprintf(dodgeNarratives[rng.IntN(len(dodgeNarratives))],
						def.HorseName, atk.MaceType, atk.HorseName)
					fightRound.Events = append(fightRound.Events, FightEvent{
						Tick: tick, AttackerID: atk.HorseID, Event: "dodge", Damage: 0, Text: narr,
					})
					result.Narrative = append(result.Narrative, narr)
					def.Morale += 2 // confidence boost from dodging
					atk.Rage += 3   // frustration from missing
				} else if roll < dodgeChance+hitChance {
					// Calculate damage (use chaosMode-modified values)
					atkMaceMods := getMaceModifiers(atk.MaceType)
					rageBonus := 1.0 + atk.Rage/200.0
					dmg := atkAttack * rageBonus * atkMaceMods.damageMultiplier

					// Apply arena damage modifier
					dmg *= arenaMods.damageMultiplier

					// Apply arena SZE bonus (The Pit)
					if arenaMods.szeBonus > 0 {
						szeScore := geneScore(horse1.Genome, models.GeneSZE)
						if attackerIdx == 1 {
							szeScore = geneScore(horse2.Genome, models.GeneSZE)
						}
						if szeScore > 0.7 {
							dmg *= (1.0 + arenaMods.szeBonus)
						}
					}

					// E-008 mace random damage
					if atkMaceMods.randomDamage {
						randomFactor := 0.5 + rng.Float64() // 0.5 to 1.5
						dmg *= randomFactor
					}

					// Check if haunted mace is active for attacker
					for _, eff := range activeEffects {
						if eff.effectType == "haunted_mace" && eff.targetIdx == attackerIdx {
							dmg *= 1.50
						}
					}

					// Critical hit multiplier
					if isCrit {
						dmg *= 2.5
					}

					// Subtract defense (use chaosMode-modified defDefense)
					effectiveDef := defDefense * 0.5
					if atk.Rage >= 100 {
						// Berserk mode: ignore defense, but defender also has 0 def
						effectiveDef = 0
					}
					dmg -= effectiveDef
					if dmg < 1 {
						dmg = 1
					}

					// E-008 mace: 3% chance to heal opponent instead
					if atkMaceMods.healOpponentPct > 0 && rng.Float64() < atkMaceMods.healOpponentPct {
						def.HP = math.Min(def.MaxHP, def.HP+dmg)
						narr := fmt.Sprintf("The %s glows ominously... and HEALS %s for %.0f HP?! The crowd is confused. E-008 is confused. Everyone is confused.",
							atk.MaceType, def.HorseName, dmg)
						fightRound.Events = append(fightRound.Events, FightEvent{
							Tick: tick, AttackerID: atk.HorseID, Event: "e008_heal", Damage: -dmg, Text: narr,
						})
						result.Narrative = append(result.Narrative, narr)
					} else {
						// Apply damage
						def.HP -= dmg

						// Self-injury from Spiked Morningstar
						selfDmg := 0.0
						if atkMaceMods.selfInjuryChance > 0 && rng.Float64() < atkMaceMods.selfInjuryChance {
							selfDmg = dmg * 0.15
							atk.HP -= selfDmg
						}

						// Narrative
						var narr string
						if isCrit {
							narr = fmt.Sprintf(criticalNarratives[rng.IntN(len(criticalNarratives))],
								atk.HorseName, atk.MaceType, atk.HorseName, dmg, def.HorseName)
							// Use simpler format
							narr = fmt.Sprintf("CRITICAL HIT! %s's %s connects with DEVASTATING force! %.0f DAMAGE to %s! The arena SHAKES!",
								atk.HorseName, atk.MaceType, dmg, def.HorseName)
						} else {
							narr = fmt.Sprintf("%s swings the %s! %.0f damage to %s!",
								atk.HorseName, atk.MaceType, dmg, def.HorseName)
							if hitIdx := rng.IntN(len(hitNarratives)); hitIdx < 3 {
								templates := []string{
									"%s swings the %s with reckless abandon! %s takes %.0f damage to the flank! The crowd goes absolutely unhinged!",
									"%s connects with a THUNDEROUS blow from the %s! %.0f damage to %s! Teeth are optional after that one!",
									"WHAM! %s brings the %s down like a HAMMER! %s staggers! %.0f damage! The arena SHAKES!",
								}
								switch hitIdx {
								case 0:
									narr = fmt.Sprintf(templates[0], atk.HorseName, atk.MaceType, def.HorseName, dmg)
								case 1:
									narr = fmt.Sprintf(templates[1], atk.HorseName, atk.MaceType, dmg, def.HorseName)
								case 2:
									narr = fmt.Sprintf(templates[2], atk.HorseName, atk.MaceType, def.HorseName, dmg)
								}
							}
						}

						if selfDmg > 0 {
							narr += fmt.Sprintf(" (The Spiked Morningstar bites back! %s takes %.0f self-damage!)", atk.HorseName, selfDmg)
						}

						evtType := "hit"
						if isCrit {
							evtType = "critical"
						}
						fightRound.Events = append(fightRound.Events, FightEvent{
							Tick: tick, AttackerID: atk.HorseID, Event: evtType, Damage: dmg, Text: narr,
						})
						result.Narrative = append(result.Narrative, narr)
					}

					// Rage and morale adjustments
					// BUG 10 FIX: Scale rage gain by TMP gene (hot-tempered horses gain rage faster)
					var atkTMP, defTMP float64
					if attackerIdx == 0 {
						atkTMP = geneScore(horse1.Genome, models.GeneTMP)
						defTMP = geneScore(horse2.Genome, models.GeneTMP)
					} else {
						atkTMP = geneScore(horse2.Genome, models.GeneTMP)
						defTMP = geneScore(horse1.Genome, models.GeneTMP)
					}
					atkRageMul := 1.0 + atkTMP*0.5
					defRageMul := 1.0 + defTMP*0.5
					atk.Rage += 3 * atkRageMul // rage from hitting
					def.Rage += 5 * defRageMul // rage from being hit

					// BUG 5 FIX: Use actual computed dmg for morale calculation instead of recalculating
					if def.MaxHP > 0 {
						dmgPct := dmg / def.MaxHP
						if dmgPct > 0.30 {
							def.Morale -= 10
						} else {
							def.Morale -= 5
						}
					}
				} else {
					// Miss (low hit chance)
					narr := fmt.Sprintf("%s swings wildly but WHIFFS! The %s cuts through empty air! Embarrassing!", atk.HorseName, atk.MaceType)
					fightRound.Events = append(fightRound.Events, FightEvent{
						Tick: tick, AttackerID: atk.HorseID, Event: "miss", Damage: 0, Text: narr,
					})
					result.Narrative = append(result.Narrative, narr)
					atk.Rage += 2
				}
			}

		checkDeath:
			// Clamp values
			e[0].Rage = math.Min(100, math.Max(0, e[0].Rage))
			e[1].Rage = math.Min(100, math.Max(0, e[1].Rage))
			e[0].Morale = math.Min(100, math.Max(0, e[0].Morale))
			e[1].Morale = math.Min(100, math.Max(0, e[1].Morale))

			// Check for death / KO
			bothDead := e[0].HP <= 0 && e[1].HP <= 0
			// Mutual destruction chance (2%) if both are below 30% HP
			if !bothDead && e[0].HP < e[0].MaxHP*0.30 && e[1].HP < e[1].MaxHP*0.30 {
				if rng.Float64() < 0.02 {
					bothDead = true
					e[0].HP = 0
					e[1].HP = 0
				}
			}

			if bothDead {
				narr := fmt.Sprintf(mutualDestructionNarratives[rng.IntN(len(mutualDestructionNarratives))],
					e[0].HorseName, e[1].HorseName)
				result.Narrative = append(result.Narrative, narr)
				result.WinnerID = "" // no winner
				result.IsFatality = config.IsToDeath
				result.KORound = round
				fightOver = true
			} else if e[0].HP <= 0 {
				result.WinnerID = e[1].HorseID
				result.WinnerName = e[1].HorseName
				result.LoserID = e[0].HorseID
				result.LoserName = e[0].HorseName
				result.IsFatality = config.IsToDeath
				result.KORound = round
				if config.IsToDeath {
					narr := fmt.Sprintf(fatalityNarratives[rng.IntN(len(fatalityNarratives))],
						e[0].HorseName, e[1].HorseName, e[1].MaceType)
					result.Narrative = append(result.Narrative, narr)
				} else {
					narr := fmt.Sprintf(koNarratives[rng.IntN(len(koNarratives))],
						e[0].HorseName, e[1].HorseName)
					result.Narrative = append(result.Narrative, narr)
				}
				fightOver = true
			} else if e[1].HP <= 0 {
				result.WinnerID = e[0].HorseID
				result.WinnerName = e[0].HorseName
				result.LoserID = e[1].HorseID
				result.LoserName = e[1].HorseName
				result.IsFatality = config.IsToDeath
				result.KORound = round
				if config.IsToDeath {
					narr := fmt.Sprintf(fatalityNarratives[rng.IntN(len(fatalityNarratives))],
						e[1].HorseName, e[0].HorseName, e[0].MaceType)
					result.Narrative = append(result.Narrative, narr)
				} else {
					narr := fmt.Sprintf(koNarratives[rng.IntN(len(koNarratives))],
						e[1].HorseName, e[0].HorseName)
					result.Narrative = append(result.Narrative, narr)
				}
				fightOver = true
			}

			// Check surrender (morale <= 0 in non-death matches)
			if !fightOver && !config.IsToDeath {
				if e[0].Morale <= 0 {
					narr := fmt.Sprintf(surrenderNarratives[rng.IntN(len(surrenderNarratives))],
						e[0].HorseName, e[0].MaceType, e[1].HorseName)
					result.Narrative = append(result.Narrative, narr)
					result.WinnerID = e[1].HorseID
					result.WinnerName = e[1].HorseName
					result.LoserID = e[0].HorseID
					result.LoserName = e[0].HorseName
					result.IsFatality = false
					result.KORound = round
					fightOver = true
				} else if e[1].Morale <= 0 {
					narr := fmt.Sprintf(surrenderNarratives[rng.IntN(len(surrenderNarratives))],
						e[1].HorseName, e[1].MaceType, e[0].HorseName)
					result.Narrative = append(result.Narrative, narr)
					result.WinnerID = e[0].HorseID
					result.WinnerName = e[0].HorseName
					result.LoserID = e[1].HorseID
					result.LoserName = e[1].HorseName
					result.IsFatality = false
					result.KORound = round
					fightOver = true
				}
			}

			if fightOver {
				break
			}
		} // end tick loop

		// Record round state
		fightRound.HP1After = e[0].HP
		fightRound.HP2After = e[1].HP
		fightRound.Rage1 = e[0].Rage
		fightRound.Rage2 = e[1].Rage
		result.Rounds = append(result.Rounds, fightRound)

		if fightOver {
			break
		}

		// === Between-round recovery (REC gene) ===
		for idx := 0; idx < 2; idx++ {
			var horse *models.Horse
			if idx == 0 {
				horse = horse1
			} else {
				horse = horse2
			}
			recScore := geneScore(horse.Genome, models.GeneREC)
			recovery := 10.0 * recScore // base 10 HP recovery * REC score
			// INT bonus in Dr. Mittens arena
			if arenaMods.intBonus > 0 {
				intScore := geneScore(horse.Genome, models.GeneINT)
				recovery *= (1.0 + (arenaMods.intBonus-1.0)*intScore)
			}
			oldHP := e[idx].HP
			e[idx].HP = math.Min(e[idx].MaxHP, e[idx].HP+recovery)
			actualRecovery := e[idx].HP - oldHP
			if actualRecovery > 0 {
				narr := fmt.Sprintf(roundRecoveryNarratives[rng.IntN(len(roundRecoveryNarratives))],
					e[idx].HorseName, actualRecovery)
				result.Narrative = append(result.Narrative, narr)
			}
		}

		// Slight rage decay between rounds
		e[0].Rage = math.Max(0, e[0].Rage-5)
		e[1].Rage = math.Max(0, e[1].Rage-5)
	} // end round loop

	// If fight lasted all rounds without a KO — judge by HP
	if !fightOver {
		if e[0].HP > e[1].HP {
			result.WinnerID = e[0].HorseID
			result.WinnerName = e[0].HorseName
			result.LoserID = e[1].HorseID
			result.LoserName = e[1].HorseName
		} else if e[1].HP > e[0].HP {
			result.WinnerID = e[1].HorseID
			result.WinnerName = e[1].HorseName
			result.LoserID = e[0].HorseID
			result.LoserName = e[0].HorseName
		} else {
			// Perfect tie — mutual destruction
			result.WinnerID = ""
		}
		result.KORound = config.MaxRounds
		if result.WinnerID == "" {
			result.Narrative = append(result.Narrative,
				"AFTER ALL ROUNDS, IT'S A DRAW! Both fighters are equally battered! The judges flip a coin — it lands on its EDGE! NOBODY WINS!")
		} else {
			result.Narrative = append(result.Narrative,
				fmt.Sprintf("THE BELL RINGS! After %d rounds, %s wins on POINTS! HP: %.0f vs %.0f! A HARD-FOUGHT victory!",
					config.MaxRounds, result.WinnerName, e[0].HP, e[1].HP))
		}
		result.IsFatality = false // no death in judges' decision
	}

	result.TotalTicks = totalTicks
	// Update the entries in the result with final state
	result.Entries[0] = *e[0]
	result.Entries[1] = *e[1]

	// Final narrative
	if result.WinnerID != "" {
		result.Narrative = append(result.Narrative,
			fmt.Sprintf("THE CROWD ERUPTS! %s IS VICTORIOUS! Purse: %d cummies!", result.WinnerName, result.Purse))
		if result.IsFatality {
			result.Narrative = append(result.Narrative,
				fmt.Sprintf("A moment of silence for %s... they fought bravely. The glue factory sends its regards.", result.LoserName))
		}
	}

	return result
}
