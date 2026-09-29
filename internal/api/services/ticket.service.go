package service

import (
	"database/sql"
	"errors"
	"time"

	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/adapter/input/models"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/ports/output"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/config"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/domain"
	"github.com/google/uuid"
)

func NewTicketService(
	repository port.TicketRepository,
	stockRepository port.StockRepository,
) TicketService {
	return &ticketService{
		repository:      repository,
		stockRepository: stockRepository,
	}
}

type TicketService interface {
	Create(*model.CreateTicket) ([]uuid.UUID, error)
	List(uuid.UUID) (*domain.Ticket, error)
	Delete(uuid.UUID) (uuid.UUID, error)
}

type ticketService struct {
	repository      port.TicketRepository
	stockRepository port.StockRepository
}

func (service *ticketService) Create(data *model.CreateTicket) ([]uuid.UUID, error) {
	parsedStockID, err := uuid.Parse(config.DefaultStockID)
	if err != nil {
		return nil, err
	}

	stock, err := service.stockRepository.FindStockById(parsedStockID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("stock not found")
	}
	if err != nil {
		return nil, err
	}

	if data.Quantity > stock.FreeQuantity {
		return nil, domain.ErrInsufficientStock
	}

	now := time.Now()
	tickets := make([]*domain.Ticket, 0, data.Quantity)

	for range data.Quantity {
		tickets = append(tickets, &domain.Ticket{
			ID:         uuid.New(),
			StockID:    parsedStockID,
			ReservedBy: data.ReservedBy,
			ReservedAt: now,
			CreatedAt:  now,
		})
	}

	result, err := service.repository.ReserveMany(parsedStockID, tickets)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (service *ticketService) List(id uuid.UUID) (*domain.Ticket, error) {
	ticket, err := service.repository.FindTicketByID(id)
	if err != nil {
		return &domain.Ticket{}, err
	}

	return ticket, nil
}

func (service *ticketService) Delete(id uuid.UUID) (uuid.UUID, error) {
	_, err := service.repository.FindTicketByID(id)
	if err != nil {
		return uuid.Nil, err
	}

	ticketID, err := service.repository.Delete(id)
	if err != nil {
		return uuid.Nil, err
	}

	return ticketID, nil
}
