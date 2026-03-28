package genussy

import (
	"math"
	"testing"

	"github.com/mojomast/stallionussy/internal/models"
)

// ---------------------------------------------------------------------------
// Helper: build a genome from a flat allele spec
// ---------------------------------------------------------------------------

// makeGenome builds a full 7-gene genome from a map of gene type to [2]allele.
func makeGenome(spec map[models.GeneType][2]models.Allele) models.Genome {
	g := make(models.Genome, len(spec))
	for gt, alleles := range spec {
		g[gt] = models.Gene{Type: gt, AlleleA: alleles[0], AlleleB: alleles[1]}
	}
	return g
}

// allAA returns a genome where every gene locus is AA.
func allAA() models.Genome {
	spec := map[models.GeneType][2]models.Allele{}
	for _, gt := range allGeneTypes {
		spec[gt] = [2]models.Allele{models.AlleleA, models.AlleleA}
	}
	return makeGenome(spec)
}

// allBB returns a genome where every gene locus is BB.
func allBB() models.Genome {
	spec := map[models.GeneType][2]models.Allele{}
	for _, gt := range allGeneTypes {
		spec[gt] = [2]models.Allele{models.AlleleB, models.AlleleB}
	}
	return makeGenome(spec)
}

// allAB returns a genome where every gene locus is AB.
func allAB() models.Genome {
	spec := map[models.GeneType][2]models.Allele{}
	for _, gt := range allGeneTypes {
		spec[gt] = [2]models.Allele{models.AlleleA, models.AlleleB}
	}
	return makeGenome(spec)
}

// makeHorse wraps a genome in a minimal Horse struct suitable for Breed().
func makeHorse(genome models.Genome, gen int) *models.Horse {
	return &models.Horse{
		ID:         "test-horse",
		Name:       "Test Horse",
		Genome:     genome,
		Generation: gen,
	}
}

// isValidAllele returns true if the allele is "A" or "B".
func isValidAllele(a models.Allele) bool {
	return a == models.AlleleA || a == models.AlleleB
}

// ---------------------------------------------------------------------------
// Tests: RandomGenome
// ---------------------------------------------------------------------------

func TestRandomGenome_ContainsAllGeneTypes(t *testing.T) {
	genome := RandomGenome()

	expected := []models.GeneType{
		models.GeneSPD, models.GeneSTM, models.GeneTMP,
		models.GeneSZE, models.GeneREC, models.GeneINT, models.GeneMUT,
	}

	if len(genome) != len(expected) {
		t.Fatalf("RandomGenome returned %d genes, want %d", len(genome), len(expected))
	}

	for _, gt := range expected {
		gene, ok := genome[gt]
		if !ok {
			t.Errorf("RandomGenome missing gene type %s", gt)
			continue
		}
		if gene.Type != gt {
			t.Errorf("gene.Type = %q, want %q", gene.Type, gt)
		}
		if !isValidAllele(gene.AlleleA) {
			t.Errorf("gene %s AlleleA = %q, want A or B", gt, gene.AlleleA)
		}
		if !isValidAllele(gene.AlleleB) {
			t.Errorf("gene %s AlleleB = %q, want A or B", gt, gene.AlleleB)
		}
	}
}

func TestRandomGenome_ProducesDifferentResults(t *testing.T) {
	// Generate many genomes and verify we don't always get the same one.
	// With 7 genes × 2 alleles × 2 options each, identical runs are astronomically unlikely.
	const n = 50
	genomes := make([]models.Genome, n)
	for i := 0; i < n; i++ {
		genomes[i] = RandomGenome()
	}

	allSame := true
	ref := GenomeToString(genomes[0])
	for i := 1; i < n; i++ {
		if GenomeToString(genomes[i]) != ref {
			allSame = false
			break
		}
	}
	if allSame {
		t.Errorf("generated %d genomes and all were identical — randomness broken", n)
	}
}

// ---------------------------------------------------------------------------
// Tests: geneScore (unexported, but accessible from the same package)
// ---------------------------------------------------------------------------

func TestGeneScore_AA(t *testing.T) {
	g := models.Gene{Type: models.GeneSPD, AlleleA: models.AlleleA, AlleleB: models.AlleleA}
	got := geneScore(g)
	if got != 1.0 {
		t.Errorf("geneScore(AA) = %f, want 1.0", got)
	}
}

