// Package server implements the HTTP API server for StallionUSSY.
// It wires together all subsystems (stables, market, genetics, racing,
// training, tournaments, pedigree, trading, WebSocket telemetry) behind
// a JSON REST API using only the Go standard library net/http (plus
// gorilla/websocket for the WS endpoint).
package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/mojomast/stallionussy/internal/commussy"
	"github.com/mojomast/stallionussy/internal/genussy"
	"github.com/mojomast/stallionussy/internal/marketussy"
	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/pedigreussy"
	"github.com/mojomast/stallionussy/internal/racussy"
	"github.com/mojomast/stallionussy/internal/stableussy"
	"github.com/mojomast/stallionussy/internal/tournussy"
	"github.com/mojomast/stallionussy/internal/trainussy"
)

// ---------------------------------------------------------------------------
// Server — the main HTTP API server
// ---------------------------------------------------------------------------

// Server holds all subsystem references and the HTTP mux.
type Server struct {
	stables     *stableussy.StableManager
	market      *marketussy.Market
	hub         *commussy.Hub
	trainer     *trainussy.Trainer
	tournaments *tournussy.TournamentManager
	raceHistory *tournussy.RaceHistory
	pedigree    *pedigreussy.PedigreeEngine
	trades      *pedigreussy.TradeManager
	mux         *http.ServeMux
}

// NewServer initializes all subsystems, seeds the legendary horses into a
// "House of USSY" stable, registers all routes, and returns a ready Server.
func NewServer() *Server {
	sm := stableussy.NewStableManager()
	rh := tournussy.NewRaceHistory()

	s := &Server{
		stables:     sm,
		market:      marketussy.NewMarket(),
		hub:         commussy.NewHub(),
		trainer:     trainussy.NewTrainer(),
		tournaments: tournussy.NewTournamentManager(rh),
		raceHistory: rh,
		pedigree:    pedigreussy.NewPedigreeEngine(sm.GetHorse),
		trades:      pedigreussy.NewTradeManager(),
		mux:         http.NewServeMux(),
	}

	// Start the WebSocket hub event loop.
	go s.hub.Run()

	// Seed the canonical legendary horses into the "House of USSY" stable.
	houseOfUSSY := s.stables.CreateStable("House of USSY", "system")
	s.stables.SeedLegendaries(houseOfUSSY.ID)

	// Assign traits to each legendary horse.
	legendaryHorses := s.stables.ListHorses(houseOfUSSY.ID)
	for _, h := range legendaryHorses {
		// Legendaries are founders so parents are nil — AssignTraitsAtBirth
		// handles nil parents gracefully (no parental inheritance, no
		// anomalous/legendary eligibility from parents — but we pass the
		// horse itself as a "sire" for legendary eligibility check since
		// the horse IS legendary).
		s.trainer.AssignTraitsAtBirth(h, h, nil)
	}

	log.Printf("server: seeded 12 legendary horses into stable %q (%s)", houseOfUSSY.Name, houseOfUSSY.ID)

	// Register all routes.
	s.routes()

	return s
}

// ---------------------------------------------------------------------------
// Route registration
// ---------------------------------------------------------------------------

func (s *Server) routes() {
	// --- Stables ---
	s.mux.HandleFunc("GET /api/stables", s.handleListStables)
	s.mux.HandleFunc("POST /api/stables", s.handleCreateStable)
	s.mux.HandleFunc("GET /api/stables/{id}", s.handleGetStable)
	s.mux.HandleFunc("GET /api/stables/{id}/horses", s.handleListStableHorses)
	s.mux.HandleFunc("GET /api/stables/{id}/achievements", s.handleGetStableAchievements)

	// --- Horses ---
	s.mux.HandleFunc("GET /api/horses", s.handleListHorses)
	s.mux.HandleFunc("GET /api/horses/{id}", s.handleGetHorse)
	s.mux.HandleFunc("GET /api/horses/{id}/history", s.handleGetHorseHistory)
	s.mux.HandleFunc("GET /api/horses/{id}/stats", s.handleGetHorseStats)
	s.mux.HandleFunc("GET /api/horses/{id}/achievements", s.handleCheckHorseAchievements)

	// --- Training ---
	s.mux.HandleFunc("POST /api/horses/{id}/train", s.handleTrainHorse)
	s.mux.HandleFunc("GET /api/horses/{id}/training", s.handleGetTrainingHistory)
	s.mux.HandleFunc("POST /api/horses/{id}/rest", s.handleRestHorse)

	// --- Pedigree ---
	s.mux.HandleFunc("GET /api/horses/{id}/pedigree", s.handleGetPedigree)
	s.mux.HandleFunc("GET /api/horses/{id}/pedigree/ascii", s.handleGetPedigreeASCII)
	s.mux.HandleFunc("GET /api/horses/{id}/dynasty", s.handleGetDynasty)

	// --- Breeding ---
	s.mux.HandleFunc("POST /api/breed", s.handleBreed)

	// --- Racing ---
	s.mux.HandleFunc("POST /api/races", s.handleCreateRace)
	s.mux.HandleFunc("GET /api/races/quick", s.handleQuickRace)

	// --- Race History ---
	s.mux.HandleFunc("GET /api/history", s.handleGetRaceHistory)

	// --- Market ---
	s.mux.HandleFunc("GET /api/market", s.handleListMarket)
	s.mux.HandleFunc("POST /api/market", s.handleCreateListing)
	s.mux.HandleFunc("POST /api/market/{id}/buy", s.handleBuyListing)
	s.mux.HandleFunc("DELETE /api/market/{id}", s.handleDelistListing)

	// --- Tournaments ---
	s.mux.HandleFunc("GET /api/tournaments", s.handleListTournaments)
	s.mux.HandleFunc("POST /api/tournaments", s.handleCreateTournament)
	s.mux.HandleFunc("GET /api/tournaments/{id}", s.handleGetTournament)
	s.mux.HandleFunc("POST /api/tournaments/{id}/register", s.handleRegisterTournament)
	s.mux.HandleFunc("POST /api/tournaments/{id}/race", s.handleTournamentRace)

	// --- Trading ---
	s.mux.HandleFunc("GET /api/trades", s.handleListTrades)
	s.mux.HandleFunc("POST /api/trades", s.handleCreateTrade)
	s.mux.HandleFunc("POST /api/trades/{id}/accept", s.handleAcceptTrade)
	s.mux.HandleFunc("POST /api/trades/{id}/reject", s.handleRejectTrade)
	s.mux.HandleFunc("DELETE /api/trades/{id}", s.handleCancelTrade)

	// --- Season / Aging ---
	s.mux.HandleFunc("POST /api/advance-season", s.handleAdvanceSeason)

	// --- Weather ---
	s.mux.HandleFunc("GET /api/weather", s.handleGetWeather)

	// --- WebSocket ---
	s.mux.HandleFunc("GET /ws", s.handleWebSocket)

	// --- Static files (frontend) ---
	webDir := "web"
	if info, err := os.Stat(webDir); err == nil && info.IsDir() {
		fileServer := http.FileServer(http.Dir(webDir))
		s.mux.Handle("GET /", fileServer)
	} else {
		s.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{
				"app":     "StallionUSSY",
				"status":  "running",
				"message": "The stallions are ready. Connect to /ws for live race telemetry.",
			})
		})
	}
}

