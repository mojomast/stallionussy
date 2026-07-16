package repository

import "database/sql"

// schemaCreateSQL contains all CREATE TABLE IF NOT EXISTS statements for
// StallionUSSY's PostgreSQL schema.  Tables are created in dependency
// order so that foreign-key references are valid.
//
// Design notes:
//   - Complex nested Go types (Genome, Traits, TickLog, Standings) are
//     stored as JSONB columns so they can be round-tripped without an
//     explosion of join tables.
//   - All primary keys are TEXT (UUID strings generated in Go).
//   - Timestamps default to NOW() when not supplied.
const schemaCreateSQL = `
-- ===========================================================================
-- Users
-- ===========================================================================
CREATE TABLE IF NOT EXISTS users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL DEFAULT '',
    display_name  TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_username ON users (username);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username_lower ON users (LOWER(username));

-- ===========================================================================
-- Stables
-- ===========================================================================
CREATE TABLE IF NOT EXISTS stables (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    owner_id        TEXT NOT NULL DEFAULT '',
    cummies         BIGINT NOT NULL DEFAULT 0,
    casino_chips    BIGINT NOT NULL DEFAULT 0,
    starter_grants  INT NOT NULL DEFAULT 0,
    total_earnings  BIGINT NOT NULL DEFAULT 0,
    total_races     BIGINT NOT NULL DEFAULT 0,
    motto           TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_stables_owner_id ON stables (owner_id);

-- ===========================================================================
-- Horses
-- ===========================================================================
CREATE TABLE IF NOT EXISTS horses (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL,
    stable_id        TEXT DEFAULT '',
    genome           JSONB NOT NULL DEFAULT '{}',
    sire_id          TEXT DEFAULT '',
    mare_id          TEXT DEFAULT '',
    generation       INT NOT NULL DEFAULT 0,
    age              INT NOT NULL DEFAULT 0,
    fitness_ceiling  DOUBLE PRECISION NOT NULL DEFAULT 0,
    current_fitness  DOUBLE PRECISION NOT NULL DEFAULT 0,
    wins             INT NOT NULL DEFAULT 0,
    losses           INT NOT NULL DEFAULT 0,
    races            INT NOT NULL DEFAULT 0,
    elo              DOUBLE PRECISION NOT NULL DEFAULT 1200,
    owner_id         TEXT NOT NULL DEFAULT '',
    is_legendary     BOOLEAN NOT NULL DEFAULT FALSE,
    lot_number       INT NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lore             TEXT DEFAULT '',
    traits           JSONB NOT NULL DEFAULT '[]',
    fatigue          DOUBLE PRECISION NOT NULL DEFAULT 0,
    retired          BOOLEAN NOT NULL DEFAULT FALSE,
    total_earnings   BIGINT NOT NULL DEFAULT 0,
    training_xp      DOUBLE PRECISION NOT NULL DEFAULT 0,
    peak_elo         DOUBLE PRECISION NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_horses_stable_id ON horses (stable_id);
CREATE INDEX IF NOT EXISTS idx_horses_owner_id  ON horses (owner_id);

-- ===========================================================================
-- Race Results
-- ===========================================================================
CREATE TABLE IF NOT EXISTS race_results (
    id            SERIAL PRIMARY KEY,
    race_id       TEXT NOT NULL,
    horse_id      TEXT NOT NULL DEFAULT '',
    horse_name    TEXT NOT NULL DEFAULT '',
    track_type    TEXT NOT NULL DEFAULT '',
    distance      INT NOT NULL DEFAULT 0,
    finish_place  INT NOT NULL DEFAULT 0,
    total_horses  INT NOT NULL DEFAULT 0,
    final_time_ns BIGINT NOT NULL DEFAULT 0,
    elo_before    DOUBLE PRECISION NOT NULL DEFAULT 0,
    elo_after     DOUBLE PRECISION NOT NULL DEFAULT 0,
    earnings      BIGINT NOT NULL DEFAULT 0,
    weather       TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_race_results_race_id  ON race_results (race_id);
CREATE INDEX IF NOT EXISTS idx_race_results_horse_id ON race_results (horse_id);

-- ===========================================================================
-- Market Listings (Stud Market)
-- ===========================================================================
CREATE TABLE IF NOT EXISTS stud_listings (
    id            TEXT PRIMARY KEY,
    horse_id      TEXT NOT NULL DEFAULT '',
    horse_name    TEXT NOT NULL DEFAULT '',
    owner_id      TEXT NOT NULL DEFAULT '',
    price         BIGINT NOT NULL DEFAULT 0,
    pedigree      TEXT DEFAULT '',
    sappho_score  DOUBLE PRECISION NOT NULL DEFAULT 0,
    active        BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_stud_listings_horse_id ON stud_listings (horse_id);
CREATE INDEX IF NOT EXISTS idx_stud_listings_active   ON stud_listings (active) WHERE active = TRUE;

-- ===========================================================================
-- Tournaments
-- ===========================================================================
CREATE TABLE IF NOT EXISTS tournaments (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL,
    track_type     TEXT NOT NULL DEFAULT '',
    rounds         INT NOT NULL DEFAULT 0,
    current_round  INT NOT NULL DEFAULT 0,
    entry_fee      BIGINT NOT NULL DEFAULT 0,
    prize_pool     BIGINT NOT NULL DEFAULT 0,
    standings      JSONB NOT NULL DEFAULT '[]',
    races          JSONB NOT NULL DEFAULT '[]',
    status         TEXT NOT NULL DEFAULT 'Open',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ===========================================================================
-- Trade Offers
-- ===========================================================================
CREATE TABLE IF NOT EXISTS trade_offers (
    id              TEXT PRIMARY KEY,
    horse_id        TEXT NOT NULL DEFAULT '',
    horse_name      TEXT DEFAULT '',
    from_stable_id  TEXT NOT NULL DEFAULT '',
    to_stable_id    TEXT NOT NULL DEFAULT '',
    price           BIGINT NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'Pending',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_trade_offers_from ON trade_offers (from_stable_id);
CREATE INDEX IF NOT EXISTS idx_trade_offers_to   ON trade_offers (to_stable_id);

-- ===========================================================================
-- Achievements
-- ===========================================================================
CREATE TABLE IF NOT EXISTS achievements (
    id             SERIAL PRIMARY KEY,
    stable_id      TEXT NOT NULL DEFAULT '',
    achievement_id TEXT NOT NULL,
    name           TEXT NOT NULL DEFAULT '',
    description    TEXT NOT NULL DEFAULT '',
    icon           TEXT NOT NULL DEFAULT '',
    rarity         TEXT NOT NULL DEFAULT 'common',
    unlocked_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (stable_id, achievement_id)
);

CREATE INDEX IF NOT EXISTS idx_achievements_stable_id ON achievements (stable_id);

-- ===========================================================================
-- Training Sessions
-- ===========================================================================
CREATE TABLE IF NOT EXISTS training_sessions (
    id              TEXT PRIMARY KEY,
    horse_id        TEXT NOT NULL DEFAULT '',
    workout_type    TEXT NOT NULL DEFAULT '',
    xp_gained       DOUBLE PRECISION NOT NULL DEFAULT 0,
    fitness_before  DOUBLE PRECISION NOT NULL DEFAULT 0,
    fitness_after   DOUBLE PRECISION NOT NULL DEFAULT 0,
    fatigue_after   DOUBLE PRECISION NOT NULL DEFAULT 0,
    injured         BOOLEAN NOT NULL DEFAULT FALSE,
    injury_note     TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_training_sessions_horse_id ON training_sessions (horse_id);

-- ===========================================================================
-- Player Progress
-- ===========================================================================
CREATE TABLE IF NOT EXISTS player_progress (
    user_id             TEXT PRIMARY KEY,
    login_streak        INT NOT NULL DEFAULT 0,
    last_login_date     TEXT NOT NULL DEFAULT '',
    total_logins        INT NOT NULL DEFAULT 0,
    daily_trains_left   INT NOT NULL DEFAULT 6,
    daily_races_left    INT NOT NULL DEFAULT 6,
    last_daily_reset    TEXT NOT NULL DEFAULT '',
    last_casino_grant_date TEXT NOT NULL DEFAULT '',
    prestige_level      INT NOT NULL DEFAULT 0,
    prestige_xp         BIGINT NOT NULL DEFAULT 0,
    lifetime_earnings   BIGINT NOT NULL DEFAULT 0
);

-- ===========================================================================
-- Seasons
-- ===========================================================================
CREATE TABLE IF NOT EXISTS seasons (
    id          INT PRIMARY KEY,
    name        TEXT NOT NULL DEFAULT '',
    started_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at    TIMESTAMPTZ,
    active      BOOLEAN NOT NULL DEFAULT FALSE,
    champions   JSONB NOT NULL DEFAULT '[]'
);

CREATE INDEX IF NOT EXISTS idx_seasons_active ON seasons (active);

-- ==========================================================================
-- Casino
-- ==========================================================================
CREATE TABLE IF NOT EXISTS poker_tables (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL DEFAULT '',
    created_by      TEXT NOT NULL DEFAULT '',
    stake_currency  TEXT NOT NULL DEFAULT 'casino_chips',
    buy_in          BIGINT NOT NULL DEFAULT 0,
    max_players     INT NOT NULL DEFAULT 4,
    status          TEXT NOT NULL DEFAULT 'open',
    pot             BIGINT NOT NULL DEFAULT 0,
    deck_seed       BIGINT NOT NULL DEFAULT 0,
    seats           JSONB NOT NULL DEFAULT '[]',
    log             JSONB NOT NULL DEFAULT '[]',
    started_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_poker_tables_status_created_at ON poker_tables (status, created_at DESC);

CREATE TABLE IF NOT EXISTS slot_spins (
    id             TEXT PRIMARY KEY,
    stable_id      TEXT NOT NULL DEFAULT '',
    user_id        TEXT NOT NULL DEFAULT '',
    wager_amount   BIGINT NOT NULL DEFAULT 0,
    payout_amount  BIGINT NOT NULL DEFAULT 0,
    multiplier     DOUBLE PRECISION NOT NULL DEFAULT 0,
    symbols        JSONB NOT NULL DEFAULT '[]',
    summary        TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_slot_spins_user_id_created_at ON slot_spins (user_id, created_at DESC);

-- ==========================================================================
-- Departed Horses / Return Omens
-- ==========================================================================
CREATE TABLE IF NOT EXISTS departed_horses (
    id               TEXT PRIMARY KEY,
    horse_id         TEXT NOT NULL DEFAULT '',
    horse_name       TEXT NOT NULL DEFAULT '',
    owner_id         TEXT NOT NULL DEFAULT '',
    stable_id        TEXT NOT NULL DEFAULT '',
    cause            TEXT NOT NULL DEFAULT '',
    state            TEXT NOT NULL DEFAULT 'dormant',
    horse_snapshot   JSONB NOT NULL DEFAULT '{}',
    omen_text        TEXT NOT NULL DEFAULT '',
    return_summary   TEXT NOT NULL DEFAULT '',
    returned_horse   TEXT NOT NULL DEFAULT '',
    last_roll_date   TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    omen_expires_at  TIMESTAMPTZ,
    returned_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_departed_horses_owner_state ON departed_horses (owner_id, state, created_at DESC);

-- ===========================================================================
-- Market Transactions
-- ===========================================================================
CREATE TABLE IF NOT EXISTS market_transactions (
    id            TEXT PRIMARY KEY,
    listing_id    TEXT NOT NULL DEFAULT '',
    buyer_id      TEXT NOT NULL DEFAULT '',
    seller_id     TEXT NOT NULL DEFAULT '',
    price         BIGINT NOT NULL DEFAULT 0,
    burn_amount   BIGINT NOT NULL DEFAULT 0,
    foal_id       TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_market_transactions_buyer_id  ON market_transactions (buyer_id);
CREATE INDEX IF NOT EXISTS idx_market_transactions_seller_id ON market_transactions (seller_id);

-- ===========================================================================
-- Auctions (Live Horse Auctions)
-- ===========================================================================
CREATE TABLE IF NOT EXISTS auctions (
    id               TEXT PRIMARY KEY,
    seller_id        TEXT NOT NULL DEFAULT '',
    seller_name      TEXT NOT NULL DEFAULT '',
    stable_id        TEXT NOT NULL DEFAULT '',
    horse_id         TEXT NOT NULL DEFAULT '',
    horse_name       TEXT NOT NULL DEFAULT '',
    starting_bid     BIGINT NOT NULL DEFAULT 0,
    current_bid      BIGINT NOT NULL DEFAULT 0,
    bidder_id        TEXT NOT NULL DEFAULT '',
    bidder_name      TEXT NOT NULL DEFAULT '',
    bid_count        INT NOT NULL DEFAULT 0,
    bid_history      JSONB NOT NULL DEFAULT '[]',
    status           TEXT NOT NULL DEFAULT 'open',
    duration         INT NOT NULL DEFAULT 120,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at     TIMESTAMPTZ,
    geoffrussy_tax   BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_auctions_status    ON auctions (status);
CREATE INDEX IF NOT EXISTS idx_auctions_seller_id ON auctions (seller_id);
CREATE INDEX IF NOT EXISTS idx_auctions_horse_id  ON auctions (horse_id);

-- ===========================================================================
-- Race Replays (persistent full race data for replay sharing)
-- ===========================================================================
CREATE TABLE IF NOT EXISTS race_replays (
    race_id      TEXT PRIMARY KEY,
    track_type   TEXT NOT NULL DEFAULT '',
    distance     INT NOT NULL DEFAULT 0,
    purse        BIGINT NOT NULL DEFAULT 0,
    entries      INT NOT NULL DEFAULT 0,
    weather      TEXT NOT NULL DEFAULT '',
    winner_id    TEXT NOT NULL DEFAULT '',
    winner_name  TEXT NOT NULL DEFAULT '',
    data         JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_race_replays_created_at ON race_replays (created_at DESC);

-- ===========================================================================
-- Alliances (Stable Guilds)
-- ===========================================================================
CREATE TABLE IF NOT EXISTS alliances (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    tag         TEXT NOT NULL,
    leader_id   TEXT NOT NULL DEFAULT '',
    motto       TEXT NOT NULL DEFAULT '',
    treasury    BIGINT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_alliances_leader_id ON alliances (leader_id);

-- ===========================================================================
-- Alliance Members
-- ===========================================================================
CREATE TABLE IF NOT EXISTS alliance_members (
    alliance_id TEXT NOT NULL REFERENCES alliances(id) ON DELETE CASCADE,
    user_id     TEXT NOT NULL,
    username    TEXT NOT NULL DEFAULT '',
    stable_id   TEXT NOT NULL DEFAULT '',
    role        TEXT NOT NULL DEFAULT 'member',
    joined_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (alliance_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_alliance_members_user_id ON alliance_members (user_id);

-- ===========================================================================
-- Add injury column to horses (JSONB, nullable)
-- ===========================================================================
ALTER TABLE horses ADD COLUMN IF NOT EXISTS injury JSONB;

-- ===========================================================================
-- Horse Fights
-- ===========================================================================
CREATE TABLE IF NOT EXISTS horse_fights (
    id              TEXT PRIMARY KEY,
    arena_type      TEXT NOT NULL DEFAULT '',
    horse1_id       TEXT NOT NULL DEFAULT '',
    horse1_name     TEXT NOT NULL DEFAULT '',
    horse1_owner_id TEXT NOT NULL DEFAULT '',
    horse2_id       TEXT NOT NULL DEFAULT '',
    horse2_name     TEXT NOT NULL DEFAULT '',
    horse2_owner_id TEXT NOT NULL DEFAULT '',
    winner_id       TEXT NOT NULL DEFAULT '',
    winner_name     TEXT NOT NULL DEFAULT '',
    loser_id        TEXT NOT NULL DEFAULT '',
    loser_name      TEXT NOT NULL DEFAULT '',
    is_fatality     BOOLEAN NOT NULL DEFAULT FALSE,
    is_to_death     BOOLEAN NOT NULL DEFAULT FALSE,
    purse           BIGINT NOT NULL DEFAULT 0,
    entry_fee       BIGINT NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'pending',
    ko_round        INT NOT NULL DEFAULT 0,
    total_rounds    INT NOT NULL DEFAULT 0,
    fight_log       JSONB NOT NULL DEFAULT '{}',
    narrative       JSONB NOT NULL DEFAULT '[]',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_horse_fights_status ON horse_fights (status);
CREATE INDEX IF NOT EXISTS idx_horse_fights_horse1 ON horse_fights (horse1_id);
CREATE INDEX IF NOT EXISTS idx_horse_fights_horse2 ON horse_fights (horse2_id);

-- ===========================================================================
-- Glue Factory Ledger
-- ===========================================================================
CREATE TABLE IF NOT EXISTS glue_factory (
    id              TEXT PRIMARY KEY,
    horse_id        TEXT NOT NULL DEFAULT '',
    horse_name      TEXT NOT NULL DEFAULT '',
    owner_id        TEXT NOT NULL DEFAULT '',
    stable_id       TEXT NOT NULL DEFAULT '',
    glue_produced   BIGINT NOT NULL DEFAULT 0,
    cummies_earned  BIGINT NOT NULL DEFAULT 0,
    bonus_material  TEXT NOT NULL DEFAULT '',
    bonus_amount    BIGINT NOT NULL DEFAULT 0,
    eulogy          TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_glue_factory_owner ON glue_factory (owner_id);

-- ===========================================================================
-- Breeding Stallions (Permanent Stud Duty)
-- ===========================================================================
CREATE TABLE IF NOT EXISTS breeding_stallions (
    horse_id        TEXT PRIMARY KEY,
    horse_name      TEXT NOT NULL DEFAULT '',
    owner_id        TEXT NOT NULL DEFAULT '',
    stable_id       TEXT NOT NULL DEFAULT '',
    breed_count     INT NOT NULL DEFAULT 0,
    total_earnings  BIGINT NOT NULL DEFAULT 0,
    fee             BIGINT NOT NULL DEFAULT 0,
    cooldown_hours  INT NOT NULL DEFAULT 12,
    active          BOOLEAN NOT NULL DEFAULT TRUE,
    assigned_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_breeding_stallions_owner ON breeding_stallions (owner_id);
CREATE INDEX IF NOT EXISTS idx_breeding_stallions_active ON breeding_stallions (active) WHERE active = TRUE;

-- ===========================================================================
-- Token Version (for JWT revocation on password change)
-- ===========================================================================
ALTER TABLE users ADD COLUMN IF NOT EXISTS token_version INTEGER NOT NULL DEFAULT 0;

-- ===========================================================================
-- Tournament organizer (H-4: organizer-only round advancement)
-- ===========================================================================
ALTER TABLE tournaments ADD COLUMN IF NOT EXISTS created_by TEXT NOT NULL DEFAULT '';

-- ===========================================================================
-- Horse breeding cooldown & champion flag (H-7)
-- Previously RAM-only: restarts reset breeding cooldowns and dropped the
-- retired-champion flag that drives the foal breeding bonus.
-- ===========================================================================
ALTER TABLE horses ADD COLUMN IF NOT EXISTS retired_champion BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE horses ADD COLUMN IF NOT EXISTS last_bred_at TIMESTAMPTZ;

-- ===========================================================================
-- Training specialties (Phase 3)
-- Per-discipline bonuses accumulated by distinct workout modes
-- (Sprint/Endurance/MudRun/MentalRep), consumed by the race simulator.
-- ===========================================================================
ALTER TABLE horses ADD COLUMN IF NOT EXISTS training_specialty JSONB;

-- ===========================================================================
-- Stud listing use limits (H-8)
-- Previously RAM-only: a maxed-out stud came back active with 0 uses after
-- every restart.
-- ===========================================================================
ALTER TABLE stud_listings ADD COLUMN IF NOT EXISTS times_used INT NOT NULL DEFAULT 0;
ALTER TABLE stud_listings ADD COLUMN IF NOT EXISTS max_uses INT NOT NULL DEFAULT 0;

-- ===========================================================================
-- Market transaction seller payout (H-9)
-- ===========================================================================
ALTER TABLE market_transactions ADD COLUMN IF NOT EXISTS seller_payout BIGINT NOT NULL DEFAULT 0;

-- ===========================================================================
-- Texas Hold'em poker table state (H-10)
-- Previously only the 13 legacy draw-poker columns persisted, so an
-- in-progress Hold'em hand reloaded as a corrupted draw table.
-- ===========================================================================
ALTER TABLE poker_tables ADD COLUMN IF NOT EXISTS game_type TEXT NOT NULL DEFAULT 'draw';
ALTER TABLE poker_tables ADD COLUMN IF NOT EXISTS community_cards JSONB NOT NULL DEFAULT '[]';
ALTER TABLE poker_tables ADD COLUMN IF NOT EXISTS small_blind BIGINT NOT NULL DEFAULT 0;
ALTER TABLE poker_tables ADD COLUMN IF NOT EXISTS big_blind BIGINT NOT NULL DEFAULT 0;
ALTER TABLE poker_tables ADD COLUMN IF NOT EXISTS current_bet BIGINT NOT NULL DEFAULT 0;
ALTER TABLE poker_tables ADD COLUMN IF NOT EXISTS dealer_seat INT NOT NULL DEFAULT 0;
ALTER TABLE poker_tables ADD COLUMN IF NOT EXISTS action_seat INT NOT NULL DEFAULT -1;
ALTER TABLE poker_tables ADD COLUMN IF NOT EXISTS min_raise BIGINT NOT NULL DEFAULT 0;
ALTER TABLE poker_tables ADD COLUMN IF NOT EXISTS side_pots JSONB NOT NULL DEFAULT '[]';
ALTER TABLE poker_tables ADD COLUMN IF NOT EXISTS hand_round INT NOT NULL DEFAULT 0;
ALTER TABLE poker_tables ADD COLUMN IF NOT EXISTS action_deadline TIMESTAMPTZ;
`

