# StallionUSSY Casino Design Specification

> Comprehensive gambling mechanics research and implementation plan for
> CASINOUSSY: the degenerate side-loop of a horse breeding/racing simulator.
> Currency: Cummies (primary) / Casino Chips (ring-fenced). Aesthetic: CRT
> terminal with phosphor glow, scanlines, and glitch effects.

---

## Table of Contents

1. [Current State Audit](#1-current-state-audit)
2. [Slots: Upgraded System](#2-slots-upgraded-system)
3. [Poker: Texas Hold'em & Tournaments](#3-poker-texas-holdem--tournaments)
4. [Economy Design](#4-economy-design)
5. [Horse-Theme Integration](#5-horse-theme-integration)
6. [Implementation Priority](#6-implementation-priority)

---

## 1. Current State Audit

### What exists today

**Slots** (`internal/server/casino.go:397-428`):
- 3-reel, single-payline machine
- 6 symbols: `CHERRY, OATS, BELL, SEVEN, YOGURT, SKULL`
- Payout logic: triple match = 2.0-6.5x, pair = 0.75x, YOGURT+BELL combo = 1.25x, else 0
- No wild symbols, no scatter, no bonus rounds, no progressive jackpot
- Estimated RTP: ~38-45% (very harsh house edge, well below industry standard)

**Poker** (`internal/server/casino.go:620-728`):
- 5-card draw only (not Hold'em)
- 2-4 players, single draw round
- Hand evaluation covers: high card through straight flush
- Winner-takes-all pot, no side pots
- No betting rounds (buy-in is the only wager)
- No blinds, no raise/fold mechanic

**Economy** (`internal/server/casino.go:22-27`):
- Exchange rate: 25 Cummies -> 1 Casino Chip (buy), 1 Casino Chip -> 10 Cummies (cashout)
- Protected floor: 500 Cummies minimum (cannot exchange below this)
- Daily chip grant: 40 chips/day
- Casino chips are ring-fenced from the core Cummies economy

### Key problems

1. Slots RTP is ~40%, far below the 92-97% industry standard -- players will hemorrhage chips
2. Poker has no actual betting strategy -- it's "ante and pray"
3. No progressive or escalating reward structure
4. No bonus mechanics to create excitement peaks
5. No daily engagement hooks beyond the 40-chip grant

---

## 2. Slots: Upgraded System

### 2.1 Multi-Payline Video Slots (5-Reel)

**How real video slots work:**

Modern video slots use 5 reels with 3-4 visible rows per reel, creating a grid
(typically 5x3 = 15 visible symbol positions). Paylines are predefined patterns
across this grid. Classic machines had 1-9 paylines; modern machines use 20-50
paylines or "243 ways to win" (any matching symbol on adjacent reels left to
right). Players bet per-payline, so more paylines = higher total wager but more
frequent wins.

**Recommended implementation: "STALLION SLOTS V2"**

```
Grid:       5 reels x 3 rows (15 visible positions)
Paylines:   20 fixed paylines (always active)
Bet:        wager_per_line * 20 = total bet (e.g., 1 chip/line = 20 chips/spin)
Min bet:    1 chip/line (20 chips total)
Max bet:    10 chips/line (200 chips total)
```

**Symbol table (12 symbols, weighted by rarity):**

| Symbol | Type | Weight | 3-match | 4-match | 5-match |
|--------|------|--------|---------|---------|---------|
| YOGURT | Premium | 2 | 20x | 80x | 500x |
| GOLDEN HORSESHOE | Premium | 3 | 15x | 50x | 250x |
| SEVEN | High | 4 | 10x | 30x | 150x |
| STALLION | High | 5 | 8x | 25x | 100x |
| TROPHY | Mid | 7 | 5x | 15x | 50x |
| BELL | Mid | 8 | 4x | 12x | 40x |
| OATS | Low | 10 | 3x | 8x | 20x |
| CHERRY | Low | 12 | 2x | 5x | 15x |
| CARROT | Low | 12 | 2x | 5x | 15x |
| HAY BALE | Low | 14 | 1x | 3x | 10x |
| WILD MARE | Wild | 4 | - | - | - |
| SCATTER (E-008) | Scatter | 3 | - | - | - |

**Payline patterns (20 lines on a 5x3 grid):**

```
Row indices: 0=top, 1=middle, 2=bottom

Line 01: [1,1,1,1,1]  (middle straight)
Line 02: [0,0,0,0,0]  (top straight)
Line 03: [2,2,2,2,2]  (bottom straight)
Line 04: [0,1,2,1,0]  (V shape)
Line 05: [2,1,0,1,2]  (inverted V)
Line 06: [0,0,1,2,2]  (descending slope)
Line 07: [2,2,1,0,0]  (ascending slope)
Line 08: [1,0,0,0,1]  (shallow U)
Line 09: [1,2,2,2,1]  (inverted shallow U)
Line 10: [0,1,1,1,0]  (hat shape)
Line 11: [2,1,1,1,2]  (cup shape)
Line 12: [1,0,1,0,1]  (zigzag up)
Line 13: [1,2,1,2,1]  (zigzag down)
Line 14: [0,1,0,1,0]  (top zigzag)
Line 15: [2,1,2,1,2]  (bottom zigzag)
Line 16: [0,0,1,0,0]  (top bump)
Line 17: [2,2,1,2,2]  (bottom bump)
Line 18: [1,0,0,1,2]  (step down)
Line 19: [1,2,2,1,0]  (step up)
Line 20: [0,1,2,2,1]  (slide right)
```

### 2.2 Wild & Scatter Symbols

**WILD MARE (Wild Symbol):**
- Substitutes for any symbol except SCATTER
- Appears on reels 2, 3, and 4 only (never on first/last reel)
- When part of a winning combination, the payout is doubled (2x wild multiplier)
- CRT terminal flavor: renders with glitch animation when it appears

**SCATTER: E-008 (The Haunted Horse):**
- Pays regardless of payline position (anywhere on the grid)
- 3 scatters: triggers FREE SPINS bonus + 5x total bet
- 4 scatters: triggers FREE SPINS bonus + 20x total bet
- 5 scatters: triggers FREE SPINS bonus + 100x total bet
- Scatter wins are added to payline wins

### 2.3 Bonus Rounds

**Bonus Round 1: FREE SPINS ("The Haunted Gallop")**

Triggered by 3+ E-008 scatter symbols.

```
3 scatters -> 8 free spins
4 scatters -> 15 free spins
5 scatters -> 25 free spins

During free spins:
- All wins multiplied by 3x
- WILD MARE symbols become "STACKED WILDS" (fill entire reel)
- Free spins can retrigger (3+ scatters during free spins = +8 spins, no cap)
- Wager is locked at the triggering spin's bet level
```

**Bonus Round 2: PICK-A-PRIZE ("The Yogurt Vault")**

Triggered by landing 3 YOGURT symbols on payline 1 (center row).

```
Player is shown 12 "yogurt containers" on screen (ASCII art, naturally).
Each container hides one of:
  - Chip prize: 5x to 50x total bet
  - Multiplier: 2x or 3x (applied to total bonus winnings)
  - "ROTTEN YOGURT": ends the bonus round
  - "GOLDEN YOGURT": instant 100x total bet

Player picks containers one at a time until:
  - They hit ROTTEN YOGURT, OR
  - They've picked 5 containers (guaranteed minimum)
  
All prizes accumulated and paid out.
```

**Bonus Round 3: WHEEL OF FORTUNE ("The Glue Factory Roulette")**

Triggered by landing GOLDEN HORSESHOE on reels 1, 3, and 5 simultaneously.

```
ASCII wheel with 12 segments:
  - 4 segments: 10x, 15x, 20x, 25x total bet
  - 3 segments: 30x, 40x, 50x total bet
  - 2 segments: 75x, 100x total bet
  - 1 segment:  200x total bet
  - 1 segment:  "SPIN AGAIN" (guaranteed second spin, prizes stack)
  - 1 segment:  "PROGRESSIVE JACKPOT" (wins current progressive pool)

CRT effect: wheel rendered as ASCII art with phosphor-green glow,
spinning characters, dramatic slowdown with screen flicker.
```

### 2.4 Progressive Jackpot

**How progressive jackpots work in real casinos:**

A small percentage of every bet is siphoned into a shared jackpot pool. The
jackpot grows until one player hits the specific triggering combination (usually
the rarest symbol alignment). After a win, the jackpot resets to a "seed"
amount. Progressive jackpots can be:
- **Standalone**: single machine feeds/wins from its own pool
- **Local**: group of machines in one casino share a pool
- **Wide-area**: machines across multiple locations share a pool

**StallionUSSY implementation: "THE CUMMY POT"**

```
Type:           Server-wide (all players feed the same pool)
Contribution:   2% of every slot spin wager goes to the progressive pool
Seed amount:    1,000 chips (reset value after a jackpot hit)
Trigger:        Landing on "PROGRESSIVE JACKPOT" segment of Glue Factory Roulette
                OR 5x YOGURT on payline 1 with max bet

Jackpot payout: 100% of the current pool to the winner
Display:        Running counter shown on the casino page, updating in real-time
                via WebSocket broadcast. Rendered in gold text with glow effect.
```

**Server-side tracking:**

```go
type ProgressiveJackpot struct {
    ID          string    `json:"id"`
    Pool        int64     `json:"pool"`       // current jackpot amount in chips
    SeedAmount  int64     `json:"seedAmount"` // reset-to value (1000)
    TotalFeeds  int64     `json:"totalFeeds"` // lifetime contributions
    TotalWins   int       `json:"totalWins"`  // number of times jackpot has been hit
    LastWinnerID string   `json:"lastWinnerID,omitempty"`
    LastWinnerName string `json:"lastWinnerName,omitempty"`
    LastWinAmount int64   `json:"lastWinAmount,omitempty"`
    LastWonAt   time.Time `json:"lastWonAt,omitempty"`
    UpdatedAt   time.Time `json:"updatedAt"`
}
```

### 2.5 Return-to-Player (RTP) Design

**Industry context:**

Real-world slot machines operate at 75-99% RTP depending on jurisdiction.
Nevada minimum is 75%, New Jersey 83%. Online slots typically run 92-97%.
"Loose" machines are 95-97%, "tight" machines are 88-92%.

**Recommended RTP for StallionUSSY: 94%**

This is generous enough that players don't feel robbed, but the 6% house edge
ensures a steady chip sink. Since casino chips are ring-fenced and can only be
acquired through Cummies exchange or daily grants, the economy pressure is
well-controlled.

**How to calculate and enforce RTP:**

Rather than trying to weight outcomes to hit exactly 94%, use a hybrid approach:

1. **Symbol weighting**: Configure the virtual reel strips (weighted random
   selection per reel) so that the mathematical expectation across all possible
   outcomes = 94% return.

2. **Verification**: Run a Monte Carlo simulation of 10M+ spins during
   development to verify the actual RTP matches the target.

3. **No dynamic adjustment**: The RTP should be fixed and deterministic based
   on symbol weights. Do NOT adjust outcomes based on a player's recent
   history (that's both manipulative and breaks trust).

**Calculating the weights:**

Each reel has a "virtual strip" of N positions. The symbol at each position
is weighted. Example for a single reel with 64 virtual stops:

```
HAY BALE:    14 stops (21.9%)
CARROT:      10 stops (15.6%)
CHERRY:      10 stops (15.6%)
OATS:         8 stops (12.5%)
BELL:         6 stops ( 9.4%)
TROPHY:       5 stops ( 7.8%)
STALLION:     4 stops ( 6.3%)
SEVEN:        3 stops ( 4.7%)
GOLDEN SHOE:  2 stops ( 3.1%)
YOGURT:       1 stop  ( 1.6%)
WILD MARE:    1 stop  ( 1.6%) [reels 2,3,4 only; 0 on reels 1,5]
E-008:        0 stops  [scatter uses separate roll]
Total:       64 stops
```

Scatter is rolled separately: each reel has a 3.5% chance of producing an E-008
scatter symbol in addition to its normal symbol (overlaid, like real scatter
mechanics). This keeps scatter probability independent from payline outcomes.

### 2.6 Volatility Design

**Definitions:**

- **Low volatility**: Frequent small wins, long play sessions, low risk. RTP is
  reached in a small number of spins. Players rarely go broke quickly.
- **High volatility**: Rare but large wins, short potential sessions, high risk.
  RTP requires many thousands of spins to converge. Players can go broke fast
  OR hit big.
- **Medium volatility**: Balanced mix. This is the sweet spot for engagement.

**StallionUSSY approach: Medium-High volatility**

The game's absurdist tone and degenerate aesthetic suits a machine that can
deliver both "nothing for 20 spins" stretches and explosive jackpot moments.
This matches the existing game's vibe (horses can die in fights, get sent to
glue factories, etc.).

Implementation:

```
Win frequency:     ~30% of spins return something (vs. 25-35% in real slots)
Average win size:  ~1.5x the bet (most wins are small)
Bonus frequency:   ~1 in 120 spins triggers a bonus round
Big win (>50x):    ~1 in 800 spins
Jackpot:           ~1 in 50,000 spins (depends on pool size)

Volatility index:  ~14 (on a 1-20 scale; "medium-high")
Standard deviation: ~5.5x per spin
```

The low-value symbols (HAY BALE, CARROT, CHERRY) provide frequent 1-3x returns
that keep the player's balance oscillating rather than plummeting. The high-value
symbols and bonus rounds create the aspiration peaks.

---

## 3. Poker: Texas Hold'em & Tournaments

### 3.1 Texas Hold'em Rules

**Game flow:**

```
1. BLINDS: Two forced bets rotate clockwise each hand.
   - Small blind: player left of dealer, posts half the minimum bet
   - Big blind: player left of small blind, posts the minimum bet
   - Dealer position marked by "button", rotates each hand

2. PRE-FLOP: Each player dealt 2 hole cards face-down.
   - Betting round: starts with player left of big blind
   - Actions: fold, call (match big blind), raise (increase bet)

3. THE FLOP: 3 community cards dealt face-up.
   - Betting round: starts with first active player left of dealer
   - Actions: check (if no bet), bet, call, raise, fold

4. THE TURN: 1 additional community card dealt face-up (4 total).
   - Betting round: same structure as flop

5. THE RIVER: 1 final community card dealt face-up (5 total).
   - Final betting round

6. SHOWDOWN: If 2+ players remain, best 5-card hand from 7 cards wins.
   - Players can use 0, 1, or 2 of their hole cards + community cards
```

**Betting mechanics:**

```
CHECK:    Pass action (only if no bet in current round)
BET:      Place a wager (first wager in a round)
CALL:     Match the current highest bet
RAISE:    Increase the current bet (minimum raise = size of previous raise)
FOLD:     Surrender cards and forfeit any chips in the pot
ALL-IN:   Bet all remaining chips
```

**Side pots (critical for all-in scenarios):**

When a player goes all-in for less than the current bet, a side pot is created:

```
Example: 3 players, pot = 0
  Player A bets 100 chips
  Player B has only 60 chips, goes all-in for 60
  Player C calls 100

Main pot: 60 x 3 = 180 (A, B, and C eligible)
Side pot: 40 x 2 = 80  (only A and C eligible; B cannot win this)

If B has the best hand: B wins 180, side pot of 80 goes to better of A/C
If A has the best hand: A wins 180 + 80 = 260
```

### 3.2 Hand Rankings (Standard, with Kicker Tiebreakers)

```
Rank  Hand              Description                    Tiebreaker
----  ----              -----------                    ----------
  1   Royal Flush       A-K-Q-J-10 same suit           Split pot (suits don't matter)
  2   Straight Flush    5 consecutive, same suit        Highest card wins
  3   Four of a Kind    4 cards same rank               Higher quad wins; then kicker
  4   Full House        3 of a kind + pair              Higher trips wins; then pair
  5   Flush             5 cards same suit               Compare highest card down
  6   Straight          5 consecutive ranks             Highest card wins (A-low = 5-high)
  7   Three of a Kind   3 cards same rank               Higher trips; then kickers
  8   Two Pair          2 different pairs                Higher pair first; then lower pair; then kicker
  9   One Pair          2 cards same rank               Higher pair; then kickers (3 kickers)
 10   High Card         No combination                  Compare highest card down
```

**Implementation note:** The existing `evaluatePokerHand` function
(`casino.go:773-818`) needs significant upgrades:
- Currently returns a flat score (100-800) with no tiebreaking granularity
- Needs to encode the hand rank + all relevant card values into a comparable
  integer. Standard approach: encode as a 24-bit integer where the top bits
  are the hand rank and lower bits encode card values in tiebreak order.

```go
// Proposed scoring: RRRR_AAAA_BBBB_CCCC_DDDD_EEEE (24-bit)
// R = hand rank (0-9), A-E = tiebreak cards in order of importance
// Example: Two pair Kings and Sevens with 5 kicker
//   R=7 (two pair), A=13(K), B=7(7), C=5(5)
//   Score = (7 << 20) | (13 << 16) | (7 << 12) | (5 << 8)
```

### 3.3 Blinds Structure

**Cash game blinds (fixed):**

| Table Tier | Small Blind | Big Blind | Min Buy-in | Max Buy-in |
|------------|-------------|-----------|------------|------------|
| Foal Table | 1 chip | 2 chips | 40 chips | 200 chips |
| Colt Table | 2 chips | 5 chips | 100 chips | 500 chips |
| Stallion Table | 5 chips | 10 chips | 200 chips | 1000 chips |
| Stud Table | 10 chips | 25 chips | 500 chips | 2500 chips |
| Legendary Table | 25 chips | 50 chips | 1000 chips | 5000 chips |

### 3.4 Tournament Structure (Sit-N-Go)

**How Sit-N-Go (SNG) tournaments work:**

A SNG starts as soon as enough players register (typically 6, 9, or 10). All
players buy in for the same amount and receive equal starting chip stacks.
Blinds escalate on a timer. Play continues until one player has all the chips.
Prize pool is distributed to top finishers (typically top 3).

**StallionUSSY SNG: "RODEO ROUNDUP"**

```
Format:         6-player Sit-N-Go (starts when 6 seats filled)
Buy-in:         50-500 casino chips (set by creator)
Starting stack: 1,500 chips (tournament chips, not real chips)
Blind levels:   Escalate every 5 minutes (real-time) or every 10 hands

Blind Schedule:
  Level 1:  10/20
  Level 2:  15/30
  Level 3:  25/50
  Level 4:  50/100
  Level 5:  75/150
  Level 6:  100/200 + 25 ante
  Level 7:  150/300 + 25 ante
  Level 8:  200/400 + 50 ante
  Level 9:  300/600 + 50 ante
  Level 10: 500/1000 + 100 ante (end-game pressure)

Prize distribution (% of total buy-in pool):
  1st place: 65%
  2nd place: 25%
  3rd place: 10%

Example: 6 players x 100 chip buy-in = 600 chip pool
  1st: 390 chips, 2nd: 150 chips, 3rd: 60 chips
```

**Bubble mechanics:**

The "bubble" is the last position that doesn't pay (4th place in a 6-player
SNG that pays top 3). Near the bubble, rational players tighten up to avoid
being the bubble boy. This creates natural tension.

Implementation: Track `playersRemaining` and `payingPositions`. When
`playersRemaining == payingPositions + 1`, broadcast a "BUBBLE" alert via
WebSocket. Add flavor text:

```
"The bubble trembles. One of you leaves with nothing but shame."
"Four remain. Three get paid. Someone's about to eat glue."
```

### 3.5 Pot Management

```go
type HoldemTable struct {
    ID              string
    Seats           []HoldemSeat      // 2-10 players
    CommunityCards  []PokerCard       // 0-5 cards
    Deck            []PokerCard       // remaining deck
    DealerPos       int               // button position
    ActivePos       int               // current actor
    Phase           string            // "preflop","flop","turn","river","showdown"
    Pots            []SidePot         // main pot + side pots
    CurrentBet      int64             // highest bet in current round
    MinRaise        int64             // minimum legal raise amount
    BlindSmall      int64
    BlindBig        int64
    Ante            int64
    HandNumber      int
}

type HoldemSeat struct {
    UserID      string
    Username    string
    Stack       int64               // chips in front of player
    HoleCards   [2]PokerCard
    BetThisRound int64              // amount bet in current betting round
    HasActed    bool
    IsFolded    bool
    IsAllIn     bool
    IsSittingOut bool
}

type SidePot struct {
    Amount      int64
    EligibleIDs []string           // player IDs who can win this pot
}
```

### 3.6 Timeout Handling

Players who don't act within a time limit are automatically folded (cash games)
or checked/folded (tournaments):

```
Action timeout:     30 seconds per action
Warning at:         10 seconds remaining
Auto-action:        Check if possible, otherwise Fold
Disconnection:      Player is marked "sitting out", auto-folded each hand
Sitting out limit:  3 consecutive hands -> removed from table (cash game)
                    Blinds still posted while sitting out (tournament)
```

### 3.7 Keeping 5-Card Draw

The existing 5-card draw system should remain as a simpler, quicker alternative.
It fills a different niche: fast, low-skill, social. Improvements:

1. Add proper betting: pre-draw bet round + post-draw bet round
2. Add hand rank display during showdown with ASCII art
3. Allow "stand pat" (keep all 5, draw 0) as explicit action
4. Cap discard at 3 cards (standard draw poker rule; or allow 4 with an ace showing)

---

## 4. Economy Design

### 4.1 Preventing Chip Inflation/Deflation

**Current exchange mechanics:**

```
Buy chips:   25 Cummies -> 1 Casino Chip  (expensive to enter)
Cash out:     1 Casino Chip -> 10 Cummies (lossy exit)
```

This 60% loss on cashout (25 in, 10 out) is intentional: it makes casino
chips a one-way funnel. Players are meant to spend chips, not launder them
back into Cummies efficiently. This is a strong anti-inflation mechanic.

**Chip sources (inflow):**
- Daily grant: 40 chips/day (floor)
- Cummies exchange: uncapped (but protected by 500 Cummy floor)
- Slot wins: RTP 94% means net outflow of 6% per spin
- Poker wins: zero-sum (player vs player, no house injection)
- Bonus rounds: funded from the 6% house edge, not new chips

**Chip sinks (outflow):**
- Slot house edge: 6% of all wagers
- Progressive jackpot contribution: 2% of all slot wagers (subset of house edge)
- Poker rake: 5% of each pot, capped at 25 chips (new mechanic)
- Tournament fees: 10% of buy-in goes to house (e.g., 100+10 buy-in)

**Balance calculation:**

```
Daily inflow per player:  ~40 chips (grant) + ~0-200 chips (exchange)
Daily outflow per player: ~6% of total wagered

If a player spins 200 chips worth of slots:
  Expected loss: 200 * 0.06 = 12 chips to house
  Progressive: 200 * 0.02 = 4 chips to jackpot pool

If a player plays 10 poker hands at Foal Table:
  Average pot: ~30 chips -> rake: 1.5 chips/hand -> 15 chips/session

Total daily sink: ~12 + 15 = 27 chips for a moderate player
Total daily source: 40 chips (grant)
Net: +13 chips/day (slow accumulation for casual players)
```

This means casual players slowly accumulate chips from daily grants alone,
which is correct -- the casino should feel like a fun side-activity, not a
punishing money trap.

**Preventing jackpot accumulation runaway:**

Cap the progressive pool at 50,000 chips. Once the cap is reached, the 2%
contribution is paused (or redirected to a "mini jackpot" that triggers more
frequently at smaller amounts).

### 4.2 House Edge Across Games

| Game | House Edge | Mechanism |
|------|-----------|-----------|
| Slots (base game) | 6% | Symbol weighting, ~94% RTP |
| Slots (free spins) | ~3% | Reduced edge during bonus (more generous) |
| Poker (cash game) | 5% rake | Taken from each pot, capped at 25 chips |
| Poker (tournament) | 10% fee | Entry fee surcharge (e.g., 100+10) |
| 5-Card Draw | 5% rake | Same as Hold'em cash |

### 4.3 Daily Limits and Responsible Gambling

Even in a virtual currency game with no real money, responsible gambling
mechanics serve two purposes: (1) they prevent the casino from becoming a
mandatory grind, and (2) they add thematic comedy to the CRT aesthetic.

**Recommended limits:**

```go
const (
    casinoDailySlotsSpinCap     = 200   // max spins per day
    casinoDailyPokerHandsCap    = 100   // max poker hands per day
    casinoDailyChipLossCap      = 500   // if net losses exceed this, warn player
    casinoDailyExchangeCap      = 400   // max chips purchasable from Cummies/day
    casinoSessionTimerMinutes   = 120   // after 2 hours, show "touch grass" warning
)
```

**"Touch Grass" system (comedic responsible gambling):**

After hitting any daily cap, display a CRT-style warning:

```
 ==================================================
  !! CASINOUSSY WELLNESS CHECK !!
 --------------------------------------------------
  You've been gambling for 2 hours straight.
  
  Your horses miss you.
  Your stable smells weird.
  Dr. Mittens filed a wellness complaint.
  
  The casino will still be here tomorrow.
  Maybe go train a horse or something.
 ==================================================
  [CONTINUE ANYWAY]  [RETURN TO STABLE]
```

The "CONTINUE ANYWAY" button works but is labeled with increasingly
guilt-trippy text each time: "I DON'T CARE ABOUT MY HORSES", then
"GAMBLING IS MY PERSONALITY", then the button just says "HELP".

### 4.4 Reward Loops

**Tier 1: Immediate feedback (every spin/hand)**
- Slot spin result with animated ASCII reels
- Win/loss amount with appropriate CRT effects (flashing for wins)
- Near-miss highlighting (2 matching symbols + close miss shown dramatically)

**Tier 2: Session milestones (every 15-30 minutes)**
- "Hot streak" counter: 3+ wins in a row triggers bonus multiplier (1.5x on next win)
- "Cold streak" mercy: after 15 consecutive losses, next win gets bonus 2x
- First bonus round of the day: extra 5 free spins

**Tier 3: Daily rewards**
- Daily chip grant (existing: 40 chips)
- "First Win of the Day" bonus: first slot win pays double
- Daily poker challenge: "Win a hand with a flush" -> 50 chip bonus

**Tier 4: Weekly/Seasonal**
- Weekly leaderboard: top 10 slot winners and poker earners
- "Casino Season Pass": cumulative play milestones unlock cosmetic rewards
  (custom table themes, special reel skins, table name colors)
- Monthly "MEGA RODEO" tournament: higher stakes, special prizes

**Anti-predatory design principles:**
1. Never gate core game progression behind casino wins
2. Casino chips cannot buy horses, training, or race entries
3. Daily grants ensure free-to-play players can participate
4. No "premium currency" or real-money hooks
5. All probabilities are transparent (show RTP on the help screen)
6. Loss streaks are bounded by daily chip loss caps

---

## 5. Horse-Theme Integration

### 5.1 Horse-Themed Slot Symbols

The symbol table above already uses horse-themed symbols. Additional thematic
touches:

**Reel animations (ASCII art):**
```
Normal symbols:     Static ASCII icon
WILD MARE:          Glitching/flickering horse silhouette
E-008 SCATTER:      Red-tinted, scanline distortion (haunted horse)
YOGURT:             Dripping animation effect
GOLDEN HORSESHOE:   Gold/amber glow text shadow
```

**Win narration (replaces generic "paid out" message):**
```
Small win:    "A modest trot across the finish line."
Medium win:   "Your stallion breaks from the pack! Chips rain down."
Big win:      "THUNDERING HOOVES! The crowd goes absolutely feral."
Bonus entry:  "E-008 has entered the arena. The lights are flickering."
Jackpot:      "THE CUMMY POT ERUPTS. THE ENTIRE SERVER HEARD THAT."
```

### 5.2 Bonus Round: Mini Horse Race

**"PHOTO FINISH" Bonus (Alternative bonus round concept):**

Triggered by 3 STALLION symbols on reels 1, 2, 3.

```
5 randomly generated horses appear (using existing race engine traits):
  - Each horse has visible stats (SPD, STM, TMP)
  - Player picks one horse
  - A compressed mini-race plays out (5 ticks, ASCII animation)
  - Payout based on finish position:
    1st: 25x bet
    2nd: 10x bet
    3rd: 5x bet
    4th: 2x bet
    5th: 0x (loss)

This ties directly into the core game's race simulation engine
(racussy), giving players a preview of how genetics affect outcomes.
```

### 5.3 Poker Table Names & Themes

**Preset horse-themed poker rooms:**

| Table Name | Flavor | Buy-in Range |
|------------|--------|-------------|
| The Foaling Pen | "Where baby degenerates are born" | 10-50 chips |
| The Yogurt Room | "Suspiciously sticky cards" | 50-200 chips |
| Thunderussy Arena | "Storm outside, storm inside" | 100-500 chips |
| The Stud Parlor | "Only the finest bloodlines gamble here" | 200-1000 chips |
| E-008's Table | "The chair across from you is occupied. You just can't see by what." | 500-2500 chips |
| Dr. Mittens' Office | "The house always wins, and the house is a horse veterinarian" | 1000-5000 chips |

### 5.4 "Wild Stallion" Poker Variant

A thematic 5-card draw variant unique to StallionUSSY:

```
Rules modifications:
- One random card per hand is designated the "Wild Stallion" (wild card)
- The wild card is revealed to all players after the initial deal
- The wild card changes each hand (e.g., "This hand's Wild Stallion is: 7s")
- If a player's final hand contains the Wild Stallion card naturally,
  their payout is doubled
- Standard poker hand rankings apply, with wild card enabling 5-of-a-kind
  (ranked above straight flush)

Hand rankings (Wild Stallion variant):
  1. Five of a Kind (only possible with Wild Stallion)
  2. Royal Flush
  3. Straight Flush
  4. Four of a Kind
  5-10. Standard rankings
```

### 5.5 Horse Trait Bonuses

Certain horse traits could unlock casino perks:

```
Trait: "Lucky Hoof"        -> +5% slot RTP when this horse is your "mascot"
Trait: "Card Sharp"         -> See one opponent's card in 5-card draw
Trait: "Yogurt Sommelier"   -> Yogurt symbols count as WILD in slots
Trait: "Factory Escapee"    -> Double progressive jackpot contribution chance
Trait: "E-008's Favorite"   -> Scatter symbols appear 1.5x more frequently
```

These bonuses would be **small** and **cosmetic-adjacent** -- they shouldn't
break the economy but they create a reason to care about horse traits beyond
racing. Requires a "set active mascot" UI in the casino.

---

## 6. Implementation Priority

### Phase 1: Slots V2 (Highest Impact, Moderate Effort)

1. Upgrade to 5-reel, 20-payline system with new symbol table
2. Add WILD MARE and E-008 SCATTER symbols
3. Implement weighted virtual reel strips targeting 94% RTP
4. Add Free Spins bonus round ("The Haunted Gallop")
5. Run Monte Carlo verification of RTP
6. Add daily spin cap and "Touch Grass" system

**Estimated effort:** 3-5 days backend, 2-3 days frontend animation

### Phase 2: Progressive Jackpot + Pick-a-Prize

1. Add progressive jackpot pool (server-wide state)
2. Implement "The Yogurt Vault" pick-a-prize bonus
3. Implement "Glue Factory Roulette" wheel bonus
4. Add jackpot counter to casino page (WebSocket broadcast)
5. Add "Photo Finish" mini-race bonus

**Estimated effort:** 2-3 days backend, 2-3 days frontend

### Phase 3: Texas Hold'em

1. Implement full Hold'em game state machine (preflop through showdown)
2. Upgrade hand evaluation for 7-card best-5 with kicker tiebreaking
3. Implement proper betting rounds (check/bet/call/raise/fold/all-in)
4. Implement side pot calculation
5. Add blind rotation and dealer button
6. Add timeout/auto-fold system
7. Add 5 cash game table tiers

**Estimated effort:** 5-8 days backend, 3-5 days frontend

### Phase 4: Tournaments

1. Implement SNG lobby and registration
2. Add blind escalation timer
3. Add bubble alerts and elimination handling
4. Add prize pool distribution
5. Add tournament history and leaderboard

**Estimated effort:** 3-5 days backend, 2-3 days frontend

### Phase 5: Economy Polish & Integration

1. Add poker rake system
2. Implement daily challenges
3. Add "Wild Stallion" draw poker variant
4. Add horse mascot/trait casino bonuses
5. Add weekly leaderboards
6. Daily exchange caps
7. Casino achievement badges

**Estimated effort:** 3-4 days total

---

## Appendix A: Slot RTP Verification Script

To verify the slot machine's RTP before deployment, run a Monte Carlo
simulation:

```go
func verifySlotRTP(iterations int) {
    totalWagered := int64(0)
    totalPaid := int64(0)
    bonusTriggers := 0
    
    for i := 0; i < iterations; i++ {
        bet := int64(20) // 1 chip per line * 20 lines
        totalWagered += bet
        
        grid := spinGrid()       // generate 5x3 grid using weighted reels
        paylineWins := evaluatePaylines(grid, bet)
        scatterWins := evaluateScatter(grid, bet)
        
        totalPaid += paylineWins + scatterWins
        
        if countScatters(grid) >= 3 {
            bonusTriggers++
            totalPaid += simulateFreeSpins(grid, bet)
        }
    }
    
    rtp := float64(totalPaid) / float64(totalWagered) * 100
    fmt.Printf("RTP after %d spins: %.2f%%\n", iterations, rtp)
    fmt.Printf("Bonus trigger rate: 1 in %.0f spins\n",
        float64(iterations)/float64(bonusTriggers))
}
```

Run with 10,000,000+ iterations. Target: 93.5-94.5% RTP.

## Appendix B: Poker Hand Evaluation Upgrade

The current evaluator needs to handle 7-card hands (pick best 5 from 7):

```go
// evaluateBestHand picks the best 5-card hand from 7 cards.
// There are C(7,5) = 21 possible 5-card combinations.
func evaluateBestHand(cards []PokerCard) (int, string) {
    bestScore := -1
    bestLabel := ""
    
    // Generate all 21 combinations of 5 from 7
    for i := 0; i < len(cards); i++ {
        for j := i + 1; j < len(cards); j++ {
            // Exclude cards[i] and cards[j], evaluate remaining 5
            hand := make([]PokerCard, 0, 5)
            for k, c := range cards {
                if k != i && k != j {
                    hand = append(hand, c)
                }
            }
            score, label := evaluatePokerHand(hand)
            if score > bestScore {
                bestScore = score
                bestLabel = label
            }
        }
    }
    
    return bestScore, bestLabel
}
```

## Appendix C: Side Pot Algorithm

```go
func calculateSidePots(seats []HoldemSeat) []SidePot {
    // Collect all-in amounts and sort ascending
    type contrib struct {
        userID string
        amount int64 // total contributed to pot this hand
        allIn  bool
    }
    
    // ... gather contributions, sort by amount ...
    
    // For each all-in threshold, create a side pot:
    // Main pot: everyone contributes up to the lowest all-in amount
    // Side pot 1: remaining players contribute up to next all-in amount
    // etc.
    
    var pots []SidePot
    prevThreshold := int64(0)
    
    for _, threshold := range allInThresholds {
        pot := SidePot{}
        for _, p := range activePlayers {
            contribution := min(p.totalContrib, threshold) - prevThreshold
            if contribution > 0 {
                pot.Amount += contribution
                pot.EligibleIDs = append(pot.EligibleIDs, p.userID)
            }
        }
        pots = append(pots, pot)
        prevThreshold = threshold
    }
    
    return pots
}
```
