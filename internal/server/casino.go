package server

import (
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mojomast/stallionussy/internal/authussy"
	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/trainussy"
)

const (
	casinoExchangeRate          = int64(25)
	casinoProtectedCummiesFloor = int64(500)
	casinoDailyChipGrant        = int64(40)
	deathReturnChance           = 0.015
	glueReturnChance            = 0.06
)

// ---------------------------------------------------------------------------
// 5-Reel Video Slot — 12 Horse-Themed Symbols, 9 Paylines, ~94% RTP
// ---------------------------------------------------------------------------

// slotSymbols enumerates every symbol on the reels.
const (
	symWildMare       = "WILD_MARE"
	symGoldenStallion = "GOLDEN_STALLION"
	symChampionTrophy = "CHAMPION_TROPHY"
	symRacingSaddle   = "RACING_SADDLE"
	symLuckyHorseshoe = "LUCKY_HORSESHOE"
	symSugarCube      = "SUGAR_CUBE"
	symCarrot         = "CARROT"
	symOats           = "OATS"
	symBell           = "BELL"
	symCherry         = "CHERRY"
	symYogurt         = "YOGURT" // scatter — triggers bonus
	symSkull          = "SKULL"
)

// slotWeightedPool is the weighted symbol pool for each reel. Symbols appear
// a number of times equal to their weight, giving us a 60-stop virtual reel.
// Reel composition: WILD_MARE ×2, GOLDEN_STALLION ×2, CHAMPION_TROPHY ×3,
// RACING_SADDLE ×4, LUCKY_HORSESHOE ×5, SUGAR_CUBE ×6, CARROT ×7, OATS ×8,
// BELL ×8, CHERRY ×9, YOGURT ×3, SKULL ×3 = 60 stops per reel.
var slotWeightedPool []string

func init() {
	weights := []struct {
		sym    string
		weight int
	}{
		{symWildMare, 2},
		{symGoldenStallion, 2},
		{symChampionTrophy, 3},
		{symRacingSaddle, 4},
		{symLuckyHorseshoe, 5},
		{symSugarCube, 6},
		{symCarrot, 7},
		{symOats, 8},
		{symBell, 8},
		{symCherry, 9},
		{symYogurt, 3},
		{symSkull, 3},
	}
	for _, w := range weights {
		for i := 0; i < w.weight; i++ {
			slotWeightedPool = append(slotWeightedPool, w.sym)
		}
	}
}

// slotPaylines defines the 9 payline patterns across a 3-row × 5-reel grid.
// Each entry is the row index (0=top, 1=middle, 2=bottom) for each of the 5 reels.
var slotPaylines = [9][5]int{
	{1, 1, 1, 1, 1}, // 1: Middle row
	{0, 0, 0, 0, 0}, // 2: Top row
	{2, 2, 2, 2, 2}, // 3: Bottom row
	{0, 1, 2, 1, 0}, // 4: V-shape
	{2, 1, 0, 1, 2}, // 5: Inverted V
	{1, 0, 1, 0, 1}, // 6: Zigzag up
	{1, 2, 1, 2, 1}, // 7: Zigzag down
	{0, 0, 1, 2, 2}, // 8: Diagonal down
	{2, 2, 1, 0, 0}, // 9: Diagonal up
}

// slotPayouts maps each paying symbol to multipliers for 3, 4, and 5 of a kind.
var slotPayouts = map[string][3]int64{
	symGoldenStallion: {10, 25, 100},
	symChampionTrophy: {8, 20, 75},
	symRacingSaddle:   {6, 15, 50},
	symLuckyHorseshoe: {5, 12, 40},
	symSugarCube:      {4, 10, 30},
	symCarrot:         {3, 8, 20},
	symOats:           {2, 5, 15},
	symBell:           {2, 5, 15},
	symCherry:         {1, 3, 10},
	symSkull:          {3, 8, 25},
}

// slotJackpotSeed is the initial progressive jackpot pool.
const slotJackpotSeed int64 = 500

// slotJackpotMinPayout is the minimum jackpot payout.
const slotJackpotMinPayout int64 = 1000

var omenTexts = map[string][]string{
	models.DepartureCauseFight: {
		"A hoofprint appeared on the stable wall overnight, facing inward.",
		"The memorial feed flickered and replayed the final round in reverse.",
		"Arena bookies swear they heard the dead horse demand a rematch.",
	},
	models.DepartureCauseGlue: {
		"The glue ledger bled through three pages and spelled out a name.",
		"A sealed bottle in the factory warehouse began pawing at the shelf.",
		"Dr. Mittens filed a report: adhesive sample shows aggressive nostalgia.",
	},
}

func (s *Server) handleGetCasinoOverview(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stable := s.getStableForUser(claims.UserID)
	if stable == nil {
		writeError(w, http.StatusBadRequest, "you need a stable first")
		return
	}
	claimedGrant := s.maybeGrantDailyCasinoChips(r.Context(), claims.UserID, stable)
	tables := s.listPokerTablesForUser(claims.UserID, 12)
	spins := []*models.SlotSpin{}
	if s.casinoRepo != nil {
		if recent, err := s.casinoRepo.ListSlotSpinsByUser(r.Context(), claims.UserID, 12); err == nil {
			spins = recent
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"stableID":              stable.ID,
		"cummies":               stable.Cummies,
		"casinoChips":           stable.CasinoChips,
		"exchangeRate":          casinoExchangeRate,
		"protectedCummiesFloor": casinoProtectedCummiesFloor,
		"dailyChipGrant":        casinoDailyChipGrant,
		"dailyChipGrantClaimed": claimedGrant,
		"pokerTables":           tables,
		"recentSpins":           spins,
	})
}