// schemaRetrofitSQL converges a legacy database to the current schema. It is
// executed BEFORE schemaCreateSQL so that every index and data-fixup
// statement sees current columns, and it uses ALTER TABLE IF EXISTS so
// tables that don't exist yet (fresh databases, tables introduced after a
// deployment) are skipped and left to CREATE TABLE.
const schemaRetrofitSQL = `
-- ===========================================================================
-- Schema convergence for pre-existing databases
-- CREATE TABLE IF NOT EXISTS is a no-op when the table already exists, so a
-- column added to a CREATE definition after a production database was first
-- initialized never materializes there (this is exactly how production ended
-- up without stables.casino_chips). Retrofit every column of every legacy
-- table so a database of any vintage converges to the current schema before
-- the data fix-ups below run. Columns the CREATE declares NOT NULL without a
-- default gain a zero-value default here: ALTER TABLE cannot add a
-- defaultless NOT NULL column to a populated table.
-- ===========================================================================
ALTER TABLE IF EXISTS users ADD COLUMN IF NOT EXISTS username TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS users ADD COLUMN IF NOT EXISTS password_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS users ADD COLUMN IF NOT EXISTS display_name TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS users ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE IF EXISTS users ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE IF EXISTS stables ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS stables ADD COLUMN IF NOT EXISTS owner_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS stables ADD COLUMN IF NOT EXISTS cummies BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS stables ADD COLUMN IF NOT EXISTS casino_chips BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS stables ADD COLUMN IF NOT EXISTS starter_grants INT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS stables ADD COLUMN IF NOT EXISTS total_earnings BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS stables ADD COLUMN IF NOT EXISTS total_races BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS stables ADD COLUMN IF NOT EXISTS motto TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS stables ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE IF EXISTS horses ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS horses ADD COLUMN IF NOT EXISTS stable_id TEXT DEFAULT '';
ALTER TABLE IF EXISTS horses ADD COLUMN IF NOT EXISTS genome JSONB NOT NULL DEFAULT '{}';
ALTER TABLE IF EXISTS horses ADD COLUMN IF NOT EXISTS sire_id TEXT DEFAULT '';
ALTER TABLE IF EXISTS horses ADD COLUMN IF NOT EXISTS mare_id TEXT DEFAULT '';
ALTER TABLE IF EXISTS horses ADD COLUMN IF NOT EXISTS generation INT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS horses ADD COLUMN IF NOT EXISTS age INT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS horses ADD COLUMN IF NOT EXISTS fitness_ceiling DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS horses ADD COLUMN IF NOT EXISTS current_fitness DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS horses ADD COLUMN IF NOT EXISTS wins INT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS horses ADD COLUMN IF NOT EXISTS losses INT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS horses ADD COLUMN IF NOT EXISTS races INT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS horses ADD COLUMN IF NOT EXISTS elo DOUBLE PRECISION NOT NULL DEFAULT 1200;
ALTER TABLE IF EXISTS horses ADD COLUMN IF NOT EXISTS owner_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS horses ADD COLUMN IF NOT EXISTS is_legendary BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE IF EXISTS horses ADD COLUMN IF NOT EXISTS lot_number INT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS horses ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE IF EXISTS horses ADD COLUMN IF NOT EXISTS lore TEXT DEFAULT '';
ALTER TABLE IF EXISTS horses ADD COLUMN IF NOT EXISTS traits JSONB NOT NULL DEFAULT '[]';
ALTER TABLE IF EXISTS horses ADD COLUMN IF NOT EXISTS fatigue DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS horses ADD COLUMN IF NOT EXISTS retired BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE IF EXISTS horses ADD COLUMN IF NOT EXISTS total_earnings BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS horses ADD COLUMN IF NOT EXISTS training_xp DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS horses ADD COLUMN IF NOT EXISTS peak_elo DOUBLE PRECISION NOT NULL DEFAULT 0;

ALTER TABLE IF EXISTS race_results ADD COLUMN IF NOT EXISTS race_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS race_results ADD COLUMN IF NOT EXISTS horse_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS race_results ADD COLUMN IF NOT EXISTS horse_name TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS race_results ADD COLUMN IF NOT EXISTS track_type TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS race_results ADD COLUMN IF NOT EXISTS distance INT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS race_results ADD COLUMN IF NOT EXISTS finish_place INT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS race_results ADD COLUMN IF NOT EXISTS total_horses INT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS race_results ADD COLUMN IF NOT EXISTS final_time_ns BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS race_results ADD COLUMN IF NOT EXISTS elo_before DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS race_results ADD COLUMN IF NOT EXISTS elo_after DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS race_results ADD COLUMN IF NOT EXISTS earnings BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS race_results ADD COLUMN IF NOT EXISTS weather TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS race_results ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE IF EXISTS stud_listings ADD COLUMN IF NOT EXISTS horse_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS stud_listings ADD COLUMN IF NOT EXISTS horse_name TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS stud_listings ADD COLUMN IF NOT EXISTS owner_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS stud_listings ADD COLUMN IF NOT EXISTS price BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS stud_listings ADD COLUMN IF NOT EXISTS pedigree TEXT DEFAULT '';
ALTER TABLE IF EXISTS stud_listings ADD COLUMN IF NOT EXISTS sappho_score DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS stud_listings ADD COLUMN IF NOT EXISTS active BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE IF EXISTS stud_listings ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE IF EXISTS tournaments ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS tournaments ADD COLUMN IF NOT EXISTS track_type TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS tournaments ADD COLUMN IF NOT EXISTS rounds INT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS tournaments ADD COLUMN IF NOT EXISTS current_round INT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS tournaments ADD COLUMN IF NOT EXISTS entry_fee BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS tournaments ADD COLUMN IF NOT EXISTS prize_pool BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS tournaments ADD COLUMN IF NOT EXISTS standings JSONB NOT NULL DEFAULT '[]';
ALTER TABLE IF EXISTS tournaments ADD COLUMN IF NOT EXISTS races JSONB NOT NULL DEFAULT '[]';
ALTER TABLE IF EXISTS tournaments ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'Open';
ALTER TABLE IF EXISTS tournaments ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE IF EXISTS trade_offers ADD COLUMN IF NOT EXISTS horse_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS trade_offers ADD COLUMN IF NOT EXISTS horse_name TEXT DEFAULT '';
ALTER TABLE IF EXISTS trade_offers ADD COLUMN IF NOT EXISTS from_stable_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS trade_offers ADD COLUMN IF NOT EXISTS to_stable_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS trade_offers ADD COLUMN IF NOT EXISTS price BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS trade_offers ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'Pending';
ALTER TABLE IF EXISTS trade_offers ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE IF EXISTS trade_offers ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE IF EXISTS achievements ADD COLUMN IF NOT EXISTS stable_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS achievements ADD COLUMN IF NOT EXISTS achievement_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS achievements ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS achievements ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS achievements ADD COLUMN IF NOT EXISTS icon TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS achievements ADD COLUMN IF NOT EXISTS rarity TEXT NOT NULL DEFAULT 'common';
ALTER TABLE IF EXISTS achievements ADD COLUMN IF NOT EXISTS unlocked_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE IF EXISTS training_sessions ADD COLUMN IF NOT EXISTS horse_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS training_sessions ADD COLUMN IF NOT EXISTS workout_type TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS training_sessions ADD COLUMN IF NOT EXISTS xp_gained DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS training_sessions ADD COLUMN IF NOT EXISTS fitness_before DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS training_sessions ADD COLUMN IF NOT EXISTS fitness_after DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS training_sessions ADD COLUMN IF NOT EXISTS fatigue_after DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS training_sessions ADD COLUMN IF NOT EXISTS injured BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE IF EXISTS training_sessions ADD COLUMN IF NOT EXISTS injury_note TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS training_sessions ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE IF EXISTS player_progress ADD COLUMN IF NOT EXISTS login_streak INT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS player_progress ADD COLUMN IF NOT EXISTS last_login_date TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS player_progress ADD COLUMN IF NOT EXISTS total_logins INT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS player_progress ADD COLUMN IF NOT EXISTS daily_trains_left INT NOT NULL DEFAULT 6;
ALTER TABLE IF EXISTS player_progress ADD COLUMN IF NOT EXISTS daily_races_left INT NOT NULL DEFAULT 6;
ALTER TABLE IF EXISTS player_progress ADD COLUMN IF NOT EXISTS last_daily_reset TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS player_progress ADD COLUMN IF NOT EXISTS last_casino_grant_date TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS player_progress ADD COLUMN IF NOT EXISTS prestige_level INT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS player_progress ADD COLUMN IF NOT EXISTS prestige_xp BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS player_progress ADD COLUMN IF NOT EXISTS lifetime_earnings BIGINT NOT NULL DEFAULT 0;

ALTER TABLE IF EXISTS seasons ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS seasons ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE IF EXISTS seasons ADD COLUMN IF NOT EXISTS ended_at TIMESTAMPTZ;
ALTER TABLE IF EXISTS seasons ADD COLUMN IF NOT EXISTS active BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE IF EXISTS seasons ADD COLUMN IF NOT EXISTS champions JSONB NOT NULL DEFAULT '[]';

ALTER TABLE IF EXISTS poker_tables ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS poker_tables ADD COLUMN IF NOT EXISTS created_by TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS poker_tables ADD COLUMN IF NOT EXISTS stake_currency TEXT NOT NULL DEFAULT 'casino_chips';
ALTER TABLE IF EXISTS poker_tables ADD COLUMN IF NOT EXISTS buy_in BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS poker_tables ADD COLUMN IF NOT EXISTS max_players INT NOT NULL DEFAULT 4;
ALTER TABLE IF EXISTS poker_tables ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'open';
ALTER TABLE IF EXISTS poker_tables ADD COLUMN IF NOT EXISTS pot BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS poker_tables ADD COLUMN IF NOT EXISTS deck_seed BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS poker_tables ADD COLUMN IF NOT EXISTS seats JSONB NOT NULL DEFAULT '[]';
ALTER TABLE IF EXISTS poker_tables ADD COLUMN IF NOT EXISTS log JSONB NOT NULL DEFAULT '[]';
ALTER TABLE IF EXISTS poker_tables ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ;
ALTER TABLE IF EXISTS poker_tables ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE IF EXISTS poker_tables ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE IF EXISTS slot_spins ADD COLUMN IF NOT EXISTS stable_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS slot_spins ADD COLUMN IF NOT EXISTS user_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS slot_spins ADD COLUMN IF NOT EXISTS wager_amount BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS slot_spins ADD COLUMN IF NOT EXISTS payout_amount BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS slot_spins ADD COLUMN IF NOT EXISTS multiplier DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS slot_spins ADD COLUMN IF NOT EXISTS symbols JSONB NOT NULL DEFAULT '[]';
ALTER TABLE IF EXISTS slot_spins ADD COLUMN IF NOT EXISTS summary TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS slot_spins ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE IF EXISTS departed_horses ADD COLUMN IF NOT EXISTS horse_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS departed_horses ADD COLUMN IF NOT EXISTS horse_name TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS departed_horses ADD COLUMN IF NOT EXISTS owner_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS departed_horses ADD COLUMN IF NOT EXISTS stable_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS departed_horses ADD COLUMN IF NOT EXISTS cause TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS departed_horses ADD COLUMN IF NOT EXISTS state TEXT NOT NULL DEFAULT 'dormant';
ALTER TABLE IF EXISTS departed_horses ADD COLUMN IF NOT EXISTS horse_snapshot JSONB NOT NULL DEFAULT '{}';
ALTER TABLE IF EXISTS departed_horses ADD COLUMN IF NOT EXISTS omen_text TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS departed_horses ADD COLUMN IF NOT EXISTS return_summary TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS departed_horses ADD COLUMN IF NOT EXISTS returned_horse TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS departed_horses ADD COLUMN IF NOT EXISTS last_roll_date TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS departed_horses ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE IF EXISTS departed_horses ADD COLUMN IF NOT EXISTS omen_expires_at TIMESTAMPTZ;
ALTER TABLE IF EXISTS departed_horses ADD COLUMN IF NOT EXISTS returned_at TIMESTAMPTZ;

ALTER TABLE IF EXISTS market_transactions ADD COLUMN IF NOT EXISTS listing_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS market_transactions ADD COLUMN IF NOT EXISTS buyer_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS market_transactions ADD COLUMN IF NOT EXISTS seller_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS market_transactions ADD COLUMN IF NOT EXISTS price BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS market_transactions ADD COLUMN IF NOT EXISTS burn_amount BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS market_transactions ADD COLUMN IF NOT EXISTS foal_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS market_transactions ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE IF EXISTS auctions ADD COLUMN IF NOT EXISTS seller_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS auctions ADD COLUMN IF NOT EXISTS seller_name TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS auctions ADD COLUMN IF NOT EXISTS stable_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS auctions ADD COLUMN IF NOT EXISTS horse_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS auctions ADD COLUMN IF NOT EXISTS horse_name TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS auctions ADD COLUMN IF NOT EXISTS starting_bid BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS auctions ADD COLUMN IF NOT EXISTS current_bid BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS auctions ADD COLUMN IF NOT EXISTS bidder_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS auctions ADD COLUMN IF NOT EXISTS bidder_name TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS auctions ADD COLUMN IF NOT EXISTS bid_count INT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS auctions ADD COLUMN IF NOT EXISTS bid_history JSONB NOT NULL DEFAULT '[]';
ALTER TABLE IF EXISTS auctions ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'open';
ALTER TABLE IF EXISTS auctions ADD COLUMN IF NOT EXISTS duration INT NOT NULL DEFAULT 120;
ALTER TABLE IF EXISTS auctions ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE IF EXISTS auctions ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE IF EXISTS auctions ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ;
ALTER TABLE IF EXISTS auctions ADD COLUMN IF NOT EXISTS geoffrussy_tax BIGINT NOT NULL DEFAULT 0;

ALTER TABLE IF EXISTS race_replays ADD COLUMN IF NOT EXISTS track_type TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS race_replays ADD COLUMN IF NOT EXISTS distance INT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS race_replays ADD COLUMN IF NOT EXISTS purse BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS race_replays ADD COLUMN IF NOT EXISTS entries INT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS race_replays ADD COLUMN IF NOT EXISTS weather TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS race_replays ADD COLUMN IF NOT EXISTS winner_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS race_replays ADD COLUMN IF NOT EXISTS winner_name TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS race_replays ADD COLUMN IF NOT EXISTS data JSONB NOT NULL DEFAULT '{}';
ALTER TABLE IF EXISTS race_replays ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE IF EXISTS alliances ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS alliances ADD COLUMN IF NOT EXISTS tag TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS alliances ADD COLUMN IF NOT EXISTS leader_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS alliances ADD COLUMN IF NOT EXISTS motto TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS alliances ADD COLUMN IF NOT EXISTS treasury BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS alliances ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE IF EXISTS alliance_members ADD COLUMN IF NOT EXISTS username TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS alliance_members ADD COLUMN IF NOT EXISTS stable_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS alliance_members ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'member';
ALTER TABLE IF EXISTS alliance_members ADD COLUMN IF NOT EXISTS joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE IF EXISTS horse_fights ADD COLUMN IF NOT EXISTS arena_type TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS horse_fights ADD COLUMN IF NOT EXISTS horse1_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS horse_fights ADD COLUMN IF NOT EXISTS horse1_name TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS horse_fights ADD COLUMN IF NOT EXISTS horse1_owner_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS horse_fights ADD COLUMN IF NOT EXISTS horse2_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS horse_fights ADD COLUMN IF NOT EXISTS horse2_name TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS horse_fights ADD COLUMN IF NOT EXISTS horse2_owner_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS horse_fights ADD COLUMN IF NOT EXISTS winner_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS horse_fights ADD COLUMN IF NOT EXISTS winner_name TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS horse_fights ADD COLUMN IF NOT EXISTS loser_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS horse_fights ADD COLUMN IF NOT EXISTS loser_name TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS horse_fights ADD COLUMN IF NOT EXISTS is_fatality BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE IF EXISTS horse_fights ADD COLUMN IF NOT EXISTS is_to_death BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE IF EXISTS horse_fights ADD COLUMN IF NOT EXISTS purse BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS horse_fights ADD COLUMN IF NOT EXISTS entry_fee BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS horse_fights ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE IF EXISTS horse_fights ADD COLUMN IF NOT EXISTS ko_round INT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS horse_fights ADD COLUMN IF NOT EXISTS total_rounds INT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS horse_fights ADD COLUMN IF NOT EXISTS fight_log JSONB NOT NULL DEFAULT '{}';
ALTER TABLE IF EXISTS horse_fights ADD COLUMN IF NOT EXISTS narrative JSONB NOT NULL DEFAULT '[]';
ALTER TABLE IF EXISTS horse_fights ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE IF EXISTS glue_factory ADD COLUMN IF NOT EXISTS horse_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS glue_factory ADD COLUMN IF NOT EXISTS horse_name TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS glue_factory ADD COLUMN IF NOT EXISTS owner_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS glue_factory ADD COLUMN IF NOT EXISTS stable_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS glue_factory ADD COLUMN IF NOT EXISTS glue_produced BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS glue_factory ADD COLUMN IF NOT EXISTS cummies_earned BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS glue_factory ADD COLUMN IF NOT EXISTS bonus_material TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS glue_factory ADD COLUMN IF NOT EXISTS bonus_amount BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS glue_factory ADD COLUMN IF NOT EXISTS eulogy TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS glue_factory ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE IF EXISTS breeding_stallions ADD COLUMN IF NOT EXISTS horse_name TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS breeding_stallions ADD COLUMN IF NOT EXISTS owner_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS breeding_stallions ADD COLUMN IF NOT EXISTS stable_id TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS breeding_stallions ADD COLUMN IF NOT EXISTS breed_count INT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS breeding_stallions ADD COLUMN IF NOT EXISTS total_earnings BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS breeding_stallions ADD COLUMN IF NOT EXISTS fee BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS breeding_stallions ADD COLUMN IF NOT EXISTS cooldown_hours INT NOT NULL DEFAULT 12;
ALTER TABLE IF EXISTS breeding_stallions ADD COLUMN IF NOT EXISTS active BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE IF EXISTS breeding_stallions ADD COLUMN IF NOT EXISTS assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
`

