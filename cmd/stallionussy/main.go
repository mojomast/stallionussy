// Package main is the entry point for StallionUSSY — a premium equine
// genetics trading simulator. It supports two modes:
//
//	serve  — starts the HTTP server (default)
//	cli    — launches an interactive terminal session
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mojomast/stallionussy/internal/genussy"
	"github.com/mojomast/stallionussy/internal/marketussy"
	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/nameussy"
	"github.com/mojomast/stallionussy/internal/pedigreussy"
	"github.com/mojomast/stallionussy/internal/racussy"
	"github.com/mojomast/stallionussy/internal/repository"
	"github.com/mojomast/stallionussy/internal/repository/postgres"
	"github.com/mojomast/stallionussy/internal/server"
	"github.com/mojomast/stallionussy/internal/stableussy"
	"github.com/mojomast/stallionussy/internal/tournussy"
	"github.com/mojomast/stallionussy/internal/trainussy"
)

// banner is the startup art for StallionUSSY.
const banner = `
 _____ _        _ _ _              _   _ ____ ______   __
/ ____| |      | | (_)            | | | / ___/ ___\ \ / /
| (___ | |_ __ _| | |_  ___  _ __ | | | \___ \___ \\ V / 
 \___ \| __/ _` + "`" + ` | | | |/ _ \| '_ \| | | |___) |__) || |  
 ____) | || (_| | | | | (_) | | | | |_| |____/____/ | |  
|_____/ \__\__,_|_|_|_|\___/|_| |_|\___/           |_|  
                                                          
Premium Equine Genetics Exchange v2.0
"The yogurt is patient. The yogurt remembers."
Type 'help' for commands. Type 'seed' to get started.
`

// helpText is the CLI help message displayed on "help" or unknown commands.
const helpText = `
=== STABLE MANAGEMENT ===
  seed                              Create a demo stable with legendaries
  stable                            List all stables
  horses [stableID]                 List horses (all or by stable)
  inspect <horseID>                 Show horse details, traits, and genome

=== BREEDING ===
  breed <sireID> <mareID>           Breed two horses (Punnett cross)
  pedigree <horseID>                Display ASCII pedigree tree (depth 3)
  dynasty [stableID]                Show dynasty info for a stable
  traits <horseID>                  Show horse's traits with descriptions

=== RACING ===
  race <id1> <id2> [id3...]         Race horses against each other
  quick-race                        Random race with random horses
  history [horseID]                 Show race history (all or for a horse)
  stats <horseID>                   Show computed stats (win rate, streaks...)

=== TRAINING ===
  train <horseID> <workout>         Train a horse (Sprint/Endurance/MentalRep/MudRun/RestDay/General)
  rest <horseID>                    Rest day shortcut (reduces fatigue)

=== MARKET ===
  market                            Show active stud listings
  offer <horseID> <toStableID> <price>  Create a trade offer
  offers [stableID]                 List pending trade offers
  accept <offerID>                  Accept a trade offer
  reject <offerID>                  Reject a trade offer

=== TOURNAMENTS ===
  tournament create [name]          Create a tournament
  tournament list                   List all tournaments
  tournament register <tournID> <horseID>  Register a horse
  tournament run <tournID>          Run the next round
  tournament standings <tournID>    Show current standings

=== PROGRESSION ===
  advance                           Advance one season (age, retire, etc.)
  achievements [stableID]           Show all unlocked achievements
  leaderboard                       Show ELO rankings

=== SYSTEM ===
  help                              Show this help message
  exit                              Quit StallionUSSY
`

// defaultPort is the default HTTP listen port. It can be overridden by:
//  1. The --port CLI flag (highest priority)
//  2. The STALLIONUSSY_PORT environment variable (fallback when --port is at default)
//
// This allows both CLI usage (--port 4200) and environment-based configuration
// (systemd EnvironmentFile, docker-compose env) to set the port without
// conflicting with each other.
const defaultPort = "8080"

// resolvePort returns the effective port. If the CLI flag is still at the
// default value, the STALLIONUSSY_PORT env var takes precedence.
func resolvePort(flagValue string) string {
	if flagValue != defaultPort {
		// Explicit --port flag wins.
		return flagValue
	}
	if envPort := os.Getenv("STALLIONUSSY_PORT"); envPort != "" {
		return envPort
	}
	return defaultPort
}

