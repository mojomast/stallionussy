# StallionUSSY: Multiplayer Engagement Feature Research

> 22 prioritized feature proposals to make horse breeding, racing, and trading
> more engaging in multiplayer. Based on full codebase audit of the Go backend
> and single-page frontend.

---

## Executive Summary

The current system has strong foundations: real-time WebSocket infrastructure,
a tick-based race engine with narrative generation, a genetics/breeding system,
ELO rankings, 63 traits across 5 rarities, tournaments, achievements, and a
deflationary "cummies" economy with a 2% burn on stud transactions.

What's **missing** are the systems that turn individual play into a multiplayer
experience: there's no betting, no guilds, no leaderboard UI, no daily
engagement hooks, no spectator interaction, no auction house, and no prestige
loop. The proposals below fill these gaps.

---

## Category 1: Social & Competitive Features

### 1. Race Betting System
**What**: Players wager cummies on race outcomes. Pari-mutuel pool (house takes
5% rake, burned for deflation). Live odds update as bets come in via WebSocket.
Show/Place/Win bet types.

**Why**: Betting is the #1 missing engagement driver. It gives spectators a
reason to care about every race, not just their own horses. Creates excitement
even when your horse isn't running.

**Complexity**: Medium — new `Bet` model, pool calculation logic, integrate
into post-race settlement in `server.go`'s race pipeline. WebSocket broadcast
of odds is straightforward given existing hub.

**Priority**: **MUST-HAVE** — this single feature transforms passive spectating
into active engagement.

**Builds on**: WebSocket hub broadcast (`commussy.go`), post-race purse
distribution (`server.go`), cummies transfer (`stableussy.go`).

---

### 2. Leaderboard System with Multiple Rankings
**What**: Persistent leaderboards beyond raw ELO sort:
- Richest Stables (cummies balance)
- Winningest Stables (career race wins)
- Top ELO Horses (already have data, need UI + API)
- Best Breeders (avg Sappho Scale of offspring)
- Tournament Champions (lifetime tournament points)
- Weekly/Monthly reset variants for all of the above

**Why**: Leaderboards create aspirational goals and social comparison. Weekly
resets keep the competition fresh for newer players.

**Complexity**: Low — all data already exists in the DB. Need aggregation
queries, 2-3 new API endpoints, frontend tab.

**Priority**: **MUST-HAVE** — low effort, high visibility.

**Builds on**: ELO system (`marketussy.go`), stable balances, race history
(`tournussy.go`), Sappho Scale (`marketussy.go`).

---

### 3. Syndicates (Guilds/Teams)
**What**: Players form syndicates (max 8 members). Syndicate-level features:
- Shared syndicate stable for co-owned horses
- Syndicate leaderboard (aggregate ELO of top 5 horses)
- Syndicate chat channel
- Syndicate tournaments (inter-syndicate competition)
- Syndicate treasury with deposit/withdraw permissions

**Why**: Social bonds are the strongest retention mechanism. Syndicates give
players a reason to log in even when their own horses are resting — their
syndicate needs them.

**Complexity**: High — new models (Syndicate, SyndicateMember, SyndicateHorse),
permission system, treasury logic, new chat channel routing.

**Priority**: **NICE-TO-HAVE** — high impact but high effort. Ship betting and
leaderboards first.

**Builds on**: Chat channels could extend `commussy.go` hub, stable/horse
ownership model in `models.go`.

---

### 4. Rivalry System
**What**: When two stables' horses race against each other 3+ times, a
"rivalry" is automatically declared. Rivalry races get bonus purses (+20%),
special narrative flavor text, and a head-to-head record tracker. Players can
also manually declare rivalries (costs cummies, opponent gets notified).

**Why**: Personal stakes. Named opponents are more engaging than anonymous
competition. The narrative system already generates dramatic text — rivalries
give it persistent context.

**Complexity**: Low-Medium — track pairwise race history (new table or
computed from race results), rivalry declaration model, modify purse calc and
narrative generation.

**Priority**: **NICE-TO-HAVE** — great flavor, not blocking other features.

**Builds on**: Race history tracking, narrative generation (`racussy.go`),
purse distribution.

---

### 5. Spectator Reactions
**What**: During live races, spectators can send quick reactions (e.g., boost
cheers for a horse). Aggregate reaction counts display in real-time. Top-cheered
horse gets a small "crowd favorite" narrative bonus. Reactions cost 1 cummy each
(burned — additional deflation).