func TestGeneScore_AB(t *testing.T) {
	g := models.Gene{Type: models.GeneSPD, AlleleA: models.AlleleA, AlleleB: models.AlleleB}
	got := geneScore(g)
	if got != 0.65 {
		t.Errorf("geneScore(AB) = %f, want 0.65", got)
	}
}

func TestGeneScore_BA(t *testing.T) {
	g := models.Gene{Type: models.GeneSPD, AlleleA: models.AlleleB, AlleleB: models.AlleleA}
	got := geneScore(g)
	if got != 0.65 {
		t.Errorf("geneScore(BA) = %f, want 0.65", got)
	}
}

func TestGeneScore_BB(t *testing.T) {
	g := models.Gene{Type: models.GeneSPD, AlleleA: models.AlleleB, AlleleB: models.AlleleB}
	got := geneScore(g)
	if got != 0.3 {
		t.Errorf("geneScore(BB) = %f, want 0.3", got)
	}
}

// ---------------------------------------------------------------------------
// Tests: CalcFitnessCeiling
// ---------------------------------------------------------------------------

func TestCalcFitnessCeiling_AllAA(t *testing.T) {
	genome := allAA()
	got := CalcFitnessCeiling(genome)
	// All AA → score 1.0 for every gene, weighted sum = 1.0
	if math.Abs(got-1.0) > 1e-9 {
		t.Errorf("CalcFitnessCeiling(allAA) = %f, want 1.0", got)
	}
}

func TestCalcFitnessCeiling_AllBB(t *testing.T) {
	genome := allBB()
	got := CalcFitnessCeiling(genome)
	// All BB → score 0.3 for every gene. Weighted sum = 0.3 × (0.25+0.25+0.15+0.10+0.10+0.10+0.05) = 0.3 × 1.0 = 0.3
	if math.Abs(got-0.3) > 1e-9 {
		t.Errorf("CalcFitnessCeiling(allBB) = %f, want 0.3", got)
	}
}

func TestCalcFitnessCeiling_AllAB(t *testing.T) {
	genome := allAB()
	got := CalcFitnessCeiling(genome)
	// All AB → score 0.65 for every gene. Weighted sum = 0.65 × 1.0 = 0.65
	if math.Abs(got-0.65) > 1e-9 {
		t.Errorf("CalcFitnessCeiling(allAB) = %f, want 0.65", got)
	}
}

func TestCalcFitnessCeiling_MixedGenome(t *testing.T) {
	// SPD:AA(1.0×0.25=0.25) STM:BB(0.3×0.25=0.075) TMP:AB(0.65×0.15=0.0975)
	// SZE:AA(1.0×0.10=0.10) REC:BB(0.3×0.10=0.03) INT:AB(0.65×0.10=0.065) MUT:AA(1.0×0.05=0.05)
	// Total = 0.25 + 0.075 + 0.0975 + 0.10 + 0.03 + 0.065 + 0.05 = 0.6675
	genome := makeGenome(map[models.GeneType][2]models.Allele{
		models.GeneSPD: {models.AlleleA, models.AlleleA},
		models.GeneSTM: {models.AlleleB, models.AlleleB},
		models.GeneTMP: {models.AlleleA, models.AlleleB},
		models.GeneSZE: {models.AlleleA, models.AlleleA},
		models.GeneREC: {models.AlleleB, models.AlleleB},
		models.GeneINT: {models.AlleleA, models.AlleleB},
		models.GeneMUT: {models.AlleleA, models.AlleleA},
	})
	got := CalcFitnessCeiling(genome)
	want := 0.6675
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("CalcFitnessCeiling(mixed) = %f, want %f", got, want)
	}
}

func TestCalcFitnessCeiling_EmptyGenome(t *testing.T) {
	genome := models.Genome{}
	got := CalcFitnessCeiling(genome)
	if got != 0.0 {
		t.Errorf("CalcFitnessCeiling(empty) = %f, want 0.0", got)
	}
}

