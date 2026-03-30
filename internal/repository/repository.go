// Package repository defines persistence interfaces for StallionUSSY.
//
// Each interface covers a single aggregate root (User, Stable, Horse, etc.)
// and is implemented by a concrete PostgreSQL layer (see the postgres sub-
// package). In-memory implementations can also be provided for tests.
package repository

import (
	"context"
	"time"

	"github.com/mojomast/stallionussy/internal/models"
)

// ---------------------------------------------------------------------------
// UserRepository — player accounts
// ---------------------------------------------------------------------------

// UserRepository handles CRUD operations for player accounts.
type UserRepository interface {
	// CreateUser persists a new user. The caller must set ID and timestamps.
	CreateUser(ctx context.Context, user *models.User) error

	// GetUserByID retrieves a user by their unique ID.
	GetUserByID(ctx context.Context, id string) (*models.User, error)

	// GetUserByUsername retrieves a user by their unique username (case-insensitive).
	GetUserByUsername(ctx context.Context, username string) (*models.User, error)

	// UpdateUser saves changes to an existing user record.
	UpdateUser(ctx context.Context, user *models.User) error

	// GetTokenVersion returns the current token_version for a user.
	// Used by auth middleware to reject JWTs issued before a password change.
	GetTokenVersion(ctx context.Context, userID string) (int, error)

	// IncrementTokenVersion bumps the user's token_version by 1, invalidating
	// all previously issued JWTs. Call this on password change.
	IncrementTokenVersion(ctx context.Context, userID string) error
}

// ---------------------------------------------------------------------------
// StableRepository — stables and their metadata
// ---------------------------------------------------------------------------

// StableRepository handles persistence for stables (horse collections).
type StableRepository interface {
	// CreateStable persists a new stable.
	CreateStable(ctx context.Context, stable *models.Stable) error

	// GetStable retrieves a stable by ID (without its horses slice populated).
	GetStable(ctx context.Context, id string) (*models.Stable, error)

	// GetStableByOwner retrieves the stable belonging to a given owner.
	GetStableByOwner(ctx context.Context, ownerID string) (*models.Stable, error)

	// ListStables returns all stables.
	ListStables(ctx context.Context) ([]*models.Stable, error)

	// UpdateStable saves changes to an existing stable record.
	UpdateStable(ctx context.Context, stable *models.Stable) error

	// DeleteStable removes a stable and cascades to its horses.
	DeleteStable(ctx context.Context, id string) error
}

// ---------------------------------------------------------------------------
// HorseRepository — individual horses
// ---------------------------------------------------------------------------

// HorseRepository handles persistence for individual horses.
type HorseRepository interface {
	// CreateHorse persists a new horse.
	CreateHorse(ctx context.Context, horse *models.Horse) error

	// GetHorse retrieves a horse by ID.
	GetHorse(ctx context.Context, id string) (*models.Horse, error)

	// ListHorsesByStable returns all horses belonging to a given stable.
	ListHorsesByStable(ctx context.Context, stableID string) ([]*models.Horse, error)

	// ListAllHorses returns every horse in the database.
	ListAllHorses(ctx context.Context) ([]*models.Horse, error)

	// UpdateHorse saves changes to an existing horse record.
	UpdateHorse(ctx context.Context, horse *models.Horse) error

	// DeleteHorse removes a horse by ID.
	DeleteHorse(ctx context.Context, id string) error

	// MoveHorse atomically transfers a horse from one stable to another.
	MoveHorse(ctx context.Context, horseID, fromStableID, toStableID string) error
}

// ---------------------------------------------------------------------------
// RaceResultRepository — completed race history
// ---------------------------------------------------------------------------

// RaceResultRepository handles persistence for race results / history.
type RaceResultRepository interface {
	// RecordResult persists a single horse's result from a completed race.
	RecordResult(ctx context.Context, result *models.RaceResult) error

	// GetHorseHistory returns all race results for a given horse, newest first.
	GetHorseHistory(ctx context.Context, horseID string) ([]*models.RaceResult, error)

	// GetRaceResults returns all results for a given race ID.
	GetRaceResults(ctx context.Context, raceID string) ([]*models.RaceResult, error)

	// GetRecentResults returns the most recent race results across all horses.
	GetRecentResults(ctx context.Context, limit int) ([]*models.RaceResult, error)
}