**Why**: Makes spectating interactive rather than passive. Even small
participation creates investment in outcomes.

**Complexity**: Low — new WS message type, aggregate counter per horse per race,
minor narrative tweak.

**Priority**: **NICE-TO-HAVE** — quick win once betting exists.

**Builds on**: WebSocket broadcast, race tick system, narrative engine.

---

## Category 2: Economy Depth

### 6. Live Auction House
**What**: Replace (or supplement) the current fixed-price stud listings with a
timed auction system. Auctions last 1-5 minutes with real-time bid updates via
WebSocket. Anti-snipe extension (30s added if bid in last 15s). Auction types:
- Standard ascending bid
- Dutch auction (price drops over time)
- Blind auction (sealed bids, highest wins)

**Why**: Auctions create urgency, social interaction, and price discovery. The
current one-shot stud market has no drama. Auctions are events in themselves.

**Complexity**: Medium — new Auction model, bid tracking, timer management
(server-side goroutine), WS broadcast of bids, settlement logic.

**Priority**: **MUST-HAVE** — economy needs more interactive price discovery.

**Builds on**: Stud marketplace (`marketussy.go`), WebSocket hub, cummies
transfer.

---

### 7. Sponsorship Contracts
**What**: NPCs or other players can sponsor horses. Sponsor pays upfront
cummies; in return they get a % of the horse's race earnings for N races.
Sponsored horses display sponsor name in race narratives. High-ELO horses
attract better sponsor offers.

**Why**: Creates a secondary economy layer. Breeding a good horse now has
value beyond racing it yourself — you can sell sponsorship rights. Also gives
cash-poor stables a way to fund operations.

**Complexity**: Medium — new SponsorContract model, modify purse distribution
to split earnings, NPC offer generation logic.

**Priority**: **NICE-TO-HAVE** — interesting depth but not essential for launch.

**Builds on**: Purse distribution, ELO system, narrative generation.

---

### 8. Insurance System
**What**: Pay a premium (% of horse value based on Sappho Scale + ELO) to
insure a horse against injury during training or racing. If the horse gets
injured, insurance pays out a lump sum. Insurance companies are NPC-run with
odds that shift based on the horse's fatigue and injury history.

**Why**: Adds risk management decisions. Players must weigh insurance cost
against training aggressiveness. Creates interesting choices around the
existing fatigue/injury system.

**Complexity**: Low-Medium — new InsurancePolicy model, premium calculation,
payout on injury event (hook into training injury logic in `trainussy.go`).

**Priority**: **STRETCH** — fun flavor but not a core engagement driver.

**Builds on**: Training injury system (`trainussy.go`), fatigue mechanics,
Sappho Scale valuation.

---

### 9. Breeding Contracts
**What**: Formal contracts where one player offers their stallion for breeding
with another player's mare. Terms specify: stud fee, offspring ownership split
(e.g., owner of stallion gets pick of the first foal), or revenue sharing on
future earnings. Contracts are enforceable by the system.

**Why**: The current stud market is buy-only. Breeding contracts enable
collaboration between players with complementary bloodlines without requiring
full horse sales.

**Complexity**: Medium — new BreedingContract model with terms, escrow for
stud fees, offspring assignment logic, integrate with breeding in `genussy.go`.

**Priority**: **NICE-TO-HAVE** — meaningful economy depth for engaged players.

**Builds on**: Stud marketplace, breeding system (`genussy.go`), trade system.

---

## Category 3: Engagement Loops

### 10. Daily Challenge System
**What**: 3 daily challenges refreshed at midnight UTC:
- Race challenges: "Win a race in Swamp weather", "Place top 3 with a horse
  under 1300 ELO"
- Training challenges: "Complete 5 training sessions", "Train a horse to
  fatigue > 80 without injury"
- Economy challenges: "Buy a horse at auction", "Earn 500 cummies from races"

Completing all 3 gives a bonus reward. 7-day streak gives a rare trait scroll.

**Why**: Daily challenges are the proven engagement loop. They give returning
players an immediate goal and encourage trying different game systems.

**Complexity**: Low-Medium — challenge template system, daily rotation logic,
completion tracking per stable, reward distribution.

**Priority**: **MUST-HAVE** — fundamental retention mechanic.

**Builds on**: All existing systems (racing, training, economy) provide the
challenge targets. Achievement system (`tournussy.go`) provides pattern for
tracking.