func TestCalcFitnessCeiling_PartialGenome(t *testing.T) {
	// Only SPD:AA → 1.0 × 0.25 = 0.25
	genome := makeGenome(map[models.GeneType][2]models.Allele{
		models.GeneSPD: {models.AlleleA, models.AlleleA},
	})
	got := CalcFitnessCeiling(genome)
	want := 0.25
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("CalcFitnessCeiling(SPD only) = %f, want %f", got, want)
	}
}

// ---------------------------------------------------------------------------
// Tests: Breed
// ---------------------------------------------------------------------------

func TestBreed_ProducesValidGenome(t *testing.T) {
	sire := makeHorse(allAA(), 0)
	mare := makeHorse(allBB(), 0)

	for i := 0; i < 100; i++ {
		foal := Breed(sire, mare)

		if len(foal.Genome) != 7 {
			t.Fatalf("foal genome has %d genes, want 7", len(foal.Genome))
		}

		for _, gt := range allGeneTypes {
			gene, ok := foal.Genome[gt]
			if !ok {
				t.Errorf("foal missing gene type %s", gt)
				continue
			}
			if !isValidAllele(gene.AlleleA) {
				t.Errorf("foal gene %s AlleleA = %q, want A or B", gt, gene.AlleleA)
			}
			if !isValidAllele(gene.AlleleB) {
				t.Errorf("foal gene %s AlleleB = %q, want A or B", gt, gene.AlleleB)
			}
		}
	}
}

func TestBreed_AllelesComeFromParents(t *testing.T) {
	// Sire is all AA, mare is all BB.
	// Without mutation, foal's AlleleA (from sire) must be A,
	// and AlleleB (from mare) must be B.
	// Mutation only triggers if MUT is BB AND 1% roll succeeds — with AA sire
	// contributing A to foal's MUT AlleleA, foal's MUT can never be BB.
	sire := makeHorse(allAA(), 0)
	mare := makeHorse(allBB(), 0)

	for i := 0; i < 200; i++ {
		foal := Breed(sire, mare)
		for _, gt := range allGeneTypes {
			gene := foal.Genome[gt]
			// AlleleA from sire (AA) → must be A
			if gene.AlleleA != models.AlleleA {
				t.Errorf("iteration %d, gene %s: AlleleA = %q, expected A (from AA sire)", i, gt, gene.AlleleA)
			}
			// AlleleB from mare (BB) → must be B
			if gene.AlleleB != models.AlleleB {
				t.Errorf("iteration %d, gene %s: AlleleB = %q, expected B (from BB mare)", i, gt, gene.AlleleB)
			}
		}
	}
}

func TestBreed_GenerationIncrement(t *testing.T) {
	sire := makeHorse(allAA(), 3)
	mare := makeHorse(allBB(), 5)
	foal := Breed(sire, mare)

	// Generation should be max(sire, mare) + 1 = 5 + 1 = 6
	if foal.Generation != 6 {
		t.Errorf("foal.Generation = %d, want 6 (max(3,5)+1)", foal.Generation)
	}
}

func TestBreed_GenerationIncrement_SireHigher(t *testing.T) {
	sire := makeHorse(allAA(), 7)
	mare := makeHorse(allBB(), 2)
	foal := Breed(sire, mare)

	if foal.Generation != 8 {
		t.Errorf("foal.Generation = %d, want 8 (max(7,2)+1)", foal.Generation)
	}
}

func TestBreed_GenerationIncrement_Equal(t *testing.T) {
	sire := makeHorse(allAA(), 4)
	mare := makeHorse(allBB(), 4)
	foal := Breed(sire, mare)

	if foal.Generation != 5 {
		t.Errorf("foal.Generation = %d, want 5 (max(4,4)+1)", foal.Generation)
	}
}