// schemaFixupSQL contains constraint backfills and data fix-ups that must run
// only after tables exist and legacy columns have been retrofitted.
const schemaFixupSQL = `
-- ===========================================================================
-- Race result uniqueness (M-13)
-- race_results had no unique constraint on (race_id, horse_id) and inserts
-- were plain INSERTs, so re-running result recording double-counted history
-- and earnings. Dedupe any legacy duplicates (keeping the earliest row),
-- then enforce uniqueness. RecordResult now uses ON CONFLICT DO NOTHING.
-- ===========================================================================
DELETE FROM race_results a USING race_results b
WHERE a.race_id = b.race_id
  AND a.horse_id = b.horse_id
  AND a.ctid > b.ctid;
CREATE UNIQUE INDEX IF NOT EXISTS idx_race_results_race_horse
    ON race_results (race_id, horse_id);

-- ===========================================================================
-- Progressive slot jackpot (M-2)
-- Previously RAM-only: a restart reset the pool to its seed and every 2%
-- wager contribution accumulated so far evaporated.
-- ===========================================================================
CREATE TABLE IF NOT EXISTS casino_jackpot (
    id          INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    pool        BIGINT NOT NULL DEFAULT 0,
    last_winner TEXT NOT NULL DEFAULT '',
    last_amount BIGINT NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ===========================================================================
-- Balance floors (C-9)
-- The write-through persistence blasts absolute in-memory balances at the
-- database, so a single missed in-process check could persist a negative
-- balance. Floor any legacy negatives, then enforce non-negativity at the
-- database layer as a last line of defense against double spends.
-- ===========================================================================
-- ===========================================================================
-- Server-side login sessions
-- Sessions back the JWTs: a token is only honored while a matching,
-- unexpired row exists here. Keyed by SHA-256(token) so a database leak
-- never exposes replayable credentials. Rows survive restarts, so valid
-- logins do too.
-- ===========================================================================
CREATE TABLE IF NOT EXISTS sessions (
    token_hash  TEXT PRIMARY KEY,
    player_id   TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL,
    last_seen   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_sessions_player_id  ON sessions (player_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions (expires_at);

-- ===========================================================================
-- App config (small key/value store)
-- Used by offline mode to persist a generated JWT secret so sessions
-- survive restarts without requiring the JWT_SECRET env var.
-- ===========================================================================
CREATE TABLE IF NOT EXISTS app_config (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);

-- ===========================================================================
-- Head-to-head challenges
-- Previously RAM-only: pending challenges vanished on restart.
-- ===========================================================================
CREATE TABLE IF NOT EXISTS challenges (
    id                    TEXT PRIMARY KEY,
    challenger_id         TEXT NOT NULL DEFAULT '',
    challenger_name       TEXT NOT NULL DEFAULT '',
    challenger_horse      TEXT NOT NULL DEFAULT '',
    challenger_horse_name TEXT NOT NULL DEFAULT '',
    defender_id           TEXT NOT NULL DEFAULT '',
    defender_name         TEXT NOT NULL DEFAULT '',
    defender_horse        TEXT NOT NULL DEFAULT '',
    defender_horse_name   TEXT NOT NULL DEFAULT '',
    wager                 BIGINT NOT NULL DEFAULT 0,
    status                TEXT NOT NULL DEFAULT 'pending',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_challenges_status     ON challenges (status);
CREATE INDEX IF NOT EXISTS idx_challenges_created_at ON challenges (created_at DESC);

-- ===========================================================================
-- Betting pools (pari-mutuel escrow + payout records)
-- Previously RAM-only: escrowed bets evaporated on restart (H-6 refunded
-- them at shutdown; now the pools themselves survive).
-- ===========================================================================
CREATE TABLE IF NOT EXISTS betting_pools (
    race_id     TEXT PRIMARY KEY,
    status      TEXT NOT NULL DEFAULT 'open',
    kind        TEXT NOT NULL DEFAULT 'race',
    horses      JSONB NOT NULL DEFAULT '[]',
    bets        JSONB NOT NULL DEFAULT '[]',
    total_pool  BIGINT NOT NULL DEFAULT 0,
    house_cut   BIGINT NOT NULL DEFAULT 0,
    opened_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    closed_at   TIMESTAMPTZ,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_betting_pools_status ON betting_pools (status);

-- ===========================================================================
-- Horse rivalries (head-to-head win counts)
-- Previously RAM-only.
-- ===========================================================================
CREATE TABLE IF NOT EXISTS rivalries (
    winner_id TEXT NOT NULL,
    loser_id  TEXT NOT NULL,
    wins      INT NOT NULL DEFAULT 0,
    PRIMARY KEY (winner_id, loser_id)
);

UPDATE stables SET cummies = 0 WHERE cummies < 0;
UPDATE stables SET casino_chips = 0 WHERE casino_chips < 0;
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'stables_cummies_nonnegative') THEN
        ALTER TABLE stables ADD CONSTRAINT stables_cummies_nonnegative CHECK (cummies >= 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'stables_casino_chips_nonnegative') THEN
        ALTER TABLE stables ADD CONSTRAINT stables_casino_chips_nonnegative CHECK (casino_chips >= 0);
    END IF;
END $$;
`

