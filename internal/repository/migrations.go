package repository

import "database/sql"

// schemaSQL contains all CREATE TABLE IF NOT EXISTS statements for
// StallionUSSY's PostgreSQL schema.  Tables are created in dependency
// order so that foreign-key references are valid.
//
// Design notes:
//   - Complex nested Go types (Genome, Traits, TickLog, Standings) are
//     stored as JSONB columns so they can be round-tripped without an
//     explosion of join tables.
//   - All primary keys are TEXT (UUID strings generated in Go).
//   - Timestamps default to NOW() when not supplied.
const schemaSQL = `
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

-- ===========================================================================
-- Stables
-- ===========================================================================
CREATE TABLE IF NOT EXISTS stables (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    owner_id        TEXT NOT NULL DEFAULT '',
    cummies         BIGINT NOT NULL DEFAULT 0,
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
`

// RunMigrations executes the schema DDL against the provided database
// connection.  All statements use IF NOT EXISTS so this is safe to call
// on every startup.
func RunMigrations(db *sql.DB) error {
	_, err := db.Exec(schemaSQL)
	return err
}