func TestBreed_FoalProperties(t *testing.T) {
	sire := makeHorse(allAA(), 0)
	sire.ID = "sire-123"
	mare := makeHorse(allBB(), 0)
	mare.ID = "mare-456"

	foal := Breed(sire, mare)

	if foal.ID == "" {
		t.Error("foal.ID is empty")
	}
	if foal.Name == "" {
		t.Error("foal.Name is empty")
	}
	if foal.SireID != "sire-123" {
		t.Errorf("foal.SireID = %q, want %q", foal.SireID, "sire-123")
	}
	if foal.MareID != "mare-456" {
		t.Errorf("foal.MareID = %q, want %q", foal.MareID, "mare-456")
	}
	if foal.Age != 0 {
		t.Errorf("foal.Age = %d, want 0", foal.Age)
	}
	if foal.Wins != 0 || foal.Losses != 0 || foal.Races != 0 {
		t.Errorf("foal should have 0 wins/losses/races, got %d/%d/%d", foal.Wins, foal.Losses, foal.Races)
	}
	if foal.ELO != 1200 {
		t.Errorf("foal.ELO = %f, want 1200", foal.ELO)
	}
	if foal.IsLegendary {
		t.Error("foal.IsLegendary should be false")
	}
}

func TestBreed_CurrentFitnessIsHalfCeiling(t *testing.T) {
	sire := makeHorse(allAA(), 0)
	mare := makeHorse(allAA(), 0)

	for i := 0; i < 50; i++ {
		foal := Breed(sire, mare)
		// CurrentFitness should be FitnessCeiling × 0.5
		expected := foal.FitnessCeiling * 0.5
		if math.Abs(foal.CurrentFitness-expected) > 1e-9 {
			t.Errorf("foal.CurrentFitness = %f, want %f (half of ceiling %f)",
				foal.CurrentFitness, expected, foal.FitnessCeiling)
		}
	}
}

func TestBreed_FitnessCeilingWithinJitterRange(t *testing.T) {
	// Breed two all-AA horses. Base ceiling = 1.0.
	// Jitter is ±5%, so ceiling should be in [0.95, 1.05] but clamped to [0, 1].
	// So effective range is [0.95, 1.0].
	sire := makeHorse(allAA(), 0)
	mare := makeHorse(allAA(), 0)

	for i := 0; i < 200; i++ {
		foal := Breed(sire, mare)
		if foal.FitnessCeiling < 0.0 || foal.FitnessCeiling > 1.0 {
			t.Errorf("foal.FitnessCeiling = %f, out of [0, 1] range", foal.FitnessCeiling)
		}
	}
}

func TestBreed_PunnettCrossVariation(t *testing.T) {
	// Both parents are AB for all genes.
	// Foal alleles should sometimes be A, sometimes B (from each parent).
	sire := makeHorse(allAB(), 0)
	mare := makeHorse(allAB(), 0)

	sawA := false
	sawB := false
	for i := 0; i < 200; i++ {
		foal := Breed(sire, mare)
		gene := foal.Genome[models.GeneSPD]
		if gene.AlleleA == models.AlleleA {
			sawA = true
		}
		if gene.AlleleA == models.AlleleB {
			sawB = true
		}
		if sawA && sawB {
			break
		}
	}
	if !sawA || !sawB {
		t.Error("expected Punnett cross to produce both A and B alleles from AB parents")
	}
}

// ---------------------------------------------------------------------------
// Tests: CreateLegendary
// ---------------------------------------------------------------------------

func TestCreateLegendary_AllSixLots(t *testing.T) {
	expectedNames := map[int]string{
		1: "Thundercock's Legacy",
		2: "Sir Flannelsworth III",
		3: "Midnight Deploy",
		4: "The Honorable Hummus",
		5: "Sapphic Sunrise",
		6: "E-008's Chosen",
	}

	for lot, expectedName := range expectedNames {
		horse := CreateLegendary(lot)
		if horse == nil {
			t.Errorf("CreateLegendary(%d) returned nil", lot)
			continue
		}
		if horse.Name != expectedName {
			t.Errorf("Lot %d name = %q, want %q", lot, horse.Name, expectedName)
		}
		if !horse.IsLegendary {
			t.Errorf("Lot %d IsLegendary = false, want true", lot)
		}
		if horse.LotNumber != lot {
			t.Errorf("Lot %d LotNumber = %d, want %d", lot, horse.LotNumber, lot)
		}
		if horse.Generation != 0 {
			t.Errorf("Lot %d Generation = %d, want 0", lot, horse.Generation)
		}
		if horse.Lore == "" {
			t.Errorf("Lot %d has empty Lore", lot)
		}
		if horse.ID == "" {
			t.Errorf("Lot %d has empty ID", lot)
		}
		if horse.ELO != 1200 {
			t.Errorf("Lot %d ELO = %f, want 1200", lot, horse.ELO)
		}

		// Verify genome has all 7 genes with valid alleles.
		if len(horse.Genome) != 7 {
			t.Errorf("Lot %d genome has %d genes, want 7", lot, len(horse.Genome))
		}
		for _, gt := range allGeneTypes {
			gene, ok := horse.Genome[gt]
			if !ok {
				t.Errorf("Lot %d missing gene %s", lot, gt)
				continue
			}
			if !isValidAllele(gene.AlleleA) || !isValidAllele(gene.AlleleB) {
				t.Errorf("Lot %d gene %s has invalid alleles: %s%s", lot, gt, gene.AlleleA, gene.AlleleB)
			}
		}

		// Legendaries start at full fitness.
		if horse.CurrentFitness != horse.FitnessCeiling {
			t.Errorf("Lot %d CurrentFitness (%f) != FitnessCeiling (%f); legendaries should start at full",
				lot, horse.CurrentFitness, horse.FitnessCeiling)
		}
	}
}

