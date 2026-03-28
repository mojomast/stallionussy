// Package server implements the HTTP API server for StallionUSSY.
// It wires together all subsystems (stables, market, genetics, racing,
// training, tournaments, pedigree, trading, WebSocket telemetry) behind
// a JSON REST API using only the Go standard library net/http (plus
// gorilla/websocket for the WS endpoint).
package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mojomast/stallionussy/internal/authussy"
	"github.com/mojomast/stallionussy/internal/commussy"
	"github.com/mojomast/stallionussy/internal/genussy"
	"github.com/mojomast/stallionussy/internal/marketussy"
	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/pedigreussy"
	"github.com/mojomast/stallionussy/internal/racussy"
	"github.com/mojomast/stallionussy/internal/repository"
	"github.com/mojomast/stallionussy/internal/repository/postgres"
	"github.com/mojomast/stallionussy/internal/stableussy"
	"github.com/mojomast/stallionussy/internal/tournussy"
	"github.com/mojomast/stallionussy/internal/trainussy"
)

// ---------------------------------------------------------------------------
// Server — the main HTTP API server
// ---------------------------------------------------------------------------

// Server holds all subsystem references and the HTTP mux.
type Server struct {
	// Existing in-memory managers (business logic layer).
	stables     *stableussy.StableManager
	market      *marketussy.Market
	hub         *commussy.Hub
	trainer     *trainussy.Trainer
	tournaments *tournussy.TournamentManager
	raceHistory *tournussy.RaceHistory
	pedigree    *pedigreussy.PedigreeEngine
	trades      *pedigreussy.TradeManager
	mux         *http.ServeMux

	// Race replay cache — stores recent full race results for sharing.
	raceCacheMu sync.RWMutex
	raceCache   map[string]*raceResult // raceID -> full result

	// Persistence layer (nil when running without DB).
	userRepo        repository.UserRepository
	stableRepo      repository.StableRepository
	horseRepo       repository.HorseRepository
	raceResultRepo  repository.RaceResultRepository
	marketRepo      repository.MarketRepository
	tournamentRepo  repository.TournamentRepository
	tradeRepo       repository.TradeRepository
	achievementRepo repository.AchievementRepository
	trainingRepo    repository.TrainingSessionRepository
	marketTxRepo    repository.MarketTransactionRepository
	auctionRepo     repository.AuctionRepository
	replayRepo      repository.RaceReplayRepository
	allianceRepo    repository.AllianceRepository

	// Auth (nil when running without DB).
	auth        *authussy.AuthService
	authHandler *authussy.AuthHandler

	// Head-to-head challenges (in-memory store).
	challenges  map[string]*models.Challenge
	challengeMu sync.RWMutex

	// Live auctions (in-memory store).
	auctions  map[string]*models.Auction
	auctionMu sync.RWMutex

	// Race betting pools (in-memory store).
	bettingPools map[string]*models.BettingPool // raceID -> pool
	bettingMu    sync.RWMutex

	// Seasonal competition tracking (in-memory store).
	currentSeason *models.Season
	pastSeasons   []models.Season
	seasonMu      sync.RWMutex

	// Player engagement tracking (in-memory store).
	progress   map[string]*models.PlayerProgress // userID -> progress
	progressMu sync.RWMutex

	// Horse rivalry tracking (in-memory store).
	// rivalries[winnerID][loserID] = win count.
	rivalries map[string]map[string]int
	rivalryMu sync.RWMutex

	// Stable alliances / guilds (in-memory store).
	alliances  map[string]*models.Alliance // allianceID -> alliance
	allianceMu sync.RWMutex
}

// NewServer initializes all subsystems, seeds the legendary horses into a
// "House of USSY" stable, registers all routes, and returns a ready Server.
//
// If db is non-nil, PostgreSQL repositories are created and existing data is
// loaded from the database into the in-memory managers. All state-mutating
// handlers then write through to the database in addition to updating
// in-memory state. If db is nil, the server operates in pure in-memory mode
// (backward-compatible with the pre-database setup).
func NewServer(db *postgres.DB) *Server {
	sm := stableussy.NewStableManager()
	rh := tournussy.NewRaceHistory()

	s := &Server{
		stables:      sm,
		market:       marketussy.NewMarket(),
		hub:          commussy.NewHub(),
		trainer:      trainussy.NewTrainer(),
		tournaments:  tournussy.NewTournamentManager(rh),
		raceHistory:  rh,
		pedigree:     pedigreussy.NewPedigreeEngine(sm.GetHorse),
		trades:       pedigreussy.NewTradeManager(),
		mux:          http.NewServeMux(),
		raceCache:    make(map[string]*raceResult),
		challenges:   make(map[string]*models.Challenge),
		auctions:     make(map[string]*models.Auction),
		bettingPools: make(map[string]*models.BettingPool),
		currentSeason: &models.Season{
			ID:        1,
			Name:      "Season 1: The Ussening",
			StartedAt: time.Now(),
			Active:    true,
		},
		pastSeasons: []models.Season{},
		progress:    make(map[string]*models.PlayerProgress),
		rivalries:   make(map[string]map[string]int),
		alliances:   make(map[string]*models.Alliance),
	}

	// If a DB connection is provided, wire up persistence and auth.
	if db != nil {
		// Create repository instances.
		s.userRepo = postgres.NewUserRepo(db)
		s.stableRepo = postgres.NewStableRepo(db)
		s.horseRepo = postgres.NewHorseRepo(db)
		s.raceResultRepo = postgres.NewRaceResultRepo(db)
		s.marketRepo = postgres.NewMarketRepo(db)
		s.tournamentRepo = postgres.NewTournamentRepo(db)
		s.tradeRepo = postgres.NewTradeRepo(db)
		s.achievementRepo = postgres.NewAchievementRepo(db)
		s.trainingRepo = postgres.NewTrainingSessionRepo(db)
		s.marketTxRepo = postgres.NewMarketTransactionRepo(db)
		s.auctionRepo = postgres.NewAuctionRepo(db)
		s.replayRepo = postgres.NewReplayRepo(db)
		s.allianceRepo = postgres.NewAllianceRepo(db)

		// Create auth service.
		jwtSecret := os.Getenv("JWT_SECRET")
		if jwtSecret == "" {
			jwtSecret = "stallionussy-default-secret-change-me"
		}
		s.auth = authussy.NewAuthService(jwtSecret, 72*time.Hour)

		// Create auth handler with stable creation callback.
		s.authHandler = authussy.NewAuthHandler(s.auth, s.userRepo, func(name, ownerID string) *models.Stable {
			stable := s.stables.CreateStable(name, ownerID)
			// Also persist the new stable to the database.
			ctx := context.Background()
			if err := s.stableRepo.CreateStable(ctx, stable); err != nil {
				log.Printf("server: failed to persist stable %s to DB: %v", stable.ID, err)
			}
			return stable
		})

		// Load persisted data from DB into in-memory managers.
		s.loadFromDB()
	}

	// Start the WebSocket hub event loop.
	go s.hub.Run()

	// Start the challenge expiry cleanup goroutine.
	go s.challengeExpiryLoop()

	// Start the auction expiry loop goroutine.
	go s.auctionExpiryLoop()

	// Start the replay cleanup goroutine (deletes replays older than 7 days, hourly).
	go s.replayCleanupLoop()

	// Set up the chat command callback so the hub can forward commands
	// (e.g. /send, /trade) to the server for processing.
	s.hub.OnChatCommand = func(senderUserID, senderUsername, command string, args map[string]interface{}) {
		switch command {
		case "send":
			s.handleChatSend(senderUserID, senderUsername, args)
		case "trade":
			s.handleChatTrade(senderUserID, senderUsername, args)
		case "challenge":
			s.handleChatChallenge(senderUserID, senderUsername, args)
		case "accept":
			s.handleChatAccept(senderUserID, senderUsername, args)
		case "decline":
			s.handleChatDecline(senderUserID, senderUsername, args)
		case "bet":
			s.handleChatBet(senderUserID, senderUsername, args)
		case "auction":
			s.handleChatAuction(senderUserID, senderUsername, args)
		case "w", "whisper", "msg", "pm":
			s.handleChatWhisper(senderUserID, senderUsername, args)
		case "alliance":
			s.handleChatAlliance(senderUserID, senderUsername, args)
		}
	}

	// Seed legendary horses only if no stables exist yet (fresh start).
	if len(s.stables.ListStables()) == 0 {
		houseOfUSSY := s.stables.CreateStable("House of USSY", "system")
		s.stables.SeedLegendaries(houseOfUSSY.ID)

		// Assign traits to each legendary horse.
		legendaryHorses := s.stables.ListHorses(houseOfUSSY.ID)
		for _, h := range legendaryHorses {
			s.trainer.AssignTraitsAtBirth(h, h, nil)
		}

		log.Printf("server: seeded 12 legendary horses into stable %q (%s)", houseOfUSSY.Name, houseOfUSSY.ID)

		// Persist the newly seeded stable and horses to DB.
		if s.stableRepo != nil {
			ctx := context.Background()
			if err := s.stableRepo.CreateStable(ctx, houseOfUSSY); err != nil {
				log.Printf("server: failed to persist House of USSY stable: %v", err)
			}
			for _, h := range legendaryHorses {
				s.persistHorse(ctx, h)
			}
		}
	}

	// Register all routes.
	s.routes()

	return s
}

// ---------------------------------------------------------------------------
// Route registration
// ---------------------------------------------------------------------------

