// Package models defines the core data types for StallionUSSY,
// a horse breeding and racing simulator.
package models

import "time"

// ---------------------------------------------------------------------------
// Gene Types — the seven heritable traits every horse carries
// ---------------------------------------------------------------------------

// GeneType identifies which trait a gene controls.
type GeneType string

const (
	GeneSPD GeneType = "SPD" // Speed: A=High Burst, B=Sustained
	GeneSTM GeneType = "STM" // Stamina: A=High Floor, B=Low Floor
	GeneTMP GeneType = "TMP" // Temper: A=Calm, B=Volatile
	GeneSZE GeneType = "SZE" // Size: A=Power, B=Lean
	GeneREC GeneType = "REC" // Recovery: A=Fast, B=Slow
	GeneINT GeneType = "INT" // Intelligence: A=High, B=Low
	GeneMUT GeneType = "MUT" // Mutation: A=Signal, B=Noise (1% elite/debuff)
)

// Backward-compatible aliases used by genussy, racussy, etc.
const (
	GeneSpeed    = GeneSPD
	GeneStamina  = GeneSTM
	GeneTemper   = GeneTMP
	GeneSize     = GeneSZE
	GeneRecovery = GeneREC
	GeneIntel    = GeneINT
	GeneMutation = GeneMUT
)

// AllGeneTypes is the canonical ordering of the seven gene types.
var AllGeneTypes = []GeneType{
	GeneSPD, GeneSTM, GeneTMP, GeneSZE, GeneREC, GeneINT, GeneMUT,
}

// Allele represents a single allele value. A is dominant, B is recessive.
type Allele string

const (
	AlleleA Allele = "A" // Dominant
	AlleleB Allele = "B" // Recessive
)

// Backward-compatible aliases for allele constants.
const (
	Dominant  = AlleleA
	Recessive = AlleleB
)

// ---------------------------------------------------------------------------
// Gene — a single gene with two alleles
// ---------------------------------------------------------------------------

// Gene is a pair of alleles for a specific gene type.
type Gene struct {
	Type    GeneType `json:"type"`
	AlleleA Allele   `json:"allele_a"` // First allele (from sire)
	AlleleB Allele   `json:"allele_b"` // Second allele (from mare)
}

// Allele1 returns the first allele (sire side). Alias for AlleleA field.
// Provided for backward compatibility with genussy/racussy.
func (g Gene) Allele1() Allele { return g.AlleleA }

// Allele2 returns the second allele (mare side). Alias for AlleleB field.
// Provided for backward compatibility with genussy/racussy.
func (g Gene) Allele2() Allele { return g.AlleleB }

// Express returns the diploid expression string: "AA", "AB", or "BB".
// The alleles are always returned in sorted order (A before B).
func (g Gene) Express() string {
	// Normalise to canonical order so AB == BA.
	a, b := g.AlleleA, g.AlleleB
	if a > b {
		a, b = b, a
	}
	return string(a) + string(b)
}

// GeneScore returns a numeric fitness contribution for this gene pair.
//
//	AA (homozygous dominant)  -> 1.00
//	AB (heterozygous)         -> 0.65
//	BB (homozygous recessive) -> 0.30
func (g Gene) GeneScore() float64 {
	switch g.Express() {
	case "AA":
		return 1.0
	case "AB":
		return 0.65
	default: // "BB"
		return 0.3
	}
}

// ---------------------------------------------------------------------------
// Genome — the full genetic profile of a horse (7 gene pairs)
// ---------------------------------------------------------------------------

// Genome maps each GeneType to its Gene pair.
type Genome map[GeneType]Gene

// ---------------------------------------------------------------------------
// Horse — the central game entity
// ---------------------------------------------------------------------------