func main() {
	// -----------------------------------------------------------------------
	// Subcommand parsing
	// -----------------------------------------------------------------------
	serveCmd := flag.NewFlagSet("serve", flag.ExitOnError)
	servePort := serveCmd.String("port", defaultPort, "HTTP server port (env: STALLIONUSSY_PORT)")

	cliCmd := flag.NewFlagSet("cli", flag.ExitOnError)

	// If no subcommand is given, default to "serve".
	if len(os.Args) < 2 {
		runServe(resolvePort(*servePort))
		return
	}

	switch os.Args[1] {
	case "serve":
		serveCmd.Parse(os.Args[2:])
		runServe(resolvePort(*servePort))
	case "cli":
		cliCmd.Parse(os.Args[2:])
		runCLI()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		fmt.Fprintln(os.Stderr, "Usage: stallionussy [serve|cli]")
		fmt.Fprintln(os.Stderr, "  serve  --port 8080   Start the HTTP server")
		fmt.Fprintln(os.Stderr, "  cli                  Interactive terminal mode")
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// Server mode
// ---------------------------------------------------------------------------

// defaultDatabaseURL is the fallback PostgreSQL connection string when
// the DATABASE_URL environment variable is not set.
const defaultDatabaseURL = "postgres://stallionussy:h0rs3ussy420@localhost/stallionussy?sslmode=disable"

// connectDB establishes a PostgreSQL connection and runs schema migrations.
// Returns the *postgres.DB (caller must Close) or nil + error.
func connectDB() (*postgres.DB, error) {
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		connStr = defaultDatabaseURL
	}

	db, err := postgres.New(connStr)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	if err := repository.RunMigrations(db.GetDB()); err != nil {
		db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return db, nil
}

// runServe starts the HTTP API server on the given port.
func runServe(port string) {
	fmt.Print(banner)
	fmt.Println()

	addr := ":" + port
	fmt.Printf("Starting StallionUSSY server on %s ...\n", addr)

	// Connect to PostgreSQL and run migrations.
	db, err := connectDB()
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	log.Println("Database connected and migrations applied.")

	// Create server with DB — the server package will be updated to accept it.
	srv := server.NewServer(db)

	// srv.Start blocks until shutdown signal is received and graceful drain
	// completes. After it returns, we can safely close the DB connection.
	if err := srv.Start(addr); err != nil {
		// Close DB before exiting on fatal error.
		db.Close()
		log.Fatalf("Server failed: %v", err)
	}

	// Clean shutdown: close the database connection now that all in-flight
	// requests have drained and background goroutines have stopped.
	log.Println("Closing database connection...")
	db.Close()
	log.Println("StallionUSSY shut down cleanly. The yogurt rests.")
}

// ---------------------------------------------------------------------------
// CLI mode — interactive terminal session
// ---------------------------------------------------------------------------

// cliState holds all the in-memory state for a CLI session.
type cliState struct {
	sm          *stableussy.StableManager
	market      *marketussy.Market
	trainer     *trainussy.Trainer
	tournaments *tournussy.TournamentManager
	raceHistory *tournussy.RaceHistory
	pedigree    *pedigreussy.PedigreeEngine
	trades      *pedigreussy.TradeManager
	season      int // current season counter

	// Persistence repositories — nil if database is not available.
	// When non-nil, mutations are written through to PostgreSQL.
	horseRepo       repository.HorseRepository
	stableRepo      repository.StableRepository
	raceResultRepo  repository.RaceResultRepository
	marketRepo      repository.MarketRepository
	achievementRepo repository.AchievementRepository
}

// ---------------------------------------------------------------------------
// Persistence helpers — nil-safe, best-effort write-through to PostgreSQL.
// These log warnings on failure but never stop the CLI from functioning.
// ---------------------------------------------------------------------------

// persistHorse writes the current state of a horse to the database.
// Safe to call when horseRepo is nil (DB unavailable).
func (s *cliState) persistHorse(horse *models.Horse) {
	if s.horseRepo == nil {
		return
	}
	ctx := context.Background()
	// Try update first; if the horse doesn't exist yet, create it.
	if err := s.horseRepo.UpdateHorse(ctx, horse); err != nil {
		// Attempt a create in case this is a new horse.
		if createErr := s.horseRepo.CreateHorse(ctx, horse); createErr != nil {
			log.Printf("[DB] Warning: failed to persist horse %s: update=%v, create=%v", horse.ID, err, createErr)
		}
	}
}

// persistStable writes the current state of a stable to the database.
// Safe to call when stableRepo is nil (DB unavailable).
func (s *cliState) persistStable(stable *models.Stable) {
	if s.stableRepo == nil {
		return
	}
	ctx := context.Background()
	if err := s.stableRepo.UpdateStable(ctx, stable); err != nil {
		if createErr := s.stableRepo.CreateStable(ctx, stable); createErr != nil {
			log.Printf("[DB] Warning: failed to persist stable %s: update=%v, create=%v", stable.ID, err, createErr)
		}
	}
}

// persistRaceResult writes a race result to the database.
// Safe to call when raceResultRepo is nil (DB unavailable).
func (s *cliState) persistRaceResult(result *models.RaceResult) {
	if s.raceResultRepo == nil {
		return
	}
	ctx := context.Background()
	if err := s.raceResultRepo.RecordResult(ctx, result); err != nil {
		log.Printf("[DB] Warning: failed to persist race result for horse %s: %v", result.HorseID, err)
	}
}

// persistAchievement writes a newly unlocked achievement to the database.
// Safe to call when achievementRepo is nil (DB unavailable).
func (s *cliState) persistAchievement(stableID string, achievement *models.Achievement) {
	if s.achievementRepo == nil {
		return
	}
	ctx := context.Background()
	if err := s.achievementRepo.AddAchievement(ctx, stableID, achievement); err != nil {
		log.Printf("[DB] Warning: failed to persist achievement %s for stable %s: %v", achievement.ID, stableID, err)
	}
}

// runCLI enters the interactive CLI loop.
func runCLI() {
	fmt.Print(banner)
	fmt.Println()

	// Attempt to connect to PostgreSQL for persistence.
	// If the connection fails, the CLI continues in memory-only mode.
	db, dbErr := connectDB()
	if dbErr != nil {
		log.Printf("Warning: Could not connect to database: %v", dbErr)
		log.Printf("Running in memory-only mode (data will not be persisted).")
	} else {
		defer db.Close()
		log.Println("Database connected — CLI state will be persisted to PostgreSQL.")
	}

	sm := stableussy.NewStableManager()
	raceHistory := tournussy.NewRaceHistory()

	state := &cliState{
		sm:          sm,
		market:      marketussy.NewMarket(),
		trainer:     trainussy.NewTrainer(),
		tournaments: tournussy.NewTournamentManager(raceHistory),
		raceHistory: raceHistory,
		pedigree:    pedigreussy.NewPedigreeEngine(sm.GetHorse),
		trades:      pedigreussy.NewTradeManager(),
		season:      0,
	}

	// Wire up persistence repos if DB is available.
	if db != nil {
		state.horseRepo = postgres.NewHorseRepo(db)
		state.stableRepo = postgres.NewStableRepo(db)
		state.raceResultRepo = postgres.NewRaceResultRepo(db)
		state.marketRepo = postgres.NewMarketRepo(db)
		state.achievementRepo = postgres.NewAchievementRepo(db)
	}

	fmt.Println("Welcome to StallionUSSY CLI. Type 'help' for commands.")
	fmt.Println("Tip: run 'seed' first to create a demo stable with legendary horses.")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("stallionussy> ")

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			fmt.Print("stallionussy> ")
			continue
		}

		parts := strings.Fields(line)
		cmd := strings.ToLower(parts[0])
		args := parts[1:]

		switch cmd {
		case "exit", "quit", "q":
			fmt.Println("The yogurt bids you farewell.")
			return
		case "help", "?":
			fmt.Print(helpText)

		// --- Stable Management ---
		case "seed":
			cmdSeed(state)
		case "stable", "stables":
			cmdListStables(state)
		case "horses":
			cmdListHorses(state, args)
		case "inspect":
			cmdInspect(state, args)

		// --- Breeding ---
		case "breed":
			cmdBreed(state, args)
		case "pedigree":
			cmdPedigree(state, args)
		case "dynasty":
			cmdDynasty(state, args)
		case "traits":
			cmdTraits(state, args)

		// --- Racing ---
		case "race":
			cmdRace(state, args)
		case "quick-race":
			cmdQuickRace(state)
		case "history":
			cmdHistory(state, args)
		case "stats":
			cmdStats(state, args)

		// --- Training ---
		case "train":
			cmdTrain(state, args)
		case "rest":
			cmdRest(state, args)

		// --- Market & Trading ---
		case "market":
			cmdMarket(state)
		case "offer":
			cmdOffer(state, args)
		case "offers":
			cmdOffers(state, args)
		case "accept":
			cmdAccept(state, args)
		case "reject":
			cmdReject(state, args)

		// --- Tournaments ---
		case "tournament":
			cmdTournament(state, args)

		// --- Progression ---
		case "advance":
			cmdAdvance(state)
		case "achievements":
			cmdAchievements(state, args)
		case "leaderboard":
			cmdLeaderboard(state)

		default:
			fmt.Printf("Unknown command: %s (type 'help' for commands)\n", cmd)
		}

		fmt.Println()
		fmt.Print("stallionussy> ")
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
		os.Exit(1)
	}
}

// ===========================================================================
// CLI Commands — Stable Management
// ===========================================================================

// cmdSeed creates a demo stable and populates it with the 6 legendary horses.
func cmdSeed(state *cliState) {
	stableName := nameussy.GenerateStableName()
	stable := state.sm.CreateStable(stableName, "cli-player")
	state.sm.SeedLegendaries(stable.ID)

	fmt.Printf("Created stable: %s (ID: %s)\n", stable.Name, stable.ID)
	fmt.Printf("Starting Cummies: %d\n", stable.Cummies)
	fmt.Println("Seeded 6 legendary horses:")

	horses := state.sm.ListHorses(stable.ID)
	for _, h := range horses {
		sappho := marketussy.CalcSapphoScore(h)
		sapphoStr := fmt.Sprintf("%.1f", sappho)
		if math.IsNaN(sappho) {
			sapphoStr = "NaN (ANOMALOUS)"
		}
		fmt.Printf("  [Lot %d] %-25s  Fitness: %.2f  Sappho: %s\n",
			h.LotNumber, h.Name, h.FitnessCeiling, sapphoStr)
	}
	fmt.Println()
	fmt.Println("Your stable is ready. The yogurt approves.")

	// Persist the new stable and all seeded horses to the database.
	state.persistStable(stable)
	for _, h := range horses {
		state.persistHorse(h)
	}
} // cmdListStables prints all stables.
func cmdListStables(state *cliState) {
	stables := state.sm.ListStables()
	if len(stables) == 0 {
		fmt.Println("No stables found. Run 'seed' to create a demo stable.")
		return
	}

	fmt.Println("=== STABLES ===")
	for _, s := range stables {
		fmt.Printf("  %-40s  ID: %s  Cummies: %d  Horses: %d\n",
			s.Name, s.ID, s.Cummies, len(s.Horses))
	}
}

// cmdListHorses prints horses, optionally filtered by stable ID.
func cmdListHorses(state *cliState, args []string) {
	if len(args) > 0 {
		// List horses for a specific stable.
		stableID := args[0]
		horses := state.sm.ListHorses(stableID)
		if horses == nil {
			fmt.Printf("Stable not found: %s\n", stableID)
			return
		}
		if len(horses) == 0 {
			fmt.Println("No horses in this stable.")
			return
		}

		stable, _ := state.sm.GetStable(stableID)
		stableName := stableID
		if stable != nil {
			stableName = stable.Name
		}

		fmt.Printf("=== HORSES in %s ===\n", stableName)
		printHorseList(horses)
		return
	}

	// List all horses across all stables.
	stables := state.sm.ListStables()
	if len(stables) == 0 {
		fmt.Println("No stables found. Run 'seed' to create a demo stable.")
		return
	}

	for _, s := range stables {
		fmt.Printf("=== %s ===\n", s.Name)
		horses := state.sm.ListHorses(s.ID)
		if len(horses) == 0 {
			fmt.Println("  (empty)")
		} else {
			printHorseList(horses)
		}
		fmt.Println()
	}
}