// ---------------------------------------------------------------------------
// Start — launches the HTTP server
// ---------------------------------------------------------------------------

// Start begins listening on the given address (e.g. ":8080") and serves
// HTTP requests. It blocks until the server errors or is shut down.
func (s *Server) Start(addr string) error {
	handler := enableCORS(loggingMiddleware(s.mux))
	log.Printf("server: StallionUSSY listening on %s", addr)
	return http.ListenAndServe(addr, handler)
}

// ===========================================================================
// Stable handlers
// ===========================================================================

func (s *Server) handleListStables(w http.ResponseWriter, r *http.Request) {
	stables := s.stables.ListStables()
	writeJSON(w, http.StatusOK, stables)
}

func (s *Server) handleCreateStable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		OwnerID string `json:"ownerID"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	// FIX: default ownerID to "player" if empty.
	if req.OwnerID == "" {
		req.OwnerID = "player"
	}

	stable := s.stables.CreateStable(req.Name, req.OwnerID)
	writeJSON(w, http.StatusCreated, stable)
}

func (s *Server) handleGetStable(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	stable, err := s.stables.GetStable(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stable)
}

func (s *Server) handleListStableHorses(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if _, err := s.stables.GetStable(id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	horses := s.stables.ListHorses(id)
	if horses == nil {
		horses = []*models.Horse{}
	}
	writeJSON(w, http.StatusOK, horses)
}

func (s *Server) handleGetStableAchievements(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	stable, err := s.stables.GetStable(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	achievements := stable.Achievements
	if achievements == nil {
		achievements = []models.Achievement{}
	}
	writeJSON(w, http.StatusOK, achievements)
}

// ===========================================================================
// Horse handlers
// ===========================================================================

func (s *Server) handleListHorses(w http.ResponseWriter, r *http.Request) {
	// Leaderboard: all horses sorted by ELO descending.
	horses := s.stables.GetLeaderboard()
	writeJSON(w, http.StatusOK, horses)
}

func (s *Server) handleGetHorse(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	horse, err := s.stables.GetHorse(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, horse)
}

// ===========================================================================
// Training handlers
// ===========================================================================

func (s *Server) handleTrainHorse(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	horse, err := s.stables.GetHorse(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "horse not found: "+err.Error())
		return
	}

	if horse.Retired {
		writeError(w, http.StatusBadRequest, "horse is retired and cannot train")
		return
	}

	var req struct {
		WorkoutType models.WorkoutType `json:"workoutType"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	// Validate workout type.
	validWorkouts := map[models.WorkoutType]bool{
		models.WorkoutSprint:    true,
		models.WorkoutEndurance: true,
		models.WorkoutMentalRep: true,
		models.WorkoutMudRun:    true,
		models.WorkoutRecovery:  true,
		models.WorkoutGeneral:   true,
	}
	if !validWorkouts[req.WorkoutType] {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid workoutType: %q (valid: Sprint, Endurance, MentalRep, MudRun, RestDay, General)", req.WorkoutType))
		return
	}

	session := s.trainer.Train(horse, req.WorkoutType)

	// Sync horse state back to stable.
	s.syncHorseToStable(horse)

	writeJSON(w, http.StatusCreated, session)
}

func (s *Server) handleGetTrainingHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Verify horse exists.
	if _, err := s.stables.GetHorse(id); err != nil {
		writeError(w, http.StatusNotFound, "horse not found: "+err.Error())
		return
	}

	sessions := s.trainer.GetTrainingHistory(id)
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) handleRestHorse(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	horse, err := s.stables.GetHorse(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "horse not found: "+err.Error())
		return
	}

	if horse.Retired {
		writeError(w, http.StatusBadRequest, "horse is retired")
		return
	}

	// Run a RestDay workout which reduces fatigue by 30.
	session := s.trainer.Train(horse, models.WorkoutRecovery)
	s.syncHorseToStable(horse)

	writeJSON(w, http.StatusCreated, session)
}

// ===========================================================================
// Pedigree handlers
// ===========================================================================

func (s *Server) handleGetPedigree(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	depth := 3
	if depthStr := r.URL.Query().Get("depth"); depthStr != "" {
		if d, err := strconv.Atoi(depthStr); err == nil && d > 0 && d <= 10 {
			depth = d
		}
	}

	tree, err := s.pedigree.BuildPedigree(id, depth)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build pedigree: "+err.Error())
		return
	}
	if tree == nil {
		writeError(w, http.StatusNotFound, "horse not found: "+id)
		return
	}

	// Also compute inbreeding coefficient.
	inbreeding, _ := s.pedigree.CalcInbreedingCoefficient(id)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"pedigree":              tree,
		"inbreedingCoefficient": inbreeding,
	})
}

func (s *Server) handleGetPedigreeASCII(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	depth := 3
	if depthStr := r.URL.Query().Get("depth"); depthStr != "" {
		if d, err := strconv.Atoi(depthStr); err == nil && d > 0 && d <= 10 {
			depth = d
		}
	}

	tree, err := s.pedigree.BuildPedigree(id, depth)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build pedigree: "+err.Error())
		return
	}
	if tree == nil {
		writeError(w, http.StatusNotFound, "horse not found: "+id)
		return
	}

	ascii := pedigreussy.PedigreeToASCII(tree, depth)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ascii)
}