// Horse represents a single horse in the simulation.
type Horse struct {
	ID              string    `json:"id"`              // UUID
	Name            string    `json:"name"`            // Generated or user-set name
	Genome          Genome    `json:"genome"`          // Full 7-gene profile
	SireID          string    `json:"sire_id"`         // Father's ID (empty for gen-0)
	MareID          string    `json:"mare_id"`         // Mother's ID (empty for gen-0)
	Generation      int       `json:"generation"`      // 0 for founders, increments each cross
	Age             int       `json:"age"`             // Age in race-days
	FitnessCeiling  float64   `json:"fitness_ceiling"` // Hidden potential set at birth
	CurrentFitness  float64   `json:"current_fitness"` // Revealed through racing / training
	Wins            int       `json:"wins"`
	Losses          int       `json:"losses"`
	Races           int       `json:"races"`
	ELO             float64   `json:"elo"`          // Matchmaking rating, starts at 1200
	OwnerID         string    `json:"owner_id"`     // Player who owns this horse
	IsLegendary     bool      `json:"is_legendary"` // True for canonical legendary lots
	LotNumber       int       `json:"lot_number"`   // 0 = normal, 1-12 = legendary lot index
	CreatedAt       time.Time `json:"created_at"`
	Lore            string    `json:"lore"`    // Flavor text / backstory
	Traits          []Trait   `json:"traits"`  // Quirks and special abilities
	Fatigue         float64   `json:"fatigue"` // 0-100, affects training and racing
	Retired         bool      `json:"retired"`
	RetiredChampion bool      `json:"retiredChampion,omitempty"`
	TotalEarnings   int64     `json:"total_earnings"`
	TrainingXP      float64   `json:"training_xp"`
	PeakELO         float64   `json:"peak_elo"`
	LastBredAt      time.Time `json:"lastBredAt,omitempty"` // Breeding cooldown tracker
	Injury          *Injury   `json:"injury,omitempty"`     // Current injury (nil = healthy)
}

// ---------------------------------------------------------------------------
// Horse Injuries — types, severities, and lore
// ---------------------------------------------------------------------------

// InjuryType identifies the kind of injury a horse has sustained.
type InjuryType string

const (
	InjuryMuscleStrain     InjuryType = "muscle_strain"
	InjuryTendonTear       InjuryType = "tendon_tear"
	InjuryHoofCrack        InjuryType = "hoof_crack"
	InjuryYogurtPoisoning  InjuryType = "yogurt_poisoning"
	InjuryExistentialDread InjuryType = "existential_dread"
	InjuryHauntedByE008    InjuryType = "haunted_by_e008"
)

// InjurySeverity indicates how bad an injury is.
type InjurySeverity string

const (
	SeverityMinor        InjurySeverity = "minor"         // 1 race cooldown
	SeverityModerate     InjurySeverity = "moderate"      // 3 race cooldown
	SeveritySevere       InjurySeverity = "severe"        // 5 race cooldown
	SeverityCareerEnding InjurySeverity = "career_ending" // Forced retirement
)

// Injury represents a horse's current injury state.
type Injury struct {
	Type        InjuryType     `json:"type"`
	Severity    InjurySeverity `json:"severity"`
	Description string         `json:"description"`
	RacesLeft   int            `json:"races_left"` // Races remaining before recovery (0 = healed)
	OccurredAt  time.Time      `json:"occurred_at"`
}

// InjuryHealCost returns the cummies cost to heal an injury by severity.
func InjuryHealCost(severity InjurySeverity) int64 {
	switch severity {
	case SeverityMinor:
		return 100
	case SeverityModerate:
		return 300
	case SeveritySevere:
		return 500
	case SeverityCareerEnding:
		return 0 // Can't heal career-ending injuries
	default:
		return 0
	}
}

// InjuryRaceCooldown returns the number of races a horse must sit out by severity.
func InjuryRaceCooldown(severity InjurySeverity) int {
	switch severity {
	case SeverityMinor:
		return 1
	case SeverityModerate:
		return 3
	case SeveritySevere:
		return 5
	case SeverityCareerEnding:
		return 9999 // Effectively permanent
	default:
		return 0
	}
}

// ---------------------------------------------------------------------------
// Stable — a player's collection of horses
// ---------------------------------------------------------------------------

// Stable groups a player's horses and tracks their currency.
type Stable struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	OwnerID       string        `json:"owner_id"`
	Cummies       int64         `json:"cummies"` // In-game currency balance
	CasinoChips   int64         `json:"casino_chips"`
	StarterGrants int           `json:"starter_grants"`
	Horses        []Horse       `json:"horses"`
	CreatedAt     time.Time     `json:"created_at"`
	Achievements  []Achievement `json:"achievements"`
	TotalEarnings int64         `json:"total_earnings"`
	TotalRaces    int64         `json:"total_races"`
	Motto         string        `json:"motto"` // Random flavor motto assigned at creation
}