func (s *Server) routes() {
	// --- Auth (only when DB/auth is configured) ---
	if s.authHandler != nil {
		s.registerAuthRoutes()
	}

	// --- Stables ---
	s.mux.HandleFunc("GET /api/stables", s.handleListStables)
	s.mux.HandleFunc("POST /api/stables", s.handleCreateStable)
	s.mux.HandleFunc("GET /api/stables/{id}", s.handleGetStable)
	s.mux.HandleFunc("PUT /api/stables/{id}", s.handleUpdateStable)
	s.mux.HandleFunc("GET /api/stables/{id}/horses", s.handleListStableHorses)
	s.mux.HandleFunc("GET /api/stables/{id}/achievements", s.handleGetStableAchievements)

	// --- Horses ---
	s.mux.HandleFunc("GET /api/horses", s.handleListHorses)
	s.mux.HandleFunc("GET /api/horses/{id}", s.handleGetHorse)
	s.mux.HandleFunc("GET /api/horses/{id}/history", s.handleGetHorseHistory)
	s.mux.HandleFunc("GET /api/horses/{id}/stats", s.handleGetHorseStats)
	s.mux.HandleFunc("GET /api/horses/{id}/achievements", s.handleCheckHorseAchievements)
	s.mux.HandleFunc("GET /api/horses/{id}/rivals", s.handleGetHorseRivals)
	s.mux.HandleFunc("POST /api/horses/{id}/retire", s.handleRetireHorse)

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
	s.mux.HandleFunc("GET /api/races/recent", s.handleListRecentReplays)
	s.mux.HandleFunc("GET /api/races/{id}", s.handleGetRaceReplay)

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

	// --- Challenges (Head-to-Head) ---
	s.mux.HandleFunc("POST /api/challenges", s.handleCreateChallenge)
	s.mux.HandleFunc("POST /api/challenges/{id}/accept", s.handleAcceptChallenge)
	s.mux.HandleFunc("POST /api/challenges/{id}/decline", s.handleDeclineChallenge)
	s.mux.HandleFunc("GET /api/challenges", s.handleListChallenges)

	// --- Auctions (Live Horse Auctions) ---
	s.mux.HandleFunc("GET /api/auctions", s.handleListAuctions)
	s.mux.HandleFunc("POST /api/auctions", s.handleCreateAuction)
	s.mux.HandleFunc("GET /api/auctions/{id}", s.handleGetAuction)
	s.mux.HandleFunc("POST /api/auctions/{id}/bid", s.handlePlaceAuctionBid)
	s.mux.HandleFunc("DELETE /api/auctions/{id}", s.handleCancelAuction)

	// --- Betting ---
	s.mux.HandleFunc("POST /api/betting/pools", s.handleOpenBettingPool)
	s.mux.HandleFunc("GET /api/betting/pools/{raceID}", s.handleGetBettingPool)
	s.mux.HandleFunc("POST /api/betting/pools/{raceID}/bet", s.handlePlaceBet)
	s.mux.HandleFunc("GET /api/betting/active", s.handleListActivePools)

	// --- Season / Aging ---
	s.mux.HandleFunc("POST /api/advance-season", s.handleAdvanceSeason)

	// --- Alliances (Stable Guilds) ---
	s.mux.HandleFunc("GET /api/alliances", s.handleListAlliances)
	s.mux.HandleFunc("POST /api/alliances", s.handleCreateAlliance)
	s.mux.HandleFunc("GET /api/alliances/{id}", s.handleGetAlliance)
	s.mux.HandleFunc("POST /api/alliances/{id}/join", s.handleJoinAlliance)
	s.mux.HandleFunc("POST /api/alliances/{id}/leave", s.handleLeaveAlliance)
	s.mux.HandleFunc("POST /api/alliances/{id}/kick", s.handleKickFromAlliance)
	s.mux.HandleFunc("POST /api/alliances/{id}/donate", s.handleDonateToAlliance)
	s.mux.HandleFunc("DELETE /api/alliances/{id}", s.handleDisbandAlliance)

	// --- Horse Injury & Age ---
	s.mux.HandleFunc("POST /api/horses/{id}/heal", s.handleHealHorse)
	s.mux.HandleFunc("GET /api/horses/{id}/age-info", s.handleGetHorseAgeInfo)

	// --- Leaderboard ---
	s.mux.HandleFunc("GET /api/leaderboard", s.handleGetLeaderboard)
	s.mux.HandleFunc("GET /api/leaderboard/horses", s.handleGetHorseLeaderboard)

	// --- Seasons ---
	s.mux.HandleFunc("GET /api/seasons", s.handleListSeasons)
	s.mux.HandleFunc("GET /api/seasons/current", s.handleGetCurrentSeason)
	s.mux.HandleFunc("POST /api/seasons/end", s.handleEndSeason)

	// --- Engagement (daily rewards, prestige, progress) ---
	s.mux.HandleFunc("GET /api/progress", s.handleGetProgress)
	s.mux.HandleFunc("POST /api/daily-reward", s.handleClaimDailyReward)
	s.mux.HandleFunc("GET /api/prestige", s.handleGetPrestige)

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
	var handler http.Handler
	if s.auth != nil {
		// Auth mode: CORS → AuthMiddleware → Logging → Mux
		handler = enableCORS(s.auth.AuthMiddleware(loggingMiddleware(s.mux)))
	} else {
		// No-auth mode (backward compatible).
		handler = enableCORS(loggingMiddleware(s.mux))
	}
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

	// Prefer JWT-authenticated user ID over client-sent ownerID.
	if claims, ok := authussy.GetUserFromContext(r.Context()); ok {
		req.OwnerID = claims.UserID
	}

	// Fall back: default ownerID to "player" if empty (guest mode).
	if req.OwnerID == "" {
		req.OwnerID = "player"
	}

	stable := s.stables.CreateStable(req.Name, req.OwnerID)

	// Write-through: persist stable to DB.
	s.persistStable(r.Context(), stable)

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

	// Ownership check: only the horse's owner can train it.
	if claims, ok := authussy.GetUserFromContext(r.Context()); ok {
		if !s.userOwnsHorse(claims.UserID, id) {
			http.Error(w, "you do not own this horse", http.StatusForbidden)
			return
		}
	}

	if horse.Retired {
		writeError(w, http.StatusBadRequest, "horse is retired and cannot train")
		return
	}

	// Training while injured: 50% chance to worsen the injury.
	if horse.Injury != nil && horse.Injury.Severity != models.SeverityCareerEnding {
		if rand.Float64() < 0.50 {
			// Worsen the injury by one severity level.
			switch horse.Injury.Severity {
			case models.SeverityMinor:
				horse.Injury.Severity = models.SeverityModerate
				horse.Injury.RacesLeft = models.InjuryRaceCooldown(models.SeverityModerate)
				horse.Injury.Description = "Training aggravated the injury! " + horse.Injury.Description
			case models.SeverityModerate:
				horse.Injury.Severity = models.SeveritySevere
				horse.Injury.RacesLeft = models.InjuryRaceCooldown(models.SeveritySevere)
				horse.Injury.Description = "Training made it much worse! " + horse.Injury.Description
			case models.SeveritySevere:
				horse.Injury.Severity = models.SeverityCareerEnding
				horse.Injury.RacesLeft = models.InjuryRaceCooldown(models.SeverityCareerEnding)
				horse.Injury.Description = "CATASTROPHIC: Training destroyed what was left. Career over."
				// Force retirement for career-ending injuries.
				trainussy.RetireHorse(horse, "Career-ending injury sustained during training")
			}

			s.syncHorseToStable(horse)
			s.persistHorse(r.Context(), horse)

			// Broadcast the worsened injury.
			s.hub.BroadcastJSON(map[string]interface{}{
				"type":      "injury_worsened",
				"horseName": horse.Name,
				"horseID":   horse.ID,
				"severity":  string(horse.Injury.Severity),
				"text":      fmt.Sprintf("🤕 %s trained while injured and made it WORSE! Now: %s (%s)", horse.Name, horse.Injury.Severity, horse.Injury.Type),
				"ts":        time.Now().Unix(),
			})

			writeJSON(w, http.StatusOK, map[string]interface{}{
				"warning":  "Training while injured worsened the condition!",
				"horse":    horse,
				"severity": horse.Injury.Severity,
			})
			return
		}
		// 50% chance: training proceeds normally despite injury (lucky).
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

	// Apply prestige training bonus: multiply the fitness gain by the owner's TrainingBonus.
	if claims, ok := authussy.GetUserFromContext(r.Context()); ok {
		ownerTier := s.getPrestigeTierForUser(claims.UserID)
		if ownerTier.TrainingBonus > 1.0 {
			fitnessGain := session.FitnessAfter - session.FitnessBefore
			extraGain := fitnessGain * (ownerTier.TrainingBonus - 1.0)
			horse.CurrentFitness += extraGain
			if horse.CurrentFitness > horse.FitnessCeiling {
				horse.CurrentFitness = horse.FitnessCeiling
			}
			session.FitnessAfter = horse.CurrentFitness
		}
	}

	// Sync horse state back to stable.
	s.syncHorseToStable(horse)

	// Write-through: persist trained horse and training session to DB.
	s.persistHorse(r.Context(), horse)
	s.persistTrainingSession(r.Context(), session)

	// Grant 10 prestige XP for training.
	if claims, ok := authussy.GetUserFromContext(r.Context()); ok {
		s.addPrestigeXP(claims.UserID, claims.Username, 10)
	}

	// Broadcast training event to all connected clients.
	trainEvent := map[string]interface{}{
		"type":      "training_update",
		"action":    "trained",
		"horseName": horse.Name,
		"workout":   string(req.WorkoutType),
	}
	// Look up the stable name for this horse.
	for _, st := range s.stables.ListStables() {
		for _, h := range st.Horses {
			if h.ID == horse.ID {
				trainEvent["stableName"] = st.Name
				break
			}
		}
	}
	s.hub.BroadcastJSON(trainEvent)

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

	// Ownership check: only the horse's owner can rest it.
	if claims, ok := authussy.GetUserFromContext(r.Context()); ok {
		if !s.userOwnsHorse(claims.UserID, id) {
			http.Error(w, "you do not own this horse", http.StatusForbidden)
			return
		}
	}

	if horse.Retired {
		writeError(w, http.StatusBadRequest, "horse is retired")
		return
	}

	// Run a RestDay workout which reduces fatigue by 30.
	session := s.trainer.Train(horse, models.WorkoutRecovery)
	s.syncHorseToStable(horse)

	// Write-through: persist rested horse and training session to DB.
	s.persistHorse(r.Context(), horse)
	s.persistTrainingSession(r.Context(), session)

	// Broadcast rest event to all connected clients.
	restEvent := map[string]interface{}{
		"type":      "training_update",
		"action":    "rested",
		"horseName": horse.Name,
	}
	// Look up the stable name for this horse.
	for _, st := range s.stables.ListStables() {
		for _, h := range st.Horses {
			if h.ID == horse.ID {
				restEvent["stableName"] = st.Name
				break
			}
		}
	}
	s.hub.BroadcastJSON(restEvent)

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

	// Ownership check: user must own either the sire or mare, and the target stable.
	if claims, ok := authussy.GetUserFromContext(r.Context()); ok {
		ownsSire := s.userOwnsHorse(claims.UserID, req.SireID)
		ownsMare := s.userOwnsHorse(claims.UserID, req.MareID)
		if !ownsSire && !ownsMare {
			http.Error(w, "you must own at least one of the breeding horses", http.StatusForbidden)
			return
		}
		// Verify target stable belongs to user.
		userStable := s.getStableForUser(claims.UserID)
		if userStable == nil || userStable.ID != req.StableID {
			http.Error(w, "target stable does not belong to you", http.StatusForbidden)
			return
		}
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

	// Breeding cooldown: each horse can only breed once every N hours.
	cooldown := time.Duration(breedingCooldownHours) * time.Hour
	if !sire.LastBredAt.IsZero() && time.Since(sire.LastBredAt) < cooldown {
		remaining := cooldown - time.Since(sire.LastBredAt)
		writeError(w, http.StatusBadRequest, fmt.Sprintf("sire %q is on breeding cooldown (%.0f minutes remaining)", sire.Name, remaining.Minutes()))
		return
	}
	if !mare.LastBredAt.IsZero() && time.Since(mare.LastBredAt) < cooldown {
		remaining := cooldown - time.Since(mare.LastBredAt)
		writeError(w, http.StatusBadRequest, fmt.Sprintf("mare %q is on breeding cooldown (%.0f minutes remaining)", mare.Name, remaining.Minutes()))
		return
	}

	// Prestige max horses check: can't exceed the stable's horse limit.
	targetStable, err := s.stables.GetStable(req.StableID)
	if err != nil {
		writeError(w, http.StatusNotFound, "target stable not found: "+err.Error())
		return
	}
	ownerTier := s.getPrestigeTierForUser(targetStable.OwnerID)
	if len(targetStable.Horses) >= ownerTier.MaxHorses {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("stable is at max capacity (%d horses) for prestige level %q — level up to unlock more slots!", ownerTier.MaxHorses, ownerTier.Name))
		return
	}

	// Breed!
	foal := genussy.Breed(sire, mare)

	// Apply bloodline bonus via pedigreussy.
	bloodlineBonus := pedigreussy.CalcBloodlineBonus(foal, sire.ID, mare.ID, s.stables.GetHorse)

	// Apply bloodline bonus to fitness ceiling.
	foal.FitnessCeiling *= bloodlineBonus

	// Assign traits based on parentage.
	s.trainer.AssignTraitsAtBirth(foal, sire, mare)

	// Add the foal to the target stable BEFORE computing inbreeding,
	// so the pedigree engine can look up the foal by ID.
	if err := s.stables.AddHorseToStable(req.StableID, foal); err != nil {
		writeError(w, http.StatusBadRequest, "failed to add foal to stable: "+err.Error())
		return
	}

	// Calculate inbreeding coefficient for the FOAL (not the sire).
	// The foal must be in the stable manager so the pedigree engine can find it.
	inbreeding := 0.0
	if coeff, cErr := s.pedigree.CalcInbreedingCoefficient(foal.ID); cErr == nil {
		inbreeding = coeff
	}

	// Apply inbreeding penalty to foal's fitness ceiling.
	inbreedingPenalty := pedigreussy.InbreedingPenalty(inbreeding)
	foal.FitnessCeiling *= inbreedingPenalty

	// Cap fitness ceiling.
	if foal.FitnessCeiling > 1.0 {
		foal.FitnessCeiling = 1.0
	}

	// Recompute current fitness (starts untrained at 50% of ceiling).
	foal.CurrentFitness = foal.FitnessCeiling * 0.5

	// Write-through: persist the new foal to DB.
	s.persistHorse(r.Context(), foal)

	// Set breeding cooldown on both parents.
	sire.LastBredAt = time.Now()
	mare.LastBredAt = time.Now()
	s.syncHorseToStable(sire)
	s.syncHorseToStable(mare)
	s.persistHorse(r.Context(), sire)
	s.persistHorse(r.Context(), mare)

	// Grant 50 prestige XP for breeding.
	if claims, ok := authussy.GetUserFromContext(r.Context()); ok {
		s.addPrestigeXP(claims.UserID, claims.Username, 50)
	}

	log.Printf("server: bred foal %q (%s) from sire %q and mare %q into stable %s (bloodline: %.4f)",
		foal.Name, foal.ID, sire.Name, mare.Name, req.StableID, bloodlineBonus)

	// Broadcast breeding event to all connected clients.
	breedingEvent := map[string]interface{}{
		"type": "breeding_event",
		"foal": map[string]interface{}{
			"id":       foal.ID,
			"name":     foal.Name,
			"sireName": sire.Name,
			"mareName": mare.Name,
		},
		"stableID": req.StableID,
		"lore":     foal.Lore,
	}
	if breedStable, err := s.stables.GetStable(req.StableID); err == nil {
		breedingEvent["stableName"] = breedStable.Name
	}
	s.hub.BroadcastJSON(breedingEvent)

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

	// Ownership check: the requesting player must own at least one horse
	// in the race. Other slots can be filled with AI / system horses or
	// horses from other stables (to allow vs-AI and multiplayer races).
	if claims, ok := authussy.GetUserFromContext(r.Context()); ok {
		ownsAtLeastOne := false
		for _, hid := range req.HorseIDs {
			if s.userOwnsHorse(claims.UserID, hid) {
				ownsAtLeastOne = true
				break
			}
		}
		if !ownsAtLeastOne {
			http.Error(w, "you must enter at least one of your own horses in the race", http.StatusForbidden)
			return
		}
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

	// Close any open betting pool for this race before simulation starts.
	s.closeBettingPool(race.ID)

	race = racussy.SimulateRaceWithWeather(race, horses, weather)

	// 3. Generate the indexed narrative with weather context.
	narrativeIndexed := racussy.GenerateRaceNarrativeIndexed(race, weather)
	// Also generate the plain string narrative for backward compatibility.
	narrative := make([]string, len(narrativeIndexed))
	for i, nl := range narrativeIndexed {
		narrative[i] = nl.Text
	}

	// 3b. Roll for random events (15% chance per race).
	randomEvent := rollRandomEvent(horses, race, weather)
	if randomEvent != nil {
		// Apply the event effects and add to narrative.
		eventNarrative := applyRandomEvent(randomEvent, horses, race, &purse)
		narrative = append(narrative, eventNarrative...)

		// Broadcast the random event via WebSocket.
		s.hub.BroadcastJSON(map[string]interface{}{
			"type":        "random_event",
			"event":       randomEvent,
			"raceID":      race.ID,
			"description": randomEvent.Description,
			"ts":          time.Now().Unix(),
		})
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
		eloDelta := eloDeltas[horse.ID]

		// Trait: elo_boost (e.g. ELO Farmer) — multiply positive ELO gains by magnitude.
		// Only applies to winners (positive delta).
		if eloDelta > 0 {
			if has, mag := hasTraitEffect(horse, "elo_boost"); has {
				eloDelta *= mag
			}
		}

		newELO := horse.ELO + eloDelta

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

		// Trait: earnings_boost (e.g. Cummies Magnet) — multiply earnings by magnitude.
		if earnings > 0 {
			if has, mag := hasTraitEffect(horse, "earnings_boost"); has {
				earnings = int64(float64(earnings) * mag)
			}
		}

		// Win streak bonus: horses on a winning streak earn more.
		if earnings > 0 {
			stats := s.raceHistory.GetHorseStats(horse.ID)
			// Streak is calculated BEFORE this race is recorded, so a win
			// here means the streak is stats.CurrentStreak (prior streak) + 1.
			streak := stats.CurrentStreak
			if entry.FinishPlace == 1 && streak > 0 {
				streak++ // this win extends the streak
			}
			if streak >= 2 {
				streakMult := winStreakMultiplier(streak)
				earnings = int64(float64(earnings) * streakMult)
			}
		}

		// Prestige bonus: multiply earnings by the owner's prestige CummiesBonus.
		if earnings > 0 {
			ownerTier := s.getPrestigeTierForUser(horse.OwnerID)
			if ownerTier.CummiesBonus > 1.0 {
				earnings = int64(float64(earnings) * ownerTier.CummiesBonus)
			}
		}

		horse.TotalEarnings += earnings

		// Add earnings to the horse's stable.
		if earnings > 0 {
			s.addEarningsToStable(horse, earnings)
		}

		// Grant prestige XP for racing: 100 XP for wins, 25 XP for participation.
		// Since runRace doesn't have request context, look up the owner from horse.
		raceXP := int64(25) // participation XP
		if entry.FinishPlace == 1 {
			raceXP = 100 // win XP
		}
		s.addPrestigeXPForHorse(horse, raceXP)

		// Apply post-race fatigue.
		fatigue := racussy.CalcPostRaceFatigue(horse, race, entry.FinishPlace, weather)
		horse.Fatigue += fatigue
		if horse.Fatigue > 100 {
			horse.Fatigue = 100
		}

		// Decrement injury cooldown if the horse has a non-career-ending injury.
		if horse.Injury != nil && horse.Injury.Severity != models.SeverityCareerEnding {
			horse.Injury.RacesLeft--
			if horse.Injury.RacesLeft <= 0 {
				// Injury healed naturally!
				narrative = append(narrative, fmt.Sprintf("💪 %s has fully recovered from %s!", horse.Name, horse.Injury.Type))
				horse.Injury = nil
			}
		}

		// Post-race injury check: base 3% + 5% for Elder/Ancient + 2% per 20 fatigue over 60.
		if horse.Injury == nil && !horse.Retired {
			injuryChance := 0.03
			ageBracket := getAgeBracket(horse.Age)
			if ageBracket == "Elder" || ageBracket == "Ancient" {
				injuryChance += 0.05
			}
			if horse.Fatigue > 60 {
				extraFatigue := (horse.Fatigue - 60) / 20.0
				injuryChance += extraFatigue * 0.02
			}
			if rand.Float64() < injuryChance {
				injury := rollInjury(horse)
				horse.Injury = injury
				narrative = append(narrative, fmt.Sprintf("🤕 INJURY: %s suffered a %s (%s)! %s",
					horse.Name, injury.Type, injury.Severity, injury.Description))

				// Career-ending injury forces retirement.
				if injury.Severity == models.SeverityCareerEnding {
					trainussy.RetireHorse(horse, fmt.Sprintf("Career-ending %s", injury.Type))
					narrative = append(narrative, fmt.Sprintf("💀 %s's career is OVER. Forced retirement due to %s.",
						horse.Name, injury.Type))
				}

				// Broadcast injury event.
				s.hub.BroadcastJSON(map[string]interface{}{
					"type":        "horse_injured",
					"horseName":   horse.Name,
					"horseID":     horse.ID,
					"injuryType":  string(injury.Type),
					"severity":    string(injury.Severity),
					"description": injury.Description,
					"ts":          time.Now().Unix(),
				})
			}
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

		// Write-through: persist race result to DB.
		ctx := context.Background()
		s.persistRaceResult(ctx, result)

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

		// Write-through: persist horse state to DB AFTER milestone traits are assigned,
		// so traits like "first_win" and "10_races" are persisted immediately.
		s.persistHorse(ctx, horse)
	}

	// Resolve any betting pool for this race. The winner is the first entry
	// in the sorted slice (FinishPlace == 1).
	if len(sorted) > 0 {
		s.resolveBets(race.ID, sorted[0].HorseID)
	}

	// Update rivalry records: the winner's count is incremented against each loser.
	if len(sorted) > 1 {
		winnerID := sorted[0].HorseID
		s.rivalryMu.Lock()
		if s.rivalries[winnerID] == nil {
			s.rivalries[winnerID] = make(map[string]int)
		}
		for _, entry := range sorted[1:] {
			s.rivalries[winnerID][entry.HorseID]++
		}
		s.rivalryMu.Unlock()
	}

	// Rivalry commentary: if two horses have raced 3+ times (combined head-to-head),
	// add a rivalry narrative line to the race commentary.
	if len(sorted) > 1 {
		winnerID := sorted[0].HorseID
		winnerName := ""
		if h := horseMap[winnerID]; h != nil {
			winnerName = h.Name
		}
		s.rivalryMu.RLock()
		for _, entry := range sorted[1:] {
			loserID := entry.HorseID
			loserName := ""
			if h := horseMap[loserID]; h != nil {
				loserName = h.Name
			}
			// Count total head-to-head encounters (both directions).
			h2h := s.rivalries[winnerID][loserID] + s.rivalries[loserID][winnerID]
			if h2h >= 3 && winnerName != "" && loserName != "" {
				rivalryLine := fmt.Sprintf("🔥 RIVALRY ALERT: %s and %s meet for the %dth time! %s takes this round.",
					winnerName, loserName, h2h, winnerName)
				narrative = append(narrative, rivalryLine)
			}
		}
		s.rivalryMu.RUnlock()
	}

	// Broadcast tick-by-tick replay to WebSocket clients.
	go s.broadcastRaceReplay(race, narrativeIndexed)

	log.Printf("server: race %s finished on %s (%dm) with %d entries, weather: %s",
		race.ID, race.TrackType, race.Distance, len(race.Entries), weather)

	result := raceResult{
		Race:             race,
		Narrative:        narrative,
		NarrativeIndexed: narrativeIndexed,
		Weather:          weather,
	}

	// Cache the full result for replay sharing.
	s.cacheRaceResult(race.ID, &result)

	return result
}

// addEarningsToStable finds the stable that owns a horse and adds earnings.
func (s *Server) addEarningsToStable(horse *models.Horse, earnings int64) {
	for _, stable := range s.stables.ListStables() {
		for _, h := range stable.Horses {
			if h.ID == horse.ID {
				stable.Cummies += earnings
				stable.TotalEarnings += earnings
				stable.TotalRaces++
				// Write-through: persist updated stable balance to DB.
				s.persistStable(context.Background(), stable)
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

// getStableForUser finds the stable owned by a given user ID.
// Returns nil if no stable is found for the user.
func (s *Server) getStableForUser(userID string) *models.Stable {
	for _, stable := range s.stables.ListStables() {
		if stable.OwnerID == userID {
			return stable
		}
	}
	return nil
}

// userOwnsHorse checks whether a user owns a specific horse by checking
// if the horse exists in the user's stable.
func (s *Server) userOwnsHorse(userID, horseID string) bool {
	stable := s.getStableForUser(userID)
	if stable == nil {
		return false
	}
	for _, h := range stable.Horses {
		if h.ID == horseID {
			return true
		}
	}
	return false
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

						// Write-through: persist achievement to DB.
						s.persistAchievement(context.Background(), stable.ID, &a)

						// Broadcast achievement unlock to all connected clients.
						s.hub.BroadcastJSON(map[string]interface{}{
							"type":        "achievement_unlocked",
							"stableName":  stable.Name,
							"stableID":    stable.ID,
							"achievement": a.ID,
							"name":        a.Name,
							"description": a.Description,
						})
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

// grantAchievementToStable grants an event-based achievement to a stable if
// the stable doesn't already have it. Used for achievements that can't be
// detected from horse/stable state alone (trades, challenges, bets, streaks).
func (s *Server) grantAchievementToStable(stable *models.Stable, achievementID string) {
	if stable == nil {
		return
	}
	if hasAchievement(stable.Achievements, achievementID) {
		return
	}
	def, ok := tournussy.AllAchievements[achievementID]
	if !ok {
		return
	}
	a := def
	a.UnlockedAt = time.Now()
	stable.Achievements = append(stable.Achievements, a)

	log.Printf("server: achievement unlocked for stable %s: %s (%s)",
		stable.Name, a.Name, a.Description)

	// Write-through: persist achievement to DB.
	s.persistAchievement(context.Background(), stable.ID, &a)

	// Broadcast achievement unlock to all connected clients.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":        "achievement_unlocked",
		"stableName":  stable.Name,
		"stableID":    stable.ID,
		"achievement": a.ID,
		"name":        a.Name,
		"description": a.Description,
	})
}

// maxRaceCacheSize limits the number of races we keep in memory for replay.
const maxRaceCacheSize = 200

// cacheRaceResult stores a race result for later replay retrieval.
// Older entries are evicted when the cache exceeds maxRaceCacheSize.
// Also persists the result to the database for long-term storage.
func (s *Server) cacheRaceResult(raceID string, result *raceResult) {
	s.raceCacheMu.Lock()
	defer s.raceCacheMu.Unlock()

	s.raceCache[raceID] = result

	// Evict oldest if we exceed the cap. Since Go maps are unordered we just
	// trim randomly — for a production system you'd want an LRU, but this is
	// fine for StallionUSSY's scale.
	if len(s.raceCache) > maxRaceCacheSize {
		for id := range s.raceCache {
			if id != raceID {
				delete(s.raceCache, id)
				break
			}
		}
	}

	// Persist to DB for long-term storage.
	s.persistRaceReplay(raceID, result)
}

// handleGetRaceReplay returns a cached race result for replay.
// Falls back to the database if not found in the in-memory cache.
func (s *Server) handleGetRaceReplay(w http.ResponseWriter, r *http.Request) {
	raceID := r.PathValue("id")

	// First, check in-memory cache.
	s.raceCacheMu.RLock()
	result, ok := s.raceCache[raceID]
	s.raceCacheMu.RUnlock()

	if ok {
		writeJSON(w, http.StatusOK, result)
		return
	}

	// Fallback: try loading from the database.
	if s.replayRepo != nil {
		replay, err := s.replayRepo.GetReplay(r.Context(), raceID)
		if err == nil && replay != nil && len(replay.Data) > 0 {
			var dbResult raceResult
			if err := json.Unmarshal(replay.Data, &dbResult); err == nil {
				// Re-cache for future hits.
				s.cacheRaceResult(raceID, &dbResult)
				writeJSON(w, http.StatusOK, &dbResult)
				return
			}
		}
	}

	writeError(w, http.StatusNotFound, "race not found or expired")
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

			// Write-through: persist achievement to DB.
			s.persistAchievement(r.Context(), stable.ID, &a)

			// Broadcast achievement unlock to all connected clients.
			s.hub.BroadcastJSON(map[string]interface{}{
				"type":        "achievement_unlocked",
				"stableName":  stable.Name,
				"stableID":    stable.ID,
				"achievement": a.ID,
				"name":        a.Name,
				"description": a.Description,
			})
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

	// Ownership check: only the horse's owner can list it.
	if claims, ok := authussy.GetUserFromContext(r.Context()); ok {
		if !s.userOwnsHorse(claims.UserID, req.HorseID) {
			http.Error(w, "you do not own this horse", http.StatusForbidden)
			return
		}
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

	// Write-through: persist listing to DB.
	s.persistListing(r.Context(), listing)

	// Broadcast new market listing to all connected clients.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":   "market_update",
		"action": "listed",
		"listing": map[string]interface{}{
			"id":        listing.ID,
			"horseName": listing.HorseName,
			"ownerID":   listing.OwnerID,
			"price":     listing.Price,
		},
	})

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

	// If authenticated, resolve buyerStableID from JWT and verify ownership.
	if claims, ok := authussy.GetUserFromContext(r.Context()); ok {
		if req.BuyerStableID == "" {
			// Auto-resolve: find the stable owned by this user.
			for _, stable := range s.stables.ListStables() {
				if stable.OwnerID == claims.UserID {
					req.BuyerStableID = stable.ID
					break
				}
			}
			if req.BuyerStableID == "" {
				writeError(w, http.StatusBadRequest, "no stable found for authenticated user")
				return
			}
		} else {
			// Client sent a buyerStableID — verify the authenticated user owns it.
			buyerStable, err := s.stables.GetStable(req.BuyerStableID)
			if err != nil {
				writeError(w, http.StatusNotFound, "buyer stable not found: "+err.Error())
				return
			}
			if buyerStable.OwnerID != claims.UserID {
				writeError(w, http.StatusForbidden, "buyer stable does not belong to authenticated user")
				return
			}
		}
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

	// Write-through: persist updated balances for both buyer and seller stables.
	if updatedBuyer, err := s.stables.GetStable(req.BuyerStableID); err == nil {
		s.persistStable(r.Context(), updatedBuyer)
	}
	if updatedSeller, err := s.stables.GetStable(sellerStableID); err == nil {
		s.persistStable(r.Context(), updatedSeller)
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

	// Write-through: persist the new foal and update the listing (now inactive).
	s.persistHorse(r.Context(), foal)
	if s.marketRepo != nil {
		// The listing is no longer active after purchase.
		listing.Active = false
		if err := s.marketRepo.UpdateListing(r.Context(), listing); err != nil {
			log.Printf("server: failed to update listing %s after purchase: %v", listing.ID, err)
		}
	}

	tx.FoalID = foal.ID

	// Write-through: persist the market transaction to DB.
	s.persistMarketTransaction(r.Context(), tx)

	log.Printf("server: stud purchase — %s bought breeding from %s, foal %q (%s) for %d cummies (burned %d)",
		buyerStable.OwnerID, listing.OwnerID, foal.Name, foal.ID, listing.Price, tx.BurnAmount)

	// Broadcast market purchase to all connected clients.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":   "market_update",
		"action": "purchased",
		"listing": map[string]interface{}{
			"id":        listing.ID,
			"horseName": listing.HorseName,
			"ownerID":   listing.OwnerID,
			"price":     listing.Price,
		},
		"buyerStable": buyerStable.Name,
		"foalName":    foal.Name,
	})

	// Broadcast balance updates for buyer and seller stables.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":       "balance_update",
		"stableID":   buyerStable.ID,
		"newBalance": buyerStable.Cummies,
	})
	if sellerStable, err := s.stables.GetStable(sellerStableID); err == nil {
		s.hub.BroadcastJSON(map[string]interface{}{
			"type":       "balance_update",
			"stableID":   sellerStable.ID,
			"newBalance": sellerStable.Cummies,
		})
	}

	// Grant 30 prestige XP to the seller for the market sale.
	if listing.OwnerID != "" && listing.OwnerID != "system" {
		sellerName := listing.OwnerID
		if ss, err := s.stables.GetStable(sellerStableID); err == nil {
			sellerName = ss.Name
		}
		s.addPrestigeXP(listing.OwnerID, sellerName, 30)
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"transaction": tx,
		"foal":        foal,
	})
}

func (s *Server) handleDelistListing(w http.ResponseWriter, r *http.Request) {
	listingID := r.PathValue("id")

	// Look up the listing first so we can verify ownership.
	listing, err := s.market.GetListing(listingID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	// Determine the ownerID: prefer JWT-authenticated user over client-sent value.
	ownerID := ""
	if claims, ok := authussy.GetUserFromContext(r.Context()); ok {
		// Authenticated: verify the listing belongs to this user.
		// The listing's OwnerID should match the JWT user's ID, or the user
		// should own the stable that owns the listed horse.
		ownerID = claims.UserID
		if listing.OwnerID != ownerID {
			// Check if the user owns a stable that matches the listing's ownerID.
			ownerMatch := false
			for _, stable := range s.stables.ListStables() {
				if stable.OwnerID == claims.UserID && stable.OwnerID == listing.OwnerID {
					ownerMatch = true
					break
				}
			}
			if !ownerMatch {
				writeError(w, http.StatusForbidden, "you do not own this listing")
				return
			}
		}
	} else {
		// Guest mode fallback: read ownerID from query param or body.
		ownerID = r.URL.Query().Get("ownerID")
		if ownerID == "" {
			var req struct {
				OwnerID string `json:"ownerID"`
			}
			if err := readJSON(r, &req); err == nil && req.OwnerID != "" {
				ownerID = req.OwnerID
			}
		}
		// If ownerID still empty, default to listing owner for backward compat.
		if ownerID == "" {
			ownerID = listing.OwnerID
		}
	}

	if err := s.market.DelistStud(listingID, ownerID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Write-through: mark listing inactive in DB.
	if s.marketRepo != nil {
		listing.Active = false
		if err := s.marketRepo.UpdateListing(r.Context(), listing); err != nil {
			log.Printf("server: failed to update listing %s after delist: %v", listingID, err)
		}
	}

	log.Printf("server: delisted stud listing %s for horse %s", listingID, listing.HorseID)

	// Broadcast delist event to all connected clients.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":      "market_update",
		"action":    "delisted",
		"listingID": listingID,
	})

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

	// Write-through: persist tournament to DB.
	s.persistTournament(r.Context(), tournament)

	// Broadcast tournament creation via WebSocket.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":   "tournament_update",
		"action": "created",
		"tournament": map[string]interface{}{
			"id":        tournament.ID,
			"name":      tournament.Name,
			"entryFee":  tournament.EntryFee,
			"prizePool": tournament.PrizePool,
			"trackType": tournament.TrackType,
			"rounds":    tournament.Rounds,
			"status":    tournament.Status,
		},
	})

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

	// Look up the tournament to check entry fee.
	tournament, err := s.tournaments.GetTournament(tournamentID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
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

	// Collect entry fee if the tournament has one.
	if tournament.EntryFee > 0 {
		// Find the registering user's stable. Prefer JWT-authenticated user,
		// fall back to the stableID in the request.
		var payingStable *models.Stable
		if claims, ok := authussy.GetUserFromContext(r.Context()); ok {
			payingStable = s.getStableForUser(claims.UserID)
		}
		if payingStable == nil {
			// Fall back to the stableID provided in the request.
			payingStable, _ = s.stables.GetStable(req.StableID)
		}
		if payingStable == nil {
			writeError(w, http.StatusBadRequest, "stable not found for entry fee payment")
			return
		}

		// Check sufficient funds.
		if payingStable.Cummies < tournament.EntryFee {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("insufficient cummies: have %d, need %d entry fee", payingStable.Cummies, tournament.EntryFee))
			return
		}

		// Deduct entry fee from stable.
		payingStable.Cummies -= tournament.EntryFee

		// Accumulate into tournament prize pool.
		tournament.PrizePool += tournament.EntryFee

		// Persist the stable after deduction.
		s.persistStable(r.Context(), payingStable)

		log.Printf("server: collected %d cummies entry fee from stable %s (%s) for tournament %s",
			tournament.EntryFee, payingStable.Name, payingStable.ID, tournament.Name)
	}

	if err := s.tournaments.RegisterHorse(tournamentID, horse, req.StableID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Write-through: persist updated tournament (with new registration + prize pool) to DB.
	if updatedTournament, err := s.tournaments.GetTournament(tournamentID); err == nil {
		s.persistTournament(r.Context(), updatedTournament)
	}

	// Broadcast horse registration via WebSocket.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":   "tournament_update",
		"action": "horse_registered",
		"tournament": map[string]interface{}{
			"id":        tournament.ID,
			"name":      tournament.Name,
			"entryFee":  tournament.EntryFee,
			"prizePool": tournament.PrizePool,
			"status":    tournament.Status,
		},
		"horse": map[string]interface{}{
			"id":       horse.ID,
			"name":     horse.Name,
			"stableID": req.StableID,
		},
	})

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"status":       "registered",
		"tournamentID": tournamentID,
		"horseID":      req.HorseID,
		"entryFeePaid": tournament.EntryFee,
		"prizePool":    tournament.PrizePool,
	})
}

func (s *Server) handleTournamentRace(w http.ResponseWriter, r *http.Request) {
	tournamentID := r.PathValue("id")

	// Auth note: any authenticated user can trigger tournament races for now.
	// Organizer-only check will be added in a future update.

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

	// Auto-open a betting pool for this tournament race so spectators
	// who have been waiting can place bets. In a future async implementation,
	// this would have a timed betting window. For now, the pool is opened
	// and immediately closed before simulation.
	s.openBettingPool(race.ID, horses)

	// Generate weather.
	weather := tournussy.RandomWeatherForTrack(tournament.TrackType)

	// Close betting pool before simulation starts.
	s.closeBettingPool(race.ID)

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

	// Resolve betting pool for this tournament race.
	for _, entry := range race.Entries {
		if entry.FinishPlace == 1 {
			s.resolveBets(race.ID, entry.HorseID)
			break
		}
	}

	// Write-through: persist tournament state after round.
	updatedTournament, _ := s.tournaments.GetTournament(tournamentID)
	if updatedTournament != nil {
		s.persistTournament(r.Context(), updatedTournament)
	}

	// Broadcast replay.
	go s.broadcastRaceReplay(race, narrativeIndexed)

	// Cache for replay sharing.
	s.cacheRaceResult(race.ID, &raceResult{
		Race:             race,
		Narrative:        narrative,
		NarrativeIndexed: narrativeIndexed,
		Weather:          weather,
	})

	// Get updated standings.
	standings := s.tournaments.GetStandings(tournamentID)

	// Broadcast round completion via WebSocket.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":   "tournament_update",
		"action": "round_complete",
		"tournament": map[string]interface{}{
			"id":           tournament.ID,
			"name":         tournament.Name,
			"currentRound": updatedTournament.CurrentRound,
			"totalRounds":  updatedTournament.Rounds,
			"status":       updatedTournament.Status,
			"prizePool":    updatedTournament.PrizePool,
		},
		"raceID":    race.ID,
		"weather":   weather,
		"standings": standings,
	})

	// If tournament is now finished, distribute the prize pool.
	if updatedTournament != nil && updatedTournament.Status == "Finished" {
		s.distributeTournamentPrizes(r.Context(), updatedTournament, standings)
	}

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
		eloDelta := eloDeltas[horse.ID]

		// Trait: elo_boost (e.g. ELO Farmer) — multiply positive ELO gains by magnitude.
		if eloDelta > 0 {
			if has, mag := hasTraitEffect(horse, "elo_boost"); has {
				eloDelta *= mag
			}
		}

		newELO := horse.ELO + eloDelta

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

		// Trait: earnings_boost (e.g. Cummies Magnet) — multiply earnings by magnitude.
		if earnings > 0 {
			if has, mag := hasTraitEffect(horse, "earnings_boost"); has {
				earnings = int64(float64(earnings) * mag)
			}
		}

		// Win streak bonus: horses on a winning streak earn more.
		if earnings > 0 {
			stats := s.raceHistory.GetHorseStats(horse.ID)
			streak := stats.CurrentStreak
			if entry.FinishPlace == 1 && streak > 0 {
				streak++
			}
			if streak >= 2 {
				earnings = int64(float64(earnings) * winStreakMultiplier(streak))
			}
		}

		// Prestige bonus: multiply earnings by the owner's prestige CummiesBonus.
		if earnings > 0 {
			ownerTier := s.getPrestigeTierForUser(horse.OwnerID)
			if ownerTier.CummiesBonus > 1.0 {
				earnings = int64(float64(earnings) * ownerTier.CummiesBonus)
			}
		}

		horse.TotalEarnings += earnings

		if earnings > 0 {
			s.addEarningsToStable(horse, earnings)
		}

		// Grant prestige XP: 100 XP for wins, 25 XP for participation.
		raceXP := int64(25)
		if entry.FinishPlace == 1 {
			raceXP = 100
		}
		s.addPrestigeXPForHorse(horse, raceXP)

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

		// Write-through: persist race result and horse state to DB.
		ctx := context.Background()
		s.persistRaceResult(ctx, result)
		s.persistHorse(ctx, horse)

		s.syncHorseToStable(horse)
		s.checkAndApplyAchievements(horse)
	}
}