func (s *Server) handleGetDynasty(w http.ResponseWriter, r *http.Request) {
	horseID := r.PathValue("id")

	// Find which stable this horse belongs to.
	horse, err := s.stables.GetHorse(horseID)
	if err != nil {
		writeError(w, http.StatusNotFound, "horse not found: "+err.Error())
		return
	}

	// Find the stable that owns this horse.
	stableID := ""
	for _, stable := range s.stables.ListStables() {
		for _, h := range stable.Horses {
			if h.ID == horse.ID {
				stableID = stable.ID
				break
			}
		}
		if stableID != "" {
			break
		}
	}

	if stableID == "" {
		writeError(w, http.StatusNotFound, "horse's stable not found")
		return
	}

	dynasty := pedigreussy.CalcDynastyScore(stableID, s.stables)
	writeJSON(w, http.StatusOK, dynasty)
}

// ===========================================================================
// Breeding handler
// ===========================================================================

func (s *Server) handleBreed(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SireID   string `json:"sireID"`
		MareID   string `json:"mareID"`
		StableID string `json:"stableID"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.SireID == "" || req.MareID == "" || req.StableID == "" {
		writeError(w, http.StatusBadRequest, "sireID, mareID, and stableID are all required")
		return
	}

	// Look up both parents.
	sire, err := s.stables.GetHorse(req.SireID)
	if err != nil {
		writeError(w, http.StatusNotFound, "sire not found: "+err.Error())
		return
	}
	mare, err := s.stables.GetHorse(req.MareID)
	if err != nil {
		writeError(w, http.StatusNotFound, "mare not found: "+err.Error())
		return
	}

	// Breed!
	foal := genussy.Breed(sire, mare)

	// Calculate inbreeding coefficient for the foal.
	// We need to temporarily register the foal to compute its inbreeding,
	// but we can also compute it from its parents' lineages.
	inbreeding := 0.0

	// Apply bloodline bonus via pedigreussy.
	bloodlineBonus := pedigreussy.CalcBloodlineBonus(foal, sire.ID, mare.ID, s.stables.GetHorse)

	// Apply bloodline bonus to fitness ceiling.
	foal.FitnessCeiling *= bloodlineBonus

	// Apply inbreeding penalty to foal's fitness ceiling.
	inbreedingPenalty := pedigreussy.InbreedingPenalty(inbreeding)
	foal.FitnessCeiling *= inbreedingPenalty

	// Cap fitness ceiling.
	if foal.FitnessCeiling > 1.0 {
		foal.FitnessCeiling = 1.0
	}

	// Recompute current fitness (starts untrained at 50% of ceiling).
	foal.CurrentFitness = foal.FitnessCeiling * 0.5

	// Assign traits based on parentage.
	s.trainer.AssignTraitsAtBirth(foal, sire, mare)

	// Add the foal to the target stable.
	if err := s.stables.AddHorseToStable(req.StableID, foal); err != nil {
		writeError(w, http.StatusBadRequest, "failed to add foal to stable: "+err.Error())
		return
	}

	log.Printf("server: bred foal %q (%s) from sire %q and mare %q into stable %s (bloodline: %.4f)",
		foal.Name, foal.ID, sire.Name, mare.Name, req.StableID, bloodlineBonus)

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"foal":                  foal,
		"bloodlineBonus":        bloodlineBonus,
		"inbreedingCoefficient": inbreeding,
		"inbreedingPenalty":     inbreedingPenalty,
	})
}

// ===========================================================================
// Race handlers
// ===========================================================================

func (s *Server) handleCreateRace(w http.ResponseWriter, r *http.Request) {
	var req struct {
		HorseIDs  []string         `json:"horseIDs"`
		TrackType models.TrackType `json:"trackType"`
		Purse     int64            `json:"purse"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if len(req.HorseIDs) < 2 {
		writeError(w, http.StatusBadRequest, "at least 2 horses are required to race")
		return
	}

	// Validate track type.
	if models.TrackDistance(req.TrackType) == 0 {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid track type: %q (valid: Sprintussy, Grindussy, Mudussy, Thunderussy, Frostussy, Hauntedussy)", req.TrackType))
		return
	}

	// Resolve horse IDs to horse objects.
	horses, errMsg := s.resolveHorses(req.HorseIDs)
	if errMsg != "" {
		writeError(w, http.StatusNotFound, errMsg)
		return
	}

	// Run the race with weather and all post-race processing.
	result := s.runRace(horses, req.TrackType, req.Purse)
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) handleQuickRace(w http.ResponseWriter, r *http.Request) {
	// Get all horses from the leaderboard (excluding retired ones).
	allHorses := s.stables.GetLeaderboard()
	var eligible []*models.Horse
	for _, h := range allHorses {
		if !h.Retired {
			eligible = append(eligible, h)
		}
	}

	if len(eligible) < 2 {
		writeError(w, http.StatusBadRequest, "not enough non-retired horses to run a race (need at least 2)")
		return
	}

	// Pick 4-8 horses randomly.
	count := 4 + rand.IntN(5) // [4, 8]
	if count > len(eligible) {
		count = len(eligible)
	}

	// Shuffle and take the first `count`.
	shuffled := make([]*models.Horse, len(eligible))
	copy(shuffled, eligible)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	selected := shuffled[:count]

	// Pick a random track type.
	tracks := []models.TrackType{
		models.TrackSprintussy, models.TrackGrindussy, models.TrackMudussy,
		models.TrackThunderussy, models.TrackFrostussy, models.TrackHauntedussy,
	}
	trackType := tracks[rand.IntN(len(tracks))]

	// Default purse for quick races.
	purse := int64(500)

	result := s.runRace(selected, trackType, purse)
	writeJSON(w, http.StatusOK, result)
}

// resolveHorses looks up a slice of horse IDs and returns the horse objects.
// Returns an error message string if any horse is not found.
func (s *Server) resolveHorses(ids []string) ([]*models.Horse, string) {
	horses := make([]*models.Horse, 0, len(ids))
	for _, id := range ids {
		h, err := s.stables.GetHorse(id)
		if err != nil {
			return nil, fmt.Sprintf("horse not found: %s", id)
		}
		if h.Retired {
			return nil, fmt.Sprintf("horse %s (%s) is retired and cannot race", h.Name, h.ID)
		}
		horses = append(horses, h)
	}
	return horses, ""
}