// ---------------------------------------------------------------------------
// MarketRepository — stud market listings
// ---------------------------------------------------------------------------

// MarketRepository handles persistence for stud market listings.
type MarketRepository interface {
	// CreateListing persists a new stud market listing.
	CreateListing(ctx context.Context, listing *models.StudListing) error

	// GetListing retrieves a listing by ID.
	GetListing(ctx context.Context, id string) (*models.StudListing, error)

	// ListActiveListings returns all currently active listings.
	ListActiveListings(ctx context.Context) ([]*models.StudListing, error)

	// UpdateListing saves changes to an existing listing.
	UpdateListing(ctx context.Context, listing *models.StudListing) error

	// DeleteListing removes a listing by ID.
	DeleteListing(ctx context.Context, id string) error
}

// ---------------------------------------------------------------------------
// TournamentRepository — tournaments
// ---------------------------------------------------------------------------

// TournamentRepository handles persistence for tournaments.
type TournamentRepository interface {
	// CreateTournament persists a new tournament.
	CreateTournament(ctx context.Context, tournament *models.Tournament) error

	// GetTournament retrieves a tournament by ID.
	GetTournament(ctx context.Context, id string) (*models.Tournament, error)

	// ListTournaments returns all tournaments.
	ListTournaments(ctx context.Context) ([]*models.Tournament, error)

	// UpdateTournament saves changes to an existing tournament.
	UpdateTournament(ctx context.Context, tournament *models.Tournament) error
}

// ---------------------------------------------------------------------------
// TradeRepository — trade offers between stables
// ---------------------------------------------------------------------------

// TradeRepository handles persistence for trade offers.
type TradeRepository interface {
	// CreateTrade persists a new trade offer.
	CreateTrade(ctx context.Context, trade *models.TradeOffer) error

	// GetTrade retrieves a trade offer by ID.
	GetTrade(ctx context.Context, id string) (*models.TradeOffer, error)

	// ListTradesByStable returns all trades involving a given stable
	// (either as sender or receiver).
	ListTradesByStable(ctx context.Context, stableID string) ([]*models.TradeOffer, error)

	// ListAllTrades returns all trade offers in the database.
	ListAllTrades(ctx context.Context) ([]*models.TradeOffer, error)

	// UpdateTrade saves changes to an existing trade offer (e.g. status change).
	UpdateTrade(ctx context.Context, trade *models.TradeOffer) error
}

// ---------------------------------------------------------------------------
// AchievementRepository — achievement tracking per stable
// ---------------------------------------------------------------------------

// AchievementRepository handles persistence for achievements.
type AchievementRepository interface {
	// AddAchievement grants an achievement to a stable.
	AddAchievement(ctx context.Context, stableID string, achievement *models.Achievement) error

	// GetAchievements returns all achievements unlocked by a stable.
	GetAchievements(ctx context.Context, stableID string) ([]*models.Achievement, error)

	// HasAchievement checks whether a stable has already unlocked an achievement.
	HasAchievement(ctx context.Context, stableID, achievementID string) (bool, error)
}

// ---------------------------------------------------------------------------
// TrainingSessionRepository — training session history
// ---------------------------------------------------------------------------

// TrainingSessionRepository handles persistence for training sessions.
type TrainingSessionRepository interface {
	// SaveSession persists a single training session.
	SaveSession(ctx context.Context, session *models.TrainingSession) error

	// GetSessionsByHorse returns all training sessions for a given horse,
	// newest first.
	GetSessionsByHorse(ctx context.Context, horseID string) ([]*models.TrainingSession, error)

	// GetRecentSessions returns the most recent training sessions across
	// all horses, newest first.
	GetRecentSessions(ctx context.Context, limit int) ([]*models.TrainingSession, error)
}

// ---------------------------------------------------------------------------
// PlayerProgressRepository — engagement/progression state
// ---------------------------------------------------------------------------