// distributeTournamentPrizes distributes the tournament prize pool to the top 3
// finishers and burns 5%. Called when a tournament transitions to "Finished".
//
// Prize distribution: 1st=60%, 2nd=25%, 3rd=10%, burned=5%.
func (s *Server) distributeTournamentPrizes(ctx context.Context, tournament *models.Tournament, standings []models.TournamentEntry) {
	if tournament.PrizePool <= 0 || len(standings) == 0 {
		return
	}

	prizePool := tournament.PrizePool
	firstPrize := int64(float64(prizePool) * 0.60)
	secondPrize := int64(float64(prizePool) * 0.25)
	thirdPrize := int64(float64(prizePool) * 0.10)
	burnAmount := prizePool - firstPrize - secondPrize - thirdPrize // ~5%, absorbs rounding

	type prizeInfo struct {
		place    int
		stableID string
		name     string
		amount   int64
	}
	var prizes []prizeInfo

	// Helper: find a stable by ID.
	findStable := func(stableID string) *models.Stable {
		st, err := s.stables.GetStable(stableID)
		if err != nil {
			return nil
		}
		return st
	}

	// Award prizes to top 3 (or fewer if less than 3 entries).
	prizeAmounts := []int64{firstPrize, secondPrize, thirdPrize}
	for i := 0; i < len(standings) && i < 3; i++ {
		entry := standings[i]
		stable := findStable(entry.StableID)
		if stable == nil {
			log.Printf("server: tournament prize — stable %s not found for horse %s, skipping prize",
				entry.StableID, entry.HorseName)
			continue
		}

		prize := prizeAmounts[i]
		stable.Cummies += prize
		stable.TotalEarnings += prize
		s.persistStable(ctx, stable)

		prizes = append(prizes, prizeInfo{
			place:    i + 1,
			stableID: stable.ID,
			name:     stable.Name,
			amount:   prize,
		})

		// Grant prestige XP for tournament placement: 500 XP for 1st, 250 for 2nd, 100 for 3rd.
		tournXP := int64(0)
		switch i {
		case 0:
			tournXP = 500
		case 1:
			tournXP = 250
		case 2:
			tournXP = 100
		}
		if tournXP > 0 {
			s.addPrestigeXP(stable.OwnerID, stable.Name, tournXP)
		}

		log.Printf("server: tournament %s — %s place: %s (%s) receives %d cummies",
			tournament.Name, ordinal(i+1), stable.Name, stable.ID, prize)
	}

	log.Printf("server: tournament %s — %d cummies burned (5%% tax)", tournament.Name, burnAmount)

	// Build WS broadcast payload.
	wsPrizes := make([]map[string]interface{}, len(prizes))
	for i, p := range prizes {
		wsPrizes[i] = map[string]interface{}{
			"place":      p.place,
			"stableID":   p.stableID,
			"stableName": p.name,
			"amount":     p.amount,
		}
	}

	s.hub.BroadcastJSON(map[string]interface{}{
		"type":           "tournament_prize",
		"tournamentID":   tournament.ID,
		"tournamentName": tournament.Name,
		"prizePool":      prizePool,
		"prizes":         wsPrizes,
		"burned":         burnAmount,
	})

	// Also broadcast the tournament_finished event.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":   "tournament_update",
		"action": "finished",
		"tournament": map[string]interface{}{
			"id":        tournament.ID,
			"name":      tournament.Name,
			"prizePool": prizePool,
			"status":    tournament.Status,
		},
	})
}

// ordinal returns the English ordinal suffix for an integer (1st, 2nd, 3rd, etc.).
func ordinal(n int) string {
	switch n {
	case 1:
		return "1st"
	case 2:
		return "2nd"
	case 3:
		return "3rd"
	default:
		return fmt.Sprintf("%dth", n)
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

	// If authenticated, verify the user owns the source stable (fromStable).
	if claims, ok := authussy.GetUserFromContext(r.Context()); ok {
		fromStable, err := s.stables.GetStable(req.FromStable)
		if err != nil {
			writeError(w, http.StatusNotFound, "source stable not found: "+err.Error())
			return
		}
		if fromStable.OwnerID != claims.UserID {
			writeError(w, http.StatusForbidden, "you do not own the source stable")
			return
		}
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

	// Write-through: persist the new trade offer to DB.
	if s.tradeRepo != nil {
		if err := s.tradeRepo.CreateTrade(r.Context(), offer); err != nil {
			log.Printf("server: failed to persist trade offer %s: %v", offer.ID, err)
		}
	}

	// Broadcast trade creation to all connected clients.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":   "trade_update",
		"action": "created",
		"trade": map[string]interface{}{
			"id":           offer.ID,
			"fromStableID": offer.FromStableID,
			"toStableID":   offer.ToStableID,
			"horseName":    offer.HorseName,
			"cummies":      offer.Price,
		},
	})

	writeJSON(w, http.StatusCreated, offer)
}

func (s *Server) handleAcceptTrade(w http.ResponseWriter, r *http.Request) {
	tradeID := r.PathValue("id")

	// If authenticated, verify the user owns the receiving stable (ToStable).
	if claims, ok := authussy.GetUserFromContext(r.Context()); ok {
		offer, err := s.trades.GetOffer(tradeID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		toStable, err := s.stables.GetStable(offer.ToStableID)
		if err != nil {
			writeError(w, http.StatusNotFound, "destination stable not found: "+err.Error())
			return
		}
		if toStable.OwnerID != claims.UserID {
			writeError(w, http.StatusForbidden, "only the recipient stable owner can accept this trade")
			return
		}
	}

	offer, err := s.trades.AcceptOffer(tradeID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Execute the transfer: move horse and transfer Cummies.
	if offer.Price > 0 {
		if err := s.stables.TransferCummies(offer.ToStableID, offer.FromStableID, offer.Price); err != nil {
			writeError(w, http.StatusBadRequest, "payment failed: "+err.Error())
			return
		}
		// Write-through: persist updated balances for both stables after cummies transfer.
		if updatedTo, err := s.stables.GetStable(offer.ToStableID); err == nil {
			s.persistStable(r.Context(), updatedTo)
		}
		if updatedFrom, err := s.stables.GetStable(offer.FromStableID); err == nil {
			s.persistStable(r.Context(), updatedFrom)
		}
	}

	if err := s.stables.MoveHorse(offer.HorseID, offer.FromStableID, offer.ToStableID); err != nil {
		writeError(w, http.StatusInternalServerError, "transfer failed: "+err.Error())
		return
	}

	// Write-through: persist horse move in DB.
	if s.horseRepo != nil {
		ctx := r.Context()
		if err := s.horseRepo.MoveHorse(ctx, offer.HorseID, offer.FromStableID, offer.ToStableID); err != nil {
			log.Printf("server: failed to persist horse move for trade %s: %v", tradeID, err)
		}
	}

	log.Printf("server: trade accepted — horse %s moved from stable %s to %s for %d cummies",
		offer.HorseName, offer.FromStableID, offer.ToStableID, offer.Price)

	// Write-through: persist updated trade status to DB.
	if s.tradeRepo != nil {
		offer.UpdatedAt = time.Now()
		if err := s.tradeRepo.UpdateTrade(r.Context(), offer); err != nil {
			log.Printf("server: failed to persist trade accept for %s: %v", tradeID, err)
		}
	}

	// Broadcast trade acceptance to all connected clients.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":   "trade_update",
		"action": "accepted",
		"trade": map[string]interface{}{
			"id":           offer.ID,
			"fromStableID": offer.FromStableID,
			"toStableID":   offer.ToStableID,
			"horseName":    offer.HorseName,
			"cummies":      offer.Price,
		},
	})

	// Broadcast balance updates for both stables involved in the trade.
	if offer.Price > 0 {
		if updatedTo, err := s.stables.GetStable(offer.ToStableID); err == nil {
			s.hub.BroadcastJSON(map[string]interface{}{
				"type":       "balance_update",
				"stableID":   updatedTo.ID,
				"newBalance": updatedTo.Cummies,
			})
		}
		if updatedFrom, err := s.stables.GetStable(offer.FromStableID); err == nil {
			s.hub.BroadcastJSON(map[string]interface{}{
				"type":       "balance_update",
				"stableID":   updatedFrom.ID,
				"newBalance": updatedFrom.Cummies,
			})
		}
	}

	// Grant first_trade achievement to both stables involved.
	if fromStable, err := s.stables.GetStable(offer.FromStableID); err == nil {
		s.grantAchievementToStable(fromStable, "first_trade")
	}
	if toStable, err := s.stables.GetStable(offer.ToStableID); err == nil {
		s.grantAchievementToStable(toStable, "first_trade")
	}

	writeJSON(w, http.StatusOK, offer)
}

func (s *Server) handleRejectTrade(w http.ResponseWriter, r *http.Request) {
	tradeID := r.PathValue("id")

	// Look up the trade first to verify ownership.
	offer, err := s.trades.GetOffer(tradeID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	// Ownership check: only the recipient (ToStableID) can reject a trade.
	if claims, ok := authussy.GetUserFromContext(r.Context()); ok {
		userStable := s.getStableForUser(claims.UserID)
		if userStable == nil || userStable.ID != offer.ToStableID {
			http.Error(w, "only the trade recipient can reject this trade", http.StatusForbidden)
			return
		}
	}

	if err := s.trades.RejectOffer(tradeID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Write-through: persist updated trade status to DB.
	if s.tradeRepo != nil {
		if offer, err := s.trades.GetOffer(tradeID); err == nil {
			offer.UpdatedAt = time.Now()
			if err := s.tradeRepo.UpdateTrade(r.Context(), offer); err != nil {
				log.Printf("server: failed to persist trade reject for %s: %v", tradeID, err)
			}
		}
	}

	// Broadcast trade rejection to all connected clients.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":   "trade_update",
		"action": "rejected",
		"trade": map[string]interface{}{
			"id":           offer.ID,
			"fromStableID": offer.FromStableID,
			"toStableID":   offer.ToStableID,
			"horseName":    offer.HorseName,
			"cummies":      offer.Price,
		},
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected", "tradeID": tradeID})
}

func (s *Server) handleCancelTrade(w http.ResponseWriter, r *http.Request) {
	tradeID := r.PathValue("id")

	// Look up the trade first to verify ownership.
	offer, err := s.trades.GetOffer(tradeID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	// Ownership check: only the sender (FromStableID) can cancel a trade.
	if claims, ok := authussy.GetUserFromContext(r.Context()); ok {
		userStable := s.getStableForUser(claims.UserID)
		if userStable == nil || userStable.ID != offer.FromStableID {
			http.Error(w, "only the trade sender can cancel this trade", http.StatusForbidden)
			return
		}
	}

	if err := s.trades.CancelOffer(tradeID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Write-through: persist updated trade status to DB.
	if s.tradeRepo != nil {
		if offer, err := s.trades.GetOffer(tradeID); err == nil {
			offer.UpdatedAt = time.Now()
			if err := s.tradeRepo.UpdateTrade(r.Context(), offer); err != nil {
				log.Printf("server: failed to persist trade cancel for %s: %v", tradeID, err)
			}
		}
	}

	// Broadcast trade cancellation to all connected clients.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":   "trade_update",
		"action": "cancelled",
		"trade": map[string]interface{}{
			"id":           offer.ID,
			"fromStableID": offer.FromStableID,
			"toStableID":   offer.ToStableID,
			"horseName":    offer.HorseName,
			"cummies":      offer.Price,
		},
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled", "tradeID": tradeID})
}

// ===========================================================================
// Season / Aging handler
// ===========================================================================

func (s *Server) handleAdvanceSeason(w http.ResponseWriter, r *http.Request) {
	// Admin check: only the admin user "mojo" can advance the season.
	if claims, ok := authussy.GetUserFromContext(r.Context()); ok {
		if claims.Username != "mojo" {
			http.Error(w, "admin only", http.StatusForbidden)
			return
		}
	}

	// Parse optional season from request body. Default to 0 (any season).
	var req struct {
		Season int `json:"season"`
	}
	// Ignore decode errors — body may be empty or missing; season defaults to 0.
	_ = readJSON(r, &req)

	// Roll a seasonal event for this season.
	event := trainussy.RollSeasonalEvent(req.Season)

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
	var eventMessages []string

	for _, horse := range allHorses {
		if horse.Retired {
			continue
		}

		trainussy.AgeHorse(horse)

		// Apply seasonal event effects to non-retired horses.
		if event != nil {
			if msg := trainussy.ApplySeasonalEffect(event, horse); msg != "" {
				eventMessages = append(eventMessages, msg)
			}
		}

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

		// Write-through: persist aged horse state to DB.
		s.persistHorse(r.Context(), horse)
	}

	// Build response payload.
	response := map[string]interface{}{
		"season":  "advanced",
		"results": results,
	}

	// Include the seasonal event in the response if one was rolled.
	if event != nil {
		eventData := map[string]interface{}{
			"name":        event.Name,
			"description": event.Description,
			"effect":      event.Effect,
		}
		response["seasonalEvent"] = eventData

		if len(eventMessages) > 0 {
			response["seasonalEventMessages"] = eventMessages
		}

		// Broadcast the seasonal event via WebSocket if the hub is available.
		if s.hub != nil {
			s.hub.BroadcastJSON(map[string]interface{}{
				"type": "seasonal_event",
				"data": eventData,
			})
		}
	}

	writeJSON(w, http.StatusOK, response)
}

// ===========================================================================
// Leaderboard & Season handlers
// ===========================================================================

// seasonNames is a pool of fun names for competitive seasons. The season
// number is used as an index (mod length) so names cycle deterministically.
var seasonNames = []string{
	"The Ussening",
	"Cummy Thunder",
	"Hooves of Fury",
	"Mane Event",
	"The Great Gallop",
	"Stallion Showdown",
	"Legendary Stampede",
	"Breeding Frenzy",
	"Track Terror",
	"Foal Play",
	"Neigh Sayers",
	"The Stud Games",
	"Bridle Royale",
	"Gallop Gala",
	"Triple Crown Chaos",
	"Pedigree Pandemonium",
	"Trot of War",
	"Saddled Up",
	"Furlong Frenzy",
	"The Derby of Doom",
}

// generateSeasonName returns a fun season name like "Season 3: Hooves of Fury".
func generateSeasonName(seasonNumber int) string {
	name := seasonNames[(seasonNumber-1)%len(seasonNames)]
	return fmt.Sprintf("Season %d: %s", seasonNumber, name)
}

// handleGetLeaderboard returns a ranked list of stables sorted by the
// requested metric. Query params: sort (elo|wins|earnings|winrate|streak),
// limit (default 20).
func (s *Server) handleGetLeaderboard(w http.ResponseWriter, r *http.Request) {
	sortBy := r.URL.Query().Get("sort")
	if sortBy == "" {
		sortBy = "elo"
	}
	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}

	allStables := s.stables.ListStables()
	entries := make([]models.LeaderboardEntry, 0, len(allStables))

	for _, stable := range allStables {
		var (
			totalWins   int
			totalLosses int
			totalRaces  int
			totalELO    float64
			earnings    int64
			bestHorse   string
			bestELO     float64
			bestStreak  int
		)

		for _, h := range stable.Horses {
			totalWins += h.Wins
			totalLosses += h.Losses
			totalRaces += h.Races
			totalELO += h.ELO
			earnings += h.TotalEarnings

			if h.ELO > bestELO {
				bestELO = h.ELO
				bestHorse = h.Name
			}

			stats := s.raceHistory.GetHorseStats(h.ID)
			if stats.CurrentStreak > bestStreak {
				bestStreak = stats.CurrentStreak
			}
		}

		avgELO := 0.0
		if len(stable.Horses) > 0 {
			avgELO = totalELO / float64(len(stable.Horses))
		}

		winRate := 0.0
		if totalRaces > 0 {
			winRate = float64(totalWins) / float64(totalRaces)
		}

		entries = append(entries, models.LeaderboardEntry{
			StableID:   stable.ID,
			StableName: stable.Name,
			OwnerName:  stable.OwnerID,
			ELO:        int(avgELO),
			Wins:       totalWins,
			Losses:     totalLosses,
			WinRate:    winRate,
			TotalRaces: totalRaces,
			Earnings:   earnings,
			BestHorse:  bestHorse,
			BestELO:    int(bestELO),
			Streak:     bestStreak,
		})
	}

	switch sortBy {
	case "wins":
		sort.Slice(entries, func(i, j int) bool { return entries[i].Wins > entries[j].Wins })
	case "earnings":
		sort.Slice(entries, func(i, j int) bool { return entries[i].Earnings > entries[j].Earnings })
	case "winrate":
		sort.Slice(entries, func(i, j int) bool { return entries[i].WinRate > entries[j].WinRate })
	case "streak":
		sort.Slice(entries, func(i, j int) bool { return entries[i].Streak > entries[j].Streak })
	default:
		sort.Slice(entries, func(i, j int) bool { return entries[i].ELO > entries[j].ELO })
	}

	if len(entries) > limit {
		entries = entries[:limit]
	}

	for i := range entries {
		entries[i].Rank = i + 1
	}

	writeJSON(w, http.StatusOK, entries)
}

// handleGetHorseLeaderboard returns a ranked list of individual horses.
// Query params: sort (elo|wins|earnings), limit (default 20).
func (s *Server) handleGetHorseLeaderboard(w http.ResponseWriter, r *http.Request) {
	sortBy := r.URL.Query().Get("sort")
	if sortBy == "" {
		sortBy = "elo"
	}
	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}

	allStables := s.stables.ListStables()
	entries := make([]models.HorseLeaderboardEntry, 0)

	for _, stable := range allStables {
		for _, h := range stable.Horses {
			stats := s.raceHistory.GetHorseStats(h.ID)

			winRate := 0.0
			if h.Races > 0 {
				winRate = float64(h.Wins) / float64(h.Races)
			}

			entries = append(entries, models.HorseLeaderboardEntry{
				HorseID:    h.ID,
				HorseName:  h.Name,
				StableID:   stable.ID,
				StableName: stable.Name,
				ELO:        int(h.ELO),
				Wins:       h.Wins,
				Losses:     h.Losses,
				WinRate:    winRate,
				TotalRaces: h.Races,
				Earnings:   h.TotalEarnings,
				Streak:     stats.CurrentStreak,
			})
		}
	}

	switch sortBy {
	case "wins":
		sort.Slice(entries, func(i, j int) bool { return entries[i].Wins > entries[j].Wins })
	case "earnings":
		sort.Slice(entries, func(i, j int) bool { return entries[i].Earnings > entries[j].Earnings })
	default:
		sort.Slice(entries, func(i, j int) bool { return entries[i].ELO > entries[j].ELO })
	}

	if len(entries) > limit {
		entries = entries[:limit]
	}

	for i := range entries {
		entries[i].Rank = i + 1
	}

	writeJSON(w, http.StatusOK, entries)
}

// handleListSeasons returns all past seasons plus the current one.
func (s *Server) handleListSeasons(w http.ResponseWriter, r *http.Request) {
	s.seasonMu.RLock()
	defer s.seasonMu.RUnlock()

	all := make([]models.Season, 0, len(s.pastSeasons)+1)
	all = append(all, s.pastSeasons...)
	if s.currentSeason != nil {
		all = append(all, *s.currentSeason)
	}

	writeJSON(w, http.StatusOK, all)
}

// handleGetCurrentSeason returns the current active season.
func (s *Server) handleGetCurrentSeason(w http.ResponseWriter, r *http.Request) {
	s.seasonMu.RLock()
	defer s.seasonMu.RUnlock()

	if s.currentSeason == nil {
		writeError(w, http.StatusNotFound, "no active season")
		return
	}

	writeJSON(w, http.StatusOK, s.currentSeason)
}

// handleEndSeason ends the current competitive season. Admin only (username == "mojo").
// It archives the season with top 10 champions, distributes cummies rewards,
// soft-resets ELO (50% toward 1200 baseline), and starts a new season.
func (s *Server) handleEndSeason(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok || claims.Username != "mojo" {
		writeError(w, http.StatusForbidden, "admin only")
		return
	}

	s.seasonMu.Lock()
	defer s.seasonMu.Unlock()

	if s.currentSeason == nil || !s.currentSeason.Active {
		writeError(w, http.StatusBadRequest, "no active season to end")
		return
	}

	allStables := s.stables.ListStables()

	type stableRank struct {
		stable   *models.Stable
		avgELO   float64
		wins     int
		earnings int64
	}
	ranks := make([]stableRank, 0, len(allStables))

	for _, stable := range allStables {
		var totalELO float64
		var wins int
		var earnings int64
		for _, h := range stable.Horses {
			totalELO += h.ELO
			wins += h.Wins
			earnings += h.TotalEarnings
		}
		avgELO := 0.0
		if len(stable.Horses) > 0 {
			avgELO = totalELO / float64(len(stable.Horses))
		}
		ranks = append(ranks, stableRank{
			stable:   stable,
			avgELO:   avgELO,
			wins:     wins,
			earnings: earnings,
		})
	}

	sort.Slice(ranks, func(i, j int) bool { return ranks[i].avgELO > ranks[j].avgELO })

	rewardTiers := map[int]int64{
		1: 10000,
		2: 5000,
		3: 2500,
	}

	topN := 10
	if len(ranks) < topN {
		topN = len(ranks)
	}

	champions := make([]models.SeasonChampion, 0, topN)
	for i := 0; i < topN; i++ {
		place := i + 1
		reward, exists := rewardTiers[place]
		if !exists {
			reward = 1000
		}

		ranks[i].stable.Cummies += reward

		champions = append(champions, models.SeasonChampion{
			Place:      place,
			StableID:   ranks[i].stable.ID,
			StableName: ranks[i].stable.Name,
			ELO:        int(ranks[i].avgELO),
			Wins:       ranks[i].wins,
			Earnings:   ranks[i].earnings,
			Reward:     reward,
		})
	}

	s.currentSeason.Active = false
	s.currentSeason.EndedAt = time.Now()
	s.currentSeason.Champions = champions

	endedSeason := *s.currentSeason
	s.pastSeasons = append(s.pastSeasons, endedSeason)

	const baseline = 1200.0
	for _, stable := range allStables {
		for i := range stable.Horses {
			oldELO := stable.Horses[i].ELO
			stable.Horses[i].ELO = oldELO + (baseline-oldELO)*0.5
		}
		s.persistStable(r.Context(), stable)
	}

	newSeasonNum := endedSeason.ID + 1
	s.currentSeason = &models.Season{
		ID:        newSeasonNum,
		Name:      generateSeasonName(newSeasonNum),
		StartedAt: time.Now(),
		Active:    true,
	}

	if s.hub != nil {
		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "season_ended",
			"data": map[string]interface{}{
				"endedSeason": endedSeason,
				"newSeason":   s.currentSeason,
				"champions":   champions,
			},
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":     "Season ended successfully",
		"endedSeason": endedSeason,
		"newSeason":   s.currentSeason,
		"champions":   champions,
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
	// Extract identity from optional JWT token.
	userID := ""
	username := ""
	isGuest := true

	if token := r.URL.Query().Get("token"); token != "" {
		if s.auth != nil {
			claims, err := s.auth.ValidateToken(token)
			if err != nil {
				log.Printf("ws: invalid token from %s: %v (allowing as guest)", r.RemoteAddr, err)
			} else {
				userID = claims.UserID
				username = claims.DisplayName
				if username == "" {
					username = claims.Username
				}
				isGuest = false
				log.Printf("ws: authenticated user %s (id=%s) from %s", claims.Username, claims.UserID, r.RemoteAddr)
			}
		} else {
			log.Printf("ws: token provided but auth service not configured, ignoring")
		}
	} else {
		log.Printf("ws: anonymous connection from %s", r.RemoteAddr)
	}

	// Generate a guest username from the remote address if no identity was set.
	if username == "" {
		addr := r.RemoteAddr
		suffix := addr
		if len(suffix) > 4 {
			suffix = suffix[len(suffix)-4:]
		}
		username = fmt.Sprintf("guest_%s", suffix)
	}

	commussy.ServeWs(s.hub, w, r, userID, username, isGuest)
}

// ===========================================================================
// Challenge (Head-to-Head) API handlers
// ===========================================================================

// handleCreateChallenge processes POST /api/challenges.
// Body: {defenderName, horseID, wager}
func (s *Server) handleCreateChallenge(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required to create a challenge")
		return
	}

	var req struct {
		DefenderName string `json:"defenderName"`
		HorseID      string `json:"horseID"`
		Wager        int64  `json:"wager"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.DefenderName == "" || req.HorseID == "" {
		writeError(w, http.StatusBadRequest, "defenderName and horseID are required")
		return
	}

	// Strip @ prefix if present.
	defenderName := strings.TrimPrefix(req.DefenderName, "@")

	// Can't challenge yourself.
	if strings.EqualFold(defenderName, claims.Username) {
		writeError(w, http.StatusBadRequest, "you can't challenge yourself, weirdo")
		return
	}

	challenge, errMsg := s.createChallenge(claims.UserID, claims.Username, defenderName, req.HorseID, req.Wager)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	// Grant first_challenge achievement for issuing a challenge.
	if stable := s.getStableForUser(claims.UserID); stable != nil {
		s.grantAchievementToStable(stable, "first_challenge")
	}

	writeJSON(w, http.StatusCreated, challenge)
}

// handleAcceptChallenge processes POST /api/challenges/{id}/accept.
// Body: {horseID}
func (s *Server) handleAcceptChallenge(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required to accept a challenge")
		return
	}

	challengeID := r.PathValue("id")
	if challengeID == "" {
		writeError(w, http.StatusBadRequest, "challenge ID is required")
		return
	}

	var req struct {
		HorseID string `json:"horseID"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.HorseID == "" {
		writeError(w, http.StatusBadRequest, "horseID is required")
		return
	}

	result, errMsg := s.acceptChallenge(challengeID, claims.UserID, req.HorseID)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	// Grant first_challenge achievement for accepting a challenge.
	if stable := s.getStableForUser(claims.UserID); stable != nil {
		s.grantAchievementToStable(stable, "first_challenge")
	}

	writeJSON(w, http.StatusOK, result)
}

// handleDeclineChallenge processes POST /api/challenges/{id}/decline.
func (s *Server) handleDeclineChallenge(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required to decline a challenge")
		return
	}

	challengeID := r.PathValue("id")
	if challengeID == "" {
		writeError(w, http.StatusBadRequest, "challenge ID is required")
		return
	}

	errMsg := s.declineChallenge(challengeID, claims.UserID)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "declined"})
}

