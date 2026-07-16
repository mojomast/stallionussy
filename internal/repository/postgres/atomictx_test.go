package postgres

// Unit tests for the transactional money paths (findings C-4, H-1, H-2).
//
// A real Postgres instance isn't available in unit tests, so these use a
// minimal database/sql/driver implementation that records every statement
// executed and lets each test script the RowsAffected outcome per statement.
// That is enough to verify the two properties the findings call out:
//
//   - the SQL itself (owner resolution via the stables table, status guards),
//   - all-or-nothing behavior (rollback when a guard fires, and that no money
//     statement runs after a failed guard).

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mojomast/stallionussy/internal/models"
)

// ---------------------------------------------------------------------------
// Fake driver
// ---------------------------------------------------------------------------

type execRecord struct {
	query string
	args  []driver.Value
}

// fakeEngine scripts statement outcomes and records everything executed.
type fakeEngine struct {
	mu        sync.Mutex
	execs     []execRecord
	commits   int
	rollbacks int
	// rowsFor returns the RowsAffected for a given statement. nil = always 1.
	rowsFor func(query string) int64
}

func (e *fakeEngine) record(query string, args []driver.NamedValue) driver.Result {
	e.mu.Lock()
	defer e.mu.Unlock()
	vals := make([]driver.Value, len(args))
	for i, a := range args {
		vals[i] = a.Value
	}
	e.execs = append(e.execs, execRecord{query: query, args: vals})
	rows := int64(1)
	if e.rowsFor != nil {
		rows = e.rowsFor(query)
	}
	return fakeResult{rows: rows}
}

// find returns the first executed statement containing the given substring.
func (e *fakeEngine) find(substr string) (execRecord, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, rec := range e.execs {
		if strings.Contains(rec.query, substr) {
			return rec, true
		}
	}
	return execRecord{}, false
}

// count returns how many executed statements contain the given substring.
func (e *fakeEngine) count(substr string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, rec := range e.execs {
		if strings.Contains(rec.query, substr) {
			n++
		}
	}
	return n
}

type fakeResult struct{ rows int64 }

