// Punto de entrada: carga configuración, construye las dependencias
// (repository -> services -> handlers) y levanta el servidor HTTP.
// Es el ÚNICO lugar que conoce la implementación concreta del repositorio.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"inventario/internal/config"
	"inventario/internal/handlers"
	"inventario/internal/repository"
	"inventario/internal/services"
)

func main() {
	cfg := config.Cargar()

	// Único lugar que elige la implementación concreta del repositorio
	// (INVENTARIO_STORAGE): services y handlers solo conocen la interfaz.
	repo, origen, err := abrirRepositorio(cfg)
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
	// Modelo nuevo (oficinas, personas, tipos…): solo existe en SQL Server.
	if base, ok := repo.(*repository.SQLServerRepository); ok {
		handlers.NewCatalogoHandler(services.NewCatalogoService(base)).RegisterRoutes(mux)
		handlers.NewEquipoHandler(services.NewEquipoService(base, base)).RegisterRoutes(mux)
		handlers.NewInsumoHandler(services.NewInsumoService(base, base)).RegisterRoutes(mux)
		horaSalida, err := cfg.HoraDeSalida()
		if err != nil {
			log.Fatalf("configuración: %v", err)
		}
		handlers.NewPrestamoHandler(services.NewPrestamoService(base, base, base, horaSalida)).RegisterRoutes(mux)
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Puerto,
		Handler:           handlers.WithMiddleware(mux, cfg.CORSOrigins),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("servidor escuchando en %s (almacenamiento: %s, CORS: %v)", srv.Addr, origen, cfg.CORSOrigins)
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

// abrirRepositorio crea el almacenamiento configurado. origen describe dónde
// están los datos para el log (nunca incluye la contraseña de la base).
func abrirRepositorio(cfg config.Config) (repository.Repository, string, error) {
	switch cfg.Almacenamiento {
	case "excel":
		repo, err := repository.NewExcelRepository(cfg.RutaExcel)
		return repo, "excel " + cfg.RutaExcel, err
	case "sqlserver":
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		repo, err := repository.NewSQLServerRepository(ctx, cfg.DBDSN)
		return repo, "sqlserver", err
	}
	return nil, "", fmt.Errorf("INVENTARIO_STORAGE=%q no es válido (use excel o sqlserver)", cfg.Almacenamiento)
}
