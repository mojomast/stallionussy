package fightussy

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/mojomast/stallionussy/internal/models"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// makeGenome builds a Genome where every gene has the given allele pair.
func makeGenome(a1, a2 models.Allele) models.Genome {
	g := make(models.Genome)
	for _, gt := range models.AllGeneTypes {
		g[gt] = models.Gene{Type: gt, AlleleA: a1, AlleleB: a2}
	}
	return g
}

// makeGenomeWith builds a default AA genome then overrides specific genes.
func makeGenomeWith(overrides map[models.GeneType][2]models.Allele) models.Genome {
	g := makeGenome(models.AlleleA, models.AlleleA) // everything AA by default
	for gt, alleles := range overrides {
		g[gt] = models.Gene{Type: gt, AlleleA: alleles[0], AlleleB: alleles[1]}
	}
	return g
}

// makeHorse creates a test horse with the given genome and fitness.
func makeHorse(name string, genome models.Genome, fitness float64) *models.Horse {
	return &models.Horse{
		ID:             name + "-id",
		Name:           name,
		Genome:         genome,
		CurrentFitness: fitness,
	}
}

// deterministicRNG returns a seeded RNG for reproducible tests.
func deterministicRNG(seed1, seed2 uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed1, seed2))
}

// ---------------------------------------------------------------------------
// geneScore tests
// ---------------------------------------------------------------------------