// handleListChallenges processes GET /api/challenges.
// Optional query param: ?user=<userID> to filter by participant.
func (s *Server) handleListChallenges(w http.ResponseWriter, r *http.Request) {
	userFilter := r.URL.Query().Get("user")

	s.challengeMu.RLock()
	defer s.challengeMu.RUnlock()

	var result []*models.Challenge
	for _, c := range s.challenges {
		if c.Status != models.ChallengeStatusPending {
			continue
		}
		if userFilter != "" && c.ChallengerID != userFilter && c.DefenderID != userFilter {
			continue
		}
		result = append(result, c)
	}

	// Sort by creation time, newest first.
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})

	writeJSON(w, http.StatusOK, result)
}

// ---------------------------------------------------------------------------
// Challenge core logic (shared by API and chat handlers)
// ---------------------------------------------------------------------------

// createChallenge creates a new head-to-head challenge. Returns the challenge
// on success, or an error message string on failure.
func (s *Server) createChallenge(challengerID, challengerName, defenderName, horseID string, wager int64) (*models.Challenge, string) {
	// Verify challenger owns the horse.
	if !s.userOwnsHorse(challengerID, horseID) {
		return nil, "you don't own that horse"
	}

	// Look up the horse.
	horse, err := s.stables.GetHorse(horseID)
	if err != nil {
		return nil, "horse not found: " + horseID
	}
	if horse.Retired {
		return nil, fmt.Sprintf("horse %s is retired and cannot race", horse.Name)
	}

	// Find the defender by username. We look through stables and match by
	// checking if the user repo has a matching username, or fall back to
	// matching stable owner names.
	defenderID, defenderUsername := s.findUserByUsername(defenderName)
	if defenderID == "" {
		return nil, fmt.Sprintf("player %q not found", defenderName)
	}

	// Can't challenge yourself (belt-and-suspenders check with IDs).
	if defenderID == challengerID {
		return nil, "you can't challenge yourself"
	}

	// Verify challenger has enough cummies for the wager.
	if wager > 0 {
		challengerStable := s.getStableForUser(challengerID)
		if challengerStable == nil {
			return nil, "you don't have a stable"
		}
		if challengerStable.Cummies < wager {
			return nil, fmt.Sprintf("insufficient cummies: you have ₵%d but wagered ₵%d", challengerStable.Cummies, wager)
		}
	}

	// Check for existing pending challenge between these two.
	s.challengeMu.RLock()
	for _, c := range s.challenges {
		if c.Status == models.ChallengeStatusPending &&
			c.ChallengerID == challengerID && c.DefenderID == defenderID {
			s.challengeMu.RUnlock()
			return nil, fmt.Sprintf("you already have a pending challenge against %s", defenderUsername)
		}
	}
	s.challengeMu.RUnlock()

	now := time.Now()
	challenge := &models.Challenge{
		ID:                  uuid.New().String(),
		ChallengerID:        challengerID,
		ChallengerName:      challengerName,
		ChallengerHorse:     horseID,
		ChallengerHorseName: horse.Name,
		DefenderID:          defenderID,
		DefenderName:        defenderUsername,
		Wager:               wager,
		Status:              models.ChallengeStatusPending,
		CreatedAt:           now,
		ExpiresAt:           now.Add(5 * time.Minute),
	}

	s.challengeMu.Lock()
	s.challenges[challenge.ID] = challenge
	s.challengeMu.Unlock()

	// Broadcast the challenge to all connected clients.
	wagerText := ""
	if wager > 0 {
		wagerText = fmt.Sprintf(" for ₵%d", wager)
	}
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":      "challenge",
		"action":    "created",
		"challenge": challenge,
	})
	s.hub.BroadcastJSON(map[string]interface{}{
		"type": "chat_system",
		"text": fmt.Sprintf("⚔️ %s challenges %s to a 1v1 race! (%s vs ???)%s — /accept or /decline within 5 minutes!",
			challengerName, defenderUsername, horse.Name, wagerText),
		"ts": now.Unix(),
	})

	log.Printf("server: challenge created: %s (%s) vs %s, horse=%s, wager=%d",
		challengerName, challenge.ID, defenderUsername, horse.Name, wager)

	return challenge, ""
}

// acceptChallenge accepts a pending challenge, runs the 1v1 race, and
// distributes winnings. Returns the race result on success.
func (s *Server) acceptChallenge(challengeID, defenderUserID, defenderHorseID string) (interface{}, string) {
	s.challengeMu.Lock()
	challenge, exists := s.challenges[challengeID]
	if !exists {
		s.challengeMu.Unlock()
		return nil, "challenge not found"
	}

	if challenge.Status != models.ChallengeStatusPending {
		s.challengeMu.Unlock()
		return nil, fmt.Sprintf("challenge is no longer pending (status: %s)", challenge.Status)
	}

	if time.Now().After(challenge.ExpiresAt) {
		challenge.Status = models.ChallengeStatusExpired
		s.challengeMu.Unlock()
		return nil, "challenge has expired"
	}

	if challenge.DefenderID != defenderUserID {
		s.challengeMu.Unlock()
		return nil, "you are not the defender in this challenge"
	}

	// Mark as accepted so no one else can grab it.
	challenge.Status = models.ChallengeStatusAccepted
	s.challengeMu.Unlock()

	// Verify defender owns the horse.
	if !s.userOwnsHorse(defenderUserID, defenderHorseID) {
		s.challengeMu.Lock()
		challenge.Status = models.ChallengeStatusPending // Revert.
		s.challengeMu.Unlock()
		return nil, "you don't own that horse"
	}

	defenderHorse, err := s.stables.GetHorse(defenderHorseID)
	if err != nil {
		s.challengeMu.Lock()
		challenge.Status = models.ChallengeStatusPending
		s.challengeMu.Unlock()
		return nil, "horse not found: " + defenderHorseID
	}
	if defenderHorse.Retired {
		s.challengeMu.Lock()
		challenge.Status = models.ChallengeStatusPending
		s.challengeMu.Unlock()
		return nil, fmt.Sprintf("horse %s is retired and cannot race", defenderHorse.Name)
	}

	// Set the defender's horse on the challenge.
	challenge.DefenderHorse = defenderHorseID
	challenge.DefenderHorseName = defenderHorse.Name

	// Handle wager escrow: deduct wager from both players.
	challengerStable := s.getStableForUser(challenge.ChallengerID)
	defenderStable := s.getStableForUser(defenderUserID)

	if challenge.Wager > 0 {
		if challengerStable == nil || defenderStable == nil {
			s.challengeMu.Lock()
			challenge.Status = models.ChallengeStatusPending
			s.challengeMu.Unlock()
			return nil, "both players must have stables to wager"
		}
		if challengerStable.Cummies < challenge.Wager {
			s.challengeMu.Lock()
			challenge.Status = models.ChallengeStatusPending
			s.challengeMu.Unlock()
			return nil, fmt.Sprintf("challenger %s no longer has enough cummies (needs ₵%d)", challenge.ChallengerName, challenge.Wager)
		}
		if defenderStable.Cummies < challenge.Wager {
			s.challengeMu.Lock()
			challenge.Status = models.ChallengeStatusPending
			s.challengeMu.Unlock()
			return nil, fmt.Sprintf("you don't have enough cummies (need ₵%d, have ₵%d)", challenge.Wager, defenderStable.Cummies)
		}

		// Escrow: deduct wager from both.
		challengerStable.Cummies -= challenge.Wager
		defenderStable.Cummies -= challenge.Wager
	}

	// Resolve challenger horse.
	challengerHorse, err := s.stables.GetHorse(challenge.ChallengerHorse)
	if err != nil {
		// Refund wagers if escrowed.
		if challenge.Wager > 0 {
			challengerStable.Cummies += challenge.Wager
			defenderStable.Cummies += challenge.Wager
		}
		s.challengeMu.Lock()
		challenge.Status = models.ChallengeStatusPending
		s.challengeMu.Unlock()
		return nil, "challenger's horse no longer exists"
	}

	// Broadcast the acceptance.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":      "challenge",
		"action":    "accepted",
		"challenge": challenge,
	})
	s.hub.BroadcastJSON(map[string]interface{}{
		"type": "chat_system",
		"text": fmt.Sprintf("⚔️ %s accepts the challenge! %s vs %s — RACE IS ON!",
			challenge.DefenderName, challenge.ChallengerHorseName, defenderHorse.Name),
		"ts": time.Now().Unix(),
	})

	// Run the 1v1 race! Pick a random track for variety.
	tracks := []models.TrackType{
		models.TrackSprintussy, models.TrackGrindussy, models.TrackMudussy,
		models.TrackThunderussy, models.TrackFrostussy, models.TrackHauntedussy,
	}
	trackType := tracks[rand.IntN(len(tracks))]

	// Base purse for challenges (non-wager reward).
	basePurse := int64(500)
	horses := []*models.Horse{challengerHorse, defenderHorse}
	result := s.runRace(horses, trackType, basePurse)

	// Determine winner and distribute wager winnings.
	var winnerID, winnerName, loserName string
	for _, entry := range result.Race.Entries {
		if entry.FinishPlace == 1 {
			if entry.HorseID == challengerHorse.ID {
				winnerID = challenge.ChallengerID
				winnerName = challenge.ChallengerName
				loserName = challenge.DefenderName
			} else {
				winnerID = challenge.DefenderID
				winnerName = challenge.DefenderName
				loserName = challenge.ChallengerName
			}
			break
		}
	}

	// Distribute wager to winner (minus 5% burn).
	wagerEarnings := int64(0)
	wagerBurn := int64(0)
	if challenge.Wager > 0 && winnerID != "" {
		totalPot := challenge.Wager * 2      // Both players wagered
		wagerBurn = totalPot * 5 / 100       // 5% burn
		wagerEarnings = totalPot - wagerBurn // Winner gets the rest

		winnerStable := s.getStableForUser(winnerID)
		if winnerStable != nil {
			winnerStable.Cummies += wagerEarnings
			winnerStable.TotalEarnings += wagerEarnings
			s.persistStable(context.Background(), winnerStable)
		}

		// Persist loser's stable too (wager was already deducted).
		loserStable := s.getStableForUser(challenge.DefenderID)
		if winnerID == challenge.DefenderID {
			loserStable = s.getStableForUser(challenge.ChallengerID)
		}
		if loserStable != nil {
			s.persistStable(context.Background(), loserStable)
		}

		log.Printf("server: challenge wager settled: %s won ₵%d (burn: ₵%d)",
			winnerName, wagerEarnings, wagerBurn)
	}

	// Mark challenge as completed.
	s.challengeMu.Lock()
	challenge.Status = models.ChallengeStatusCompleted
	s.challengeMu.Unlock()

	// Broadcast the challenge result.
	wagerText := ""
	if challenge.Wager > 0 {
		wagerText = fmt.Sprintf(" — won ₵%d (₵%d burned)", wagerEarnings, wagerBurn)
	}
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":      "challenge",
		"action":    "completed",
		"challenge": challenge,
		"winnerID":  winnerID,
		"result":    result,
	})
	s.hub.BroadcastJSON(map[string]interface{}{
		"type": "chat_system",
		"text": fmt.Sprintf("🏆 %s wins the challenge against %s!%s",
			winnerName, loserName, wagerText),
		"ts": time.Now().Unix(),
	})

	return map[string]interface{}{
		"challenge": challenge,
		"race":      result,
		"winnerID":  winnerID,
	}, ""
}

// declineChallenge declines a pending challenge. Returns an error message on failure.
func (s *Server) declineChallenge(challengeID, userID string) string {
	s.challengeMu.Lock()
	defer s.challengeMu.Unlock()

	challenge, exists := s.challenges[challengeID]
	if !exists {
		return "challenge not found"
	}

	if challenge.Status != models.ChallengeStatusPending {
		return fmt.Sprintf("challenge is no longer pending (status: %s)", challenge.Status)
	}

	if challenge.DefenderID != userID {
		return "you are not the defender in this challenge"
	}

	challenge.Status = models.ChallengeStatusDeclined

	// No wager escrow to refund on decline (wager is only escrowed on accept).

	// Broadcast the decline.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":      "challenge",
		"action":    "declined",
		"challenge": challenge,
	})
	s.hub.BroadcastJSON(map[string]interface{}{
		"type": "chat_system",
		"text": fmt.Sprintf("❌ %s declined the challenge from %s.",
			challenge.DefenderName, challenge.ChallengerName),
		"ts": time.Now().Unix(),
	})

	return ""
}

// findUserByUsername looks up a user by their display name or username.
// Returns (userID, username) or ("", "") if not found.
func (s *Server) findUserByUsername(name string) (string, string) {
	// First, try the user repository (DB mode) for exact match.
	if s.userRepo != nil {
		user, err := s.userRepo.GetUserByUsername(context.Background(), name)
		if err == nil && user != nil {
			return user.ID, user.Username
		}
	}

	// Fallback: iterate stables and match by stable name pattern or ownerID.
	// Stable names are typically "{username}'s Stable".
	for _, stable := range s.stables.ListStables() {
		// Match the stable name pattern.
		if strings.EqualFold(stable.Name, name+"'s Stable") ||
			strings.EqualFold(stable.OwnerID, name) {
			return stable.OwnerID, name
		}
	}

	return "", ""
}

// findHorseByNameInStable does a case-insensitive search for a horse by name
// within a user's stable. Returns the horse or nil if not found.
func (s *Server) findHorseByNameInStable(userID, horseName string) *models.Horse {
	stable := s.getStableForUser(userID)
	if stable == nil {
		return nil
	}

	// Exact case-insensitive match first.
	for i := range stable.Horses {
		if strings.EqualFold(stable.Horses[i].Name, horseName) {
			h := &stable.Horses[i]
			return h
		}
	}

	// Fuzzy match: check if any horse name contains the search term.
	lowerName := strings.ToLower(horseName)
	for i := range stable.Horses {
		if strings.Contains(strings.ToLower(stable.Horses[i].Name), lowerName) {
			h := &stable.Horses[i]
			return h
		}
	}

	return nil
}

// findPendingChallengeForDefender finds the most recent pending challenge
// where the given user is the defender.
func (s *Server) findPendingChallengeForDefender(userID string) *models.Challenge {
	s.challengeMu.RLock()
	defer s.challengeMu.RUnlock()

	var latest *models.Challenge
	for _, c := range s.challenges {
		if c.Status == models.ChallengeStatusPending && c.DefenderID == userID {
			if latest == nil || c.CreatedAt.After(latest.CreatedAt) {
				latest = c
			}
		}
	}
	return latest
}

// challengeExpiryLoop periodically checks for expired challenges and marks
// them as expired. Runs as a background goroutine.
func (s *Server) challengeExpiryLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		s.challengeMu.Lock()
		for _, c := range s.challenges {
			if c.Status == models.ChallengeStatusPending && now.After(c.ExpiresAt) {
				c.Status = models.ChallengeStatusExpired
				log.Printf("server: challenge %s expired (%s vs %s)",
					c.ID, c.ChallengerName, c.DefenderName)

				// Broadcast the expiry.
				s.hub.BroadcastJSON(map[string]interface{}{
					"type":      "challenge",
					"action":    "expired",
					"challenge": c,
				})
				s.hub.BroadcastJSON(map[string]interface{}{
					"type": "chat_system",
					"text": fmt.Sprintf("⏰ Challenge from %s to %s has expired.",
						c.ChallengerName, c.DefenderName),
					"ts": now.Unix(),
				})
			}
		}
		s.challengeMu.Unlock()
	}
}

// ===========================================================================
// Chat command handlers (called via Hub's OnChatCommand callback)
// ===========================================================================

// handleChatSend processes a /send command from chat: transfers cummies from
// the sender's stable to the target's stable.
func (s *Server) handleChatSend(senderUserID, senderUsername string, args map[string]interface{}) {
	target, _ := args["target"].(string)
	amountF, _ := args["amount"].(float64) // JSON numbers are float64
	amount := int64(amountF)
	note, _ := args["note"].(string)

	if target == "" || amount <= 0 {
		return
	}

	// Strip @ prefix from target username.
	if strings.HasPrefix(target, "@") {
		target = target[1:]
	}

	// Find sender's stable.
	var senderStable *models.Stable
	for _, st := range s.stables.ListStables() {
		if st.OwnerID == senderUserID {
			senderStable = st
			break
		}
	}
	if senderStable == nil {
		return
	}

	// Find target's stable by username (look up user by username).
	var targetStable *models.Stable
	for _, st := range s.stables.ListStables() {
		if st.Name == target+"'s Stable" || st.OwnerID == target {
			targetStable = st
			break
		}
	}
	if targetStable == nil {
		return
	}

	// Transfer cummies.
	if err := s.stables.TransferCummies(senderStable.ID, targetStable.ID, amount); err != nil {
		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": fmt.Sprintf("*** Transfer failed: %v", err),
			"ts":   time.Now().Unix(),
		})
		return
	}

	// Persist both stables.
	s.persistStable(context.Background(), senderStable)
	s.persistStable(context.Background(), targetStable)

	msg := fmt.Sprintf("*** %s sent ₵%d to %s", senderUsername, amount, target)
	if note != "" {
		msg += fmt.Sprintf(` — "%s"`, note)
	}

	s.hub.BroadcastJSON(map[string]interface{}{
		"type": "chat_money",
		"text": msg,
		"ts":   time.Now().Unix(),
	})
}

// handleChatTrade processes a /trade command from chat: broadcasts a trade
// offer to all connected clients.
func (s *Server) handleChatTrade(senderUserID, senderUsername string, args map[string]interface{}) {
	target, _ := args["target"].(string)
	horseName, _ := args["horse"].(string)

	if target == "" || horseName == "" {
		return
	}
	if strings.HasPrefix(target, "@") {
		target = target[1:]
	}

	s.hub.BroadcastJSON(map[string]interface{}{
		"type": "chat_trade",
		"text": fmt.Sprintf("*** TRADE: %s offers %s to %s — use /accept or /reject", senderUsername, horseName, target),
		"ts":   time.Now().Unix(),
	})
}

// handleChatChallenge processes a /challenge command from chat.
// Expected args: target (string, @username), horse (string, horse name), wager (float64, optional).
// Usage: /challenge @username horsename [wager]
func (s *Server) handleChatChallenge(senderUserID, senderUsername string, args map[string]interface{}) {
	target, _ := args["target"].(string)
	horseName, _ := args["horse"].(string)

	if target == "" || horseName == "" {
		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": "*** Usage: /challenge @username horsename [wager]",
			"ts":   time.Now().Unix(),
		})
		return
	}

	// Strip @ prefix.
	if strings.HasPrefix(target, "@") {
		target = target[1:]
	}

	// Can't challenge yourself.
	if strings.EqualFold(target, senderUsername) {
		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": "*** You can't challenge yourself, weirdo.",
			"ts":   time.Now().Unix(),
		})
		return
	}

	// Find the horse by name in the sender's stable.
	horse := s.findHorseByNameInStable(senderUserID, horseName)
	if horse == nil {
		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": fmt.Sprintf("*** Horse %q not found in your stable.", horseName),
			"ts":   time.Now().Unix(),
		})
		return
	}

	// Parse optional wager.
	wager := int64(0)
	if w, ok := args["wager"].(float64); ok && w > 0 {
		wager = int64(w)
	}

	challenge, errMsg := s.createChallenge(senderUserID, senderUsername, target, horse.ID, wager)
	if errMsg != "" {
		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": fmt.Sprintf("*** Challenge failed: %s", errMsg),
			"ts":   time.Now().Unix(),
		})
		return
	}

	_ = challenge // Broadcast already happens inside createChallenge.
}

// handleChatAccept processes an /accept command from chat.
// Expected args: horse (string, horse name, optional — uses first non-retired horse if omitted).
// Usage: /accept [horsename]
func (s *Server) handleChatAccept(senderUserID, senderUsername string, args map[string]interface{}) {
	// Find the most recent pending challenge where the user is the defender.
	challenge := s.findPendingChallengeForDefender(senderUserID)
	if challenge == nil {
		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": "*** No pending challenge found for you.",
			"ts":   time.Now().Unix(),
		})
		return
	}

	// Find the defender's horse by name, or pick the first non-retired horse.
	horseName, _ := args["horse"].(string)
	var horse *models.Horse
	if horseName != "" {
		horse = s.findHorseByNameInStable(senderUserID, horseName)
		if horse == nil {
			s.hub.BroadcastJSON(map[string]interface{}{
				"type": "chat_system",
				"text": fmt.Sprintf("*** Horse %q not found in your stable.", horseName),
				"ts":   time.Now().Unix(),
			})
			return
		}
	} else {
		// Auto-pick the first non-retired horse in the stable.
		stable := s.getStableForUser(senderUserID)
		if stable != nil {
			for i := range stable.Horses {
				if !stable.Horses[i].Retired {
					horse = &stable.Horses[i]
					break
				}
			}
		}
		if horse == nil {
			s.hub.BroadcastJSON(map[string]interface{}{
				"type": "chat_system",
				"text": "*** You have no eligible horses. Specify one with /accept horsename",
				"ts":   time.Now().Unix(),
			})
			return
		}
	}

	if horse.Retired {
		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": fmt.Sprintf("*** %s is retired and cannot race.", horse.Name),
			"ts":   time.Now().Unix(),
		})
		return
	}

	_, errMsg := s.acceptChallenge(challenge.ID, senderUserID, horse.ID)
	if errMsg != "" {
		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": fmt.Sprintf("*** Accept failed: %s", errMsg),
			"ts":   time.Now().Unix(),
		})
		return
	}
	// Results are broadcast inside acceptChallenge.
}

// handleChatDecline processes a /decline command from chat.
// Usage: /decline
func (s *Server) handleChatDecline(senderUserID, senderUsername string, args map[string]interface{}) {
	// Find the most recent pending challenge where the user is the defender.
	challenge := s.findPendingChallengeForDefender(senderUserID)
	if challenge == nil {
		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": "*** No pending challenge found for you.",
			"ts":   time.Now().Unix(),
		})
		return
	}

	errMsg := s.declineChallenge(challenge.ID, senderUserID)
	if errMsg != "" {
		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": fmt.Sprintf("*** Decline failed: %s", errMsg),
			"ts":   time.Now().Unix(),
		})
		return
	}
	// Decline broadcast happens inside declineChallenge.
}

// ===========================================================================
// Helper functions
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

// ===========================================================================
// Engagement System — daily rewards, prestige, win streak bonuses
// ===========================================================================

// dailyRewards defines the 7-day login streak reward cycle.
var dailyRewards = []models.DailyReward{
	{Day: 1, Cummies: 100, Bonus: "Welcome back, breeder"},
	{Day: 2, Cummies: 150, Bonus: "Streak building..."},
	{Day: 3, Cummies: 200, Bonus: "Triple threat!"},
	{Day: 4, Cummies: 250, Bonus: "Dedicated stable hand"},
	{Day: 5, Cummies: 500, Bonus: "5-day streak! Bonus cummies!"},
	{Day: 6, Cummies: 300, Bonus: "Almost there..."},
	{Day: 7, Cummies: 1000, Bonus: "WEEKLY JACKPOT! Dr. Mittens approves"},
}