// ---------------------------------------------------------------------------
// Track Types
// ---------------------------------------------------------------------------

// TrackType determines the terrain and distance profile of a race.
type TrackType string

const (
	TrackSprintussy  TrackType = "Sprintussy"  // 800 m — raw speed
	TrackGrindussy   TrackType = "Grindussy"   // 3200 m — endurance grind
	TrackMudussy     TrackType = "Mudussy"     // 1600 m — middle-distance mud
	TrackThunderussy TrackType = "Thunderussy" // 2400 m — balanced with weather chaos
	TrackFrostussy   TrackType = "Frostussy"   // 1200 m — ice surface, SZE inverted
	TrackHauntedussy TrackType = "Hauntedussy" // 666 m — E-008 events more common
)

// TrackDistance returns the canonical distance in metres for a track type.
func TrackDistance(t TrackType) int {
	switch t {
	case TrackSprintussy:
		return 800
	case TrackGrindussy:
		return 3200
	case TrackMudussy:
		return 1600
	case TrackThunderussy:
		return 2400
	case TrackFrostussy:
		return 1200
	case TrackHauntedussy:
		return 666
	default:
		return 0
	}
}

// ---------------------------------------------------------------------------
// Race Status
// ---------------------------------------------------------------------------

// RaceStatus tracks the lifecycle of a race event.
type RaceStatus string

const (
	RaceStatusPending  RaceStatus = "Pending"
	RaceStatusRunning  RaceStatus = "Running"
	RaceStatusFinished RaceStatus = "Finished"
)

// ---------------------------------------------------------------------------
// TickEvent — a single simulation tick snapshot for a horse in a race
// ---------------------------------------------------------------------------

// TickEvent captures one discrete simulation step for race replay.
type TickEvent struct {
	Tick     int     `json:"tick"`
	Position float64 `json:"position"` // Distance covered so far (metres)
	Speed    float64 `json:"speed"`    // Instantaneous speed at this tick
	Event    string  `json:"event"`    // Optional narrative event (e.g. "BURST", "STUMBLE")
}

// ---------------------------------------------------------------------------
// RaceEntry — one horse's participation record within a race
// ---------------------------------------------------------------------------

// RaceEntry holds per-horse state and results for a single race.
type RaceEntry struct {
	HorseID     string        `json:"horse_id"`
	HorseName   string        `json:"horse_name"`
	Position    float64       `json:"position"`     // Current distance covered (metres)
	Finished    bool          `json:"finished"`     // True once the horse crosses the line
	FinalTime   time.Duration `json:"final_time"`   // Total race duration (zero until finished)
	FinishPlace int           `json:"finish_place"` // 1-indexed placement (0 until finished)
	TickLog     []TickEvent   `json:"tick_log"`     // Full tick-by-tick replay data
}

// ---------------------------------------------------------------------------
// Race — a race event that groups entries on a track
// ---------------------------------------------------------------------------