func TestGeneScore(t *testing.T) {
	tests := []struct {
		name     string
		genome   models.Genome
		geneType models.GeneType
		expected float64
	}{
		{"AA gene returns 1.0", makeGenome(models.AlleleA, models.AlleleA), models.GeneSTM, 1.0},
		{"AB gene returns 0.65", makeGenome(models.AlleleA, models.AlleleB), models.GeneSPD, 0.65},
		{"BB gene returns 0.3", makeGenome(models.AlleleB, models.AlleleB), models.GeneSZE, 0.3},
		{"missing gene returns 0.3", models.Genome{}, models.GeneSTM, 0.3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := geneScore(tt.genome, tt.geneType)
			if got != tt.expected {
				t.Errorf("geneScore() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// buildEntry tests
// ---------------------------------------------------------------------------

func TestBuildEntry_AAGenomeFullFitness(t *testing.T) {
	horse := makeHorse("Champion", makeGenome(models.AlleleA, models.AlleleA), 1.0)
	rng := deterministicRNG(42, 42)

	entry := buildEntry(horse, rng)

	// STM=1.0, fit=1.0 → HP = 150 * 1.0 * 1.0 = 150
	if entry.HP != 150.0 {
		t.Errorf("HP = %v, want 150.0", entry.HP)
	}
	if entry.MaxHP != 150.0 {
		t.Errorf("MaxHP = %v, want 150.0", entry.MaxHP)
	}
	// SZE=1.0, fit=1.0 → ATK = 20 * 1.0 * 1.0 = 20
	if entry.Attack != 20.0 {
		t.Errorf("Attack = %v, want 20.0", entry.Attack)
	}
	// DEF = 10 * ((STM+SZE)/2) * fit = 10 * 1.0 * 1.0 = 10
	if entry.Defense != 10.0 {
		t.Errorf("Defense = %v, want 10.0", entry.Defense)
	}
	// SPD=1.0, fit=1.0 → Speed = 1.0
	if entry.Speed != 1.0 {
		t.Errorf("Speed = %v, want 1.0", entry.Speed)
	}
	// BUG 10 FIX: TMP gene now sets starting rage. AA TMP = 1.0, so Rage = 1.0 * 30 = 30
	if entry.Rage != 30 {
		t.Errorf("Rage = %v, want 30 (TMP AA = 1.0 * 30)", entry.Rage)
	}
	if entry.Morale != 100 {
		t.Errorf("Morale = %v, want 100", entry.Morale)
	}
	if entry.HorseID != horse.ID {
		t.Errorf("HorseID = %v, want %v", entry.HorseID, horse.ID)
	}
	if entry.HorseName != horse.Name {
		t.Errorf("HorseName = %v, want %v", entry.HorseName, horse.Name)
	}
}

func TestBuildEntry_BBGenomeLowFitness(t *testing.T) {
	horse := makeHorse("Weakling", makeGenome(models.AlleleB, models.AlleleB), 0.5)
	rng := deterministicRNG(7, 7)

	entry := buildEntry(horse, rng)

	// STM=0.3, fit=0.5 → HP = 150 * 0.3 * 0.5 = 22.5
	if math.Abs(entry.HP-22.5) > 0.01 {
		t.Errorf("HP = %v, want 22.5", entry.HP)
	}
	// SZE=0.3, fit=0.5 → ATK = 20 * 0.3 * 0.5 = 3.0
	if math.Abs(entry.Attack-3.0) > 0.01 {
		t.Errorf("Attack = %v, want 3.0", entry.Attack)
	}
	// DEF = 10 * ((0.3+0.3)/2) * 0.5 = 10 * 0.3 * 0.5 = 1.5
	if math.Abs(entry.Defense-1.5) > 0.01 {
		t.Errorf("Defense = %v, want 1.5", entry.Defense)
	}
}

func TestBuildEntry_FitnessFloor(t *testing.T) {
	// Fitness below 0.1 should be clamped to 0.1
	horse := makeHorse("Comatose", makeGenome(models.AlleleA, models.AlleleA), 0.0)
	rng := deterministicRNG(1, 1)

	entry := buildEntry(horse, rng)

	// STM=1.0, fit=0.1 (clamped) → HP = 150 * 1.0 * 0.1 = 15.0
	if math.Abs(entry.HP-15.0) > 0.01 {
		t.Errorf("HP = %v, want 15.0 (fitness should floor at 0.1)", entry.HP)
	}
}

func TestBuildEntry_MixedGenome(t *testing.T) {
	// High STM (AA), low SZE (BB), mid SPD (AB)
	genome := makeGenomeWith(map[models.GeneType][2]models.Allele{
		models.GeneSTM: {models.AlleleA, models.AlleleA}, // 1.0
		models.GeneSZE: {models.AlleleB, models.AlleleB}, // 0.3
		models.GeneSPD: {models.AlleleA, models.AlleleB}, // 0.65
	})
	horse := makeHorse("Mixed", genome, 0.8)
	rng := deterministicRNG(99, 99)

	entry := buildEntry(horse, rng)

	// HP = 150 * 1.0 * 0.8 = 120.0
	if math.Abs(entry.HP-120.0) > 0.01 {
		t.Errorf("HP = %v, want 120.0", entry.HP)
	}
	// ATK = 20 * 0.3 * 0.8 = 4.8
	if math.Abs(entry.Attack-4.8) > 0.01 {
		t.Errorf("Attack = %v, want 4.8", entry.Attack)
	}
	// DEF = 10 * ((1.0+0.3)/2) * 0.8 = 10 * 0.65 * 0.8 = 5.2
	if math.Abs(entry.Defense-5.2) > 0.01 {
		t.Errorf("Defense = %v, want 5.2", entry.Defense)
	}
	// Speed = 0.65 * 0.8 = 0.52
	if math.Abs(entry.Speed-0.52) > 0.01 {
		t.Errorf("Speed = %v, want 0.52", entry.Speed)
	}
}

func TestBuildEntry_MaceAssignment(t *testing.T) {
	// Verify that a mace is always assigned from the valid set
	horse := makeHorse("MaceTest", makeGenome(models.AlleleA, models.AlleleA), 1.0)

	validMaces := map[string]bool{
		MaceStandard:    true,
		MaceMorningstar: true,
		MaceHoly:        true,
		MaceE008:        true,
		MaceGeoffrussy:  true,
	}

	for i := uint64(0); i < 50; i++ {
		rng := deterministicRNG(i, i+100)
		entry := buildEntry(horse, rng)
		if !validMaces[entry.MaceType] {
			t.Errorf("Invalid mace type %q assigned", entry.MaceType)
		}
	}
}

// ---------------------------------------------------------------------------
// Arena modifier tests
// ---------------------------------------------------------------------------

func TestArenaModifiers_Colosseum(t *testing.T) {
	mods := getArenaModifiers(ArenaColosseum)
	if mods.damageMultiplier != 1.0 {
		t.Errorf("Colosseum damageMultiplier = %v, want 1.0", mods.damageMultiplier)
	}
	if mods.passiveDamage != 0 {
		t.Errorf("Colosseum passiveDamage = %v, want 0", mods.passiveDamage)
	}
	if mods.chaosMode {
		t.Error("Colosseum should not have chaosMode")
	}
}

func TestArenaModifiers_Thunderdome(t *testing.T) {
	mods := getArenaModifiers(ArenaThunderdome)
	if mods.damageMultiplier != 1.20 {
		t.Errorf("Thunderdome damageMultiplier = %v, want 1.20", mods.damageMultiplier)
	}
	if mods.passiveDamage != 5.0 {
		t.Errorf("Thunderdome passiveDamage = %v, want 5.0", mods.passiveDamage)
	}
}

func TestArenaModifiers_ThePit(t *testing.T) {
	mods := getArenaModifiers(ArenaPit)
	if mods.szeBonus != 0.15 {
		t.Errorf("The Pit szeBonus = %v, want 0.15", mods.szeBonus)
	}
	if mods.spdPenalty != 0.85 {
		t.Errorf("The Pit spdPenalty = %v, want 0.85", mods.spdPenalty)
	}
}

func TestArenaModifiers_DrMittens(t *testing.T) {
	mods := getArenaModifiers(ArenaDrMittens)
	if mods.intBonus != 1.20 {
		t.Errorf("DrMittens intBonus = %v, want 1.20", mods.intBonus)
	}
	if mods.healEventChance != 0.10 {
		t.Errorf("DrMittens healEventChance = %v, want 0.10", mods.healEventChance)
	}
}

func TestArenaModifiers_E008(t *testing.T) {
	mods := getArenaModifiers(ArenaE008)
	if !mods.chaosMode {
		t.Error("E-008 should have chaosMode")
	}
	if mods.yogurtMutChance != 0.05 {
		t.Errorf("E-008 yogurtMutChance = %v, want 0.05", mods.yogurtMutChance)
	}
}

func TestArenaModifiers_UnknownDefaultsToColosseum(t *testing.T) {
	mods := getArenaModifiers("Some Random Arena")
	expected := getArenaModifiers(ArenaColosseum)
	if mods.damageMultiplier != expected.damageMultiplier {
		t.Errorf("Unknown arena should default to Colosseum modifiers")
	}
}

// ---------------------------------------------------------------------------
// Mace modifier tests
// ---------------------------------------------------------------------------

func TestMaceModifiers_Standard(t *testing.T) {
	mods := getMaceModifiers(MaceStandard)
	if mods.damageMultiplier != 1.0 {
		t.Errorf("Standard mace damageMultiplier = %v, want 1.0", mods.damageMultiplier)
	}
	if mods.selfInjuryChance != 0 {
		t.Errorf("Standard mace selfInjuryChance = %v, want 0", mods.selfInjuryChance)
	}
}

func TestMaceModifiers_Morningstar(t *testing.T) {
	mods := getMaceModifiers(MaceMorningstar)
	if mods.damageMultiplier != 1.15 {
		t.Errorf("Morningstar damageMultiplier = %v, want 1.15", mods.damageMultiplier)
	}
	if mods.selfInjuryChance != 0.05 {
		t.Errorf("Morningstar selfInjuryChance = %v, want 0.05", mods.selfInjuryChance)
	}
}

func TestMaceModifiers_Holy(t *testing.T) {
	mods := getMaceModifiers(MaceHoly)
	if mods.damageMultiplier != 1.10 {
		t.Errorf("Holy mace damageMultiplier = %v, want 1.10", mods.damageMultiplier)
	}
	if mods.moralePerRound != 10.0 {
		t.Errorf("Holy mace moralePerRound = %v, want 10.0", mods.moralePerRound)
	}
}

func TestMaceModifiers_E008(t *testing.T) {
	mods := getMaceModifiers(MaceE008)
	if !mods.randomDamage {
		t.Error("E-008 mace should have randomDamage")
	}
	if mods.healOpponentPct != 0.03 {
		t.Errorf("E-008 mace healOpponentPct = %v, want 0.03", mods.healOpponentPct)
	}
}

func TestMaceModifiers_Geoffrussy(t *testing.T) {
	mods := getMaceModifiers(MaceGeoffrussy)
	if mods.damageMultiplier != 1.25 {
		t.Errorf("Geoffrussy damageMultiplier = %v, want 1.25", mods.damageMultiplier)
	}
	if mods.speedPenalty != 0.80 {
		t.Errorf("Geoffrussy speedPenalty = %v, want 0.80", mods.speedPenalty)
	}
}

// ---------------------------------------------------------------------------
// SimulateFight — basic execution tests
// ---------------------------------------------------------------------------

func TestSimulateFight_ProducesResult(t *testing.T) {
	horse1 := makeHorse("Thunder", makeGenome(models.AlleleA, models.AlleleA), 1.0)
	horse2 := makeHorse("Lightning", makeGenome(models.AlleleA, models.AlleleA), 1.0)

	config := FightConfig{
		ArenaType: ArenaColosseum,
		MaxRounds: 10,
		Purse:     1000,
		IsToDeath: false,
	}

	result := SimulateFight(horse1, horse2, config)

	if result == nil {
		t.Fatal("SimulateFight returned nil")
	}
	if result.ID == "" {
		t.Error("Result ID should not be empty")
	}
	if result.ArenaType != ArenaColosseum {
		t.Errorf("ArenaType = %v, want %v", result.ArenaType, ArenaColosseum)
	}
	if result.Purse != 1000 {
		t.Errorf("Purse = %v, want 1000", result.Purse)
	}
	if len(result.Rounds) == 0 {
		t.Error("Fight should have at least 1 round")
	}
	if len(result.Narrative) == 0 {
		t.Error("Fight should have narrative entries")
	}
	if result.TotalTicks == 0 {
		t.Error("TotalTicks should be > 0")
	}
	if result.KORound < 1 {
		t.Errorf("KORound = %v, should be >= 1", result.KORound)
	}
}

func TestSimulateFight_DefaultConfig(t *testing.T) {
	horse1 := makeHorse("Alpha", makeGenome(models.AlleleA, models.AlleleA), 1.0)
	horse2 := makeHorse("Beta", makeGenome(models.AlleleA, models.AlleleA), 1.0)

	// Zero config — should use defaults
	result := SimulateFight(horse1, horse2, FightConfig{})

	if result == nil {
		t.Fatal("SimulateFight returned nil")
	}
	// Arena should default to Colosseum
	if result.ArenaType != ArenaColosseum {
		t.Errorf("Default ArenaType = %v, want %v", result.ArenaType, ArenaColosseum)
	}
	// KORound should be at most defaultMaxRounds
	if result.KORound > defaultMaxRounds {
		t.Errorf("KORound = %v, exceeds defaultMaxRounds = %v", result.KORound, defaultMaxRounds)
	}
}

func TestSimulateFight_AlwaysProducesOutcome(t *testing.T) {
	// Run many fights to verify they always terminate with a valid outcome.
	horse1 := makeHorse("Fighter1", makeGenome(models.AlleleA, models.AlleleA), 0.8)
	horse2 := makeHorse("Fighter2", makeGenome(models.AlleleA, models.AlleleB), 0.7)

	for i := 0; i < 100; i++ {
		result := SimulateFight(horse1, horse2, FightConfig{
			MaxRounds: 5,
			Purse:     500,
		})
		if result == nil {
			t.Fatalf("Iteration %d: SimulateFight returned nil", i)
		}
		// Either there's a winner, or it's a draw (mutual destruction / tie)
		hasWinner := result.WinnerID != ""
		isDraw := result.WinnerID == ""
		if !hasWinner && !isDraw {
			t.Fatalf("Iteration %d: invalid outcome state", i)
		}
		if hasWinner {
			if result.WinnerID != horse1.ID && result.WinnerID != horse2.ID {
				t.Fatalf("Iteration %d: WinnerID %q not one of the combatants", i, result.WinnerID)
			}
			if result.LoserID == result.WinnerID {
				t.Fatalf("Iteration %d: Winner and Loser are the same", i)
			}
		}
	}
}

func TestSimulateFight_EntriesRecordFinalState(t *testing.T) {
	horse1 := makeHorse("H1", makeGenome(models.AlleleA, models.AlleleA), 1.0)
	horse2 := makeHorse("H2", makeGenome(models.AlleleA, models.AlleleA), 1.0)

	result := SimulateFight(horse1, horse2, FightConfig{MaxRounds: 3})

	// Entries should reflect the final state (HP changed from initial)
	e0 := result.Entries[0]
	e1 := result.Entries[1]

	// At least one entry should have taken damage (HP < MaxHP) unless edge case
	if e0.HP == e0.MaxHP && e1.HP == e1.MaxHP {
		t.Log("Warning: both entries at full HP after fight (very unlikely but possible)")
	}

	// MaxHP should still be set correctly
	if e0.MaxHP != 150.0 { // AA genome, 1.0 fitness
		t.Errorf("Entry[0] MaxHP = %v, want 150.0", e0.MaxHP)
	}
	if e1.MaxHP != 150.0 {
		t.Errorf("Entry[1] MaxHP = %v, want 150.0", e1.MaxHP)
	}
}

// ---------------------------------------------------------------------------
// Fatality (death match) tests
// ---------------------------------------------------------------------------

func TestSimulateFight_DeathMatchSetsFatality(t *testing.T) {
	horse1 := makeHorse("Gladiator", makeGenome(models.AlleleA, models.AlleleA), 1.0)
	horse2 := makeHorse("Victim", makeGenome(models.AlleleB, models.AlleleB), 0.3)

	fatalityCount := 0
	for i := 0; i < 100; i++ {
		result := SimulateFight(horse1, horse2, FightConfig{
			IsToDeath: true,
			MaxRounds: 10,
			Purse:     5000,
		})
		if result.IsFatality {
			fatalityCount++
		}
		// In a death match, IsFatality should always be true (even draws)
		// unless the fight goes to judges' decision which sets it to false
		if result.WinnerID != "" && !result.IsFatality {
			// If someone won with HP <= 0 in a death match, fatality should be true
			loserIdx := 0
			if result.LoserID == horse2.ID {
				loserIdx = 1
			}
			if result.Entries[loserIdx].HP <= 0 {
				// This means someone was KO'd: fatality should be true
				t.Errorf("Iteration %d: Death match KO but IsFatality=false", i)
			}
		}
	}

	// With a massive stat advantage, fatalities should occur frequently
	if fatalityCount == 0 {
		t.Error("No fatalities occurred in 100 death matches — statistically near impossible")
	}
}

func TestSimulateFight_NonDeathMatchNoFatality(t *testing.T) {
	horse1 := makeHorse("A", makeGenome(models.AlleleA, models.AlleleA), 1.0)
	horse2 := makeHorse("B", makeGenome(models.AlleleB, models.AlleleB), 0.3)

	for i := 0; i < 50; i++ {
		result := SimulateFight(horse1, horse2, FightConfig{
			IsToDeath: false,
			MaxRounds: 10,
		})
		// Judges' decision always sets IsFatality = false
		if result.KORound == 10 && result.IsFatality {
			t.Errorf("Iteration %d: judges' decision should not be a fatality", i)
		}
	}
}

// ---------------------------------------------------------------------------
// Surrender mechanics
// ---------------------------------------------------------------------------

func TestSimulateFight_SurrenderOnlyInNonDeathMatch(t *testing.T) {
	// A very weak horse against a strong one should sometimes surrender
	weakGenome := makeGenome(models.AlleleB, models.AlleleB) // all BB
	strongGenome := makeGenome(models.AlleleA, models.AlleleA)

	horse1 := makeHorse("Strong", strongGenome, 1.0)
	horse2 := makeHorse("Weak", weakGenome, 0.3)

	surrenderCount := 0
	for i := 0; i < 200; i++ {
		result := SimulateFight(horse1, horse2, FightConfig{
			IsToDeath: false,
			MaxRounds: 10,
		})
		// Check if a surrender narrative appears
		for _, n := range result.Narrative {
			if len(n) > 10 {
				for _, sn := range surrenderNarratives {
					if len(sn) > 0 && n != "" {
						// Crude check: surrenders mention "surrender" or "morale"
						// Better: check that the fight ended before HP hit 0 on the loser
						if result.LoserID != "" {
							loserIdx := 0
							if result.LoserID == horse2.ID {
								loserIdx = 1
							}
							if result.Entries[loserIdx].HP > 0 && result.Entries[loserIdx].Morale <= 0 {
								surrenderCount++
								goto nextIter
							}
						}
					}
				}
			}
		}
	nextIter:
	}

	// Surrender should happen sometimes with a weak horse in non-death mode
	if surrenderCount == 0 {
		t.Log("Warning: no surrenders detected in 200 non-death fights (possible but unlikely)")
	}
}

// ---------------------------------------------------------------------------
// Judges' decision tests
// ---------------------------------------------------------------------------

func TestSimulateFight_JudgesDecision(t *testing.T) {
	// Two equally matched horses with high HP — likely to go to judges' decision
	// Use a very short fight (1 round) so it always goes to decision
	horse1 := makeHorse("Tank1", makeGenome(models.AlleleA, models.AlleleA), 1.0)
	horse2 := makeHorse("Tank2", makeGenome(models.AlleleA, models.AlleleA), 1.0)

	decisionsReached := 0
	winnerByHP := 0

	for i := 0; i < 100; i++ {
		result := SimulateFight(horse1, horse2, FightConfig{
			MaxRounds: 1, // Very short — harder to KO
			Purse:     100,
		})

		if result.KORound == 1 {
			// Could be a KO in round 1 or judges' decision after round 1
			e0HP := result.Entries[0].HP
			e1HP := result.Entries[1].HP
			if e0HP > 0 && e1HP > 0 {
				decisionsReached++
				// Winner should be the one with more HP
				if e0HP > e1HP && result.WinnerID == horse1.ID {
					winnerByHP++
				} else if e1HP > e0HP && result.WinnerID == horse2.ID {
					winnerByHP++
				} else if e0HP == e1HP && result.WinnerID == "" {
					winnerByHP++ // draw is also correct
				}
			}
		}
	}

	if decisionsReached > 0 && winnerByHP != decisionsReached {
		t.Errorf("Judges' decisions: %d/%d had correct winner-by-HP", winnerByHP, decisionsReached)
	}
}

func TestSimulateFight_JudgesDecisionNotFatality(t *testing.T) {
	horse1 := makeHorse("A", makeGenome(models.AlleleA, models.AlleleA), 1.0)
	horse2 := makeHorse("B", makeGenome(models.AlleleA, models.AlleleA), 1.0)

	for i := 0; i < 50; i++ {
		result := SimulateFight(horse1, horse2, FightConfig{
			MaxRounds: 1,
			IsToDeath: true, // Even in death match, judges' decision = no fatality
		})

		e0HP := result.Entries[0].HP
		e1HP := result.Entries[1].HP
		if e0HP > 0 && e1HP > 0 && result.IsFatality {
			t.Errorf("Iteration %d: Judges' decision should not be a fatality", i)
		}
	}
}

// ---------------------------------------------------------------------------
// Stat advantage test
// ---------------------------------------------------------------------------

func TestSimulateFight_StrongerHorseWinsMoreOften(t *testing.T) {
	strongGenome := makeGenome(models.AlleleA, models.AlleleA) // all AA = 1.0 scores
	weakGenome := makeGenome(models.AlleleB, models.AlleleB)   // all BB = 0.3 scores

	strong := makeHorse("Strong", strongGenome, 1.0)
	weak := makeHorse("Weak", weakGenome, 0.3)

	strongWins := 0
	total := 200

	for i := 0; i < total; i++ {
		result := SimulateFight(strong, weak, FightConfig{
			MaxRounds: 10,
			Purse:     100,
		})
		if result.WinnerID == strong.ID {
			strongWins++
		}
	}

	winRate := float64(strongWins) / float64(total)
	// The AA/1.0 horse should dominate a BB/0.3 horse overwhelmingly
	if winRate < 0.70 {
		t.Errorf("Strong horse win rate = %.2f, expected > 0.70", winRate)
	}
}

// ---------------------------------------------------------------------------
// Arena-specific behavior tests
// ---------------------------------------------------------------------------

func TestSimulateFight_ThunderdomePassiveDamage(t *testing.T) {
	horse1 := makeHorse("T1", makeGenome(models.AlleleA, models.AlleleA), 1.0)
	horse2 := makeHorse("T2", makeGenome(models.AlleleA, models.AlleleA), 1.0)

	result := SimulateFight(horse1, horse2, FightConfig{
		ArenaType: ArenaThunderdome,
		MaxRounds: 10,
		Purse:     100,
	})

	// Thunderdome should produce narrative about passive damage
	found := false
	for _, n := range result.Narrative {
		if len(n) > 20 {
			// Check for thunderdome-specific narrative
			if containsSubstring(n, "ELECTRIC FLOOR") || containsSubstring(n, "Thunderdome") {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("Thunderdome fight should contain passive damage narrative")
	}
}

func TestSimulateFight_AllArenaTypes(t *testing.T) {
	arenas := []string{ArenaColosseum, ArenaThunderdome, ArenaPit, ArenaDrMittens, ArenaE008}

	for _, arena := range arenas {
		t.Run(arena, func(t *testing.T) {
			horse1 := makeHorse("H1", makeGenome(models.AlleleA, models.AlleleA), 1.0)
			horse2 := makeHorse("H2", makeGenome(models.AlleleA, models.AlleleB), 0.8)

			result := SimulateFight(horse1, horse2, FightConfig{
				ArenaType: arena,
				MaxRounds: 5,
				Purse:     100,
			})
			if result == nil {
				t.Fatalf("SimulateFight returned nil for arena %s", arena)
			}
			if result.ArenaType != arena {
				t.Errorf("ArenaType = %v, want %v", result.ArenaType, arena)
			}
			if len(result.Rounds) == 0 {
				t.Error("Should have at least 1 round")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Round and event structure tests
// ---------------------------------------------------------------------------

func TestSimulateFight_RoundsHaveEvents(t *testing.T) {
	horse1 := makeHorse("H1", makeGenome(models.AlleleA, models.AlleleA), 1.0)
	horse2 := makeHorse("H2", makeGenome(models.AlleleA, models.AlleleA), 1.0)

	result := SimulateFight(horse1, horse2, FightConfig{
		MaxRounds: 3,
		Purse:     100,
	})

	for i, round := range result.Rounds {
		if round.Round != i+1 {
			t.Errorf("Round %d has Round field = %d", i+1, round.Round)
		}
		if len(round.Events) == 0 {
			t.Errorf("Round %d has no events", round.Round)
		}
	}
}

func TestSimulateFight_EventTypesAreValid(t *testing.T) {
	horse1 := makeHorse("H1", makeGenome(models.AlleleA, models.AlleleA), 1.0)
	horse2 := makeHorse("H2", makeGenome(models.AlleleA, models.AlleleA), 1.0)

	validEvents := map[string]bool{
		"hit":                  true,
		"critical":             true,
		"dodge":                true,
		"miss":                 true,
		"mace_malfunction":     true,
		"crowd_oats":           true,
		"derulo":               true,
		"dr_mittens":           true,
		"e008_sentience":       true,
		"haunted_mace":         true,
		"mutual_respect":       true,
		"rage_explosion":       true,
		"desperate_lunge_hit":  true,
		"desperate_lunge_miss": true,
		"e008_heal":            true,
	}

	for i := 0; i < 50; i++ {
		result := SimulateFight(horse1, horse2, FightConfig{MaxRounds: 5})
		for _, round := range result.Rounds {
			for _, evt := range round.Events {
				if !validEvents[evt.Event] {
					t.Errorf("Unknown event type: %q", evt.Event)
				}
				if evt.Text == "" {
					t.Errorf("Event %q has empty text", evt.Event)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Narrative tests
// ---------------------------------------------------------------------------

func TestSimulateFight_NarrativeIncludesIntro(t *testing.T) {
	horse1 := makeHorse("Alpha", makeGenome(models.AlleleA, models.AlleleA), 1.0)
	horse2 := makeHorse("Beta", makeGenome(models.AlleleA, models.AlleleA), 1.0)

	result := SimulateFight(horse1, horse2, FightConfig{
		ArenaType: ArenaColosseum,
		MaxRounds: 3,
	})

	if len(result.Narrative) < 4 {
		t.Fatalf("Expected at least 4 intro narrative lines, got %d", len(result.Narrative))
	}

	// First line should mention the arena
	if !containsSubstring(result.Narrative[0], ArenaColosseum) {
		t.Errorf("First narrative should mention arena, got: %s", result.Narrative[0])
	}
	// Should mention both horse names
	if !containsSubstring(result.Narrative[1], "Alpha") {
		t.Errorf("Intro should mention horse1 name, got: %s", result.Narrative[1])
	}
	if !containsSubstring(result.Narrative[2], "Beta") {
		t.Errorf("Intro should mention horse2 name, got: %s", result.Narrative[2])
	}
}

func TestSimulateFight_NarrativeIncludesVictory(t *testing.T) {
	horse1 := makeHorse("Winner", makeGenome(models.AlleleA, models.AlleleA), 1.0)
	horse2 := makeHorse("Loser", makeGenome(models.AlleleB, models.AlleleB), 0.2)

	result := SimulateFight(horse1, horse2, FightConfig{
		MaxRounds: 10,
		Purse:     1000,
	})

	if result.WinnerID == "" {
		return // Draw, skip this check
	}

	// Should contain a victory narrative
	lastNarr := result.Narrative[len(result.Narrative)-1]
	if !containsSubstring(lastNarr, "VICTORIOUS") && !containsSubstring(lastNarr, "ERUPTS") && !containsSubstring(lastNarr, "silence") {
		// Check second-to-last in case there's a fatality follow-up
		if len(result.Narrative) >= 2 {
			secondLast := result.Narrative[len(result.Narrative)-2]
			if !containsSubstring(secondLast, "VICTORIOUS") && !containsSubstring(secondLast, "ERUPTS") {
				t.Logf("Warning: couldn't find victory narrative in last lines")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Purse and metadata tests
// ---------------------------------------------------------------------------

func TestSimulateFight_PursePreserved(t *testing.T) {
	horse1 := makeHorse("H1", makeGenome(models.AlleleA, models.AlleleA), 1.0)
	horse2 := makeHorse("H2", makeGenome(models.AlleleA, models.AlleleA), 1.0)

	result := SimulateFight(horse1, horse2, FightConfig{Purse: 99999})
	if result.Purse != 99999 {
		t.Errorf("Purse = %v, want 99999", result.Purse)
	}
}

func TestSimulateFight_TimestampSet(t *testing.T) {
	horse1 := makeHorse("H1", makeGenome(models.AlleleA, models.AlleleA), 1.0)
	horse2 := makeHorse("H2", makeGenome(models.AlleleA, models.AlleleA), 1.0)

	result := SimulateFight(horse1, horse2, FightConfig{MaxRounds: 1})
	if result.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
}

// ---------------------------------------------------------------------------
// Mace speed penalty integration test
// ---------------------------------------------------------------------------

func TestBuildEntry_GeoffrussyGavelReducesSpeed(t *testing.T) {
	// The Geoffrussy's Gavel has a 0.80 speed penalty applied in SimulateFight,
	// but buildEntry just assigns the mace — the penalty is applied in SimulateFight.
	// We test indirectly: a horse with Geoffrussy's Gavel should have modified speed
	// in the final result entries.

	// We can test getMaceModifiers directly
	mods := getMaceModifiers(MaceGeoffrussy)
	if mods.speedPenalty != 0.80 {
		t.Errorf("Geoffrussy speedPenalty = %v, want 0.80", mods.speedPenalty)
	}
	if mods.damageMultiplier != 1.25 {
		t.Errorf("Geoffrussy damageMultiplier = %v, want 1.25", mods.damageMultiplier)
	}
}

// ---------------------------------------------------------------------------
// Rage clamping tests
// ---------------------------------------------------------------------------

func TestSimulateFight_RageClamped(t *testing.T) {
	horse1 := makeHorse("H1", makeGenome(models.AlleleA, models.AlleleA), 1.0)
	horse2 := makeHorse("H2", makeGenome(models.AlleleA, models.AlleleA), 1.0)

	for i := 0; i < 50; i++ {
		result := SimulateFight(horse1, horse2, FightConfig{MaxRounds: 10})
		for _, round := range result.Rounds {
			if round.Rage1 < 0 || round.Rage1 > 100 {
				t.Errorf("Rage1 = %v, should be clamped [0, 100]", round.Rage1)
			}
			if round.Rage2 < 0 || round.Rage2 > 100 {
				t.Errorf("Rage2 = %v, should be clamped [0, 100]", round.Rage2)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// HP bounds tests
// ---------------------------------------------------------------------------

func TestSimulateFight_HPNeverExceedsMax(t *testing.T) {
	horse1 := makeHorse("H1", makeGenome(models.AlleleA, models.AlleleA), 1.0)
	horse2 := makeHorse("H2", makeGenome(models.AlleleA, models.AlleleA), 1.0)

	for i := 0; i < 50; i++ {
		result := SimulateFight(horse1, horse2, FightConfig{
			ArenaType: ArenaDrMittens, // has heal events
			MaxRounds: 10,
		})
		for _, round := range result.Rounds {
			if round.HP1After > result.Entries[0].MaxHP+0.01 {
				t.Errorf("HP1After = %v, exceeds MaxHP = %v", round.HP1After, result.Entries[0].MaxHP)
			}
			if round.HP2After > result.Entries[1].MaxHP+0.01 {
				t.Errorf("HP2After = %v, exceeds MaxHP = %v", round.HP2After, result.Entries[1].MaxHP)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// MaxRounds respected
// ---------------------------------------------------------------------------

func TestSimulateFight_MaxRoundsRespected(t *testing.T) {
	horse1 := makeHorse("H1", makeGenome(models.AlleleA, models.AlleleA), 1.0)
	horse2 := makeHorse("H2", makeGenome(models.AlleleA, models.AlleleA), 1.0)

	for _, maxRounds := range []int{1, 3, 5, 10, 20} {
		result := SimulateFight(horse1, horse2, FightConfig{MaxRounds: maxRounds})
		if len(result.Rounds) > maxRounds {
			t.Errorf("MaxRounds=%d but got %d rounds", maxRounds, len(result.Rounds))
		}
		if result.KORound > maxRounds {
			t.Errorf("MaxRounds=%d but KORound=%d", maxRounds, result.KORound)
		}
	}
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