// raceResult is the JSON response for race endpoints.
type raceResult struct {
	Race             *models.Race            `json:"race"`
	Narrative        []string                `json:"narrative"`
	NarrativeIndexed []racussy.NarrativeLine `json:"narrative_indexed"`
	Weather          models.Weather          `json:"weather"`
}

// runRace creates a race, simulates it with weather, updates stats, records
// history, distributes purse, applies fatigue, checks achievements, and
// broadcasts to WebSocket clients.
func (s *Server) runRace(horses []*models.Horse, trackType models.TrackType, purse int64) raceResult {
	// 1. Generate weather appropriate for the track.
	weather := tournussy.RandomWeatherForTrack(trackType)

	// 2. Create and simulate the race with weather.
	race := racussy.NewRace(horses, trackType, purse)
	race = racussy.SimulateRaceWithWeather(race, horses, weather)

	// 3. Generate the indexed narrative with weather context.
	narrativeIndexed := racussy.GenerateRaceNarrativeIndexed(race, weather)
	// Also generate the plain string narrative for backward compatibility.
	narrative := make([]string, len(narrativeIndexed))
	for i, nl := range narrativeIndexed {
		narrative[i] = nl.Text
	}

	// 4. Build horse lookup map.
	horseMap := make(map[string]*models.Horse, len(horses))
	for _, h := range horses {
		horseMap[h.ID] = h
	}

	// 5. Sort entries by finish place.
	sorted := make([]models.RaceEntry, len(race.Entries))
	copy(sorted, race.Entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].FinishPlace < sorted[j].FinishPlace
	})

	// 6. Accumulate pairwise ELO deltas (BUG 7 fix: don't mutate in-place).
	eloDeltas := make(map[string]float64, len(sorted))
	for _, e := range sorted {
		eloDeltas[e.HorseID] = 0
	}
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			winner := horseMap[sorted[i].HorseID]
			loser := horseMap[sorted[j].HorseID]
			if winner == nil || loser == nil {
				continue
			}
			newWinnerELO, newLoserELO := marketussy.ELOUpdate(winner, loser)
			eloDeltas[winner.ID] += newWinnerELO - winner.ELO
			eloDeltas[loser.ID] += newLoserELO - loser.ELO
		}
	}

	// 7. Calculate purse distribution (1st=50%, 2nd=30%, 3rd=20%).
	purseDistribution := map[int]float64{1: 0.50, 2: 0.30, 3: 0.20}

	// 8. Apply all updates.
	for _, entry := range sorted {
		horse := horseMap[entry.HorseID]
		if horse == nil {
			continue
		}

		oldELO := horse.ELO
		newELO := horse.ELO + eloDeltas[horse.ID]

		wins := 0
		losses := 0
		if entry.FinishPlace == 1 {
			wins = 1
		} else {
			losses = 1
		}

		// Update stats via stable manager.
		if err := s.stables.UpdateHorseStats(horse.ID, wins, losses, 1, newELO); err != nil {
			log.Printf("server: failed to update stats for horse %s: %v", horse.ID, err)
		}

		// Track peak ELO.
		if newELO > horse.PeakELO {
			horse.PeakELO = newELO
		}

		// Calculate and distribute purse earnings.
		earnings := int64(0)
		if share, ok := purseDistribution[entry.FinishPlace]; ok {
			earnings = int64(float64(purse) * share)
		}
		horse.TotalEarnings += earnings

		// Add earnings to the horse's stable.
		if earnings > 0 {
			s.addEarningsToStable(horse, earnings)
		}

		// Apply post-race fatigue.
		fatigue := racussy.CalcPostRaceFatigue(horse, race, entry.FinishPlace, weather)
		horse.Fatigue += fatigue
		if horse.Fatigue > 100 {
			horse.Fatigue = 100
		}

		// Record race result in history.
		result := &models.RaceResult{
			RaceID:      race.ID,
			HorseID:     horse.ID,
			HorseName:   horse.Name,
			TrackType:   trackType,
			Distance:    race.Distance,
			FinishPlace: entry.FinishPlace,
			TotalHorses: len(race.Entries),
			FinalTime:   entry.FinalTime,
			ELOBefore:   oldELO,
			ELOAfter:    newELO,
			Earnings:    earnings,
			Weather:     string(weather),
			CreatedAt:   time.Now(),
		}
		s.raceHistory.RecordResult(result)

		// Sync horse state back to stable.
		s.syncHorseToStable(horse)

		// Check achievements for this horse.
		s.checkAndApplyAchievements(horse)

		// Check milestone traits.
		if wins == 1 && horse.Wins == 1 {
			s.trainer.AssignTraitOnMilestone(horse, "first_win")
		}
		if horse.Races == 10 {
			s.trainer.AssignTraitOnMilestone(horse, "10_races")
		}
	}

	// Broadcast tick-by-tick replay to WebSocket clients.
	go s.broadcastRaceReplay(race, narrativeIndexed)

	log.Printf("server: race %s finished on %s (%dm) with %d entries, weather: %s",
		race.ID, race.TrackType, race.Distance, len(race.Entries), weather)

	return raceResult{
		Race:             race,
		Narrative:        narrative,
		NarrativeIndexed: narrativeIndexed,
		Weather:          weather,
	}
}

// addEarningsToStable finds the stable that owns a horse and adds earnings.
func (s *Server) addEarningsToStable(horse *models.Horse, earnings int64) {
	for _, stable := range s.stables.ListStables() {
		for _, h := range stable.Horses {
			if h.ID == horse.ID {
				stable.Cummies += earnings
				stable.TotalEarnings += earnings
				stable.TotalRaces++
				return
			}
		}
	}
}

// syncHorseToStable triggers the stable manager to update the embedded horse
// in the stable's horse slice. We call UpdateHorseStats with zero deltas
// to trigger the sync, since the horse pointer is already mutated.
func (s *Server) syncHorseToStable(horse *models.Horse) {
	// UpdateHorseStats with zero deltas just syncs the horse to the stable.
	_ = s.stables.UpdateHorseStats(horse.ID, 0, 0, 0, horse.ELO)
}