// Race represents a single race event.
type Race struct {
	ID        string      `json:"id"`
	TrackType TrackType   `json:"track_type"`
	Distance  int         `json:"distance"` // Track distance in metres
	Entries   []RaceEntry `json:"entries"`
	Status    RaceStatus  `json:"status"`
	Purse     int64       `json:"purse"` // Prize pool in cummies
	CreatedAt time.Time   `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Stud Market
// ---------------------------------------------------------------------------

// StudListing is a marketplace entry advertising a horse for breeding.
type StudListing struct {
	ID          string    `json:"id"`
	HorseID     string    `json:"horse_id"`
	HorseName   string    `json:"horse_name"`
	OwnerID     string    `json:"owner_id"`
	Price       int64     `json:"price"`        // Cost in cummies
	Pedigree    string    `json:"pedigree"`     // Human-readable lineage summary
	SapphoScore float64   `json:"sappho_score"` // Quality rating 0-12
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Market Transaction
// ---------------------------------------------------------------------------

// MarketTransaction records a completed stud-market deal.
type MarketTransaction struct {
	ID         string    `json:"id"`
	ListingID  string    `json:"listing_id"`
	BuyerID    string    `json:"buyer_id"`
	SellerID   string    `json:"seller_id"`
	Price      int64     `json:"price"`       // Agreed price in cummies
	BurnAmount int64     `json:"burn_amount"` // 2% burn deducted from the economy
	FoalID     string    `json:"foal_id"`     // ID of the resulting baby horse
	CreatedAt  time.Time `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Training System
// ---------------------------------------------------------------------------

// WorkoutType identifies the kind of training session.
type WorkoutType string

const (
	WorkoutSprint    WorkoutType = "Sprint"    // Boosts SPD expression
	WorkoutEndurance WorkoutType = "Endurance" // Boosts STM expression
	WorkoutMentalRep WorkoutType = "MentalRep" // Boosts TMP/INT
	WorkoutMudRun    WorkoutType = "MudRun"    // Boosts SZE for mud
	WorkoutRecovery  WorkoutType = "RestDay"   // Reduces fatigue, boosts REC
	WorkoutGeneral   WorkoutType = "General"   // Small boost to everything
)

// TrainingSession records a single training workout for a horse.
type TrainingSession struct {
	ID            string      `json:"id"`
	HorseID       string      `json:"horse_id"`
	WorkoutType   WorkoutType `json:"workout_type"`
	XPGained      float64     `json:"xp_gained"`
	FitnessBefore float64     `json:"fitness_before"`
	FitnessAfter  float64     `json:"fitness_after"`
	FatigueAfter  float64     `json:"fatigue_after"`
	Injury        bool        `json:"injury"` // 2% chance per session
	InjuryNote    string      `json:"injury_note"`
	CreatedAt     time.Time   `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Horse Traits / Quirks
// ---------------------------------------------------------------------------

// Trait represents a special quirk or ability a horse can possess.
type Trait struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Effect      string  `json:"effect"`    // e.g. "speed_boost", "panic_resist", "mud_lover"
	Magnitude   float64 `json:"magnitude"` // multiplier, e.g. 1.05 = +5%
	Rarity      string  `json:"rarity"`    // "common", "rare", "legendary", "anomalous"
}

// ---------------------------------------------------------------------------
// Race History
// ---------------------------------------------------------------------------

// RaceResult captures a single horse's performance in a completed race.
type RaceResult struct {
	RaceID      string        `json:"race_id"`
	HorseID     string        `json:"horse_id"`
	HorseName   string        `json:"horse_name"`
	TrackType   TrackType     `json:"track_type"`
	Distance    int           `json:"distance"`
	FinishPlace int           `json:"finish_place"`
	TotalHorses int           `json:"total_horses"`
	FinalTime   time.Duration `json:"final_time"`
	ELOBefore   float64       `json:"elo_before"`
	ELOAfter    float64       `json:"elo_after"`
	Earnings    int64         `json:"earnings"`
	Weather     string        `json:"weather"`
	CreatedAt   time.Time     `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Weather System
// ---------------------------------------------------------------------------

// Weather represents atmospheric conditions during a race.
type Weather string

const (
	WeatherClear     Weather = "Clear"
	WeatherRainy     Weather = "Rainy"
	WeatherStormy    Weather = "Stormy"
	WeatherFoggy     Weather = "Foggy"
	WeatherScorching Weather = "Scorching"
	WeatherHaunted   Weather = "Haunted" // E-008 special weather
)

// ---------------------------------------------------------------------------
// Tournaments
// ---------------------------------------------------------------------------

// Tournament represents a multi-round competitive event.
type Tournament struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	TrackType    TrackType         `json:"track_type"`
	Rounds       int               `json:"rounds"`
	CurrentRound int               `json:"current_round"`
	EntryFee     int64             `json:"entry_fee"`
	PrizePool    int64             `json:"prize_pool"`
	Standings    []TournamentEntry `json:"standings"`
	Races        []string          `json:"races"`  // race IDs
	Status       string            `json:"status"` // "Open", "InProgress", "Finished"
	CreatedAt    time.Time         `json:"created_at"`
}

// TournamentEntry tracks a single horse's standing within a tournament.
type TournamentEntry struct {
	HorseID   string `json:"horse_id"`
	HorseName string `json:"horse_name"`
	StableID  string `json:"stable_id"`
	Points    int    `json:"points"`
	RacesRun  int    `json:"races_run"`
	BestPlace int    `json:"best_place"`
}

// ---------------------------------------------------------------------------
// Achievements
// ---------------------------------------------------------------------------

// Achievement represents an unlockable badge or milestone for a stable.
type Achievement struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Icon        string    `json:"icon"`   // emoji or ASCII art
	Rarity      string    `json:"rarity"` // "common", "rare", "epic", "legendary"
	UnlockedAt  time.Time `json:"unlocked_at"`
}