// printHorseList prints a formatted list of horses with traits and fatigue.
func printHorseList(horses []*models.Horse) {
	for _, h := range horses {
		legendary := ""
		if h.IsLegendary {
			legendary = fmt.Sprintf(" [Lot %d]", h.LotNumber)
		}
		retired := ""
		if h.Retired {
			retired = " [RETIRED]"
		}
		fatigueStr := fmt.Sprintf("Ftg:%.0f", h.Fatigue)
		traitsStr := fmt.Sprintf("Traits:%d", len(h.Traits))
		fmt.Printf("  %-30s  ELO: %6.0f  W/L: %d/%d  Fitness: %.2f  %s  %s%s%s\n",
			h.Name, h.ELO, h.Wins, h.Losses, h.CurrentFitness, fatigueStr, traitsStr, legendary, retired)
		fmt.Printf("    ID: %s\n", h.ID)
	}
}

// ===========================================================================
// CLI Commands — Inspection
// ===========================================================================

// cmdInspect displays detailed information about a horse, including traits,
// fatigue, retired status, training XP, peak ELO, and inbreeding coefficient.
func cmdInspect(state *cliState, args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: inspect <horseID>")
		return
	}

	h, err := state.sm.GetHorse(args[0])
	if err != nil {
		fmt.Printf("Horse not found: %s\n", args[0])
		return
	}

	sappho := marketussy.CalcSapphoScore(h)
	sapphoStr := fmt.Sprintf("%.2f", sappho)
	if math.IsNaN(sappho) {
		sapphoStr = "NaN (ANOMALOUS — the Sappho Scale cannot classify this entity)"
	}

	fmt.Println("=== HORSE INSPECTION ===")
	fmt.Printf("  Name:            %s\n", h.Name)
	fmt.Printf("  ID:              %s\n", h.ID)
	fmt.Printf("  Legendary:       %v\n", h.IsLegendary)
	if h.LotNumber > 0 {
		fmt.Printf("  Lot Number:      %d\n", h.LotNumber)
	}
	fmt.Printf("  Generation:      %d\n", h.Generation)
	fmt.Printf("  Age:             %d (%s)\n", h.Age, trainussy.LifeStage(h))
	fmt.Printf("  Retired:         %v\n", h.Retired)
	fmt.Println()
	fmt.Printf("  Fitness Ceiling: %.4f\n", h.FitnessCeiling)
	fmt.Printf("  Current Fitness: %.4f\n", h.CurrentFitness)
	fmt.Printf("  Sappho Score:    %s\n", sapphoStr)
	fmt.Printf("  Fatigue:         %.0f/100\n", h.Fatigue)
	fmt.Printf("  Training XP:     %.1f\n", h.TrainingXP)
	fmt.Println()
	fmt.Printf("  ELO:             %.0f\n", h.ELO)
	fmt.Printf("  Peak ELO:        %.0f\n", h.PeakELO)
	fmt.Printf("  Races:           %d\n", h.Races)
	fmt.Printf("  Wins:            %d\n", h.Wins)
	fmt.Printf("  Losses:          %d\n", h.Losses)
	fmt.Printf("  Total Earnings:  %d Cummies\n", h.TotalEarnings)
	fmt.Println()

	if h.SireID != "" {
		fmt.Printf("  Sire ID:         %s\n", h.SireID)
	} else {
		fmt.Printf("  Sire:            (founder / no sire)\n")
	}
	if h.MareID != "" {
		fmt.Printf("  Mare ID:         %s\n", h.MareID)
	} else {
		fmt.Printf("  Mare:            (founder / no mare)\n")
	}

	// Inbreeding coefficient
	coeff, cErr := state.pedigree.CalcInbreedingCoefficient(h.ID)
	if cErr == nil {
		penaltyLabel := inbreedingLabel(coeff)
		fmt.Printf("  Inbreeding:      %.4f (%s)\n", coeff, penaltyLabel)
	}
	fmt.Println()

	// --- Traits ---
	if len(h.Traits) > 0 {
		fmt.Println("  === TRAITS ===")
		for _, t := range h.Traits {
			fmt.Printf("    [%s] %s — %s (mag: %.2f, %s)\n",
				strings.ToUpper(t.Rarity), t.Name, t.Description, t.Magnitude, t.Effect)
		}
		fmt.Println()
	}

	fmt.Println("  === GENOME ===")
	fmt.Printf("  %s\n", genussy.GenomeToString(h.Genome))
	fmt.Println()
	fmt.Println("  Gene Breakdown:")
	for _, gt := range models.AllGeneTypes {
		gene, ok := h.Genome[gt]
		if !ok {
			fmt.Printf("    %-4s: ?? (missing)\n", gt)
			continue
		}
		expr := gene.Express()
		score := gene.GeneScore()
		label := geneLabel(gt, expr)
		fmt.Printf("    %-4s: %s  (score: %.2f)  %s\n", gt, expr, score, label)
	}

	if h.Lore != "" {
		fmt.Println()
		fmt.Println("  === LORE ===")
		fmt.Printf("  %s\n", h.Lore)
	}
}

// geneLabel returns a human-readable description of a gene expression.
func geneLabel(gt models.GeneType, expr string) string {
	labels := map[models.GeneType]map[string]string{
		models.GeneSPD: {"AA": "Elite Burst Speed", "AB": "Good Speed", "BB": "Sluggish"},
		models.GeneSTM: {"AA": "Iron Lungs", "AB": "Decent Stamina", "BB": "Glass Cannon"},
		models.GeneTMP: {"AA": "Ice Cold Calm", "AB": "Steady", "BB": "Volatile (Panic Risk)"},
		models.GeneSZE: {"AA": "Powerhouse Build", "AB": "Balanced Frame", "BB": "Lean & Light"},
		models.GeneREC: {"AA": "Fast Recovery", "AB": "Normal Recovery", "BB": "Slow Recovery"},
		models.GeneINT: {"AA": "Genius", "AB": "Sharp", "BB": "Dumb as Rocks"},
		models.GeneMUT: {"AA": "Stable Genome", "AB": "Carrier", "BB": "Mutation Hotspot"},
	}

	if m, ok := labels[gt]; ok {
		if l, ok := m[expr]; ok {
			return l
		}
	}
	return ""
}

// inbreedingLabel returns a human-readable description for an inbreeding coefficient.
func inbreedingLabel(coeff float64) string {
	switch {
	case coeff < 0.01:
		return "None"
	case coeff < 0.10:
		return "Negligible"
	case coeff < 0.25:
		return "Mild — 5% ceiling penalty"
	case coeff < 0.50:
		return "Moderate — 15% ceiling penalty"
	default:
		return "Severe — 30% ceiling penalty (gene puddle)"
	}
}

// ===========================================================================
// CLI Commands — Breeding
// ===========================================================================

