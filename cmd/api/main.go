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

	sRepository := repository.NewStockRepository(db)

	err = sRepository.CreateTable()
	if err != nil {
		log.Fatal(err)
	}

	sService := service.NewStockService(sRepository)
	sController := controller.NewStockController(sService)

	r.PUT("/stock/update-quantity", sController.UpdateStockQuantity)

	err = sRepository.CreateDefaultStock()
	if err != nil {
		log.Fatal(err)
	}

	tRepository := repository.NewTicketRepository(db)

	err = tRepository.CreateTable()
	if err != nil {
		log.Fatal(err)
	}

	oRepository := repository.NewOutboxRepository(db)

	err = oRepository.CreateTable()
	if err != nil {
		log.Fatal(err)
	}

	tService := service.NewTicketService(tRepository, sRepository, cRepository)

	tController := controller.NewTicketController(tService)

	r.POST("/tickets", tController.Create)
	r.GET("/tickets", tController.List)
	r.DELETE("/tickets", tController.Delete)

	r.Run()
}
