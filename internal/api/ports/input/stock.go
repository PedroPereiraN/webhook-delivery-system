package port

import (
	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/adapter/input/models"
	"github.com/google/uuid"
)

type StockService interface {
	UpdateStockQuantity(
		uuid.UUID,
		*model.UpdateStockQuantity,
	) (uuid.UUID, error)
}