// prestigeTiers defines progression levels and their bonuses.
var prestigeTiers = []models.PrestigeTier{
	{Level: 0, Name: "Stable Hand", RequiredXP: 0, CummiesBonus: 1.0, TrainingBonus: 1.0, MaxHorses: 5},
	{Level: 1, Name: "Paddock Manager", RequiredXP: 1000, CummiesBonus: 1.05, TrainingBonus: 1.05, MaxHorses: 7},
	{Level: 2, Name: "Ranch Owner", RequiredXP: 5000, CummiesBonus: 1.1, TrainingBonus: 1.1, MaxHorses: 10},
	{Level: 3, Name: "Stud Baron", RequiredXP: 15000, CummiesBonus: 1.15, TrainingBonus: 1.15, MaxHorses: 15},
	{Level: 4, Name: "Racing Magnate", RequiredXP: 50000, CummiesBonus: 1.2, TrainingBonus: 1.2, MaxHorses: 20},
	{Level: 5, Name: "Ussy Lord", RequiredXP: 150000, CummiesBonus: 1.3, TrainingBonus: 1.3, MaxHorses: 30},
	{Level: 6, Name: "The Geoffrussy", RequiredXP: 500000, CummiesBonus: 1.5, TrainingBonus: 1.5, MaxHorses: 50},
}

// breedingCooldownHours is the minimum time between breeds for a single horse.
const breedingCooldownHours = 4

// getOrCreateProgress returns the player's progress, creating a fresh one if needed.
// Caller must hold progressMu or call within a locked section.
func (s *Server) getOrCreateProgress(userID string) *models.PlayerProgress {
	p, ok := s.progress[userID]
	if !ok {
		p = &models.PlayerProgress{
			UserID:          userID,
			DailyTrainsLeft: 5,
			DailyRacesLeft:  10,
			LastDailyReset:  time.Now().UTC().Format("2006-01-02"),
		}
		s.progress[userID] = p
	}
	return p
}

// resetDailyLimitsIfNeeded resets daily trains/races if the date has changed.
func resetDailyLimitsIfNeeded(p *models.PlayerProgress) {
	today := time.Now().UTC().Format("2006-01-02")
	if p.LastDailyReset != today {
		p.DailyTrainsLeft = 5
		p.DailyRacesLeft = 10
		p.LastDailyReset = today
	}
}

// getPrestigeTier returns the current prestige tier for the given XP.
func getPrestigeTier(xp int64) models.PrestigeTier {
	tier := prestigeTiers[0]
	for _, t := range prestigeTiers {
		if xp >= t.RequiredXP {
			tier = t
		}
	}
	return tier
}

// getNextPrestigeTier returns the next tier after the current one, or nil if maxed.
func getNextPrestigeTier(currentLevel int) *models.PrestigeTier {
	for _, t := range prestigeTiers {
		if t.Level == currentLevel+1 {
			return &t
		}
	}
	return nil
}

// addPrestigeXP grants XP to a player and checks for level-ups.
func (s *Server) addPrestigeXP(userID string, username string, xp int64) {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()

	p := s.getOrCreateProgress(userID)
	oldTier := getPrestigeTier(p.PrestigeXP)
	p.PrestigeXP += xp
	newTier := getPrestigeTier(p.PrestigeXP)
	p.PrestigeLevel = newTier.Level

	// Broadcast level-up if the tier changed.
	if newTier.Level > oldTier.Level {
		s.hub.BroadcastJSON(map[string]interface{}{
			"type":     "prestige_levelup",
			"username": username,
			"newLevel": newTier.Level,
			"tierName": newTier.Name,
		})
		log.Printf("server: prestige level-up! %s reached %s (level %d, XP %d)",
			username, newTier.Name, newTier.Level, p.PrestigeXP)
	}
}

// getPrestigeTierForUser returns the prestige tier for a user (read-locked).
func (s *Server) getPrestigeTierForUser(userID string) models.PrestigeTier {
	s.progressMu.RLock()
	defer s.progressMu.RUnlock()
	p, ok := s.progress[userID]
	if !ok {
		return prestigeTiers[0]
	}
	return getPrestigeTier(p.PrestigeXP)
}

// addPrestigeXPForHorse is a convenience that looks up the horse's owner
// and grants them prestige XP. Safe to call from contexts without a request.
func (s *Server) addPrestigeXPForHorse(horse *models.Horse, xp int64) {
	if horse.OwnerID == "" || horse.OwnerID == "system" {
		return
	}
	// Resolve owner username for the broadcast message.
	username := horse.OwnerID // fallback to ID
	for _, st := range s.stables.ListStables() {
		if st.OwnerID == horse.OwnerID {
			username = st.Name
			break
		}
	}
	s.addPrestigeXP(horse.OwnerID, username, xp)
}

// winStreakMultiplier returns the earnings multiplier based on a horse's
// current win streak.
func winStreakMultiplier(streak int) float64 {
	switch {
	case streak >= 10:
		return 2.0
	case streak >= 7:
		return 1.75
	case streak >= 5:
		return 1.5
	case streak >= 3:
		return 1.2
	case streak >= 2:
		return 1.1
	default:
		return 1.0
	}
}

// handleGetProgress returns the calling user's engagement progress.
func (s *Server) handleGetProgress(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	s.progressMu.Lock()
	p := s.getOrCreateProgress(claims.UserID)
	resetDailyLimitsIfNeeded(p)
	s.progressMu.Unlock()

	// Return a copy under read lock.
	s.progressMu.RLock()
	defer s.progressMu.RUnlock()
	writeJSON(w, http.StatusOK, p)
}

// handleClaimDailyReward processes a daily login reward claim.
func (s *Server) handleClaimDailyReward(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	today := time.Now().UTC().Format("2006-01-02")
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")

	s.progressMu.Lock()
	p := s.getOrCreateProgress(claims.UserID)
	resetDailyLimitsIfNeeded(p)

	// Check if already claimed today.
	if p.LastLoginDate == today {
		s.progressMu.Unlock()
		writeError(w, http.StatusBadRequest, "daily reward already claimed today")
		return
	}

	// Update streak: continues if they logged in yesterday, otherwise resets.
	if p.LastLoginDate == yesterday {
		p.LoginStreak++
	} else {
		p.LoginStreak = 1
	}
	p.LastLoginDate = today
	p.TotalLogins++

	// Determine reward based on streak position in the 7-day cycle.
	cycleDay := ((p.LoginStreak - 1) % 7) + 1 // 1-7
	cycleNumber := (p.LoginStreak - 1) / 7    // 0, 1, 2, ...

	reward := dailyRewards[cycleDay-1]

	// After day 7, cycle back with 1.5x multiplier per full cycle completed.
	multiplier := 1.0
	for i := 0; i < cycleNumber; i++ {
		multiplier *= 1.5
	}
	actualCummies := int64(float64(reward.Cummies) * multiplier)

	s.progressMu.Unlock()

	// Find the player's stable and add cummies.
	stable := s.getStableForUser(claims.UserID)
	if stable == nil {
		writeError(w, http.StatusBadRequest, "no stable found for user")
		return
	}
	stable.Cummies += actualCummies
	s.persistStable(r.Context(), stable)

	// Grant streak_7 achievement when login streak reaches 7+.
	if p.LoginStreak >= 7 {
		s.grantAchievementToStable(stable, "streak_7")
	}

	// Broadcast the daily reward event.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":     "daily_reward",
		"username": claims.Username,
		"streak":   p.LoginStreak,
		"cummies":  actualCummies,
		"bonus":    reward.Bonus,
	})

	log.Printf("server: daily reward claimed by %s — streak %d, reward %d cummies (%s)",
		claims.Username, p.LoginStreak, actualCummies, reward.Bonus)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"streak":   p.LoginStreak,
		"day":      cycleDay,
		"cummies":  actualCummies,
		"bonus":    reward.Bonus,
		"progress": p,
	})
}

// handleGetPrestige returns the user's current prestige level and XP info.
func (s *Server) handleGetPrestige(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	s.progressMu.RLock()
	p, exists := s.progress[claims.UserID]
	if !exists {
		s.progressMu.RUnlock()
		// Create a default progress entry.
		s.progressMu.Lock()
		p = s.getOrCreateProgress(claims.UserID)
		s.progressMu.Unlock()
		s.progressMu.RLock()
	}

	currentTier := getPrestigeTier(p.PrestigeXP)
	nextTier := getNextPrestigeTier(currentTier.Level)
	xp := p.PrestigeXP
	s.progressMu.RUnlock()

	resp := map[string]interface{}{
		"currentTier": currentTier,
		"xp":          xp,
		"allTiers":    prestigeTiers,
	}
	if nextTier != nil {
		resp["nextTier"] = nextTier
		resp["xpToNext"] = nextTier.RequiredXP - xp
	} else {
		resp["maxed"] = true
	}
	writeJSON(w, http.StatusOK, resp)
}

// ===========================================================================
// Auth route registration
// ===========================================================================

// registerAuthRoutes adds authentication endpoints to the mux. Only called
// when auth is configured (DB mode).
func (s *Server) registerAuthRoutes() {
	s.mux.HandleFunc("POST /api/auth/register", s.authHandler.HandleRegister)
	s.mux.HandleFunc("POST /api/auth/login", s.authHandler.HandleLogin)
	s.mux.HandleFunc("GET /api/auth/me", s.authHandler.HandleMe)
}

// ===========================================================================
// Persistence helpers — write-through to PostgreSQL
// ===========================================================================

// persistHorse creates or updates a horse in the database. If CreateHorse
// fails (e.g. duplicate key), falls back to UpdateHorse. No-op when DB is nil.
func (s *Server) persistHorse(ctx context.Context, horse *models.Horse) {
	if s.horseRepo == nil {
		return
	}
	if err := s.horseRepo.CreateHorse(ctx, horse); err != nil {
		if err2 := s.horseRepo.UpdateHorse(ctx, horse); err2 != nil {
			log.Printf("server: persistHorse %s: create=%v update=%v", horse.ID, err, err2)
		}
	}
}

// persistStable creates or updates a stable in the database. No-op when DB is nil.
func (s *Server) persistStable(ctx context.Context, stable *models.Stable) {
	if s.stableRepo == nil {
		return
	}
	if err := s.stableRepo.CreateStable(ctx, stable); err != nil {
		if err2 := s.stableRepo.UpdateStable(ctx, stable); err2 != nil {
			log.Printf("server: persistStable %s: create=%v update=%v", stable.ID, err, err2)
		}
	}
}

// persistRaceResult writes a race result to the database. No-op when DB is nil.
func (s *Server) persistRaceResult(ctx context.Context, result *models.RaceResult) {
	if s.raceResultRepo == nil {
		return
	}
	if err := s.raceResultRepo.RecordResult(ctx, result); err != nil {
		log.Printf("server: persistRaceResult race=%s horse=%s: %v", result.RaceID, result.HorseID, err)
	}
}

// persistListing creates or updates a market listing in the database. No-op when DB is nil.
func (s *Server) persistListing(ctx context.Context, listing *models.StudListing) {
	if s.marketRepo == nil {
		return
	}
	if err := s.marketRepo.CreateListing(ctx, listing); err != nil {
		if err2 := s.marketRepo.UpdateListing(ctx, listing); err2 != nil {
			log.Printf("server: persistListing %s: create=%v update=%v", listing.ID, err, err2)
		}
	}
}

// persistTournament creates or updates a tournament in the database. No-op when DB is nil.
func (s *Server) persistTournament(ctx context.Context, tournament *models.Tournament) {
	if s.tournamentRepo == nil {
		return
	}
	if err := s.tournamentRepo.CreateTournament(ctx, tournament); err != nil {
		if err2 := s.tournamentRepo.UpdateTournament(ctx, tournament); err2 != nil {
			log.Printf("server: persistTournament %s: create=%v update=%v", tournament.ID, err, err2)
		}
	}
}

// persistAchievement writes an achievement to the database. No-op when DB is nil.
func (s *Server) persistAchievement(ctx context.Context, stableID string, achievement *models.Achievement) {
	if s.achievementRepo == nil {
		return
	}
	if err := s.achievementRepo.AddAchievement(ctx, stableID, achievement); err != nil {
		log.Printf("server: persistAchievement stable=%s achievement=%s: %v", stableID, achievement.ID, err)
	}
}

// persistTrainingSession writes a training session to the database. No-op when DB is nil.
func (s *Server) persistTrainingSession(ctx context.Context, session *models.TrainingSession) {
	if s.trainingRepo == nil {
		return
	}
	if err := s.trainingRepo.SaveSession(ctx, session); err != nil {
		log.Printf("server: persistTrainingSession horse=%s: %v", session.HorseID, err)
	}
}

// persistMarketTransaction writes a market transaction to the database. No-op when DB is nil.
func (s *Server) persistMarketTransaction(ctx context.Context, tx *models.MarketTransaction) {
	if s.marketTxRepo == nil {
		return
	}
	if err := s.marketTxRepo.SaveTransaction(ctx, tx); err != nil {
		log.Printf("server: persistMarketTransaction %s: %v", tx.ID, err)
	}
}

// ===========================================================================
// Database loading — hydrate in-memory state from PostgreSQL on startup
// ===========================================================================

// loadFromDB loads stables, horses, market listings, and achievements from
// the database into the in-memory managers. Called from NewServer when a DB
// connection is provided.
func (s *Server) loadFromDB() {
	ctx := context.Background()

	// 1. Load all stables.
	dbStables, err := s.stableRepo.ListStables(ctx)
	if err != nil {
		log.Printf("server: loadFromDB: failed to list stables: %v", err)
		return
	}

	for _, stable := range dbStables {
		// Ensure non-nil slices for in-memory use.
		if stable.Horses == nil {
			stable.Horses = []models.Horse{}
		}
		if stable.Achievements == nil {
			stable.Achievements = []models.Achievement{}
		}

		// Load horses for this stable. The horse repo queries by owner_id,
		// so we pass the stable's OwnerID.
		dbHorses, err := s.horseRepo.ListHorsesByStable(ctx, stable.OwnerID)
		if err != nil {
			log.Printf("server: loadFromDB: failed to list horses for stable %s (owner %s): %v",
				stable.ID, stable.OwnerID, err)
		} else {
			for _, h := range dbHorses {
				stable.Horses = append(stable.Horses, *h)
			}
		}

		// Load achievements for this stable.
		dbAchievements, err := s.achievementRepo.GetAchievements(ctx, stable.ID)
		if err != nil {
			log.Printf("server: loadFromDB: failed to load achievements for stable %s: %v", stable.ID, err)
		} else {
			for _, a := range dbAchievements {
				stable.Achievements = append(stable.Achievements, *a)
			}
		}

		// Import into the in-memory manager (registers horses globally too).
		s.stables.ImportStable(stable)

		log.Printf("server: loadFromDB: loaded stable %q (%s) with %d horses, %d achievements",
			stable.Name, stable.ID, len(stable.Horses), len(stable.Achievements))
	}

	// 2. Load active market listings.
	dbListings, err := s.marketRepo.ListActiveListings(ctx)
	if err != nil {
		log.Printf("server: loadFromDB: failed to list market listings: %v", err)
	} else {
		for _, listing := range dbListings {
			s.market.ImportListing(listing)
		}
		if len(dbListings) > 0 {
			log.Printf("server: loadFromDB: loaded %d active market listings", len(dbListings))
		}
	}

	// 3. Load race history into the in-memory RaceHistory store.
	if s.raceResultRepo != nil {
		// GetRecentResults returns newest-first, which matches the in-memory ordering.
		// Use a high limit to load all historical results.
		dbResults, err := s.raceResultRepo.GetRecentResults(ctx, 100000)
		if err != nil {
			log.Printf("server: loadFromDB: failed to load race results: %v", err)
		} else if len(dbResults) > 0 {
			s.raceHistory.ImportResults(dbResults)
			log.Printf("server: loadFromDB: loaded %d race results into history", len(dbResults))
		}
	}

	// 4. Load tournaments into the in-memory TournamentManager.
	if s.tournamentRepo != nil {
		dbTournaments, err := s.tournamentRepo.ListTournaments(ctx)
		if err != nil {
			log.Printf("server: loadFromDB: failed to load tournaments: %v", err)
		} else {
			for _, t := range dbTournaments {
				s.tournaments.ImportTournament(t)
			}
			if len(dbTournaments) > 0 {
				log.Printf("server: loadFromDB: loaded %d tournaments", len(dbTournaments))
			}
		}
	}

	// 5. Load trade offers into the in-memory TradeManager.
	if s.tradeRepo != nil {
		dbTrades, err := s.tradeRepo.ListAllTrades(ctx)
		if err != nil {
			log.Printf("server: loadFromDB: failed to load trade offers: %v", err)
		} else {
			for _, t := range dbTrades {
				s.trades.ImportOffer(t)
			}
			if len(dbTrades) > 0 {
				log.Printf("server: loadFromDB: loaded %d trade offers", len(dbTrades))
			}
		}
	}

	log.Printf("server: loadFromDB: completed — %d stables loaded from database", len(dbStables))

	// 6. Load alliances into the in-memory store.
	if s.allianceRepo != nil {
		dbAlliances, err := s.allianceRepo.ListAlliances(ctx)
		if err != nil {
			log.Printf("server: loadFromDB: failed to load alliances: %v", err)
		} else {
			for _, a := range dbAlliances {
				// Load members for each alliance.
				members, err := s.allianceRepo.ListMembers(ctx, a.ID)
				if err != nil {
					log.Printf("server: loadFromDB: failed to load members for alliance %s: %v", a.ID, err)
				} else {
					a.Members = make([]models.AllianceMember, len(members))
					for i, m := range members {
						a.Members[i] = *m
					}
				}
				s.allianceMu.Lock()
				s.alliances[a.ID] = a
				s.allianceMu.Unlock()
			}
			if len(dbAlliances) > 0 {
				log.Printf("server: loadFromDB: loaded %d alliances", len(dbAlliances))
			}
		}
	}
}

// ===========================================================================
// Pari-Mutuel Betting System
// ===========================================================================
//
// Spectators and players can bet cummies on race outcomes. The system uses
// pari-mutuel odds: all bets go into a pool, and the payout is proportional
// to how much was bet on the winning horse vs. the total pool. A 10% house
// cut is applied to the pool before distribution.
//
// Flow:
//   1. A betting pool is opened before a race (auto or manual).
//   2. Players place bets while the pool is "open".
//   3. The pool is "closed" when the race starts (no more bets).
//   4. After the race, resolveBets pays out winners from the pool.

const bettingHouseCutPct = 0.10 // 10% house cut on betting pools.

// ---------------------------------------------------------------------------
// Core betting logic (not HTTP-specific)
// ---------------------------------------------------------------------------

// openBettingPool creates a new betting pool for a race. The horses slice
// defines which horses can be bet on. Returns the created pool.
func (s *Server) openBettingPool(raceID string, horses []*models.Horse) *models.BettingPool {
	s.bettingMu.Lock()
	defer s.bettingMu.Unlock()

	bettingHorses := make([]models.BettingHorse, len(horses))
	for i, h := range horses {
		bettingHorses[i] = models.BettingHorse{
			HorseID:   h.ID,
			HorseName: h.Name,
		}
	}

	pool := &models.BettingPool{
		RaceID:   raceID,
		Status:   "open",
		Horses:   bettingHorses,
		OpenedAt: time.Now(),
	}
	s.bettingPools[raceID] = pool

	// Broadcast pool opened event.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":   "betting_pool_opened",
		"raceID": raceID,
		"horses": bettingHorses,
	})
	s.hub.BroadcastJSON(map[string]interface{}{
		"type": "chat_system",
		"text": fmt.Sprintf("🎰 Betting pool opened! %d horses in the race. Use /bet horsename amount to place your bets!",
			len(horses)),
		"ts": time.Now().Unix(),
	})

	log.Printf("server: betting pool opened for race %s with %d horses", raceID, len(horses))
	return pool
}

// closeBettingPool marks a pool as "closed" so no more bets are accepted.
func (s *Server) closeBettingPool(raceID string) {
	s.bettingMu.Lock()
	defer s.bettingMu.Unlock()

	pool, ok := s.bettingPools[raceID]
	if !ok || pool.Status != "open" {
		return
	}
	pool.Status = "closed"
	pool.ClosedAt = time.Now()

	// Recalculate final odds before closing.
	s.calcOddsLocked(pool)

	// Broadcast pool closed event.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":      "betting_pool_closed",
		"raceID":    raceID,
		"totalPool": pool.TotalPool,
		"horses":    pool.Horses,
	})
	s.hub.BroadcastJSON(map[string]interface{}{
		"type": "chat_system",
		"text": fmt.Sprintf("🔒 Betting closed! ₵%d total in the pool. Let the race begin!",
			pool.TotalPool),
		"ts": time.Now().Unix(),
	})

	log.Printf("server: betting pool closed for race %s (total: ₵%d, bets: %d)",
		raceID, pool.TotalPool, len(pool.Bets))
}

// placeBet places a bet on a horse in an open pool. Deducts cummies from the
// bettor's stable. Returns the updated pool or an error message.
func (s *Server) placeBet(raceID, userID, username, horseID string, amount int64) (*models.BettingPool, string) {
	if amount <= 0 {
		return nil, "bet amount must be positive"
	}
	if amount < 10 {
		return nil, "minimum bet is ₵10"
	}
	if amount > 100000 {
		return nil, "maximum bet is ₵100,000"
	}

	s.bettingMu.Lock()
	defer s.bettingMu.Unlock()

	pool, ok := s.bettingPools[raceID]
	if !ok {
		return nil, "no betting pool found for this race"
	}
	if pool.Status != "open" {
		return nil, "betting is closed for this race"
	}

	// Verify horse is in the pool.
	horseIdx := -1
	var horseName string
	for i, bh := range pool.Horses {
		if bh.HorseID == horseID {
			horseIdx = i
			horseName = bh.HorseName
			break
		}
	}
	if horseIdx < 0 {
		return nil, "horse is not in this race"
	}

	// Check if user already bet on this race (allow multiple bets, but cap at 3).
	userBetCount := 0
	for _, b := range pool.Bets {
		if b.UserID == userID {
			userBetCount++
		}
	}
	if userBetCount >= 3 {
		return nil, "maximum 3 bets per race"
	}

	// Deduct cummies from the bettor's stable.
	stable := s.getStableForUser(userID)
	if stable == nil {
		return nil, "you don't have a stable"
	}
	if stable.Cummies < amount {
		return nil, fmt.Sprintf("not enough cummies (have ₵%d, need ₵%d)", stable.Cummies, amount)
	}
	stable.Cummies -= amount
	s.persistStable(context.Background(), stable)

	// Record the bet.
	bet := models.Bet{
		ID:        fmt.Sprintf("bet_%s_%d", raceID, len(pool.Bets)+1),
		RaceID:    raceID,
		UserID:    userID,
		Username:  username,
		StableID:  stable.ID,
		HorseID:   horseID,
		HorseName: horseName,
		Amount:    amount,
		CreatedAt: time.Now(),
	}
	pool.Bets = append(pool.Bets, bet)
	pool.TotalPool += amount
	pool.Horses[horseIdx].TotalBet += amount
	pool.Horses[horseIdx].BetCount++

	// Recalculate odds.
	s.calcOddsLocked(pool)

	// Broadcast bet update.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":      "betting_update",
		"raceID":    raceID,
		"username":  username,
		"horseName": horseName,
		"amount":    amount,
		"totalPool": pool.TotalPool,
		"horses":    pool.Horses,
	})

	log.Printf("server: bet placed — %s bet ₵%d on %s (race %s, pool total: ₵%d)",
		username, amount, horseName, raceID, pool.TotalPool)

	return pool, ""
}

