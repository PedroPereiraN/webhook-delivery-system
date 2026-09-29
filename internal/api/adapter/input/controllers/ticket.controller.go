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
		c.JSON(http.StatusBadRequest, "Unspecified customer")
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
		ReservedBy: data.ReservedBy,
		ReservedAt: data.ReservedAt,
		CreatedAt:  data.CreatedAt,
		DeletedAt:  data.DeletedAt,
	}

	c.JSON(http.StatusOK, result)
}

func (controller *ticketController) Delete(c *gin.Context) {
	paramsID := c.Query("id")

	if paramsID == "" {
		c.JSON(http.StatusBadRequest, "Unspecified customer")

		return
	}

	ticketID, err := uuid.Parse(paramsID)
	if err != nil {
		c.JSON(http.StatusBadRequest, "Invalid ID")
		return
	}

	result, err := controller.service.Delete(ticketID)

	if err != nil && err.Error() == "sql: no rows in result set" {
		c.JSON(http.StatusNotFound, "Ticket not found")
		return
	}

	if err != nil && err.Error() != "sql: no rows in result set" {
		c.JSON(http.StatusInternalServerError, err.Error())
	}

	c.JSON(http.StatusOK, "Ticket deleted successfully: "+result.String())
}