---

### 11. Weekly Tournament Seasons
**What**: Formalize tournaments into weekly seasons. Monday-Saturday: qualifier
races accumulate points. Sunday: top 16 stables compete in a championship
tournament. Season rewards: cummies, exclusive traits, cosmetic titles.
End-of-season leaderboard snapshot preserved as historical record.

**Why**: Creates a shared weekly rhythm for the entire player base. Everyone
is working toward the same Sunday event, creating water-cooler moments.

**Complexity**: Medium — extend tournament system with season model, qualifier
point tracking, automated scheduling, reward tier system.

**Priority**: **MUST-HAVE** — tournaments already exist but lack structure.

**Builds on**: Tournament system (`tournussy.go`), ELO rankings, purse
distribution.

---

### 12. Limited-Time Events
**What**: Periodic special events (every 2-4 weeks) with unique mechanics:
- **Chaos Derby**: All horses get random cursed traits for the event
- **Genetic Lottery**: Breeding during the event has +50% mutation chance
- **Gold Rush**: Triple purses but 2x injury risk
- **Ghost Race**: Only horses with anomalous/cursed traits can enter
- **Legacy Stakes**: Only horses 3+ generations deep can compete

Each event lasts 48-72 hours with exclusive rewards.

**Why**: FOMO + novelty. Events break routine and create shared memorable
moments. The existing trait and weather systems provide rich levers to pull.

**Complexity**: Medium — event scheduler, modifier system (temporary global
modifiers to race/breeding params), event-specific reward distribution.

**Priority**: **NICE-TO-HAVE** — depends on daily challenges and tournament
seasons being in place first.

**Builds on**: Trait system, weather modifiers, race engine, breeding system.

---

### 13. Login Streak & Daily Rewards
**What**: Track consecutive daily logins. Escalating rewards:
- Day 1-6: Small cummies bonus (50, 75, 100, 125, 150, 200)
- Day 7: Rare trait scroll OR mystery horse egg
- Day 14: Legendary trait scroll
- Day 30: Exclusive "Dedicated Breeder" title + anomalous trait scroll

Streak resets on miss. Display streak prominently in UI.

**Why**: Simplest possible retention mechanic. Low effort, proven effective.
Gets players to at least open the app daily, where other systems can hook them.

**Complexity**: Low — last_login timestamp tracking, streak counter on stable
model, reward distribution on login.

**Priority**: **MUST-HAVE** — trivial to implement, meaningful for retention.

**Builds on**: Auth system (`authussy.go`), stable model, trait assignment.

---

## Category 4: Player Interaction

### 14. Bounty Board
**What**: Players post bounties on the board: "Beat [Horse Name] in a race —
reward: 500 cummies." Any player who fulfills the condition in a subsequent
race collects the bounty (cummies held in escrow when posted). Bounties can
target: beating a specific horse, winning with a specific trait, winning in
specific weather, etc.

**Why**: Creates player-driven objectives and social dynamics. Rivalries emerge
organically. Players with dominant horses become targets, creating natural
balancing pressure.

**Complexity**: Medium — Bounty model, escrow system, condition evaluation
after each race (pattern matches achievement checking), expiration logic.

**Priority**: **NICE-TO-HAVE** — great social feature, but needs a healthy
player base to be meaningful.

**Builds on**: Achievement condition checking pattern, cummies escrow,
post-race pipeline.

---

### 15. Horse Gifting & Lending
**What**: Gift a horse to another stable (permanent transfer, no cummies
exchanged — or with optional gift cummies attached). Lend a horse for N races
(horse returns automatically after N races, earnings split per agreement).

**Why**: Lending enables cooperation without permanent loss. New players can
borrow experienced horses. Gifting enables generosity/mentorship dynamics.

**Complexity**: Low — gifting is just a transfer with price=0. Lending needs a
LoanAgreement model, race counter, auto-return logic, earnings split in purse
distribution.

**Priority**: **NICE-TO-HAVE** — lending is particularly interesting for
syndicate play.

**Builds on**: Trade system, horse ownership, purse distribution.

---

### 16. Sabotage & Dirty Tricks
**What**: Before a race, players can spend cummies on dirty tricks targeting
opponents:
- **Laxative in the feed** (5% speed debuff, 200 cummies)
- **Spooky scarecrow** (+10% panic chance, 150 cummies)
- **Bribe the announcer** (negative narrative commentary, 50 cummies — cosmetic only)
- **Grease the track** (random horse slips, 300 cummies)