// ---------------------------------------------------------------------------
// User Accounts
// ---------------------------------------------------------------------------

// User represents a player account.
type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"` // Never sent to client
	DisplayName  string    `json:"display_name"`
	TokenVersion int       `json:"-"` // Incremented on password change to invalidate JWTs
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ---------------------------------------------------------------------------
// Trading
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Head-to-Head Challenges
// ---------------------------------------------------------------------------

// Challenge represents a head-to-head race challenge between two players.
type Challenge struct {
	ID                  string    `json:"id"`
	ChallengerID        string    `json:"challengerID"` // user ID
	ChallengerName      string    `json:"challengerName"`
	ChallengerHorse     string    `json:"challengerHorse"` // horse ID
	ChallengerHorseName string    `json:"challengerHorseName"`
	DefenderID          string    `json:"defenderID"` // user ID
	DefenderName        string    `json:"defenderName"`
	DefenderHorse       string    `json:"defenderHorse"` // horse ID (set on accept)
	DefenderHorseName   string    `json:"defenderHorseName"`
	Wager               int64     `json:"wager"`  // cummies wagered (0 = no wager)
	Status              string    `json:"status"` // pending, accepted, completed, expired, declined
	CreatedAt           time.Time `json:"createdAt"`
	ExpiresAt           time.Time `json:"expiresAt"`
}

const (
	ChallengeStatusPending   = "pending"
	ChallengeStatusAccepted  = "accepted"
	ChallengeStatusCompleted = "completed"
	ChallengeStatusExpired   = "expired"
	ChallengeStatusDeclined  = "declined"
)

// ---------------------------------------------------------------------------
// Betting
// ---------------------------------------------------------------------------

// Bet represents a wager on a race outcome.
type Bet struct {
	ID        string    `json:"id"`
	RaceID    string    `json:"raceID"`
	UserID    string    `json:"userID"`
	Username  string    `json:"username"`
	StableID  string    `json:"stableID"`
	HorseID   string    `json:"horseID"`
	HorseName string    `json:"horseName"`
	Amount    int64     `json:"amount"`
	Payout    int64     `json:"payout"` // filled after resolution
	Won       bool      `json:"won"`    // filled after resolution
	CreatedAt time.Time `json:"createdAt"`
}

// BettingPool tracks all bets for a single race.
type BettingPool struct {
	RaceID    string         `json:"raceID"`
	Status    string         `json:"status"` // open, closed, resolved
	Horses    []BettingHorse `json:"horses"`
	TotalPool int64          `json:"totalPool"`
	Bets      []Bet          `json:"bets"`
	HouseCut  int64          `json:"houseCut"`
	OpenedAt  time.Time      `json:"openedAt"`
	ClosedAt  time.Time      `json:"closedAt"`
}

// BettingHorse represents a horse's odds and total bet amount in a betting pool.
type BettingHorse struct {
	HorseID   string  `json:"horseID"`
	HorseName string  `json:"horseName"`
	TotalBet  int64   `json:"totalBet"`
	Odds      float64 `json:"odds"` // pari-mutuel odds
	BetCount  int     `json:"betCount"`
}

// ---------------------------------------------------------------------------
// Seasons & Leaderboards
// ---------------------------------------------------------------------------

// Season represents a competitive season with ELO resets and rewards.
type Season struct {
	ID        int              `json:"id"`
	Name      string           `json:"name"`
	StartedAt time.Time        `json:"startedAt"`
	EndedAt   time.Time        `json:"endedAt,omitempty"`
	Active    bool             `json:"active"`
	Champions []SeasonChampion `json:"champions,omitempty"`
}

