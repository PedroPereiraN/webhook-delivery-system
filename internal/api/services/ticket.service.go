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
	customerRepository port.CustomerRepository,
) TicketService {
	return &ticketService{
		repository:         repository,
		stockRepository:    stockRepository,
		customerRepository: customerRepository,
	}
}

type TicketService interface {
	Create(*model.CreateTicket) ([]uuid.UUID, error)
	List(uuid.UUID) (*domain.Ticket, error)
	DeleteMany([]uuid.UUID) ([]uuid.UUID, error)
}

type ticketService struct {
	repository         port.TicketRepository
	stockRepository    port.StockRepository
	customerRepository port.CustomerRepository
}

func (service *ticketService) Create(data *model.CreateTicket) ([]uuid.UUID, error) {
	parsedStockID, err := uuid.Parse(config.DefaultStockID)
	if err != nil {
		return nil, err
	}

	customer, err := service.findCustomer(data.ReservedBy)
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
	ticketIDs := make([]uuid.UUID, 0, data.Quantity)

	for range data.Quantity {
		ticket := &domain.Ticket{
			ID:         uuid.New(),
			StockID:    parsedStockID,
			ReservedBy: data.ReservedBy,
			ReservedAt: now,
			CreatedAt:  now,
		}

		tickets = append(tickets, ticket)
		ticketIDs = append(ticketIDs, ticket.ID)
	}

	event, err := domain.NewTicketEvent(domain.EventCreated, customer, parsedStockID, ticketIDs, now)
	if err != nil {
		return nil, err
	}

	result, err := service.repository.ReserveMany(parsedStockID, tickets, event)
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

// DeleteMany cancela todos os tickets ou nenhum. Gera um evento por
// customer/stock, assim cada customer recebe um único email com os tickets dele.
func (service *ticketService) DeleteMany(ids []uuid.UUID) ([]uuid.UUID, error) {
	ids = uniqueIDs(ids)

	tickets, err := service.repository.FindTicketsByIDs(ids)
	if err != nil {
		return nil, err
	}

	if len(tickets) != len(ids) {
		return nil, domain.ErrTicketNotFound
	}

	type groupKey struct {
		customerID uuid.UUID
		stockID    uuid.UUID
	}

	groups := map[groupKey][]uuid.UUID{}
	order := []groupKey{}

	for _, ticket := range tickets {
		key := groupKey{customerID: ticket.ReservedBy, stockID: ticket.StockID}

		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}

		groups[key] = append(groups[key], ticket.ID)
	}

	now := time.Now()
	events := make([]*domain.Outbox, 0, len(order))

	for _, key := range order {
		customer, err := service.findCustomer(key.customerID)
		if err != nil {
			return nil, err
		}

		event, err := domain.NewTicketEvent(domain.EventDeleted, customer, key.stockID, groups[key], now)
		if err != nil {
			return nil, err
		}

		events = append(events, event)
	}

	if err := service.repository.DeleteMany(ids, events); err != nil {
		return nil, err
	}

	return ids, nil
}

func (service *ticketService) findCustomer(id uuid.UUID) (*domain.Customer, error) {
	customer, err := service.customerRepository.FindCustomerById(id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrCustomerNotFound
	}
	if err != nil {
		return nil, err
	}

	return customer, nil
}

// uniqueIDs remove IDs repetidos mantendo a ordem, senão a contagem de
// tickets encontrados nunca bateria com a de IDs enviados.
func uniqueIDs(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	result := make([]uuid.UUID, 0, len(ids))

	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}

		seen[id] = struct{}{}
		result = append(result, id)
	}

	return result
}
