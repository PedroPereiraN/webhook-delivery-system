package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/adapter/input/models"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/ports/input"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/domain"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func NewDeadLetterController(
	service port.DeadLetterService,
) DeadLetterController {
	return &deadLetterController{
		service: service,
	}
}

type DeadLetterController interface {
	List(*gin.Context)
	Replay(*gin.Context)
}

type deadLetterController struct {
	service port.DeadLetterService
}

func (controller *deadLetterController) List(c *gin.Context) {
	limit := 0

	if param := c.Query("limit"); param != "" {
		parsed, err := strconv.Atoi(param)
		if err != nil {
			c.JSON(http.StatusBadRequest, "Invalid limit")
			return
		}

		limit = parsed
	}

	events, err := controller.service.List(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, err.Error())
		return
	}

	result := make([]model.DeadLetter, 0, len(events))

	for _, event := range events {
		result = append(result, model.DeadLetter{
			ID:        event.ID,
			Email:     event.Email,
			EventType: string(event.EventType),
			Attempts:  event.Attempts,
			LastError: event.LastError,
			FailedAt:  event.FailedAt,
			CreatedAt: event.CreatedAt,
			Payload:   event.Payload,
		})
	}

	c.JSON(http.StatusOK, result)
}

func (controller *deadLetterController) Replay(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, "Invalid event id")
		return
	}

	err = controller.service.Replay(id)
	if errors.Is(err, domain.ErrDeadLetterNotFound) {
		c.JSON(http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Event queued for replay",
		"id":      id,
	})
}