// SeasonChampion records a top finisher in a completed season.
type SeasonChampion struct {
	Place      int    `json:"place"`
	StableID   string `json:"stableID"`
	StableName string `json:"stableName"`
	ELO        int    `json:"elo"`
	Wins       int    `json:"wins"`
	Earnings   int64  `json:"earnings"`
	Reward     int64  `json:"reward"` // cummies reward
}

// LeaderboardEntry represents a single stable's ranking in the leaderboard.
type LeaderboardEntry struct {
	Rank       int     `json:"rank"`
	StableID   string  `json:"stableID"`
	StableName string  `json:"stableName"`
	OwnerName  string  `json:"ownerName"`
	ELO        int     `json:"elo"`
	Wins       int     `json:"wins"`
	Losses     int     `json:"losses"`
	WinRate    float64 `json:"winRate"`
	TotalRaces int     `json:"totalRaces"`
	Earnings   int64   `json:"earnings"`
	BestHorse  string  `json:"bestHorse"`
	BestELO    int     `json:"bestElo"`
	Streak     int     `json:"streak"`
}

// HorseLeaderboardEntry represents a single horse's ranking in the horse leaderboard.
type HorseLeaderboardEntry struct {
	Rank       int     `json:"rank"`
	HorseID    string  `json:"horseID"`
	HorseName  string  `json:"horseName"`
	StableID   string  `json:"stableID"`
	StableName string  `json:"stableName"`
	ELO        int     `json:"elo"`
	Wins       int     `json:"wins"`
	Losses     int     `json:"losses"`
	WinRate    float64 `json:"winRate"`
	TotalRaces int     `json:"totalRaces"`
	Earnings   int64   `json:"earnings"`
	Streak     int     `json:"streak"`
}

// ---------------------------------------------------------------------------
// Engagement System
// ---------------------------------------------------------------------------

// PlayerProgress tracks a player's engagement metrics (daily logins, streaks,
// prestige, and daily action limits).
type PlayerProgress struct {
	UserID              string `json:"userID"`
	LoginStreak         int    `json:"loginStreak"`
	LastLoginDate       string `json:"lastLoginDate"` // YYYY-MM-DD format
	TotalLogins         int    `json:"totalLogins"`
	DailyTrainsLeft     int    `json:"dailyTrainsLeft"` // resets daily, default 5
	DailyRacesLeft      int    `json:"dailyRacesLeft"`  // resets daily, default 10
	LastDailyReset      string `json:"lastDailyReset"`  // YYYY-MM-DD
	LastCasinoGrantDate string `json:"lastCasinoGrantDate,omitempty"`
	PrestigeLevel       int    `json:"prestigeLevel"`
	PrestigeXP          int64  `json:"prestigeXP"`
	LifetimeEarnings    int64  `json:"lifetimeEarnings"`
}

// DailyReward describes the reward for a specific day in the login streak cycle.
type DailyReward struct {
	Day     int    `json:"day"`
	Cummies int64  `json:"cummies"`
	Bonus   string `json:"bonus"` // description of extra reward
}

// PrestigeTier defines what each prestige level gives.
type PrestigeTier struct {
	Level         int     `json:"level"`
	Name          string  `json:"name"`
	RequiredXP    int64   `json:"requiredXP"`
	CummiesBonus  float64 `json:"cummiesBonus"`  // multiplier on race earnings
	TrainingBonus float64 `json:"trainingBonus"` // multiplier on training effectiveness
	MaxHorses     int     `json:"maxHorses"`     // max horses in stable
}

// ---------------------------------------------------------------------------
// Trading
// ---------------------------------------------------------------------------