// schemaSQL is the complete PostgreSQL schema document, retained for tests
// and callers that inspect the DDL as a whole. Execution order differs: see
// RunMigrationsFor.
var schemaSQL = schemaRetrofitSQL + schemaCreateSQL + schemaFixupSQL

// RunMigrations executes the PostgreSQL schema DDL against the provided
// database connection.  All statements use IF NOT EXISTS so this is safe to
// call on every startup.
func RunMigrations(db *sql.DB) error {
	return RunMigrationsFor(db, DialectPostgres)
}

// RunMigrationsFor executes the schema DDL for the given SQL dialect.
// SQLite uses schemaSQLiteSQL, a fresh-schema rendering with all retrofitted
// columns baked in. PostgreSQL runs in three phases: legacy-column retrofits
// first (ALTER TABLE IF EXISTS, so fresh databases skip them), then the
// CREATE TABLE/INDEX statements, then constraint backfills and data fix-ups.
// The retrofits must precede the creates because index and fix-up statements
// reference columns that pre-existing databases may not have yet. All phases
// are idempotent and safe to run on every startup.
func RunMigrationsFor(db *sql.DB, dialect Dialect) error {
	if dialect == DialectSQLite {
		_, err := db.Exec(schemaSQLiteSQL)
		return err
	}
	for _, phase := range []string{schemaRetrofitSQL, schemaCreateSQL, schemaFixupSQL} {
		if _, err := db.Exec(phase); err != nil {
			return err
		}
	}
	return nil
}