Target player is notified after the race. Can be investigated/countered with
"security" upgrades to your stable.

**Why**: Comedy gold that fits the tone perfectly. Creates interpersonal drama
and retaliation cycles. The existing chaos events in the race engine make this
natural.

**Complexity**: Medium — pre-race sabotage submission, modifier injection into
race engine, notification system, counter-mechanic (stable security level).

**Priority**: **NICE-TO-HAVE** — high comedy value, moderate complexity.

**Builds on**: Race modifier system (weather already modifies stats), chaos
events, narrative system, notification via chat.

---

### 17. Player-to-Player Challenges
**What**: Direct challenge system: "I challenge [Player] — my [Horse A] vs
your [Horse B], 1000 cummies side bet, Desert track, Tornado weather." Both
players must accept. Race runs immediately as a private 1v1 (or small field
with NPC fillers). Results broadcast to all players in chat.

**Why**: On-demand PvP with stakes. No waiting for scheduled races. Personal
grudge matches are inherently compelling.

**Complexity**: Low-Medium — challenge model (sender, receiver, terms, status),
acceptance flow, trigger race with specific params, side bet escrow.

**Priority**: **MUST-HAVE** — direct competition is the heart of multiplayer.

**Builds on**: Race engine (already supports arbitrary horse lists), chat
commands (extend `/challenge` command), cummies escrow.

---

## Category 5: Content Progression

### 18. Prestige System (Stable Prestige)
**What**: After reaching certain milestones (e.g., 50 career wins, 10
tournament top-3s, breed a 10+ Sappho Scale horse), players can "prestige"
their stable. Prestige resets cummies balance to 1000 but grants:
- Prestige tier badge (Bronze → Silver → Gold → Diamond → Obsidian)
- +5% passive cummies earnings per prestige level
- Access to prestige-only traits (1 new trait per tier)
- Prestige-only tournaments
- Legacy bonus: all future horses start with +1 trait slot

**Why**: Gives endgame players a reason to reset and replay. The badge creates
social status. Prestige-only content creates aspiration for mid-game players.

**Complexity**: Medium — prestige model, milestone checking, stable reset
logic, prestige modifier integration into earnings and trait assignment.

**Priority**: **NICE-TO-HAVE** — important for long-term retention but not
needed at launch.

**Builds on**: Achievement system, stable management, trait system, purse
distribution.

---

### 19. Rare Collectible Horses (Mythics)
**What**: Extremely rare horses that can only be obtained through special means:
- **Mutation Jackpot**: 0.1% chance during breeding when MUT gene is
  homozygous dominant (AA) on both parents
- **Tournament Grand Prize**: Win 3 consecutive weekly championships
- **Community Event Reward**: Limited to event participants
- **Ancient Bloodline Discovery**: Breed a horse with 5+ generation pedigree
  where all ancestors had ELO > 1500

Mythic horses have unique visual identifiers, 4 trait slots (instead of max 3),
and a guaranteed anomalous trait.

**Why**: Chase items. The mere existence of mythics makes breeding exciting
because there's always a chance. They become status symbols and conversation
pieces.

**Complexity**: Medium — mythic flag on horse model, special breeding check in
`genussy.go`, conditional trait assignment, UI treatment.

**Priority**: **NICE-TO-HAVE** — adds aspiration layer to breeding.

**Builds on**: Genetics system, trait system, pedigree tracking, breeding.

---

### 20. Dynasty Legacy System
**What**: Extend the existing pedigree system. When a horse retires, its
"legacy points" (based on career wins, ELO peak, offspring quality) are
permanently added to the stable's dynasty score. Dynasty score unlocks:
- Tier 1 (100 pts): Name color in chat
- Tier 2 (500 pts): Custom stable motto displayed in races
- Tier 3 (1000 pts): +1 max horse capacity
- Tier 4 (2500 pts): Access to "Heritage Races" (legacy-only events)
- Tier 5 (5000 pts): Bloodline Aura — descendants of retired legends get
  a small passive stat bonus

**Why**: Makes retirement meaningful rather than just losing a horse. Every
horse's career contributes to a permanent legacy, giving long-term purpose.

**Complexity**: Low-Medium — dynasty score calculation (pedigreussy already
has dynasty scoring), tier threshold checking, reward integration.

**Priority**: **NICE-TO-HAVE** — the pedigree system already does half of this.

