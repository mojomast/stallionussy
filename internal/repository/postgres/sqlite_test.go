package postgres

// The first real DB-backed tests in the repo: they exercise the shared SQL
// repository implementation against SQLite (offline mode) using the pure-Go
// modernc.org/sqlite driver, so they run anywhere — including CI — with no
// database server.

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mojomast/stallionussy/internal/genussy"
	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/repository"
)

// newSQLiteDB opens a fresh temp-file SQLite database with the full schema
// applied. The file lives in t.TempDir() so it is cleaned up automatically.
func newSQLiteDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stallionussy_test.db")
	db, err := NewSQLite(path)
	if err != nil {
		t.Fatalf("NewSQLite(%q): %v", path, err)
	}
	t.Cleanup(func() { db.Close() })
	if err := repository.RunMigrationsFor(db.GetDB(), repository.DialectSQLite); err != nil {
		t.Fatalf("RunMigrationsFor(sqlite): %v", err)
	}
	return db
}

func TestSQLiteDialect(t *testing.T) {
	db := newSQLiteDB(t)
	if db.Dialect() != repository.DialectSQLite {
		t.Fatalf("Dialect() = %q, want %q", db.Dialect(), repository.DialectSQLite)
	}
}

func TestSQLiteMigrationsIdempotent(t *testing.T) {
	db := newSQLiteDB(t)
	// Running the migrations a second time on the same database must be a
	// no-op (every statement is IF NOT EXISTS).
	if err := repository.RunMigrationsFor(db.GetDB(), repository.DialectSQLite); err != nil {
		t.Fatalf("second RunMigrationsFor: %v", err)
	}
}