// resolveBets resolves a betting pool after a race completes. Pays out winners
// using pari-mutuel rules with a house cut. Returns the number of winners.
func (s *Server) resolveBets(raceID, winnerHorseID string) int {
	s.bettingMu.Lock()
	defer s.bettingMu.Unlock()

	pool, ok := s.bettingPools[raceID]
	if !ok {
		return 0
	}
	if pool.Status == "resolved" {
		return 0 // Already resolved.
	}

	pool.Status = "resolved"

	if len(pool.Bets) == 0 {
		// No bets placed — nothing to resolve.
		delete(s.bettingPools, raceID)
		return 0
	}

	// Calculate house cut and distributable pool.
	houseCut := int64(float64(pool.TotalPool) * bettingHouseCutPct)
	pool.HouseCut = houseCut
	distributable := pool.TotalPool - houseCut

	// Find total amount bet on the winning horse.
	winnerTotalBet := int64(0)
	for _, bh := range pool.Horses {
		if bh.HorseID == winnerHorseID {
			winnerTotalBet = bh.TotalBet
			break
		}
	}

	// Pay out winning bets proportionally from the distributable pool.
	winnerCount := 0
	var payoutMessages []string

	for i := range pool.Bets {
		bet := &pool.Bets[i]
		if bet.HorseID == winnerHorseID {
			bet.Won = true
			if winnerTotalBet > 0 {
				// Pari-mutuel: payout = (your bet / total bet on winner) * distributable pool.
				bet.Payout = (bet.Amount * distributable) / winnerTotalBet
			}
			winnerCount++

			// Credit winnings to the bettor's stable.
			betStable := s.getStableForUser(bet.UserID)
			if betStable != nil {
				betStable.Cummies += bet.Payout
				s.persistStable(context.Background(), betStable)

				// Grant betting_winner achievement for winning a bet.
				s.grantAchievementToStable(betStable, "betting_winner")
			}

			profit := bet.Payout - bet.Amount
			payoutMessages = append(payoutMessages,
				fmt.Sprintf("  🤑 %s won ₵%d (bet ₵%d, profit ₵%d)", bet.Username, bet.Payout, bet.Amount, profit))
		}
	}

	// Find winning horse name for broadcast.
	winnerHorseName := ""
	for _, bh := range pool.Horses {
		if bh.HorseID == winnerHorseID {
			winnerHorseName = bh.HorseName
			break
		}
	}

	// Broadcast resolution.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":            "betting_resolved",
		"raceID":          raceID,
		"winnerHorseID":   winnerHorseID,
		"winnerHorseName": winnerHorseName,
		"totalPool":       pool.TotalPool,
		"houseCut":        houseCut,
		"distributable":   distributable,
		"winnerCount":     winnerCount,
		"bets":            pool.Bets,
	})

	// Announce results in chat.
	if winnerCount > 0 {
		resultText := fmt.Sprintf("🎰 Betting results for %s!\n  Pool: ₵%d (house cut: ₵%d)\n%s",
			winnerHorseName, pool.TotalPool, houseCut, strings.Join(payoutMessages, "\n"))
		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": resultText,
			"ts":   time.Now().Unix(),
		})
	} else {
		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": fmt.Sprintf("🎰 No one bet on the winner %s! The house keeps ₵%d.",
				winnerHorseName, pool.TotalPool),
			"ts": time.Now().Unix(),
		})
	}

	log.Printf("server: betting resolved for race %s — %d winners, pool: ₵%d, house cut: ₵%d",
		raceID, winnerCount, pool.TotalPool, houseCut)

	// Clean up old pools after a delay (keep for 5 minutes for queries).
	go func() {
		time.Sleep(5 * time.Minute)
		s.bettingMu.Lock()
		delete(s.bettingPools, raceID)
		s.bettingMu.Unlock()
	}()

	return winnerCount
}

// calcOddsLocked recalculates pari-mutuel odds for all horses in a pool.
// MUST be called while holding s.bettingMu.
func (s *Server) calcOddsLocked(pool *models.BettingPool) {
	if pool.TotalPool == 0 {
		for i := range pool.Horses {
			pool.Horses[i].Odds = 0
		}
		return
	}
	for i := range pool.Horses {
		if pool.Horses[i].TotalBet > 0 {
			// Odds = (total pool / amount on this horse) — represents the
			// multiplier a bettor would receive (before house cut).
			pool.Horses[i].Odds = float64(pool.TotalPool) / float64(pool.Horses[i].TotalBet)
		} else {
			pool.Horses[i].Odds = 0 // No bets yet — odds undefined.
		}
	}
}

// findActiveBettingPool returns the most recently opened betting pool that
// is still "open". Returns nil if none found.
func (s *Server) findActiveBettingPool() *models.BettingPool {
	s.bettingMu.RLock()
	defer s.bettingMu.RUnlock()

	var latest *models.BettingPool
	for _, pool := range s.bettingPools {
		if pool.Status == "open" {
			if latest == nil || pool.OpenedAt.After(latest.OpenedAt) {
				latest = pool
			}
		}
	}
	return latest
}

// findHorseInPool does fuzzy name matching to find a horse in a betting pool.
// Returns (horseID, horseName) or ("", "") if not found.
func (s *Server) findHorseInPool(pool *models.BettingPool, name string) (string, string) {
	lowerName := strings.ToLower(name)

	// Exact case-insensitive match first.
	for _, bh := range pool.Horses {
		if strings.EqualFold(bh.HorseName, name) {
			return bh.HorseID, bh.HorseName
		}
	}

	// Fuzzy: substring match.
	for _, bh := range pool.Horses {
		if strings.Contains(strings.ToLower(bh.HorseName), lowerName) {
			return bh.HorseID, bh.HorseName
		}
	}

	return "", ""
}

// ---------------------------------------------------------------------------
// HTTP handlers for betting API
// ---------------------------------------------------------------------------

// handleOpenBettingPool creates a betting pool for a given race.
// POST /api/betting/pools  { raceID, horseIDs: [] }
func (s *Server) handleOpenBettingPool(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RaceID   string   `json:"raceID"`
		HorseIDs []string `json:"horseIDs"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RaceID == "" || len(req.HorseIDs) < 2 {
		writeError(w, http.StatusBadRequest, "raceID and at least 2 horseIDs required")
		return
	}

	// Check if pool already exists.
	s.bettingMu.RLock()
	if _, exists := s.bettingPools[req.RaceID]; exists {
		s.bettingMu.RUnlock()
		writeError(w, http.StatusConflict, "betting pool already exists for this race")
		return
	}
	s.bettingMu.RUnlock()

	// Resolve horse IDs to horse objects.
	var horses []*models.Horse
	for _, hid := range req.HorseIDs {
		h, err := s.stables.GetHorse(hid)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("horse not found: %s", hid))
			return
		}
		horses = append(horses, h)
	}

	pool := s.openBettingPool(req.RaceID, horses)
	writeJSON(w, http.StatusCreated, pool)
}

// handleGetBettingPool returns the betting pool for a specific race.
// GET /api/betting/pools/{raceID}
func (s *Server) handleGetBettingPool(w http.ResponseWriter, r *http.Request) {
	raceID := r.PathValue("raceID")
	if raceID == "" {
		writeError(w, http.StatusBadRequest, "raceID required")
		return
	}

	s.bettingMu.RLock()
	pool, ok := s.bettingPools[raceID]
	s.bettingMu.RUnlock()

	if !ok {
		writeError(w, http.StatusNotFound, "no betting pool for this race")
		return
	}

	writeJSON(w, http.StatusOK, pool)
}

// handlePlaceBet places a bet on a horse in an open pool.
// POST /api/betting/pools/{raceID}/bet  { horseID, amount }
func (s *Server) handlePlaceBet(w http.ResponseWriter, r *http.Request) {
	raceID := r.PathValue("raceID")
	if raceID == "" {
		writeError(w, http.StatusBadRequest, "raceID required")
		return
	}

	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	userID := claims.UserID
	username := claims.Username

	var req struct {
		HorseID string `json:"horseID"`
		Amount  int64  `json:"amount"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.HorseID == "" || req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "horseID and positive amount required")
		return
	}

	pool, errMsg := s.placeBet(raceID, userID, username, req.HorseID, req.Amount)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	writeJSON(w, http.StatusOK, pool)
}

// handleListActivePools returns all betting pools that are currently open.
// GET /api/betting/active
func (s *Server) handleListActivePools(w http.ResponseWriter, r *http.Request) {
	s.bettingMu.RLock()
	defer s.bettingMu.RUnlock()

	var active []*models.BettingPool
	for _, pool := range s.bettingPools {
		if pool.Status == "open" || pool.Status == "closed" {
			active = append(active, pool)
		}
	}
	if active == nil {
		active = []*models.BettingPool{} // Return empty array, not null.
	}

	writeJSON(w, http.StatusOK, active)
}

// ---------------------------------------------------------------------------
// Chat command handler: /bet horsename amount
// ---------------------------------------------------------------------------

func (s *Server) handleChatBet(senderUserID, senderUsername string, args map[string]interface{}) {
	horseName, _ := args["horse"].(string)

	// Parse amount from args (JSON numbers come as float64).
	amount := int64(0)
	if a, ok := args["amount"].(float64); ok && a > 0 {
		amount = int64(a)
	}

	if horseName == "" || amount <= 0 {
		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": "*** Usage: /bet horsename amount",
			"ts":   time.Now().Unix(),
		})
		return
	}

	// Find the active betting pool.
	pool := s.findActiveBettingPool()
	if pool == nil {
		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": "*** No active betting pool right now.",
			"ts":   time.Now().Unix(),
		})
		return
	}

	// Fuzzy match horse name within the pool.
	horseID, matchedName := s.findHorseInPool(pool, horseName)
	if horseID == "" {
		// List available horses for the user.
		var names []string
		for _, bh := range pool.Horses {
			names = append(names, bh.HorseName)
		}
		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": fmt.Sprintf("*** Horse %q not found in the pool. Available: %s",
				horseName, strings.Join(names, ", ")),
			"ts": time.Now().Unix(),
		})
		return
	}

	// Place the bet.
	_, errMsg := s.placeBet(pool.RaceID, senderUserID, senderUsername, horseID, amount)
	if errMsg != "" {
		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": fmt.Sprintf("*** Bet failed: %s", errMsg),
			"ts":   time.Now().Unix(),
		})
		return
	}

	// Confirmation is already broadcast by placeBet via betting_update.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type": "chat_system",
		"text": fmt.Sprintf("🎰 %s bet ₵%d on %s!", senderUsername, amount, matchedName),
		"ts":   time.Now().Unix(),
	})
}

// ===========================================================================
// Rivalry Endpoint
// ===========================================================================

// rivalRecord represents a single rivalry entry returned by the rivals endpoint.
type rivalRecord struct {
	HorseID   string `json:"horseID"`
	HorseName string `json:"horseName"`
	WinsVs    int    `json:"winsVs"`   // Times this horse beat the rival
	LossesVs  int    `json:"lossesVs"` // Times the rival beat this horse
	Total     int    `json:"total"`    // Total head-to-head encounters
}

// handleGetHorseRivals returns the top 5 rivals for a given horse based on
// head-to-head race records.
// GET /api/horses/{id}/rivals
func (s *Server) handleGetHorseRivals(w http.ResponseWriter, r *http.Request) {
	horseID := r.PathValue("id")

	// Verify the horse exists.
	horse, err := s.stables.GetHorse(horseID)
	if err != nil || horse == nil {
		writeError(w, http.StatusNotFound, "horse not found")
		return
	}

	s.rivalryMu.RLock()
	defer s.rivalryMu.RUnlock()

	// Collect all opponents this horse has faced (in either direction).
	opponentTotals := make(map[string]*rivalRecord)

	// Wins by this horse against opponents.
	if wins, ok := s.rivalries[horseID]; ok {
		for oppID, count := range wins {
			if opponentTotals[oppID] == nil {
				opponentTotals[oppID] = &rivalRecord{HorseID: oppID}
			}
			opponentTotals[oppID].WinsVs = count
		}
	}

	// Losses: other horses that beat this horse.
	for otherID, victims := range s.rivalries {
		if count, ok := victims[horseID]; ok && count > 0 {
			if opponentTotals[otherID] == nil {
				opponentTotals[otherID] = &rivalRecord{HorseID: otherID}
			}
			opponentTotals[otherID].LossesVs = count
		}
	}

	// Calculate totals and resolve horse names.
	var rivals []rivalRecord
	for _, rec := range opponentTotals {
		rec.Total = rec.WinsVs + rec.LossesVs
		if opp, err := s.stables.GetHorse(rec.HorseID); err == nil && opp != nil {
			rec.HorseName = opp.Name
		} else {
			rec.HorseName = "Unknown"
		}
		rivals = append(rivals, *rec)
	}

	// Sort by total encounters descending, then by wins descending.
	sort.Slice(rivals, func(i, j int) bool {
		if rivals[i].Total != rivals[j].Total {
			return rivals[i].Total > rivals[j].Total
		}
		return rivals[i].WinsVs > rivals[j].WinsVs
	})

	// Return top 5.
	if len(rivals) > 5 {
		rivals = rivals[:5]
	}

	writeJSON(w, http.StatusOK, rivals)
}

// ===========================================================================
// Update Stable Endpoint
// ===========================================================================

// handleUpdateStable allows a stable owner to update their stable's name and motto.
// PUT /api/stables/{id}
func (s *Server) handleUpdateStable(w http.ResponseWriter, r *http.Request) {
	stableID := r.PathValue("id")

	// Verify ownership.
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	stable, err := s.stables.GetStable(stableID)
	if err != nil {
		writeError(w, http.StatusNotFound, "stable not found: "+err.Error())
		return
	}

	if stable.OwnerID != claims.UserID {
		writeError(w, http.StatusForbidden, "you can only update your own stable")
		return
	}

	var req struct {
		Name  *string `json:"name"`
		Motto *string `json:"motto"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	// Apply updates (only if provided).
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "stable name cannot be empty")
			return
		}
		if len(name) > 50 {
			writeError(w, http.StatusBadRequest, "stable name must be 50 characters or fewer")
			return
		}
		stable.Name = name
	}
	if req.Motto != nil {
		motto := strings.TrimSpace(*req.Motto)
		if len(motto) > 200 {
			writeError(w, http.StatusBadRequest, "motto must be 200 characters or fewer")
			return
		}
		stable.Motto = motto
	}

	// Write-through: persist updated stable to DB.
	s.persistStable(r.Context(), stable)

	log.Printf("server: stable %s (%s) updated by %s", stable.ID, stable.Name, claims.Username)

	// Broadcast stable update event.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":     "stable_updated",
		"stableID": stable.ID,
		"name":     stable.Name,
		"motto":    stable.Motto,
	})

	writeJSON(w, http.StatusOK, stable)
}

// ===========================================================================
// Retire Horse Endpoint
// ===========================================================================

// handleRetireHorse retires a horse owned by the authenticated user.
// POST /api/horses/{id}/retire
func (s *Server) handleRetireHorse(w http.ResponseWriter, r *http.Request) {
	horseID := r.PathValue("id")

	// Verify ownership.
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	if !s.userOwnsHorse(claims.UserID, horseID) {
		writeError(w, http.StatusForbidden, "you can only retire your own horses")
		return
	}

	horse, err := s.stables.GetHorse(horseID)
	if err != nil || horse == nil {
		writeError(w, http.StatusNotFound, "horse not found")
		return
	}

	if horse.Retired {
		writeError(w, http.StatusBadRequest, "horse is already retired")
		return
	}

	// Parse optional reason from request body.
	var req struct {
		Reason string `json:"reason"`
	}
	_ = readJSON(r, &req) // Body is optional; ignore errors.

	reason := req.Reason
	if reason == "" {
		reason = "Retired by owner"
	}

	// Retire the horse (may grant Hall of Fame status if 5+ wins).
	trainussy.RetireHorse(horse, reason)

	// Sync the updated horse back to the stable.
	s.syncHorseToStable(horse)

	// Write-through: persist horse state to DB.
	s.persistHorse(r.Context(), horse)

	log.Printf("server: horse %s (%s) retired by %s (champion: %v)",
		horse.ID, horse.Name, claims.Username, horse.RetiredChampion)

	// Broadcast retirement event.
	retireEvent := map[string]interface{}{
		"type":            "horse_retired",
		"horseID":         horse.ID,
		"horseName":       horse.Name,
		"retiredChampion": horse.RetiredChampion,
		"wins":            horse.Wins,
	}
	s.hub.BroadcastJSON(retireEvent)

	// Announce in chat if it's a champion retirement.
	if horse.RetiredChampion {
		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": fmt.Sprintf("🏆 %s has been inducted into the Hall of Fame with %d career wins! A legend retires.", horse.Name, horse.Wins),
			"ts":   time.Now().Unix(),
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"horse":           horse,
		"retiredChampion": horse.RetiredChampion,
		"message":         fmt.Sprintf("%s has been retired. %s", horse.Name, reason),
	})
}

// ===========================================================================
// Live Auction System — HTTP Handlers
// ===========================================================================

// handleListAuctions returns all active auctions (status open or ending).
// GET /api/auctions
func (s *Server) handleListAuctions(w http.ResponseWriter, r *http.Request) {
	s.auctionMu.RLock()
	defer s.auctionMu.RUnlock()

	active := make([]*models.Auction, 0)
	for _, a := range s.auctions {
		if a.Status == models.AuctionStatusOpen || a.Status == models.AuctionStatusEnding {
			active = append(active, a)
		}
	}

	// Sort by expires_at ascending (soonest ending first).
	sort.Slice(active, func(i, j int) bool {
		return active[i].ExpiresAt.Before(active[j].ExpiresAt)
	})

	writeJSON(w, http.StatusOK, active)
}

// handleCreateAuction creates a new auction for a horse owned by the auth'd user.
// POST /api/auctions
func (s *Server) handleCreateAuction(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required — Dr. Mittens demands credentials")
		return
	}

	var req struct {
		HorseID     string `json:"horseID"`
		StartingBid int64  `json:"startingBid"`
		Duration    int    `json:"duration"` // seconds, default 120
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.HorseID == "" {
		writeError(w, http.StatusBadRequest, "horseID is required")
		return
	}
	if req.StartingBid < 1 {
		writeError(w, http.StatusBadRequest, "startingBid must be at least 1 cummies")
		return
	}
	if req.Duration <= 0 {
		req.Duration = 120 // Default 2 minutes
	}
	if req.Duration > 600 {
		req.Duration = 600 // Max 10 minutes
	}

	// Verify ownership.
	if !s.userOwnsHorse(claims.UserID, req.HorseID) {
		writeError(w, http.StatusForbidden, "you can only auction your own horses — no horse theft in B.U.R.P. territory")
		return
	}

	// Get horse details.
	horse, err := s.stables.GetHorse(req.HorseID)
	if err != nil || horse == nil {
		writeError(w, http.StatusNotFound, "horse not found")
		return
	}
	if horse.Retired {
		writeError(w, http.StatusBadRequest, "retired horses cannot be auctioned — they've earned their rest")
		return
	}

	// Check horse is not already in an active auction.
	s.auctionMu.RLock()
	for _, a := range s.auctions {
		if a.HorseID == req.HorseID && (a.Status == models.AuctionStatusOpen || a.Status == models.AuctionStatusEnding) {
			s.auctionMu.RUnlock()
			writeError(w, http.StatusConflict, "this horse is already in an active auction")
			return
		}
	}
	s.auctionMu.RUnlock()

	// Find the seller's stable.
	sellerStable := s.getStableForUser(claims.UserID)
	if sellerStable == nil {
		writeError(w, http.StatusBadRequest, "you need a stable to auction horses")
		return
	}

	now := time.Now()
	auction := &models.Auction{
		ID:          uuid.New().String(),
		SellerID:    claims.UserID,
		SellerName:  claims.Username,
		StableID:    sellerStable.ID,
		HorseID:     req.HorseID,
		HorseName:   horse.Name,
		StartingBid: req.StartingBid,
		CurrentBid:  0,
		BidderID:    "",
		BidderName:  "",
		BidCount:    0,
		BidHistory:  []models.AuctionBid{},
		Status:      models.AuctionStatusOpen,
		Duration:    req.Duration,
		CreatedAt:   now,
		ExpiresAt:   now.Add(time.Duration(req.Duration) * time.Second),
	}

	// Store in memory.
	s.auctionMu.Lock()
	s.auctions[auction.ID] = auction
	s.auctionMu.Unlock()

	// Persist to DB.
	s.persistAuction(r.Context(), auction)

	log.Printf("server: auction %s created by %s for horse %s (%s) — starting at %d cummies",
		auction.ID, claims.Username, horse.Name, horse.ID, req.StartingBid)

	// Broadcast auction creation.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":    "auction_created",
		"auction": auction,
	})
	s.hub.BroadcastJSON(map[string]interface{}{
		"type": "chat_system",
		"text": fmt.Sprintf("🔨 NEW AUCTION: %s is selling %s! Starting bid: %d cummies. Ends in %ds. Place your bids!",
			claims.Username, horse.Name, req.StartingBid, req.Duration),
		"ts": now.Unix(),
	})

	writeJSON(w, http.StatusCreated, auction)
}

// handleGetAuction returns a single auction by ID.
// GET /api/auctions/{id}
func (s *Server) handleGetAuction(w http.ResponseWriter, r *http.Request) {
	auctionID := r.PathValue("id")

	s.auctionMu.RLock()
	auction, ok := s.auctions[auctionID]
	s.auctionMu.RUnlock()

	if !ok {
		writeError(w, http.StatusNotFound, "auction not found — perhaps it was a ghost auction from E-008's realm")
		return
	}

	writeJSON(w, http.StatusOK, auction)
}

// handlePlaceAuctionBid places a bid on an active auction.
// POST /api/auctions/{id}/bid
func (s *Server) handlePlaceAuctionBid(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	auctionID := r.PathValue("id")

	var req struct {
		Amount int64 `json:"amount"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "bid amount must be positive")
		return
	}

	s.auctionMu.Lock()
	auction, exists := s.auctions[auctionID]
	if !exists {
		s.auctionMu.Unlock()
		writeError(w, http.StatusNotFound, "auction not found")
		return
	}

	// Validate auction is still open.
	if auction.Status != models.AuctionStatusOpen && auction.Status != models.AuctionStatusEnding {
		s.auctionMu.Unlock()
		writeError(w, http.StatusBadRequest, "this auction is no longer accepting bids")
		return
	}

	// Can't bid on your own auction.
	if auction.SellerID == claims.UserID {
		s.auctionMu.Unlock()
		writeError(w, http.StatusBadRequest, "you cannot bid on your own auction — Pastor Router McEthernet III frowns upon self-dealing")
		return
	}

	// Bid must meet or exceed starting bid.
	if req.Amount < auction.StartingBid {
		s.auctionMu.Unlock()
		writeError(w, http.StatusBadRequest, fmt.Sprintf("bid must be at least %d cummies (starting bid)", auction.StartingBid))
		return
	}

	// Bid must be higher than current bid.
	if req.Amount <= auction.CurrentBid {
		s.auctionMu.Unlock()
		writeError(w, http.StatusBadRequest, fmt.Sprintf("bid must exceed current bid of %d cummies", auction.CurrentBid))
		return
	}

	// Check bidder has enough cummies.
	bidderStable := s.getStableForUser(claims.UserID)
	if bidderStable == nil {
		s.auctionMu.Unlock()
		writeError(w, http.StatusBadRequest, "you need a stable to place bids")
		return
	}
	if bidderStable.Cummies < req.Amount {
		s.auctionMu.Unlock()
		writeError(w, http.StatusBadRequest, fmt.Sprintf("insufficient cummies — you have %d but need %d", bidderStable.Cummies, req.Amount))
		return
	}

	// Escrow: deduct bid amount from bidder.
	bidderStable.Cummies -= req.Amount
	s.persistStable(r.Context(), bidderStable)

	// Refund previous bidder (if any).
	previousBidderID := auction.BidderID
	previousBidAmount := auction.CurrentBid
	if previousBidderID != "" && previousBidAmount > 0 {
		prevStable := s.getStableForUser(previousBidderID)
		if prevStable != nil {
			prevStable.Cummies += previousBidAmount
			s.persistStable(r.Context(), prevStable)
			log.Printf("server: auction %s — refunded %d cummies to %s",
				auctionID, previousBidAmount, auction.BidderName)
		}
	}

	// Record the bid.
	now := time.Now()
	bid := models.AuctionBid{
		BidderID:   claims.UserID,
		BidderName: claims.Username,
		Amount:     req.Amount,
		Timestamp:  now,
	}
	auction.BidHistory = append(auction.BidHistory, bid)
	auction.CurrentBid = req.Amount
	auction.BidderID = claims.UserID
	auction.BidderName = claims.Username
	auction.BidCount++

	// Anti-snipe: if less than 30s remaining, extend by 30s.
	timeLeft := time.Until(auction.ExpiresAt)
	if timeLeft < 30*time.Second {
		auction.ExpiresAt = now.Add(30 * time.Second)
		auction.Status = models.AuctionStatusEnding
		log.Printf("server: auction %s — anti-snipe extension! New expiry: %s",
			auctionID, auction.ExpiresAt.Format(time.RFC3339))
	}

	s.auctionMu.Unlock()

	// Persist updated auction.
	s.persistAuction(r.Context(), auction)

	log.Printf("server: auction %s — bid of %d cummies by %s (bid #%d)",
		auctionID, req.Amount, claims.Username, auction.BidCount)

	// Broadcast bid event.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":    "auction_bid",
		"auction": auction,
		"bid":     bid,
	})
	s.hub.BroadcastJSON(map[string]interface{}{
		"type": "chat_system",
		"text": fmt.Sprintf("💰 %s bid %d cummies on %s! (bid #%d)",
			claims.Username, req.Amount, auction.HorseName, auction.BidCount),
		"ts": now.Unix(),
	})

	// Notify previous bidder they were outbid.
	if previousBidderID != "" {
		s.hub.BroadcastJSON(map[string]interface{}{
			"type":      "auction_outbid",
			"auctionID": auctionID,
			"horseName": auction.HorseName,
			"newBid":    req.Amount,
			"newBidder": claims.Username,
			"oldBidder": auction.BidHistory[len(auction.BidHistory)-2].BidderName,
			"refund":    previousBidAmount,
		})
	}

	writeJSON(w, http.StatusOK, auction)
}