// cmdBreed breeds two horses by their IDs with enhanced pedigree info.
func cmdBreed(state *cliState, args []string) {
	if len(args) < 2 {
		fmt.Println("Usage: breed <sireID> <mareID>")
		return
	}

	sire, err := state.sm.GetHorse(args[0])
	if err != nil {
		fmt.Printf("Sire not found: %s\n", args[0])
		return
	}

	mare, err := state.sm.GetHorse(args[1])
	if err != nil {
		fmt.Printf("Mare not found: %s\n", args[1])
		return
	}

	foal := genussy.Breed(sire, mare)
	fmt.Println("Breeding initiated...")
	fmt.Printf("  Sire: %s (%s)\n", sire.Name, sire.ID)
	fmt.Printf("  Mare: %s (%s)\n", mare.Name, mare.ID)
	fmt.Println()

	// Calculate bloodline bonus
	bloodlineBonus := pedigreussy.CalcBloodlineBonus(foal, sire.ID, mare.ID, state.sm.GetHorse)
	fmt.Printf("  Bloodline Bonus: %.2f (%+.0f%% ceiling bonus)\n", bloodlineBonus, (bloodlineBonus-1.0)*100)

	// Apply bloodline bonus to the foal's fitness ceiling.
	foal.FitnessCeiling *= bloodlineBonus
	if foal.FitnessCeiling > 1.0 && !sire.IsLegendary && !mare.IsLegendary {
		foal.FitnessCeiling = 1.0
	}
	foal.CurrentFitness = foal.FitnessCeiling * 0.5

	// Assign traits at birth
	state.trainer.AssignTraitsAtBirth(foal, sire, mare)

	// Print traits
	if len(foal.Traits) > 0 {
		traitNames := make([]string, len(foal.Traits))
		for i, t := range foal.Traits {
			traitNames[i] = fmt.Sprintf("[%s]", t.Name)
		}
		fmt.Printf("  Traits: %s\n", strings.Join(traitNames, " "))
		for _, t := range foal.Traits {
			fmt.Printf("    %s — %s (%s)\n", t.Name, t.Description, t.Rarity)
		}
	} else {
		fmt.Println("  Traits: (none assigned)")
	}
	fmt.Println()

	// Add foal to the sire's owner's stable (find the first stable owned by sire's owner).
	stables := state.sm.ListStables()
	placed := false
	for _, s := range stables {
		if s.OwnerID == sire.OwnerID {
			if err := state.sm.AddHorseToStable(s.ID, foal); err == nil {
				placed = true
				fmt.Printf("Foal added to stable: %s\n", s.Name)
			}
			break
		}
	}
	if !placed {
		// No stable found — create one for the foal.
		newStable := state.sm.CreateStable(nameussy.GenerateStableName(), "cli-player")
		_ = state.sm.AddHorseToStable(newStable.ID, foal)
		fmt.Printf("Created new stable '%s' for the foal.\n", newStable.Name)
	}

	// Calculate inbreeding coefficient on the FOAL (not the sire).
	// The foal must be in the stable manager so the pedigree engine can find it.
	inbreedCoeff := 0.0
	if coeff, cErr := state.pedigree.CalcInbreedingCoefficient(foal.ID); cErr == nil {
		inbreedCoeff = coeff
	}

	// Apply inbreeding penalty to foal's fitness ceiling.
	inbreedingPenalty := pedigreussy.InbreedingPenalty(inbreedCoeff)
	foal.FitnessCeiling *= inbreedingPenalty
	if foal.FitnessCeiling > 1.0 {
		foal.FitnessCeiling = 1.0
	}
	foal.CurrentFitness = foal.FitnessCeiling * 0.5

	penaltyLabel := inbreedingLabel(inbreedCoeff)
	fmt.Printf("  Inbreeding: %.2f (%s)\n", inbreedCoeff, penaltyLabel)

	fmt.Println()
	fmt.Printf("A new horse is born: %s\n", foal.Name)
	fmt.Printf("  ID:              %s\n", foal.ID)
	fmt.Printf("  Generation:      %d\n", foal.Generation)
	fmt.Printf("  Fitness Ceiling: %.4f\n", foal.FitnessCeiling)
	fmt.Printf("  Current Fitness: %.4f\n", foal.CurrentFitness)
	fmt.Printf("  Genome:          %s\n", genussy.GenomeToString(foal.Genome))
	fmt.Printf("  ELO:             %.0f\n", foal.ELO)

	// Persist the new foal to the database.
	state.persistHorse(foal)
}

// cmdPedigree displays an ASCII pedigree tree for a horse.
func cmdPedigree(state *cliState, args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: pedigree <horseID>")
		return
	}

	tree, err := state.pedigree.BuildPedigree(args[0], 3)
	if err != nil || tree == nil {
		fmt.Printf("Could not build pedigree for horse: %s\n", args[0])
		return
	}

	fmt.Println("=== PEDIGREE TREE ===")
	fmt.Print(pedigreussy.PedigreeToASCII(tree, 3))
}

// cmdDynasty displays dynasty info for a stable.
func cmdDynasty(state *cliState, args []string) {
	stableID := ""
	if len(args) > 0 {
		stableID = args[0]
	} else {
		// Use the first stable.
		stables := state.sm.ListStables()
		if len(stables) == 0 {
			fmt.Println("No stables found. Run 'seed' first.")
			return
		}
		stableID = stables[0].ID
	}

	info := pedigreussy.CalcDynastyScore(stableID, state.sm)

	fmt.Println("=== DYNASTY INFO ===")
	fmt.Printf("  Rating:            %s\n", info.DynastyRating)
	fmt.Printf("  Bloodline Strength: %.2f\n", info.BloodlineStrength)
	fmt.Printf("  Total Horses:      %d\n", info.TotalHorses)
	fmt.Printf("  Total Generations: %d\n", info.TotalGenerations)
	fmt.Printf("  Oldest Lineage:    Gen %d\n", info.OldestLineage)
	fmt.Printf("  Average ELO:       %.0f\n", info.AverageELO)
	fmt.Printf("  Legendary Count:   %d\n", info.LegendaryCount)
	fmt.Printf("  Best Horse:        %s\n", info.BestHorse)

	if len(info.FamousAncestors) > 0 {
		fmt.Printf("  Famous Ancestors:  %s\n", strings.Join(info.FamousAncestors, ", "))
	}
}

// cmdTraits shows a horse's traits with descriptions.
func cmdTraits(state *cliState, args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: traits <horseID>")
		return
	}

	h, err := state.sm.GetHorse(args[0])
	if err != nil {
		fmt.Printf("Horse not found: %s\n", args[0])
		return
	}

	fmt.Printf("=== TRAITS: %s ===\n", h.Name)
	if len(h.Traits) == 0 {
		fmt.Println("  No traits. Train or breed to acquire traits.")
		return
	}

	for _, t := range h.Traits {
		fmt.Printf("  [%s] %s\n", strings.ToUpper(t.Rarity), t.Name)
		fmt.Printf("    %s\n", t.Description)
		fmt.Printf("    Effect: %s (magnitude: %.2f)\n", t.Effect, t.Magnitude)
		fmt.Println()
	}
}

// ===========================================================================
// CLI Commands — Racing
// ===========================================================================

// cmdRace runs a race between the specified horse IDs.
func cmdRace(state *cliState, args []string) {
	if len(args) < 2 {
		fmt.Println("Usage: race <horseID1> <horseID2> [horseID3...]")
		return
	}

	horses := make([]*models.Horse, 0, len(args))
	for _, id := range args {
		h, err := state.sm.GetHorse(id)
		if err != nil {
			fmt.Printf("Horse not found: %s\n", id)
			return
		}
		if h.Retired {
			fmt.Printf("Horse %s is retired and cannot race.\n", h.Name)
			return
		}
		horses = append(horses, h)
	}

	runAndDisplayRace(state, horses, models.TrackMudussy)
}

// cmdQuickRace creates random horses and races them on a random track.
func cmdQuickRace(state *cliState) {
	// Ensure we have at least one stable.
	stables := state.sm.ListStables()
	var stableID string
	if len(stables) > 0 {
		stableID = stables[0].ID
	} else {
		s := state.sm.CreateStable(nameussy.GenerateStableName(), "cli-player")
		stableID = s.ID
		fmt.Printf("Created quick-race stable: %s\n", s.Name)
	}

	// Generate 4 random horses for the race.
	numHorses := 4
	horses := make([]*models.Horse, numHorses)
	for i := 0; i < numHorses; i++ {
		genome := genussy.RandomGenome()
		ceiling := genussy.CalcFitnessCeiling(genome)
		h := &models.Horse{
			ID:             generateID(),
			Name:           nameussy.GenerateName(),
			Genome:         genome,
			Generation:     0,
			Age:            0,
			FitnessCeiling: ceiling,
			CurrentFitness: ceiling * (0.6 + rand.Float64()*0.4), // 60-100% of ceiling
			ELO:            1200,
			CreatedAt:      now(),
		}
		_ = state.sm.AddHorseToStable(stableID, h)
		horses[i] = h

		// Persist newly generated horse to DB.
		state.persistHorse(h)
	}

	// Pick a random track.
	tracks := []models.TrackType{
		models.TrackSprintussy, models.TrackGrindussy, models.TrackMudussy,
		models.TrackThunderussy, models.TrackFrostussy, models.TrackHauntedussy,
	}
	track := tracks[rand.IntN(len(tracks))]

	fmt.Println("=== QUICK RACE ===")
	fmt.Println("Generating 4 random horses...")
	for _, h := range horses {
		fmt.Printf("  %-30s  Fitness: %.2f  Genome: %s\n",
			h.Name, h.CurrentFitness, genussy.GenomeToString(h.Genome))
	}
	fmt.Println()

	runAndDisplayRace(state, horses, track)
}