// checkAndApplyAchievements checks for new achievements for a horse and
// adds them to the stable's achievement list.
func (s *Server) checkAndApplyAchievements(horse *models.Horse) {
	// Find the stable that owns this horse.
	for _, stable := range s.stables.ListStables() {
		for _, h := range stable.Horses {
			if h.ID == horse.ID {
				newAchievements := tournussy.CheckAchievements(horse, s.raceHistory, stable)
				for _, a := range newAchievements {
					if !hasAchievement(stable.Achievements, a.ID) {
						stable.Achievements = append(stable.Achievements, a)
						log.Printf("server: achievement unlocked for stable %s: %s (%s)",
							stable.Name, a.Name, a.Description)
					}
				}
				return
			}
		}
	}
}

// hasAchievement checks if an achievement ID exists in a slice.
func hasAchievement(achievements []models.Achievement, id string) bool {
	for _, a := range achievements {
		if a.ID == id {
			return true
		}
	}
	return false
}

// broadcastRaceReplay sends tick-by-tick telemetry to WebSocket clients with
// narrative lines interleaved at the correct ticks for real-time play-by-play.
func (s *Server) broadcastRaceReplay(race *models.Race, narrativeIndexed []racussy.NarrativeLine) {
	tickDelay := 50 * time.Millisecond

	if envDelay := os.Getenv("STALLIONUSSY_TICK_DELAY_MS"); envDelay != "" {
		var ms int
		if _, err := fmt.Sscanf(envDelay, "%d", &ms); err == nil && ms > 0 {
			tickDelay = time.Duration(ms) * time.Millisecond
		}
	}

	// Build a map of tick -> narrative lines for that tick.
	narrativeByTick := make(map[int][]racussy.NarrativeLine)
	for _, nl := range narrativeIndexed {
		narrativeByTick[nl.Tick] = append(narrativeByTick[nl.Tick], nl)
	}

	// Broadcast race_start.
	s.hub.BroadcastRaceStart(race.ID, race)

	// Send pre-race narrative (tick 0) before the first tick.
	if preRace, ok := narrativeByTick[0]; ok {
		texts := make([]string, len(preRace))
		classes := make([]string, len(preRace))
		for i, nl := range preRace {
			texts[i] = nl.Text
			classes[i] = nl.Class
		}
		s.hub.BroadcastNarrativeTick(race.ID, 0, texts, classes)
	}
	time.Sleep(tickDelay * 5) // Brief pause for pre-race announcements

	// Determine the max tick.
	maxTick := 0
	for _, entry := range race.Entries {
		if len(entry.TickLog) > 0 {
			lastTick := entry.TickLog[len(entry.TickLog)-1].Tick
			if lastTick > maxTick {
				maxTick = lastTick
			}
		}
	}

	// Build per-entry tick index.
	type tickData struct {
		position float64
		speed    float64
		event    string
	}
	tickIndices := make([]map[int]tickData, len(race.Entries))
	for i, entry := range race.Entries {
		idx := make(map[int]tickData, len(entry.TickLog))
		for _, te := range entry.TickLog {
			idx[te.Tick] = tickData{
				position: te.Position,
				speed:    te.Speed,
				event:    te.Event,
			}
		}
		tickIndices[i] = idx
	}

	// Replay tick-by-tick with interleaved narrative.
	for tick := 1; tick <= maxTick; tick++ {
		tickEntries := make([]models.RaceEntry, len(race.Entries))
		for i, entry := range race.Entries {
			tickEntries[i] = models.RaceEntry{
				HorseID:   entry.HorseID,
				HorseName: entry.HorseName,
			}
			if td, ok := tickIndices[i][tick]; ok {
				tickEntries[i].Position = td.position
				tickEntries[i].TickLog = []models.TickEvent{
					{
						Tick:     tick,
						Position: td.position,
						Speed:    td.speed,
						Event:    td.event,
					},
				}
			}
		}

		s.hub.BroadcastRaceTick(race.ID, tick, tickEntries)

		// Send any narrative lines for this tick.
		if nls, ok := narrativeByTick[tick]; ok {
			texts := make([]string, len(nls))
			classes := make([]string, len(nls))
			for i, nl := range nls {
				texts[i] = nl.Text
				classes[i] = nl.Class
			}
			s.hub.BroadcastNarrativeTick(race.ID, tick, texts, classes)
		}

		time.Sleep(tickDelay)
	}

	// Send any post-race narrative lines (tick > maxTick).
	for tick, nls := range narrativeByTick {
		if tick <= maxTick {
			continue
		}
		texts := make([]string, len(nls))
		classes := make([]string, len(nls))
		for i, nl := range nls {
			texts[i] = nl.Text
			classes[i] = nl.Class
		}
		s.hub.BroadcastNarrativeTick(race.ID, tick, texts, classes)
	}

	// Broadcast race_end.
	s.hub.BroadcastRaceEnd(race.ID, race)
}

// ===========================================================================
// Race History handlers
// ===========================================================================