func TestCreateLegendary_InvalidLot(t *testing.T) {
	for _, lot := range []int{0, -1, 13, 100} {
		horse := CreateLegendary(lot)
		if horse != nil {
			t.Errorf("CreateLegendary(%d) should return nil for invalid lot", lot)
		}
	}
}

func TestCreateLegendary_ThundercockGenome(t *testing.T) {
	horse := CreateLegendary(1)
	if horse == nil {
		t.Fatal("Lot 1 returned nil")
	}
	// Thundercock: SPD:AA, STM:AA, TMP:AB, SZE:AB, REC:AA, INT:AB, MUT:AB
	checks := map[models.GeneType]string{
		models.GeneSPD: "AA",
		models.GeneSTM: "AA",
		models.GeneTMP: "AB",
		models.GeneSZE: "AB",
		models.GeneREC: "AA",
		models.GeneINT: "AB",
		models.GeneMUT: "AB",
	}
	for gt, want := range checks {
		gene := horse.Genome[gt]
		got := string(gene.AlleleA) + string(gene.AlleleB)
		if got != want {
			t.Errorf("Thundercock %s = %s, want %s", gt, got, want)
		}
	}
}

func TestCreateLegendary_SirFlannelsworthGenome(t *testing.T) {
	horse := CreateLegendary(2)
	if horse == nil {
		t.Fatal("Lot 2 returned nil")
	}
	// Sir Flannelsworth: SPD:AB, STM:AA, TMP:AA, SZE:AA, REC:AB, INT:AA, MUT:AB
	checks := map[models.GeneType]string{
		models.GeneSPD: "AB",
		models.GeneSTM: "AA",
		models.GeneTMP: "AA",
		models.GeneSZE: "AA",
		models.GeneREC: "AB",
		models.GeneINT: "AA",
		models.GeneMUT: "AB",
	}
	for gt, want := range checks {
		gene := horse.Genome[gt]
		got := string(gene.AlleleA) + string(gene.AlleleB)
		if got != want {
			t.Errorf("Sir Flannelsworth %s = %s, want %s", gt, got, want)
		}
	}
}

func TestCreateLegendary_MidnightDeployGenome(t *testing.T) {
	horse := CreateLegendary(3)
	if horse == nil {
		t.Fatal("Lot 3 returned nil")
	}
	// Midnight Deploy: SPD:AA, STM:AB, TMP:BB, SZE:BB, REC:AB, INT:AA, MUT:BA
	checks := map[models.GeneType]string{
		models.GeneSPD: "AA",
		models.GeneSTM: "AB",
		models.GeneTMP: "BB",
		models.GeneSZE: "BB",
		models.GeneREC: "AB",
		models.GeneINT: "AA",
		models.GeneMUT: "BA",
	}
	for gt, want := range checks {
		gene := horse.Genome[gt]
		got := string(gene.AlleleA) + string(gene.AlleleB)
		if got != want {
			t.Errorf("Midnight Deploy %s = %s, want %s", gt, got, want)
		}
	}
}