// cmdHistory shows race history for all or a specific horse.
func cmdHistory(state *cliState, args []string) {
	if len(args) > 0 {
		// History for a specific horse.
		horseID := args[0]
		h, err := state.sm.GetHorse(horseID)
		if err != nil {
			fmt.Printf("Horse not found: %s\n", horseID)
			return
		}

		results := state.raceHistory.GetHorseHistory(horseID)
		if len(results) == 0 {
			fmt.Printf("No race history for %s.\n", h.Name)
			return
		}

		fmt.Printf("=== RACE HISTORY: %s ===\n", h.Name)
		for _, r := range results {
			placeStr := ordinal(r.FinishPlace)
			fmt.Printf("  %s | %s %dm | %s | ELO: %.0f→%.0f | +%d Cummies | %s\n",
				placeStr, r.TrackType, r.Distance,
				r.FinalTime.Truncate(time.Millisecond),
				r.ELOBefore, r.ELOAfter, r.Earnings, r.Weather)
		}
		return
	}

	// Show recent results across all horses.
	results := state.raceHistory.GetRecentResults(20)
	if len(results) == 0 {
		fmt.Println("No race history yet. Run some races first!")
		return
	}

	fmt.Println("=== RECENT RACE HISTORY (last 20) ===")
	for _, r := range results {
		placeStr := ordinal(r.FinishPlace)
		fmt.Printf("  %-25s %s | %s %dm | ELO: %.0f→%.0f | +%d | %s\n",
			r.HorseName, placeStr, r.TrackType, r.Distance,
			r.ELOBefore, r.ELOAfter, r.Earnings, r.Weather)
	}
}

// cmdStats shows computed statistics for a specific horse.
func cmdStats(state *cliState, args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: stats <horseID>")
		return
	}

	h, err := state.sm.GetHorse(args[0])
	if err != nil {
		fmt.Printf("Horse not found: %s\n", args[0])
		return
	}

	stats := state.raceHistory.GetHorseStats(args[0])
	if stats.TotalRaces == 0 {
		fmt.Printf("No stats for %s — no recorded races.\n", h.Name)
		return
	}

	fmt.Printf("=== STATS: %s ===\n", h.Name)
	fmt.Printf("  Total Races:     %d\n", stats.TotalRaces)
	fmt.Printf("  Wins:            %d\n", stats.Wins)
	fmt.Printf("  Top 3 Finishes:  %d\n", stats.Places)
	fmt.Printf("  Win Rate:        %.1f%%\n", stats.WinRate*100)
	fmt.Printf("  Avg Finish:      %.1f\n", stats.AvgFinishPlace)
	fmt.Printf("  Best Time:       %s\n", stats.BestTime.Truncate(time.Millisecond))
	fmt.Printf("  Worst Time:      %s\n", stats.WorstTime.Truncate(time.Millisecond))
	fmt.Printf("  Favorite Track:  %s\n", stats.FavoriteTrack)
	fmt.Printf("  Total Earnings:  %d Cummies\n", stats.TotalEarnings)

	streakLabel := ""
	if stats.CurrentStreak > 0 {
		streakLabel = fmt.Sprintf("%d wins", stats.CurrentStreak)
	} else if stats.CurrentStreak < 0 {
		streakLabel = fmt.Sprintf("%d losses", -stats.CurrentStreak)
	} else {
		streakLabel = "none"
	}
	fmt.Printf("  Current Streak:  %s\n", streakLabel)
	fmt.Printf("  Best Win Streak: %d\n", stats.BestStreak)
}

// ===========================================================================
// CLI Commands — Training
// ===========================================================================

// cmdTrain trains a horse with the specified workout type.
func cmdTrain(state *cliState, args []string) {
	if len(args) < 2 {
		fmt.Println("Usage: train <horseID> <workout>")
		fmt.Println("  Workouts: Sprint, Endurance, MentalRep, MudRun, RestDay, General")
		return
	}

	h, err := state.sm.GetHorse(args[0])
	if err != nil {
		fmt.Printf("Horse not found: %s\n", args[0])
		return
	}

	if h.Retired {
		fmt.Printf("%s is retired and cannot train.\n", h.Name)
		return
	}

	workout := parseWorkoutType(args[1])
	if workout == "" {
		fmt.Printf("Unknown workout type: %s\n", args[1])
		fmt.Println("  Valid: Sprint, Endurance, MentalRep, MudRun, RestDay, General")
		return
	}

	// Pre-training fatigue warning
	if h.Fatigue > 70 {
		fmt.Printf("*** WARNING: %s has %.0f fatigue! Injury risk is elevated. ***\n", h.Name, h.Fatigue)
	}

	session := state.trainer.Train(h, workout)

	fmt.Printf("=== TRAINING: %s ===\n", h.Name)
	fmt.Printf("  Workout:    %s\n", session.WorkoutType)
	fmt.Printf("  XP Gained:  %.1f\n", session.XPGained)
	fmt.Printf("  Fitness:    %.4f → %.4f\n", session.FitnessBefore, session.FitnessAfter)
	fmt.Printf("  Fatigue:    %.0f/100\n", h.Fatigue)

	if session.Injury {
		fmt.Println()
		fmt.Println("  *** INJURY! ***")
		fmt.Printf("  >>> %s <<<\n", session.InjuryNote)
		fmt.Println("  Fatigue set to 100. Rest this horse!")
	}

	if h.Fatigue > 70 && !session.Injury {
		fmt.Println()
		fmt.Println("  Fatigue is HIGH. Consider a rest day to avoid injury.")
	}

	// Persist trained horse state to DB.
	state.persistHorse(h)
}

// cmdRest is a shortcut for training with RestDay.
func cmdRest(state *cliState, args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: rest <horseID>")
		return
	}

	h, err := state.sm.GetHorse(args[0])
	if err != nil {
		fmt.Printf("Horse not found: %s\n", args[0])
		return
	}

	if h.Retired {
		fmt.Printf("%s is retired.\n", h.Name)
		return
	}

	session := state.trainer.Train(h, models.WorkoutRecovery)

	fmt.Printf("=== REST DAY: %s ===\n", h.Name)
	fmt.Printf("  XP Gained:  %.1f\n", session.XPGained)
	fmt.Printf("  Fitness:    %.4f → %.4f\n", session.FitnessBefore, session.FitnessAfter)
	fmt.Printf("  Fatigue:    %.0f/100 (recovered!)\n", h.Fatigue)

	// Persist rested horse state to DB.
	state.persistHorse(h)
}

// parseWorkoutType converts a user string to a WorkoutType constant.
func parseWorkoutType(s string) models.WorkoutType {
	switch strings.ToLower(s) {
	case "sprint":
		return models.WorkoutSprint
	case "endurance":
		return models.WorkoutEndurance
	case "mentalrep", "mental":
		return models.WorkoutMentalRep
	case "mudrun", "mud":
		return models.WorkoutMudRun
	case "restday", "rest":
		return models.WorkoutRecovery
	case "general":
		return models.WorkoutGeneral
	default:
		return ""
	}
}

// ===========================================================================
// CLI Commands — Market & Trading
// ===========================================================================

// cmdMarket displays active stud market listings.
func cmdMarket(state *cliState) {
	listings := state.market.ListActiveListings()
	if len(listings) == 0 {
		fmt.Println("No active stud listings. The market is quiet.")
		fmt.Println("(In a full session, horses would be listed here for breeding.)")
		return
	}

	fmt.Println("=== STUD MARKET ===")
	fmt.Printf("  %-30s  %8s  %7s  %s\n", "Horse", "Price", "Sappho", "Pedigree")
	fmt.Println("  " + strings.Repeat("-", 80))

	for _, l := range listings {
		sapphoStr := fmt.Sprintf("%.1f", l.SapphoScore)
		if math.IsNaN(l.SapphoScore) {
			sapphoStr = "NaN"
		}
		fmt.Printf("  %-30s  %8d  %7s  %s\n",
			l.HorseName, l.Price, sapphoStr, l.Pedigree)
		fmt.Printf("    Listing ID: %s\n", l.ID)
	}

	fmt.Println()
	fmt.Printf("  Total Cummies burned: %d\n", state.market.GetTotalBurned())
}

