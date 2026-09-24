package controller

import (
	"net/http"

	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/adapter/input/models"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/ports/input"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func NewCustomerController(
	service port.CustomerService,
) CustomerController {
	return &customerController{
		service: service,
	}
}

type CustomerController interface {
	Create(*gin.Context)
	List(*gin.Context)
	Update(*gin.Context)
	Delete(*gin.Context)
}

type customerController struct {
	service port.CustomerService
}

func (controller *customerController) Create(c *gin.Context) {
	var customerData model.CreateCustomer

	if err := c.ShouldBindJSON(&customerData); err != nil {
		if err.Error() == "EOF" {
			c.JSON(http.StatusBadRequest, "Invalid fields")

			return
		}
		c.JSON(http.StatusBadRequest, err.Error())

		return
	}

	result, err := controller.service.Create(&customerData)
	if err != nil {
		c.JSON(http.StatusInternalServerError, err.Error())

		return
	}

	c.JSON(http.StatusOK, "Customer created successfully: "+result.String())
}

func (controller *customerController) List(c *gin.Context) {
	paramsID := c.Query("id")

	if paramsID == "" {
		c.JSON(http.StatusBadRequest, "Unspecified customer")
		return
	}

	customerID, err := uuid.Parse(paramsID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, err.Error())
		return
	}

	data, err := controller.service.List(customerID)
	if err != nil {
		c.JSON(http.StatusNotFound, err.Error())
		return
	}

	result := model.Customer{
		ID:        data.ID,
		Name:      data.Name,
		Email:     data.Email,
		CreatedAt: data.CreatedAt,
		UpdatedAt: data.UpdatedAt,
		DeletedAt: data.DeletedAt,
	}

	c.JSON(http.StatusOK, result)
}

func (controller *customerController) Update(c *gin.Context) {
	paramsID := c.Query("id")

	if paramsID == "" {
		c.JSON(http.StatusBadRequest, "Invalid id")

		return
	}

	customerID, err := uuid.Parse(paramsID)
	if err != nil {
		c.JSON(http.StatusBadRequest, "Invalid id")
		return
	}

	var customerData model.UpdateCustomer

	if err := c.ShouldBindJSON(&customerData); err != nil {
		c.JSON(http.StatusBadRequest, err.Error())

		return
	}

	result, err := controller.service.Update(customerID, &customerData)
	if err != nil {

		c.JSON(http.StatusInternalServerError, err.Error())

		return
	}

	c.JSON(http.StatusOK, "Customer updated successfully: "+result.String())
}

func (controller *customerController) Delete(c *gin.Context) {
	paramsID := c.Query("id")

	if paramsID == "" {
		c.JSON(http.StatusBadRequest, "Unspecified customer")

		return
	}

	customerID, err := uuid.Parse(paramsID)
	if err != nil {
		c.JSON(http.StatusBadRequest, "Invalid ID")
		return
	}

	result, err := controller.service.Delete(customerID)

	if err != nil && err.Error() == "sql: no rows in result set" {
		c.JSON(http.StatusNotFound, "Customer not found")
		return
	}

	if err != nil && err.Error() != "sql: no rows in result set" {
		c.JSON(http.StatusInternalServerError, err.Error())
	}

	c.JSON(http.StatusOK, "Customer deleted successfully: "+result.String())
}