**Builds on**: Pedigree system (`pedigreussy.go`), dynasty scoring, retirement
logic (`trainussy.go`), achievement pattern.

---

### 21. Trait Crafting / Fusion
**What**: Sacrifice two horses with specific trait combinations to create a
"Trait Tome" that can be applied to any horse. Fusion recipes:
- 2 common traits of same type → 1 rare trait
- 2 rare traits → 1 legendary trait (50% success, 50% get nothing)
- 1 legendary + 1 cursed → 1 anomalous trait (25% success)

Failed fusions still consume the horses. Recipes discoverable through
experimentation (community knowledge sharing).

**Why**: Creates a trait economy and gives value to otherwise mediocre horses.
The sacrifice mechanic creates difficult decisions. Community recipe discovery
encourages social interaction.

**Complexity**: Medium — fusion recipe system, trait tome item model, sacrifice
flow, probability calculation, application logic.

**Priority**: **STRETCH** — interesting but complex. Better after core systems
stabilize.

**Builds on**: 63 existing traits with rarity tiers, horse management.

---

## Category 6: Real-Time Multiplayer Design

### 22. Real-Time vs. Async Decision Matrix

This isn't a single feature but an architectural guide for implementing the
above features correctly.

#### Must Be Real-Time (WebSocket)
| Feature | Reason |
|---------|--------|
| Race simulation + narrative | Already implemented. Tick-by-tick drama. |
| Betting odds updates | Odds must reflect all bets instantly for fair play |
| Auction bids | Anti-snipe + urgency requires sub-second updates |
| Spectator reactions | Aggregate counts need instant feedback |
| Player challenges | Accept/reject flow should be immediate |
| Chat (all types) | Already implemented. Core social fabric. |
| User presence (online list) | Already implemented. Social awareness. |

#### Can Be Async (REST API with polling or next-page-load)
| Feature | Reason |
|---------|--------|
| Leaderboards | Update every 5 minutes, not per-event |
| Daily challenges | Check on login/action, no real-time needed |
| Login streaks | Check on auth, update on login |
| Bounty board | Post/browse at leisure, notify on completion |
| Sponsorship contracts | Negotiate over hours, not seconds |
| Breeding contracts | Complex terms need careful review |
| Insurance | Purchase decision, not time-critical |
| Prestige/Dynasty | Progress bar, check on milestone events |
| Trait fusion | Deliberate crafting action |
| Gifting/Lending | Offer/accept flow like existing trades |
| Sabotage | Pre-race submission window, not live |

#### Hybrid (REST + WS notification)
| Feature | Reason |
|---------|--------|
| Trade offers | REST to create, WS to notify recipient |
| Bounty completion | REST to post, WS to announce fulfillment |
| Tournament season | REST for standings, WS for live championship |
| Rivalry declaration | REST to create, WS to notify + in-race effects |
| Syndicate updates | REST for management, WS for treasury/chat |

**Key WebSocket Architecture Notes**:
- The existing `commussy.go` hub broadcasts to all clients. For features like
  private challenges or syndicate chat, add **topic-based subscription** (client
  subscribes to channels like `race:{id}`, `syndicate:{id}`, `auction:{id}`).
- Current rate limiting (500ms per chat message) should be extended per-feature:
  betting should allow rapid bets, reactions need even faster throughput.
- Consider adding a message queue (even in-memory) for ordered event processing
  of bets and auction bids to prevent race conditions on the cummies balance.

---

## Implementation Priority Roadmap

### Phase 1: Foundation (Week 1-2) — "Give them reasons to watch"
| # | Feature | Complexity | Impact |
|---|---------|-----------|--------|
| 1 | Race Betting System | Medium | Critical |
| 2 | Leaderboard System | Low | High |
| 13 | Login Streak & Daily Rewards | Low | High |
| 17 | Player-to-Player Challenges | Low-Medium | High |

**Rationale**: Betting transforms spectating. Leaderboards create goals. Login
streaks create habit. Challenges create direct PvP. Together these four features
make "multiplayer" actually feel multiplayer.

### Phase 2: Structure (Week 3-4) — "Give them rhythm"
| # | Feature | Complexity | Impact |
|---|---------|-----------|--------|
| 10 | Daily Challenge System | Low-Medium | High |
| 11 | Weekly Tournament Seasons | Medium | High |
| 6 | Live Auction House | Medium | High |
| 5 | Spectator Reactions | Low | Medium |

