package server

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"sort"
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

var slotSymbolPool = []string{"CHERRY", "OATS", "BELL", "SEVEN", "YOGURT", "SKULL"}

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
	if req.MaxPlayers < 2 || req.MaxPlayers > 4 {
		req.MaxPlayers = 4
	}
	if req.Currency == "" {
		req.Currency = "casino_chips"
	}
	if err := s.withdrawCasinoStake(stable, req.Currency, req.BuyIn); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	table := &models.PokerTable{
		ID:            uuid.New().String(),
		Name:          strings.TrimSpace(req.Name),
		CreatedBy:     claims.UserID,
		StakeCurrency: req.Currency,
		BuyIn:         req.BuyIn,
		MaxPlayers:    req.MaxPlayers,
		Status:        models.PokerTableOpen,
		Pot:           req.BuyIn,
		DeckSeed:      rand.Uint64(),
		Log:           []string{"Table opened. Waiting for more degenerates."},
		Seats: []models.PokerSeat{{
			UserID:       claims.UserID,
			Username:     claims.Username,
			StableID:     stable.ID,
			BuyIn:        req.BuyIn,
			Currency:     req.Currency,
			JoinedAt:     time.Now(),
			LastActionAt: time.Now(),
		}},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if table.Name == "" {
		table.Name = fmt.Sprintf("%s's draw table", claims.Username)
	}
	s.savePokerTable(r.Context(), table)
	s.persistStable(r.Context(), stable)
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
	if err := s.withdrawCasinoStake(stable, table.StakeCurrency, table.BuyIn); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	table.Seats = append(table.Seats, models.PokerSeat{
		UserID:       claims.UserID,
		Username:     claims.Username,
		StableID:     stable.ID,
		BuyIn:        table.BuyIn,
		Currency:     table.StakeCurrency,
		JoinedAt:     time.Now(),
		LastActionAt: time.Now(),
	})
	table.Pot += table.BuyIn
	table.UpdatedAt = time.Now()
	table.Log = append(table.Log, fmt.Sprintf("%s bought in for %d %s.", claims.Username, table.BuyIn, renderCasinoCurrency(table.StakeCurrency)))
	if len(table.Seats) >= 2 {
		s.startPokerTable(table)
	}
	s.savePokerTable(r.Context(), table)
	s.persistStable(r.Context(), stable)
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
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Wager <= 0 {
		writeError(w, http.StatusBadRequest, "wager must be positive")
		return
	}
	stable := s.getStableForUser(claims.UserID)
	if stable == nil {
		writeError(w, http.StatusBadRequest, "you need a stable first")
		return
	}
	if stable.CasinoChips < req.Wager {
		writeError(w, http.StatusBadRequest, "insufficient casino chips")
		return
	}
	stable.CasinoChips -= req.Wager
	symbols := []string{slotSymbolPool[rand.IntN(len(slotSymbolPool))], slotSymbolPool[rand.IntN(len(slotSymbolPool))], slotSymbolPool[rand.IntN(len(slotSymbolPool))]}
	multiplier := slotMultiplier(symbols)
	payout := int64(math.Round(float64(req.Wager) * multiplier))
	stable.CasinoChips += payout
	spin := &models.SlotSpin{
		ID:           uuid.New().String(),
		StableID:     stable.ID,
		UserID:       claims.UserID,
		WagerAmount:  req.Wager,
		PayoutAmount: payout,
		Multiplier:   multiplier,
		Symbols:      symbols,
		Summary:      describeSpin(symbols, multiplier),
		CreatedAt:    time.Now(),
	}
	if s.casinoRepo != nil {
		_ = s.casinoRepo.RecordSlotSpin(r.Context(), spin)
	}
	s.persistStable(r.Context(), stable)
	writeJSON(w, http.StatusOK, spin)
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
	for i, seat := range table.Seats {
		clone.Seats[i] = seat
		if seat.UserID != userID && clone.Status != models.PokerTableSettled {
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
		if table.StakeCurrency == "cummies" {
			winningStable.Cummies += table.Pot
		} else {
			winningStable.CasinoChips += table.Pot
		}
		s.persistStable(context.Background(), winningStable)
	}
	table.UpdatedAt = time.Now()
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
	if len(vals) == 2 && vals[0] == 4 {
		return 700, "four of a kind"
	}
	if len(vals) == 2 && vals[0] == 3 {
		return 650, "full house"
	}
	if sameSuit {
		return 600, "flush"
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