// cmdOffer creates a trade offer for a horse.
func cmdOffer(state *cliState, args []string) {
	if len(args) < 3 {
		fmt.Println("Usage: offer <horseID> <toStableID> <price>")
		return
	}

	h, err := state.sm.GetHorse(args[0])
	if err != nil {
		fmt.Printf("Horse not found: %s\n", args[0])
		return
	}

	// Verify target stable exists.
	_, err = state.sm.GetStable(args[1])
	if err != nil {
		fmt.Printf("Target stable not found: %s\n", args[1])
		return
	}

	price, err := strconv.ParseInt(args[2], 10, 64)
	if err != nil || price <= 0 {
		fmt.Println("Price must be a positive number.")
		return
	}

	// Find the horse's owning stable.
	fromStable := findHorseStable(state, h.ID)
	if fromStable == "" {
		fmt.Println("Could not determine horse's stable.")
		return
	}

	offer := state.trades.CreateOffer(h.ID, h.Name, fromStable, args[1], price)
	fmt.Printf("Trade offer created!\n")
	fmt.Printf("  Offer ID:    %s\n", offer.ID)
	fmt.Printf("  Horse:       %s\n", offer.HorseName)
	fmt.Printf("  To Stable:   %s\n", offer.ToStableID)
	fmt.Printf("  Price:       %d Cummies\n", offer.Price)
	fmt.Printf("  Status:      %s\n", offer.Status)
}

// cmdOffers lists pending trade offers for a stable.
func cmdOffers(state *cliState, args []string) {
	stableID := ""
	if len(args) > 0 {
		stableID = args[0]
	} else {
		stables := state.sm.ListStables()
		if len(stables) == 0 {
			fmt.Println("No stables found. Run 'seed' first.")
			return
		}
		stableID = stables[0].ID
	}

	incoming := state.trades.ListPendingOffers(stableID)
	outgoing := state.trades.ListOutgoingOffers(stableID)

	if len(incoming) == 0 && len(outgoing) == 0 {
		fmt.Println("No pending trade offers.")
		return
	}

	if len(incoming) > 0 {
		fmt.Println("=== INCOMING OFFERS ===")
		for _, o := range incoming {
			fmt.Printf("  [%s] %s for %d Cummies from stable %s\n",
				o.ID, o.HorseName, o.Price, o.FromStableID)
		}
	}

	if len(outgoing) > 0 {
		fmt.Println("=== OUTGOING OFFERS ===")
		for _, o := range outgoing {
			fmt.Printf("  [%s] %s for %d Cummies to stable %s\n",
				o.ID, o.HorseName, o.Price, o.ToStableID)
		}
	}
}

// cmdAccept accepts a pending trade offer.
func cmdAccept(state *cliState, args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: accept <offerID>")
		return
	}

	offer, err := state.trades.AcceptOffer(args[0])
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// Transfer cummies from the accepting stable to the offering stable.
	transferErr := state.sm.TransferCummies(offer.ToStableID, offer.FromStableID, offer.Price)
	if transferErr != nil {
		fmt.Printf("Trade accepted but currency transfer failed: %v\n", transferErr)
		return
	}

	fmt.Printf("Trade accepted!\n")
	fmt.Printf("  Horse:       %s\n", offer.HorseName)
	fmt.Printf("  Price paid:  %d Cummies\n", offer.Price)

	// Persist updated stables after trade.
	if fromStable, err := state.sm.GetStable(offer.FromStableID); err == nil {
		state.persistStable(fromStable)
	}
	if toStable, err := state.sm.GetStable(offer.ToStableID); err == nil {
		state.persistStable(toStable)
	}
}

// cmdReject rejects a pending trade offer.
func cmdReject(state *cliState, args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: reject <offerID>")
		return
	}

	err := state.trades.RejectOffer(args[0])
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Println("Trade offer rejected.")
}

// findHorseStable returns the stable ID that contains the given horse.
func findHorseStable(state *cliState, horseID string) string {
	stables := state.sm.ListStables()
	for _, s := range stables {
		for _, h := range s.Horses {
			if h.ID == horseID {
				return s.ID
			}
		}
	}
	return ""
}

// ===========================================================================
// CLI Commands — Tournaments
// ===========================================================================

// cmdTournament dispatches tournament subcommands.
func cmdTournament(state *cliState, args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: tournament <create|list|register|run|standings> [args...]")
		return
	}

	subcmd := strings.ToLower(args[0])
	subargs := args[1:]

	switch subcmd {
	case "create":
		cmdTournamentCreate(state, subargs)
	case "list":
		cmdTournamentList(state)
	case "register":
		cmdTournamentRegister(state, subargs)
	case "run":
		cmdTournamentRun(state, subargs)
	case "standings":
		cmdTournamentStandings(state, subargs)
	default:
		fmt.Printf("Unknown tournament subcommand: %s\n", subcmd)
		fmt.Println("  Subcommands: create, list, register, run, standings")
	}
}

// cmdTournamentCreate creates a new tournament.
func cmdTournamentCreate(state *cliState, args []string) {
	name := ""
	if len(args) > 0 {
		name = strings.Join(args, " ")
	}

	// Auto-pick track type and rounds.
	tracks := []models.TrackType{
		models.TrackSprintussy, models.TrackGrindussy, models.TrackMudussy,
		models.TrackThunderussy, models.TrackFrostussy, models.TrackHauntedussy,
	}
	trackType := tracks[rand.IntN(len(tracks))]
	rounds := 3
	entryFee := int64(100)

	t := state.tournaments.CreateTournament(name, trackType, rounds, entryFee)

	fmt.Println("=== TOURNAMENT CREATED ===")
	fmt.Printf("  Name:       %s\n", t.Name)
	fmt.Printf("  ID:         %s\n", t.ID)
	fmt.Printf("  Track:      %s\n", t.TrackType)
	fmt.Printf("  Rounds:     %d\n", t.Rounds)
	fmt.Printf("  Entry Fee:  %d Cummies\n", t.EntryFee)
	fmt.Printf("  Prize Pool: %d Cummies\n", t.PrizePool)
	fmt.Printf("  Status:     %s\n", t.Status)
}

// cmdTournamentList lists all tournaments.
func cmdTournamentList(state *cliState) {
	list := state.tournaments.ListTournaments()
	if len(list) == 0 {
		fmt.Println("No tournaments. Create one with 'tournament create [name]'.")
		return
	}

	fmt.Println("=== TOURNAMENTS ===")
	for _, t := range list {
		fmt.Printf("  %-40s  %s  Round %d/%d  Entries: %d  Status: %s\n",
			t.Name, t.TrackType, t.CurrentRound, t.Rounds,
			len(t.Standings), t.Status)
		fmt.Printf("    ID: %s\n", t.ID)
	}
}

// cmdTournamentRegister registers a horse in a tournament.
func cmdTournamentRegister(state *cliState, args []string) {
	if len(args) < 2 {
		fmt.Println("Usage: tournament register <tournamentID> <horseID>")
		return
	}

	h, err := state.sm.GetHorse(args[1])
	if err != nil {
		fmt.Printf("Horse not found: %s\n", args[1])
		return
	}

	stableID := findHorseStable(state, h.ID)

	err = state.tournaments.RegisterHorse(args[0], h, stableID)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Registered %s in tournament.\n", h.Name)
}