func (r fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (r fakeResult) RowsAffected() (int64, error) { return r.rows, nil }

type fakeConn struct{ engine *fakeEngine }

func (c *fakeConn) Prepare(query string) (driver.Stmt, error) {
	return nil, fmt.Errorf("fake driver: Prepare not supported (use ExecContext)")
}
func (c *fakeConn) Close() error              { return nil }
func (c *fakeConn) Begin() (driver.Tx, error) { return &fakeTx{engine: c.engine}, nil }

// ExecContext implements driver.ExecerContext so database/sql routes Exec
// calls (including tx.ExecContext) here instead of Prepare.
func (c *fakeConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return c.engine.record(query, args), nil
}

type fakeTx struct{ engine *fakeEngine }

func (t *fakeTx) Commit() error {
	t.engine.mu.Lock()
	defer t.engine.mu.Unlock()
	t.engine.commits++
	return nil
}

func (t *fakeTx) Rollback() error {
	t.engine.mu.Lock()
	defer t.engine.mu.Unlock()
	t.engine.rollbacks++
	return nil
}

type fakeDriver struct{}

var (
	fakeEnginesMu sync.Mutex
	fakeEngines   = map[string]*fakeEngine{}
	fakeDSNSeq    int
)

func (fakeDriver) Open(name string) (driver.Conn, error) {
	fakeEnginesMu.Lock()
	defer fakeEnginesMu.Unlock()
	engine, ok := fakeEngines[name]
	if !ok {
		return nil, fmt.Errorf("fake driver: unknown dsn %q", name)
	}
	return &fakeConn{engine: engine}, nil
}

func init() {
	sql.Register("stallionfake", fakeDriver{})
}

// newFakeDB returns a *DB backed by the fake driver plus its engine.
func newFakeDB(t *testing.T, rowsFor func(query string) int64) (*DB, *fakeEngine) {
	t.Helper()
	fakeEnginesMu.Lock()
	fakeDSNSeq++
	dsn := fmt.Sprintf("fake-%d", fakeDSNSeq)
	engine := &fakeEngine{rowsFor: rowsFor}
	fakeEngines[dsn] = engine
	fakeEnginesMu.Unlock()

	sqlDB, err := sql.Open("stallionfake", dsn)
	if err != nil {
		t.Fatalf("open fake db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return &DB{db: sqlDB}, engine
}

func testTrade() *models.TradeOffer {
	return &models.TradeOffer{
		ID:           "trade-1",
		HorseID:      "horse-1",
		FromStableID: "stable-seller",
		ToStableID:   "stable-buyer",
		Price:        250,
		Status:       "Accepted",
		UpdatedAt:    time.Now(),
	}
}

// ---------------------------------------------------------------------------
// AcceptTradeAtomically (C-4, H-2)
// ---------------------------------------------------------------------------

// TestAcceptTradeAtomically_ResolvesOwnersViaStables is the C-4 regression:
// horses.owner_id stores user IDs, so the horse move must resolve the stable
// IDs through the stables table instead of comparing stable IDs to user IDs
// (which matched zero rows and rolled the whole trade back).
func TestAcceptTradeAtomically_ResolvesOwnersViaStables(t *testing.T) {
	db, engine := newFakeDB(t, nil)

	if err := db.AcceptTradeAtomically(context.Background(), testTrade()); err != nil {
		t.Fatalf("AcceptTradeAtomically: %v", err)
	}

	rec, ok := engine.find("UPDATE horses")
	if !ok {
		t.Fatal("no horses UPDATE executed")
	}
	if !strings.Contains(rec.query, "SELECT owner_id FROM stables") {
		t.Fatalf("horse move must resolve stable IDs to owner user IDs via the stables table (C-4); got query:\n%s", rec.query)
	}
	if len(rec.args) != 3 || rec.args[0] != "stable-buyer" || rec.args[1] != "horse-1" || rec.args[2] != "stable-seller" {
		t.Fatalf("horse move args = %v, want [stable-buyer horse-1 stable-seller]", rec.args)
	}
	if engine.commits != 1 {
		t.Fatalf("commits = %d, want 1", engine.commits)
	}
}

// TestAcceptTradeAtomically_RollsBackWhenHorseMoveFails: if the horse move
// matches zero rows the entire transaction (including the cummies transfer)
// must roll back and surface an error.
func TestAcceptTradeAtomically_RollsBackWhenHorseMoveFails(t *testing.T) {
	db, engine := newFakeDB(t, func(query string) int64 {
		if strings.Contains(query, "UPDATE horses") {
			return 0
		}
		return 1
	})

	err := db.AcceptTradeAtomically(context.Background(), testTrade())
	if err == nil {
		t.Fatal("expected error when horse move affects no rows")
	}
	if engine.commits != 0 {
		t.Fatalf("commits = %d, want 0 (transaction must roll back)", engine.commits)
	}
	if engine.rollbacks == 0 {
		t.Fatal("expected a rollback")
	}
}

// TestAcceptTradeAtomically_RejectsNonPendingTrade is the H-2 regression: the
// status update must be guarded on status = 'Pending' and abort BEFORE any
// money moves when the trade was already accepted (or doesn't exist).
func TestAcceptTradeAtomically_RejectsNonPendingTrade(t *testing.T) {
	db, engine := newFakeDB(t, func(query string) int64 {
		if strings.Contains(query, "UPDATE trade_offers") {
			return 0 // trade missing or already accepted
		}
		return 1
	})

	err := db.AcceptTradeAtomically(context.Background(), testTrade())
	if err == nil {
		t.Fatal("expected error for non-pending trade")
	}

	rec, ok := engine.find("UPDATE trade_offers")
	if !ok {
		t.Fatal("no trade_offers UPDATE executed")
	}
	if !strings.Contains(rec.query, "status = 'Pending'") {
		t.Fatalf("trade status update must be guarded on the Pending status (H-2); got query:\n%s", rec.query)
	}
	if n := engine.count("UPDATE stables"); n != 0 {
		t.Fatalf("%d stables UPDATEs executed after a failed status guard — money moved for a dead trade (H-2)", n)
	}
	if engine.commits != 0 {
		t.Fatalf("commits = %d, want 0", engine.commits)
	}
}

// ---------------------------------------------------------------------------
// SettleAuctionAtomically (H-1)
// ---------------------------------------------------------------------------

func testAuction() *models.Auction {
	return &models.Auction{
		ID:          "auction-1",
		HorseID:     "horse-9",
		Status:      models.AuctionStatusSold,
		CurrentBid:  1000,
		BidderID:    "user-buyer",
		BidderName:  "buyer",
		BidCount:    3,
		CompletedAt: time.Now(),
	}
}

// TestSettleAuctionAtomically_CommitsBothBalances is the H-1 regression: the
// buyer's (escrowed) balance must be written in the SAME transaction as the
// seller's payout, so a lost bid-time escrow persist can no longer let the
// settlement mint the payout from nothing.
func TestSettleAuctionAtomically_CommitsBothBalances(t *testing.T) {
	db, engine := newFakeDB(t, nil)

	err := db.SettleAuctionAtomically(context.Background(), testAuction(),
		"stable-seller", 5950, // seller final balance (incl. payout)
		"stable-buyer", 4000, // buyer final balance (incl. escrow)
		"user-buyer")
	if err != nil {
		t.Fatalf("SettleAuctionAtomically: %v", err)
	}

	if n := engine.count("UPDATE stables"); n != 2 {
		t.Fatalf("stables UPDATEs = %d, want 2 (seller AND buyer must be written atomically, H-1)", n)
	}
	// Verify the guard on the auction status.
	rec, ok := engine.find("UPDATE auctions")
	if !ok {
		t.Fatal("no auctions UPDATE executed")
	}
	if !strings.Contains(rec.query, "status IN ('open', 'ending')") {
		t.Fatalf("auction settlement must be guarded against double settlement; got query:\n%s", rec.query)
	}
	// Verify the balances and stable IDs were bound.
	sawSeller, sawBuyer := false, false
	engine.mu.Lock()
	for _, r := range engine.execs {
		if !strings.Contains(r.query, "UPDATE stables") || len(r.args) != 2 {
			continue
		}
		if r.args[1] == "stable-seller" && r.args[0] == int64(5950) {
			sawSeller = true
		}
		if r.args[1] == "stable-buyer" && r.args[0] == int64(4000) {
			sawBuyer = true
		}
	}
	engine.mu.Unlock()
	if !sawSeller || !sawBuyer {
		t.Fatalf("expected both seller and buyer balance writes (seller=%v buyer=%v)", sawSeller, sawBuyer)
	}
	if engine.commits != 1 {
		t.Fatalf("commits = %d, want 1", engine.commits)
	}
}

// TestSettleAuctionAtomically_RejectsDoubleSettlement: settling an auction
// whose status guard matches no rows must abort before any balances change.
func TestSettleAuctionAtomically_RejectsDoubleSettlement(t *testing.T) {
	db, engine := newFakeDB(t, func(query string) int64 {
		if strings.Contains(query, "UPDATE auctions") {
			return 0 // already settled
		}
		return 1
	})

	err := db.SettleAuctionAtomically(context.Background(), testAuction(),
		"stable-seller", 5950, "stable-buyer", 4000, "user-buyer")
	if err == nil {
		t.Fatal("expected error for already-settled auction")
	}
	if n := engine.count("UPDATE stables"); n != 0 {
		t.Fatalf("%d stables UPDATEs executed after failed settlement guard — double payout (H-1)", n)
	}
	if engine.commits != 0 {
		t.Fatalf("commits = %d, want 0", engine.commits)
	}
}

// ---------------------------------------------------------------------------
// HorseRepo.MoveHorse (C-4, non-transactional fallback path)
// ---------------------------------------------------------------------------

func TestHorseRepoMoveHorse_ResolvesOwnersViaStables(t *testing.T) {
	db, engine := newFakeDB(t, nil)
	repo := NewHorseRepo(db)

	if err := repo.MoveHorse(context.Background(), "horse-1", "stable-a", "stable-b"); err != nil {
		t.Fatalf("MoveHorse: %v", err)
	}
	rec, ok := engine.find("UPDATE horses")
	if !ok {
		t.Fatal("no horses UPDATE executed")
	}
	if !strings.Contains(rec.query, "SELECT owner_id FROM stables") {
		t.Fatalf("MoveHorse must resolve stable IDs via the stables table (C-4); got query:\n%s", rec.query)
	}
}
