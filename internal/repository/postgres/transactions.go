package postgres

import (
	"context"
	"fmt"

	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/repository"
)

// Compile-time interface check.
var _ repository.MarketTransactionRepository = (*MarketTransactionRepo)(nil)

// MarketTransactionRepo implements repository.MarketTransactionRepository backed by PostgreSQL.
type MarketTransactionRepo struct {
	db *DB
}

// NewMarketTransactionRepo returns a new MarketTransactionRepo.
func NewMarketTransactionRepo(db *DB) *MarketTransactionRepo {
	return &MarketTransactionRepo{db: db}
}

// txCols is the canonical column list for market_transactions queries.
const txCols = `
	id, listing_id, buyer_id, seller_id, price,
	burn_amount, foal_id, created_at`

// SaveTransaction persists a completed market transaction.
func (r *MarketTransactionRepo) SaveTransaction(ctx context.Context, tx *models.MarketTransaction) error {
	query := `
		INSERT INTO market_transactions (
			id, listing_id, buyer_id, seller_id, price,
			burn_amount, foal_id, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	_, err := r.db.db.ExecContext(ctx, query,
		tx.ID,
		tx.ListingID,
		tx.BuyerID,
		tx.SellerID,
		tx.Price,
		tx.BurnAmount,
		tx.FoalID,
		tx.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("save market transaction: %w", err)
	}
	return nil
}

// GetTransactionsByBuyer returns all transactions where the given user was the buyer.
func (r *MarketTransactionRepo) GetTransactionsByBuyer(ctx context.Context, buyerID string) ([]*models.MarketTransaction, error) {
	query := `SELECT ` + txCols + ` FROM market_transactions WHERE buyer_id = $1 ORDER BY created_at DESC`
	rows, err := r.db.db.QueryContext(ctx, query, buyerID)
	if err != nil {
		return nil, fmt.Errorf("get transactions by buyer: %w", err)
	}
	defer rows.Close()

	var txs []*models.MarketTransaction
	for rows.Next() {
		t, err := scanMarketTransaction(rows)
		if err != nil {
			return nil, fmt.Errorf("get transactions by buyer scan: %w", err)
		}
		txs = append(txs, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get transactions by buyer rows: %w", err)
	}
	return txs, nil
}

// GetRecentTransactions returns the most recent market transactions.
func (r *MarketTransactionRepo) GetRecentTransactions(ctx context.Context, limit int) ([]*models.MarketTransaction, error) {
	query := `SELECT ` + txCols + ` FROM market_transactions ORDER BY created_at DESC LIMIT $1`
	rows, err := r.db.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("get recent transactions: %w", err)
	}
	defer rows.Close()

	var txs []*models.MarketTransaction
	for rows.Next() {
		t, err := scanMarketTransaction(rows)
		if err != nil {
			return nil, fmt.Errorf("get recent transactions scan: %w", err)
		}
		txs = append(txs, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get recent transactions rows: %w", err)
	}
	return txs, nil
}

// scanMarketTransaction scans a single market transaction row.
func scanMarketTransaction(sc interface{ Scan(dest ...any) error }) (*models.MarketTransaction, error) {
	t := &models.MarketTransaction{}
	err := sc.Scan(
		&t.ID,
		&t.ListingID,
		&t.BuyerID,
		&t.SellerID,
		&t.Price,
		&t.BurnAmount,
		&t.FoalID,
		&t.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return t, nil
}
