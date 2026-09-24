package main

import (
	"database/sql"
	"log"

	controller "github.com/PedroPereiraN/webhook-delivery-system/internal/api/adapter/input/controllers"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/adapter/output/repository"
	service "github.com/PedroPereiraN/webhook-delivery-system/internal/api/services"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
)

func main() {
	r := gin.Default()

	db, err := sql.Open("postgres", "postgres://postgres:postgres@postgres:5432/db?sslmode=disable")
	if err != nil {
		log.Fatal(err)
	}

	defer db.Close()

	if err = db.Ping(); err != nil {
		log.Fatal(err)
	}

	cRepository := repository.NewCustomerRepository(db)

	err = cRepository.CreateTable()
	if err != nil {
		log.Fatal(err)
	}

	cService := service.NewCustomerService(cRepository)
	cController := controller.NewCustomerController(cService)

	r.POST("/customers", cController.Create)
	r.GET("/customers", cController.List)
	r.PUT("/customers", cController.Update)
	r.DELETE("/customers", cController.Delete)

	r.Run()
}
