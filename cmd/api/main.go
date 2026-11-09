package main

import (
	"database/sql"
	"log"

	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/adapter/input/controllers"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/adapter/output/repository"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/api/services"
	"github.com/PedroPereiraN/webhook-delivery-system/internal/config"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
)

func main() {
	r := gin.Default()

	db, err := sql.Open("postgres", config.DatabaseURL())
	if err != nil {
		log.Fatal(err)
	}

	defer db.Close()

	if err = db.Ping(); err != nil {
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

	tService := service.NewTicketService(tRepository)

	tController := controller.NewTicketController(tService)

	r.POST("/tickets", tController.Create)
	r.GET("/tickets", tController.List)
	r.DELETE("/tickets", tController.Delete)
	r.GET("/stock", tController.Stock)

	r.Run()
}
