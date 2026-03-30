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
}

const (
	PokerTableOpen    = "open"
	PokerTableDrawing = "drawing"
	PokerTableSettled = "settled"
)

type SlotSpin struct {
	ID           string    `json:"id"`
	StableID     string    `json:"stableID"`
	UserID       string    `json:"userID"`
	WagerAmount  int64     `json:"wagerAmount"`
	PayoutAmount int64     `json:"payoutAmount"`
	Multiplier   float64   `json:"multiplier"`
	Symbols      []string  `json:"symbols"`
	Summary      string    `json:"summary"`
	CreatedAt    time.Time `json:"createdAt"`
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