func (s *Server) handleExchangeCasinoChips(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		Direction string `json:"direction"`
		Amount    int64  `json:"amount"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "amount must be positive")
		return
	}
	stable := s.getStableForUser(claims.UserID)
	if stable == nil {
		writeError(w, http.StatusBadRequest, "you need a stable first")
		return
	}

	// Lock the per-stable mutex for the read-check-write on balance.
	mu := s.stableMu(stable.ID)
	mu.Lock()
	defer mu.Unlock()

	s.maybeGrantDailyCasinoChips(r.Context(), claims.UserID, stable)

	switch strings.ToLower(strings.TrimSpace(req.Direction)) {
	case "buy", "to_chips":
		cost := req.Amount * casinoExchangeRate
		if stable.Cummies-cost < casinoProtectedCummiesFloor {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("casino exchange cannot reduce your stable below the protected floor of %d cummies", casinoProtectedCummiesFloor))
			return
		}
		stable.Cummies -= cost
		stable.CasinoChips += req.Amount
	case "cashout", "to_cummies":
		if stable.CasinoChips < req.Amount {
			writeError(w, http.StatusBadRequest, "insufficient casino chips")
			return
		}
		stable.CasinoChips -= req.Amount
		stable.Cummies += req.Amount * 10
	default:
		writeError(w, http.StatusBadRequest, "direction must be buy or cashout")
		return
	}
	s.persistStable(r.Context(), stable)
	writeJSON(w, http.StatusOK, map[string]any{
		"stableID":     stable.ID,
		"cummies":      stable.Cummies,
		"casinoChips":  stable.CasinoChips,
		"exchangeRate": casinoExchangeRate,
	})
}

func (s *Server) handleListPokerTables(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	writeJSON(w, http.StatusOK, s.listPokerTablesForUser(claims.UserID, 30))
}

func (s *Server) listPokerTablesForUser(userID string, limit int) []*models.PokerTable {
	if s.casinoRepo != nil {
		tables, err := s.casinoRepo.ListPokerTables(context.Background(), limit)
		if err == nil {
			return redactPokerTablesForUser(tables, userID)
		}
	}
	s.pokerMu.RLock()
	defer s.pokerMu.RUnlock()
	tables := make([]*models.PokerTable, 0, len(s.pokerTables))
	for _, table := range s.pokerTables {
		clone := *table
		tables = append(tables, &clone)
	}
	sort.Slice(tables, func(i, j int) bool { return tables[i].UpdatedAt.After(tables[j].UpdatedAt) })
	if len(tables) > limit && limit > 0 {
		tables = tables[:limit]
	}
	return redactPokerTablesForUser(tables, userID)
}

func (s *Server) handleCreatePokerTable(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		Name       string `json:"name"`
		BuyIn      int64  `json:"buyIn"`
		MaxPlayers int    `json:"maxPlayers"`
		Currency   string `json:"currency"`
		GameType   string `json:"gameType"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	stable := s.getStableForUser(claims.UserID)
	if stable == nil {
		writeError(w, http.StatusBadRequest, "you need a stable first")
		return
	}
	if req.BuyIn <= 0 {
		req.BuyIn = 50
	}
	if req.Currency == "" {
		req.Currency = "casino_chips"
	}
	// Default to holdem; support "draw" for backward compat.
	gameType := strings.ToLower(strings.TrimSpace(req.GameType))
	if gameType != models.PokerGameDraw {
		gameType = models.PokerGameHoldem
	}
	// Validate max players by game type.
	switch gameType {
	case models.PokerGameHoldem:
		if req.MaxPlayers < 2 || req.MaxPlayers > 6 {
			req.MaxPlayers = 6
		}
	default: // draw
		if req.MaxPlayers < 2 || req.MaxPlayers > 4 {
			req.MaxPlayers = 4
		}
	}
	createPokerMu := s.stableMu(stable.ID)
	createPokerMu.Lock()
	if err := s.withdrawCasinoStake(stable, req.Currency, req.BuyIn); err != nil {
		createPokerMu.Unlock()
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.persistStable(r.Context(), stable)
	createPokerMu.Unlock()
	table := &models.PokerTable{
		ID:            uuid.New().String(),
		Name:          strings.TrimSpace(req.Name),
		CreatedBy:     claims.UserID,
		StakeCurrency: req.Currency,
		BuyIn:         req.BuyIn,
		MaxPlayers:    req.MaxPlayers,
		Status:        models.PokerTableOpen,
		Pot:           0,
		DeckSeed:      rand.Uint64(),
		GameType:      gameType,
		ActionSeat:    -1,
		Log:           []string{"Table opened. Waiting for more degenerates."},
		Seats: []models.PokerSeat{{
			UserID:       claims.UserID,
			Username:     claims.Username,
			StableID:     stable.ID,
			BuyIn:        req.BuyIn,
			Currency:     req.Currency,
			ChipStack:    req.BuyIn,
			JoinedAt:     time.Now(),
			LastActionAt: time.Now(),
		}},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	// For draw tables, the pot accumulates buy-ins directly (legacy behavior).
	if gameType == models.PokerGameDraw {
		table.Pot = req.BuyIn
	}
	// For holdem tables, set blinds based on buy-in.
	if gameType == models.PokerGameHoldem {
		table.SmallBlind = req.BuyIn / 20
		if table.SmallBlind < 1 {
			table.SmallBlind = 1
		}
		table.BigBlind = req.BuyIn / 10
		if table.BigBlind < 2 {
			table.BigBlind = 2
		}
	}
	if table.Name == "" {
		if gameType == models.PokerGameHoldem {
			table.Name = fmt.Sprintf("%s's hold'em table", claims.Username)
		} else {
			table.Name = fmt.Sprintf("%s's draw table", claims.Username)
		}
	}
	s.savePokerTable(r.Context(), table)
	writeJSON(w, http.StatusCreated, redactPokerTableForUser(table, claims.UserID))
}

func (s *Server) handleJoinPokerTable(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	table, err := s.getPokerTable(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if table.Status != models.PokerTableOpen {
		writeError(w, http.StatusBadRequest, "table is no longer open")
		return
	}
	for _, seat := range table.Seats {
		if seat.UserID == claims.UserID {
			writeError(w, http.StatusBadRequest, "you are already seated at this table")
			return
		}
	}
	if len(table.Seats) >= table.MaxPlayers {
		writeError(w, http.StatusBadRequest, "table is full")
		return
	}
	stable := s.getStableForUser(claims.UserID)
	if stable == nil {
		writeError(w, http.StatusBadRequest, "you need a stable first")
		return
	}
	joinPokerMu := s.stableMu(stable.ID)
	joinPokerMu.Lock()
	if err := s.withdrawCasinoStake(stable, table.StakeCurrency, table.BuyIn); err != nil {
		joinPokerMu.Unlock()
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.persistStable(r.Context(), stable)
	joinPokerMu.Unlock()
	table.Seats = append(table.Seats, models.PokerSeat{
		UserID:       claims.UserID,
		Username:     claims.Username,
		StableID:     stable.ID,
		BuyIn:        table.BuyIn,
		Currency:     table.StakeCurrency,
		ChipStack:    table.BuyIn,
		JoinedAt:     time.Now(),
		LastActionAt: time.Now(),
	})
	// For draw tables, pot accumulates buy-ins directly.
	if table.GameType != models.PokerGameHoldem {
		table.Pot += table.BuyIn
	}
	table.UpdatedAt = time.Now()
	table.Log = append(table.Log, fmt.Sprintf("%s bought in for %d %s.", claims.Username, table.BuyIn, renderCasinoCurrency(table.StakeCurrency)))
	if len(table.Seats) >= 2 {
		s.startPokerTable(table)
	}
	s.savePokerTable(r.Context(), table)
	writeJSON(w, http.StatusOK, redactPokerTableForUser(table, claims.UserID))
}

func (s *Server) handleDrawPokerHand(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	table, err := s.getPokerTable(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if table.Status != models.PokerTableDrawing {
		writeError(w, http.StatusBadRequest, "hand is not in draw phase")
		return
	}
	var req struct {
		Discard []int `json:"discard"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	seatIdx := -1
	for i := range table.Seats {
		if table.Seats[i].UserID == claims.UserID {
			seatIdx = i
			break
		}
	}
	if seatIdx < 0 {
		writeError(w, http.StatusForbidden, "you are not seated at this table")
		return
	}
	if table.Seats[seatIdx].HasDrawn {
		writeError(w, http.StatusBadRequest, "you already acted this hand")
		return
	}
	reseedPokerHand(table, seatIdx, req.Discard)
	table.Seats[seatIdx].Discarded = append([]int(nil), req.Discard...)
	table.Seats[seatIdx].HasDrawn = true
	table.Seats[seatIdx].LastActionAt = time.Now()
	table.Log = append(table.Log, fmt.Sprintf("%s drew %d cards.", table.Seats[seatIdx].Username, len(req.Discard)))
	table.UpdatedAt = time.Now()
	allDone := true
	for _, seat := range table.Seats {
		if !seat.HasDrawn {
			allDone = false
			break
		}
	}
	if allDone {
		s.settlePokerTable(table)
	}
	s.savePokerTable(r.Context(), table)
	writeJSON(w, http.StatusOK, redactPokerTableForUser(table, claims.UserID))
}

func (s *Server) handleSpinSlots(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		Wager int64 `json:"wager"`
		Lines int   `json:"lines"`
	}
	if r.Method == http.MethodGet {
		wager, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("wager")), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "wager query parameter is required")
			return
		}
		req.Wager = wager
		if linesStr := r.URL.Query().Get("lines"); linesStr != "" {
			if l, err := strconv.Atoi(linesStr); err == nil {
				req.Lines = l
			}
		}
	} else {
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	if req.Wager <= 0 {
		writeError(w, http.StatusBadRequest, "wager must be positive")
		return
	}
	if req.Lines <= 0 || req.Lines > 9 {
		req.Lines = 9
	}
	stable := s.getStableForUser(claims.UserID)
	if stable == nil {
		writeError(w, http.StatusBadRequest, "you need a stable first")
		return
	}
	slotMu := s.stableMu(stable.ID)
	slotMu.Lock()
	totalCost := req.Wager * int64(req.Lines)
	if stable.CasinoChips < totalCost {
		slotMu.Unlock()
		writeError(w, http.StatusBadRequest, "insufficient casino chips")
		return
	}
	spin, err := s.spinSlotsForStable(r.Context(), stable, claims.UserID, claims.Username, req.Wager, req.Lines)
	slotMu.Unlock()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, spin)
}

// handleGetJackpot returns the current progressive jackpot info.
func (s *Server) handleGetJackpot(w http.ResponseWriter, r *http.Request) {
	s.jackpotMu.Lock()
	pool := s.jackpotPool
	winner := s.jackpotLastWinner
	amount := s.jackpotLastAmount
	s.jackpotMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"jackpotPool":   pool,
		"lastWinner":    winner,
		"lastWinAmount": amount,
	})
}

// ---------------------------------------------------------------------------
// Slot engine core
// ---------------------------------------------------------------------------

// spinSlotsForStable runs a full 5-reel video slot spin. The caller must hold
// the stable mutex. wager is the per-line wager; lines is 1–9.
func (s *Server) spinSlotsForStable(ctx context.Context, stable *models.Stable, userID, username string, wager int64, lines int) (*models.SlotSpin, error) {
	if stable == nil {
		return nil, fmt.Errorf("you need a stable first")
	}
	if wager <= 0 {
		return nil, fmt.Errorf("wager must be positive")
	}
	if lines < 1 || lines > 9 {
		lines = 9
	}
	totalCost := wager * int64(lines)
	if stable.CasinoChips < totalCost {
		return nil, fmt.Errorf("insufficient casino chips")
	}

	// Deduct the total wager.
	stable.CasinoChips -= totalCost

	// Contribute 2% of total wager to the progressive jackpot pool.
	jackpotContribution := totalCost * 2 / 100
	if jackpotContribution < 1 && totalCost > 0 {
		jackpotContribution = 1
	}
	s.jackpotMu.Lock()
	s.jackpotPool += jackpotContribution
	s.jackpotMu.Unlock()

	// Generate the 3×5 reel grid.
	grid := generateSlotGrid()

	// Extract the middle row for backward compatibility.
	middleRow := make([]string, 5)
	for reel := 0; reel < 5; reel++ {
		middleRow[reel] = grid[reel][1]
	}

	// Evaluate paylines.
	var winningPaylines []models.PaylineResult
	var totalPayout int64

	for lineIdx := 0; lineIdx < lines; lineIdx++ {
		pattern := slotPaylines[lineIdx]
		symbols := make([]string, 5)
		for reel := 0; reel < 5; reel++ {
			symbols[reel] = grid[reel][pattern[reel]]
		}

		matchCount, matchSymbol := evaluatePayline(symbols)
		if matchCount >= 3 && matchSymbol != "" {
			payoutMultipliers, hasPayout := slotPayouts[matchSymbol]
			if hasPayout {
				payout := payoutMultipliers[matchCount-3] * wager
				winningPaylines = append(winningPaylines, models.PaylineResult{
					LineNum: lineIdx + 1,
					Symbols: symbols,
					Match:   matchCount,
					Payout:  payout,
				})
				totalPayout += payout
			}
		}
	}

	// Check for scatter bonus (YOGURT anywhere on the grid).
	yogurtCount := countScatters(grid, symYogurt)
	var freeSpinsWon int
	var bonusTriggered string
	var bonusPayout int64

	if yogurtCount >= 3 {
		switch yogurtCount {
		case 3:
			freeSpinsWon = 5
		case 4:
			freeSpinsWon = 10
		default:
			freeSpinsWon = 15
		}
		bonusTriggered = "YOGURT_FREE_SPINS"

		// Simulate free spins with 2× multiplier.
		bonusPayout = simulateFreeSpins(wager, lines, freeSpinsWon)
		totalPayout += bonusPayout
	}

	// Check for progressive jackpot: ALL 5 reels show GOLDEN_STALLION on middle row.
	var jackpotWin bool
	if middleRow[0] == symGoldenStallion && middleRow[1] == symGoldenStallion &&
		middleRow[2] == symGoldenStallion && middleRow[3] == symGoldenStallion &&
		middleRow[4] == symGoldenStallion {
		s.jackpotMu.Lock()
		pool := s.jackpotPool
		if pool < slotJackpotMinPayout {
			pool = slotJackpotMinPayout
		}
		totalPayout += pool
		jackpotWin = true
		s.jackpotLastWinner = username
		s.jackpotLastAmount = pool
		s.jackpotPool = slotJackpotSeed // Reset after payout
		s.jackpotMu.Unlock()
	}

	// Credit winnings.
	stable.CasinoChips += totalPayout

	// Compute backward-compat multiplier.
	var multiplier float64
	if totalCost > 0 {
		multiplier = float64(totalPayout) / float64(totalCost)
	}

	// Build the reel display (reels[reelIdx] = [row0, row1, row2]).
	reels := make([][]string, 5)
	for reel := 0; reel < 5; reel++ {
		reels[reel] = grid[reel][:]
	}

	spin := &models.SlotSpin{
		ID:             uuid.New().String(),
		StableID:       stable.ID,
		UserID:         userID,
		WagerAmount:    wager,
		Lines:          lines,
		PayoutAmount:   totalPayout,
		Multiplier:     multiplier,
		Symbols:        middleRow,
		Reels:          reels,
		Paylines:       winningPaylines,
		BonusTriggered: bonusTriggered,
		BonusPayout:    bonusPayout,
		JackpotWin:     jackpotWin,
		FreeSpinsWon:   freeSpinsWon,
		TotalPayout:    totalPayout,
		Summary:        describeVideoSpin(middleRow, winningPaylines, bonusTriggered, jackpotWin, multiplier),
		CreatedAt:      time.Now(),
	}
	if s.casinoRepo != nil {
		_ = s.casinoRepo.RecordSlotSpin(ctx, spin)
	}
	s.persistStable(ctx, stable)
	return spin, nil
}

// generateSlotGrid creates a 5-reel × 3-row grid using the weighted symbol pool.
// Returns grid[reel][row].
func generateSlotGrid() [5][3]string {
	var grid [5][3]string
	poolSize := len(slotWeightedPool)
	for reel := 0; reel < 5; reel++ {
		for row := 0; row < 3; row++ {
			grid[reel][row] = slotWeightedPool[rand.IntN(poolSize)]
		}
	}
	return grid
}

// evaluatePayline checks a 5-symbol payline for left-to-right consecutive
// matching symbols (with WILD_MARE substitution). Returns (matchCount, symbol).
// Scatters (YOGURT) are not paid via paylines.
func evaluatePayline(symbols []string) (int, string) {
	if len(symbols) != 5 {
		return 0, ""
	}

	// Find the base symbol (first non-wild symbol from the left).
	baseSymbol := ""
	for _, sym := range symbols {
		if sym == symYogurt {
			// Scatter on a payline — no payline pay.
			return 0, ""
		}
		if sym != symWildMare {
			baseSymbol = sym
			break
		}
	}

	// All wilds? Treat as the highest-paying symbol.
	if baseSymbol == "" {
		baseSymbol = symGoldenStallion
	}

	// Count consecutive matches from the left.
	matchCount := 0
	for _, sym := range symbols {
		if sym == baseSymbol || sym == symWildMare {
			matchCount++
		} else {
			break
		}
	}

	if matchCount < 3 {
		return 0, ""
	}
	return matchCount, baseSymbol
}

// countScatters counts occurrences of a scatter symbol anywhere on the 3×5 grid.
func countScatters(grid [5][3]string, scatter string) int {
	count := 0
	for reel := 0; reel < 5; reel++ {
		for row := 0; row < 3; row++ {
			if grid[reel][row] == scatter {
				count++
			}
		}
	}
	return count
}

// simulateFreeSpins runs the given number of free spins and returns total
// payout. During free spins, all payouts are 2× multiplied.
func simulateFreeSpins(wager int64, lines, spins int) int64 {
	var total int64
	for i := 0; i < spins; i++ {
		grid := generateSlotGrid()
		for lineIdx := 0; lineIdx < lines; lineIdx++ {
			pattern := slotPaylines[lineIdx]
			symbols := make([]string, 5)
			for reel := 0; reel < 5; reel++ {
				symbols[reel] = grid[reel][pattern[reel]]
			}
			matchCount, matchSymbol := evaluatePayline(symbols)
			if matchCount >= 3 && matchSymbol != "" {
				if payoutMultipliers, ok := slotPayouts[matchSymbol]; ok {
					total += payoutMultipliers[matchCount-3] * wager * 2 // 2× multiplier
				}
			}
		}
	}
	return total
}

// describeVideoSpin generates a CRT-style summary string for the spin result.
func describeVideoSpin(middleRow []string, paylines []models.PaylineResult, bonus string, jackpot bool, multiplier float64) string {
	line := strings.Join(middleRow, " | ")
	if jackpot {
		return line + " -> ★★★ GOLDEN STALLION JACKPOT ★★★ THE MACHINE IS SCREAMING"
	}
	if bonus != "" {
		return line + " -> YOGURT TSUNAMI! FREE SPINS TRIGGERED"
	}
	if len(paylines) == 0 {
		return line + " -> the house wins"
	}
	// Check for DEATH JACKPOT flavor (3+ skulls on any payline).
	for _, pl := range paylines {
		if pl.Match >= 3 {
			allSkull := true
			for i := 0; i < pl.Match; i++ {
				if pl.Symbols[i] != symSkull && pl.Symbols[i] != symWildMare {
					allSkull = false
					break
				}
			}
			if allSkull {
				return line + " -> ☠ DEATH JACKPOT ☠ the skulls are rattling"
			}
		}
	}
	if multiplier >= 10 {
		return line + " -> alarmingly loud jackpot noises"
	}
	if multiplier >= 2 {
		return line + " -> nice hit, degenerate"
	}
	if multiplier >= 1 {
		return line + " -> paid out"
	}
	return line + " -> consolation dribble"
}

func (s *Server) handleListDepartedHorses(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	s.rollDepartureOmens(r.Context(), claims.UserID)
	records := s.listDepartureRecords(claims.UserID, 50)
	writeJSON(w, http.StatusOK, records)
}

func (s *Server) handleClaimDepartureReturn(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rec, err := s.getDepartureRecord(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if rec.OwnerID != claims.UserID {
		writeError(w, http.StatusForbidden, "that omen does not belong to you")
		return
	}
	if rec.State == models.DepartureStateClaimed {
		writeError(w, http.StatusConflict, "that omen was already claimed")
		return
	}
	if rec.State == models.DepartureStateLost {
		writeError(w, http.StatusBadRequest, "that omen has already faded")
		return
	}
	if !rec.OmenExpiresAt.IsZero() && time.Now().After(rec.OmenExpiresAt) {
		rec.State = models.DepartureStateLost
		s.saveDepartureRecord(r.Context(), rec)
		writeError(w, http.StatusBadRequest, "that omen has already faded")
		return
	}
	if rec.State != models.DepartureStateOmen {
		writeError(w, http.StatusBadRequest, "that horse is not currently trying to claw its way back")
		return
	}
	stable := s.getStableForUser(claims.UserID)
	if stable == nil {
		writeError(w, http.StatusBadRequest, "you need a stable first")
		return
	}
	ownerTier := s.getPrestigeTierForUser(stable.OwnerID)
	if countActiveStableHorses(stable) >= ownerTier.MaxHorses {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("stable is at max active capacity (%d horses) for prestige level %q", ownerTier.MaxHorses, ownerTier.Name))
		return
	}
	rec.State = models.DepartureStateClaimed
	rec.ReturnedAt = time.Now()
	horse := rec.HorseSnapshot
	horse.ID = uuid.New().String()
	horse.OwnerID = stable.OwnerID
	horse.Retired = false
	horse.Injury = nil
	horse.Age++
	horse.CurrentFitness = maxFloat64(0.45, horse.CurrentFitness*0.88)
	horse.Fatigue = 18
	horse.TrainingXP *= 0.55
	horse.ELO = maxFloat64(1050, horse.ELO*0.92)
	horse.PeakELO = maxFloat64(horse.PeakELO, horse.ELO)
	horse.TotalEarnings = horse.TotalEarnings / 2
	horse.Lore = strings.TrimSpace(horse.Lore + " " + rec.ReturnSummary)
	horse.Traits = append(horse.Traits, returnTraitForRecord(rec))
	if err := s.stables.AddHorseToStable(stable.ID, &horse); err != nil {
		rec.State = models.DepartureStateOmen
		rec.ReturnedAt = time.Time{}
		writeError(w, http.StatusBadRequest, "failed to re-home returned horse: "+err.Error())
		return
	}
	rec.ReturnedHorse = horse.ID
	s.persistHorse(r.Context(), &horse)
	s.saveDepartureRecord(r.Context(), rec)
	writeJSON(w, http.StatusOK, map[string]any{
		"record": rec,
		"horse":  horse,
	})
}

func (s *Server) withdrawCasinoStake(stable *models.Stable, currency string, amount int64) error {
	if stable == nil {
		return fmt.Errorf("stable not found")
	}
	switch currency {
	case "cummies":
		if stable.Cummies-amount < casinoProtectedCummiesFloor {
			return fmt.Errorf("cummies stake would drop below the protected floor of %d", casinoProtectedCummiesFloor)
		}
		stable.Cummies -= amount
	default:
		if stable.CasinoChips < amount {
			return fmt.Errorf("insufficient casino chips")
		}
		stable.CasinoChips -= amount
	}
	return nil
}

func (s *Server) maybeGrantDailyCasinoChips(ctx context.Context, userID string, stable *models.Stable) bool {
	if stable == nil || userID == "" {
		return false
	}
	today := time.Now().UTC().Format("2006-01-02")
	granted := false
	s.progressMu.Lock()
	p := s.getOrCreateProgress(userID)
	resetDailyLimitsIfNeeded(p)
	if p.LastCasinoGrantDate != today {
		stable.CasinoChips += casinoDailyChipGrant
		p.LastCasinoGrantDate = today
		granted = true
		progressSnapshot := clonePlayerProgress(p)
		s.progressMu.Unlock()
		s.persistProgress(ctx, progressSnapshot)
		s.persistStable(ctx, stable)
		return granted
	}
	s.progressMu.Unlock()
	return granted
}

func redactPokerTablesForUser(tables []*models.PokerTable, userID string) []*models.PokerTable {
	out := make([]*models.PokerTable, 0, len(tables))
	for _, table := range tables {
		out = append(out, redactPokerTableForUser(table, userID))
	}
	return out
}

func redactPokerTableForUser(table *models.PokerTable, userID string) *models.PokerTable {
	if table == nil {
		return nil
	}
	clone := *table
	clone.Seats = make([]models.PokerSeat, len(table.Seats))
	// Hands are visible to all at showdown/settled; otherwise only to the owner.
	showAll := clone.Status == models.PokerTableSettled || clone.Status == models.PokerTableShowdown
	for i, seat := range table.Seats {
		clone.Seats[i] = seat
		if seat.UserID != userID && !showAll {
			clone.Seats[i].Hand = nil
			clone.Seats[i].Discarded = nil
			clone.Seats[i].HandRank = ""
		}
	}
	return &clone
}

func renderCasinoCurrency(currency string) string {
	if currency == "cummies" {
		return "cummies"
	}
	return "casino chips"
}

func (s *Server) getPokerTable(id string) (*models.PokerTable, error) {
	if s.casinoRepo != nil {
		if table, err := s.casinoRepo.GetPokerTable(context.Background(), id); err == nil {
			return table, nil
		}
	}
	s.pokerMu.RLock()
	defer s.pokerMu.RUnlock()
	table, ok := s.pokerTables[id]
	if !ok {
		return nil, fmt.Errorf("poker table not found: %s", id)
	}
	clone := *table
	return &clone, nil
}

func (s *Server) savePokerTable(ctx context.Context, table *models.PokerTable) {
	if table == nil {
		return
	}
	s.pokerMu.Lock()
	clone := *table
	s.pokerTables[table.ID] = &clone
	s.pokerMu.Unlock()
	if s.casinoRepo != nil {
		if err := s.casinoRepo.CreatePokerTable(ctx, table); err != nil {
			_ = s.casinoRepo.UpdatePokerTable(ctx, table)
		}
	}
}

func (s *Server) startPokerTable(table *models.PokerTable) {
	if table.GameType == models.PokerGameHoldem {
		s.startHoldemHand(table)
		return
	}
	// Legacy 5-card draw start.
	table.Status = models.PokerTableDrawing
	table.StartedAt = time.Now()
	table.UpdatedAt = time.Now()
	table.Log = append(table.Log, "The dealer slides out five-card draw hands. One draw only.")
	deck := shuffledDeck(table.DeckSeed)
	idx := 0
	for seatIdx := range table.Seats {
		table.Seats[seatIdx].Hand = append([]models.PokerCard(nil), deck[idx:idx+5]...)
		table.Seats[seatIdx].HasDrawn = false
		table.Seats[seatIdx].Discarded = nil
		idx += 5
	}
}

func shuffledDeck(seed uint64) []models.PokerCard {
	rng := rand.New(rand.NewPCG(seed, seed^0xBADF00D))
	ranks := []string{"2", "3", "4", "5", "6", "7", "8", "9", "10", "J", "Q", "K", "A"}
	suits := []string{"S", "H", "D", "C"}
	deck := make([]models.PokerCard, 0, 52)
	for _, suit := range suits {
		for _, rank := range ranks {
			deck = append(deck, models.PokerCard{Rank: rank, Suit: suit})
		}
	}
	rng.Shuffle(len(deck), func(i, j int) { deck[i], deck[j] = deck[j], deck[i] })
	return deck
}

func reseedPokerHand(table *models.PokerTable, seatIdx int, discard []int) {
	if seatIdx < 0 || seatIdx >= len(table.Seats) {
		return
	}
	deck := shuffledDeck(table.DeckSeed + uint64(seatIdx+1)*31 + uint64(len(discard))*7)
	used := map[string]bool{}
	for _, seat := range table.Seats {
		for _, card := range seat.Hand {
			used[card.Rank+card.Suit] = true
		}
	}
	hand := append([]models.PokerCard(nil), table.Seats[seatIdx].Hand...)
	replaceSet := map[int]bool{}
	for _, idx := range discard {
		if idx >= 0 && idx < len(hand) {
			replaceSet[idx] = true
		}
	}
	nextCard := 0
	for i := range hand {
		if !replaceSet[i] {
			continue
		}
		for nextCard < len(deck) && used[deck[nextCard].Rank+deck[nextCard].Suit] {
			nextCard++
		}
		if nextCard >= len(deck) {
			break
		}
		hand[i] = deck[nextCard]
		used[deck[nextCard].Rank+deck[nextCard].Suit] = true
		nextCard++
	}
	table.Seats[seatIdx].Hand = hand
}

func (s *Server) settlePokerTable(table *models.PokerTable) {
	bestIdx := 0
	bestScore := -1
	for i := range table.Seats {
		score, label := evaluatePokerHand(table.Seats[i].Hand)
		table.Seats[i].HandRank = label
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	table.Status = models.PokerTableSettled
	table.Seats[bestIdx].Payout = table.Pot
	table.Log = append(table.Log, fmt.Sprintf("%s wins the pot with %s.", table.Seats[bestIdx].Username, table.Seats[bestIdx].HandRank))
	winningStable := s.getStableForUser(table.Seats[bestIdx].UserID)
	if winningStable != nil {
		pokerMu := s.stableMu(winningStable.ID)
		pokerMu.Lock()
		// Update in-memory state.
		if table.StakeCurrency == "cummies" {
			winningStable.Cummies += table.Pot
		} else {
			winningStable.CasinoChips += table.Pot
		}
		pokerMu.Unlock()
	}
	table.UpdatedAt = time.Now()

	// Write-through: persist poker settlement atomically (table status +
	// winner payout in a single DB transaction).
	if s.pgDB != nil && winningStable != nil {
		if err := s.pgDB.SettlePokerAtomically(
			context.Background(),
			table,
			winningStable.ID,
			table.Pot,
			table.StakeCurrency,
		); err != nil {
			log.Printf("server: atomic poker settlement failed for table %s: %v", table.ID, err)
		}
	} else if winningStable != nil {
		// Fallback: non-transactional persistence.
		s.persistStable(context.Background(), winningStable)
	}
}

// pokerRankOrder maps rank strings to their numeric value for straight detection.
var pokerRankOrder = map[string]int{
	"2": 2, "3": 3, "4": 4, "5": 5, "6": 6, "7": 7,
	"8": 8, "9": 9, "10": 10, "J": 11, "Q": 12, "K": 13, "A": 14,
}

// isPokerStraight checks whether the given 5-card hand forms a straight.
// It handles the ace-low straight (A-2-3-4-5) as well.
func isPokerStraight(hand []models.PokerCard) bool {
	if len(hand) != 5 {
		return false
	}
	ranks := make([]int, len(hand))
	for i, card := range hand {
		r, ok := pokerRankOrder[card.Rank]
		if !ok {
			return false
		}
		ranks[i] = r
	}
	sort.Ints(ranks)

	// Normal straight: consecutive values
	straight := true
	for i := 1; i < len(ranks); i++ {
		if ranks[i] != ranks[i-1]+1 {
			straight = false
			break
		}
	}
	if straight {
		return true
	}

	// Ace-low straight: A-2-3-4-5 (ranks sorted as [2, 3, 4, 5, 14])
	if ranks[0] == 2 && ranks[1] == 3 && ranks[2] == 4 && ranks[3] == 5 && ranks[4] == 14 {
		return true
	}

	return false
}

func evaluatePokerHand(hand []models.PokerCard) (int, string) {
	counts := map[string]int{}
	for _, card := range hand {
		counts[card.Rank]++
	}
	vals := make([]int, 0, len(counts))
	for _, count := range counts {
		vals = append(vals, count)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(vals)))
	sameSuit := true
	for i := 1; i < len(hand); i++ {
		if hand[i].Suit != hand[0].Suit {
			sameSuit = false
			break
		}
	}
	straight := isPokerStraight(hand)

	// Straight flush (includes royal flush)
	if sameSuit && straight {
		return 800, "straight flush"
	}
	if len(vals) == 2 && vals[0] == 4 {
		return 700, "four of a kind"
	}
	if len(vals) == 2 && vals[0] == 3 {
		return 650, "full house"
	}
	if sameSuit {
		return 600, "flush"
	}
	if straight {
		return 550, "straight"
	}
	if len(vals) == 3 && vals[0] == 3 {
		return 500, "three of a kind"
	}
	if len(vals) == 3 && vals[0] == 2 && vals[1] == 2 {
		return 400, "two pair"
	}
	if len(vals) == 4 && vals[0] == 2 {
		return 300, "one pair"
	}
	return 100, "high card"
}

// ---------------------------------------------------------------------------
// Texas Hold'em Engine
// ---------------------------------------------------------------------------
//
// The hold'em flow:
//   open -> preflop -> flop -> turn -> river -> showdown -> settled
//
// Each betting round waits for all non-folded players to match the current bet
// (or go all-in). The action seat advances clockwise until everyone has acted.

const holdemActionTimeout = 60 * time.Second

// startHoldemHand initialises a new hold'em hand: shuffles, posts blinds, and
// deals 2 hole cards to each active player.
func (s *Server) startHoldemHand(table *models.PokerTable) {
	table.Round++
	table.DeckSeed = rand.Uint64()
	table.CommunityCards = nil
	table.SidePots = nil
	table.CurrentBet = 0
	table.MinRaise = table.BigBlind

	// Reset per-seat state.
	for i := range table.Seats {
		table.Seats[i].Hand = nil
		table.Seats[i].Folded = false
		table.Seats[i].AllIn = false
		table.Seats[i].CurrentBet = 0
		table.Seats[i].LastAction = ""
		table.Seats[i].HandRank = ""
		table.Seats[i].Payout = 0
		table.Seats[i].HasDrawn = false
	}

	// Advance the dealer button.
	if table.Round > 1 {
		table.DealerSeat = holdemNextActiveSeat(table, table.DealerSeat)
	}

	// Post blinds.
	sbIdx := holdemNextActiveSeat(table, table.DealerSeat)
	bbIdx := holdemNextActiveSeat(table, sbIdx)

	holdemPostBlind(table, sbIdx, table.SmallBlind, "small blind")
	holdemPostBlind(table, bbIdx, table.BigBlind, "big blind")
	table.CurrentBet = table.BigBlind
	table.MinRaise = table.BigBlind

	// Deal 2 hole cards to each player.
	deck := shuffledDeck(table.DeckSeed)
	idx := 0
	for i := range table.Seats {
		if table.Seats[i].ChipStack > 0 || table.Seats[i].AllIn {
			table.Seats[i].Hand = append([]models.PokerCard(nil), deck[idx:idx+2]...)
			idx += 2
		}
	}

	table.Status = models.PokerTablePreFlop
	table.StartedAt = time.Now()
	table.UpdatedAt = time.Now()

	// Action starts left of the big blind.
	table.ActionSeat = holdemNextActionSeat(table, bbIdx)
	table.ActionDeadline = time.Now().Add(holdemActionTimeout)

	table.Log = append(table.Log, fmt.Sprintf(
		"Hand #%d — %s posts SB (%d), %s posts BB (%d). The oat dust settles. Two cards hit the felt.",
		table.Round,
		table.Seats[sbIdx].Username, table.SmallBlind,
		table.Seats[bbIdx].Username, table.BigBlind,
	))

	// If only one player still needs to act (heads-up all-in from blinds), skip to showdown.
	if holdemCountActive(table) <= 1 || holdemAllActed(table) {
		s.advanceHoldemRound(table)
	}
}

// holdemPostBlind deducts a blind from a player's chip stack into the pot.
func holdemPostBlind(table *models.PokerTable, seatIdx int, amount int64, label string) {
	seat := &table.Seats[seatIdx]
	posted := amount
	if seat.ChipStack <= amount {
		posted = seat.ChipStack
		seat.AllIn = true
	}
	seat.ChipStack -= posted
	seat.CurrentBet = posted
	table.Pot += posted
}

// holdemNextActiveSeat returns the next seat index clockwise that is not folded
// and has chips (or is all-in). Wraps around.
func holdemNextActiveSeat(table *models.PokerTable, from int) int {
	n := len(table.Seats)
	for i := 1; i <= n; i++ {
		idx := (from + i) % n
		if !table.Seats[idx].Folded {
			return idx
		}
	}
	return from // fallback (should never happen if >1 player)
}

// holdemNextActionSeat returns the next seat that can still act (not folded, not all-in).
func holdemNextActionSeat(table *models.PokerTable, from int) int {
	n := len(table.Seats)
	for i := 1; i <= n; i++ {
		idx := (from + i) % n
		if !table.Seats[idx].Folded && !table.Seats[idx].AllIn {
			return idx
		}
	}
	return -1 // everyone is folded or all-in
}

// holdemCountActive returns the number of players that are not folded.
func holdemCountActive(table *models.PokerTable) int {
	count := 0
	for _, s := range table.Seats {
		if !s.Folded {
			count++
		}
	}
	return count
}

// holdemCountCanAct returns the number of players who can still take an action
// (not folded, not all-in).
func holdemCountCanAct(table *models.PokerTable) int {
	count := 0
	for _, s := range table.Seats {
		if !s.Folded && !s.AllIn {
			count++
		}
	}
	return count
}

// holdemAllActed returns true if every non-folded, non-all-in player has matched
// the current bet and has taken at least one action this round.
func holdemAllActed(table *models.PokerTable) bool {
	for _, seat := range table.Seats {
		if seat.Folded || seat.AllIn {
			continue
		}
		if seat.LastAction == "" {
			return false // hasn't acted yet this round
		}
		if seat.CurrentBet < table.CurrentBet {
			return false
		}
	}
	return true
}

// handleHoldemAction processes a player action at a Hold'em table.
func (s *Server) handleHoldemAction(w http.ResponseWriter, r *http.Request) {
	claims, ok := authussy.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	table, err := s.getPokerTable(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	// Verify this is a hold'em table in an active betting round.
	if table.GameType != models.PokerGameHoldem {
		writeError(w, http.StatusBadRequest, "this endpoint is for hold'em tables only; use /draw for 5-card draw")
		return
	}
	switch table.Status {
	case models.PokerTablePreFlop, models.PokerTableFlop, models.PokerTableTurn, models.PokerTableRiver:
		// OK, active round.
	default:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("table is not in an active betting round (status: %s)", table.Status))
		return
	}

	// Check for timeout on current action seat — auto-fold if expired.
	if !table.ActionDeadline.IsZero() && time.Now().After(table.ActionDeadline) && table.ActionSeat >= 0 {
		timedOutSeat := table.ActionSeat
		s.holdemApplyAction(table, timedOutSeat, "fold", 0)
		table.Log = append(table.Log, fmt.Sprintf("%s took too long and was auto-folded by the merciless clock.", table.Seats[timedOutSeat].Username))

		// Check if round should advance after the auto-fold.
		if holdemCountActive(table) <= 1 {
			s.settleHoldemSingleWinner(table)
			s.savePokerTable(r.Context(), table)
			writeJSON(w, http.StatusOK, redactPokerTableForUser(table, claims.UserID))
			return
		}
		if holdemAllActed(table) || holdemCountCanAct(table) == 0 {
			s.advanceHoldemRound(table)
		} else {
			table.ActionSeat = holdemNextActionSeat(table, timedOutSeat)
			table.ActionDeadline = time.Now().Add(holdemActionTimeout)
		}
		// If the table was settled during advance, just return.
		if table.Status == models.PokerTableSettled {
			s.savePokerTable(r.Context(), table)
			writeJSON(w, http.StatusOK, redactPokerTableForUser(table, claims.UserID))
			return
		}
	}

	// Find the player's seat.
	seatIdx := -1
	for i := range table.Seats {
		if table.Seats[i].UserID == claims.UserID {
			seatIdx = i
			break
		}
	}
	if seatIdx < 0 {
		writeError(w, http.StatusForbidden, "you are not seated at this table")
		return
	}

	// Check it's their turn.
	if table.ActionSeat != seatIdx {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("it's not your turn (seat %d is acting)", table.ActionSeat))
		return
	}

	var req struct {
		Action string `json:"action"`
		Amount int64  `json:"amount"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))

	// Validate and apply the action.
	if err := s.holdemValidateAction(table, seatIdx, action, req.Amount); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.holdemApplyAction(table, seatIdx, action, req.Amount)

	// Check for single winner (everyone else folded).
	if holdemCountActive(table) <= 1 {
		s.settleHoldemSingleWinner(table)
		s.savePokerTable(r.Context(), table)
		writeJSON(w, http.StatusOK, redactPokerTableForUser(table, claims.UserID))
		return
	}

	// Check if round should advance.
	if holdemAllActed(table) || holdemCountCanAct(table) == 0 {
		s.advanceHoldemRound(table)
	} else {
		table.ActionSeat = holdemNextActionSeat(table, seatIdx)
		if table.ActionSeat < 0 {
			// All remaining players are all-in; run out the board.
			s.advanceHoldemRound(table)
		} else {
			table.ActionDeadline = time.Now().Add(holdemActionTimeout)
		}
	}

	s.savePokerTable(r.Context(), table)
	writeJSON(w, http.StatusOK, redactPokerTableForUser(table, claims.UserID))
}

// holdemValidateAction checks if the proposed action is legal.
func (s *Server) holdemValidateAction(table *models.PokerTable, seatIdx int, action string, amount int64) error {
	seat := &table.Seats[seatIdx]
	if seat.Folded {
		return fmt.Errorf("you've already folded, champion")
	}
	if seat.AllIn {
		return fmt.Errorf("you're already all-in — just pray now")
	}

	toCall := table.CurrentBet - seat.CurrentBet

	switch action {
	case "fold":
		return nil
	case "check":
		if toCall > 0 {
			return fmt.Errorf("can't check when there's %d to call — try 'call' or 'raise'", toCall)
		}
		return nil
	case "call":
		if toCall <= 0 {
			return fmt.Errorf("nothing to call — use 'check'")
		}
		return nil
	case "raise":
		if amount <= 0 {
			return fmt.Errorf("raise amount must be positive")
		}
		// Total bet after raise must be at least currentBet + minRaise.
		totalBet := seat.CurrentBet + amount
		if totalBet < table.CurrentBet+table.MinRaise && amount < seat.ChipStack {
			return fmt.Errorf("raise must be at least %d total (current bet %d + min raise %d); you proposed %d",
				table.CurrentBet+table.MinRaise, table.CurrentBet, table.MinRaise, totalBet)
		}
		return nil
	case "allin":
		return nil
	default:
		return fmt.Errorf("unknown action %q — valid: fold, check, call, raise, allin", action)
	}
}

// holdemApplyAction modifies the table state based on a validated action.
func (s *Server) holdemApplyAction(table *models.PokerTable, seatIdx int, action string, amount int64) {
	seat := &table.Seats[seatIdx]
	seat.LastAction = action
	seat.LastActionAt = time.Now()

	switch action {
	case "fold":
		seat.Folded = true
		table.Log = append(table.Log, fmt.Sprintf("%s folds. Discretion is the better part of cowardice.", seat.Username))

	case "check":
		table.Log = append(table.Log, fmt.Sprintf("%s checks. The tension is palpable. Or is that just gas?", seat.Username))

	case "call":
		toCall := table.CurrentBet - seat.CurrentBet
		if toCall > seat.ChipStack {
			// All-in call.
			toCall = seat.ChipStack
			seat.AllIn = true
			seat.LastAction = "allin"
		}
		seat.ChipStack -= toCall
		seat.CurrentBet += toCall
		table.Pot += toCall
		if seat.AllIn {
			table.Log = append(table.Log, fmt.Sprintf("%s calls ALL-IN for %d! The stable's reputation hangs by a thread.", seat.Username, toCall))
		} else {
			table.Log = append(table.Log, fmt.Sprintf("%s calls %d. Throwing good chips after bad.", seat.Username, toCall))
		}

	case "raise":
		// Amount is the total additional chips the player puts in beyond their current bet.
		actual := amount
		if actual >= seat.ChipStack {
			actual = seat.ChipStack
			seat.AllIn = true
		}
		raiseTotal := seat.CurrentBet + actual
		if raiseTotal > table.CurrentBet {
			raiseIncrement := raiseTotal - table.CurrentBet
			if raiseIncrement > table.MinRaise {
				table.MinRaise = raiseIncrement
			}
			table.CurrentBet = raiseTotal
		}
		seat.ChipStack -= actual
		seat.CurrentBet += actual
		table.Pot += actual
		// Reset LastAction for other players so they must act again.
		for i := range table.Seats {
			if i != seatIdx && !table.Seats[i].Folded && !table.Seats[i].AllIn {
				table.Seats[i].LastAction = ""
			}
		}
		if seat.AllIn {
			table.Log = append(table.Log, fmt.Sprintf("%s raises ALL-IN to %d! Absolutely unhinged.", seat.Username, seat.CurrentBet))
		} else {
			table.Log = append(table.Log, fmt.Sprintf("%s raises to %d. Bold strategy, Cotton.", seat.Username, seat.CurrentBet))
		}

	case "allin":
		allInAmount := seat.ChipStack
		seat.ChipStack = 0
		seat.CurrentBet += allInAmount
		seat.AllIn = true
		table.Pot += allInAmount
		if seat.CurrentBet > table.CurrentBet {
			raiseIncrement := seat.CurrentBet - table.CurrentBet
			if raiseIncrement > table.MinRaise {
				table.MinRaise = raiseIncrement
			}
			table.CurrentBet = seat.CurrentBet
			// Reset others.
			for i := range table.Seats {
				if i != seatIdx && !table.Seats[i].Folded && !table.Seats[i].AllIn {
					table.Seats[i].LastAction = ""
				}
			}
		}
		table.Log = append(table.Log, fmt.Sprintf("%s goes ALL-IN for %d! The casino rats scatter.", seat.Username, allInAmount))
	}
	table.UpdatedAt = time.Now()
}

// advanceHoldemRound progresses the table to the next community card phase.
// If we reach showdown or only one player remains, settles the hand.
func (s *Server) advanceHoldemRound(table *models.PokerTable) {
	// Build a deck that excludes already-dealt cards.
	deck := shuffledDeck(table.DeckSeed)
	dealt := map[string]bool{}
	for _, seat := range table.Seats {
		for _, c := range seat.Hand {
			dealt[c.Rank+c.Suit] = true
		}
	}
	for _, c := range table.CommunityCards {
		dealt[c.Rank+c.Suit] = true
	}
	// Collect remaining cards for community dealing.
	remaining := make([]models.PokerCard, 0, 52-len(dealt))
	for _, c := range deck {
		if !dealt[c.Rank+c.Suit] {
			remaining = append(remaining, c)
		}
	}

	// Reset betting state for the new round.
	holdemResetBettingRound(table)

	switch table.Status {
	case models.PokerTablePreFlop:
		// Deal flop (3 community cards).
		if len(remaining) >= 3 {
			table.CommunityCards = append(table.CommunityCards, remaining[0:3]...)
			remaining = remaining[3:]
		}
		table.Status = models.PokerTableFlop
		table.Log = append(table.Log, fmt.Sprintf("FLOP: %s %s %s — the plot thickens like expired yogurt.",
			cardStr(table.CommunityCards[0]), cardStr(table.CommunityCards[1]), cardStr(table.CommunityCards[2])))

	case models.PokerTableFlop:
		// Deal turn (1 community card).
		if len(remaining) >= 1 {
			table.CommunityCards = append(table.CommunityCards, remaining[0])
			remaining = remaining[1:]
		}
		table.Status = models.PokerTableTurn
		table.Log = append(table.Log, fmt.Sprintf("TURN: %s — the degeneracy deepens.", cardStr(table.CommunityCards[3])))

	case models.PokerTableTurn:
		// Deal river (1 community card).
		if len(remaining) >= 1 {
			table.CommunityCards = append(table.CommunityCards, remaining[0])
		}
		table.Status = models.PokerTableRiver
		table.Log = append(table.Log, fmt.Sprintf("RIVER: %s — final card, final prayers.", cardStr(table.CommunityCards[4])))

	case models.PokerTableRiver:
		// Move to showdown.
		table.Status = models.PokerTableShowdown
		s.settleHoldemTable(table)
		return
	}

	// Set action to first active seat left of dealer.
	table.ActionSeat = holdemNextActionSeat(table, table.DealerSeat)
	if table.ActionSeat < 0 || holdemCountCanAct(table) <= 0 {
		// Everyone is all-in; run out the next street.
		s.advanceHoldemRound(table)
		return
	}
	table.ActionDeadline = time.Now().Add(holdemActionTimeout)
	table.UpdatedAt = time.Now()
}

// holdemResetBettingRound clears bets for a new street.
func holdemResetBettingRound(table *models.PokerTable) {
	table.CurrentBet = 0
	table.MinRaise = table.BigBlind
	for i := range table.Seats {
		table.Seats[i].CurrentBet = 0
		if !table.Seats[i].Folded && !table.Seats[i].AllIn {
			table.Seats[i].LastAction = ""
		}
	}
}

// settleHoldemSingleWinner awards the pot when all opponents have folded.
func (s *Server) settleHoldemSingleWinner(table *models.PokerTable) {
	winnerIdx := -1
	for i, seat := range table.Seats {
		if !seat.Folded {
			winnerIdx = i
			break
		}
	}
	if winnerIdx < 0 {
		return // should not happen
	}
	winner := &table.Seats[winnerIdx]
	winner.ChipStack += table.Pot
	winner.Payout = table.Pot
	table.Log = append(table.Log, fmt.Sprintf("%s collects the pot of %d — everyone else folded like cheap lawn chairs.", winner.Username, table.Pot))
	table.Pot = 0
	table.Status = models.PokerTableSettled
	table.ActionSeat = -1
	table.UpdatedAt = time.Now()

	// Cash out all players.
	s.holdemCashOutPlayers(table)
}

// settleHoldemTable evaluates hands at showdown and distributes the pot.
func (s *Server) settleHoldemTable(table *models.PokerTable) {
	type playerHand struct {
		seatIdx int
		score   int
		label   string
		kickers []int
	}

	var hands []playerHand
	for i := range table.Seats {
		if table.Seats[i].Folded {
			continue
		}
		// Combine hole cards + community cards.
		allCards := make([]models.PokerCard, 0, 7)
		allCards = append(allCards, table.Seats[i].Hand...)
		allCards = append(allCards, table.CommunityCards...)

		score, label, kickers := evaluateBestHoldemHand(allCards)
		table.Seats[i].HandRank = label
		hands = append(hands, playerHand{
			seatIdx: i,
			score:   score,
			label:   label,
			kickers: kickers,
		})
	}

	// Sort by hand strength (descending), then by kickers.
	sort.Slice(hands, func(i, j int) bool {
		if hands[i].score != hands[j].score {
			return hands[i].score > hands[j].score
		}
		return compareKickers(hands[i].kickers, hands[j].kickers) > 0
	})

	// Build side pots and distribute.
	pots := holdemBuildPots(table)
	for _, pot := range pots {
		// Find the best hand(s) among eligible players.
		var potWinners []int
		bestScore := -1
		var bestKickers []int
		for _, ph := range hands {
			uid := table.Seats[ph.seatIdx].UserID
			if !containsStr(pot.Eligible, uid) {
				continue
			}
			cmp := 0
			if ph.score > bestScore {
				cmp = 1
			} else if ph.score == bestScore {
				cmp = compareKickers(ph.kickers, bestKickers)
			}
			if cmp > 0 {
				bestScore = ph.score
				bestKickers = ph.kickers
				potWinners = []int{ph.seatIdx}
			} else if cmp == 0 {
				potWinners = append(potWinners, ph.seatIdx)
			}
		}

		if len(potWinners) == 0 {
			continue
		}
		share := pot.Amount / int64(len(potWinners))
		remainder := pot.Amount % int64(len(potWinners))
		for i, wi := range potWinners {
			payout := share
			if i == 0 {
				payout += remainder // first winner gets the remainder
			}
			table.Seats[wi].ChipStack += payout
			table.Seats[wi].Payout += payout
		}
	}

	// Log the results.
	if len(hands) > 0 {
		winnerIdx := hands[0].seatIdx
		table.Log = append(table.Log, fmt.Sprintf(
			"SHOWDOWN: %s wins with %s! The crowd (three drunk stable-hands) goes wild.",
			table.Seats[winnerIdx].Username, table.Seats[winnerIdx].HandRank,
		))
	}

	table.Pot = 0
	table.Status = models.PokerTableSettled
	table.ActionSeat = -1
	table.UpdatedAt = time.Now()

	// Cash out all players.
	s.holdemCashOutPlayers(table)
}

// holdemCashOutPlayers returns each player's remaining chip stack to their stable.
func (s *Server) holdemCashOutPlayers(table *models.PokerTable) {
	for i := range table.Seats {
		seat := &table.Seats[i]
		cashout := seat.ChipStack
		if cashout <= 0 {
			continue
		}
		stable := s.getStableForUser(seat.UserID)
		if stable == nil {
			continue
		}
		mu := s.stableMu(stable.ID)
		mu.Lock()
		if table.StakeCurrency == "cummies" {
			stable.Cummies += cashout
		} else {
			stable.CasinoChips += cashout
		}
		mu.Unlock()
		// Persist asynchronously in background.
		s.persistStable(context.Background(), stable)
		seat.ChipStack = 0
	}
}

// holdemBuildPots builds the main pot and any side pots from the all-in amounts.
func holdemBuildPots(table *models.PokerTable) []models.SidePot {
	// Collect bet-levels from all-in players.
	hasAllIn := false
	for _, seat := range table.Seats {
		if seat.AllIn && !seat.Folded {
			hasAllIn = true
			break
		}
	}

	// If no all-ins, simple: one pot, all non-folded players eligible.
	if !hasAllIn {
		eligible := make([]string, 0)
		for _, seat := range table.Seats {
			if !seat.Folded {
				eligible = append(eligible, seat.UserID)
			}
		}
		return []models.SidePot{{Amount: table.Pot, Eligible: eligible}}
	}

	// Compute total contribution per player: buyIn - remaining chipStack.
	type playerContrib struct {
		seatIdx int
		userID  string
		total   int64
		folded  bool
	}
	var contribs []playerContrib
	allInLevels := make(map[int64]bool)
	maxContrib := int64(0)
	for i, seat := range table.Seats {
		total := seat.BuyIn - seat.ChipStack
		if total < 0 {
			total = 0
		}
		contribs = append(contribs, playerContrib{
			seatIdx: i,
			userID:  seat.UserID,
			total:   total,
			folded:  seat.Folded,
		})
		if seat.AllIn && !seat.Folded {
			allInLevels[total] = true
		}
		if total > maxContrib {
			maxContrib = total
		}
	}

	// Build sorted levels.
	levels := make([]int64, 0, len(allInLevels)+1)
	for lvl := range allInLevels {
		levels = append(levels, lvl)
	}
	sort.Slice(levels, func(i, j int) bool { return levels[i] < levels[j] })
	// Add maxContrib if not already there.
	if len(levels) == 0 || levels[len(levels)-1] < maxContrib {
		levels = append(levels, maxContrib)
	}

	var pots []models.SidePot
	prevLevel := int64(0)
	remainingPot := table.Pot

	for _, level := range levels {
		layerSize := level - prevLevel
		if layerSize <= 0 {
			continue
		}

		// Compute how much each player contributes to this layer.
		eligible := make([]string, 0)
		potAmount := int64(0)
		for _, pc := range contribs {
			if pc.total <= prevLevel {
				continue // already accounted for
			}
			contrib := pc.total - prevLevel
			if contrib > layerSize {
				contrib = layerSize
			}
			potAmount += contrib
			// Only non-folded players with enough total are eligible.
			if !pc.folded && pc.total >= level {
				eligible = append(eligible, pc.userID)
			}
		}

		if potAmount > remainingPot {
			potAmount = remainingPot
		}
		if potAmount > 0 {
			pots = append(pots, models.SidePot{Amount: potAmount, Eligible: eligible})
			remainingPot -= potAmount
		}
		prevLevel = level
	}

	// If there's any remainder, add to last pot.
	if remainingPot > 0 && len(pots) > 0 {
		pots[len(pots)-1].Amount += remainingPot
	} else if remainingPot > 0 {
		eligible := make([]string, 0)
		for _, seat := range table.Seats {
			if !seat.Folded {
				eligible = append(eligible, seat.UserID)
			}
		}
		pots = append(pots, models.SidePot{Amount: remainingPot, Eligible: eligible})
	}

	return pots
}

// evaluateBestHoldemHand finds the best 5-card hand from 5, 6, or 7 cards.
// Returns (score, label, kickers) where kickers is a descending-sorted list
// of card values used for tie-breaking.
func evaluateBestHoldemHand(cards []models.PokerCard) (int, string, []int) {
	n := len(cards)
	if n < 5 {
		// Shouldn't happen, but handle gracefully.
		score, label := evaluatePokerHand(cards)
		return score, label, nil
	}
	if n == 5 {
		score, label := evaluatePokerHand(cards)
		return score, label, handKickers(cards, score)
	}

	bestScore := -1
	bestLabel := ""
	var bestKickers []int

	// Try all C(n,5) combinations.
	combos := combinations(n, 5)
	for _, combo := range combos {
		hand := make([]models.PokerCard, 5)
		for i, idx := range combo {
			hand[i] = cards[idx]
		}
		score, label := evaluatePokerHand(hand)
		kickers := handKickers(hand, score)

		cmp := 0
		if score > bestScore {
			cmp = 1
		} else if score == bestScore {
			cmp = compareKickers(kickers, bestKickers)
		}
		if cmp > 0 {
			bestScore = score
			bestLabel = label
			bestKickers = kickers
		}
	}
	return bestScore, bestLabel, bestKickers
}

// handKickers returns a descending-sorted list of card values representing
// the kicker breakdown for tie-breaking. The structure depends on hand type.
func handKickers(hand []models.PokerCard, score int) []int {
	rankCounts := map[int]int{}
	for _, c := range hand {
		rankCounts[pokerRankOrder[c.Rank]]++
	}

	// Group by count: quads, trips, pairs, singles.
	var quads, trips, pairs, singles []int
	for rank, count := range rankCounts {
		switch count {
		case 4:
			quads = append(quads, rank)
		case 3:
			trips = append(trips, rank)
		case 2:
			pairs = append(pairs, rank)
		default:
			singles = append(singles, rank)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(quads)))
	sort.Sort(sort.Reverse(sort.IntSlice(trips)))
	sort.Sort(sort.Reverse(sort.IntSlice(pairs)))
	sort.Sort(sort.Reverse(sort.IntSlice(singles)))

	// For straights, the "high card" is the top of the straight.
	// Special case: ace-low straight (A-2-3-4-5) has high card 5.
	allRanks := make([]int, len(hand))
	for i, c := range hand {
		allRanks[i] = pokerRankOrder[c.Rank]
	}
	sort.Sort(sort.Reverse(sort.IntSlice(allRanks)))

	switch {
	case score >= 800: // straight flush
		if isAceLowStraight(allRanks) {
			return []int{5} // ace-low straight flush: 5-high
		}
		return []int{allRanks[0]}
	case score >= 700: // four of a kind
		result := append(quads, singles...)
		return result
	case score >= 650: // full house
		result := append(trips, pairs...)
		return result
	case score >= 600: // flush
		return allRanks
	case score >= 550: // straight
		if isAceLowStraight(allRanks) {
			return []int{5}
		}
		return []int{allRanks[0]}
	case score >= 500: // three of a kind
		result := append(trips, singles...)
		return result
	case score >= 400: // two pair
		result := append(pairs, singles...)
		return result
	case score >= 300: // one pair
		result := append(pairs, singles...)
		return result
	default: // high card
		return allRanks
	}
}

// isAceLowStraight checks if sorted-descending ranks represent A-2-3-4-5.
func isAceLowStraight(ranks []int) bool {
	if len(ranks) != 5 {
		return false
	}
	sorted := make([]int, len(ranks))
	copy(sorted, ranks)
	sort.Ints(sorted)
	return sorted[0] == 2 && sorted[1] == 3 && sorted[2] == 4 && sorted[3] == 5 && sorted[4] == 14
}

// compareKickers compares two kicker arrays. Returns +1 if a > b, -1 if a < b, 0 if equal.
func compareKickers(a, b []int) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] > b[i] {
			return 1
		}
		if a[i] < b[i] {
			return -1
		}
	}
	return 0
}

// combinations generates all C(n,k) index combinations.
func combinations(n, k int) [][]int {
	if k > n {
		return nil
	}
	var result [][]int
	combo := make([]int, k)
	var generate func(start, depth int)
	generate = func(start, depth int) {
		if depth == k {
			tmp := make([]int, k)
			copy(tmp, combo)
			result = append(result, tmp)
			return
		}
		for i := start; i < n; i++ {
			combo[depth] = i
			generate(i+1, depth+1)
		}
	}
	generate(0, 0)
	return result
}

// cardStr returns a human-readable card string like "A♠" or "10♥".
func cardStr(c models.PokerCard) string {
	suitSymbol := c.Suit
	switch c.Suit {
	case "S":
		suitSymbol = "♠"
	case "H":
		suitSymbol = "♥"
	case "D":
		suitSymbol = "♦"
	case "C":
		suitSymbol = "♣"
	}
	return c.Rank + suitSymbol
}

// containsStr checks if a string slice contains a value.
func containsStr(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

func slotMultiplier(symbols []string) float64 {
	if len(symbols) != 3 {
		return 0
	}
	if symbols[0] == symbols[1] && symbols[1] == symbols[2] {
		switch symbols[0] {
		case "SEVEN":
			return 4.0
		case "YOGURT":
			return 6.5
		case "SKULL":
			return 2.5
		default:
			return 2.0
		}
	}
	if symbols[0] == symbols[1] || symbols[1] == symbols[2] || symbols[0] == symbols[2] {
		return 0.75
	}
	if containsSymbol(symbols, "YOGURT") && containsSymbol(symbols, "BELL") {
		return 1.25
	}
	return 0
}

func containsSymbol(symbols []string, target string) bool {
	for _, sym := range symbols {
		if sym == target {
			return true
		}
	}
	return false
}

func describeSpin(symbols []string, multiplier float64) string {
	line := strings.Join(symbols, " | ")
	if multiplier <= 0 {
		return line + " -> the house wins"
	}
	if multiplier < 1 {
		return line + " -> consolation dribble"
	}
	if multiplier >= 4 {
		return line + " -> alarmingly loud jackpot noises"
	}
	return line + " -> paid out"
}

func (s *Server) recordDeparture(ctx context.Context, stable *models.Stable, horse *models.Horse, cause string) {
	if stable == nil || horse == nil {
		return
	}
	rec := &models.DepartureRecord{
		ID:            uuid.New().String(),
		HorseID:       horse.ID,
		HorseName:     horse.Name,
		OwnerID:       stable.OwnerID,
		StableID:      stable.ID,
		Cause:         cause,
		State:         models.DepartureStateDormant,
		HorseSnapshot: *horse,
		CreatedAt:     time.Now(),
	}
	s.saveDepartureRecord(ctx, rec)
}

func (s *Server) listDepartureRecords(ownerID string, limit int) []*models.DepartureRecord {
	if s.departureRepo != nil {
		if records, err := s.departureRepo.ListDeparturesByOwner(context.Background(), ownerID, limit); err == nil {
			return records
		}
	}
	s.departMu.RLock()
	defer s.departMu.RUnlock()
	list := make([]*models.DepartureRecord, 0)
	for _, rec := range s.departures {
		if rec.OwnerID == ownerID {
			clone := *rec
			list = append(list, &clone)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.After(list[j].CreatedAt) })
	if len(list) > limit && limit > 0 {
		return list[:limit]
	}
	return list
}

func (s *Server) getDepartureRecord(id string) (*models.DepartureRecord, error) {
	if s.departureRepo != nil {
		if rec, err := s.departureRepo.GetDeparture(context.Background(), id); err == nil {
			return rec, nil
		}
	}
	s.departMu.RLock()
	defer s.departMu.RUnlock()
	rec, ok := s.departures[id]
	if !ok {
		return nil, fmt.Errorf("departure not found: %s", id)
	}
	clone := *rec
	return &clone, nil
}

func (s *Server) saveDepartureRecord(ctx context.Context, record *models.DepartureRecord) {
	if record == nil {
		return
	}
	s.departMu.Lock()
	clone := *record
	s.departures[record.ID] = &clone
	s.departMu.Unlock()
	if s.departureRepo != nil {
		if err := s.departureRepo.CreateDeparture(ctx, record); err != nil {
			_ = s.departureRepo.UpdateDeparture(ctx, record)
		}
	}
}

func (s *Server) rollDepartureOmens(ctx context.Context, ownerID string) {
	today := time.Now().UTC().Format("2006-01-02")
	for _, rec := range s.listDepartureRecords(ownerID, 100) {
		if rec.State != models.DepartureStateDormant || rec.LastRollDate == today {
			continue
		}
		rec.LastRollDate = today
		chance := glueReturnChance
		if rec.Cause == models.DepartureCauseFight {
			chance = deathReturnChance
		}
		if rand.Float64() >= chance {
			s.saveDepartureRecord(ctx, rec)
			continue
		}
		options := omenTexts[rec.Cause]
		if len(options) == 0 {
			options = []string{"Something impossible scratched at the stable door."}
		}
		rec.State = models.DepartureStateOmen
		rec.OmenText = options[rand.IntN(len(options))]
		rec.ReturnSummary = buildReturnSummary(rec)
		rec.OmenExpiresAt = time.Now().Add(72 * time.Hour)
		s.saveDepartureRecord(ctx, rec)
	}
}

func buildReturnSummary(rec *models.DepartureRecord) string {
	switch rec.Cause {
	case models.DepartureCauseFight:
		return "Returned from the arena veil with unfinished violence in its lungs. It runs colder, stranger, and hungrier for spotlight moments."
	default:
		return "Escaped the adhesive afterlife under grotesquely improbable circumstances. Crowds cannot decide whether to worship it or report it."
	}
}

func returnTraitForRecord(rec *models.DepartureRecord) models.Trait {
	trait := models.Trait{
		ID:        uuid.New().String(),
		Magnitude: 1.08,
		Rarity:    "anomalous",
	}
	if rec.Cause == models.DepartureCauseFight {
		trait.Name = "Scarred Brawler"
		trait.Description = "Returned from a fatal arena result. Bursts harder in chaos but carries the memory of impact."
		trait.Effect = "haunted_boost"
		return trait
	}
	trait.Name = "Factory Escapee"
	trait.Description = "Should not exist. Spectators flock to the scandal and the horse now pulls occult momentum from haunted tracks."
	trait.Effect = "anomalous_boost"
	trait.Magnitude = 1.12
	return trait
}

func loadCasinoState(s *Server, ctx context.Context) {
	if s.casinoRepo != nil {
		if tables, err := s.casinoRepo.ListPokerTables(ctx, 100); err == nil {
			s.pokerMu.Lock()
			for _, table := range tables {
				s.pokerTables[table.ID] = table
			}
			s.pokerMu.Unlock()
		}
	}
	if s.departureRepo != nil {
		for _, stable := range s.stables.ListStables() {
			if stable.OwnerID == "" || stable.OwnerID == "system" {
				continue
			}
			records, err := s.departureRepo.ListDeparturesByOwner(ctx, stable.OwnerID, 200)
			if err != nil {
				continue
			}
			s.departMu.Lock()
			for _, rec := range records {
				s.departures[rec.ID] = rec
			}
			s.departMu.Unlock()
		}
	}
}

func grantReturnLoreTrait(horse *models.Horse) {
	if horse == nil {
		return
	}
	horse.Traits = append(horse.Traits, trainussy.InitTraitPool()[0])
}