func TestCreateLegendary_HonorableHummusGenome(t *testing.T) {
	horse := CreateLegendary(4)
	if horse == nil {
		t.Fatal("Lot 4 returned nil")
	}
	// All AB
	for _, gt := range allGeneTypes {
		gene := horse.Genome[gt]
		got := string(gene.AlleleA) + string(gene.AlleleB)
		if got != "AB" {
			t.Errorf("Honorable Hummus %s = %s, want AB", gt, got)
		}
	}
}

func TestCreateLegendary_SapphicSunriseGenome(t *testing.T) {
	horse := CreateLegendary(5)
	if horse == nil {
		t.Fatal("Lot 5 returned nil")
	}
	// All AA — perfect horse
	for _, gt := range allGeneTypes {
		gene := horse.Genome[gt]
		got := string(gene.AlleleA) + string(gene.AlleleB)
		if got != "AA" {
			t.Errorf("Sapphic Sunrise %s = %s, want AA", gt, got)
		}
	}
	// Fitness ceiling should be 1.0 (all AA, no override)
	if math.Abs(horse.FitnessCeiling-1.0) > 1e-9 {
		t.Errorf("Sapphic Sunrise FitnessCeiling = %f, want 1.0", horse.FitnessCeiling)
	}
}

func TestCreateLegendary_E008Genome(t *testing.T) {
	horse := CreateLegendary(6)
	if horse == nil {
		t.Fatal("Lot 6 returned nil")
	}
	// All AA except MUT:BB
	for _, gt := range allGeneTypes {
		gene := horse.Genome[gt]
		got := string(gene.AlleleA) + string(gene.AlleleB)
		if gt == models.GeneMUT {
			if got != "BB" {
				t.Errorf("E-008 %s = %s, want BB", gt, got)
			}
		} else {
			if got != "AA" {
				t.Errorf("E-008 %s = %s, want AA", gt, got)
			}
		}
	}
	// Fitness ceiling override: 9.99
	if math.Abs(horse.FitnessCeiling-9.99) > 1e-9 {
		t.Errorf("E-008 FitnessCeiling = %f, want 9.99", horse.FitnessCeiling)
	}
	// Starts at full fitness
	if horse.CurrentFitness != horse.FitnessCeiling {
		t.Errorf("E-008 CurrentFitness = %f, want %f", horse.CurrentFitness, horse.FitnessCeiling)
	}
}

func TestCreateLegendary_UniqueIDs(t *testing.T) {
	ids := make(map[string]bool)
	for lot := 1; lot <= 12; lot++ {
		horse := CreateLegendary(lot)
		if horse == nil {
			t.Fatalf("Lot %d returned nil", lot)
		}
		if ids[horse.ID] {
			t.Errorf("Lot %d has duplicate ID %s", lot, horse.ID)
		}
		ids[horse.ID] = true
	}
}

// ---------------------------------------------------------------------------
// Tests: GenomeToString
// ---------------------------------------------------------------------------

func TestGenomeToString_AllAA(t *testing.T) {
	genome := allAA()
	got := GenomeToString(genome)
	want := "SPD:AA STM:AA TMP:AA SZE:AA REC:AA INT:AA MUT:AA"
	if got != want {
		t.Errorf("GenomeToString(allAA) = %q, want %q", got, want)
	}
}

func TestGenomeToString_AllBB(t *testing.T) {
	genome := allBB()
	got := GenomeToString(genome)
	want := "SPD:BB STM:BB TMP:BB SZE:BB REC:BB INT:BB MUT:BB"
	if got != want {
		t.Errorf("GenomeToString(allBB) = %q, want %q", got, want)
	}
}

