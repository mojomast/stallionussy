package repository

import (
	"context"

	"github.com/mojomast/stallionussy/internal/models"
)

type CasinoRepository interface {
	CreatePokerTable(ctx context.Context, table *models.PokerTable) error
	GetPokerTable(ctx context.Context, id string) (*models.PokerTable, error)
	ListPokerTables(ctx context.Context, limit int) ([]*models.PokerTable, error)
	UpdatePokerTable(ctx context.Context, table *models.PokerTable) error
	RecordSlotSpin(ctx context.Context, spin *models.SlotSpin) error
	ListSlotSpinsByUser(ctx context.Context, userID string, limit int) ([]*models.SlotSpin, error)
}

type DepartureRepository interface {
	CreateDeparture(ctx context.Context, record *models.DepartureRecord) error
	GetDeparture(ctx context.Context, id string) (*models.DepartureRecord, error)
	ListDeparturesByOwner(ctx context.Context, ownerID string, limit int) ([]*models.DepartureRecord, error)
	UpdateDeparture(ctx context.Context, record *models.DepartureRecord) error
}
