package port

import (
	"github.com/PedroPereiraN/webhook-delivery-system/internal/domain"
	"github.com/google/uuid"
)

type StockRepository interface {
	FindStockById(uuid.UUID) (*domain.Stock, error)
	UpdateStockQuantity(uuid.UUID, int32) (uuid.UUID, error)
}
