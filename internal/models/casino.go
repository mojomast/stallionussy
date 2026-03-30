package models

import "time"

type PokerCard struct {
	Rank string `json:"rank"`
	Suit string `json:"suit"`
}

type PokerSeat struct {
	UserID       string      `json:"userID"`
	Username     string      `json:"username"`
	StableID     string      `json:"stableID"`
	BuyIn        int64       `json:"buyIn"`
	Currency     string      `json:"currency"`
	Hand         []PokerCard `json:"hand,omitempty"`
	Discarded    []int       `json:"discarded,omitempty"`
	HasDrawn     bool        `json:"hasDrawn"`
	HandRank     string      `json:"handRank,omitempty"`
	Payout       int64       `json:"payout,omitempty"`
	JoinedAt     time.Time   `json:"joinedAt"`
	LastActionAt time.Time   `json:"lastActionAt"`

	// Hold'em-specific fields
	ChipStack  int64  `json:"chipStack"`            // chips at the table
	CurrentBet int64  `json:"currentBet"`           // bet in current betting round
	Folded     bool   `json:"folded"`               // true if player has folded this hand
	AllIn      bool   `json:"allIn"`                // true if player is all-in
	LastAction string `json:"lastAction,omitempty"` // "check", "call", "raise", "fold", "allin"
}

// SidePot represents an auxiliary pot created when a player goes all-in for
// less than the current bet, restricting eligibility.
type SidePot struct {
	Amount   int64    `json:"amount"`
	Eligible []string `json:"eligible"` // userIDs eligible for this pot
}

type PokerTable struct {
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	CreatedBy     string      `json:"createdBy"`
	StakeCurrency string      `json:"stakeCurrency"`
	BuyIn         int64       `json:"buyIn"`
	MaxPlayers    int         `json:"maxPlayers"`
	Status        string      `json:"status"`
	Pot           int64       `json:"pot"`
	DeckSeed      uint64      `json:"-"`
	Log           []string    `json:"log"`
	Seats         []PokerSeat `json:"seats"`
	StartedAt     time.Time   `json:"startedAt,omitempty"`
	CreatedAt     time.Time   `json:"createdAt"`
	UpdatedAt     time.Time   `json:"updatedAt"`

	// Hold'em-specific fields
	GameType       string      `json:"gameType"`                 // "draw" or "holdem"
	CommunityCards []PokerCard `json:"communityCards,omitempty"` // shared cards (flop/turn/river)
	SmallBlind     int64       `json:"smallBlind,omitempty"`
	BigBlind       int64       `json:"bigBlind,omitempty"`
	CurrentBet     int64       `json:"currentBet,omitempty"` // current highest bet in the betting round
	DealerSeat     int         `json:"dealerSeat"`           // button position
	ActionSeat     int         `json:"actionSeat"`           // whose turn it is (-1 = nobody)
	MinRaise       int64       `json:"minRaise,omitempty"`   // minimum raise increment
	SidePots       []SidePot   `json:"sidePots,omitempty"`
	Round          int         `json:"round"`                    // hand number (increments each deal)
	ActionDeadline time.Time   `json:"actionDeadline,omitempty"` // 60s timeout per action
}

const (
	PokerTableOpen    = "open"
	PokerTableDrawing = "drawing" // 5-card draw phase
	PokerTableSettled = "settled"

	// Texas Hold'em round statuses
	PokerTablePreFlop  = "preflop"
	PokerTableFlop     = "flop"
	PokerTableTurn     = "turn"
	PokerTableRiver    = "river"
	PokerTableShowdown = "showdown"

	// Game types
	PokerGameDraw   = "draw"
	PokerGameHoldem = "holdem"
)

// PaylineResult describes a single winning payline from a 5-reel slot spin.
type PaylineResult struct {
	LineNum int      `json:"lineNum"`
	Symbols []string `json:"symbols"`
	Match   int      `json:"match"` // how many symbols matched (3, 4, or 5)
	Payout  int64    `json:"payout"`
}

// SlotSpin is the result of a single slot machine pull. The 5-reel video slot
// produces a 3×5 grid (Reels), evaluates up to 9 paylines, and may trigger
// scatter bonuses or the progressive jackpot.
type SlotSpin struct {
	ID             string          `json:"id"`
	StableID       string          `json:"stableID"`
	UserID         string          `json:"userID"`
	WagerAmount    int64           `json:"wagerAmount"`
	Lines          int             `json:"lines"`                    // number of active paylines (1-9)
	PayoutAmount   int64           `json:"payoutAmount"`             // backward-compat: same as TotalPayout
	Multiplier     float64         `json:"multiplier"`               // backward-compat: totalPayout / totalWager
	Symbols        []string        `json:"symbols"`                  // backward-compat: middle row of the grid
	Reels          [][]string      `json:"reels"`                    // 5 reels × 3 visible rows
	Paylines       []PaylineResult `json:"paylines,omitempty"`       // winning paylines
	BonusTriggered string          `json:"bonusTriggered,omitempty"` // bonus round type
	BonusPayout    int64           `json:"bonusPayout,omitempty"`    // bonus round winnings
	JackpotWin     bool            `json:"jackpotWin,omitempty"`     // progressive jackpot hit
	FreeSpinsWon   int             `json:"freeSpinsWon,omitempty"`   // free spins awarded
	TotalPayout    int64           `json:"totalPayout"`              // total across all paylines + bonus
	Summary        string          `json:"summary"`
	CreatedAt      time.Time       `json:"createdAt"`
}

type DepartureRecord struct {
	ID            string    `json:"id"`
	HorseID       string    `json:"horseID"`
	HorseName     string    `json:"horseName"`
	OwnerID       string    `json:"ownerID"`
	StableID      string    `json:"stableID"`
	Cause         string    `json:"cause"`
	State         string    `json:"state"`
	HorseSnapshot Horse     `json:"horseSnapshot"`
	OmenText      string    `json:"omenText,omitempty"`
	ReturnSummary string    `json:"returnSummary,omitempty"`
	ReturnedHorse string    `json:"returnedHorse,omitempty"`
	LastRollDate  string    `json:"lastRollDate,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	OmenExpiresAt time.Time `json:"omenExpiresAt,omitempty"`
	ReturnedAt    time.Time `json:"returnedAt,omitempty"`
}

const (
	DepartureCauseGlue  = "glue_factory"
	DepartureCauseFight = "fatal_fight"

	DepartureStateDormant = "dormant"
	DepartureStateOmen    = "omen"
	DepartureStateClaimed = "claimed"
	DepartureStateLost    = "lost"
)