// cmdTournamentRun runs the next round of a tournament.
func cmdTournamentRun(state *cliState, args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: tournament run <tournamentID>")
		return
	}

	tournID := args[0]
	t, err := state.tournaments.GetTournament(tournID)
	if err != nil {
		fmt.Printf("Tournament not found: %s\n", tournID)
		return
	}

	// Gather the horses for this tournament round.
	horses := make([]*models.Horse, 0, len(t.Standings))
	for _, entry := range t.Standings {
		h, hErr := state.sm.GetHorse(entry.HorseID)
		if hErr == nil {
			horses = append(horses, h)
		}
	}

	if len(horses) < 2 {
		fmt.Println("Not enough registered horses to run a round (need at least 2).")
		return
	}

	// Create the race for this round.
	race, rErr := state.tournaments.RunNextRound(tournID, horses)
	if rErr != nil {
		fmt.Printf("Error: %v\n", rErr)
		return
	}

	fmt.Printf("=== TOURNAMENT ROUND %d: %s ===\n", t.CurrentRound, t.Name)

	// Generate weather and simulate.
	weather := tournussy.RandomWeatherForTrack(t.TrackType)
	wEffects := tournussy.WeatherEffects(weather)

	fmt.Printf("WEATHER: %s — %s\n", weather, wEffects.Description)
	fmt.Printf("Track: %s (%d m)  |  Purse: %d Cummies  |  Entrants: %d\n",
		race.TrackType, race.Distance, race.Purse, len(race.Entries))
	fmt.Println()
	fmt.Println("And they're off!")
	fmt.Println(strings.Repeat("-", 60))

	// Simulate the race with weather.
	race = racussy.SimulateRaceWithWeather(race, horses, weather)

	// Generate and display narrative.
	narrative := racussy.GenerateRaceNarrativeWithWeather(race, weather)
	for _, line := range narrative {
		fmt.Println(line)
	}

	// Update ELO and stats.
	updatePostRaceStats(state, race, horses, weather)

	// Record tournament results.
	_ = state.tournaments.RecordRoundResults(tournID, race)

	fmt.Println()
	fmt.Printf("Tournament round complete.\n")

	// Refresh tournament status.
	t, _ = state.tournaments.GetTournament(tournID)
	if t != nil && t.Status == "Finished" {
		fmt.Println("*** TOURNAMENT FINISHED! ***")
		standings := state.tournaments.GetStandings(tournID)
		if len(standings) > 0 {
			fmt.Printf("  Champion: %s with %d points!\n", standings[0].HorseName, standings[0].Points)
		}
	}
}

// cmdTournamentStandings shows tournament standings.
func cmdTournamentStandings(state *cliState, args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: tournament standings <tournamentID>")
		return
	}

	standings := state.tournaments.GetStandings(args[0])
	if standings == nil {
		fmt.Printf("Tournament not found: %s\n", args[0])
		return
	}

	if len(standings) == 0 {
		fmt.Println("No entries in this tournament.")
		return
	}

	t, _ := state.tournaments.GetTournament(args[0])
	name := args[0]
	if t != nil {
		name = t.Name
	}

	fmt.Printf("=== STANDINGS: %s ===\n", name)
	fmt.Printf("  %-4s  %-30s  %6s  %6s  %5s\n", "Rank", "Horse", "Points", "Races", "Best")
	fmt.Println("  " + strings.Repeat("-", 60))

	for i, s := range standings {
		bestStr := "-"
		if s.BestPlace > 0 {
			bestStr = ordinal(s.BestPlace)
		}
		fmt.Printf("  %-4d  %-30s  %6d  %6d  %5s\n",
			i+1, s.HorseName, s.Points, s.RacesRun, bestStr)
	}
}

// ===========================================================================
// CLI Commands — Progression
// ===========================================================================

// cmdAdvance advances one season: ages all horses, checks retirements,
// prints a summary.
func cmdAdvance(state *cliState) {
	state.season++
	fmt.Printf("=== ADVANCING TO SEASON %d ===\n", state.season)
	fmt.Println()

	stables := state.sm.ListStables()
	for _, s := range stables {
		horses := state.sm.ListHorses(s.ID)
		for _, h := range horses {
			if h.Retired {
				continue
			}

			oldAge := h.Age
			oldCeiling := h.FitnessCeiling

			// Age the horse.
			trainussy.AgeHorse(h)

			fmt.Printf("  %-30s  Age: %d → %d (%s)",
				h.Name, oldAge, h.Age, trainussy.LifeStage(h))

			// Check fitness ceiling change.
			ceilingDelta := h.FitnessCeiling - oldCeiling
			if math.Abs(ceilingDelta) > 0.0001 {
				sign := "+"
				if ceilingDelta < 0 {
					sign = ""
				}
				fmt.Printf("  Ceiling: %s%.4f", sign, ceilingDelta)
			}
			fmt.Println()

			// Check if the horse was retired by the aging process.
			if h.Retired {
				msg := "Age caught up"
				fmt.Printf("    *** RETIRED: %s — %s ***\n", h.Name, msg)
				state.persistHorse(h)
				continue
			}

			// Check if the horse should retire from other conditions.
			shouldRetire, reason := trainussy.ShouldRetire(h)
			if shouldRetire {
				trainussy.RetireHorse(h, reason)
				fmt.Printf("    *** RETIRED: %s — %s ***\n", h.Name, reason)
			}

			// Persist aged horse state to DB.
			state.persistHorse(h)
		}
	}

	fmt.Println()
	fmt.Printf("Season %d complete.\n", state.season)
}

// cmdAchievements shows all unlocked achievements for a stable.
func cmdAchievements(state *cliState, args []string) {
	stableID := ""
	if len(args) > 0 {
		stableID = args[0]
	} else {
		stables := state.sm.ListStables()
		if len(stables) == 0 {
			fmt.Println("No stables found. Run 'seed' first.")
			return
		}
		stableID = stables[0].ID
	}

	stable, err := state.sm.GetStable(stableID)
	if err != nil {
		fmt.Printf("Stable not found: %s\n", stableID)
		return
	}

	fmt.Printf("=== ACHIEVEMENTS: %s ===\n", stable.Name)
	if len(stable.Achievements) == 0 {
		fmt.Println("  No achievements unlocked yet. Keep racing!")
		return
	}

	for _, a := range stable.Achievements {
		fmt.Printf("  [%s] %s — %s\n", strings.ToUpper(a.Rarity), a.Name, a.Description)
	}
}

// cmdLeaderboard displays the ELO leaderboard.
func cmdLeaderboard(state *cliState) {
	board := state.sm.GetLeaderboard()
	if len(board) == 0 {
		fmt.Println("No horses registered yet. Run 'seed' first.")
		return
	}

	fmt.Println("=== ELO LEADERBOARD ===")
	fmt.Printf("  %-4s  %-30s  %6s  %4s  %4s  %5s\n",
		"Rank", "Name", "ELO", "W", "L", "Races")
	fmt.Println("  " + strings.Repeat("-", 70))

	for i, h := range board {
		legendary := ""
		if h.IsLegendary {
			legendary = " *"
		}
		retired := ""
		if h.Retired {
			retired = " [R]"
		}
		fmt.Printf("  %-4d  %-30s  %6.0f  %4d  %4d  %5d%s%s\n",
			i+1, h.Name, h.ELO, h.Wins, h.Losses, h.Races, legendary, retired)
	}

	fmt.Println()
	fmt.Println("  * = Legendary horse   [R] = Retired")
}

// ===========================================================================
// Race execution & display — enhanced with weather, purse, fatigue, achievements
// ===========================================================================