// handleCancelAuction cancels an auction (only if no bids have been placed).
// DELETE /api/auctions/{id}
func (s *Server) handleCancelAuction(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	auctionID := r.PathValue("id")

	s.auctionMu.Lock()
	auction, exists := s.auctions[auctionID]
	if !exists {
		s.auctionMu.Unlock()
		writeError(w, http.StatusNotFound, "auction not found")
		return
	}

	// Only the seller can cancel.
	if auction.SellerID != claims.UserID {
		s.auctionMu.Unlock()
		writeError(w, http.StatusForbidden, "only the seller can cancel this auction")
		return
	}

	// Can only cancel if no bids.
	if auction.BidCount > 0 {
		s.auctionMu.Unlock()
		writeError(w, http.StatusBadRequest, "cannot cancel an auction with active bids — the people have spoken")
		return
	}

	// Can only cancel open auctions.
	if auction.Status != models.AuctionStatusOpen {
		s.auctionMu.Unlock()
		writeError(w, http.StatusBadRequest, "can only cancel open auctions")
		return
	}

	auction.Status = models.AuctionStatusCancelled
	auction.CompletedAt = time.Now()
	s.auctionMu.Unlock()

	// Persist.
	s.persistAuction(r.Context(), auction)

	log.Printf("server: auction %s cancelled by %s", auctionID, claims.Username)

	// Broadcast cancellation.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type":    "auction_cancelled",
		"auction": auction,
	})
	s.hub.BroadcastJSON(map[string]interface{}{
		"type": "chat_system",
		"text": fmt.Sprintf("❌ %s cancelled the auction for %s.", claims.Username, auction.HorseName),
		"ts":   time.Now().Unix(),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "auction cancelled",
		"auction": auction,
	})
}

// ===========================================================================
// Auction Expiry Loop
// ===========================================================================

// auctionExpiryLoop runs every 5 seconds to check for expired auctions,
// transfer horses to winners, and apply the Geoffrussy Tax.
func (s *Server) auctionExpiryLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		s.auctionMu.Lock()

		for _, auction := range s.auctions {
			// Only process open/ending auctions that have expired.
			if (auction.Status != models.AuctionStatusOpen && auction.Status != models.AuctionStatusEnding) ||
				now.Before(auction.ExpiresAt) {
				continue
			}

			ctx := context.Background()

			if auction.BidCount > 0 && auction.BidderID != "" {
				// SOLD — transfer horse to winner.
				auction.Status = models.AuctionStatusSold
				auction.CompletedAt = now

				// Calculate 5% Geoffrussy Tax (burned from economy).
				tax := auction.CurrentBid * 5 / 100
				sellerPayout := auction.CurrentBid - tax
				auction.GeoffrussyTax = tax

				// Pay the seller (minus tax). Bid was already escrowed.
				sellerStable := s.getStableForUser(auction.SellerID)
				if sellerStable != nil {
					sellerStable.Cummies += sellerPayout
					s.persistStable(ctx, sellerStable)
				}

				// Transfer the horse from seller's stable to buyer's stable.
				buyerStable := s.getStableForUser(auction.BidderID)
				if buyerStable != nil && sellerStable != nil {
					if err := s.stables.MoveHorse(auction.HorseID, sellerStable.ID, buyerStable.ID); err != nil {
						log.Printf("server: auction %s — failed to move horse: %v", auction.ID, err)
					} else {
						// Update horse owner ID.
						if horse, err := s.stables.GetHorse(auction.HorseID); err == nil && horse != nil {
							horse.OwnerID = buyerStable.OwnerID
							s.persistHorse(ctx, horse)
						}
						log.Printf("server: auction %s SOLD — %s goes to %s for %d cummies (tax: %d)",
							auction.ID, auction.HorseName, auction.BidderName, auction.CurrentBid, tax)
					}
				}

				s.persistAuction(ctx, auction)

				// Broadcast sold event.
				s.hub.BroadcastJSON(map[string]interface{}{
					"type":    "auction_sold",
					"auction": auction,
				})
				s.hub.BroadcastJSON(map[string]interface{}{
					"type": "chat_system",
					"text": fmt.Sprintf("🏇 SOLD! %s won the auction for %s — %d cummies! (Geoffrussy Tax: %d cummies burned 🔥)",
						auction.BidderName, auction.HorseName, auction.CurrentBid, tax),
					"ts": now.Unix(),
				})
			} else {
				// EXPIRED — no bids, horse stays with seller.
				auction.Status = models.AuctionStatusExpired
				auction.CompletedAt = now

				s.persistAuction(ctx, auction)

				log.Printf("server: auction %s expired — %s returned to %s (no bids)",
					auction.ID, auction.HorseName, auction.SellerName)

				// Broadcast expiry.
				s.hub.BroadcastJSON(map[string]interface{}{
					"type":    "auction_expired",
					"auction": auction,
				})
				s.hub.BroadcastJSON(map[string]interface{}{
					"type": "chat_system",
					"text": fmt.Sprintf("⏰ Auction for %s expired with no bids. Better luck next time, %s.",
						auction.HorseName, auction.SellerName),
					"ts": now.Unix(),
				})
			}
		}

		s.auctionMu.Unlock()
	}
}

// ===========================================================================
// Whisper Chat Command
// ===========================================================================

// handleChatWhisper processes the /w, /whisper, /msg, /pm chat commands.
// Usage: /w @username message text here
// The "target" and "text" fields are extracted from the args map, which is
// populated by the frontend's chat command parser.
func (s *Server) handleChatWhisper(senderUserID, senderUsername string, args map[string]interface{}) {
	target, _ := args["target"].(string)
	text, _ := args["text"].(string)

	// Strip leading @ from target if present.
	target = strings.TrimPrefix(target, "@")

	if target == "" || text == "" {
		s.hub.SendToUser(senderUserID, map[string]interface{}{
			"type": "whisper_error",
			"text": "Usage: /w @username message",
			"ts":   time.Now().Unix(),
		})
		return
	}

	// Don't allow whispering yourself.
	if strings.EqualFold(senderUsername, target) {
		s.hub.SendToUser(senderUserID, map[string]interface{}{
			"type": "whisper_error",
			"text": "You can't whisper to yourself, weirdo.",
			"ts":   time.Now().Unix(),
		})
		return
	}

	// Find the target client by username.
	targetClient := s.hub.GetClientByUsername(target)
	if targetClient == nil {
		s.hub.SendToUser(senderUserID, map[string]interface{}{
			"type": "whisper_error",
			"text": fmt.Sprintf("User '%s' is not online.", target),
			"ts":   time.Now().Unix(),
		})
		return
	}

	now := time.Now()

	// Send whisper to target user (all their connections).
	s.hub.SendToUser(targetClient.UserID, map[string]interface{}{
		"type":       "whisper",
		"from":       senderUsername,
		"fromUserID": senderUserID,
		"text":       text,
		"ts":         now.Unix(),
	})

	// Echo back to sender as confirmation.
	s.hub.SendToUser(senderUserID, map[string]interface{}{
		"type":     "whisper_sent",
		"to":       targetClient.Username,
		"toUserID": targetClient.UserID,
		"text":     text,
		"ts":       now.Unix(),
	})
}

// ===========================================================================
// Auction Chat Command
// ===========================================================================

// handleChatAuction processes the /auction chat command.
// Usage: /auction <horseID> <startingBid> [duration]
func (s *Server) handleChatAuction(senderUserID, senderUsername string, args map[string]interface{}) {
	horseID, _ := args["horseID"].(string)
	if horseID == "" {
		horseID, _ = args["horse"].(string)
	}
	startingBidF, _ := args["startingBid"].(float64)
	startingBid := int64(startingBidF)
	if startingBid == 0 {
		amtF, _ := args["amount"].(float64)
		startingBid = int64(amtF)
	}
	durationF, _ := args["duration"].(float64)
	duration := int(durationF)

	if horseID == "" || startingBid <= 0 {
		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": fmt.Sprintf("⚠️ %s — Usage: /auction <horseID> <startingBid> [duration]", senderUsername),
			"ts":   time.Now().Unix(),
		})
		return
	}

	if duration <= 0 {
		duration = 120
	}
	if duration > 600 {
		duration = 600
	}

	// Verify ownership.
	if !s.userOwnsHorse(senderUserID, horseID) {
		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": fmt.Sprintf("⚠️ %s — You don't own that horse!", senderUsername),
			"ts":   time.Now().Unix(),
		})
		return
	}

	horse, err := s.stables.GetHorse(horseID)
	if err != nil || horse == nil {
		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": fmt.Sprintf("⚠️ %s — Horse not found.", senderUsername),
			"ts":   time.Now().Unix(),
		})
		return
	}

	if horse.Retired {
		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": fmt.Sprintf("⚠️ %s — %s is retired and cannot be auctioned.", senderUsername, horse.Name),
			"ts":   time.Now().Unix(),
		})
		return
	}

	// Check not already in auction.
	s.auctionMu.RLock()
	for _, a := range s.auctions {
		if a.HorseID == horseID && (a.Status == models.AuctionStatusOpen || a.Status == models.AuctionStatusEnding) {
			s.auctionMu.RUnlock()
			s.hub.BroadcastJSON(map[string]interface{}{
				"type": "chat_system",
				"text": fmt.Sprintf("⚠️ %s — %s is already in an active auction!", senderUsername, horse.Name),
				"ts":   time.Now().Unix(),
			})
			return
		}
	}
	s.auctionMu.RUnlock()

	sellerStable := s.getStableForUser(senderUserID)
	if sellerStable == nil {
		return
	}

	now := time.Now()
	auction := &models.Auction{
		ID:          uuid.New().String(),
		SellerID:    senderUserID,
		SellerName:  senderUsername,
		StableID:    sellerStable.ID,
		HorseID:     horseID,
		HorseName:   horse.Name,
		StartingBid: startingBid,
		CurrentBid:  0,
		BidHistory:  []models.AuctionBid{},
		Status:      models.AuctionStatusOpen,
		Duration:    duration,
		CreatedAt:   now,
		ExpiresAt:   now.Add(time.Duration(duration) * time.Second),
	}

	s.auctionMu.Lock()
	s.auctions[auction.ID] = auction
	s.auctionMu.Unlock()

	s.persistAuction(context.Background(), auction)

	log.Printf("server: auction %s created via chat by %s for %s — starting at %d cummies",
		auction.ID, senderUsername, horse.Name, startingBid)

	s.hub.BroadcastJSON(map[string]interface{}{
		"type":    "auction_created",
		"auction": auction,
	})
	s.hub.BroadcastJSON(map[string]interface{}{
		"type": "chat_system",
		"text": fmt.Sprintf("🔨 NEW AUCTION: %s is selling %s! Starting bid: %d cummies. Ends in %ds. Place your bids!",
			senderUsername, horse.Name, startingBid, duration),
		"ts": now.Unix(),
	})
}

// ===========================================================================
// Auction Persistence Helper
// ===========================================================================

// persistAuction creates or updates an auction in the database. No-op when DB is nil.
func (s *Server) persistAuction(ctx context.Context, auction *models.Auction) {
	if s.auctionRepo == nil {
		return
	}
	if err := s.auctionRepo.CreateAuction(ctx, auction); err != nil {
		if err2 := s.auctionRepo.UpdateAuction(ctx, auction); err2 != nil {
			log.Printf("server: persistAuction %s: create=%v update=%v", auction.ID, err, err2)
		}
	}
}

// ===========================================================================
// Race Replay Persistence
// ===========================================================================

// persistRaceReplay saves a full race result to the database for long-term
// replay storage. No-op when the replay repo is nil.
func (s *Server) persistRaceReplay(raceID string, result *raceResult) {
	if s.replayRepo == nil {
		return
	}

	// Marshal the full result to JSON for the data column.
	data, err := json.Marshal(result)
	if err != nil {
		log.Printf("server: persistRaceReplay marshal error for %s: %v", raceID, err)
		return
	}

	// Extract winner info from the race entries.
	winnerID := ""
	winnerName := ""
	if result.Race != nil {
		for _, entry := range result.Race.Entries {
			if entry.FinishPlace == 1 {
				winnerID = entry.HorseID
				winnerName = entry.HorseName
				break
			}
		}
	}

	replay := &models.RaceReplay{
		RaceID:     raceID,
		TrackType:  string(result.Race.TrackType),
		Distance:   result.Race.Distance,
		Purse:      result.Race.Purse,
		Entries:    len(result.Race.Entries),
		Weather:    string(result.Weather),
		WinnerID:   winnerID,
		WinnerName: winnerName,
		Data:       data,
		CreatedAt:  time.Now(),
	}

	ctx := context.Background()
	if err := s.replayRepo.SaveReplay(ctx, replay); err != nil {
		log.Printf("server: persistRaceReplay %s: %v", raceID, err)
	}
}

// handleListRecentReplays returns recent race replays for the race history UI.
func (s *Server) handleListRecentReplays(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	// If DB is available, fetch from there.
	if s.replayRepo != nil {
		replays, err := s.replayRepo.ListRecentReplays(r.Context(), limit)
		if err != nil {
			log.Printf("server: handleListRecentReplays DB error: %v", err)
			// Fall through to in-memory fallback.
		} else {
			writeJSON(w, http.StatusOK, replays)
			return
		}
	}

	// Fallback: build list from in-memory cache.
	s.raceCacheMu.RLock()
	results := make([]map[string]interface{}, 0, len(s.raceCache))
	for raceID, result := range s.raceCache {
		winnerID := ""
		winnerName := ""
		if result.Race != nil {
			for _, entry := range result.Race.Entries {
				if entry.FinishPlace == 1 {
					winnerID = entry.HorseID
					winnerName = entry.HorseName
					break
				}
			}
		}
		results = append(results, map[string]interface{}{
			"raceID":     raceID,
			"trackType":  string(result.Race.TrackType),
			"distance":   result.Race.Distance,
			"purse":      result.Race.Purse,
			"entries":    len(result.Race.Entries),
			"weather":    string(result.Weather),
			"winnerID":   winnerID,
			"winnerName": winnerName,
			"createdAt":  result.Race.CreatedAt,
		})
	}
	s.raceCacheMu.RUnlock()

	// Sort by createdAt descending and limit.
	sort.Slice(results, func(i, j int) bool {
		ti, _ := results[i]["createdAt"].(time.Time)
		tj, _ := results[j]["createdAt"].(time.Time)
		return ti.After(tj)
	})
	if len(results) > limit {
		results = results[:limit]
	}

	writeJSON(w, http.StatusOK, results)
}

// replayCleanupLoop periodically deletes race replays older than 7 days.
// Runs every hour. No-op when the replay repo is nil.
func (s *Server) replayCleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		if s.replayRepo == nil {
			continue
		}
		cutoff := time.Now().Add(-7 * 24 * time.Hour)
		count, err := s.replayRepo.DeleteOldReplays(context.Background(), cutoff)
		if err != nil {
			log.Printf("server: replayCleanupLoop error: %v", err)
		} else if count > 0 {
			log.Printf("server: replayCleanupLoop deleted %d old replays", count)
		}
	}
}

// ===========================================================================
// Stable Alliances / Guild System — HTTP Handlers
// ===========================================================================

// persistAlliance creates or updates an alliance in the database. No-op when DB is nil.
func (s *Server) persistAlliance(ctx context.Context, alliance *models.Alliance) {
	if s.allianceRepo == nil {
		return
	}
	if err := s.allianceRepo.CreateAlliance(ctx, alliance); err != nil {
		if err2 := s.allianceRepo.UpdateAlliance(ctx, alliance); err2 != nil {
			log.Printf("server: persistAlliance %s: create=%v update=%v", alliance.ID, err, err2)
		}
	}
}

// getUserAllianceID returns the alliance ID a user belongs to, or empty string.
func (s *Server) getUserAllianceID(userID string) string {
	s.allianceMu.RLock()
	defer s.allianceMu.RUnlock()
	for _, a := range s.alliances {
		for _, m := range a.Members {
			if m.UserID == userID {
				return a.ID
			}
		}
	}
	return ""
}

// handleListAlliances returns all alliances.
func (s *Server) handleListAlliances(w http.ResponseWriter, r *http.Request) {
	s.allianceMu.RLock()
	result := make([]*models.Alliance, 0, len(s.alliances))
	for _, a := range s.alliances {
		result = append(result, a)
	}
	s.allianceMu.RUnlock()

	// Sort by creation time (newest first).
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})

	writeJSON(w, http.StatusOK, result)
}

