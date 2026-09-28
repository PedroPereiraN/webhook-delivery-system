package service

import (
	"time"

	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/adapter/input/models"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/ports/output"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/config"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/domain"
	"github.com/google/uuid"
)

func NewTicketService(
	repository port.TicketRepository,
) TicketService {
	return &ticketService{
		repository: repository,
	}
}

type TicketService interface {
	Create(*model.CreateTicket) (uuid.UUID, error)
	List(uuid.UUID) (*domain.Ticket, error)
	Delete(uuid.UUID) (uuid.UUID, error)
}

type ticketService struct {
	repository port.TicketRepository
}

func (service *ticketService) Create(data *model.CreateTicket) (uuid.UUID, error) {
	parsedStockID, err := uuid.Parse(config.DefaultStockID)
	if err != nil {
		return uuid.Nil, err
	}

	tDomain := &domain.Ticket{
		ID:         uuid.New(),
		StockID:    parsedStockID,
		ReservedBy: data.ReservedBy,
		ReservedAt: time.Now(),
		CreatedAt:  time.Now(),
	}

	result, err := service.repository.Create(tDomain)
	if err != nil {
		return uuid.Nil, err
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
