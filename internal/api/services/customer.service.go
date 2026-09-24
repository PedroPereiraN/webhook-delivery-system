package service

import (
	"errors"
	"time"

	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/adapter/input/models"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/ports/output"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/domain"
	"github.com/google/uuid"
)

func NewCustomerService(
	repository port.CustomerRepository,
) CustomerService {
	return &customerService{
		repository: repository,
	}
}

type CustomerService interface {
	Create(*model.CreateCustomer) (uuid.UUID, error)
	List(uuid.UUID) (*domain.Customer, error)
	Update(uuid.UUID, *model.UpdateCustomer) (uuid.UUID, error)
	Delete(uuid.UUID) (uuid.UUID, error)
}

type customerService struct {
	repository port.CustomerRepository
}

func (service *customerService) Create(data *model.CreateCustomer) (uuid.UUID, error) {
	// check email
	_, err := service.repository.FindCustomerByEmail(data.Email)

	if err != nil && err.Error() != "sql: no rows in result set" {
		return uuid.Nil, err
	}

	if err == nil {
		return uuid.Nil, errors.New("email is already registered")
	}

	cDomain := &domain.Customer{
		ID:        uuid.New(),
		Name:      data.Name,
		Email:     data.Email,
		CreatedAt: time.Now(),
	}

	result, err := service.repository.Create(cDomain)
	if err != nil {
		return uuid.Nil, err
	}

	return result, nil
}

func (service *customerService) List(id uuid.UUID) (*domain.Customer, error) {
	customer, err := service.repository.FindCustomerById(id)
	if err != nil {
		return &domain.Customer{}, err
	}

	return customer, nil
}

func (service *customerService) Update(id uuid.UUID, data *model.UpdateCustomer) (uuid.UUID, error) {
	_, err := service.repository.FindCustomerById(id)
	if err != nil {
		return uuid.Nil, errors.New("customer not found")
	}

	// check email
	emailCheckData, err := service.repository.FindCustomerByEmail(data.Email)

	if err != nil && err.Error() != "sql: no rows in result set" {
		return uuid.Nil, err
	}

	if err == nil && emailCheckData.ID != id {
		return uuid.Nil, errors.New("email is already registered")
	}

	cDomain := &domain.Customer{
		ID:    uuid.Nil,
		Name:  data.Name,
		Email: data.Email,
	}

	result, err := service.repository.Update(id, cDomain)
	if err != nil {
		return uuid.Nil, err
	}

	return result, nil
}

func (service *customerService) Delete(id uuid.UUID) (uuid.UUID, error) {
	_, err := service.repository.FindCustomerById(id)
	if err != nil {
		return uuid.Nil, err
	}

	customerID, err := service.repository.Delete(id)
	if err != nil {
		return uuid.Nil, err
	}

	return customerID, nil
}