// PlayerProgressRepository handles persistence for player progression state.
type PlayerProgressRepository interface {
	// CreateProgress persists a new player progress record.
	CreateProgress(ctx context.Context, progress *models.PlayerProgress) error

	// GetProgress retrieves a player's progress by user ID.
	GetProgress(ctx context.Context, userID string) (*models.PlayerProgress, error)

	// ListProgress returns all player progress records.
	ListProgress(ctx context.Context) ([]*models.PlayerProgress, error)

	// UpdateProgress saves changes to an existing player progress record.
	UpdateProgress(ctx context.Context, progress *models.PlayerProgress) error
}

// ---------------------------------------------------------------------------
// SeasonRepository — seasonal competition state
// ---------------------------------------------------------------------------

// SeasonRepository handles persistence for seasons.
type SeasonRepository interface {
	// CreateSeason persists a new season.
	CreateSeason(ctx context.Context, season *models.Season) error

	// GetCurrentSeason retrieves the active season.
	GetCurrentSeason(ctx context.Context) (*models.Season, error)

	// ListSeasons returns all seasons, oldest first.
	ListSeasons(ctx context.Context) ([]*models.Season, error)

	// UpdateSeason saves changes to an existing season.
	UpdateSeason(ctx context.Context, season *models.Season) error
}

// ---------------------------------------------------------------------------
// MarketTransactionRepository — market transaction history
// ---------------------------------------------------------------------------

// MarketTransactionRepository handles persistence for stud market transactions.
type MarketTransactionRepository interface {
	// SaveTransaction persists a completed market transaction.
	SaveTransaction(ctx context.Context, tx *models.MarketTransaction) error

	// GetTransactionsByBuyer returns all transactions where the given user
	// was the buyer, newest first.
	GetTransactionsByBuyer(ctx context.Context, buyerID string) ([]*models.MarketTransaction, error)

	// GetRecentTransactions returns the most recent market transactions,
	// newest first.
	GetRecentTransactions(ctx context.Context, limit int) ([]*models.MarketTransaction, error)
}

// ---------------------------------------------------------------------------
// AuctionRepository — live horse auctions
// ---------------------------------------------------------------------------

// AuctionRepository handles persistence for live horse auctions.
type AuctionRepository interface {
	// CreateAuction persists a new auction.
	CreateAuction(ctx context.Context, auction *models.Auction) error

	// GetAuction retrieves an auction by ID.
	GetAuction(ctx context.Context, id string) (*models.Auction, error)

	// ListActiveAuctions returns all auctions with status "open" or "ending".
	ListActiveAuctions(ctx context.Context) ([]*models.Auction, error)

	// ListAuctionsByUser returns all auctions created by or bid on by a user.
	ListAuctionsByUser(ctx context.Context, userID string) ([]*models.Auction, error)

	// UpdateAuction saves changes to an existing auction.
	UpdateAuction(ctx context.Context, auction *models.Auction) error
}

// ---------------------------------------------------------------------------
// AllianceRepository — stable alliances / guilds
// ---------------------------------------------------------------------------

// AllianceRepository handles persistence for stable alliances (guilds).
type AllianceRepository interface {
	// CreateAlliance persists a new alliance.
	CreateAlliance(ctx context.Context, alliance *models.Alliance) error

	// GetAlliance retrieves an alliance by ID, including its members.
	GetAlliance(ctx context.Context, id string) (*models.Alliance, error)

	// ListAlliances returns all alliances.
	ListAlliances(ctx context.Context) ([]*models.Alliance, error)

	// UpdateAlliance saves changes to an existing alliance (name, tag, motto, treasury).
	UpdateAlliance(ctx context.Context, alliance *models.Alliance) error

	// DeleteAlliance removes an alliance and cascades to its members.
	DeleteAlliance(ctx context.Context, id string) error

	// AddMember adds a member to an alliance.
	AddMember(ctx context.Context, member *models.AllianceMember) error

	// RemoveMember removes a member from an alliance.
	RemoveMember(ctx context.Context, allianceID, userID string) error

	// GetMember retrieves a specific member record.
	GetMember(ctx context.Context, allianceID, userID string) (*models.AllianceMember, error)

	// GetMemberByUser finds which alliance a user belongs to (if any).
	GetMemberByUser(ctx context.Context, userID string) (*models.AllianceMember, error)

	// ListMembers returns all members of a given alliance.
	ListMembers(ctx context.Context, allianceID string) ([]*models.AllianceMember, error)

	// UpdateMember saves changes to a member record (e.g. role change).
	UpdateMember(ctx context.Context, member *models.AllianceMember) error
}