// runAndDisplayRace creates, simulates, and displays a race with weather,
// narrative, purse distribution, post-race fatigue, and achievements.
func runAndDisplayRace(state *cliState, horses []*models.Horse, trackType models.TrackType) {
	purse := int64(1000)
	race := racussy.NewRace(horses, trackType, purse)

	// Generate random weather for the track.
	weather := tournussy.RandomWeatherForTrack(trackType)
	wEffects := tournussy.WeatherEffects(weather)

	// Display weather with flavor text.
	fmt.Printf("WEATHER: %s — %s\n", weather, wEffects.Description)
	fmt.Printf("Track: %s (%d m)  |  Purse: %d Cummies  |  Entrants: %d\n",
		race.TrackType, race.Distance, race.Purse, len(race.Entries))
	fmt.Println()
	fmt.Println("And they're off!")
	fmt.Println(strings.Repeat("-", 60))

	// Simulate the race with weather.
	race = racussy.SimulateRaceWithWeather(race, horses, weather)

	// Generate and display narrative with weather context.
	narrative := racussy.GenerateRaceNarrativeWithWeather(race, weather)
	for _, line := range narrative {
		fmt.Println(line)
	}

	// --- Purse distribution ---
	fmt.Println()
	fmt.Println("=== PURSE DISTRIBUTION ===")
	purseSplit := calcPurseSplit(purse, len(horses))
	for place, amount := range purseSplit {
		fmt.Printf("  %s: +%d Cummies\n", ordinal(place+1), amount)
	}

	// Update ELO and stats for all finishers (with accumulated ELO delta — BUG 7 fix).
	updatePostRaceStats(state, race, horses, weather)

	// --- Post-race fatigue ---
	fmt.Println()
	fmt.Println("=== POST-RACE FATIGUE ===")
	for _, entry := range race.Entries {
		h, err := state.sm.GetHorse(entry.HorseID)
		if err != nil {
			continue
		}
		fatigue := racussy.CalcPostRaceFatigue(h, race, entry.FinishPlace, weather)
		h.Fatigue += fatigue
		if h.Fatigue > 100 {
			h.Fatigue = 100
		}
		fmt.Printf("  %-30s  +%.0f fatigue → %.0f/100\n", h.Name, fatigue, h.Fatigue)

		// Persist horse after fatigue update.
		state.persistHorse(h)
	}

	// --- Achievement check ---
	newAchievements := checkRaceAchievements(state, race, horses)
	if len(newAchievements) > 0 {
		fmt.Println()
		fmt.Println("=== ACHIEVEMENTS UNLOCKED ===")
		for _, a := range newAchievements {
			fmt.Printf("  [%s] %s — %s\n", strings.ToUpper(a.Rarity), a.Name, a.Description)
		}
	}

	fmt.Println()
	fmt.Printf("Race complete. Purse of %d Cummies awarded.\n", race.Purse)
}

// calcPurseSplit distributes a purse among the top 3 finishers.
// 1st: 50%, 2nd: 30%, 3rd: 20%. If fewer than 3 horses, adjust.
func calcPurseSplit(purse int64, numHorses int) []int64 {
	if numHorses <= 0 {
		return nil
	}

	splits := []int64{}
	switch {
	case numHorses >= 3:
		splits = append(splits, purse*50/100) // 1st
		splits = append(splits, purse*30/100) // 2nd
		splits = append(splits, purse*20/100) // 3rd
	case numHorses == 2:
		splits = append(splits, purse*70/100) // 1st
		splits = append(splits, purse*30/100) // 2nd
	default:
		splits = append(splits, purse) // 1st takes all
	}

	return splits
}

// updatePostRaceStats updates ELO, wins, losses, race counts, earnings, and
// peak ELO after a race. Uses accumulated ELO deltas (BUG 7 fix) to avoid
// compounding pairwise ELO updates.
func updatePostRaceStats(state *cliState, race *models.Race, horses []*models.Horse, weather models.Weather) {
	// Build a map for quick horse lookup.
	horseMap := make(map[string]*models.Horse, len(horses))
	for _, h := range horses {
		horseMap[h.ID] = h
	}

	// Sort entries by finish place for pairwise ELO updates.
	type entryInfo struct {
		entry *models.RaceEntry
		horse *models.Horse
	}
	entries := make([]entryInfo, 0, len(race.Entries))
	for i := range race.Entries {
		e := &race.Entries[i]
		h, ok := horseMap[e.HorseID]
		if !ok {
			continue
		}
		entries = append(entries, entryInfo{entry: e, horse: h})
	}

	// BUG 7 Fix: Accumulate ELO deltas, then apply all at once.
	// This prevents compounding from sequential pairwise updates.
	eloBefore := make(map[string]float64, len(entries))
	eloDelta := make(map[string]float64, len(entries))
	for _, ei := range entries {
		eloBefore[ei.horse.ID] = ei.horse.ELO
		eloDelta[ei.horse.ID] = 0
	}

	// Pairwise ELO calculation using ORIGINAL ratings for expected score.
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[i].entry.FinishPlace < entries[j].entry.FinishPlace {
				wID := entries[i].horse.ID
				lID := entries[j].horse.ID

				// Calculate expected scores from original ELOs.
				wELO := eloBefore[wID]
				lELO := eloBefore[lID]

				expW := 1.0 / (1.0 + math.Pow(10.0, (lELO-wELO)/400.0))
				expL := 1.0 / (1.0 + math.Pow(10.0, (wELO-lELO)/400.0))

				k := 32.0
				eloDelta[wID] += k * (1.0 - expW)
				eloDelta[lID] += k * (0.0 - expL)
			}
		}
	}

	// Apply accumulated deltas.
	// Trait: elo_boost (e.g. ELO Farmer) — multiply positive ELO gains by magnitude.
	for _, ei := range entries {
		delta := eloDelta[ei.horse.ID]
		if delta > 0 {
			if has, mag := hasTraitEffect(ei.horse, "elo_boost"); has {
				delta *= mag
			}
		}
		ei.horse.ELO = eloBefore[ei.horse.ID] + delta
	}

	// Distribute purse earnings.
	purseSplit := calcPurseSplit(race.Purse, len(entries))

	// Update stats in the stable manager and record race history.
	for _, ei := range entries {
		wins := 0
		losses := 0
		if ei.entry.FinishPlace == 1 {
			wins = 1
		} else {
			losses = 1
		}

		// Calculate earnings for this horse.
		var earnings int64
		placeIdx := ei.entry.FinishPlace - 1
		if placeIdx >= 0 && placeIdx < len(purseSplit) {
			earnings = purseSplit[placeIdx]
		}

		// Trait: earnings_boost (e.g. Cummies Magnet) — multiply earnings by magnitude.
		if earnings > 0 {
			if has, mag := hasTraitEffect(ei.horse, "earnings_boost"); has {
				earnings = int64(float64(earnings) * mag)
			}
		}

		ei.horse.TotalEarnings += earnings

		// Update peak ELO.
		if ei.horse.ELO > ei.horse.PeakELO {
			ei.horse.PeakELO = ei.horse.ELO
		}

		_ = state.sm.UpdateHorseStats(ei.horse.ID, wins, losses, 1, ei.horse.ELO)

		// Record in race history.
		result := &models.RaceResult{
			RaceID:      race.ID,
			HorseID:     ei.horse.ID,
			HorseName:   ei.entry.HorseName,
			TrackType:   race.TrackType,
			Distance:    race.Distance,
			FinishPlace: ei.entry.FinishPlace,
			TotalHorses: len(entries),
			FinalTime:   ei.entry.FinalTime,
			ELOBefore:   eloBefore[ei.horse.ID],
			ELOAfter:    ei.horse.ELO,
			Earnings:    earnings,
			Weather:     string(weather),
			CreatedAt:   time.Now(),
		}
		state.raceHistory.RecordResult(result)

		// Persist horse state and race result to DB.
		state.persistHorse(ei.horse)
		state.persistRaceResult(result)
	}
}

// checkRaceAchievements evaluates achievements after a race for all participating
// horses and returns any newly unlocked ones.
func checkRaceAchievements(state *cliState, race *models.Race, horses []*models.Horse) []models.Achievement {
	var allUnlocked []models.Achievement

	for _, h := range horses {
		stableID := findHorseStable(state, h.ID)
		if stableID == "" {
			continue
		}

		stable, err := state.sm.GetStable(stableID)
		if err != nil {
			continue
		}

		newAchs := tournussy.CheckAchievements(h, state.raceHistory, stable)
		for _, a := range newAchs {
			// Add to the stable's achievements.
			stable.Achievements = append(stable.Achievements, a)
			allUnlocked = append(allUnlocked, a)

			// Persist new achievement to DB.
			state.persistAchievement(stableID, &a)
		}
	}

	return allUnlocked
}

// ===========================================================================
// Trait helpers
// ===========================================================================

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

// ===========================================================================
// Utility helpers
// ===========================================================================

// generateID creates a simple unique ID for quick-race horses.
// Uses a combination of timestamp and random suffix.
func generateID() string {
	return fmt.Sprintf("%x-%04x", time.Now().UnixNano(), rand.IntN(0xFFFF))
}

// now returns the current time. Extracted for potential test mocking.
func now() time.Time {
	return time.Now()
}

// ordinal converts a placement number to its ordinal string (1st, 2nd, ...).
func ordinal(n int) string {
	suffix := "th"
	switch n % 100 {
	case 11, 12, 13:
		// Special cases: 11th, 12th, 13th always end in "th".
	default:
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%d%s", n, suffix)
}