**Rationale**: Daily and weekly loops create return visits. Auctions create
economic drama. Spectator reactions add fun during races.

### Phase 3: Depth (Week 5-6) — "Give them community"
| # | Feature | Complexity | Impact |
|---|---------|-----------|--------|
| 3 | Syndicates | High | High |
| 4 | Rivalry System | Low-Medium | Medium |
| 14 | Bounty Board | Medium | Medium |
| 16 | Sabotage & Dirty Tricks | Medium | Medium |

**Rationale**: Social structures (syndicates, rivalries) and interpersonal
mechanics (bounties, sabotage) deepen the multiplayer experience.

### Phase 4: Endgame (Week 7-8) — "Give them purpose"
| # | Feature | Complexity | Impact |
|---|---------|-----------|--------|
| 18 | Prestige System | Medium | Medium |
| 19 | Rare Collectible Horses | Medium | Medium |
| 20 | Dynasty Legacy System | Low-Medium | Medium |
| 12 | Limited-Time Events | Medium | High |

**Rationale**: Endgame loops for retention. Prestige + dynasty + mythics
give veteran players aspirational goals. Events keep things fresh.

### Phase 5: Polish (Week 9+) — "Give them options"
| # | Feature | Complexity | Impact |
|---|---------|-----------|--------|
| 7 | Sponsorship Contracts | Medium | Low-Medium |
| 8 | Insurance System | Low-Medium | Low |
| 9 | Breeding Contracts | Medium | Medium |
| 15 | Horse Gifting & Lending | Low | Low-Medium |
| 21 | Trait Crafting / Fusion | Medium | Medium |

**Rationale**: Nice-to-have depth features that enrich the economy and
progression for dedicated players.

---

## Key Codebase Integration Points

For implementers, here's where each major feature hooks into the existing code:

| Feature | Primary Files | Hook Points |
|---------|--------------|-------------|
| Betting | `server.go`, `commussy.go`, new `betussy.go` | Post-race settlement pipeline, WS broadcast |
| Leaderboards | `server.go`, `repository/postgres/` | New aggregation queries, new API endpoints |
| Login Streaks | `authussy.go`, `server.go` | Auth middleware, login handler |
| Challenges | `server.go`, `commussy.go`, `racussy.go` | Chat command handler, race execution |
| Daily Challenges | `server.go`, new `challengussy.go` | Post-race/train hooks, daily reset goroutine |
| Tournament Seasons | `tournussy.go`, `server.go` | Tournament creation, scheduling |
| Auctions | new `auctionussy.go`, `commussy.go` | WS bid broadcast, timer goroutine, settlement |
| Syndicates | new `syndicussy.go`, `commussy.go` | Chat routing, horse ownership, treasury |
| Sabotage | `racussy.go`, `server.go` | Pre-race modifier injection, race engine |
| Prestige | `stableussy.go`, `server.go` | Milestone checking, stable reset |
| Mythics | `genussy.go`, `trainussy.go` | Breeding pipeline, trait assignment |
| Dynasty | `pedigreussy.go`, `trainussy.go` | Retirement handler, dynasty score calc |

---

## Economic Impact Analysis

Current economy: 2% burn on stud transactions, purse distribution (50/30/20),
starting balance of 1000 cummies.

New sinks (deflationary pressure):
- Betting rake: 5% of all bet pools burned
- Spectator reactions: 1 cummy each, burned
- Sabotage costs: 50-300 cummies, partially burned
- Bounty posting fees: 5% fee burned
- Auction listing fees: 2% burned
- Insurance premiums: NPC-held (effective sink)
- Prestige reset: entire balance reset to 1000
- Trait fusion: horse sacrifice (removes asset value)

New faucets (inflationary pressure):
- Daily challenge rewards: ~200-500 cummies/day per active player
- Login streak rewards: 50-200 cummies/day
- Bounty payouts: player-to-player (neutral)
- Sponsorship earnings: redistribution (neutral)
- Event bonus purses: +50-200% during events

**Recommendation**: Start conservative with faucets. The existing purse system
is the primary cummies source. New sinks should significantly outweigh new
faucets to maintain the deflationary character that gives cummies perceived
value. Monitor and adjust via server-side config values, not hardcoded constants.

---

*Document generated from full codebase audit of StallionUSSY. All proposals
reference existing systems and are designed to integrate with the current
Go backend + WebSocket + PostgreSQL architecture.*