// TradeOffer represents a pending, accepted, rejected, or cancelled trade
// between two stables for a single horse.
type TradeOffer struct {
	ID           string    `json:"id"`
	HorseID      string    `json:"horse_id"`
	HorseName    string    `json:"horse_name"`
	FromStableID string    `json:"from_stable_id"`
	ToStableID   string    `json:"to_stable_id"`
	Price        int64     `json:"price"`  // in Cummies
	Status       string    `json:"status"` // "Pending", "Accepted", "Rejected", "Cancelled"
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ---------------------------------------------------------------------------
// Live Auctions
// ---------------------------------------------------------------------------

// Auction status constants.
const (
	AuctionStatusOpen      = "open"      // Accepting bids
	AuctionStatusEnding    = "ending"    // Anti-snipe extension active (last 30s)
	AuctionStatusSold      = "sold"      // Winner determined, horse transferred
	AuctionStatusExpired   = "expired"   // Time ran out with no bids
	AuctionStatusCancelled = "cancelled" // Seller cancelled before any bids
)

// Auction represents a live horse auction with real-time bidding.
type Auction struct {
	ID            string       `json:"id"`
	SellerID      string       `json:"sellerID"`    // User ID of the seller
	SellerName    string       `json:"sellerName"`  // Display name of the seller
	StableID      string       `json:"stableID"`    // Seller's stable ID
	HorseID       string       `json:"horseID"`     // The horse being auctioned
	HorseName     string       `json:"horseName"`   // Cached horse name
	StartingBid   int64        `json:"startingBid"` // Minimum opening bid in cummies
	CurrentBid    int64        `json:"currentBid"`  // Current highest bid (0 = no bids yet)
	BidderID      string       `json:"bidderID"`    // Current highest bidder user ID
	BidderName    string       `json:"bidderName"`  // Current highest bidder display name
	BidCount      int          `json:"bidCount"`    // Total number of bids placed
	BidHistory    []AuctionBid `json:"bidHistory"`  // Full bid history (stored as JSONB)
	Status        string       `json:"status"`      // open, ending, sold, expired, cancelled
	Duration      int          `json:"duration"`    // Auction duration in seconds (default 120)
	CreatedAt     time.Time    `json:"createdAt"`
	ExpiresAt     time.Time    `json:"expiresAt"`             // When the auction ends (can be extended by anti-snipe)
	CompletedAt   time.Time    `json:"completedAt,omitempty"` // When the auction was finalized
	GeoffrussyTax int64        `json:"geoffrussyTax"`         // 5% tax on sale price (burned from economy)
}

// AuctionBid records a single bid in an auction's history.
type AuctionBid struct {
	BidderID   string    `json:"bidderID"`
	BidderName string    `json:"bidderName"`
	Amount     int64     `json:"amount"`
	Timestamp  time.Time `json:"timestamp"`
}

// ---------------------------------------------------------------------------
// Stable Alliances / Guild System
// ---------------------------------------------------------------------------

// AllianceRole identifies a member's rank within an alliance.
type AllianceRole string

const (
	AllianceRoleLeader  AllianceRole = "leader"
	AllianceRoleOfficer AllianceRole = "officer"
	AllianceRoleMember  AllianceRole = "member"
)

// Alliance represents a stable alliance (guild).
type Alliance struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Tag       string           `json:"tag"`       // Short 2-5 char tag shown in brackets
	LeaderID  string           `json:"leader_id"` // User ID of the founding leader
	Motto     string           `json:"motto"`     // Randomly assigned lore motto on creation
	Treasury  int64            `json:"treasury"`  // Shared cummies pool
	Members   []AllianceMember `json:"members"`
	CreatedAt time.Time        `json:"created_at"`
}

// AllianceMember records a single player's membership in an alliance.
type AllianceMember struct {
	AllianceID string       `json:"alliance_id"`
	UserID     string       `json:"user_id"`
	Username   string       `json:"username"`
	StableID   string       `json:"stable_id"`
	Role       AllianceRole `json:"role"`
	JoinedAt   time.Time    `json:"joined_at"`
}

// AllianceLoreMottos are the 8 sacred mottos randomly assigned on creation.
var AllianceLoreMottos = []string{
	"We ride at dawn, smeared in yogurt and glory.",
	"Our stallions fear nothing — except maybe E-008.",
	"Hooves of thunder, hearts of questionable integrity.",
	"United by cummies, divided by who gets the good hay.",
	"Born to breed. Forced to race. Destined to lose spectacularly.",
	"In the name of the Sire, the Mare, and the Holy Foal.",
	"One stable to rule them all (and still finish last).",
	"We didn't choose the stallion life. The stallion life chose poorly.",
}

