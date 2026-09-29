package service

import (
	"database/sql"
	"errors"

	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/adapter/input/models"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/ports/output"
	"github.com/google/uuid"
)

func NewStockService(
	repository port.StockRepository,
) StockService {
	return &stockService{
		repository: repository,
	}
}

type StockService interface {
	UpdateStockQuantity(uuid.UUID, *model.UpdateStockQuantity) (uuid.UUID, error)
}

type stockService struct {
	repository port.StockRepository
}

func (service *stockService) UpdateStockQuantity(id uuid.UUID, data *model.UpdateStockQuantity) (uuid.UUID, error) {
	stock, err := service.repository.FindStockById(id)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, errors.New("stock not found")
	}
	if err != nil {
		return uuid.Nil, err
	}

	if data.Type == model.Remove && data.Quantity > stock.FreeQuantity {
		return uuid.Nil, errors.New("insufficient free quantity to remove")
	}

	var quantity int32

	if data.Type == model.Add {
		quantity = data.Quantity
	} else {
		quantity = -data.Quantity
	}

	result, err := service.repository.UpdateStockQuantity(id, quantity)
	if err != nil {
		return uuid.Nil, err
	}

	return result, nil
}
