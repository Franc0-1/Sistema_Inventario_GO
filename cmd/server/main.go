// Punto de entrada: carga configuración, construye las dependencias
// (repository -> services -> handlers) y levanta el servidor HTTP.
// Es el ÚNICO lugar que conoce la implementación concreta del repositorio.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"inventario/internal/handlers"
	"inventario/internal/repository"
	"inventario/internal/services"
	"inventario/internal/utils"
)

func main() {
	cfg := utils.CargarConfig()

	// Único lugar que elige la implementación concreta del repositorio.
	// Otro almacenamiento en el futuro = otra implementación de
	// repository.Repository, sin tocar services ni handlers.
	repo, err := repository.NewExcelRepository(cfg.RutaExcel)
	if err != nil {
		log.Fatalf("repositorio: %v", err)
	}
	defer repo.Close()

	service := services.NewInventoryService(repo)
	inventory := handlers.NewInventoryHandler(service)
	web, err := handlers.NewWebHandler(cfg.RutaTemplates)
	if err != nil {
		log.Fatalf("frontend: %v", err)
	}

	mux := http.NewServeMux()
	inventory.RegisterRoutes(mux)
	web.RegisterRoutes(mux)

	srv := &http.Server{
		Addr:              ":" + cfg.Puerto,
		Handler:           handlers.WithMiddleware(mux, cfg.CORSOrigins),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("servidor escuchando en %s (excel: %s, CORS: %v)", srv.Addr, cfg.RutaExcel, cfg.CORSOrigins)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("servidor: %v", err)
		}
	}()

	// Apagado ordenado: deja terminar la escritura en curso del Excel.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("apagado: %v", err)
	}
}