// ---------------------------------------------------------------------------
// Random Events System
// ---------------------------------------------------------------------------

// RandomEvent describes a mid-race random event that modifies outcomes.
type RandomEvent struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Effect      string  `json:"effect"` // e.g. "speed_buff", "speed_debuff", "chaos", "cosmetic"
	Magnitude   float64 `json:"magnitude"`
	Target      string  `json:"target"` // "random_horse", "all_horses", "leader", "last_place"
}

// ---------------------------------------------------------------------------
// Race Replays (persistent full race data for replay sharing)
// ---------------------------------------------------------------------------

// RaceReplay stores a complete race result for persistent replay sharing.
// The Data field holds the full JSON-encoded raceResult (race, narrative, weather).
type RaceReplay struct {
	RaceID     string    `json:"raceID"`
	TrackType  string    `json:"trackType"`
	Distance   int       `json:"distance"`
	Purse      int64     `json:"purse"`
	Entries    int       `json:"entries"` // Number of horses in the race
	Weather    string    `json:"weather"`
	WinnerID   string    `json:"winnerID"`
	WinnerName string    `json:"winnerName"`
	Data       []byte    `json:"-"` // Full JSON-encoded race result (stored as JSONB)
	CreatedAt  time.Time `json:"createdAt"`
}

// ---------------------------------------------------------------------------
// Horse Fights (Gladiatorial Combat)
// ---------------------------------------------------------------------------

// HorseFight represents a gladiatorial combat event between two horses.
type HorseFight struct {
	ID            string    `json:"id"`
	ArenaType     string    `json:"arenaType"`
	Horse1ID      string    `json:"horse1ID"`
	Horse1Name    string    `json:"horse1Name"`
	Horse1OwnerID string    `json:"horse1OwnerID"`
	Horse2ID      string    `json:"horse2ID"`
	Horse2Name    string    `json:"horse2Name"`
	Horse2OwnerID string    `json:"horse2OwnerID"`
	WinnerID      string    `json:"winnerID"`
	WinnerName    string    `json:"winnerName"`
	LoserID       string    `json:"loserID"`
	LoserName     string    `json:"loserName"`
	IsFatality    bool      `json:"isFatality"`
	IsToDeath     bool      `json:"isToDeath"`
	Purse         int64     `json:"purse"`
	EntryFee      int64     `json:"entryFee"`
	Status        string    `json:"status"` // "pending", "fighting", "finished"
	KORound       int       `json:"koRound"`
	TotalRounds   int       `json:"totalRounds"`
	FightLog      []byte    `json:"fightLog,omitempty"` // JSON-encoded full fight result
	Narrative     []string  `json:"narrative"`
	CreatedAt     time.Time `json:"createdAt"`
}

const (
	FightStatusPending  = "pending"
	FightStatusFighting = "fighting"
	FightStatusFinished = "finished"
)

// GlueFactoryResult represents the outcome of sending a horse to the glue factory.
type GlueFactoryResult struct {
	HorseID       string    `json:"horseID"`
	HorseName     string    `json:"horseName"`
	GlueProduced  int64     `json:"glueProduced"`  // units of glue
	CummiesEarned int64     `json:"cummiesEarned"` // payment for the glue
	BonusMaterial string    `json:"bonusMaterial"` // random bonus resource
	BonusAmount   int64     `json:"bonusAmount"`
	Eulogy        string    `json:"eulogy"` // randomly generated farewell
	CreatedAt     time.Time `json:"createdAt"`
}

// BreedingStallion tracks a horse permanently assigned to breeding duty.
type BreedingStallion struct {
	HorseID       string    `json:"horseID"`
	HorseName     string    `json:"horseName"`
	OwnerID       string    `json:"ownerID"`
	StableID      string    `json:"stableID"`
	BreedCount    int       `json:"breedCount"`
	TotalEarnings int64     `json:"totalEarnings"`
	Fee           int64     `json:"fee"`           // breeding fee charged
	CooldownHours int       `json:"cooldownHours"` // reduced cooldown (default 12 vs normal 24)
	Active        bool      `json:"active"`
	AssignedAt    time.Time `json:"assignedAt"`
}
