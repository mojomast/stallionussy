package server

// Rehydration round-trip tests: boot a full server on a temp SQLite database,
// mutate state through the real code paths, then boot a SECOND server on the
// same database file and assert the players resume their exact prior state —
// balances, horses, sessions, challenges, betting escrow, rivalries, market
// history, and training history all intact.

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/repository"
	"github.com/mojomast/stallionussy/internal/repository/postgres"
)

// openTestServerDB opens (or reopens) the SQLite database at path with
// migrations applied.
func openTestServerDB(t *testing.T, path string) *postgres.DB {
	t.Helper()
	db, err := postgres.NewSQLite(path)
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	if err := repository.RunMigrationsFor(db.GetDB(), repository.DialectSQLite); err != nil {
		db.Close()
		t.Fatalf("RunMigrationsFor: %v", err)
	}
	return db
}

// registerTestUser registers a user through the real auth handler and returns
// the issued token and user.
func registerTestUser(t *testing.T, s *Server, username string) (string, *models.User) {
	t.Helper()
	body := `{"username":"` + username + `","password":"password123"}`
	req := httptest.NewRequest("POST", "/api/auth/register", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.authHandler.HandleRegister(rec, req)
	if rec.Code != 201 {
		t.Fatalf("register %s: status %d: %s", username, rec.Code, rec.Body.String())
	}
	var resp struct {
		Token string       `json:"token"`
		User  *models.User `json:"user"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	return resp.Token, resp.User
}

func TestRehydrationRoundTrip(t *testing.T) {
	t.Setenv("JWT_SECRET", "rehydration-test-secret")
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "rehydrate.db")

	// ------------------------------------------------------------------
	// Phase 1: first server lifetime — create state through real paths.
	// ------------------------------------------------------------------
	db1 := openTestServerDB(t, dbPath)
	s1 := NewServer(db1)

	tokenA, userA := registerTestUser(t, s1, "rehydrussy_a")
	_, userB := registerTestUser(t, s1, "rehydrussy_b")

	stableA := s1.getStableForUser(userA.ID)
	stableB := s1.getStableForUser(userB.ID)
	if stableA == nil || stableB == nil {
		t.Fatal("starter stables missing")
	}
	if len(stableA.Horses) == 0 || len(stableB.Horses) == 0 {
		t.Fatal("starter horses missing")
	}

	// Mutate a horse through the live registry pointer and persist.
	horseA, err := s1.stables.GetHorse(stableA.Horses[0].ID)
	if err != nil {
		t.Fatalf("GetHorse: %v", err)
	}
	bred := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	horseA.CurrentFitness = 0.66
	horseA.Fatigue = 33
	horseA.ELO = 1337
	horseA.LastBredAt = bred
	horseA.RetiredChampion = true
	s1.persistHorse(ctx, horseA)

	// Train another horse for real (mutates fitness/fatigue/specialty and
	// records a session).
	horseA2, _ := s1.stables.GetHorse(stableA.Horses[1].ID)
	session, err := s1.trainer.Train(horseA2, models.WorkoutSprint)
	if err != nil {
		t.Fatalf("Train: %v", err)
	}
	s1.persistTrainingSession(ctx, session)
	s1.persistHorse(ctx, horseA2)

	// Adjust the balance and persist.
	stableA.Cummies = 7777
	s1.persistStable(ctx, stableA)

	// Create a pending challenge A -> B (real path).
	challenge, errMsg := s1.createChallenge(userA.ID, userA.Username, userB.Username, horseA.ID, 250)
	if errMsg != "" {
		t.Fatalf("createChallenge: %s", errMsg)
	}

	// Open an exhibition betting pool and place a real (escrowed) bet from B.
	horseB, _ := s1.stables.GetHorse(stableB.Horses[0].ID)
	pool := s1.openBettingPool("rehydrate-race", []*models.Horse{horseA, horseB}, models.PoolKindExhibition)
	if pool == nil {
		t.Fatal("openBettingPool returned nil")
	}
	balanceBeforeBet := stableB.Cummies
	if _, errMsg := s1.placeBet("rehydrate-race", userB.ID, userB.Username, horseB.ID, 100); errMsg != "" {
		t.Fatalf("placeBet: %s", errMsg)
	}
	if stableB.Cummies != balanceBeforeBet-100 {
		t.Fatalf("bet did not escrow: %d", stableB.Cummies)
	}

	// Record a rivalry win.
	s1.rivalryMu.Lock()
	s1.rivalries[horseA.ID] = map[string]int{horseB.ID: 3}
	s1.rivalryMu.Unlock()
	for i := 0; i < 3; i++ {
		s1.persistRivalryWin(ctx, horseA.ID, horseB.ID)
	}

	// Record a market transaction.
	tx := &models.MarketTransaction{
		ID: "tx-rehydrate", ListingID: "listing-x",
		BuyerID: userB.ID, SellerID: userA.ID,
		Price: 500, BurnAmount: 10, SellerPayout: 490,
		FoalID: "foal-x", CreatedAt: time.Now().UTC(),
	}
	s1.market.ImportTransaction(tx)
	s1.persistMarketTransaction(ctx, tx)

	// Bump the jackpot.
	s1.jackpotMu.Lock()
	s1.jackpotPool = 4242
	s1.jackpotMu.Unlock()
	s1.persistJackpotState(ctx)

	// Capture expectations, then simulate the restart.
	wantBalanceA := stableA.Cummies
	wantBalanceB := stableB.Cummies
	wantHorseA2Fitness := horseA2.CurrentFitness
	wantHorseA2Specialty := horseA2.SpecialtyOf("SPD")
	if err := db1.Close(); err != nil {
		t.Fatalf("close db1: %v", err)
	}

	// ------------------------------------------------------------------
	// Phase 2: second server lifetime — everything must come back.
	// ------------------------------------------------------------------
	db2 := openTestServerDB(t, dbPath)
	defer db2.Close()
	s2 := NewServer(db2)

	// Player account + balance.
	stableA2 := s2.getStableForUser(userA.ID)
	stableB2 := s2.getStableForUser(userB.ID)
	if stableA2 == nil || stableB2 == nil {
		t.Fatal("stables lost on restart")
	}
	if stableA2.Cummies != wantBalanceA {
		t.Errorf("stable A balance = %d, want %d", stableA2.Cummies, wantBalanceA)
	}
	if stableB2.Cummies != wantBalanceB {
		t.Errorf("stable B balance = %d, want %d (bet escrow must persist)", stableB2.Cummies, wantBalanceB)
	}

	// Horse state.
	gotHorse, err := s2.stables.GetHorse(horseA.ID)
	if err != nil {
		t.Fatalf("horse lost on restart: %v", err)
	}
	if gotHorse.CurrentFitness != 0.66 || gotHorse.Fatigue != 33 || gotHorse.ELO != 1337 {
		t.Errorf("horse stats: fitness=%v fatigue=%v elo=%v", gotHorse.CurrentFitness, gotHorse.Fatigue, gotHorse.ELO)
	}
	if !gotHorse.RetiredChampion {
		t.Error("RetiredChampion lost")
	}
	if !gotHorse.LastBredAt.Equal(bred) {
		t.Errorf("LastBredAt = %v, want %v", gotHorse.LastBredAt, bred)
	}
	gotHorse2, err := s2.stables.GetHorse(horseA2.ID)
	if err != nil {
		t.Fatalf("trained horse lost: %v", err)
	}
	if gotHorse2.CurrentFitness != wantHorseA2Fitness {
		t.Errorf("trained fitness = %v, want %v", gotHorse2.CurrentFitness, wantHorseA2Fitness)
	}
	if gotHorse2.SpecialtyOf("SPD") != wantHorseA2Specialty {
		t.Errorf("SPD specialty = %v, want %v", gotHorse2.SpecialtyOf("SPD"), wantHorseA2Specialty)
	}

	// Session: the token issued before the restart must still authenticate.
	if err := s2.auth.ValidateSession(ctx, tokenA); err != nil {
		t.Errorf("session did not survive restart: %v", err)
	}
	if claims, err := s2.auth.ValidateToken(tokenA); err != nil || claims.UserID != userA.ID {
		t.Errorf("JWT no longer valid after restart: %v", err)
	}

	// Challenge: still pending.
	s2.challengeMu.RLock()
	gotChallenge := s2.challenges[challenge.ID]
	s2.challengeMu.RUnlock()
	if gotChallenge == nil {
		t.Fatal("pending challenge lost on restart")
	}
	if gotChallenge.Status != models.ChallengeStatusPending || gotChallenge.Wager != 250 {
		t.Errorf("challenge = %+v", gotChallenge)
	}
	if gotChallenge.ChallengerHorse != horseA.ID || gotChallenge.DefenderID != userB.ID {
		t.Errorf("challenge participants mangled: %+v", gotChallenge)
	}

	// Betting pool: escrowed bet intact.
	s2.bettingMu.RLock()
	gotPool := s2.bettingPools["rehydrate-race"]
	s2.bettingMu.RUnlock()
	if gotPool == nil {
		t.Fatal("betting pool lost on restart")
	}
	if gotPool.TotalPool != 100 || len(gotPool.Bets) != 1 {
		t.Errorf("pool = total %d, %d bets; want 100/1", gotPool.TotalPool, len(gotPool.Bets))
	}
	if gotPool.Bets[0].UserID != userB.ID || gotPool.Bets[0].Amount != 100 {
		t.Errorf("bet mangled: %+v", gotPool.Bets[0])
	}
	if gotPool.Kind != models.PoolKindExhibition {
		t.Errorf("pool kind = %q", gotPool.Kind)
	}

	// Rivalries.
	s2.rivalryMu.RLock()
	gotRivalry := s2.rivalries[horseA.ID][horseB.ID]
	s2.rivalryMu.RUnlock()
	if gotRivalry != 3 {
		t.Errorf("rivalry count = %d, want 3", gotRivalry)
	}

	// Market transaction history + burn total.
	history := s2.market.GetTransactionHistory()
	found := false
	for _, htx := range history {
		if htx.ID == "tx-rehydrate" && htx.BurnAmount == 10 && htx.SellerPayout == 490 {
			found = true
		}
	}
	if !found {
		t.Errorf("market transaction history lost: %d entries", len(history))
	}
	if s2.market.GetTotalBurned() < 10 {
		t.Errorf("burn total = %d, want >= 10", s2.market.GetTotalBurned())
	}

	// Training history.
	trainHist := s2.trainer.GetTrainingHistory(horseA2.ID)
	if len(trainHist) != 1 || trainHist[0].WorkoutType != models.WorkoutSprint {
		t.Errorf("training history = %+v", trainHist)
	}

	// Jackpot.
	s2.jackpotMu.Lock()
	gotJackpot := s2.jackpotPool
	s2.jackpotMu.Unlock()
	if gotJackpot != 4242 {
		t.Errorf("jackpot = %d, want 4242", gotJackpot)
	}
}

// TestRehydrationExpiredSessionRejected: a session that expired while the
// server was down must NOT come back to life.
func TestRehydrationExpiredSessionRejected(t *testing.T) {
	t.Setenv("JWT_SECRET", "rehydration-test-secret")
	// A TTL so short the session is dead by the time the second server
	// validates it. The JWT itself also expires, but ValidateSession is
	// checked directly here to prove the session store enforces expiry.
	t.Setenv("STALLION_SESSION_TTL", "1ms")
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "expire.db")

	db1 := openTestServerDB(t, dbPath)
	s1 := NewServer(db1)
	token, _ := registerTestUser(t, s1, "expirussy")
	db1.Close()

	time.Sleep(5 * time.Millisecond)

	db2 := openTestServerDB(t, dbPath)
	defer db2.Close()
	s2 := NewServer(db2)
	if err := s2.auth.ValidateSession(ctx, token); err == nil {
		t.Fatal("expired session validated after restart")
	}
}