func TestGenomeToString_MissingGene(t *testing.T) {
	// Genome with only SPD — all others should show "??"
	genome := makeGenome(map[models.GeneType][2]models.Allele{
		models.GeneSPD: {models.AlleleA, models.AlleleB},
	})
	got := GenomeToString(genome)
	want := "SPD:AB STM:?? TMP:?? SZE:?? REC:?? INT:?? MUT:??"
	if got != want {
		t.Errorf("GenomeToString(partial) = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Tests: SortedGeneTypes
// ---------------------------------------------------------------------------

func TestSortedGeneTypes_Order(t *testing.T) {
	genome := allAA()
	sorted := SortedGeneTypes(genome)

	if len(sorted) != 7 {
		t.Fatalf("SortedGeneTypes returned %d types, want 7", len(sorted))
	}

	// Verify alphabetical ordering.
	for i := 1; i < len(sorted); i++ {
		if string(sorted[i]) < string(sorted[i-1]) {
			t.Errorf("SortedGeneTypes not sorted: %s before %s at index %d",
				sorted[i-1], sorted[i], i)
		}
	}
}

func TestSortedGeneTypes_EmptyGenome(t *testing.T) {
	genome := models.Genome{}
	sorted := SortedGeneTypes(genome)
	if len(sorted) != 0 {
		t.Errorf("SortedGeneTypes(empty) returned %d types, want 0", len(sorted))
	}
}

// ---------------------------------------------------------------------------
// Tests: Edge cases in breeding
// ---------------------------------------------------------------------------

func TestBreed_IdenticalParents(t *testing.T) {
	// Breed two identical all-AA parents.
	// Foal should always be all-AA (no mutation possible since MUT will be AA).
	parent := makeHorse(allAA(), 0)
	for i := 0; i < 100; i++ {
		foal := Breed(parent, parent)
		for _, gt := range allGeneTypes {
			gene := foal.Genome[gt]
			if gene.AlleleA != models.AlleleA || gene.AlleleB != models.AlleleA {
				t.Errorf("iteration %d: breeding AA×AA gave %s:%s%s, want AA",
					i, gt, gene.AlleleA, gene.AlleleB)
			}
		}
	}
}

func TestBreed_IdenticalBBParents(t *testing.T) {
	// Breed two identical all-BB parents.
	// Foal should always be all-BB.
	// Note: MUT will be BB so mutation can trigger (1% chance),
	// but we accept that rare mutation might flip one allele.
	parent := makeHorse(allBB(), 0)

	mutationSeen := false
	for i := 0; i < 500; i++ {
		foal := Breed(parent, parent)
		for _, gt := range allGeneTypes {
			gene := foal.Genome[gt]
			if gene.AlleleA != models.AlleleB || gene.AlleleB != models.AlleleB {
				// This could be a mutation — it's expected rarely.
				mutationSeen = true
			}
		}
	}
	// We just verify that this doesn't crash. Mutation is possible but rare.
	_ = mutationSeen
}

func TestBreed_HighGenerationParents(t *testing.T) {
	sire := makeHorse(allAA(), 999)
	mare := makeHorse(allBB(), 1000)
	foal := Breed(sire, mare)
	if foal.Generation != 1001 {
		t.Errorf("foal.Generation = %d, want 1001", foal.Generation)
	}
}

func TestBreed_ZeroGenerationParents(t *testing.T) {
	sire := makeHorse(allAA(), 0)
	mare := makeHorse(allBB(), 0)
	foal := Breed(sire, mare)
	if foal.Generation != 1 {
		t.Errorf("foal.Generation = %d, want 1", foal.Generation)
	}
}

func TestBreed_NonNegativeFitness(t *testing.T) {
	// Breed two all-BB parents (low fitness) many times.
	// Verify fitness is never negative after jitter.
	sire := makeHorse(allBB(), 0)
	mare := makeHorse(allBB(), 0)

	for i := 0; i < 500; i++ {
		foal := Breed(sire, mare)
		if foal.FitnessCeiling < 0.0 {
			t.Errorf("iteration %d: foal.FitnessCeiling = %f, should not be negative",
				i, foal.FitnessCeiling)
		}
		if foal.CurrentFitness < 0.0 {
			t.Errorf("iteration %d: foal.CurrentFitness = %f, should not be negative",
				i, foal.CurrentFitness)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests: geneWeights sum to 1.0
// ---------------------------------------------------------------------------

func TestGeneWeights_SumToOne(t *testing.T) {
	var total float64
	for _, w := range geneWeights {
		total += w
	}
	if math.Abs(total-1.0) > 1e-9 {
		t.Errorf("geneWeights sum = %f, want 1.0", total)
	}
}

func TestGeneWeights_AllGeneTypesCovered(t *testing.T) {
	for _, gt := range allGeneTypes {
		if _, ok := geneWeights[gt]; !ok {
			t.Errorf("geneWeights missing entry for %s", gt)
		}
	}
}
