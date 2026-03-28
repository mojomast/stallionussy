// Package repository defines persistence interfaces for StallionUSSY.
//
// Each interface covers a single aggregate root (User, Stable, Horse, etc.)
// and is implemented by a concrete PostgreSQL layer (see the postgres sub-
// package). In-memory implementations can also be provided for tests.
package repository

import (
	"context"

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