func (s *Server) handleGetRaceHistory(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	results := s.raceHistory.GetRecentResults(limit)
	if results == nil {
		results = []*models.RaceResult{}
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleGetHorseHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if _, err := s.stables.GetHorse(id); err != nil {
		writeError(w, http.StatusNotFound, "horse not found: "+err.Error())
		return
	}

	results := s.raceHistory.GetHorseHistory(id)
	if results == nil {
		results = []*models.RaceResult{}
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleGetHorseStats(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if _, err := s.stables.GetHorse(id); err != nil {
		writeError(w, http.StatusNotFound, "horse not found: "+err.Error())
		return
	}

	stats := s.raceHistory.GetHorseStats(id)
	writeJSON(w, http.StatusOK, stats)
}

// ===========================================================================
// Achievement handlers
// ===========================================================================

func (s *Server) handleCheckHorseAchievements(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	horse, err := s.stables.GetHorse(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "horse not found: "+err.Error())
		return
	}

	// Find the stable for this horse.
	var stable *models.Stable
	for _, st := range s.stables.ListStables() {
		for _, h := range st.Horses {
			if h.ID == horse.ID {
				stable = st
				break
			}
		}
		if stable != nil {
			break
		}
	}

	if stable == nil {
		writeError(w, http.StatusNotFound, "horse's stable not found")
		return
	}

	newAchievements := tournussy.CheckAchievements(horse, s.raceHistory, stable)

	// Apply new achievements.
	for _, a := range newAchievements {
		if !hasAchievement(stable.Achievements, a.ID) {
			stable.Achievements = append(stable.Achievements, a)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"newAchievements": newAchievements,
		"allAchievements": stable.Achievements,
	})
}

// ===========================================================================
// Market handlers
// ===========================================================================

func (s *Server) handleListMarket(w http.ResponseWriter, r *http.Request) {
	listings := s.market.ListActiveListings()
	writeJSON(w, http.StatusOK, listings)
}

func (s *Server) handleCreateListing(w http.ResponseWriter, r *http.Request) {
	var req struct {
		HorseID string `json:"horseID"`
		Price   int64  `json:"price"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.HorseID == "" {
		writeError(w, http.StatusBadRequest, "horseID is required")
		return
	}
	if req.Price <= 0 {
		writeError(w, http.StatusBadRequest, "price must be positive")
		return
	}

	horse, err := s.stables.GetHorse(req.HorseID)
	if err != nil {
		writeError(w, http.StatusNotFound, "horse not found: "+err.Error())
		return
	}

	listing, err := s.market.CreateListing(horse, horse.OwnerID, req.Price)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	log.Printf("server: listed horse %q (%s) on stud market for %d cummies", horse.Name, horse.ID, req.Price)
	writeJSON(w, http.StatusCreated, listing)
}

func (s *Server) handleBuyListing(w http.ResponseWriter, r *http.Request) {
	listingID := r.PathValue("id")

	var req struct {
		BuyerStableID string `json:"buyerStableID"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.BuyerStableID == "" {
		writeError(w, http.StatusBadRequest, "buyerStableID is required")
		return
	}

	// Look up the listing.
	listing, err := s.market.GetListing(listingID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	// Look up the buyer stable.
	buyerStable, err := s.stables.GetStable(req.BuyerStableID)
	if err != nil {
		writeError(w, http.StatusNotFound, "buyer stable not found: "+err.Error())
		return
	}

	// Look up the stud horse.
	studHorse, err := s.stables.GetHorse(listing.HorseID)
	if err != nil {
		writeError(w, http.StatusNotFound, "stud horse not found: "+err.Error())
		return
	}

	// Find the seller's stable.
	sellerStableID := ""
	for _, stable := range s.stables.ListStables() {
		if stable.OwnerID == listing.OwnerID {
			sellerStableID = stable.ID
			break
		}
	}
	if sellerStableID == "" {
		writeError(w, http.StatusInternalServerError, "seller stable not found")
		return
	}

	// Process the purchase (economic side).
	tx, err := s.market.PurchaseBreeding(listingID, buyerStable.OwnerID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Transfer cummies from buyer to seller (minus burn).
	transferAmount := listing.Price - tx.BurnAmount
	if err := s.stables.TransferCummies(req.BuyerStableID, sellerStableID, transferAmount); err != nil {
		writeError(w, http.StatusBadRequest, "payment failed: "+err.Error())
		return
	}

	// The buyer needs a mare to breed with the stud.
	buyerHorses := s.stables.ListHorses(req.BuyerStableID)
	var mare *models.Horse
	if len(buyerHorses) > 0 {
		mare = buyerHorses[0]
	} else {
		mare = &models.Horse{
			ID:             "temp-mare",
			Name:           "Anonymous Mare",
			Genome:         genussy.RandomGenome(),
			Generation:     0,
			FitnessCeiling: genussy.CalcFitnessCeiling(genussy.RandomGenome()),
			CurrentFitness: 0.5,
			ELO:            1200,
		}
	}

	// Breed the stud horse with the mare.
	foal := genussy.Breed(studHorse, mare)

	// Assign traits to the foal.
	s.trainer.AssignTraitsAtBirth(foal, studHorse, mare)

	// Add the foal to the buyer's stable.
	if err := s.stables.AddHorseToStable(req.BuyerStableID, foal); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add foal to stable: "+err.Error())
		return
	}

	tx.FoalID = foal.ID

	log.Printf("server: stud purchase — %s bought breeding from %s, foal %q (%s) for %d cummies (burned %d)",
		buyerStable.OwnerID, listing.OwnerID, foal.Name, foal.ID, listing.Price, tx.BurnAmount)

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"transaction": tx,
		"foal":        foal,
	})
}

func (s *Server) handleDelistListing(w http.ResponseWriter, r *http.Request) {
	listingID := r.PathValue("id")

	// FIX: require ownerID in query param or body.
	ownerID := r.URL.Query().Get("ownerID")
	if ownerID == "" {
		// Try reading from body.
		var req struct {
			OwnerID string `json:"ownerID"`
		}
		if err := readJSON(r, &req); err == nil && req.OwnerID != "" {
			ownerID = req.OwnerID
		}
	}

	// Look up the listing.
	listing, err := s.market.GetListing(listingID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	// If ownerID still empty, default to listing owner for backward compat.
	if ownerID == "" {
		ownerID = listing.OwnerID
	}

	if err := s.market.DelistStud(listingID, ownerID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	log.Printf("server: delisted stud listing %s for horse %s", listingID, listing.HorseID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "delisted", "listingID": listingID})
}

// ===========================================================================
// Tournament handlers
// ===========================================================================

func (s *Server) handleListTournaments(w http.ResponseWriter, r *http.Request) {
	tournaments := s.tournaments.ListTournaments()
	writeJSON(w, http.StatusOK, tournaments)
}

func (s *Server) handleCreateTournament(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string           `json:"name"`
		TrackType models.TrackType `json:"trackType"`
		Rounds    int              `json:"rounds"`
		EntryFee  int64            `json:"entryFee"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.TrackType != "" && models.TrackDistance(req.TrackType) == 0 {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid track type: %q", req.TrackType))
		return
	}

	// Default track type.
	if req.TrackType == "" {
		req.TrackType = models.TrackSprintussy
	}
	if req.Rounds <= 0 {
		req.Rounds = 3
	}

	tournament := s.tournaments.CreateTournament(req.Name, req.TrackType, req.Rounds, req.EntryFee)
	writeJSON(w, http.StatusCreated, tournament)
}

func (s *Server) handleGetTournament(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	tournament, err := s.tournaments.GetTournament(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	standings := s.tournaments.GetStandings(id)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tournament": tournament,
		"standings":  standings,
	})
}

func (s *Server) handleRegisterTournament(w http.ResponseWriter, r *http.Request) {
	tournamentID := r.PathValue("id")

	var req struct {
		HorseID  string `json:"horseID"`
		StableID string `json:"stableID"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.HorseID == "" || req.StableID == "" {
		writeError(w, http.StatusBadRequest, "horseID and stableID are required")
		return
	}

	horse, err := s.stables.GetHorse(req.HorseID)
	if err != nil {
		writeError(w, http.StatusNotFound, "horse not found: "+err.Error())
		return
	}

	if horse.Retired {
		writeError(w, http.StatusBadRequest, "horse is retired and cannot race")
		return
	}

	if err := s.tournaments.RegisterHorse(tournamentID, horse, req.StableID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"status":       "registered",
		"tournamentID": tournamentID,
		"horseID":      req.HorseID,
	})
}

func (s *Server) handleTournamentRace(w http.ResponseWriter, r *http.Request) {
	tournamentID := r.PathValue("id")

	// Get tournament to find registered horses.
	tournament, err := s.tournaments.GetTournament(tournamentID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if len(tournament.Standings) < 2 {
		writeError(w, http.StatusBadRequest, "at least 2 registered horses are required to run a round")
		return
	}

	// Resolve registered horses.
	var horses []*models.Horse
	for _, entry := range tournament.Standings {
		h, err := s.stables.GetHorse(entry.HorseID)
		if err != nil {
			continue // skip missing horses
		}
		if !h.Retired {
			horses = append(horses, h)
		}
	}

	if len(horses) < 2 {
		writeError(w, http.StatusBadRequest, "not enough non-retired horses for this round")
		return
	}

	// Run next round via tournament manager (creates the race object).
	race, err := s.tournaments.RunNextRound(tournamentID, horses)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Generate weather.
	weather := tournussy.RandomWeatherForTrack(tournament.TrackType)

	// Simulate the race with weather.
	race = racussy.SimulateRaceWithWeather(race, horses, weather)

	// Generate indexed narrative.
	narrativeIndexed := racussy.GenerateRaceNarrativeIndexed(race, weather)
	narrative := make([]string, len(narrativeIndexed))
	for i, nl := range narrativeIndexed {
		narrative[i] = nl.Text
	}

	// Record round results in tournament standings.
	if err := s.tournaments.RecordRoundResults(tournamentID, race); err != nil {
		log.Printf("server: failed to record tournament round results: %v", err)
	}

	// Apply all post-race processing (ELO, fatigue, history, achievements).
	s.applyPostRaceEffects(race, horses, weather)

	// Broadcast replay.
	go s.broadcastRaceReplay(race, narrativeIndexed)

	// Get updated standings.
	standings := s.tournaments.GetStandings(tournamentID)

	log.Printf("server: tournament %s round completed, race %s, weather: %s",
		tournamentID, race.ID, weather)

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"race":              race,
		"narrative":         narrative,
		"narrative_indexed": narrativeIndexed,
		"weather":           weather,
		"standings":         standings,
	})
}

// applyPostRaceEffects handles ELO, fatigue, history, achievements after a race.
// Used by tournament rounds to avoid duplicating the race processing logic.
func (s *Server) applyPostRaceEffects(race *models.Race, horses []*models.Horse, weather models.Weather) {
	horseMap := make(map[string]*models.Horse, len(horses))
	for _, h := range horses {
		horseMap[h.ID] = h
	}

	sorted := make([]models.RaceEntry, len(race.Entries))
	copy(sorted, race.Entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].FinishPlace < sorted[j].FinishPlace
	})

	// Accumulate pairwise ELO deltas.
	eloDeltas := make(map[string]float64, len(sorted))
	for _, e := range sorted {
		eloDeltas[e.HorseID] = 0
	}
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			winner := horseMap[sorted[i].HorseID]
			loser := horseMap[sorted[j].HorseID]
			if winner == nil || loser == nil {
				continue
			}
			newW, newL := marketussy.ELOUpdate(winner, loser)
			eloDeltas[winner.ID] += newW - winner.ELO
			eloDeltas[loser.ID] += newL - loser.ELO
		}
	}

	purseDistribution := map[int]float64{1: 0.50, 2: 0.30, 3: 0.20}

	for _, entry := range sorted {
		horse := horseMap[entry.HorseID]
		if horse == nil {
			continue
		}

		oldELO := horse.ELO
		newELO := horse.ELO + eloDeltas[horse.ID]

		wins := 0
		losses := 0
		if entry.FinishPlace == 1 {
			wins = 1
		} else {
			losses = 1
		}

		if err := s.stables.UpdateHorseStats(horse.ID, wins, losses, 1, newELO); err != nil {
			log.Printf("server: failed to update stats for horse %s: %v", horse.ID, err)
		}

		if newELO > horse.PeakELO {
			horse.PeakELO = newELO
		}

		earnings := int64(0)
		if share, ok := purseDistribution[entry.FinishPlace]; ok {
			earnings = int64(float64(race.Purse) * share)
		}
		horse.TotalEarnings += earnings

		if earnings > 0 {
			s.addEarningsToStable(horse, earnings)
		}

		fatigue := racussy.CalcPostRaceFatigue(horse, race, entry.FinishPlace, weather)
		horse.Fatigue += fatigue
		if horse.Fatigue > 100 {
			horse.Fatigue = 100
		}

		result := &models.RaceResult{
			RaceID:      race.ID,
			HorseID:     horse.ID,
			HorseName:   horse.Name,
			TrackType:   race.TrackType,
			Distance:    race.Distance,
			FinishPlace: entry.FinishPlace,
			TotalHorses: len(race.Entries),
			FinalTime:   entry.FinalTime,
			ELOBefore:   oldELO,
			ELOAfter:    newELO,
			Earnings:    earnings,
			Weather:     string(weather),
			CreatedAt:   time.Now(),
		}
		s.raceHistory.RecordResult(result)

		s.syncHorseToStable(horse)
		s.checkAndApplyAchievements(horse)
	}
}

