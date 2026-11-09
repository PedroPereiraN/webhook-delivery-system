package controller

import (
	"errors"
	"net/http"

	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/adapter/input/models"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/ports/input"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/domain"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func NewTicketController(
	service port.TicketService,
) TicketController {
	return &ticketController{
		service: service,
	}
}

type TicketController interface {
	Create(*gin.Context)
	List(*gin.Context)
	Delete(*gin.Context)
	Stock(*gin.Context)
}

type ticketController struct {
	service port.TicketService
}

func (controller *ticketController) Create(c *gin.Context) {
	var ticketData model.CreateTicket

	if err := c.ShouldBindJSON(&ticketData); err != nil {
		if err.Error() == "EOF" {
			c.JSON(http.StatusBadRequest, "Invalid fields")

			return
		}
		c.JSON(http.StatusBadRequest, err.Error())

		return
	}

	result, err := controller.service.Create(&ticketData)
	if errors.Is(err, domain.ErrInsufficientStock) {
		c.JSON(http.StatusConflict, err.Error())

		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, err.Error())

		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Tickets reserved successfully",
		"tickets": result,
	})
}

func (controller *ticketController) List(c *gin.Context) {
	paramsID := c.Query("id")

	if paramsID == "" {
		c.JSON(http.StatusBadRequest, "Unspecified ticket")
		return
	}

	ticketID, err := uuid.Parse(paramsID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, err.Error())
		return
	}

	data, err := controller.service.List(ticketID)
	if err != nil {
		c.JSON(http.StatusNotFound, err.Error())
		return
	}

	result := model.Ticket{
		ID:         data.ID,
		Email:      data.Email,
		ReservedAt: data.ReservedAt,
		CreatedAt:  data.CreatedAt,
		DeletedAt:  data.DeletedAt,
	}

	c.JSON(http.StatusOK, result)
}

func (controller *ticketController) Delete(c *gin.Context) {
	var data model.DeleteTickets

	if err := c.ShouldBindJSON(&data); err != nil {
		if err.Error() == "EOF" {
			c.JSON(http.StatusBadRequest, "Invalid fields")

			return
		}
		c.JSON(http.StatusBadRequest, err.Error())

		return
	}

	result, err := controller.service.DeleteMany(data.IDs)
	if errors.Is(err, domain.ErrTicketNotFound) {
		c.JSON(http.StatusNotFound, err.Error())

		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, err.Error())

		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Tickets deleted successfully",
		"tickets": result,
	})
}

func (controller *ticketController) Stock(c *gin.Context) {
	data, err := controller.service.Stock()
	if err != nil {
		c.JSON(http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusOK, model.Stock{
		Capacity:  data.Capacity,
		Reserved:  data.Reserved,
		Returned:  data.Returned,
		Available: data.Available,
	})
}
