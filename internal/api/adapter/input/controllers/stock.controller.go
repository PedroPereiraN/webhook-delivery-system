package controller

import (
	"net/http"

	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/adapter/input/models"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/ports/input"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func NewStockController(
	service port.StockService,
) StockController {
	return &stockController{
		service: service,
	}
}

type StockController interface {
	UpdateStockQuantity(*gin.Context)
}

type stockController struct {
	service port.StockService
}

func (controller *stockController) UpdateStockQuantity(c *gin.Context) {
	paramsID := c.Query("id")

	if paramsID == "" {
		c.JSON(http.StatusBadRequest, "Invalid id")

		return
	}

	stockID, err := uuid.Parse(paramsID)
	if err != nil {
		c.JSON(http.StatusBadRequest, "Invalid id")
		return
	}

	var stockData model.UpdateStockQuantity

	if err := c.ShouldBindJSON(&stockData); err != nil {
		c.JSON(http.StatusBadRequest, err.Error())

		return
	}

	result, err := controller.service.UpdateStockQuantity(stockID, &stockData)
	if err != nil {

		c.JSON(http.StatusInternalServerError, err.Error())

		return
	}

	c.JSON(http.StatusOK, "Stock updated successfully: "+result.String())
}
