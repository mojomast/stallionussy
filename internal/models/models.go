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
	ID             string    `json:"id"`              // UUID
	Name           string    `json:"name"`            // Generated or user-set name
	Genome         Genome    `json:"genome"`          // Full 7-gene profile
	SireID         string    `json:"sire_id"`         // Father's ID (empty for gen-0)
	MareID         string    `json:"mare_id"`         // Mother's ID (empty for gen-0)
	Generation     int       `json:"generation"`      // 0 for founders, increments each cross
	Age            int       `json:"age"`             // Age in race-days
	FitnessCeiling float64   `json:"fitness_ceiling"` // Hidden potential set at birth
	CurrentFitness float64   `json:"current_fitness"` // Revealed through racing / training
	Wins           int       `json:"wins"`
	Losses         int       `json:"losses"`
	Races          int       `json:"races"`
	ELO            float64   `json:"elo"`          // Matchmaking rating, starts at 1200
	OwnerID        string    `json:"owner_id"`     // Player who owns this horse
	IsLegendary    bool      `json:"is_legendary"` // True for canonical legendary lots
	LotNumber      int       `json:"lot_number"`   // 0 = normal, 1-6 = legendary lot index
	CreatedAt      time.Time `json:"created_at"`
	Lore           string    `json:"lore"`    // Flavor text / backstory
	Traits         []Trait   `json:"traits"`  // Quirks and special abilities
	Fatigue        float64   `json:"fatigue"` // 0-100, affects training and racing
	Retired        bool      `json:"retired"`
	TotalEarnings  int64     `json:"total_earnings"`
	TrainingXP     float64   `json:"training_xp"`
	PeakELO        float64   `json:"peak_elo"`
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
	Horses        []Horse       `json:"horses"`
	CreatedAt     time.Time     `json:"created_at"`
	Achievements  []Achievement `json:"achievements"`
	TotalEarnings int64         `json:"total_earnings"`
	TotalRaces    int64         `json:"total_races"`
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
