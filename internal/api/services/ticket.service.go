package service

import (
	"strings"
	"time"

	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/adapter/input/models"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/ports/output"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/domain"
	"github.com/google/uuid"
)

func NewTicketService(repository port.TicketRepository, capacity int) TicketService {
	return &ticketService{
		repository: repository,
		capacity:   capacity,
	}
}

type TicketService interface {
	Create(*model.CreateTicket) ([]uuid.UUID, error)
	List(uuid.UUID) (*domain.Ticket, error)
	DeleteMany([]uuid.UUID) ([]uuid.UUID, error)
	Stock() (*domain.Stock, error)
}

type ticketService struct {
	repository port.TicketRepository
	capacity   int
}

func (service *ticketService) Create(data *model.CreateTicket) ([]uuid.UUID, error) {
	// Normaliza para que o mesmo email com outra caixa conte como o mesmo
	// destinatário ao agrupar as devoluções.
	email := strings.ToLower(strings.TrimSpace(data.Email))

	now := time.Now()
	tickets := make([]*domain.Ticket, 0, data.Quantity)
	ticketIDs := make([]uuid.UUID, 0, data.Quantity)

	for range data.Quantity {
		ticket := &domain.Ticket{
			ID:         uuid.New(),
			Email:      email,
			ReservedAt: now,
			CreatedAt:  now,
		}

		tickets = append(tickets, ticket)
		ticketIDs = append(ticketIDs, ticket.ID)
	}

	event, err := domain.NewTicketEvent(domain.EventCreated, email, ticketIDs, now)
	if err != nil {
		return nil, err
	}

	result, err := service.repository.ReserveMany(service.capacity, tickets, event)
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

// DeleteMany cancela todos os tickets ou nenhum. Gera um evento por email,
// assim cada destinatário recebe um único email com os tickets dele.
func (service *ticketService) DeleteMany(ids []uuid.UUID) ([]uuid.UUID, error) {
	ids = uniqueIDs(ids)

	tickets, err := service.repository.FindTicketsByIDs(ids)
	if err != nil {
		return nil, err
	}

	if len(tickets) != len(ids) {
		return nil, domain.ErrTicketNotFound
	}

	groups := map[string][]uuid.UUID{}
	order := []string{}

	for _, ticket := range tickets {
		if _, ok := groups[ticket.Email]; !ok {
			order = append(order, ticket.Email)
		}

		groups[ticket.Email] = append(groups[ticket.Email], ticket.ID)
	}

	now := time.Now()
	events := make([]*domain.Outbox, 0, len(order))

	for _, email := range order {
		event, err := domain.NewTicketEvent(domain.EventDeleted, email, groups[email], now)
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

func (service *ticketService) Stock() (*domain.Stock, error) {
	reserved, returned, err := service.repository.CountTickets()
	if err != nil {
		return nil, err
	}

	return &domain.Stock{
		Capacity:  service.capacity,
		Reserved:  reserved,
		Returned:  returned,
		Available: max(service.capacity-reserved, 0),
	}, nil
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