func TestSQLiteUserRepoCRUD(t *testing.T) {
	db := newSQLiteDB(t)
	repo := NewUserRepo(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	user := &models.User{
		ID:           "user-1",
		Username:     "GeoffRussy",
		PasswordHash: "$2a$10$fakehash",
		DisplayName:  "Geoff of the USSY",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := repo.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	got, err := repo.GetUserByID(ctx, "user-1")
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if got.Username != user.Username || got.PasswordHash != user.PasswordHash || got.DisplayName != user.DisplayName {
		t.Fatalf("GetUserByID mismatch: got %+v", got)
	}
	if !got.CreatedAt.Equal(now) {
		t.Fatalf("CreatedAt round-trip: got %v, want %v", got.CreatedAt, now)
	}

	// Case-insensitive username lookup (LOWER() expression index path).
	got, err = repo.GetUserByUsername(ctx, "geoffrussy")
	if err != nil || got == nil {
		t.Fatalf("GetUserByUsername(lowercase): %v, %v", got, err)
	}

	// Token version lifecycle.
	v, err := repo.GetTokenVersion(ctx, "user-1")
	if err != nil || v != 0 {
		t.Fatalf("GetTokenVersion: %d, %v", v, err)
	}
	if err := repo.IncrementTokenVersion(ctx, "user-1"); err != nil {
		t.Fatalf("IncrementTokenVersion: %v", err)
	}
	if v, _ = repo.GetTokenVersion(ctx, "user-1"); v != 1 {
		t.Fatalf("token version after increment = %d, want 1", v)
	}

	// Update.
	user.DisplayName = "Geoffrussy the Patient"
	user.UpdatedAt = time.Now().UTC()
	if err := repo.UpdateUser(ctx, user); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	got, _ = repo.GetUserByID(ctx, "user-1")
	if got.DisplayName != "Geoffrussy the Patient" {
		t.Fatalf("UpdateUser did not persist: %+v", got)
	}
}

func TestSQLiteStableRepoAndBalanceFloor(t *testing.T) {
	db := newSQLiteDB(t)
	repo := NewStableRepo(db)
	ctx := context.Background()

	stable := &models.Stable{
		ID:        "stable-1",
		Name:      "Yogurt Meadows",
		OwnerID:   "user-1",
		Cummies:   5000,
		CreatedAt: time.Now().UTC(),
	}
	if err := repo.CreateStable(ctx, stable); err != nil {
		t.Fatalf("CreateStable: %v", err)
	}

	got, err := repo.GetStableByOwner(ctx, "user-1")
	if err != nil || got == nil {
		t.Fatalf("GetStableByOwner: %v, %v", got, err)
	}
	if got.Cummies != 5000 {
		t.Fatalf("Cummies = %d, want 5000", got.Cummies)
	}

	stable.Cummies = 4200
	stable.CasinoChips = 40
	if err := repo.UpdateStable(ctx, stable); err != nil {
		t.Fatalf("UpdateStable: %v", err)
	}
	got, _ = repo.GetStable(ctx, "stable-1")
	if got.Cummies != 4200 || got.CasinoChips != 40 {
		t.Fatalf("balances = %d/%d, want 4200/40", got.Cummies, got.CasinoChips)
	}

	// The C-9 balance floor must hold in SQLite too: a negative balance is
	// rejected at the database layer via the inline CHECK constraint.
	stable.Cummies = -1
	if err := repo.UpdateStable(ctx, stable); err == nil {
		t.Fatal("UpdateStable with negative cummies succeeded; CHECK constraint missing")
	}
}

func TestSQLiteHorseRoundTrip(t *testing.T) {
	db := newSQLiteDB(t)
	repo := NewHorseRepo(db)
	ctx := context.Background()

	bred := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	horse := &models.Horse{
		ID:              "horse-1",
		Name:            "Sir Gallops-A-Lot",
		Genome:          genussy.RandomGenome(),
		SireID:          "sire-1",
		MareID:          "mare-1",
		Generation:      3,
		Age:             4,
		FitnessCeiling:  0.87,
		CurrentFitness:  0.55,
		Wins:            2,
		Losses:          5,
		Races:           7,
		ELO:             1234.5,
		OwnerID:         "user-1",
		IsLegendary:     true,
		LotNumber:       7,
		CreatedAt:       time.Now().UTC().Truncate(time.Second),
		Lore:            "Once outran a sentient sourdough starter.",
		Traits:          []models.Trait{{ID: "mud_lover", Name: "Mud Lover", Effect: "mud_lover", Magnitude: 1.05, Rarity: "rare"}},
		Fatigue:         12.5,
		Retired:         false,
		RetiredChampion: true,
		TotalEarnings:   9000,
		TrainingXP:      42.5,
		PeakELO:         1300,
		LastBredAt:      bred,
		Injury:          &models.Injury{Type: "Pulled Hammy", Severity: models.SeverityModerate, RacesLeft: 3},
		TrainingSpecialty: map[string]float64{
			"SPD": 0.02,
			"STM": 0.045,
		},
	}
	if err := repo.CreateHorse(ctx, horse); err != nil {
		t.Fatalf("CreateHorse: %v", err)
	}

	got, err := repo.GetHorse(ctx, "horse-1")
	if err != nil {
		t.Fatalf("GetHorse: %v", err)
	}

	// The JSON columns (genome, traits, injury, training specialty) and the
	// persistence-critical scalar fields must all round-trip losslessly.
	if !reflect.DeepEqual(got.Genome, horse.Genome) {
		t.Errorf("Genome mismatch: got %+v want %+v", got.Genome, horse.Genome)
	}
	if len(got.Traits) != 1 || got.Traits[0] != horse.Traits[0] {
		t.Errorf("Traits mismatch: got %+v", got.Traits)
	}
	if got.Injury == nil || got.Injury.Type != "Pulled Hammy" {
		t.Errorf("Injury mismatch: got %+v", got.Injury)
	}
	if got.SpecialtyOf("STM") != 0.045 || got.SpecialtyOf("SPD") != 0.02 {
		t.Errorf("TrainingSpecialty mismatch: got %+v", got.TrainingSpecialty)
	}
	if !got.RetiredChampion {
		t.Error("RetiredChampion flag lost")
	}
	if !got.LastBredAt.Equal(bred) {
		t.Errorf("LastBredAt = %v, want %v", got.LastBredAt, bred)
	}
	if got.ELO != 1234.5 || got.FitnessCeiling != 0.87 || got.TotalEarnings != 9000 {
		t.Errorf("scalar mismatch: elo=%v ceiling=%v earnings=%v", got.ELO, got.FitnessCeiling, got.TotalEarnings)
	}

	// Update + list-by-stable (owner_id keyed, per the C-4 convention).
	got.Wins = 3
	got.Fatigue = 40
	if err := repo.UpdateHorse(ctx, got); err != nil {
		t.Fatalf("UpdateHorse: %v", err)
	}
	list, err := repo.ListHorsesByStable(ctx, "user-1")
	if err != nil || len(list) != 1 {
		t.Fatalf("ListHorsesByStable: %d horses, err %v", len(list), err)
	}
	if list[0].Wins != 3 || list[0].Fatigue != 40 {
		t.Fatalf("UpdateHorse did not persist: %+v", list[0])
	}
}

func TestSQLiteRaceResultDedupe(t *testing.T) {
	db := newSQLiteDB(t)
	repo := NewRaceResultRepo(db)
	ctx := context.Background()

	res := &models.RaceResult{
		RaceID:      "race-1",
		HorseID:     "horse-1",
		HorseName:   "Sir Gallops-A-Lot",
		TrackType:   models.TrackType("Mudussy Downs"),
		Distance:    1200,
		FinishPlace: 1,
		TotalHorses: 6,
		FinalTime:   90 * time.Second,
		ELOBefore:   1200,
		ELOAfter:    1216,
		Earnings:    500,
		Weather:     "Ominous Yogurt Fog",
		CreatedAt:   time.Now().UTC(),
	}
	if err := repo.RecordResult(ctx, res); err != nil {
		t.Fatalf("RecordResult: %v", err)
	}
	// Re-recording the same (race, horse) pair must be a silent no-op
	// (M-13: ON CONFLICT DO NOTHING on the unique index).
	if err := repo.RecordResult(ctx, res); err != nil {
		t.Fatalf("duplicate RecordResult should be a no-op, got: %v", err)
	}

	results, err := repo.GetRaceResults(ctx, "race-1")
	if err != nil {
		t.Fatalf("GetRaceResults: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 deduped result, got %d", len(results))
	}
	if results[0].Earnings != 500 || results[0].FinishPlace != 1 {
		t.Fatalf("result round-trip mismatch: %+v", results[0])
	}

	history, err := repo.GetHorseHistory(ctx, "horse-1")
	if err != nil || len(history) != 1 {
		t.Fatalf("GetHorseHistory: %d results, err %v", len(history), err)
	}
}

func TestSQLiteMarketListingUseLimits(t *testing.T) {
	db := newSQLiteDB(t)
	repo := NewMarketRepo(db)
	ctx := context.Background()

	listing := &models.StudListing{
		ID:          "listing-1",
		HorseID:     "horse-1",
		HorseName:   "Sir Gallops-A-Lot",
		OwnerID:     "user-1",
		Price:       750,
		Pedigree:    "Son of Nobody, Grandson of Chaos",
		SapphoScore: 8.5,
		Active:      true,
		TimesUsed:   0,
		MaxUses:     3,
		CreatedAt:   time.Now().UTC(),
	}
	if err := repo.CreateListing(ctx, listing); err != nil {
		t.Fatalf("CreateListing: %v", err)
	}

	active, err := repo.ListActiveListings(ctx)
	if err != nil || len(active) != 1 {
		t.Fatalf("ListActiveListings: %d, err %v", len(active), err)
	}
	if active[0].MaxUses != 3 {
		t.Fatalf("MaxUses = %d, want 3 (H-8 persistence)", active[0].MaxUses)
	}

	// Exhaust the stud and deactivate — must survive a round-trip.
	listing.TimesUsed = 3
	listing.Active = false
	if err := repo.UpdateListing(ctx, listing); err != nil {
		t.Fatalf("UpdateListing: %v", err)
	}
	got, err := repo.GetListing(ctx, "listing-1")
	if err != nil {
		t.Fatalf("GetListing: %v", err)
	}
	if got.TimesUsed != 3 || got.Active {
		t.Fatalf("use limits lost: times_used=%d active=%v", got.TimesUsed, got.Active)
	}
	if active, _ = repo.ListActiveListings(ctx); len(active) != 0 {
		t.Fatalf("deactivated listing still active: %d", len(active))
	}
}

func TestSQLiteJackpotUpsert(t *testing.T) {
	db := newSQLiteDB(t)
	repo := NewCasinoRepo(db)
	ctx := context.Background()

	// Empty table reads as zero state, not an error.
	pool, winner, amount, err := repo.GetJackpotState(ctx)
	if err != nil || pool != 0 || winner != "" || amount != 0 {
		t.Fatalf("empty jackpot state: %d/%q/%d, err %v", pool, winner, amount, err)
	}

	if err := repo.SaveJackpotState(ctx, 500, "", 0); err != nil {
		t.Fatalf("SaveJackpotState (insert): %v", err)
	}
	if err := repo.SaveJackpotState(ctx, 1750, "mojo", 1200); err != nil {
		t.Fatalf("SaveJackpotState (upsert): %v", err)
	}

	pool, winner, amount, err = repo.GetJackpotState(ctx)
	if err != nil {
		t.Fatalf("GetJackpotState: %v", err)
	}
	if pool != 1750 || winner != "mojo" || amount != 1200 {
		t.Fatalf("jackpot state = %d/%q/%d, want 1750/mojo/1200", pool, winner, amount)
	}
}

func TestSQLiteAcceptTradeAtomically(t *testing.T) {
	db := newSQLiteDB(t)
	users := NewUserRepo(db)
	stables := NewStableRepo(db)
	horses := NewHorseRepo(db)
	trades := NewTradeRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()

	// Seller (user-a / stable-a) owns the horse; buyer (user-b / stable-b)
	// pays 1000 cummies.
	for _, u := range []string{"user-a", "user-b"} {
		if err := users.CreateUser(ctx, &models.User{ID: u, Username: u, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("CreateUser(%s): %v", u, err)
		}
	}
	if err := stables.CreateStable(ctx, &models.Stable{ID: "stable-a", Name: "A", OwnerID: "user-a", Cummies: 100, CreatedAt: now}); err != nil {
		t.Fatalf("CreateStable(a): %v", err)
	}
	if err := stables.CreateStable(ctx, &models.Stable{ID: "stable-b", Name: "B", OwnerID: "user-b", Cummies: 5000, CreatedAt: now}); err != nil {
		t.Fatalf("CreateStable(b): %v", err)
	}
	if err := horses.CreateHorse(ctx, &models.Horse{ID: "horse-t", Name: "Tradeussy", OwnerID: "user-a", Genome: genussy.RandomGenome(), CreatedAt: now}); err != nil {
		t.Fatalf("CreateHorse: %v", err)
	}
	offer := &models.TradeOffer{
		ID: "trade-1", HorseID: "horse-t", HorseName: "Tradeussy",
		FromStableID: "stable-a", ToStableID: "stable-b",
		Price: 1000, Status: "Pending", CreatedAt: now, UpdatedAt: now,
	}
	if err := trades.CreateTrade(ctx, offer); err != nil {
		t.Fatalf("CreateTrade: %v", err)
	}

	offer.Status = "Accepted"
	offer.UpdatedAt = time.Now().UTC()
	if err := db.AcceptTradeAtomically(ctx, offer); err != nil {
		t.Fatalf("AcceptTradeAtomically: %v", err)
	}

	// Money moved, horse moved, status flipped — atomically.
	a, _ := stables.GetStable(ctx, "stable-a")
	b, _ := stables.GetStable(ctx, "stable-b")
	if a.Cummies != 1100 || b.Cummies != 4000 {
		t.Fatalf("balances after trade: seller=%d buyer=%d, want 1100/4000", a.Cummies, b.Cummies)
	}
	h, _ := horses.GetHorse(ctx, "horse-t")
	if h.OwnerID != "user-b" {
		t.Fatalf("horse owner = %q, want user-b", h.OwnerID)
	}

	// A second acceptance must fail on the status guard (H-2) without
	// moving any more money.
	if err := db.AcceptTradeAtomically(ctx, offer); err == nil || !strings.Contains(err.Error(), "no longer pending") {
		t.Fatalf("double-accept should fail on the pending guard, got: %v", err)
	}
	b, _ = stables.GetStable(ctx, "stable-b")
	if b.Cummies != 4000 {
		t.Fatalf("double-accept moved money: buyer=%d", b.Cummies)
	}
}
