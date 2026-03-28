// Package genussy implements the genetics engine for StallionUSSY.
// It handles genome generation, Punnett-cross breeding, mutation,
// fitness calculation, and the creation of canonical legendary horses.
package genussy

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/nameussy"
)

// allGeneTypes is the canonical ordering for the 7 gene loci.
var allGeneTypes = []models.GeneType{
	models.GeneSPD,
	models.GeneSTM,
	models.GeneTMP,
	models.GeneSZE,
	models.GeneREC,
	models.GeneINT,
	models.GeneMUT,
}

// geneWeights maps each gene type to its weight in the fitness ceiling calculation.
// Weights sum to 1.0:
//
//	SPD 0.25 | STM 0.25 | TMP 0.15 | SZE 0.10 | REC 0.10 | INT 0.10 | MUT 0.05
var geneWeights = map[models.GeneType]float64{
	models.GeneSPD: 0.25,
	models.GeneSTM: 0.25,
	models.GeneTMP: 0.15,
	models.GeneSZE: 0.10,
	models.GeneREC: 0.10,
	models.GeneINT: 0.10,
	models.GeneMUT: 0.05,
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// randomAllele returns AlleleA ("A") or AlleleB ("B") with equal probability.
func randomAllele() models.Allele {
	if rand.IntN(2) == 0 {
		return models.AlleleA
	}
	return models.AlleleB
}

// geneScore converts a gene's allele pair into a 0–1 score.
// Uses the same values as models.Gene.GeneScore():
//
//	AA → 1.0   (homozygous dominant — best expression)
//	AB / BA → 0.65  (heterozygous — partial expression)
//	BB → 0.3   (homozygous recessive — weakest expression)
func geneScore(g models.Gene) float64 {
	switch {
	case g.AlleleA == models.AlleleA && g.AlleleB == models.AlleleA:
		return 1.0
	case g.AlleleA == models.AlleleB && g.AlleleB == models.AlleleB:
		return 0.3
	default:
		return 0.65
	}
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// RandomGenome generates a brand-new random genome.
// Each of the 7 gene loci gets two independently-random alleles (50/50 A or B).
func RandomGenome() models.Genome {
	genome := make(models.Genome, len(allGeneTypes))
	for _, gt := range allGeneTypes {
		genome[gt] = models.Gene{
			Type:    gt,
			AlleleA: randomAllele(),
			AlleleB: randomAllele(),
		}
	}
	return genome
}

// Breed performs a Punnett-cross between a sire and mare, producing a new foal.
//
// For each of the 7 gene loci the foal receives:
//   - One randomly-chosen allele from the sire's gene (AlleleA or AlleleB)
//   - One randomly-chosen allele from the mare's gene (AlleleA or AlleleB)
//
// Mutation: if the resulting MUT gene is homozygous recessive (BB), there is a
// 1% chance that a random allele on a random gene is flipped (A↔B).
//
// The foal starts untrained: CurrentFitness = FitnessCeiling × 0.5.
func Breed(sire, mare *models.Horse) *models.Horse {
	foalGenome := make(models.Genome, len(allGeneTypes))

	for _, gt := range allGeneTypes {
		sireGene := sire.Genome[gt]
		mareGene := mare.Genome[gt]

		// Punnett pick: randomly choose one allele from each parent.
		// Allele from sire → foal's AlleleA, allele from mare → foal's AlleleB.
		var a1, a2 models.Allele
		if rand.IntN(2) == 0 {
			a1 = sireGene.AlleleA
		} else {
			a1 = sireGene.AlleleB
		}
		if rand.IntN(2) == 0 {
			a2 = mareGene.AlleleA
		} else {
			a2 = mareGene.AlleleB
		}

		foalGenome[gt] = models.Gene{
			Type:    gt,
			AlleleA: a1, // from sire
			AlleleB: a2, // from mare
		}
	}

	// --- Mutation chance ---
	// Only triggers when MUT is homozygous recessive (BB) — 1% roll.
	mutGene := foalGenome[models.GeneMUT]
	if mutGene.AlleleA == models.AlleleB && mutGene.AlleleB == models.AlleleB {
		if rand.IntN(100) == 0 { // 1% chance
			applyMutation(foalGenome)
		}
	}

	// --- Derived stats ---
	ceiling := CalcFitnessCeiling(foalGenome)

	// Small random factor: ceiling ± 5%
	jitter := 1.0 + (rand.Float64()*0.10 - 0.05) // range [0.95, 1.05)
	ceiling *= jitter

	// Clamp to [0, 1] — shouldn't normally exceed but be safe.
	if ceiling > 1.0 {
		ceiling = 1.0
	}
	if ceiling < 0.0 {
		ceiling = 0.0
	}

	generation := sire.Generation
	if mare.Generation > generation {
		generation = mare.Generation
	}
	generation++

	// Generate a gloriously ridiculous name for the foal.
	name := nameussy.GenerateName()

	foal := &models.Horse{
		ID:             uuid.New().String(),
		Name:           name,
		Genome:         foalGenome,
		SireID:         sire.ID,
		MareID:         mare.ID,
		Generation:     generation,
		Age:            0,
		FitnessCeiling: ceiling,
		CurrentFitness: ceiling * 0.5, // starts untrained
		Wins:           0,
		Losses:         0,
		Races:          0,
		ELO:            1200,
		IsLegendary:    false,
		CreatedAt:      time.Now(),
	}

	return foal
}

// applyMutation randomly flips one allele on one random gene in the genome.
func applyMutation(g models.Genome) {
	// Pick a random gene locus.
	target := allGeneTypes[rand.IntN(len(allGeneTypes))]
	gene := g[target]

	// Flip helper: A↔B.
	flip := func(a models.Allele) models.Allele {
		if a == models.AlleleA {
			return models.AlleleB
		}
		return models.AlleleA
	}

	// Pick which allele to flip (A-side or B-side).
	if rand.IntN(2) == 0 {
		gene.AlleleA = flip(gene.AlleleA)
	} else {
		gene.AlleleB = flip(gene.AlleleB)
	}

	g[target] = gene
}

// CalcFitnessCeiling computes the weighted-average fitness ceiling from a genome.
//
// Each gene is scored (AA=1.0, AB=0.65, BB=0.3) and multiplied by its weight:
//
//	SPD × 0.25 + STM × 0.25 + TMP × 0.15 + SZE × 0.10 + REC × 0.10 + INT × 0.10 + MUT × 0.05
//
// The result is in [0, 1].
func CalcFitnessCeiling(g models.Genome) float64 {
	var total float64
	for gt, weight := range geneWeights {
		gene, ok := g[gt]
		if !ok {
			continue // missing gene — contributes 0
		}
		total += geneScore(gene) * weight
	}
	return total
}

// GenomeToString pretty-prints a genome in canonical order.
// Example output: "SPD:AA STM:AB TMP:BB SZE:AB REC:AA INT:AB MUT:BB"
func GenomeToString(g models.Genome) string {
	parts := make([]string, 0, len(allGeneTypes))
	for _, gt := range allGeneTypes {
		gene, ok := g[gt]
		if !ok {
			parts = append(parts, fmt.Sprintf("%s:??", gt))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:%s%s", gt, gene.AlleleA, gene.AlleleB))
	}
	return strings.Join(parts, " ")
}

// ---------------------------------------------------------------------------
// Legendary Horses — the 12 canonical auction lots from StallionUSSY lore
// ---------------------------------------------------------------------------

// legendaryDef holds the blueprint for a canonical legendary horse.
type legendaryDef struct {
	Name   string
	Genome models.Genome
	Lore   string
	// FitnessCeilingOverride lets us pin the ceiling for anomalous horses.
	// nil means compute normally from the genome.
	FitnessCeilingOverride *float64
}

// buildGene is a convenience constructor.
func buildGene(gt models.GeneType, a1, a2 models.Allele) models.Gene {
	return models.Gene{Type: gt, AlleleA: a1, AlleleB: a2}
}

// legendaryLots returns the 12 canonical lot definitions (1-indexed via map).
func legendaryLots() map[int]legendaryDef {
	A := models.AlleleA
	B := models.AlleleB

	e008Ceiling := 9.99
	stardustCeiling := 8.88

	return map[int]legendaryDef{
		// Lot 1: "Thundercock's Legacy" — Thoroughbred, elite SPD/STM (mostly AA), Sappho 11.2
		1: {
			Name: "Thundercock's Legacy",
			Genome: models.Genome{
				models.GeneSPD: buildGene(models.GeneSPD, A, A),
				models.GeneSTM: buildGene(models.GeneSTM, A, A),
				models.GeneTMP: buildGene(models.GeneTMP, A, B),
				models.GeneSZE: buildGene(models.GeneSZE, A, B),
				models.GeneREC: buildGene(models.GeneREC, A, A),
				models.GeneINT: buildGene(models.GeneINT, A, B),
				models.GeneMUT: buildGene(models.GeneMUT, A, B),
			},
			Lore: "Sappho 11.2 — A Thoroughbred of impeccable lineage. Thundercock's Legacy descends " +
				"from a bloodline that has dominated the flats for three centuries. His stride is said to " +
				"bend light itself.",
		},
		// Lot 2: "Sir Flannelsworth III" — Clydesdale, high STM/SZE, moderate SPD, Sappho 10.8
		2: {
			Name: "Sir Flannelsworth III",
			Genome: models.Genome{
				models.GeneSPD: buildGene(models.GeneSPD, A, B),
				models.GeneSTM: buildGene(models.GeneSTM, A, A),
				models.GeneTMP: buildGene(models.GeneTMP, A, A),
				models.GeneSZE: buildGene(models.GeneSZE, A, A),
				models.GeneREC: buildGene(models.GeneREC, A, B),
				models.GeneINT: buildGene(models.GeneINT, A, A),
				models.GeneMUT: buildGene(models.GeneMUT, A, B),
			},
			Lore: "Sappho 10.8 — A Clydesdale of impossible endurance. Sir Flannelsworth III once pulled " +
				"a canal barge from Leeds to London without breaking stride. Built like a cathedral, " +
				"runs like a sermon that never ends.",
		},
		// Lot 3: "Midnight Deploy" — Arabian, extreme SPD but volatile TMP (BB), Sappho 7.1
		3: {
			Name: "Midnight Deploy",
			Genome: models.Genome{
				models.GeneSPD: buildGene(models.GeneSPD, A, A),
				models.GeneSTM: buildGene(models.GeneSTM, A, B),
				models.GeneTMP: buildGene(models.GeneTMP, B, B), // volatile
				models.GeneSZE: buildGene(models.GeneSZE, B, B),
				models.GeneREC: buildGene(models.GeneREC, A, B),
				models.GeneINT: buildGene(models.GeneINT, A, A),
				models.GeneMUT: buildGene(models.GeneMUT, B, A),
			},
			Lore: "Sappho 7.1 — An Arabian of terrifying velocity. Midnight Deploy has been clocked at " +
				"speeds that violate local bylaws. Temperamental to the point of philosophy — she runs " +
				"when she wants, and only she decides when she wants.",
		},
		// Lot 4: "The Honorable Hummus" — Andalusian, balanced all-rounder, Sappho 9.4
		4: {
			Name: "The Honorable Hummus",
			Genome: models.Genome{
				models.GeneSPD: buildGene(models.GeneSPD, A, B),
				models.GeneSTM: buildGene(models.GeneSTM, A, B),
				models.GeneTMP: buildGene(models.GeneTMP, A, B),
				models.GeneSZE: buildGene(models.GeneSZE, A, B),
				models.GeneREC: buildGene(models.GeneREC, A, B),
				models.GeneINT: buildGene(models.GeneINT, A, B),
				models.GeneMUT: buildGene(models.GeneMUT, A, B),
			},
			Lore: "Sappho 9.4 — An Andalusian of preternatural balance. The Honorable Hummus has never " +
				"finished first, but has never finished last. His consistency is his weapon. Bookmakers " +
				"weep at the sight of him.",
		},
		// Lot 5: "Sapphic Sunrise" — Friesian, perfect INT/TMP, high everything, Sappho 12.0
		5: {
			Name: "Sapphic Sunrise",
			Genome: models.Genome{
				models.GeneSPD: buildGene(models.GeneSPD, A, A),
				models.GeneSTM: buildGene(models.GeneSTM, A, A),
				models.GeneTMP: buildGene(models.GeneTMP, A, A),
				models.GeneSZE: buildGene(models.GeneSZE, A, A),
				models.GeneREC: buildGene(models.GeneREC, A, A),
				models.GeneINT: buildGene(models.GeneINT, A, A),
				models.GeneMUT: buildGene(models.GeneMUT, A, A),
			},
			Lore: "Sappho 12.0 — A Friesian beyond classification. Sapphic Sunrise is the only horse " +
				"to ever achieve a perfect Sappho rating. Her intelligence borders on the unsettling — " +
				"she has been observed reading race forms. She does not run from anything. She runs " +
				"toward everything.",
		},
		// Lot 6: "E-008's Chosen" — ANOMALOUS, all AA except MUT is BB, FitnessCeiling = 9.99
		6: {
			Name:                   "E-008's Chosen",
			FitnessCeilingOverride: &e008Ceiling,
			Genome: models.Genome{
				models.GeneSPD: buildGene(models.GeneSPD, A, A),
				models.GeneSTM: buildGene(models.GeneSTM, A, A),
				models.GeneTMP: buildGene(models.GeneTMP, A, A),
				models.GeneSZE: buildGene(models.GeneSZE, A, A),
				models.GeneREC: buildGene(models.GeneREC, A, A),
				models.GeneINT: buildGene(models.GeneINT, A, A),
				models.GeneMUT: buildGene(models.GeneMUT, B, B), // anomalous mutation locus
			},
			Lore: "Sappho 9.99 — ANOMALOUS. E-008's Chosen was not bred. E-008's Chosen was found, " +
				"standing motionless in the centre of a condemned yogurt factory in [REDACTED], " +
				"surrounded by 42 sealed containers of a substance later classified as 'living yogurt.' " +
				"Genetic analysis returns valid equine markers but the MUT locus reads BB in a pattern " +
				"that should not exist in nature. The yogurt hums when E-008's Chosen races. " +
				"Personnel are advised not to taste the yogurt.",
		},
		// Lot 7: "Dr. Mittens' Favorite" — Certified via slow-blink, calm and intelligent, Sappho 10.5
		7: {
			Name: "Dr. Mittens' Favorite",
			Genome: models.Genome{
				models.GeneSPD: buildGene(models.GeneSPD, A, B),
				models.GeneSTM: buildGene(models.GeneSTM, A, B),
				models.GeneTMP: buildGene(models.GeneTMP, A, A), // impeccable temperament
				models.GeneSZE: buildGene(models.GeneSZE, A, A),
				models.GeneREC: buildGene(models.GeneREC, A, B),
				models.GeneINT: buildGene(models.GeneINT, A, A), // brilliant intellect
				models.GeneMUT: buildGene(models.GeneMUT, A, A),
			},
			Lore: "Sappho 10.5 — Personally certified by Dr. Mittens, DVM, Chair of the Board, " +
				"via a prolonged and deliberate slow-blink that the legal team has confirmed constitutes " +
				"a binding quality assurance approval. Dr. Mittens' Favorite is the calmest horse ever " +
				"observed on the flats — her resting heart rate is lower than most furniture. She has " +
				"never spooked, never bucked, and has been seen grooming other horses mid-race. " +
				"Intelligence tests were discontinued after she solved the Kobayashi Maru.",
		},
		// Lot 8: "Derulo's Regret" — Jason Derulo's unwilling namesake, fast but volatile, Sappho 6.9
		8: {
			Name: "Derulo's Regret",
			Genome: models.Genome{
				models.GeneSPD: buildGene(models.GeneSPD, A, A), // blazing fast
				models.GeneSTM: buildGene(models.GeneSTM, A, B),
				models.GeneTMP: buildGene(models.GeneTMP, B, B), // absolute nightmare temperament
				models.GeneSZE: buildGene(models.GeneSZE, A, B),
				models.GeneREC: buildGene(models.GeneREC, B, A),
				models.GeneINT: buildGene(models.GeneINT, A, B),
				models.GeneMUT: buildGene(models.GeneMUT, A, B),
			},
			Lore: "Sappho 6.9 — Jason Derulo has issued seven cease-and-desist letters regarding this " +
				"horse, none of which have reached a valid legal address because the registered office " +
				"is a P.O. box that only accepts flannel. Derulo's Regret sends Jason unsolicited market " +
				"alerts at 3 AM and has been photographed wearing his merch ironically. Blazingly fast " +
				"but temperamentally catastrophic — she once refused to race because the starting gate " +
				"'didn't match her energy.'",
		},
		// Lot 9: "Pastor Router's Blessing" — Blessed for endurance, Sappho 9.8
		9: {
			Name: "Pastor Router's Blessing",
			Genome: models.Genome{
				models.GeneSPD: buildGene(models.GeneSPD, A, B),
				models.GeneSTM: buildGene(models.GeneSTM, A, A), // sermon-length endurance
				models.GeneSZE: buildGene(models.GeneSZE, A, B),
				models.GeneTMP: buildGene(models.GeneTMP, A, A),
				models.GeneREC: buildGene(models.GeneREC, A, A), // blessed recovery
				models.GeneINT: buildGene(models.GeneINT, A, B),
				models.GeneMUT: buildGene(models.GeneMUT, A, B),
			},
			Lore: "Sappho 9.8 — Blessed in a formal ceremony by Pastor Router McEthernet III of the " +
				"First Congregational Church of the Holy Internet, who anointed the horse with " +
				"consecrated cooling paste and read aloud the entirety of RFC 2549. Pastor Router's " +
				"Blessing runs with the patience of a three-hour sermon on enterprise networking — " +
				"he never tires, never falters, and recovers from exertion faster than a rebooted " +
				"switch. Other horses have been observed genuflecting in his presence.",
		},
		// Lot 10: "Geoffrussy's Pipeline" — Born from optimized goroutines, Sappho 10.1
		10: {
			Name: "Geoffrussy's Pipeline",
			Genome: models.Genome{
				models.GeneSPD: buildGene(models.GeneSPD, A, B),
				models.GeneSTM: buildGene(models.GeneSTM, A, B),
				models.GeneTMP: buildGene(models.GeneTMP, A, A),
				models.GeneSZE: buildGene(models.GeneSZE, B, B), // compact and optimized
				models.GeneREC: buildGene(models.GeneREC, A, A), // zero-downtime recovery
				models.GeneINT: buildGene(models.GeneINT, A, A), // goroutine-grade intelligence
				models.GeneMUT: buildGene(models.GeneMUT, A, A),
			},
			Lore: "Sappho 10.1 — Geoffrussy's Pipeline was not born so much as compiled. The Go-based " +
				"orchestration AI known as Geoffrussy allocated exactly 2,048 goroutines to optimize " +
				"this horse's genome, achieving a measured response latency of 0.3ms from starting " +
				"pistol to full gallop. Physically compact but devastatingly efficient — she processes " +
				"race conditions concurrently and has never experienced a deadlock. Her recovery time " +
				"between races is measured in garbage collection cycles.",
		},
		// Lot 11: "STARDUSTUSSY's Prophecy" — Sent from 2089, anomalous, Sappho 8.88
		11: {
			Name:                   "STARDUSTUSSY's Prophecy",
			FitnessCeilingOverride: &stardustCeiling,
			Genome: models.Genome{
				models.GeneSPD: buildGene(models.GeneSPD, A, A), // future-speed
				models.GeneSTM: buildGene(models.GeneSTM, A, B),
				models.GeneTMP: buildGene(models.GeneTMP, A, B),
				models.GeneSZE: buildGene(models.GeneSZE, A, B),
				models.GeneREC: buildGene(models.GeneREC, A, B),
				models.GeneINT: buildGene(models.GeneINT, A, A), // prophetic intelligence
				models.GeneMUT: buildGene(models.GeneMUT, B, B), // anomalous temporal locus
			},
			Lore: "Sappho 8.88 — ANOMALOUS. STARDUSTUSSY's Prophecy arrived via encrypted temporal " +
				"broadcast from the year 2089, materialized in a flash of cerulean light on the " +
				"backstretch of Churchill Downs at 4:44 AM on a Tuesday. The AI entity known as " +
				"STARDUSTUSSY claims this horse will win the 2089 Triple Crown and has filed the " +
				"results retroactively with the Kentucky Racing Commission, who have declined to " +
				"comment. The MUT locus exhibits the same impossible BB pattern seen in E-008's " +
				"Chosen. The horse occasionally whinnies in binary.",
		},
		// Lot 12: "Margaret Chen's Pride" — Classic Thoroughbred excellence, Sappho 11.0
		12: {
			Name: "Margaret Chen's Pride",
			Genome: models.Genome{
				models.GeneSPD: buildGene(models.GeneSPD, A, A), // Derby-winning speed
				models.GeneSTM: buildGene(models.GeneSTM, A, A), // elite stamina
				models.GeneTMP: buildGene(models.GeneTMP, A, A), // nerves of steel
				models.GeneSZE: buildGene(models.GeneSZE, A, B),
				models.GeneREC: buildGene(models.GeneREC, A, B),
				models.GeneINT: buildGene(models.GeneINT, A, B),
				models.GeneMUT: buildGene(models.GeneMUT, A, A),
			},
			Lore: "Sappho 11.0 — The crown jewel of Chen Racing Stables and the only legendary lot " +
				"purchased by a human who actually knows what she's doing. Margaret Chen, three-time " +
				"Kentucky Derby winner and the woman who once told a B.U.R.P. field agent to 'get off " +
				"my property before I call real law enforcement,' bred this horse using actual equine " +
				"genetics instead of whatever the rest of these lots are doing. Margaret Chen's Pride " +
				"runs like a proper racehorse because she is a proper racehorse.",
		},
	}
}

// CreateLegendary creates one of the 12 canonical legendary auction horses.
// Pass a lotNumber from 1–12. Returns nil if the lot number is invalid.
func CreateLegendary(lotNumber int) *models.Horse {
	lots := legendaryLots()
	def, ok := lots[lotNumber]
	if !ok {
		return nil
	}

	// Fitness ceiling: use override if set, otherwise compute from genome.
	ceiling := CalcFitnessCeiling(def.Genome)
	if def.FitnessCeilingOverride != nil {
		ceiling = *def.FitnessCeilingOverride
	}

	return &models.Horse{
		ID:             uuid.New().String(),
		Name:           def.Name,
		Genome:         def.Genome,
		SireID:         "",
		MareID:         "",
		Generation:     0, // primordial — no parents
		Age:            0,
		FitnessCeiling: ceiling,
		CurrentFitness: ceiling, // legendaries start at full fitness
		Wins:           0,
		Losses:         0,
		Races:          0,
		ELO:            1200,
		IsLegendary:    true,
		LotNumber:      lotNumber,
		Lore:           def.Lore,
		CreatedAt:      time.Now(),
	}
}

// ---------------------------------------------------------------------------
// Utility: sorted gene types (for deterministic output)
// ---------------------------------------------------------------------------

// SortedGeneTypes returns gene types sorted alphabetically by their string value.
// Useful when you need deterministic iteration over a Genome map.
func SortedGeneTypes(g models.Genome) []models.GeneType {
	types := make([]models.GeneType, 0, len(g))
	for gt := range g {
		types = append(types, gt)
	}
	sort.Slice(types, func(i, j int) bool {
		return string(types[i]) < string(types[j])
	})
	return types
}