// handleCreateAlliance creates a new alliance. Costs 500 cummies.
func (s *Server) handleCreateAlliance(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req struct {
		Name string `json:"name"`
		Tag  string `json:"tag"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Name == "" || req.Tag == "" {
		writeError(w, http.StatusBadRequest, "name and tag are required")
		return
	}
	if len(req.Tag) < 2 || len(req.Tag) > 5 {
		writeError(w, http.StatusBadRequest, "tag must be 2-5 characters")
		return
	}

	// Check if user is already in an alliance.
	if alID := s.getUserAllianceID(claims.UserID); alID != "" {
		writeError(w, http.StatusBadRequest, "you are already in an alliance — leave first")
		return
	}

	// Deduct 500 cummies.
	stable := s.getStableForUser(claims.UserID)
	if stable == nil {
		writeError(w, http.StatusBadRequest, "you need a stable first")
		return
	}
	if stable.Cummies < 500 {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("creating an alliance costs 500 cummies (you have %d)", stable.Cummies))
		return
	}
	stable.Cummies -= 500
	s.persistStable(r.Context(), stable)

	// Pick a random lore motto.
	motto := models.AllianceLoreMottos[rand.IntN(len(models.AllianceLoreMottos))]

	alliance := &models.Alliance{
		ID:        uuid.New().String(),
		Name:      req.Name,
		Tag:       strings.ToUpper(req.Tag),
		LeaderID:  claims.UserID,
		Motto:     motto,
		Treasury:  0,
		CreatedAt: time.Now(),
		Members: []models.AllianceMember{
			{
				AllianceID: "", // filled below
				UserID:     claims.UserID,
				Username:   claims.Username,
				StableID:   stable.ID,
				Role:       models.AllianceRoleLeader,
				JoinedAt:   time.Now(),
			},
		},
	}
	alliance.Members[0].AllianceID = alliance.ID

	// Store in memory.
	s.allianceMu.Lock()
	s.alliances[alliance.ID] = alliance
	s.allianceMu.Unlock()

	// Persist to DB.
	ctx := r.Context()
	s.persistAlliance(ctx, alliance)
	if s.allianceRepo != nil {
		_ = s.allianceRepo.AddMember(ctx, &alliance.Members[0])
	}

	// Broadcast creation.
	s.hub.BroadcastJSON(map[string]interface{}{
		"type": "chat_system",
		"text": fmt.Sprintf("⚔️ NEW ALLIANCE: [%s] %s founded by %s! \"%s\"",
			alliance.Tag, alliance.Name, claims.Username, motto),
		"ts": time.Now().Unix(),
	})

	log.Printf("server: alliance %q [%s] created by %s", alliance.Name, alliance.Tag, claims.Username)
	writeJSON(w, http.StatusCreated, alliance)
}

// handleGetAlliance returns a single alliance with its members.
func (s *Server) handleGetAlliance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.allianceMu.RLock()
	alliance, ok := s.alliances[id]
	s.allianceMu.RUnlock()
	if !ok {
		writeError(w, http.StatusNotFound, "alliance not found")
		return
	}
	writeJSON(w, http.StatusOK, alliance)
}

// handleJoinAlliance adds the requesting user to an alliance.
func (s *Server) handleJoinAlliance(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	id := r.PathValue("id")

	// Check if already in an alliance.
	if alID := s.getUserAllianceID(claims.UserID); alID != "" {
		writeError(w, http.StatusBadRequest, "you are already in an alliance — leave first")
		return
	}

	stable := s.getStableForUser(claims.UserID)
	if stable == nil {
		writeError(w, http.StatusBadRequest, "you need a stable first")
		return
	}

	s.allianceMu.Lock()
	alliance, exists := s.alliances[id]
	if !exists {
		s.allianceMu.Unlock()
		writeError(w, http.StatusNotFound, "alliance not found")
		return
	}

	member := models.AllianceMember{
		AllianceID: alliance.ID,
		UserID:     claims.UserID,
		Username:   claims.Username,
		StableID:   stable.ID,
		Role:       models.AllianceRoleMember,
		JoinedAt:   time.Now(),
	}
	alliance.Members = append(alliance.Members, member)
	s.allianceMu.Unlock()

	// Persist.
	if s.allianceRepo != nil {
		_ = s.allianceRepo.AddMember(r.Context(), &member)
	}

	s.hub.BroadcastJSON(map[string]interface{}{
		"type": "chat_system",
		"text": fmt.Sprintf("⚔️ %s joined [%s] %s!", claims.Username, alliance.Tag, alliance.Name),
		"ts":   time.Now().Unix(),
	})

	writeJSON(w, http.StatusOK, alliance)
}

// handleLeaveAlliance removes the requesting user from their alliance.
func (s *Server) handleLeaveAlliance(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	id := r.PathValue("id")

	s.allianceMu.Lock()
	alliance, exists := s.alliances[id]
	if !exists {
		s.allianceMu.Unlock()
		writeError(w, http.StatusNotFound, "alliance not found")
		return
	}

	// Leader can't leave — must disband instead.
	if alliance.LeaderID == claims.UserID {
		s.allianceMu.Unlock()
		writeError(w, http.StatusBadRequest, "leaders can't leave — disband the alliance instead (DELETE /api/alliances/{id})")
		return
	}

	// Remove member.
	found := false
	newMembers := make([]models.AllianceMember, 0, len(alliance.Members))
	for _, m := range alliance.Members {
		if m.UserID == claims.UserID {
			found = true
			continue
		}
		newMembers = append(newMembers, m)
	}
	if !found {
		s.allianceMu.Unlock()
		writeError(w, http.StatusBadRequest, "you are not in this alliance")
		return
	}
	alliance.Members = newMembers
	s.allianceMu.Unlock()

	// Persist.
	if s.allianceRepo != nil {
		_ = s.allianceRepo.RemoveMember(r.Context(), id, claims.UserID)
	}

	s.hub.BroadcastJSON(map[string]interface{}{
		"type": "chat_system",
		"text": fmt.Sprintf("⚔️ %s left [%s] %s.", claims.Username, alliance.Tag, alliance.Name),
		"ts":   time.Now().Unix(),
	})

	writeJSON(w, http.StatusOK, map[string]string{"message": "you have left the alliance"})
}

// handleKickFromAlliance removes a member (leader/officer only).
func (s *Server) handleKickFromAlliance(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	id := r.PathValue("id")

	var req struct {
		UserID string `json:"userID"`
	}
	if err := readJSON(r, &req); err != nil || req.UserID == "" {
		writeError(w, http.StatusBadRequest, "userID is required")
		return
	}

	s.allianceMu.Lock()
	alliance, exists := s.alliances[id]
	if !exists {
		s.allianceMu.Unlock()
		writeError(w, http.StatusNotFound, "alliance not found")
		return
	}

	// Only leader or officer can kick.
	callerRole := models.AllianceRole("")
	for _, m := range alliance.Members {
		if m.UserID == claims.UserID {
			callerRole = m.Role
			break
		}
	}
	if callerRole != models.AllianceRoleLeader && callerRole != models.AllianceRoleOfficer {
		s.allianceMu.Unlock()
		writeError(w, http.StatusForbidden, "only leaders and officers can kick members")
		return
	}

	// Can't kick yourself or the leader.
	if req.UserID == claims.UserID {
		s.allianceMu.Unlock()
		writeError(w, http.StatusBadRequest, "you can't kick yourself")
		return
	}
	if req.UserID == alliance.LeaderID {
		s.allianceMu.Unlock()
		writeError(w, http.StatusBadRequest, "you can't kick the leader")
		return
	}

	// Remove the member.
	kickedName := ""
	newMembers := make([]models.AllianceMember, 0, len(alliance.Members))
	for _, m := range alliance.Members {
		if m.UserID == req.UserID {
			kickedName = m.Username
			continue
		}
		newMembers = append(newMembers, m)
	}
	if kickedName == "" {
		s.allianceMu.Unlock()
		writeError(w, http.StatusNotFound, "that user is not in this alliance")
		return
	}
	alliance.Members = newMembers
	s.allianceMu.Unlock()

	// Persist.
	if s.allianceRepo != nil {
		_ = s.allianceRepo.RemoveMember(r.Context(), id, req.UserID)
	}

	s.hub.BroadcastJSON(map[string]interface{}{
		"type": "chat_system",
		"text": fmt.Sprintf("⚔️ %s was kicked from [%s] %s by %s!", kickedName, alliance.Tag, alliance.Name, claims.Username),
		"ts":   time.Now().Unix(),
	})

	writeJSON(w, http.StatusOK, map[string]string{"message": fmt.Sprintf("%s has been kicked", kickedName)})
}

// handleDonateToAlliance transfers cummies from the player's stable to the alliance treasury.
func (s *Server) handleDonateToAlliance(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	id := r.PathValue("id")

	var req struct {
		Amount int64 `json:"amount"`
	}
	if err := readJSON(r, &req); err != nil || req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "positive amount is required")
		return
	}

	stable := s.getStableForUser(claims.UserID)
	if stable == nil {
		writeError(w, http.StatusBadRequest, "you need a stable first")
		return
	}
	if stable.Cummies < req.Amount {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("insufficient cummies (have %d, need %d)", stable.Cummies, req.Amount))
		return
	}

	s.allianceMu.Lock()
	alliance, exists := s.alliances[id]
	if !exists {
		s.allianceMu.Unlock()
		writeError(w, http.StatusNotFound, "alliance not found")
		return
	}

	// Verify membership.
	isMember := false
	for _, m := range alliance.Members {
		if m.UserID == claims.UserID {
			isMember = true
			break
		}
	}
	if !isMember {
		s.allianceMu.Unlock()
		writeError(w, http.StatusForbidden, "you are not a member of this alliance")
		return
	}

	// Transfer cummies.
	stable.Cummies -= req.Amount
	alliance.Treasury += req.Amount
	s.allianceMu.Unlock()

	// Persist.
	s.persistStable(r.Context(), stable)
	s.persistAlliance(r.Context(), alliance)

	s.hub.BroadcastJSON(map[string]interface{}{
		"type": "chat_system",
		"text": fmt.Sprintf("💰 %s donated ₵%d to [%s] %s! Treasury: ₵%d",
			claims.Username, req.Amount, alliance.Tag, alliance.Name, alliance.Treasury),
		"ts": time.Now().Unix(),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":  fmt.Sprintf("donated ₵%d to %s", req.Amount, alliance.Name),
		"treasury": alliance.Treasury,
	})
}

// handleDisbandAlliance deletes an alliance, returning treasury evenly to members.
func (s *Server) handleDisbandAlliance(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	id := r.PathValue("id")

	s.allianceMu.Lock()
	alliance, exists := s.alliances[id]
	if !exists {
		s.allianceMu.Unlock()
		writeError(w, http.StatusNotFound, "alliance not found")
		return
	}

	// Only the leader can disband.
	if alliance.LeaderID != claims.UserID {
		s.allianceMu.Unlock()
		writeError(w, http.StatusForbidden, "only the leader can disband the alliance")
		return
	}

	// Distribute treasury evenly to all members.
	if len(alliance.Members) > 0 && alliance.Treasury > 0 {
		share := alliance.Treasury / int64(len(alliance.Members))
		for _, m := range alliance.Members {
			memberStable := s.getStableForUser(m.UserID)
			if memberStable != nil {
				memberStable.Cummies += share
				s.persistStable(r.Context(), memberStable)
			}
		}
	}

	allianceName := alliance.Name
	allianceTag := alliance.Tag
	delete(s.alliances, id)
	s.allianceMu.Unlock()

	// Persist deletion.
	if s.allianceRepo != nil {
		_ = s.allianceRepo.DeleteAlliance(r.Context(), id)
	}

	s.hub.BroadcastJSON(map[string]interface{}{
		"type": "chat_system",
		"text": fmt.Sprintf("💀 [%s] %s has been DISBANDED by %s! The treasury was split among members.",
			allianceTag, allianceName, claims.Username),
		"ts": time.Now().Unix(),
	})

	writeJSON(w, http.StatusOK, map[string]string{"message": fmt.Sprintf("alliance %s has been disbanded", allianceName)})
}

// ===========================================================================
// Alliance Chat Commands
// ===========================================================================

// handleChatAlliance processes /alliance chat commands.
// Sub-commands: create <name> <tag>, join <id>, leave, donate <amount>
func (s *Server) handleChatAlliance(senderUserID, senderUsername string, args map[string]interface{}) {
	subCmd, _ := args["sub"].(string)
	if subCmd == "" {
		subCmd, _ = args["action"].(string)
	}

	switch strings.ToLower(subCmd) {
	case "create":
		name, _ := args["name"].(string)
		tag, _ := args["tag"].(string)
		if name == "" || tag == "" {
			s.hub.BroadcastJSON(map[string]interface{}{
				"type": "chat_system",
				"text": fmt.Sprintf("⚠️ %s — Usage: /alliance create <name> <tag>", senderUsername),
				"ts":   time.Now().Unix(),
			})
			return
		}

		// Check if already in alliance.
		if alID := s.getUserAllianceID(senderUserID); alID != "" {
			s.hub.BroadcastJSON(map[string]interface{}{
				"type": "chat_system",
				"text": fmt.Sprintf("⚠️ %s — You're already in an alliance! Leave first.", senderUsername),
				"ts":   time.Now().Unix(),
			})
			return
		}

		stable := s.getStableForUser(senderUserID)
		if stable == nil || stable.Cummies < 500 {
			s.hub.BroadcastJSON(map[string]interface{}{
				"type": "chat_system",
				"text": fmt.Sprintf("⚠️ %s — Creating an alliance costs 500 cummies.", senderUsername),
				"ts":   time.Now().Unix(),
			})
			return
		}

		stable.Cummies -= 500
		s.persistStable(context.Background(), stable)

		motto := models.AllianceLoreMottos[rand.IntN(len(models.AllianceLoreMottos))]
		alliance := &models.Alliance{
			ID:        uuid.New().String(),
			Name:      name,
			Tag:       strings.ToUpper(tag),
			LeaderID:  senderUserID,
			Motto:     motto,
			Treasury:  0,
			CreatedAt: time.Now(),
			Members: []models.AllianceMember{
				{
					AllianceID: "", // filled below
					UserID:     senderUserID,
					Username:   senderUsername,
					StableID:   stable.ID,
					Role:       models.AllianceRoleLeader,
					JoinedAt:   time.Now(),
				},
			},
		}
		alliance.Members[0].AllianceID = alliance.ID

		s.allianceMu.Lock()
		s.alliances[alliance.ID] = alliance
		s.allianceMu.Unlock()

		ctx := context.Background()
		s.persistAlliance(ctx, alliance)
		if s.allianceRepo != nil {
			_ = s.allianceRepo.AddMember(ctx, &alliance.Members[0])
		}

		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": fmt.Sprintf("⚔️ NEW ALLIANCE: [%s] %s founded by %s! \"%s\"",
				alliance.Tag, alliance.Name, senderUsername, motto),
			"ts": time.Now().Unix(),
		})

	case "join":
		allianceID, _ := args["id"].(string)
		if allianceID == "" {
			s.hub.BroadcastJSON(map[string]interface{}{
				"type": "chat_system",
				"text": fmt.Sprintf("⚠️ %s — Usage: /alliance join <allianceID>", senderUsername),
				"ts":   time.Now().Unix(),
			})
			return
		}

		if alID := s.getUserAllianceID(senderUserID); alID != "" {
			s.hub.BroadcastJSON(map[string]interface{}{
				"type": "chat_system",
				"text": fmt.Sprintf("⚠️ %s — You're already in an alliance! Leave first.", senderUsername),
				"ts":   time.Now().Unix(),
			})
			return
		}

		stable := s.getStableForUser(senderUserID)
		if stable == nil {
			return
		}

		s.allianceMu.Lock()
		alliance, exists := s.alliances[allianceID]
		if !exists {
			s.allianceMu.Unlock()
			s.hub.BroadcastJSON(map[string]interface{}{
				"type": "chat_system",
				"text": fmt.Sprintf("⚠️ %s — Alliance not found.", senderUsername),
				"ts":   time.Now().Unix(),
			})
			return
		}

		member := models.AllianceMember{
			AllianceID: alliance.ID,
			UserID:     senderUserID,
			Username:   senderUsername,
			StableID:   stable.ID,
			Role:       models.AllianceRoleMember,
			JoinedAt:   time.Now(),
		}
		alliance.Members = append(alliance.Members, member)
		s.allianceMu.Unlock()

		if s.allianceRepo != nil {
			_ = s.allianceRepo.AddMember(context.Background(), &member)
		}

		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": fmt.Sprintf("⚔️ %s joined [%s] %s!", senderUsername, alliance.Tag, alliance.Name),
			"ts":   time.Now().Unix(),
		})

	case "leave":
		alID := s.getUserAllianceID(senderUserID)
		if alID == "" {
			s.hub.BroadcastJSON(map[string]interface{}{
				"type": "chat_system",
				"text": fmt.Sprintf("⚠️ %s — You're not in any alliance.", senderUsername),
				"ts":   time.Now().Unix(),
			})
			return
		}

		s.allianceMu.Lock()
		alliance := s.alliances[alID]
		if alliance == nil {
			s.allianceMu.Unlock()
			return
		}
		if alliance.LeaderID == senderUserID {
			s.allianceMu.Unlock()
			s.hub.BroadcastJSON(map[string]interface{}{
				"type": "chat_system",
				"text": fmt.Sprintf("⚠️ %s — Leaders can't leave. Disband the alliance instead.", senderUsername),
				"ts":   time.Now().Unix(),
			})
			return
		}

		newMembers := make([]models.AllianceMember, 0, len(alliance.Members))
		for _, m := range alliance.Members {
			if m.UserID != senderUserID {
				newMembers = append(newMembers, m)
			}
		}
		alliance.Members = newMembers
		s.allianceMu.Unlock()

		if s.allianceRepo != nil {
			_ = s.allianceRepo.RemoveMember(context.Background(), alID, senderUserID)
		}

		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": fmt.Sprintf("⚔️ %s left [%s] %s.", senderUsername, alliance.Tag, alliance.Name),
			"ts":   time.Now().Unix(),
		})

	case "donate":
		amountF, _ := args["amount"].(float64)
		amount := int64(amountF)
		if amount <= 0 {
			s.hub.BroadcastJSON(map[string]interface{}{
				"type": "chat_system",
				"text": fmt.Sprintf("⚠️ %s — Usage: /alliance donate <amount>", senderUsername),
				"ts":   time.Now().Unix(),
			})
			return
		}

		alID := s.getUserAllianceID(senderUserID)
		if alID == "" {
			s.hub.BroadcastJSON(map[string]interface{}{
				"type": "chat_system",
				"text": fmt.Sprintf("⚠️ %s — You're not in any alliance.", senderUsername),
				"ts":   time.Now().Unix(),
			})
			return
		}

		stable := s.getStableForUser(senderUserID)
		if stable == nil || stable.Cummies < amount {
			s.hub.BroadcastJSON(map[string]interface{}{
				"type": "chat_system",
				"text": fmt.Sprintf("⚠️ %s — Insufficient cummies.", senderUsername),
				"ts":   time.Now().Unix(),
			})
			return
		}

		s.allianceMu.Lock()
		alliance := s.alliances[alID]
		if alliance == nil {
			s.allianceMu.Unlock()
			return
		}
		stable.Cummies -= amount
		alliance.Treasury += amount
		s.allianceMu.Unlock()

		s.persistStable(context.Background(), stable)
		s.persistAlliance(context.Background(), alliance)

		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": fmt.Sprintf("💰 %s donated ₵%d to [%s] %s! Treasury: ₵%d",
				senderUsername, amount, alliance.Tag, alliance.Name, alliance.Treasury),
			"ts": time.Now().Unix(),
		})

	default:
		s.hub.BroadcastJSON(map[string]interface{}{
			"type": "chat_system",
			"text": fmt.Sprintf("⚠️ %s — Usage: /alliance <create|join|leave|donate> [args]", senderUsername),
			"ts":   time.Now().Unix(),
		})
	}
}

// ===========================================================================
// Horse Aging, Injury, and Retirement — Handlers & Helpers
// ===========================================================================

// getAgeBracket returns the age bracket name for a given age.
// Foal (0-2), Prime (3-5), Veteran (6-8), Elder (9-11), Ancient (12+)
func getAgeBracket(age int) string {
	switch {
	case age <= 2:
		return "Foal"
	case age <= 5:
		return "Prime"
	case age <= 8:
		return "Veteran"
	case age <= 11:
		return "Elder"
	default:
		return "Ancient"
	}
}

// injuryLoreDescriptions maps injury types to their lore descriptions.
var injuryLoreDescriptions = map[models.InjuryType]string{
	models.InjuryMuscleStrain:     "A pulled muscle from trying to look cool at the finish line.",
	models.InjuryTendonTear:       "Something went *snap* and it wasn't the crowd's enthusiasm.",
	models.InjuryHoofCrack:        "The hoof has a crack so dramatic it needs its own backstory.",
	models.InjuryYogurtPoisoning:  "Someone left expired yogurt in the feed room. Again.",
	models.InjuryExistentialDread: "The horse has realized it's in a simulation and refuses to run.",
	models.InjuryHauntedByE008:    "E-008's spectral presence follows this horse, whispering forbidden race strategies.",
}

// rollInjury generates a random injury for a horse after a race.
func rollInjury(horse *models.Horse) *models.Injury {
	// Pick random injury type.
	types := []models.InjuryType{
		models.InjuryMuscleStrain,
		models.InjuryTendonTear,
		models.InjuryHoofCrack,
		models.InjuryYogurtPoisoning,
		models.InjuryExistentialDread,
		models.InjuryHauntedByE008,
	}
	injType := types[rand.IntN(len(types))]

	// Pick severity: weighted — minor 50%, moderate 30%, severe 15%, career-ending 5%.
	roll := rand.Float64()
	var severity models.InjurySeverity
	switch {
	case roll < 0.50:
		severity = models.SeverityMinor
	case roll < 0.80:
		severity = models.SeverityModerate
	case roll < 0.95:
		severity = models.SeveritySevere
	default:
		severity = models.SeverityCareerEnding
	}

	desc := injuryLoreDescriptions[injType]

	return &models.Injury{
		Type:        injType,
		Severity:    severity,
		Description: desc,
		RacesLeft:   models.InjuryRaceCooldown(severity),
		OccurredAt:  time.Now(),
	}
}

// handleHealHorse heals a horse's injury for a cummies cost.
func (s *Server) handleHealHorse(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	horseID := r.PathValue("id")
	if !s.userOwnsHorse(claims.UserID, horseID) {
		writeError(w, http.StatusForbidden, "you can only heal your own horses")
		return
	}

	horse, err := s.stables.GetHorse(horseID)
	if err != nil || horse == nil {
		writeError(w, http.StatusNotFound, "horse not found")
		return
	}

	if horse.Injury == nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("%s is healthy — no injury to heal", horse.Name))
		return
	}

	if horse.Injury.Severity == models.SeverityCareerEnding {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("%s has a career-ending injury that cannot be healed. The dream is over.", horse.Name))
		return
	}

	cost := models.InjuryHealCost(horse.Injury.Severity)
	stable := s.getStableForUser(claims.UserID)
	if stable == nil {
		writeError(w, http.StatusBadRequest, "stable not found")
		return
	}
	if stable.Cummies < cost {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("healing costs ₵%d (you have ₵%d)", cost, stable.Cummies))
		return
	}

	// Deduct cost and heal.
	oldInjury := *horse.Injury
	stable.Cummies -= cost
	horse.Injury = nil

	s.syncHorseToStable(horse)
	s.persistHorse(r.Context(), horse)
	s.persistStable(r.Context(), stable)

	s.hub.BroadcastJSON(map[string]interface{}{
		"type": "chat_system",
		"text": fmt.Sprintf("💊 %s healed %s from %s (%s) for ₵%d!",
			claims.Username, horse.Name, oldInjury.Type, oldInjury.Severity, cost),
		"ts": time.Now().Unix(),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": fmt.Sprintf("%s has been healed!", horse.Name),
		"cost":    cost,
		"horse":   horse,
	})
}

// handleGetHorseAgeInfo returns age bracket, injury status, and retirement eligibility.
func (s *Server) handleGetHorseAgeInfo(w http.ResponseWriter, r *http.Request) {
	horseID := r.PathValue("id")
	horse, err := s.stables.GetHorse(horseID)
	if err != nil || horse == nil {
		writeError(w, http.StatusNotFound, "horse not found")
		return
	}

	bracket := getAgeBracket(horse.Age)

	// Age bracket effects.
	effects := ""
	switch bracket {
	case "Foal":
		effects = "Learning the ropes. -10% speed, +20% training XP gain."
	case "Prime":
		effects = "Peak performance. No modifiers. Let's ride."
	case "Veteran":
		effects = "Experienced but slowing. -5% speed, +10% tactical awareness."
	case "Elder":
		effects = "Getting creaky. -15% speed, +5% injury chance, wisdom bonus."
	case "Ancient":
		effects = "Living legend. -25% speed, +5% injury chance, double prestige XP."
	}

	resp := map[string]interface{}{
		"horseID":            horse.ID,
		"horseName":          horse.Name,
		"age":                horse.Age,
		"ageBracket":         bracket,
		"ageBracketEffects":  effects,
		"retired":            horse.Retired,
		"retiredChampion":    horse.RetiredChampion,
		"retirementEligible": horse.Age >= 9 || horse.Wins >= 10,
		"injury":             horse.Injury,
	}

	if horse.Injury != nil {
		resp["healCost"] = models.InjuryHealCost(horse.Injury.Severity)
	}

	writeJSON(w, http.StatusOK, resp)
}

// ===========================================================================
// Random Events System — 20 mid-race events
// ===========================================================================

// allRandomEvents defines the 20 random events that can occur during races.
var allRandomEvents = []models.RandomEvent{
	// === Speed Buffs ===
	{ID: "evt_tailwind", Name: "Tailwind Surge", Description: "A sudden gust of wind propels the lead horse forward!", Effect: "speed_buff", Magnitude: 1.15, Target: "leader"},
	{ID: "evt_energy_drink", Name: "Mystery Energy Drink", Description: "Someone left an unmarked bottle on the track. The nearest horse drank it. IT'S WORKING.", Effect: "speed_buff", Magnitude: 1.20, Target: "random_horse"},
	{ID: "evt_crowd_cheer", Name: "Thunderous Applause", Description: "The crowd goes WILD! The last-place horse is inspired to give it everything!", Effect: "speed_buff", Magnitude: 1.25, Target: "last_place"},

	// === Speed Debuffs ===
	{ID: "evt_mud_puddle", Name: "Surprise Mud Puddle", Description: "A massive puddle appeared from nowhere! The leader splashes through and loses momentum.", Effect: "speed_debuff", Magnitude: 0.85, Target: "leader"},
	{ID: "evt_bee_swarm", Name: "Angry Bee Swarm", Description: "A swarm of bees descends on the pack! One unlucky horse takes the brunt.", Effect: "speed_debuff", Magnitude: 0.80, Target: "random_horse"},
	{ID: "evt_existential", Name: "Mid-Race Existential Crisis", Description: "A horse suddenly questions the meaning of racing. Is winning even real?", Effect: "speed_debuff", Magnitude: 0.75, Target: "random_horse"},

	// === Chaos Events ===
	{ID: "evt_track_reversal", Name: "Track Reversal!", Description: "WAIT — THE TRACK IS GOING THE OTHER WAY NOW? Everyone loses their bearings!", Effect: "chaos_shuffle", Magnitude: 0.0, Target: "all_horses"},
	{ID: "evt_yogurt_rain", Name: "Yogurt Rain", Description: "It's raining yogurt. Nobody asked for this. Everyone is confused.", Effect: "chaos_slow_all", Magnitude: 0.90, Target: "all_horses"},
	{ID: "evt_e008_apparition", Name: "E-008 Apparition", Description: "The ghost of E-008 manifests on the track! Horses scatter in terror!", Effect: "chaos_shuffle", Magnitude: 0.0, Target: "all_horses"},
	{ID: "evt_announcer_curse", Name: "Announcer's Curse", Description: "The announcer said 'What could go wrong?' and the universe answered.", Effect: "chaos_slow_all", Magnitude: 0.85, Target: "all_horses"},

	// === Purse Modifiers ===
	{ID: "evt_sponsor_bonus", Name: "Sponsor Bonus", Description: "CummiesCorp™ just doubled the purse! MONEY MONEY MONEY!", Effect: "purse_double", Magnitude: 2.0, Target: "all_horses"},
	{ID: "evt_tax_collector", Name: "Geoffrussey Tax Audit", Description: "The Geoffrussey Tax Authority shows up mid-race. 30% purse garnished.", Effect: "purse_reduce", Magnitude: 0.70, Target: "all_horses"},
	{ID: "evt_treasure_chest", Name: "Buried Treasure", Description: "A horse's hoof strikes a buried treasure chest! +1000 bonus cummies!", Effect: "purse_bonus", Magnitude: 1000, Target: "random_horse"},

	// === Cosmetic / Lore Events ===
	{ID: "evt_rainbow", Name: "Double Rainbow", Description: "A magnificent double rainbow appears over the track. It means nothing but it's beautiful.", Effect: "cosmetic", Magnitude: 0, Target: "all_horses"},
	{ID: "evt_commentator", Name: "Guest Commentator", Description: "A very drunk commentator has taken over the mic and is narrating everything wrong.", Effect: "cosmetic", Magnitude: 0, Target: "all_horses"},
	{ID: "evt_streaker", Name: "Track Streaker", Description: "A naked fan has run onto the track! Security is in pursuit! The horses are unfazed.", Effect: "cosmetic", Magnitude: 0, Target: "all_horses"},
	{ID: "evt_bird_poop", Name: "Strategic Bird Poop", Description: "A bird has pooped on the leader. Is it good luck? Science says no.", Effect: "cosmetic", Magnitude: 0, Target: "leader"},

	// === Special ===
	{ID: "evt_clone_glitch", Name: "Clone Glitch", Description: "The simulation glitches! For a brief moment, there appear to be TWO of the same horse!", Effect: "cosmetic", Magnitude: 0, Target: "random_horse"},
	{ID: "evt_time_warp", Name: "Time Warp", Description: "Time seems to slow down... then speed up! The last-place horse phases forward!", Effect: "speed_buff", Magnitude: 1.30, Target: "last_place"},
	{ID: "evt_motivational", Name: "Motivational Speech", Description: "A tiny mouse on the railing screams 'BELIEVE IN YOURSELF!' at a random horse. It works.", Effect: "speed_buff", Magnitude: 1.10, Target: "random_horse"},
}

// rollRandomEvent has a 15% chance to trigger a random event during a race.
// Returns nil if no event triggers.
func rollRandomEvent(horses []*models.Horse, race *models.Race, weather models.Weather) *models.RandomEvent {
	// 15% base chance. +10% on Hauntedussy track. +5% in Haunted weather.
	chance := 0.15
	if race.TrackType == models.TrackHauntedussy {
		chance += 0.10
	}
	if weather == models.WeatherHaunted {
		chance += 0.05
	}

	if rand.Float64() >= chance {
		return nil
	}

	// Pick a random event.
	event := allRandomEvents[rand.IntN(len(allRandomEvents))]
	return &event
}

// applyRandomEvent applies an event's effects to the race and returns narrative lines.
func applyRandomEvent(event *models.RandomEvent, horses []*models.Horse, race *models.Race, purse *int64) []string {
	narrative := []string{
		fmt.Sprintf("⚡ RANDOM EVENT: %s", event.Name),
		fmt.Sprintf("   %s", event.Description),
	}

	switch event.Effect {
	case "speed_buff":
		target := pickEventTarget(event.Target, horses, race)
		if target != nil {
			// Speed buff is cosmetic in post-simulation — we add it to narrative only.
			// The race is already simulated, but the lore effect is what matters.
			narrative = append(narrative, fmt.Sprintf("   🏃 %s gets a speed boost! (x%.0f%%)", target.Name, event.Magnitude*100))
		}

	case "speed_debuff":
		target := pickEventTarget(event.Target, horses, race)
		if target != nil {
			narrative = append(narrative, fmt.Sprintf("   🐌 %s is slowed down! (x%.0f%%)", target.Name, event.Magnitude*100))
		}

	case "chaos_shuffle":
		narrative = append(narrative, "   🌀 The entire field is thrown into chaos!")

	case "chaos_slow_all":
		narrative = append(narrative, fmt.Sprintf("   🌀 All horses affected! Speed reduced to %.0f%%", event.Magnitude*100))

	case "purse_double":
		*purse *= 2
		narrative = append(narrative, fmt.Sprintf("   💰 PURSE DOUBLED to ₵%d!", *purse))

	case "purse_reduce":
		*purse = int64(float64(*purse) * event.Magnitude)
		narrative = append(narrative, fmt.Sprintf("   📉 Purse reduced to ₵%d!", *purse))

	case "purse_bonus":
		*purse += int64(event.Magnitude)
		narrative = append(narrative, fmt.Sprintf("   💎 Bonus ₵%.0f added to the purse! Now ₵%d!", event.Magnitude, *purse))

	case "cosmetic":
		// Pure lore — no mechanical effect.
		narrative = append(narrative, "   ✨ (No mechanical effect — just vibes)")
	}

	return narrative
}

// pickEventTarget selects a horse based on the event's target type.
func pickEventTarget(target string, horses []*models.Horse, race *models.Race) *models.Horse {
	if len(horses) == 0 {
		return nil
	}

	switch target {
	case "random_horse":
		return horses[rand.IntN(len(horses))]

	case "leader":
		// Find the horse in 1st place.
		for _, entry := range race.Entries {
			if entry.FinishPlace == 1 {
				for _, h := range horses {
					if h.ID == entry.HorseID {
						return h
					}
				}
			}
		}
		return horses[0]

	case "last_place":
		// Find the horse in last place.
		lastPlace := 0
		var lastHorseID string
		for _, entry := range race.Entries {
			if entry.FinishPlace > lastPlace {
				lastPlace = entry.FinishPlace
				lastHorseID = entry.HorseID
			}
		}
		for _, h := range horses {
			if h.ID == lastHorseID {
				return h
			}
		}
		return horses[len(horses)-1]

	default:
		return nil
	}
}