// ===========================================================================
// Trading handlers
// ===========================================================================

func (s *Server) handleListTrades(w http.ResponseWriter, r *http.Request) {
	stableID := r.URL.Query().Get("stableID")
	trades := s.trades.ListAllPending(stableID)
	if trades == nil {
		trades = []*pedigreussy.TradeOffer{}
	}
	writeJSON(w, http.StatusOK, trades)
}

func (s *Server) handleCreateTrade(w http.ResponseWriter, r *http.Request) {
	var req struct {
		HorseID    string `json:"horseID"`
		FromStable string `json:"fromStable"`
		ToStable   string `json:"toStable"`
		Price      int64  `json:"price"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.HorseID == "" || req.FromStable == "" || req.ToStable == "" {
		writeError(w, http.StatusBadRequest, "horseID, fromStable, and toStable are required")
		return
	}

	horse, err := s.stables.GetHorse(req.HorseID)
	if err != nil {
		writeError(w, http.StatusNotFound, "horse not found: "+err.Error())
		return
	}

	// Verify stables exist.
	if _, err := s.stables.GetStable(req.FromStable); err != nil {
		writeError(w, http.StatusNotFound, "source stable not found: "+err.Error())
		return
	}
	if _, err := s.stables.GetStable(req.ToStable); err != nil {
		writeError(w, http.StatusNotFound, "destination stable not found: "+err.Error())
		return
	}

	offer := s.trades.CreateOffer(req.HorseID, horse.Name, req.FromStable, req.ToStable, req.Price)
	writeJSON(w, http.StatusCreated, offer)
}

func (s *Server) handleAcceptTrade(w http.ResponseWriter, r *http.Request) {
	tradeID := r.PathValue("id")

	offer, err := s.trades.AcceptOffer(tradeID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Execute the transfer: move horse and transfer Cummies.
	if offer.Price > 0 {
		if err := s.stables.TransferCummies(offer.ToStable, offer.FromStable, offer.Price); err != nil {
			writeError(w, http.StatusBadRequest, "payment failed: "+err.Error())
			return
		}
	}

	if err := s.stables.MoveHorse(offer.HorseID, offer.FromStable, offer.ToStable); err != nil {
		writeError(w, http.StatusInternalServerError, "transfer failed: "+err.Error())
		return
	}

	log.Printf("server: trade accepted — horse %s moved from stable %s to %s for %d cummies",
		offer.HorseName, offer.FromStable, offer.ToStable, offer.Price)

	writeJSON(w, http.StatusOK, offer)
}

func (s *Server) handleRejectTrade(w http.ResponseWriter, r *http.Request) {
	tradeID := r.PathValue("id")

	if err := s.trades.RejectOffer(tradeID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected", "tradeID": tradeID})
}

func (s *Server) handleCancelTrade(w http.ResponseWriter, r *http.Request) {
	tradeID := r.PathValue("id")

	if err := s.trades.CancelOffer(tradeID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled", "tradeID": tradeID})
}

// ===========================================================================
// Season / Aging handler
// ===========================================================================

func (s *Server) handleAdvanceSeason(w http.ResponseWriter, r *http.Request) {
	allHorses := s.stables.GetLeaderboard()

	type agingResult struct {
		HorseID   string `json:"horseID"`
		HorseName string `json:"horseName"`
		NewAge    int    `json:"newAge"`
		LifeStage string `json:"lifeStage"`
		Retired   bool   `json:"retired"`
		Message   string `json:"message,omitempty"`
	}

	var results []agingResult

	for _, horse := range allHorses {
		if horse.Retired {
			continue
		}

		trainussy.AgeHorse(horse)

		result := agingResult{
			HorseID:   horse.ID,
			HorseName: horse.Name,
			NewAge:    horse.Age,
			LifeStage: trainussy.LifeStage(horse),
			Retired:   horse.Retired,
		}

		// Check for retirement.
		if !horse.Retired {
			shouldRetire, reason := trainussy.ShouldRetire(horse)
			if shouldRetire {
				trainussy.RetireHorse(horse, reason)
				result.Retired = true
				result.Message = reason
			}
		} else {
			result.Message = "Retired during aging"
		}

		s.syncHorseToStable(horse)
		results = append(results, result)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"season":  "advanced",
		"results": results,
	})
}

// ===========================================================================
// Weather handler
// ===========================================================================

func (s *Server) handleGetWeather(w http.ResponseWriter, r *http.Request) {
	weather := tournussy.RandomWeather()
	effects := tournussy.WeatherEffects(weather)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"weather":     weather,
		"description": effects.Description,
		"modifiers": map[string]float64{
			"speedMod":   effects.SpeedMod,
			"fatigueMod": effects.FatigueMod,
			"chaosMod":   effects.ChaosMod,
			"panicMod":   effects.PanicMod,
		},
	})
}

// ===========================================================================
// WebSocket handler
// ===========================================================================

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	commussy.ServeWs(s.hub, w, r)
}

// ===========================================================================
// Helper functions
// ===========================================================================

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("server: failed to encode JSON response: %v", err)
	}
}

// writeError writes a JSON error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// readJSON decodes the request body into the provided value.
func readJSON(r *http.Request, v interface{}) error {
	if r.Body == nil {
		return fmt.Errorf("request body is empty")
	}
	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	return decoder.Decode(v)
}

// enableCORS wraps a handler to add permissive CORS headers for development.
// Properly handles OPTIONS preflight requests.
func enableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		w.Header().Set("Access-Control-Max-Age", "86400")

		// Handle preflight OPTIONS requests.
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware wraps a handler to log each incoming request.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		lrw := &loggingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(lrw, r)

		log.Printf("%s %s %d %s", r.Method, r.URL.Path, lrw.statusCode, time.Since(start).Round(time.Microsecond))
	})
}

// loggingResponseWriter wraps http.ResponseWriter to capture the status code.
type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

// Hijack implements http.Hijacker so WebSocket upgrades work through the
// logging middleware. Without this, gorilla/websocket gets:
//
//	"websocket: response does not implement http.Hijacker"
func (lrw *loggingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := lrw.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not implement http.Hijacker")
}