// ---------------------------------------------------------------------------
// RaceReplayRepository — persistent race replay data
// ---------------------------------------------------------------------------

// RaceReplayRepository handles persistence for full race replays.
type RaceReplayRepository interface {
	// SaveReplay persists a full race replay. Uses upsert semantics.
	SaveReplay(ctx context.Context, replay *models.RaceReplay) error

	// GetReplay retrieves a single race replay by race ID.
	GetReplay(ctx context.Context, raceID string) (*models.RaceReplay, error)

	// ListRecentReplays returns the most recent race replays, newest first.
	ListRecentReplays(ctx context.Context, limit int) ([]*models.RaceReplay, error)

	// DeleteOldReplays removes replays older than the given cutoff time.
	DeleteOldReplays(ctx context.Context, olderThan time.Time) (int64, error)
}

// ---------------------------------------------------------------------------
// HorseFightRepository — gladiatorial combat records
// ---------------------------------------------------------------------------

// HorseFightRepository handles persistence for horse fight records.
type HorseFightRepository interface {
	// CreateFight persists a new horse fight record.
	CreateFight(ctx context.Context, fight *models.HorseFight) error

	// GetFight retrieves a fight by ID.
	GetFight(ctx context.Context, id string) (*models.HorseFight, error)

	// ListRecentFights returns the most recent fights, newest first.
	ListRecentFights(ctx context.Context, limit int) ([]*models.HorseFight, error)

	// ListFightsByHorse returns all fights involving a given horse.
	ListFightsByHorse(ctx context.Context, horseID string) ([]*models.HorseFight, error)

	// UpdateFight saves changes to an existing fight record.
	UpdateFight(ctx context.Context, fight *models.HorseFight) error
}

// ---------------------------------------------------------------------------
// GlueFactoryRepository — glue production records
// ---------------------------------------------------------------------------

// GlueFactoryRepository handles persistence for glue factory records.
type GlueFactoryRepository interface {
	// RecordGlue persists a glue factory result.
	RecordGlue(ctx context.Context, result *models.GlueFactoryResult, ownerID, stableID string) error

	// GetStableGlueHistory returns all glue factory records for a stable.
	GetStableGlueHistory(ctx context.Context, stableID string) ([]*models.GlueFactoryResult, error)

	// GetTotalGlueProduced returns the total glue produced across all stables.
	GetTotalGlueProduced(ctx context.Context) (int64, error)
}

// ---------------------------------------------------------------------------
// BreedingStallionRepository — permanent stud duty records
// ---------------------------------------------------------------------------

// BreedingStallionRepository handles persistence for permanent breeding stallions.
type BreedingStallionRepository interface {
	// AssignBreeder persists a new breeding stallion record.
	AssignBreeder(ctx context.Context, breeder *models.BreedingStallion) error

	// GetBreeder retrieves a breeding stallion by horse ID.
	GetBreeder(ctx context.Context, horseID string) (*models.BreedingStallion, error)

	// ListActiveBreedersByOwner returns all active breeders owned by a user.
	ListActiveBreedersByOwner(ctx context.Context, ownerID string) ([]*models.BreedingStallion, error)

	// ListAllActiveBreeders returns all active breeding stallions.
	ListAllActiveBreeders(ctx context.Context) ([]*models.BreedingStallion, error)

	// UpdateBreeder saves changes to an existing breeding stallion record.
	UpdateBreeder(ctx context.Context, breeder *models.BreedingStallion) error

	// DeactivateBreeder marks a breeding stallion as inactive.
	DeactivateBreeder(ctx context.Context, horseID string) error
}
